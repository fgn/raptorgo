// Package tracemutate defines deterministic negative transformations for
// ObjectDecoder traces. Applicability is structural and independent of TLC.
package tracemutate

import (
	"fmt"
	"sort"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

const (
	DropStep        = "drop-step"
	ForgeBinding    = "forge-binding"
	EarlyDeliver    = "early-deliver"
	SkipVerify      = "skip-verify"
	Resurrect       = "resurrect"
	ForgeCompletion = "forge-completion"
	DoubleDelivery  = "double-delivery"
)

// Operators is the stable, closed operator order used by coverage gates.
var Operators = []string{
	DropStep,
	ForgeBinding,
	EarlyDeliver,
	SkipVerify,
	Resurrect,
	ForgeCompletion,
	DoubleDelivery,
}

// Mutant is one stable-ID transformed trace.
type Mutant struct {
	ID       string
	Operator string
	Events   []objtrace.Event
}

// Generate applies every structurally applicable negative transformation.
// DropStep deliberately excludes semantic no-ops and terminal prefixes: their
// deletion can leave a legal shorter execution and is therefore not a sound
// negative control.
func Generate(events []objtrace.Event) ([]Mutant, error) {
	if err := objtrace.ValidateTrace(events); err != nil {
		return nil, err
	}
	mutants := make([]Mutant, 0)
	mutants = append(mutants, dropStepMutants(events)...)
	mutants = append(mutants, forgeBindingMutants(events)...)
	if mutant, ok := earlyDeliverMutant(events); ok {
		mutants = append(mutants, mutant)
	}
	if mutant, ok := skipVerifyMutant(events); ok {
		mutants = append(mutants, mutant)
	}
	mutants = append(mutants, resurrectMutants(events)...)
	mutants = append(mutants, forgeCompletionMutants(events)...)
	mutants = append(mutants, doubleDeliveryMutants(events)...)
	sort.Slice(mutants, func(left, right int) bool { return mutants[left].ID < mutants[right].ID })
	return mutants, nil
}

// AppliedOperators returns a stable set of operators represented by mutants.
func AppliedOperators(mutants []Mutant) []string {
	seen := make(map[string]bool, len(Operators))
	for _, mutant := range mutants {
		seen[mutant.Operator] = true
	}
	result := make([]string, 0, len(seen))
	for _, operator := range Operators {
		if seen[operator] {
			result = append(result, operator)
		}
	}
	return result
}

func dropStepMutants(events []objtrace.Event) []Mutant {
	var candidates []int
	for index := 1; index+1 < len(events); index++ {
		switch events[index].Op {
		case "block_complete", "verify_enter", "verified", "deliver_enter", "decode_failure":
			candidates = append(candidates, index)
		}
	}
	candidates = stableCap(candidates, 8)
	result := make([]Mutant, 0, len(candidates))
	for _, index := range candidates {
		mutated := clone(events)
		mutated = append(mutated[:index], mutated[index+1:]...)
		result = append(result, Mutant{
			ID: fmt.Sprintf("%s-%04d", DropStep, index), Operator: DropStep, Events: mutated,
		})
	}
	return result
}

func forgeBindingMutants(events []objtrace.Event) []Mutant {
	result := make([]Mutant, 0)
	for index, event := range events {
		switch event.Op {
		case "verify_enter", "verified", "deliver_enter":
			mutated := clone(events)
			mutated[index].BindingOK = false
			result = append(result, Mutant{
				ID: fmt.Sprintf("%s-%04d", ForgeBinding, index), Operator: ForgeBinding, Events: mutated,
			})
		}
	}
	return result
}

func earlyDeliverMutant(events []objtrace.Event) (Mutant, bool) {
	verified, delivery := indexOf(events, "verified"), indexOf(events, "deliver_enter")
	if verified < 0 || delivery < 0 || delivery <= verified {
		return Mutant{}, false
	}
	mutated := clone(events)
	event := mutated[delivery]
	mutated = append(mutated[:delivery], mutated[delivery+1:]...)
	mutated = insert(mutated, verified, event)
	return Mutant{ID: EarlyDeliver, Operator: EarlyDeliver, Events: mutated}, true
}

func skipVerifyMutant(events []objtrace.Event) (Mutant, bool) {
	verified, delivery := indexOf(events, "verified"), indexOf(events, "deliver_enter")
	if verified < 0 || delivery < 0 || delivery <= verified {
		return Mutant{}, false
	}
	mutated := clone(events)
	mutated = append(mutated[:verified], mutated[verified+1:]...)
	return Mutant{ID: SkipVerify, Operator: SkipVerify, Events: mutated}, true
}

func resurrectMutants(events []objtrace.Event) []Mutant {
	result := make([]Mutant, 0)
	for index, event := range events {
		if event.Op != "terminal_add" {
			continue
		}
		mutated := clone(events)
		mutated[index].Op = "add_wait"
		mutated[index].Result = "ok"
		mutated[index].State = "receiving"
		result = append(result, Mutant{
			ID: fmt.Sprintf("%s-%04d", Resurrect, index), Operator: Resurrect, Events: mutated,
		})
	}
	return result
}

func forgeCompletionMutants(events []objtrace.Event) []Mutant {
	result := make([]Mutant, 0)
	for index, event := range events {
		if event.Op != "verify_enter" || event.Completed == 0 {
			continue
		}
		mutated := clone(events)
		mutated[index].Completed--
		result = append(result, Mutant{
			ID: fmt.Sprintf("%s-%04d", ForgeCompletion, index), Operator: ForgeCompletion, Events: mutated,
		})
	}
	return result
}

func doubleDeliveryMutants(events []objtrace.Event) []Mutant {
	result := make([]Mutant, 0)
	for index, event := range events {
		if event.Op != "deliver_enter" {
			continue
		}
		mutated := clone(events)
		mutated = insert(mutated, index+1, mutated[index])
		result = append(result, Mutant{
			ID: fmt.Sprintf("%s-%04d", DoubleDelivery, index), Operator: DoubleDelivery, Events: mutated,
		})
	}
	return result
}

func indexOf(events []objtrace.Event, operation string) int {
	for index, event := range events {
		if event.Op == operation {
			return index
		}
	}
	return -1
}

func stableCap(values []int, limit int) []int {
	if len(values) <= limit {
		return values
	}
	result := make([]int, 0, limit)
	last := -1
	for index := 0; index < limit; index++ {
		selected := values[index*(len(values)-1)/(limit-1)]
		if selected != last {
			result = append(result, selected)
			last = selected
		}
	}
	return result
}

func insert(events []objtrace.Event, index int, event objtrace.Event) []objtrace.Event {
	events = append(events, objtrace.Event{})
	copy(events[index+1:], events[index:])
	events[index] = event
	return events
}

func clone(events []objtrace.Event) []objtrace.Event {
	result := make([]objtrace.Event, len(events))
	for index, event := range events {
		result[index] = event
		result[index].Blocks = cloneInt32(event.Blocks)
		result[index].SBN = cloneInt32(event.SBN)
		result[index].ESI = cloneInt32(event.ESI)
		result[index].Symbols = cloneInt32(event.Symbols)
	}
	return result
}

func cloneInt32(value *int32) *int32 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
