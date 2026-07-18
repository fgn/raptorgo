package objtrace

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/fgn/raptorgo"
)

var (
	errVerificationBinding = errors.New("objtrace: verification binding mismatch")
	errDeliveryBinding     = errors.New("objtrace: delivery binding mismatch")
)

// Recorder observes a single-goroutine ObjectDecoder driver. Its event store
// is synchronized so callbacks may reenter AddPacket, but concurrent packet
// drivers are intentionally outside the trace schema's ordering contract.
type Recorder struct {
	mu sync.Mutex

	decoder *raptorgo.ObjectDecoder
	oti     raptorgo.OTI
	context string
	want    []byte
	events  []Event

	verification       uint32
	delivery           uint32
	constructionSeen   bool
	constructionBindOK bool
}

// NewRecorder constructs an unbound recorder. BindDecoder must be called with
// the decoder returned by NewObjectDecoder before packets are added.
func NewRecorder(oti raptorgo.OTI, context string, want []byte) *Recorder {
	return &Recorder{
		oti: oti, context: context, want: append([]byte(nil), want...),
		constructionBindOK: true,
	}
}

// Verifier returns a binding-checking verifier with a fixed injected outcome.
func (recorder *Recorder) Verifier(outcome error) raptorgo.ObjectVerifier {
	return recorder.VerifierFunc(func(raptorgo.VerificationRequest) error { return outcome })
}

// VerifierFunc is Verifier with a programmable post-observation callback. It
// exists for reentrant and model-based drivers; nil means success.
func (recorder *Recorder) VerifierFunc(delegate raptorgo.ObjectVerifier) raptorgo.ObjectVerifier {
	return func(request raptorgo.VerificationRequest) error {
		candidate, readErr := io.ReadAll(request.Object)
		bindingOK := readErr == nil && request.OTI == recorder.oti &&
			request.Context == recorder.context && request.Length == uint64(len(recorder.want)) &&
			bytes.Equal(candidate, recorder.want)

		recorder.mu.Lock()
		recorder.verification++
		decoder := recorder.decoder
		if decoder == nil {
			recorder.constructionSeen = true
			recorder.constructionBindOK = recorder.constructionBindOK && bindingOK
		}
		recorder.mu.Unlock()
		if decoder != nil {
			recorder.observeDecoder("verify_enter", "ok", bindingOK, decoder, packetAttribution{})
		}
		if !bindingOK {
			return errVerificationBinding
		}
		if delegate != nil {
			return delegate(request)
		}
		return nil
	}
}

// Deliverer returns a binding-checking deliverer with a fixed injected outcome.
func (recorder *Recorder) Deliverer(outcome error) raptorgo.ObjectDeliverer {
	return recorder.DelivererFunc(func(raptorgo.VerifiedObject) error { return outcome })
}

// DelivererFunc is Deliverer with a programmable post-observation callback.
// It records the verified boundary before the delivery invocation boundary.
func (recorder *Recorder) DelivererFunc(delegate raptorgo.ObjectDeliverer) raptorgo.ObjectDeliverer {
	return func(object raptorgo.VerifiedObject) error {
		bindingOK := object.OTI == recorder.oti && object.Context == recorder.context &&
			bytes.Equal(object.Bytes, recorder.want)

		recorder.mu.Lock()
		decoder := recorder.decoder
		if decoder == nil {
			recorder.constructionSeen = true
			recorder.constructionBindOK = recorder.constructionBindOK && bindingOK
			recorder.delivery++
			recorder.mu.Unlock()
		} else {
			recorder.mu.Unlock()
			recorder.observeDecoder("verified", "ok", bindingOK, decoder, packetAttribution{})
			recorder.mu.Lock()
			recorder.delivery++
			recorder.mu.Unlock()
			recorder.observeDecoder("deliver_enter", "ok", bindingOK, decoder, packetAttribution{})
		}
		if !bindingOK {
			return errDeliveryBinding
		}
		if delegate != nil {
			return delegate(object)
		}
		return nil
	}
}

// BindDecoder publishes the decoder to callbacks and emits init, or the
// summarized construct_final event for the zero-length synchronous fast path.
func (recorder *Recorder) BindDecoder(decoder *raptorgo.ObjectDecoder) {
	if decoder == nil {
		panic("objtrace: cannot bind nil decoder")
	}
	recorder.mu.Lock()
	if recorder.decoder != nil || len(recorder.events) != 0 {
		recorder.mu.Unlock()
		panic("objtrace: recorder already bound")
	}
	recorder.decoder = decoder
	verification := recorder.verification
	delivery := recorder.delivery
	bindingOK := recorder.constructionBindOK
	constructionSeen := recorder.constructionSeen
	recorder.mu.Unlock()

	state := decoder.State()
	completed := decoder.CompletedBlocks()
	op, result := "init", "ok"
	if state != raptorgo.ObjectReceiving {
		op = "construct_final"
		switch state {
		case raptorgo.ObjectDelivered:
			result = "ok"
		case raptorgo.ObjectVerificationFailed:
			result = "verification_error"
		case raptorgo.ObjectDeliveryFailed:
			result = "delivery_error"
		default:
			panic(fmt.Sprintf("objtrace: unsupported constructor state %s", state))
		}
		if !constructionSeen {
			panic("objtrace: terminal constructor ran no callbacks")
		}
	}
	blocks := int32(recorder.oti.SourceBlocks)
	recorder.appendEvent(Event{
		V: Version, Op: op, Result: result, State: state.String(), Completed: completed,
		Verification: verification, Delivery: delivery, BindingOK: bindingOK, Blocks: &blocks,
	})
}

// AddPacket delegates to the bound decoder and emits exactly one classified
// call-level event. Callback events appear before it, in actual call order.
func (recorder *Recorder) AddPacket(packet []byte) (raptorgo.ObjectDecoderState, error) {
	recorder.mu.Lock()
	decoder := recorder.decoder
	recorder.mu.Unlock()
	if decoder == nil {
		panic("objtrace: AddPacket before BindDecoder")
	}

	before := decoder.CompletedBlocks()
	state, err := decoder.AddPacket(packet)
	after := decoder.CompletedBlocks()
	if after < before || after-before > 1 {
		panic(fmt.Sprintf("objtrace: completed-block delta %d -> %d is outside 0..1", before, after))
	}

	op, result := classifyAdd(state, err, after > before)
	recorder.observe(op, result, true, state, after, attributionFor(packet, recorder.oti.SymbolSize))
	return state, err
}

// Events returns a defensive copy of all observations in emission order.
func (recorder *Recorder) Events() []Event {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return cloneEvents(recorder.events)
}

// WriteNDJSON writes a stable snapshot of the observations.
func (recorder *Recorder) WriteNDJSON(writer io.Writer) error {
	return WriteNDJSON(writer, recorder.Events())
}

func classifyAdd(state raptorgo.ObjectDecoderState, err error, completed bool) (string, string) {
	switch {
	case errors.Is(err, raptorgo.ErrObjectTerminal):
		return "terminal_add", "terminal"
	case err == nil && state == raptorgo.ObjectDelivered:
		return "add_return", "ok"
	case errors.Is(err, raptorgo.ErrObjectVerification):
		return "add_return", "verification_error"
	case errors.Is(err, raptorgo.ErrObjectDelivery):
		return "add_return", "delivery_error"
	case state == raptorgo.ObjectDecodeFailed && errors.Is(err, raptorgo.ErrObjectDecoder):
		return "decode_failure", "decode_error"
	case state == raptorgo.ObjectReceiving && errors.Is(err, raptorgo.ErrObjectDecoder):
		return "add_reject_conflict", "conflict"
	case errors.Is(err, raptorgo.ErrInvalidPacket) || errors.Is(err, raptorgo.ErrInvalidPayloadID) || errors.Is(err, raptorgo.ErrInvalidOTI):
		return "add_reject_invalid", "invalid"
	case errors.Is(err, raptorgo.ErrLimitExceeded):
		return "add_reject_limit", "limit"
	case err == nil && completed:
		return "block_complete", "ok"
	case err == nil:
		return "add_wait", "ok"
	default:
		panic(fmt.Sprintf("objtrace: unclassified AddPacket state=%s error=%v", state, err))
	}
}

type packetAttribution struct {
	sbn, esi, symbols *int32
}

func attributionFor(packet []byte, symbolSize uint16) packetAttribution {
	if len(packet) < raptorgo.PayloadIDEncodedSize {
		return packetAttribution{}
	}
	id, err := raptorgo.ParsePayloadID(packet[:raptorgo.PayloadIDEncodedSize])
	if err != nil {
		return packetAttribution{}
	}
	sbn := int32(id.SourceBlockNumber)
	esi := int32(id.EncodingSymbolID)
	result := packetAttribution{sbn: &sbn, esi: &esi}
	payload := len(packet) - raptorgo.PayloadIDEncodedSize
	if payload > 0 && symbolSize > 0 {
		count := (uint64(payload) + uint64(symbolSize) - 1) / uint64(symbolSize)
		if count <= uint64(^uint32(0)>>1) {
			symbols := int32(count)
			result.symbols = &symbols
		}
	}
	return result
}

func (recorder *Recorder) observeDecoder(op, result string, bindingOK bool, decoder *raptorgo.ObjectDecoder, attribution packetAttribution) {
	recorder.observe(op, result, bindingOK, decoder.State(), decoder.CompletedBlocks(), attribution)
}

func (recorder *Recorder) observe(
	op, result string,
	bindingOK bool,
	state raptorgo.ObjectDecoderState,
	completed uint32,
	attribution packetAttribution,
) {
	recorder.mu.Lock()
	event := Event{
		V: Version, Op: op, Result: result, State: state.String(), Completed: completed,
		Verification: recorder.verification, Delivery: recorder.delivery, BindingOK: bindingOK,
		SBN: attribution.sbn, ESI: attribution.esi, Symbols: attribution.symbols,
	}
	recorder.events = append(recorder.events, event)
	recorder.mu.Unlock()
}

func (recorder *Recorder) appendEvent(event Event) {
	recorder.mu.Lock()
	recorder.events = append(recorder.events, event)
	recorder.mu.Unlock()
}

func cloneEvents(events []Event) []Event {
	result := make([]Event, len(events))
	for index, event := range events {
		result[index] = event
		result[index].Blocks = cloneInt32(event.Blocks)
		result[index].SBN = cloneInt32(event.SBN)
		result[index].ESI = cloneInt32(event.ESI)
		result[index].Symbols = cloneInt32(event.Symbols)
	}
	return result
}

func cloneInt32(value *int32) *int32 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
