package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
	"github.com/fgn/raptorgo/dev/internal/tracemutate"
)

func main() {
	input := flag.String("input", "", "input NDJSON trace")
	output := flag.String("output", "", "output directory")
	allowEmpty := flag.Bool("allow-empty", false, "accept a valid trace to which no mutation applies")
	flag.Parse()
	if *input == "" || *output == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	encoded, err := os.ReadFile(*input)
	must(err)
	events, err := objtrace.ReadNDJSON(bytes.NewReader(encoded))
	must(err)
	mutants, err := tracemutate.Generate(events)
	must(err)
	if len(mutants) == 0 {
		if *allowEmpty {
			return
		}
		panic("tracemutate: no operators apply")
	}
	must(os.MkdirAll(*output, 0o755))
	for _, mutant := range mutants {
		encoded, encodeErr := objtrace.CanonicalNDJSON(mutant.Events)
		must(encodeErr)
		path := filepath.Join(*output, mutant.ID+".ndjson")
		must(os.WriteFile(path, encoded, 0o644))
		fmt.Println(path)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
