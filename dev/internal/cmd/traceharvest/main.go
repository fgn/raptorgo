// Command traceharvest regenerates the deterministic ObjectDecoder NDJSON
// corpus from public-API execution scenarios.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/fgn/raptorgo/dev/internal/harvest"
	"github.com/fgn/raptorgo/dev/internal/tracecorpus"
)

const minimumCuratedTraces = 20

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("traceharvest", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "", "directory for fingerprint-named NDJSON traces")
	maximum := flags.Int("max", tracecorpus.DefaultMax, "maximum curated traces")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("traceharvest: unexpected arguments: %v", flags.Args())
	}
	if *output == "" {
		return errors.New("traceharvest: -output is required")
	}

	candidates, err := harvest.Candidates()
	if err != nil {
		return err
	}
	result, err := tracecorpus.Curate(candidates, tracecorpus.Options{
		Max: *maximum, Required: harvest.RequiredFeatures(),
	})
	if err != nil {
		return err
	}
	if len(result.Traces) < minimumCuratedTraces {
		return fmt.Errorf(
			"traceharvest: curated %d traces, want at least %d",
			len(result.Traces), minimumCuratedTraces,
		)
	}
	if err := tracecorpus.WriteDirectory(*output, result, stdout); err != nil {
		return err
	}
	fmt.Fprintf(
		stdout,
		"summary candidates=%d kept=%d dropped=%d maximum=%d\n",
		len(candidates), len(result.Traces), len(result.Dropped), *maximum,
	)
	return nil
}
