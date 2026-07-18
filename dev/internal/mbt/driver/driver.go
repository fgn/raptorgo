// Package driver realizes ObjectDecoderModel schedules through the public Go
// API and checks a local, Java-free oracle after every call.
package driver

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fgn/raptorgo"
	"github.com/fgn/raptorgo/dev/internal/mbt/schedule"
	"github.com/fgn/raptorgo/dev/internal/objtrace"
	"github.com/fgn/raptorgo/internal/codec"
)

var injectedCallbackFailure = errors.New("mbt: injected callback failure")

// Result contains the independently replayable implementation trace.
type Result struct {
	Events []objtrace.Event
}

// ReadSchedule strictly decodes and validates one schedule.
func ReadSchedule(reader io.Reader) (schedule.Schedule, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var value schedule.Schedule
	if err := decoder.Decode(&value); err != nil {
		return schedule.Schedule{}, fmt.Errorf("mbt: decode schedule: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return schedule.Schedule{}, fmt.Errorf("mbt: decode schedule: %w", err)
	}
	if err := ValidateSchedule(value); err != nil {
		return schedule.Schedule{}, err
	}
	return value, nil
}

// ReadScheduleFile reads one committed schedule.
func ReadScheduleFile(path string) (schedule.Schedule, error) {
	file, err := os.Open(path)
	if err != nil {
		return schedule.Schedule{}, fmt.Errorf("mbt: open schedule %s: %w", path, err)
	}
	value, readErr := ReadSchedule(file)
	closeErr := file.Close()
	if readErr != nil {
		return schedule.Schedule{}, readErr
	}
	if closeErr != nil {
		return schedule.Schedule{}, fmt.Errorf("mbt: close schedule %s: %w", path, closeErr)
	}
	return value, nil
}

// ValidateSchedule checks the closed executable schedule schema.
func ValidateSchedule(value schedule.Schedule) error {
	if value.V != 1 || value.Model != "ObjectDecoderModel" || value.Cfg == "" {
		return errors.New("mbt: invalid schedule header")
	}
	if len(value.GraphDigest) != 64 {
		return errors.New("mbt: schedule has no 64-hex graph digest")
	}
	if _, err := hex.DecodeString(value.GraphDigest); err != nil {
		return errors.New("mbt: schedule graph digest is not hexadecimal")
	}
	switch value.Fixture {
	case "two-block", "one-block", "rank-deficient", "zero-length":
	default:
		return fmt.Errorf("mbt: unknown fixture %q", value.Fixture)
	}
	if len(value.Steps) == 0 || len(value.Steps) > schedule.DepthCap {
		return fmt.Errorf("mbt: schedule has %d steps", len(value.Steps))
	}
	finalizations := 0
	decodeFailure := false
	for index, step := range value.Steps {
		if len(step.Expect.Events) == 0 || step.Expect.State == "" || step.Expect.Result == "" ||
			!step.Expect.CompletedPresent || len(step.ModelEdges) == 0 {
			return fmt.Errorf("mbt: step %d has incomplete oracle", index)
		}
		for _, edge := range step.ModelEdges {
			if edge.Src == "" || edge.Action == "" || edge.Dst == "" {
				return fmt.Errorf("mbt: step %d has incomplete model edge", index)
			}
		}
		switch step.Action {
		case "AddWait", "BlockComplete", "RejectConflict", "FinalizeVerifyEnter":
			if step.SBN == nil || step.ESI == nil {
				return fmt.Errorf("mbt: step %d action %s has no packet coordinates", index, step.Action)
			}
		case "RejectInvalid", "RejectLimit", "TerminalAdd", "DecodeFail":
			if step.SBN != nil || step.ESI != nil {
				return fmt.Errorf("mbt: step %d action %s has unexpected coordinates", index, step.Action)
			}
		case "ConstructDelivered", "ConstructVerifyFailed", "ConstructDeliveryFailed":
			if value.Fixture != "zero-length" || len(value.Steps) != 1 {
				return fmt.Errorf("mbt: construct action requires one zero-length step")
			}
		default:
			return fmt.Errorf("mbt: step %d has unknown action %q", index, step.Action)
		}
		if step.Action == "FinalizeVerifyEnter" {
			finalizations++
			if err := validateCallbacks(step.Callbacks); err != nil {
				return fmt.Errorf("mbt: step %d: %w", index, err)
			}
		} else if step.Callbacks != nil {
			return fmt.Errorf("mbt: step %d has callbacks outside finalization", index)
		}
		decodeFailure = decodeFailure || step.Action == "DecodeFail"
	}
	if finalizations > 1 {
		return errors.New("mbt: schedule has multiple finalizations")
	}
	switch value.Cfg {
	case "A":
		if value.Fixture != "two-block" || decodeFailure {
			return errors.New("mbt: cfg A requires the two-block non-decode fixture")
		}
	case "B":
		if decodeFailure != (value.Fixture == "rank-deficient") {
			return errors.New("mbt: cfg B DecodeFail and rank-deficient fixture must coincide")
		}
		if value.Fixture != "one-block" && value.Fixture != "rank-deficient" {
			return errors.New("mbt: cfg B requires one-block or rank-deficient fixture")
		}
	case "construct":
		if value.Fixture != "zero-length" {
			return errors.New("mbt: construct cfg requires zero-length fixture")
		}
	default:
		return fmt.Errorf("mbt: unknown cfg %q", value.Cfg)
	}
	return nil
}

// Run executes one schedule and checks state, completion, error class, event
// order, and callback binding after every public API operation.
func Run(value schedule.Schedule) (Result, error) {
	if err := ValidateSchedule(value); err != nil {
		return Result{}, err
	}
	return runExecutable(value)
}

func runExecutable(value schedule.Schedule) (Result, error) {
	if value.Fixture == "zero-length" {
		return runZeroLength(value)
	}
	callbacks := callbackPlan(value)
	fixture, err := buildFixture(value, callbacks)
	if err != nil {
		return Result{}, finalFailure(FailureFixture, "ready", "fixture-error", err)
	}
	for index, step := range value.Steps {
		before := len(fixture.recorder.Events())
		packet, err := fixture.packetFor(step)
		if err != nil {
			return fixture.result(), stepFailure(
				index, step.Action, FailurePacket, "packet-ready", errorClass(err), err,
			)
		}
		state, addErr := fixture.recorder.AddPacket(packet)
		if fixture.callbackErr != nil {
			return fixture.result(), stepFailure(
				index, step.Action, FailureCallback, "none", "callback-error", fixture.callbackErr,
			)
		}
		if err := checkError(step.Expect.Result, addErr, step.Action == "DecodeFail"); err != nil {
			return fixture.result(), stepFailure(
				index, step.Action, FailureError, step.Expect.Result, errorClass(addErr), err,
			)
		}
		if got := state.String(); got != step.Expect.State {
			return fixture.result(), stepFailure(
				index, step.Action, FailureState, step.Expect.State, got, nil,
			)
		}
		if got := fixture.decoder.CompletedBlocks(); got != step.Expect.Completed {
			return fixture.result(), stepFailure(
				index, step.Action, FailureComplete,
				fmt.Sprint(step.Expect.Completed), fmt.Sprint(got), nil,
			)
		}
		events := fixture.recorder.Events()
		if err := checkEventTail(events[before:], step.Expect.Events); err != nil {
			return fixture.result(), stepFailure(
				index, step.Action, FailureEvents,
				strings.Join(step.Expect.Events, ","), eventSignature(events[before:]), err,
			)
		}
	}
	events := fixture.recorder.Events()
	if err := objtrace.ValidateTrace(events); err != nil {
		return Result{Events: events}, finalFailure(FailureTrace, "valid", "invalid", err)
	}
	return Result{Events: events}, nil
}

type fixture struct {
	oti         raptorgo.OTI
	limits      raptorgo.Limits
	decoder     *raptorgo.ObjectDecoder
	recorder    *objtrace.Recorder
	packets     map[[2]uint32][]byte
	invalid     []byte
	limit       []byte
	terminal    []byte
	decodeFail  []byte
	callbackErr error
}

func (fixture *fixture) result() Result {
	return Result{Events: fixture.recorder.Events()}
}

func buildFixture(value schedule.Schedule, callbacks schedule.Callbacks) (*fixture, error) {
	if value.Fixture == "two-block" {
		return buildTwoBlock(callbacks)
	}
	if value.Fixture == "rank-deficient" {
		return buildDecodeFailure(callbacks)
	}
	return buildOneBlock(callbacks)
}

func buildTwoBlock(callbacks schedule.Callbacks) (*fixture, error) {
	oti := raptorgo.OTI{TransferLength: 13, SymbolSize: 4, SourceBlocks: 2, SubBlocks: 2, SymbolAlignment: 1}
	object := []byte{3, 19, 41, 67, 101, 149, 211, 17, 53, 109, 181, 13, 97}
	pinned := map[[2]uint32]string{
		{0, 0}: "0000000003136595",
		{0, 1}: "000000012943d311",
		{1, 0}: "01000000356d61",
		{1, 1}: "01000001b50d",
	}
	packets := make(map[[2]uint32][]byte, len(pinned))
	encoder, err := raptorgo.NewObjectEncoder(object, oti, raptorgo.DefaultLimits())
	if err != nil {
		return nil, err
	}
	for key, encoded := range pinned {
		packet, err := hex.DecodeString(encoded)
		if err != nil {
			return nil, err
		}
		generated, err := encoder.Packet(uint8(key[0]), key[1], 1, true)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(packet, generated) {
			return nil, fmt.Errorf("mbt: independent packet (%d,%d) drifted: %x != %x", key[0], key[1], packet, generated)
		}
		packets[key] = packet
	}
	limits := raptorgo.DefaultLimits()
	limits.MaxPacketBytes = 8
	return constructFixture(oti, limits, object, "mbt-two-block", packets, callbacks, nil)
}

func buildOneBlock(callbacks schedule.Callbacks) (*fixture, error) {
	oti := raptorgo.OTI{TransferLength: 1, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	object := []byte{3}
	packets := map[[2]uint32][]byte{{0, 0}: {0, 0, 0, 0, 3}}
	limits := raptorgo.DefaultLimits()
	limits.MaxPacketBytes = 5
	return constructFixture(oti, limits, object, "mbt-one-block", packets, callbacks, nil)
}

func buildDecodeFailure(callbacks schedule.Callbacks) (*fixture, error) {
	oti := raptorgo.OTI{TransferLength: 10, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	object := []byte{3, 19, 41, 67, 101, 149, 211, 17, 53, 109}
	encoder, err := raptorgo.NewObjectEncoder(object, oti, raptorgo.DefaultLimits())
	if err != nil {
		return nil, err
	}
	limits := raptorgo.DefaultLimits()
	limits.MaxPacketBytes = 5
	limits.MaxRetainedSymbols = 11
	limits.MaxRepairSymbolsPerBlock = 1
	limits.MaxDecodeAttempts = 1
	var prologue [][]byte
	for esi := uint32(319); esi <= 328; esi++ {
		packet, packetErr := encoder.Packet(0, esi, 1, false)
		if packetErr != nil {
			return nil, packetErr
		}
		prologue = append(prologue, packet)
	}
	failure, err := encoder.Packet(0, 329, 1, false)
	if err != nil {
		return nil, err
	}
	value, err := constructFixture(oti, limits, object, "mbt-decode-failure", nil, callbacks, prologue)
	if err != nil {
		return nil, err
	}
	value.decodeFail = failure
	value.terminal = append([]byte(nil), prologue[0]...)
	return value, nil
}

func constructFixture(
	oti raptorgo.OTI,
	limits raptorgo.Limits,
	object []byte,
	context string,
	packets map[[2]uint32][]byte,
	callbacks schedule.Callbacks,
	prologue [][]byte,
) (*fixture, error) {
	value := &fixture{oti: oti, limits: limits, packets: packets}
	value.recorder = objtrace.NewRecorder(oti, context, object)
	verifier := value.recorder.VerifierFunc(func(raptorgo.VerificationRequest) error {
		for count := 0; count < callbacks.VerifyReentrantAdds; count++ {
			state, err := value.recorder.AddPacket(value.terminal)
			if !errors.Is(err, raptorgo.ErrObjectTerminal) || state != raptorgo.ObjectDecoded {
				value.callbackErr = fmt.Errorf("verification reentrant add state=%s error=%v", state, err)
				return value.callbackErr
			}
		}
		if callbacks.Verify == "fail" {
			return injectedCallbackFailure
		}
		return nil
	})
	deliverer := value.recorder.DelivererFunc(func(raptorgo.VerifiedObject) error {
		for count := 0; count < callbacks.DeliverReentrantAdds; count++ {
			state, err := value.recorder.AddPacket(value.terminal)
			if !errors.Is(err, raptorgo.ErrObjectTerminal) || state != raptorgo.ObjectVerified {
				value.callbackErr = fmt.Errorf("delivery reentrant add state=%s error=%v", state, err)
				return value.callbackErr
			}
		}
		if callbacks.Deliver == "fail" {
			return injectedCallbackFailure
		}
		return nil
	})
	decoder, err := raptorgo.NewObjectDecoder(oti, limits, context, verifier, deliverer)
	if err != nil {
		return nil, err
	}
	value.decoder = decoder
	for index, packet := range prologue {
		state, addErr := decoder.AddPacket(packet)
		if addErr != nil || state != raptorgo.ObjectReceiving || decoder.CompletedBlocks() != 0 {
			return nil, fmt.Errorf("mbt: deficient prologue %d state=%s completed=%d error=%v",
				index, state, decoder.CompletedBlocks(), addErr)
		}
	}
	value.recorder.BindDecoder(decoder)
	value.invalid = []byte{oti.SourceBlocks, 0, 0, 0, 0}
	value.limit = make([]byte, int(limits.MaxPacketBytes)+1)
	if len(value.limit) >= raptorgo.PayloadIDEncodedSize {
		copy(value.limit[:raptorgo.PayloadIDEncodedSize], []byte{0, 0, 0, 0})
	}
	if value.terminal == nil {
		value.terminal = append([]byte(nil), packets[[2]uint32{0, 0}]...)
	}
	return value, nil
}

func (fixture *fixture) packetFor(step schedule.Step) ([]byte, error) {
	switch step.Action {
	case "AddWait", "BlockComplete", "FinalizeVerifyEnter":
		packet := fixture.packets[[2]uint32{uint32(*step.SBN), *step.ESI}]
		if packet == nil {
			return nil, fmt.Errorf("no packet for (%d,%d)", *step.SBN, *step.ESI)
		}
		return append([]byte(nil), packet...), nil
	case "RejectInvalid":
		return append([]byte(nil), fixture.invalid...), nil
	case "RejectLimit":
		return append([]byte(nil), fixture.limit...), nil
	case "RejectConflict":
		packet := append([]byte(nil), fixture.packets[[2]uint32{uint32(*step.SBN), *step.ESI}]...)
		if len(packet) <= raptorgo.PayloadIDEncodedSize {
			return nil, errors.New("conflict packet has no transmitted payload byte")
		}
		packet[raptorgo.PayloadIDEncodedSize] ^= 0xff
		if _, err := raptorgo.ParseEncodingPacket(packet, fixture.oti, fixture.limits); err != nil {
			return nil, fmt.Errorf("conflict packet does not parse: %w", err)
		}
		return packet, nil
	case "TerminalAdd":
		return append([]byte(nil), fixture.terminal...), nil
	case "DecodeFail":
		if fixture.decodeFail == nil {
			return nil, errors.New("DecodeFail used without deficient fixture")
		}
		return append([]byte(nil), fixture.decodeFail...), nil
	default:
		return nil, fmt.Errorf("unsupported packet action %s", step.Action)
	}
}

func runZeroLength(value schedule.Schedule) (Result, error) {
	step := value.Steps[0]
	oti := raptorgo.OTI{TransferLength: 0, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	recorder := objtrace.NewRecorder(oti, "mbt-zero-length", nil)
	var verificationError, deliveryError error
	switch step.Action {
	case "ConstructDelivered":
	case "ConstructVerifyFailed":
		verificationError = injectedCallbackFailure
	case "ConstructDeliveryFailed":
		deliveryError = injectedCallbackFailure
	default:
		return Result{}, stepFailure(
			0, step.Action, FailureFixture, "supported zero-length action", "unsupported", nil,
		)
	}
	decoder, constructorErr := raptorgo.NewObjectDecoder(
		oti, raptorgo.DefaultLimits(), "mbt-zero-length",
		recorder.Verifier(verificationError), recorder.Deliverer(deliveryError),
	)
	if decoder == nil {
		return Result{}, stepFailure(
			0, step.Action, FailureFixture, "non-nil decoder", "nil decoder", constructorErr,
		)
	}
	recorder.BindDecoder(decoder)
	result := Result{Events: recorder.Events()}
	if err := checkError(step.Expect.Result, constructorErr, false); err != nil {
		return result, stepFailure(
			0, step.Action, FailureError, step.Expect.Result, errorClass(constructorErr), err,
		)
	}
	if got := decoder.State().String(); got != step.Expect.State {
		return result, stepFailure(0, step.Action, FailureState, step.Expect.State, got, nil)
	}
	if got := decoder.CompletedBlocks(); got != step.Expect.Completed {
		return result, stepFailure(
			0, step.Action, FailureComplete,
			fmt.Sprint(step.Expect.Completed), fmt.Sprint(got), nil,
		)
	}
	events := result.Events
	if err := checkEventTail(events, step.Expect.Events); err != nil {
		return result, stepFailure(
			0, step.Action, FailureEvents,
			strings.Join(step.Expect.Events, ","), eventSignature(events), err,
		)
	}
	if err := objtrace.ValidateTrace(events); err != nil {
		return result, finalFailure(FailureTrace, "valid", "invalid", err)
	}
	return result, nil
}

func callbackPlan(value schedule.Schedule) schedule.Callbacks {
	for _, step := range value.Steps {
		if step.Callbacks != nil {
			return *step.Callbacks
		}
	}
	return schedule.Callbacks{Verify: "ok", Deliver: "ok"}
}

func validateCallbacks(callbacks *schedule.Callbacks) error {
	if callbacks == nil {
		return errors.New("finalization has no callbacks")
	}
	if callbacks.Verify != "ok" && callbacks.Verify != "fail" {
		return fmt.Errorf("invalid verifier outcome %q", callbacks.Verify)
	}
	if callbacks.Verify == "fail" {
		if callbacks.Deliver != "" || callbacks.DeliverReentrantAdds != 0 {
			return errors.New("failed verification cannot invoke delivery")
		}
	} else if callbacks.Deliver != "ok" && callbacks.Deliver != "fail" {
		return fmt.Errorf("invalid deliverer outcome %q", callbacks.Deliver)
	}
	if callbacks.VerifyReentrantAdds < 0 || callbacks.DeliverReentrantAdds < 0 ||
		callbacks.VerifyReentrantAdds+callbacks.DeliverReentrantAdds > schedule.DepthCap {
		return errors.New("invalid reentrant add count")
	}
	return nil
}

func checkError(result string, err error, exactDecode bool) error {
	wants := make(map[error]bool)
	switch result {
	case "ok":
		if err != nil {
			return fmt.Errorf("error=%v, want nil", err)
		}
		return nil
	case "verification_error":
		wants[raptorgo.ErrObjectVerification] = true
	case "delivery_error":
		wants[raptorgo.ErrObjectDelivery] = true
	case "decode_error":
		wants[raptorgo.ErrObjectDecoder] = true
		if exactDecode {
			wants[codec.ErrDecoderAttemptLimit] = true
		}
	case "terminal":
		wants[raptorgo.ErrObjectTerminal] = true
	case "invalid":
		wants[raptorgo.ErrInvalidPacket] = true
	case "limit":
		wants[raptorgo.ErrLimitExceeded] = true
	case "conflict":
		wants[raptorgo.ErrObjectDecoder] = true
		wants[codec.ErrDecoderDuplicateConflict] = true
	default:
		return fmt.Errorf("unknown expected result %q", result)
	}
	if err == nil {
		return fmt.Errorf("error=nil, want %s", result)
	}
	for _, sentinel := range oracleErrorSentinels {
		got, want := errors.Is(err, sentinel), wants[sentinel]
		if got != want {
			return fmt.Errorf("error=%v sentinel %v=%t, want %t", err, sentinel, got, want)
		}
	}
	return nil
}

// oracleErrorSentinels is the closed error alphabet asserted both positively
// and negatively by Oracle 1. New expected result classes must extend this
// list and the mapping above together.
var oracleErrorSentinels = []error{
	raptorgo.ErrObjectVerification,
	raptorgo.ErrObjectDelivery,
	raptorgo.ErrObjectDecoder,
	raptorgo.ErrObjectTerminal,
	raptorgo.ErrInvalidPacket,
	raptorgo.ErrInvalidPayloadID,
	raptorgo.ErrLimitExceeded,
	codec.ErrDecoderLimits,
	codec.ErrDecoderBacking,
	codec.ErrDecoderSymbol,
	codec.ErrDecoderAttemptLimit,
	codec.ErrDecoderDuplicateConflict,
	codec.ErrDecoderRetainedLimit,
	codec.ErrDecoderComplexity,
	codec.ErrDecoderConflict,
	codec.ErrDecoderComplete,
	codec.ErrDecoderNotComplete,
}

func checkEventTail(events []objtrace.Event, operations []string) error {
	if len(events) != len(operations) {
		return fmt.Errorf("event count=%d, want %d (%v)", len(events), len(operations), operations)
	}
	for index, operation := range operations {
		if events[index].Op != operation {
			return fmt.Errorf("event %d=%s, want %s", index, events[index].Op, operation)
		}
		if !events[index].BindingOK {
			return fmt.Errorf("event %d %s has false binding", index, operation)
		}
	}
	return nil
}
