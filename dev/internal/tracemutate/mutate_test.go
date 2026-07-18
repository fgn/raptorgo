package tracemutate

import (
	"fmt"
	"testing"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

func deliveredTrace() []objtrace.Event {
	blocks := int32(2)
	return []objtrace.Event{
		{V: 1, Op: "init", Result: "ok", State: "receiving", BindingOK: true, Blocks: &blocks},
		{V: 1, Op: "add_wait", Result: "ok", State: "receiving", BindingOK: true},
		{V: 1, Op: "block_complete", Result: "ok", State: "receiving", Completed: 1, BindingOK: true},
		{V: 1, Op: "verify_enter", Result: "ok", State: "decoded", Completed: 2, Verification: 1, BindingOK: true},
		{V: 1, Op: "verified", Result: "ok", State: "verified", Completed: 2, Verification: 1, BindingOK: true},
		{V: 1, Op: "deliver_enter", Result: "ok", State: "verified", Completed: 2, Verification: 1, Delivery: 1, BindingOK: true},
		{V: 1, Op: "add_return", Result: "ok", State: "delivered", Completed: 2, Verification: 1, Delivery: 1, BindingOK: true},
		{V: 1, Op: "terminal_add", Result: "terminal", State: "delivered", Completed: 2, Verification: 1, Delivery: 1, BindingOK: true},
	}
}

func TestGenerateAppliesEveryOperator(t *testing.T) {
	mutants, err := Generate(deliveredTrace())
	if err != nil {
		t.Fatal(err)
	}
	got := AppliedOperators(mutants)
	if len(got) != len(Operators) {
		t.Fatalf("operators = %v, want %v", got, Operators)
	}
	for index := range Operators {
		if got[index] != Operators[index] {
			t.Fatalf("operators = %v, want %v", got, Operators)
		}
	}
}

func TestDropStepExcludesLegalNoOpDeletions(t *testing.T) {
	mutants, err := Generate(deliveredTrace())
	if err != nil {
		t.Fatal(err)
	}
	for _, mutant := range mutants {
		if mutant.Operator != DropStep {
			continue
		}
		if mutant.ID == "drop-step-0001" || mutant.ID == "drop-step-0007" {
			t.Fatalf("unsound no-op deletion generated: %s", mutant.ID)
		}
	}
}

func TestMutationShapes(t *testing.T) {
	mutants, err := Generate(deliveredTrace())
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]Mutant)
	for _, mutant := range mutants {
		byID[mutant.ID] = mutant
	}
	if got := byID[EarlyDeliver].Events; got[4].Op != "deliver_enter" || got[5].Op != "verified" {
		t.Fatalf("early delivery = %v, %v", got[4].Op, got[5].Op)
	}
	if got := byID[SkipVerify].Events; indexOf(got, "verified") >= 0 || indexOf(got, "deliver_enter") < 0 {
		t.Fatalf("skip verify = %#v", got)
	}
	if got := byID["forge-completion-0003"].Events[3].Completed; got != 1 {
		t.Fatalf("forged completion = %d", got)
	}
	if got := byID["double-delivery-0005"].Events; got[5].Op != "deliver_enter" || got[6].Op != "deliver_enter" {
		t.Fatalf("double delivery operations = %q, %q", got[5].Op, got[6].Op)
	}
}

func TestForgeBindingCoversEveryRecord(t *testing.T) {
	events := deliveredTrace()
	mutants, err := Generate(events)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, len(events)-1)
	for _, mutant := range mutants {
		if mutant.Operator != ForgeBinding {
			continue
		}
		seen[mutant.ID] = true
	}
	if seen["forge-binding-0000"] {
		t.Error("initial record must not be forged by an exact TraceComplete mutant")
	}
	for index := 1; index < len(events); index++ {
		id := fmt.Sprintf("forge-binding-%04d", index)
		if !seen[id] {
			t.Errorf("missing %s", id)
		}
	}
}

func TestGenerateDoesNotAliasInput(t *testing.T) {
	input := deliveredTrace()
	mutants, err := Generate(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutant := range mutants {
		if mutant.Operator == Resurrect {
			*mutant.Events[0].Blocks = 99
			mutant.Events[0].Op = "changed"
			break
		}
	}
	if *input[0].Blocks != 2 || input[0].Op != "init" {
		t.Fatalf("input aliased: %#v", input[0])
	}
}

func TestDropStepStableCap(t *testing.T) {
	blocks := int32(20)
	events := []objtrace.Event{{V: 1, Op: "init", Result: "ok", State: "receiving", BindingOK: true, Blocks: &blocks}}
	for completed := uint32(1); completed <= 12; completed++ {
		events = append(events, objtrace.Event{
			V: 1, Op: "block_complete", Result: "ok", State: "receiving", Completed: completed, BindingOK: true,
		})
	}
	events = append(events, objtrace.Event{
		V: 1, Op: "verify_enter", Result: "ok", State: "decoded", Completed: 20, Verification: 1, BindingOK: true,
	})
	mutants, err := Generate(events)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, mutant := range mutants {
		if mutant.Operator == DropStep {
			count++
		}
	}
	if count != 8 {
		t.Fatalf("drop-step mutants = %d, want 8", count)
	}
}
