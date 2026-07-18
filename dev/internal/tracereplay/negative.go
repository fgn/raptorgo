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

const maxNegativeCases = 16 * 1024

type negativeCase struct {
	index  int
	name   string
	events []objtrace.Event
}

func runNegative(ctx context.Context, config resolvedConfig, inputs []traceInput) (Report, error) {
	work := make(chan negativeCase, config.jobs)
	results := make(chan traceResult, config.jobs)
	var workers sync.WaitGroup
	for range config.jobs {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for test := range work {
				failure := replayEvents(ctx, config, test.name, test.events, expectTraceRejected)
				results <- traceResult{index: test.index, failure: failure}
			}
		}()
	}

	var collected []traceResult
	collectorDone := make(chan struct{})
	go func() {
		for result := range results {
			collected = append(collected, result)
		}
		close(collectorDone)
	}()

	names := make([]string, 0)
	generationErr := walkNegativeCases(inputs, maxNegativeCases, func(name string, events []objtrace.Event) error {
		index := len(names)
		names = append(names, name)
		select {
		case work <- negativeCase{index: index, name: name, events: events}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	close(work)
	workers.Wait()
	close(results)
	<-collectorDone
	if generationErr != nil {
		return Report{}, generationErr
	}

	ordered := make([]*Failure, len(names))
	for _, result := range collected {
		ordered[result.index] = result.failure
	}
	indices := make([]int, len(names))
	for index := range indices {
		indices[index] = index
	}
	sort.Slice(indices, func(left, right int) bool { return names[indices[left]] < names[indices[right]] })
	report := Report{Inputs: make([]string, 0, len(names))}
	for _, index := range indices {
		report.Inputs = append(report.Inputs, names[index])
		failure := ordered[index]
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

func walkNegativeCases(
	inputs []traceInput,
	maximum int,
	visit func(string, []objtrace.Event) error,
) error {
	applied := make(map[string]bool, len(tracemutate.Operators))
	seenNames := make(map[string]bool)
	count := 0
	for _, input := range inputs {
		file, err := os.Open(input.path)
		if err != nil {
			return fmt.Errorf("tracereplay: open %s: %w", input.name, err)
		}
		events, readErr := objtrace.ReadNDJSON(file)
		closeErr := file.Close()
		if readErr != nil {
			return fmt.Errorf("tracereplay: read %s: %w", input.name, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("tracereplay: close %s: %w", input.name, closeErr)
		}
		stem := strings.TrimSuffix(input.name, ".ndjson")
		err = tracemutate.Walk(events, func(mutant tracemutate.Mutant) error {
			name := stem + "--" + mutant.ID + ".ndjson"
			if seenNames[name] {
				return fmt.Errorf("tracereplay: duplicate mutant name %s", name)
			}
			if count >= maximum {
				return fmt.Errorf("tracereplay: more than %d negative cases", maximum)
			}
			seenNames[name] = true
			applied[mutant.Operator] = true
			count++
			return visit(name, mutant.Events)
		})
		if err != nil {
			return fmt.Errorf("tracereplay: mutate %s: %w", input.name, err)
		}
	}
	var missing []string
	for _, operator := range tracemutate.Operators {
		if !applied[operator] {
			missing = append(missing, operator)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("tracereplay: mutation operators never applied: %s", strings.Join(missing, ", "))
	}
	return nil
}
