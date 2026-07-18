// Command tracecover measures ObjectDecoder trace coverage against the pinned
// formal action alphabet.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fgn/raptorgo/dev/internal/tracecover"
)

type pathList []string

func (paths *pathList) String() string { return strings.Join(*paths, ",") }

func (paths *pathList) Set(value string) error {
	if value == "" {
		return errors.New("path must not be empty")
	}
	*paths = append(*paths, value)
	return nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("tracecover", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "formal/coverage/actions.json", "coverage manifest JSON")
	format := flags.String("format", "human", "report format: human or json")
	requireFull := flags.Bool("require-full", false, "require full combined coverage")
	requireCorpusFull := flags.Bool("require-corpus-full", false, "require full corpus-only coverage")
	requireWitnessFull := flags.Bool("require-witness-full", false, "require full witness-only coverage")
	var corpusPaths, witnessPaths pathList
	flags.Var(&corpusPaths, "corpus", "corpus directory or .ndjson file (repeatable)")
	flags.Var(&witnessPaths, "witness", "witness directory or .ndjson file (repeatable)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: tracecover [-manifest path] [-format human|json] (-corpus path | -witness path) ...")
		flags.PrintDefaults()
	}
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "tracecover: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return 2
	}
	if len(corpusPaths) == 0 && len(witnessPaths) == 0 {
		fmt.Fprintln(stderr, "tracecover: at least one -corpus or -witness input is required")
		return 2
	}
	if *format != "human" && *format != "json" {
		fmt.Fprintf(stderr, "tracecover: unknown report format %q\n", *format)
		return 2
	}
	if *requireCorpusFull && len(corpusPaths) == 0 {
		fmt.Fprintln(stderr, "tracecover: -require-corpus-full requires -corpus")
		return 2
	}
	if *requireWitnessFull && len(witnessPaths) == 0 {
		fmt.Fprintln(stderr, "tracecover: -require-witness-full requires -witness")
		return 2
	}

	manifest, err := tracecover.LoadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	corpus, err := tracecover.LoadTraces(tracecover.SourceCorpus, corpusPaths)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	witnesses, err := tracecover.LoadTraces(tracecover.SourceWitness, witnessPaths)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	traces := append(corpus, witnesses...)
	report, err := tracecover.Analyze(manifest, traces)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var rendered []byte
	if *format == "json" {
		rendered, err = report.RenderJSON()
	} else {
		rendered = report.RenderHuman()
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if _, err := stdout.Write(rendered); err != nil {
		fmt.Fprintf(stderr, "tracecover: write report: %v\n", err)
		return 1
	}

	for _, requirement := range []struct {
		enabled bool
		scope   tracecover.Scope
	}{
		{*requireFull, tracecover.ScopeAll},
		{*requireCorpusFull, tracecover.ScopeCorpus},
		{*requireWitnessFull, tracecover.ScopeWitness},
	} {
		if !requirement.enabled {
			continue
		}
		if err := report.RequireFull(requirement.scope); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	return 0
}
