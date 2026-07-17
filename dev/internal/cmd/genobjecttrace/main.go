package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/fgn/raptorgo"
	"github.com/fgn/raptorgo/internal/codec"
)

type observation struct {
	operation, result                 string
	state                             raptorgo.ObjectDecoderState
	completed, verification, delivery uint32
	bindingOK                         bool
}

type recorder struct {
	decoder                *raptorgo.ObjectDecoder
	oti                    raptorgo.OTI
	context                string
	want                   []byte
	verification, delivery uint32
	observations           []observation
}

type traceCorpus struct {
	blocks uint32
	trace  []observation
}

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
	corpus := execute(*scenario)
	if err := os.WriteFile(*output, render(module, corpus), 0o644); err != nil {
		panic(err)
	}
}

func execute(scenario string) traceCorpus {
	if scenario == "decode-fail" {
		return executeDecodeFailure()
	}
	return executeFinalization(scenario)
}

func executeFinalization(scenario string) traceCorpus {
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

	record := &recorder{oti: oti, context: "generation-6330", want: want}
	decoder, err := raptorgo.NewObjectDecoder(
		oti,
		raptorgo.DefaultLimits(),
		record.context,
		func(request raptorgo.VerificationRequest) error {
			record.verification++
			candidate, readErr := io.ReadAll(request.Object)
			bindingOK := readErr == nil && request.OTI == oti &&
				request.Context == record.context && request.Length == uint64(len(want)) &&
				bytes.Equal(candidate, want)
			record.observe("verify_enter", "ok", bindingOK)
			if scenario == "verify-fail" {
				return errors.New("injected verification failure")
			}
			if !bindingOK {
				return errors.New("verification binding mismatch")
			}
			return nil
		},
		func(object raptorgo.VerifiedObject) error {
			bindingOK := object.OTI == oti && object.Context == record.context && bytes.Equal(object.Bytes, want)
			record.observe("verified", "ok", bindingOK)
			record.delivery++
			record.observe("deliver_enter", "ok", bindingOK)
			if scenario == "delivery-fail" {
				return errors.New("injected delivery failure")
			}
			if !bindingOK {
				return errors.New("delivery binding mismatch")
			}
			return nil
		},
	)
	must(err)
	record.decoder = decoder
	record.observe("init", "ok", true)

	for index, packet := range packets[:len(packets)-1] {
		state, addErr := decoder.AddPacket(packet)
		if addErr != nil || state != raptorgo.ObjectReceiving {
			panic(fmt.Sprintf("pre-final add %d state=%s error=%v", index, state, addErr))
		}
		operation := "add_wait"
		if index == 1 {
			operation = "block_complete"
		}
		record.observe(operation, "ok", true)
	}

	state, err := decoder.AddPacket(packets[len(packets)-1])
	wantState, wantError, result := raptorgo.ObjectDelivered, error(nil), "ok"
	switch scenario {
	case "verify-fail":
		wantState, wantError, result = raptorgo.ObjectVerificationFailed, raptorgo.ErrObjectVerification, "verification_error"
	case "delivery-fail":
		wantState, wantError, result = raptorgo.ObjectDeliveryFailed, raptorgo.ErrObjectDelivery, "delivery_error"
	}
	if state != wantState || (wantError == nil && err != nil) || (wantError != nil && !errors.Is(err, wantError)) {
		panic(fmt.Sprintf("final add state=%s error=%v", state, err))
	}
	record.observe("add_return", result, true)

	state, err = decoder.AddPacket(packets[0])
	if state != wantState || !errors.Is(err, raptorgo.ErrObjectTerminal) {
		panic(fmt.Sprintf("terminal add state=%s error=%v", state, err))
	}
	record.observe("terminal_add", "terminal", true)
	return traceCorpus{blocks: uint32(oti.SourceBlocks), trace: record.observations}
}

func executeDecodeFailure() traceCorpus {
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
	record := &recorder{oti: oti, context: "decode-failure-6330", want: want}
	decoder, err := raptorgo.NewObjectDecoder(
		oti,
		limits,
		record.context,
		func(raptorgo.VerificationRequest) error {
			panic("rank-deficient trace unexpectedly invoked verification")
		},
		func(raptorgo.VerifiedObject) error {
			panic("rank-deficient trace unexpectedly invoked delivery")
		},
	)
	must(err)
	record.decoder = decoder
	record.observe("init", "ok", true)

	var firstPacket []byte
	for esi := uint32(319); esi <= 328; esi++ {
		packet, packetErr := encoder.Packet(0, esi, 1, false)
		must(packetErr)
		if firstPacket == nil {
			firstPacket = append([]byte(nil), packet...)
		}
		state, addErr := decoder.AddPacket(packet)
		if addErr != nil || state != raptorgo.ObjectReceiving {
			panic(fmt.Sprintf("deficient ESI %d state=%s error=%v", esi, state, addErr))
		}
		record.observe("add_wait", "ok", true)
	}

	packet, err := encoder.Packet(0, 329, 1, false)
	must(err)
	state, err := decoder.AddPacket(packet)
	if state != raptorgo.ObjectDecodeFailed || !errors.Is(err, raptorgo.ErrObjectDecoder) ||
		!errors.Is(err, codec.ErrDecoderAttemptLimit) {
		panic(fmt.Sprintf("decode failure state=%s error=%v", state, err))
	}
	record.observe("decode_failure", "decode_error", true)

	state, err = decoder.AddPacket(firstPacket)
	if state != raptorgo.ObjectDecodeFailed || !errors.Is(err, raptorgo.ErrObjectTerminal) {
		panic(fmt.Sprintf("decode-failed terminal add state=%s error=%v", state, err))
	}
	record.observe("terminal_add", "terminal", true)
	return traceCorpus{blocks: uint32(oti.SourceBlocks), trace: record.observations}
}

func (record *recorder) observe(operation, result string, bindingOK bool) {
	if record.decoder == nil {
		if operation != "init" {
			panic("callback observed before decoder publication")
		}
		return
	}
	record.observations = append(record.observations, observation{
		operation:    operation,
		result:       result,
		state:        record.decoder.State(),
		completed:    record.decoder.CompletedBlocks(),
		verification: record.verification,
		delivery:     record.delivery,
		bindingOK:    bindingOK,
	})
}

func render(module string, corpus traceCorpus) []byte {
	var output bytes.Buffer
	fmt.Fprintf(&output, "---------------- MODULE %s ----------------\n", module)
	fmt.Fprintln(&output, "EXTENDS Naturals, Sequences")
	fmt.Fprintf(&output, "\nImplementationBlocks == %d\n", corpus.blocks)
	fmt.Fprintln(&output, "\nImplementationTrace == <<")
	for index, event := range corpus.trace {
		comma := ","
		if index+1 == len(corpus.trace) {
			comma = ""
		}
		fmt.Fprintf(
			&output,
			"    [op |-> %q, result |-> %q, state |-> %q, completed |-> %d, verification |-> %d, delivery |-> %d, bindingOK |-> %s]%s\n",
			event.operation, event.result, event.state.String(), event.completed,
			event.verification, event.delivery, tlaBool(event.bindingOK), comma,
		)
	}
	fmt.Fprintln(&output, ">>\n\n=====================================================================")
	return output.Bytes()
}

func tlaBool(value bool) string {
	if value {
		return "TRUE"
	}
	return "FALSE"
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
