package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/fgn/raptorgo/dev/internal/verifytrace"
)

func main() {
	output := flag.String("output", "", "path for generated TLA+ module")
	scenario := flag.String("scenario", "deliver", "trace scenario: deliver, abort, verify-fail, or rejected-calls")
	flag.Parse()
	if *output == "" {
		fmt.Fprintln(os.Stderr, "gentrace: -output is required")
		os.Exit(2)
	}

	decoder, err := verifytrace.NewDecoder([]int{0, 1, 2}, 2)
	must(err)
	module := "DecoderTraceData"
	switch *scenario {
	case "deliver":
		must(decoder.Accept(0))
		must(decoder.Accept(0))
		must(decoder.Accept(99))
		must(decoder.Accept(2))
		must(decoder.Decode())
		must(decoder.Verify(true))
		must(decoder.Deliver())
	case "abort":
		module = "DecoderAbortTraceData"
		must(decoder.Accept(1))
		must(decoder.Abort())
	case "verify-fail":
		module = "DecoderVerifyFailureTraceData"
		must(decoder.Accept(0))
		must(decoder.Accept(2))
		must(decoder.Decode())
		must(decoder.Verify(false))
	case "rejected-calls":
		module = "DecoderRejectedCallsTraceData"
		mustFail(decoder.Decode())
		decoder.Observe()
		must(decoder.Accept(0))
		must(decoder.Accept(2))
		must(decoder.Decode())
		mustFail(decoder.Deliver())
		decoder.Observe()
		must(decoder.Verify(true))
		must(decoder.Deliver())
		mustFail(decoder.Accept(99))
		decoder.Observe()
		mustFail(decoder.Decode())
		decoder.Observe()
		mustFail(decoder.Verify(true))
		decoder.Observe()
		mustFail(decoder.Deliver())
		decoder.Observe()
		mustFail(decoder.Abort())
		decoder.Observe()
	default:
		must(fmt.Errorf("gentrace: unknown scenario %q", *scenario))
	}

	file, err := os.Create(*output)
	must(err)
	if err := decoder.WriteTLA(file, module); err != nil {
		_ = file.Close()
		must(err)
	}
	must(file.Close())
}

func mustFail(err error) {
	if err == nil {
		must(fmt.Errorf("gentrace: operation unexpectedly succeeded"))
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
