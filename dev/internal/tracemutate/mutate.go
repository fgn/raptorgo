// Package tracemutate defines deterministic negative transformations for
// ObjectDecoder traces. Applicability is structural and independent of TLC.
package tracemutate

import (
	"errors"
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
	mutants := make([]Mutant, 0)
	err := Walk(events, func(mutant Mutant) error {
		mutants = append(mutants, mutant)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return mutants, nil
}

// Walk applies every mutation in stable ID order while retaining only the
// current cloned trace. Callers that execute or write mutants incrementally
// avoid Generate's intentionally materialized result set.
func Walk(events []objtrace.Event, visit func(Mutant) error) error {
	if visit == nil {
		return errors.New("tracemutate: nil visitor")
	}
	if err := objtrace.ValidateTrace(events); err != nil {
		return err
	}
	plans := mutationPlans(events)
	for _, plan := range plans {
		if err := visit(applyPlan(events, plan)); err != nil {
			return fmt.Errorf("tracemutate: visit %s: %w", plan.id, err)
		}
	}
	return nil
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

type mutationPlan struct {
	id, operator string
	index, aux   int
}

func mutationPlans(events []objtrace.Event) []mutationPlan {
	plans := make([]mutationPlan, 0, len(events)+16)
	var candidates []int
	for index := 1; index+1 < len(events); index++ {
		switch events[index].Op {
		case "block_complete", "verify_enter", "verified", "deliver_enter", "decode_failure":
			candidates = append(candidates, index)
		}
	}
	candidates = stableCap(candidates, 8)
	for _, index := range candidates {
		plans = append(plans, mutationPlan{
			id: fmt.Sprintf("%s-%04d", DropStep, index), operator: DropStep, index: index,
		})
	}
	// The first record belongs to Init. Forging it yields no initial states,
	// which TLC correctly regards as an empty model rather than a TraceComplete
	// counterexample. Mutate every replayed record so rejection remains exact.
	for index := 1; index < len(events); index++ {
		plans = append(plans, mutationPlan{
			id: fmt.Sprintf("%s-%04d", ForgeBinding, index), operator: ForgeBinding, index: index,
		})
	}
	verified, delivery := indexOf(events, "verified"), indexOf(events, "deliver_enter")
	if verified >= 0 && delivery > verified {
		plans = append(plans, mutationPlan{id: EarlyDeliver, operator: EarlyDeliver, index: delivery, aux: verified})
		plans = append(plans, mutationPlan{id: SkipVerify, operator: SkipVerify, index: verified})
	}
	for index, event := range events {
		if event.Op != "terminal_add" {
			continue
		}
		plans = append(plans, mutationPlan{
			id: fmt.Sprintf("%s-%04d", Resurrect, index), operator: Resurrect, index: index,
		})
	}
	for index, event := range events {
		if event.Op != "verify_enter" || event.Completed == 0 {
			continue
		}
		plans = append(plans, mutationPlan{
			id: fmt.Sprintf("%s-%04d", ForgeCompletion, index), operator: ForgeCompletion, index: index,
		})
	}
	for index, event := range events {
		if event.Op != "deliver_enter" {
			continue
		}
		plans = append(plans, mutationPlan{
			id: fmt.Sprintf("%s-%04d", DoubleDelivery, index), operator: DoubleDelivery, index: index,
		})
	}
	sort.Slice(plans, func(left, right int) bool { return plans[left].id < plans[right].id })
	return plans
}

func applyPlan(events []objtrace.Event, plan mutationPlan) Mutant {
	mutated := clone(events)
	switch plan.operator {
	case DropStep, SkipVerify:
		mutated = append(mutated[:plan.index], mutated[plan.index+1:]...)
	case ForgeBinding:
		mutated[plan.index].BindingOK = false
	case EarlyDeliver:
		event := mutated[plan.index]
		mutated = append(mutated[:plan.index], mutated[plan.index+1:]...)
		mutated = insert(mutated, plan.aux, event)
	case Resurrect:
		mutated[plan.index].Op = "add_wait"
		mutated[plan.index].Result = "ok"
		mutated[plan.index].State = "receiving"
	case ForgeCompletion:
		mutated[plan.index].Completed--
	case DoubleDelivery:
		mutated = insert(mutated, plan.index+1, mutated[plan.index])
	default:
		panic("unknown trace mutation operator " + plan.operator)
	}
	return Mutant{ID: plan.id, Operator: plan.operator, Events: mutated}
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
