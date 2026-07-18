package main

import (
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
	file, err := os.Open(*input)
	must(err)
	events, readErr := objtrace.ReadNDJSON(file)
	closeErr := file.Close()
	must(readErr)
	must(closeErr)
	count := 0
	err = tracemutate.Walk(events, func(mutant tracemutate.Mutant) error {
		if count == 0 {
			if mkdirErr := os.MkdirAll(*output, 0o755); mkdirErr != nil {
				return mkdirErr
			}
		}
		count++
		encoded, encodeErr := objtrace.CanonicalNDJSON(mutant.Events)
		if encodeErr != nil {
			return encodeErr
		}
		path := filepath.Join(*output, mutant.ID+".ndjson")
		if writeErr := os.WriteFile(path, encoded, 0o644); writeErr != nil {
			return writeErr
		}
		fmt.Println(path)
		return nil
	})
	must(err)
	if count == 0 {
		if *allowEmpty {
			return
		}
		panic("tracemutate: no operators apply")
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
