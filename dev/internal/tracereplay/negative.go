package tracereplay

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
	"github.com/fgn/raptorgo/dev/internal/tracemutate"
)

type negativeCase struct {
	name   string
	events []objtrace.Event
}

func runNegative(ctx context.Context, config resolvedConfig, inputs []traceInput) (Report, error) {
	cases, err := buildNegativeCases(inputs)
	if err != nil {
		return Report{}, err
	}
	report := Report{Inputs: make([]string, len(cases))}
	for index, test := range cases {
		report.Inputs[index] = test.name
	}

	work := make(chan int, len(cases))
	results := make(chan traceResult, len(cases))
	for index := range cases {
		work <- index
	}
	close(work)
	var workers sync.WaitGroup
	for range config.jobs {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range work {
				failure := replayEvents(ctx, config, cases[index].name, cases[index].events, expectTraceRejected)
				results <- traceResult{index: index, failure: failure}
			}
		}()
	}
	workers.Wait()
	close(results)

	ordered := make([]*Failure, len(cases))
	for result := range results {
		ordered[result.index] = result.failure
	}
	for _, failure := range ordered {
		if failure == nil {
			report.Passed++
			continue
		}
		report.Failures = append(report.Failures, *failure)
	}
	if len(report.Failures) != 0 {
		return report, &FailuresError{Failures: append([]Failure(nil), report.Failures...)}
	}
	return report, nil
}

func buildNegativeCases(inputs []traceInput) ([]negativeCase, error) {
	var cases []negativeCase
	applied := make(map[string]bool, len(tracemutate.Operators))
	seenNames := make(map[string]bool)
	for _, input := range inputs {
		file, err := os.Open(input.path)
		if err != nil {
			return nil, fmt.Errorf("tracereplay: open %s: %w", input.name, err)
		}
		events, readErr := objtrace.ReadNDJSON(file)
		closeErr := file.Close()
		if readErr != nil {
			return nil, fmt.Errorf("tracereplay: read %s: %w", input.name, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("tracereplay: close %s: %w", input.name, closeErr)
		}
		mutants, err := tracemutate.Generate(events)
		if err != nil {
			return nil, fmt.Errorf("tracereplay: mutate %s: %w", input.name, err)
		}
		stem := strings.TrimSuffix(input.name, ".ndjson")
		for _, mutant := range mutants {
			name := stem + "--" + mutant.ID + ".ndjson"
			if seenNames[name] {
				return nil, fmt.Errorf("tracereplay: duplicate mutant name %s", name)
			}
			seenNames[name] = true
			applied[mutant.Operator] = true
			cases = append(cases, negativeCase{name: name, events: mutant.Events})
		}
	}
	var missing []string
	for _, operator := range tracemutate.Operators {
		if !applied[operator] {
			missing = append(missing, operator)
		}
	}
	if len(missing) != 0 {
		return nil, fmt.Errorf("tracereplay: mutation operators never applied: %s", strings.Join(missing, ", "))
	}
	sort.Slice(cases, func(left, right int) bool { return cases[left].name < cases[right].name })
	return cases, nil
}
