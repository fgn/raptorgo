package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/fgn/raptorgo"
	"github.com/fgn/raptorgo/dev/internal/objtrace"
	"github.com/fgn/raptorgo/internal/codec"
)

func main() {
	output := flag.String("output", "", "output TLA+ data module")
	scenario := flag.String("scenario", "deliver", "deliver, verify-fail, delivery-fail, or decode-fail")
	flag.Parse()
	if *output == "" {
		flag.Usage()
		os.Exit(2)
	}
	module := map[string]string{
		"deliver":       "ObjectDecoderTraceData",
		"verify-fail":   "ObjectDecoderVerifyFailureTraceData",
		"delivery-fail": "ObjectDecoderDeliveryFailureTraceData",
		"decode-fail":   "ObjectDecoderDecodeFailureTraceData",
	}[*scenario]
	if module == "" {
		panic(fmt.Sprintf("unknown scenario %q", *scenario))
	}
	blocks, events := execute(*scenario)
	rendered, err := objtrace.RenderTLA(module, blocks, events)
	must(err)
	if err := os.WriteFile(*output, rendered, 0o644); err != nil {
		panic(err)
	}
}

func execute(scenario string) (int32, []objtrace.Event) {
	if scenario == "decode-fail" {
		return executeDecodeFailure()
	}
	return executeFinalization(scenario)
}

func executeFinalization(scenario string) (int32, []objtrace.Event) {
	oti := raptorgo.OTI{
		TransferLength: 13, SymbolSize: 4, SourceBlocks: 2, SubBlocks: 2, SymbolAlignment: 1,
	}
	want := []byte{3, 19, 41, 67, 101, 149, 211, 17, 53, 109, 181, 13, 97}
	encoder, err := raptorgo.NewObjectEncoder(want, oti, raptorgo.DefaultLimits())
	must(err)
	var packets [][]byte
	for sourceBlockIndex := 0; sourceBlockIndex < int(oti.SourceBlocks); sourceBlockIndex++ {
		sourceBlock := uint8(sourceBlockIndex)
		k, symbolsErr := raptorgo.SourceBlockSymbols(oti, sourceBlock)
		must(symbolsErr)
		if k != 2 {
			panic(fmt.Sprintf("trace SBN %d has K=%d", sourceBlock, k))
		}
		for esi := uint32(0); esi < k; esi++ {
			packet, packetErr := encoder.Packet(sourceBlock, esi, 1, true)
			must(packetErr)
			packets = append(packets, packet)
		}
	}

	var verificationError, deliveryError error
	switch scenario {
	case "deliver":
	case "verify-fail":
		verificationError = errors.New("injected verification failure")
	case "delivery-fail":
		deliveryError = errors.New("injected delivery failure")
	default:
		panic(fmt.Sprintf("unknown finalization scenario %q", scenario))
	}
	record := objtrace.NewRecorder(oti, "generation-6330", want)
	decoder, err := raptorgo.NewObjectDecoder(
		oti,
		raptorgo.DefaultLimits(),
		"generation-6330",
		record.Verifier(verificationError),
		record.Deliverer(deliveryError),
	)
	must(err)
	record.BindDecoder(decoder)

	for index, packet := range packets[:len(packets)-1] {
		state, addErr := record.AddPacket(packet)
		if addErr != nil || state != raptorgo.ObjectReceiving {
			panic(fmt.Sprintf("pre-final add %d state=%s error=%v", index, state, addErr))
		}
	}

	state, err := record.AddPacket(packets[len(packets)-1])
	wantState, wantError := raptorgo.ObjectDelivered, error(nil)
	switch scenario {
	case "verify-fail":
		wantState, wantError = raptorgo.ObjectVerificationFailed, raptorgo.ErrObjectVerification
	case "delivery-fail":
		wantState, wantError = raptorgo.ObjectDeliveryFailed, raptorgo.ErrObjectDelivery
	}
	if state != wantState || (wantError == nil && err != nil) || (wantError != nil && !errors.Is(err, wantError)) {
		panic(fmt.Sprintf("final add state=%s error=%v", state, err))
	}

	state, err = record.AddPacket(packets[0])
	if state != wantState || !errors.Is(err, raptorgo.ErrObjectTerminal) {
		panic(fmt.Sprintf("terminal add state=%s error=%v", state, err))
	}
	return traceBlocks(record), record.Events()
}

func executeDecodeFailure() (int32, []objtrace.Event) {
	oti := raptorgo.OTI{
		TransferLength: 10, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1,
	}
	want := []byte{3, 19, 41, 67, 101, 149, 211, 17, 53, 109}
	encoder, err := raptorgo.NewObjectEncoder(want, oti, raptorgo.DefaultLimits())
	must(err)

	limits := raptorgo.DefaultLimits()
	limits.MaxRetainedSymbols = 11
	limits.MaxRepairSymbolsPerBlock = 1
	limits.MaxDecodeAttempts = 1
	record := objtrace.NewRecorder(oti, "decode-failure-6330", want)
	decoder, err := raptorgo.NewObjectDecoder(
		oti,
		limits,
		"decode-failure-6330",
		record.VerifierFunc(func(raptorgo.VerificationRequest) error {
			panic("rank-deficient trace unexpectedly invoked verification")
		}),
		record.DelivererFunc(func(raptorgo.VerifiedObject) error {
			panic("rank-deficient trace unexpectedly invoked delivery")
		}),
	)
	must(err)
	record.BindDecoder(decoder)

	var firstPacket []byte
	for esi := uint32(319); esi <= 328; esi++ {
		packet, packetErr := encoder.Packet(0, esi, 1, false)
		must(packetErr)
		if firstPacket == nil {
			firstPacket = append([]byte(nil), packet...)
		}
		state, addErr := record.AddPacket(packet)
		if addErr != nil || state != raptorgo.ObjectReceiving {
			panic(fmt.Sprintf("deficient ESI %d state=%s error=%v", esi, state, addErr))
		}
	}

	packet, err := encoder.Packet(0, 329, 1, false)
	must(err)
	state, err := record.AddPacket(packet)
	if state != raptorgo.ObjectDecodeFailed || !errors.Is(err, raptorgo.ErrObjectDecoder) ||
		!errors.Is(err, codec.ErrDecoderAttemptLimit) {
		panic(fmt.Sprintf("decode failure state=%s error=%v", state, err))
	}

	state, err = record.AddPacket(firstPacket)
	if state != raptorgo.ObjectDecodeFailed || !errors.Is(err, raptorgo.ErrObjectTerminal) {
		panic(fmt.Sprintf("decode-failed terminal add state=%s error=%v", state, err))
	}
	return traceBlocks(record), record.Events()
}

func traceBlocks(record *objtrace.Recorder) int32 {
	events := record.Events()
	if len(events) == 0 || events[0].Blocks == nil {
		panic("object trace has no initial blocks field")
	}
	return *events[0].Blocks
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
