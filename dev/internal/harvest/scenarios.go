package harvest

import (
	"errors"
	"fmt"

	"github.com/fgn/raptorgo"
	"github.com/fgn/raptorgo/dev/internal/objtrace"
	"github.com/fgn/raptorgo/dev/internal/tracecorpus"
)

var scenarios = []scenario{
	{
		name: "multi_block_deliver", required: true,
		features: []tracecorpus.Feature{FeatureMultiBlock, FeatureSuccess, FeatureShortened},
		run:      runMultiBlockDeliver,
	},
	{
		name: "multi_block_reverse_duplicate", required: true,
		features: []tracecorpus.Feature{FeatureMultiBlock, FeatureDuplicate},
		run:      runMultiBlockReverseDuplicate,
	},
	{
		name: "three_block_staggered", required: true,
		features: []tracecorpus.Feature{FeatureMultiBlock},
		run:      runThreeBlockStaggered,
	},
	{
		name: "trailing_empty_z4", required: true,
		features: []tracecorpus.Feature{FeatureTrailingEmpty},
		run:      func() ([]objtrace.Event, error) { return runTrailingEmpty(4, 0) },
	},
	{
		name: "trailing_empty_z255", required: true,
		features: []tracecorpus.Feature{FeatureTrailingEmpty, FeatureTrailingEmptyZ255, FeatureInvalidReject},
		run:      func() ([]objtrace.Event, error) { return runTrailingEmpty(255, 2) },
	},
	{
		name: "single_block_success", required: true,
		features: []tracecorpus.Feature{FeatureSuccess, FeatureContiguousLayout},
		run:      runSingleBlockSuccess,
	},
	{
		name: "verification_failure", required: true,
		features: []tracecorpus.Feature{FeatureVerifyFailure, FeatureGrouped},
		run:      func() ([]objtrace.Event, error) { return runCallbackFailure(true) },
	},
	{
		name: "delivery_failure", required: true,
		features: []tracecorpus.Feature{FeatureDeliveryFailure, FeatureGrouped},
		run:      func() ([]objtrace.Event, error) { return runCallbackFailure(false) },
	},
	{
		name: "decode_failure", required: true,
		features: []tracecorpus.Feature{FeatureDecodeFailure, FeatureLoss, FeatureTerminalReject},
		run:      runDecodeFailure,
	},
	{
		name: "conflict_then_deliver", required: true,
		features: []tracecorpus.Feature{FeatureConflictReject},
		run:      runConflictThenDeliver,
	},
	{
		name: "invalid_then_deliver", required: true,
		features: []tracecorpus.Feature{FeatureInvalidReject},
		run:      runInvalidThenDeliver,
	},
	{
		name: "limit_then_deliver", required: true,
		features: []tracecorpus.Feature{FeatureLimitReject, FeatureGrouped},
		run:      runLimitThenDeliver,
	},
	{
		name: "terminal_reject_after_delivery", required: true,
		features: []tracecorpus.Feature{FeatureTerminalReject},
		run:      runTerminalReject,
	},
	{
		name: "construct_success", required: true,
		features: []tracecorpus.Feature{FeatureConstructSuccess},
		run:      func() ([]objtrace.Event, error) { return runConstruction(nil, nil) },
	},
	{
		name: "construct_verification_failure", required: true,
		features: []tracecorpus.Feature{FeatureConstructVerifyFail},
		run:      func() ([]objtrace.Event, error) { return runConstruction(errInjectedVerification, nil) },
	},
	{
		name: "construct_delivery_failure", required: true,
		features: []tracecorpus.Feature{FeatureConstructDeliverFail},
		run:      func() ([]objtrace.Event, error) { return runConstruction(nil, errInjectedDelivery) },
	},
	{
		name: "grouped_multi_block_delivery", required: true,
		features: []tracecorpus.Feature{FeatureGrouped, FeatureMultiBlock},
		run:      runGroupedMultiBlock,
	},
	{
		name: "verifier_reentrant_reject", required: true,
		features: []tracecorpus.Feature{FeatureVerifierReentrant, FeatureTerminalReject},
		run:      func() ([]objtrace.Event, error) { return runReentrant(true) },
	},
	{
		name: "deliverer_reentrant_reject", required: true,
		features: []tracecorpus.Feature{FeatureDelivererReentrant, FeatureTerminalReject},
		run:      func() ([]objtrace.Event, error) { return runReentrant(false) },
	},
	{
		name: "idempotent_duplicate", required: true,
		features: []tracecorpus.Feature{FeatureDuplicate},
		run:      runIdempotentDuplicate,
	},
	{
		name: "source_loss_repair", required: true,
		features: []tracecorpus.Feature{FeatureLoss},
		run:      runSourceLossRepair,
	},
	{
		name: "contiguous_layout_rejections", required: true,
		features: []tracecorpus.Feature{FeatureContiguousLayout, FeatureMultiBlock, FeatureGrouped, FeatureInvalidReject},
		run:      runContiguousLayout,
	},
	{
		name: "subblocked_layout_duplicate", required: true,
		features: []tracecorpus.Feature{FeatureSubBlockedLayout, FeatureMultiBlock, FeatureGrouped, FeatureDuplicate},
		run:      runSubBlockedLayout,
	},
	{
		name: "shortened_tail_with_reject", required: true,
		features: []tracecorpus.Feature{FeatureSubBlockedLayout, FeatureShortened, FeatureInvalidReject},
		run:      runShortenedTail,
	},
	{
		name:     "inline_fuzz_raptorq",
		features: []tracecorpus.Feature{FeatureInlineFuzzSeed, FeatureGrouped, FeatureTrailingEmpty},
		run:      func() ([]objtrace.Event, error) { return runInlineFuzzSeed([]byte("raptorq"), 4, 2, 2) },
	},
	{
		name:     "inline_fuzz_one",
		features: []tracecorpus.Feature{FeatureInlineFuzzSeed, FeatureSubBlockedLayout},
		run:      func() ([]objtrace.Event, error) { return runInlineFuzzSeed([]byte{1}, 1, 4, 1) },
	},
	{
		name:     "inline_fuzz_empty",
		features: []tracecorpus.Feature{FeatureInlineFuzzSeed, FeatureConstructSuccess},
		run:      func() ([]objtrace.Event, error) { return runInlineFuzzSeed(nil, 8, 1, 8) },
	},
}

var (
	errInjectedVerification = errors.New("harvest: injected verification failure")
	errInjectedDelivery     = errors.New("harvest: injected delivery failure")
)

type session struct {
	recorder *objtrace.Recorder
	decoder  *raptorgo.ObjectDecoder
	encoder  *raptorgo.ObjectEncoder
	oti      raptorgo.OTI
}

type (
	verifierFactory  func(*objtrace.Recorder) raptorgo.ObjectVerifier
	delivererFactory func(*objtrace.Recorder) raptorgo.ObjectDeliverer
)

func newSession(
	name string,
	oti raptorgo.OTI,
	object []byte,
	limits raptorgo.Limits,
	verifier verifierFactory,
	deliverer delivererFactory,
) (*session, error) {
	encoder, err := raptorgo.NewObjectEncoder(object, oti, raptorgo.DefaultLimits())
	if err != nil {
		return nil, fmt.Errorf("construct encoder: %w", err)
	}
	recorder := objtrace.NewRecorder(oti, "traceharvest-"+name, object)
	verify := recorder.Verifier(nil)
	if verifier != nil {
		verify = verifier(recorder)
	}
	deliver := recorder.Deliverer(nil)
	if deliverer != nil {
		deliver = deliverer(recorder)
	}
	decoder, constructorErr := raptorgo.NewObjectDecoder(
		oti, limits, "traceharvest-"+name, verify, deliver,
	)
	if decoder != nil {
		recorder.BindDecoder(decoder)
	}
	return &session{recorder: recorder, decoder: decoder, encoder: encoder, oti: oti}, constructorErr
}

func (session *session) packet(sbn uint8, esi, count uint32, shorten bool) ([]byte, error) {
	return session.encoder.Packet(sbn, esi, count, shorten)
}

func (session *session) addOK(packet []byte) error {
	state, err := session.recorder.AddPacket(packet)
	if err != nil {
		return err
	}
	if state != raptorgo.ObjectReceiving && state != raptorgo.ObjectDelivered {
		return fmt.Errorf("successful add returned state %s", state)
	}
	return nil
}

func (session *session) addError(packet []byte, sentinel error, state raptorgo.ObjectDecoderState) error {
	gotState, err := session.recorder.AddPacket(packet)
	if !errors.Is(err, sentinel) || gotState != state {
		return fmt.Errorf("add state=%s error=%v, want state=%s error=%v", gotState, err, state, sentinel)
	}
	return nil
}

func standardFixture(name string, verifier verifierFactory, deliverer delivererFactory) (*session, [][]byte, error) {
	oti := raptorgo.OTI{
		TransferLength: 13, SymbolSize: 4, SourceBlocks: 2, SubBlocks: 2, SymbolAlignment: 1,
	}
	object := []byte{3, 19, 41, 67, 101, 149, 211, 17, 53, 109, 181, 13, 97}
	session, err := newSession(name, oti, object, raptorgo.DefaultLimits(), verifier, deliverer)
	if err != nil {
		return nil, nil, err
	}
	packets := make([][]byte, 0, 4)
	for sbn := uint8(0); sbn < oti.SourceBlocks; sbn++ {
		k, symbolsErr := raptorgo.SourceBlockSymbols(oti, sbn)
		if symbolsErr != nil {
			return nil, nil, symbolsErr
		}
		for esi := uint32(0); esi < k; esi++ {
			packet, packetErr := session.packet(sbn, esi, 1, true)
			if packetErr != nil {
				return nil, nil, packetErr
			}
			packets = append(packets, packet)
		}
	}
	return session, packets, nil
}

func runMultiBlockDeliver() ([]objtrace.Event, error) {
	session, packets, err := standardFixture("multi-block", nil, nil)
	if err != nil {
		return nil, err
	}
	for _, packet := range packets {
		if err := session.addOK(packet); err != nil {
			return nil, err
		}
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func runMultiBlockReverseDuplicate() ([]objtrace.Event, error) {
	session, packets, err := standardFixture("multi-block-reverse", nil, nil)
	if err != nil {
		return nil, err
	}
	for _, index := range []int{2, 2, 3, 0, 1} {
		if err := session.addOK(packets[index]); err != nil {
			return nil, err
		}
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func runThreeBlockStaggered() ([]objtrace.Event, error) {
	oti := raptorgo.OTI{TransferLength: 6, SymbolSize: 1, SourceBlocks: 3, SubBlocks: 1, SymbolAlignment: 1}
	session, err := newSession("three-block", oti, patternedObject(oti.TransferLength), raptorgo.DefaultLimits(), nil, nil)
	if err != nil {
		return nil, err
	}
	packets := make([][]byte, 0, 6)
	for esi := uint32(0); esi < 2; esi++ {
		for sbn := uint8(0); sbn < oti.SourceBlocks; sbn++ {
			packet, packetErr := session.packet(sbn, esi, 1, false)
			if packetErr != nil {
				return nil, packetErr
			}
			packets = append(packets, packet)
		}
	}
	for _, packet := range packets {
		if err := session.addOK(packet); err != nil {
			return nil, err
		}
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func runTrailingEmpty(sourceBlocks uint8, invalidAdds int) ([]objtrace.Event, error) {
	oti := raptorgo.OTI{TransferLength: 1, SymbolSize: 1, SourceBlocks: sourceBlocks, SubBlocks: 1, SymbolAlignment: 1}
	session, err := newSession("trailing-empty", oti, []byte{0x7b}, raptorgo.DefaultLimits(), nil, nil)
	if err != nil {
		return nil, err
	}
	emptyTarget, err := rawPacket(1, 0, []byte{0})
	if err != nil {
		return nil, err
	}
	for range invalidAdds {
		if err := session.addError(emptyTarget, raptorgo.ErrInvalidPacket, raptorgo.ObjectReceiving); err != nil {
			return nil, err
		}
	}
	packet, err := session.packet(0, 0, 1, false)
	if err != nil {
		return nil, err
	}
	if err := session.addOK(packet); err != nil {
		return nil, err
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func runSingleBlockSuccess() ([]objtrace.Event, error) {
	oti := raptorgo.OTI{TransferLength: 3, SymbolSize: 2, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	session, err := newSession("single-success", oti, patternedObject(oti.TransferLength), raptorgo.DefaultLimits(), nil, nil)
	if err != nil {
		return nil, err
	}
	for esi := uint32(0); esi < 2; esi++ {
		packet, packetErr := session.packet(0, esi, 1, true)
		if packetErr != nil {
			return nil, packetErr
		}
		if err := session.addOK(packet); err != nil {
			return nil, err
		}
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func runCallbackFailure(verification bool) ([]objtrace.Event, error) {
	oti := raptorgo.OTI{TransferLength: 3, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	var verifier verifierFactory
	var deliverer delivererFactory
	if verification {
		verifier = func(recorder *objtrace.Recorder) raptorgo.ObjectVerifier {
			return recorder.Verifier(errInjectedVerification)
		}
	} else {
		deliverer = func(recorder *objtrace.Recorder) raptorgo.ObjectDeliverer {
			return recorder.Deliverer(errInjectedDelivery)
		}
	}
	session, err := newSession("callback-failure", oti, patternedObject(oti.TransferLength), raptorgo.DefaultLimits(), verifier, deliverer)
	if err != nil {
		return nil, err
	}
	packet, err := session.packet(0, 0, 3, false)
	if err != nil {
		return nil, err
	}
	if verification {
		if err := session.addError(packet, raptorgo.ErrObjectVerification, raptorgo.ObjectVerificationFailed); err != nil {
			return nil, err
		}
	} else if err := session.addError(packet, raptorgo.ErrObjectDelivery, raptorgo.ObjectDeliveryFailed); err != nil {
		return nil, err
	}
	return session.recorder.Events(), nil
}

func runDecodeFailure() ([]objtrace.Event, error) {
	oti := raptorgo.OTI{TransferLength: 10, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	limits := raptorgo.DefaultLimits()
	limits.MaxRetainedSymbols = 11
	limits.MaxRepairSymbolsPerBlock = 1
	limits.MaxDecodeAttempts = 1
	callbackCalled := false
	verifier := func(recorder *objtrace.Recorder) raptorgo.ObjectVerifier {
		return recorder.VerifierFunc(func(raptorgo.VerificationRequest) error {
			callbackCalled = true
			return errors.New("unexpected verification")
		})
	}
	deliverer := func(recorder *objtrace.Recorder) raptorgo.ObjectDeliverer {
		return recorder.DelivererFunc(func(raptorgo.VerifiedObject) error {
			callbackCalled = true
			return errors.New("unexpected delivery")
		})
	}
	session, err := newSession("decode-failure", oti, patternedObject(oti.TransferLength), limits, verifier, deliverer)
	if err != nil {
		return nil, err
	}
	var first []byte
	for esi := uint32(319); esi <= 328; esi++ {
		packet, packetErr := session.packet(0, esi, 1, false)
		if packetErr != nil {
			return nil, packetErr
		}
		if first == nil {
			first = append([]byte(nil), packet...)
		}
		if err := session.addOK(packet); err != nil {
			return nil, err
		}
	}
	packet, err := session.packet(0, 329, 1, false)
	if err != nil {
		return nil, err
	}
	if err := session.addError(packet, raptorgo.ErrObjectDecoder, raptorgo.ObjectDecodeFailed); err != nil {
		return nil, err
	}
	if err := session.addError(first, raptorgo.ErrObjectTerminal, raptorgo.ObjectDecodeFailed); err != nil {
		return nil, err
	}
	if callbackCalled {
		return nil, errors.New("decode failure invoked a callback")
	}
	return session.recorder.Events(), nil
}

func runConflictThenDeliver() ([]objtrace.Event, error) {
	session, packets, err := singleByteSession("conflict", 2, raptorgo.DefaultLimits())
	if err != nil {
		return nil, err
	}
	if err := session.addOK(packets[0]); err != nil {
		return nil, err
	}
	conflict := append([]byte(nil), packets[0]...)
	conflict[len(conflict)-1] ^= 0xff
	if err := session.addError(conflict, raptorgo.ErrObjectDecoder, raptorgo.ObjectReceiving); err != nil {
		return nil, err
	}
	if err := session.addOK(packets[1]); err != nil {
		return nil, err
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func runInvalidThenDeliver() ([]objtrace.Event, error) {
	session, packets, err := singleByteSession("invalid", 2, raptorgo.DefaultLimits())
	if err != nil {
		return nil, err
	}
	if err := session.addError([]byte{0}, raptorgo.ErrInvalidPacket, raptorgo.ObjectReceiving); err != nil {
		return nil, err
	}
	for _, packet := range packets {
		if err := session.addOK(packet); err != nil {
			return nil, err
		}
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func runLimitThenDeliver() ([]objtrace.Event, error) {
	limits := raptorgo.DefaultLimits()
	limits.MaxPacketBytes = raptorgo.PayloadIDEncodedSize + 1
	session, packets, err := singleByteSession("limit", 2, limits)
	if err != nil {
		return nil, err
	}
	grouped, err := session.packet(0, 0, 2, false)
	if err != nil {
		return nil, err
	}
	if err := session.addError(grouped, raptorgo.ErrLimitExceeded, raptorgo.ObjectReceiving); err != nil {
		return nil, err
	}
	for _, packet := range packets {
		if err := session.addOK(packet); err != nil {
			return nil, err
		}
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func runTerminalReject() ([]objtrace.Event, error) {
	session, packets, err := singleByteSession("terminal", 2, raptorgo.DefaultLimits())
	if err != nil {
		return nil, err
	}
	for _, packet := range packets {
		if err := session.addOK(packet); err != nil {
			return nil, err
		}
	}
	if err := session.addError(packets[0], raptorgo.ErrObjectTerminal, raptorgo.ObjectDelivered); err != nil {
		return nil, err
	}
	return session.recorder.Events(), nil
}

func runConstruction(verificationErr, deliveryErr error) ([]objtrace.Event, error) {
	oti := raptorgo.OTI{TransferLength: 0, SymbolSize: 4, SourceBlocks: 3, SubBlocks: 1, SymbolAlignment: 1}
	verifier := func(recorder *objtrace.Recorder) raptorgo.ObjectVerifier { return recorder.Verifier(verificationErr) }
	deliverer := func(recorder *objtrace.Recorder) raptorgo.ObjectDeliverer { return recorder.Deliverer(deliveryErr) }
	session, err := newSession("construct", oti, nil, raptorgo.DefaultLimits(), verifier, deliverer)
	want := error(nil)
	if verificationErr != nil {
		want = raptorgo.ErrObjectVerification
	} else if deliveryErr != nil {
		want = raptorgo.ErrObjectDelivery
	}
	if (want == nil && err != nil) || (want != nil && !errors.Is(err, want)) {
		return nil, fmt.Errorf("constructor error=%v, want %v", err, want)
	}
	if session.decoder == nil {
		return nil, errors.New("zero-length constructor returned nil decoder")
	}
	return session.recorder.Events(), nil
}

func runGroupedMultiBlock() ([]objtrace.Event, error) {
	session, _, err := standardFixture("grouped-multi", nil, nil)
	if err != nil {
		return nil, err
	}
	for sbn := uint8(0); sbn < session.oti.SourceBlocks; sbn++ {
		k, symbolsErr := raptorgo.SourceBlockSymbols(session.oti, sbn)
		if symbolsErr != nil {
			return nil, symbolsErr
		}
		packet, packetErr := session.packet(sbn, 0, k, true)
		if packetErr != nil {
			return nil, packetErr
		}
		if err := session.addOK(packet); err != nil {
			return nil, err
		}
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func runReentrant(verifierReentry bool) ([]objtrace.Event, error) {
	oti := raptorgo.OTI{TransferLength: 1, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	object := []byte{0x51}
	encoder, err := raptorgo.NewObjectEncoder(object, oti, raptorgo.DefaultLimits())
	if err != nil {
		return nil, err
	}
	packet, err := encoder.Packet(0, 0, 1, false)
	if err != nil {
		return nil, err
	}
	var callbackErr error
	var verifier verifierFactory
	var deliverer delivererFactory
	if verifierReentry {
		verifier = func(recorder *objtrace.Recorder) raptorgo.ObjectVerifier {
			return recorder.VerifierFunc(func(raptorgo.VerificationRequest) error {
				state, addErr := recorder.AddPacket(packet)
				if state != raptorgo.ObjectDecoded || !errors.Is(addErr, raptorgo.ErrObjectTerminal) {
					callbackErr = fmt.Errorf("verifier reentry state=%s error=%v", state, addErr)
				}
				return nil
			})
		}
	} else {
		deliverer = func(recorder *objtrace.Recorder) raptorgo.ObjectDeliverer {
			return recorder.DelivererFunc(func(raptorgo.VerifiedObject) error {
				state, addErr := recorder.AddPacket(packet)
				if state != raptorgo.ObjectVerified || !errors.Is(addErr, raptorgo.ErrObjectTerminal) {
					callbackErr = fmt.Errorf("deliverer reentry state=%s error=%v", state, addErr)
				}
				return nil
			})
		}
	}
	session, err := newSession("reentrant", oti, object, raptorgo.DefaultLimits(), verifier, deliverer)
	if err != nil {
		return nil, err
	}
	if err := session.addOK(packet); err != nil {
		return nil, err
	}
	if callbackErr != nil {
		return nil, callbackErr
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func runIdempotentDuplicate() ([]objtrace.Event, error) {
	session, packets, err := singleByteSession("duplicate", 3, raptorgo.DefaultLimits())
	if err != nil {
		return nil, err
	}
	for _, index := range []int{0, 0, 1, 2} {
		if err := session.addOK(packets[index]); err != nil {
			return nil, err
		}
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func runSourceLossRepair() ([]objtrace.Event, error) {
	oti := raptorgo.OTI{TransferLength: 10, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	session, err := newSession("source-loss", oti, patternedObject(oti.TransferLength), raptorgo.DefaultLimits(), nil, nil)
	if err != nil {
		return nil, err
	}
	for esi := uint32(0); esi < 9; esi++ {
		packet, packetErr := session.packet(0, esi, 1, false)
		if packetErr != nil {
			return nil, packetErr
		}
		if err := session.addOK(packet); err != nil {
			return nil, err
		}
	}
	for repair := uint32(0); repair < 32 && session.decoder.State() == raptorgo.ObjectReceiving; repair++ {
		packet, packetErr := session.packet(0, 10+repair, 1, false)
		if packetErr != nil {
			return nil, packetErr
		}
		if err := session.addOK(packet); err != nil {
			return nil, err
		}
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func runContiguousLayout() ([]objtrace.Event, error) {
	oti := raptorgo.OTI{TransferLength: 29, SymbolSize: 6, SourceBlocks: 2, SubBlocks: 1, SymbolAlignment: 1}
	session, err := newSession("contiguous-layout", oti, patternedObject(oti.TransferLength), raptorgo.DefaultLimits(), nil, nil)
	if err != nil {
		return nil, err
	}
	for range 2 {
		if err := session.addError([]byte{0, 1}, raptorgo.ErrInvalidPacket, raptorgo.ObjectReceiving); err != nil {
			return nil, err
		}
	}
	for sbn := uint8(0); sbn < oti.SourceBlocks; sbn++ {
		k, symbolsErr := raptorgo.SourceBlockSymbols(oti, sbn)
		if symbolsErr != nil {
			return nil, symbolsErr
		}
		packet, packetErr := session.packet(sbn, 0, k, true)
		if packetErr != nil {
			return nil, packetErr
		}
		if err := session.addOK(packet); err != nil {
			return nil, err
		}
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func runSubBlockedLayout() ([]objtrace.Event, error) {
	oti := raptorgo.OTI{TransferLength: 29, SymbolSize: 6, SourceBlocks: 2, SubBlocks: 4, SymbolAlignment: 1}
	session, err := newSession("subblocked-layout", oti, patternedObject(oti.TransferLength), raptorgo.DefaultLimits(), nil, nil)
	if err != nil {
		return nil, err
	}
	first, err := session.packet(0, 0, 1, true)
	if err != nil {
		return nil, err
	}
	if err := session.addOK(first); err != nil {
		return nil, err
	}
	if err := session.addOK(first); err != nil {
		return nil, err
	}
	k0, err := raptorgo.SourceBlockSymbols(oti, 0)
	if err != nil {
		return nil, err
	}
	remainder, err := session.packet(0, 1, k0-1, true)
	if err != nil {
		return nil, err
	}
	if err := session.addOK(remainder); err != nil {
		return nil, err
	}
	k1, err := raptorgo.SourceBlockSymbols(oti, 1)
	if err != nil {
		return nil, err
	}
	final, err := session.packet(1, 0, k1, true)
	if err != nil {
		return nil, err
	}
	if err := session.addOK(final); err != nil {
		return nil, err
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func runShortenedTail() ([]objtrace.Event, error) {
	oti := raptorgo.OTI{TransferLength: 13, SymbolSize: 5, SourceBlocks: 1, SubBlocks: 3, SymbolAlignment: 1}
	session, err := newSession("shortened-tail", oti, patternedObject(oti.TransferLength), raptorgo.DefaultLimits(), nil, nil)
	if err != nil {
		return nil, err
	}
	for esi := uint32(0); esi < 3; esi++ {
		if esi == 1 {
			if err := session.addError([]byte{0}, raptorgo.ErrInvalidPacket, raptorgo.ObjectReceiving); err != nil {
				return nil, err
			}
		}
		packet, packetErr := session.packet(0, esi, 1, true)
		if packetErr != nil {
			return nil, packetErr
		}
		if err := session.addOK(packet); err != nil {
			return nil, err
		}
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func runInlineFuzzSeed(data []byte, sizeSeed, blocksSeed, subBlocksSeed uint8) ([]objtrace.Event, error) {
	if len(data) > 128 {
		data = data[:128]
	}
	symbolSize := uint16(sizeSeed%16 + 1)
	sourceBlocks := blocksSeed%4 + 1
	subBlocks := uint16(subBlocksSeed)%symbolSize + 1
	oti := raptorgo.OTI{
		TransferLength: uint64(len(data)), SymbolSize: symbolSize,
		SourceBlocks: sourceBlocks, SubBlocks: subBlocks, SymbolAlignment: 1,
	}
	session, err := newSession("inline-fuzz", oti, data, raptorgo.DefaultLimits(), nil, nil)
	if session == nil {
		return nil, err
	}
	if len(data) == 0 {
		if err != nil {
			return nil, err
		}
		return session.recorder.Events(), nil
	}
	if err != nil {
		return nil, err
	}
	for sbn := uint8(0); sbn < sourceBlocks && session.decoder.State() == raptorgo.ObjectReceiving; sbn++ {
		k, symbolsErr := raptorgo.SourceBlockSymbols(oti, sbn)
		if symbolsErr != nil {
			return nil, symbolsErr
		}
		if k == 0 {
			continue
		}
		packet, packetErr := session.packet(sbn, 0, k, true)
		if packetErr != nil {
			return nil, packetErr
		}
		if err := session.addOK(packet); err != nil {
			return nil, err
		}
	}
	return requireState(session, raptorgo.ObjectDelivered)
}

func singleByteSession(name string, symbols uint64, limits raptorgo.Limits) (*session, [][]byte, error) {
	oti := raptorgo.OTI{TransferLength: symbols, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	session, err := newSession(name, oti, patternedObject(symbols), limits, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	packets := make([][]byte, symbols)
	for esi := uint32(0); esi < uint32(symbols); esi++ {
		packets[esi], err = session.packet(0, esi, 1, false)
		if err != nil {
			return nil, nil, err
		}
	}
	return session, packets, nil
}

func rawPacket(sbn uint8, esi uint32, payload []byte) ([]byte, error) {
	id, err := raptorgo.NewPayloadID(sbn, esi)
	if err != nil {
		return nil, err
	}
	header, err := id.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return append(header, payload...), nil
}

func requireState(session *session, state raptorgo.ObjectDecoderState) ([]objtrace.Event, error) {
	if session.decoder == nil || session.decoder.State() != state {
		return nil, fmt.Errorf("decoder state=%v, want %s", session.decoder, state)
	}
	return session.recorder.Events(), nil
}
