package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("trace2tla", flag.ContinueOnError)
	input := flags.String("input", "", "input NDJSON trace")
	module := flags.String("module", "", "TLA+ data module name")
	blocks := flags.Int("blocks", -1, "number of nonempty source blocks")
	output := flags.String("output", "", "output TLA+ data module")
	var usage bytes.Buffer
	flags.SetOutput(&usage)
	if err := flags.Parse(arguments); err != nil {
		return fmt.Errorf("trace2tla: %w", err)
	}
	if flags.NArg() != 0 || *input == "" || *module == "" || *blocks < 0 || *output == "" {
		return fmt.Errorf("trace2tla: -input, -module, nonnegative -blocks, and -output are required\n%s", usage.String())
	}
	if int64(*blocks) > int64(^uint32(0)>>1) {
		return fmt.Errorf("trace2tla: blocks %d exceed int32", *blocks)
	}

	file, err := os.Open(*input)
	if err != nil {
		return fmt.Errorf("trace2tla: open %s: %w", *input, err)
	}
	events, readErr := objtrace.ReadNDJSON(file)
	closeErr := file.Close()
	if readErr != nil {
		return fmt.Errorf("trace2tla: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("trace2tla: close %s: %w", *input, closeErr)
	}
	firstBlocks := events[0].Blocks
	if firstBlocks == nil || int32(*blocks) != *firstBlocks {
		return fmt.Errorf("trace2tla: -blocks %d does not match first event", *blocks)
	}
	rendered, err := objtrace.RenderTLA(*module, int32(*blocks), events)
	if err != nil {
		return fmt.Errorf("trace2tla: %w", err)
	}
	if err := os.WriteFile(*output, rendered, 0o644); err != nil {
		return fmt.Errorf("trace2tla: write %s: %w", *output, err)
	}
	return nil
}
