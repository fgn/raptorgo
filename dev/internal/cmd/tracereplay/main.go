package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/fgn/raptorgo/dev/internal/tracereplay"
)

func main() {
	corpus := flag.String("corpus", "", "flat directory of positive NDJSON traces (default: repository corpus)")
	jobs := flag.Int("j", 1, fmt.Sprintf("parallel TLC processes (1..%d)", tracereplay.MaxJobs))
	keepFailed := flag.String("keep-failed", "", "directory in which to preserve failed replay workdirs")
	negative := flag.Bool("negative", false, "require every applicable trace mutant to fail TraceComplete")
	rootFlag := flag.String("root", "", "repository root (normally discovered from the current directory)")
	flag.Parse()
	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}

	root := *rootFlag
	if root == "" {
		working, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "tracereplay: current directory: %v\n", err)
			os.Exit(1)
		}
		root, err = tracereplay.FindRoot(working)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	corpusPath := *corpus
	if corpusPath == "" {
		corpusPath = filepath.Join(root, "dev", "formal", "corpus", "objectdecoder")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	report, err := tracereplay.Run(ctx, tracereplay.Config{
		Root: root, Corpus: corpusPath, KeepFailed: *keepFailed, Jobs: *jobs, Negative: *negative,
	})
	if err == nil {
		kind := "traces"
		if *negative {
			kind = "mutants"
		}
		fmt.Printf("tracereplay: passed %d/%d %s\n", report.Passed, len(report.Inputs), kind)
		return
	}

	var failures *tracereplay.FailuresError
	if !errors.As(err, &failures) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, failure := range report.Failures {
		fmt.Fprintf(os.Stderr, "%s: %v\n", failure.Input, failure.Err)
		if len(failure.Output) != 0 {
			output := strings.TrimSuffix(string(failure.Output), "\n")
			fmt.Fprintf(os.Stderr, "%s\n", output)
		}
		if failure.KeptAt != "" {
			fmt.Fprintf(os.Stderr, "%s: preserved at %s\n", failure.Input, failure.KeptAt)
		}
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
