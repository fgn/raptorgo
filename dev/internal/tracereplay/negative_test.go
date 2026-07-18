package tracereplay

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
	"github.com/fgn/raptorgo/dev/internal/tracemutate"
)

func TestRunNegativeRequiresExactRejectionAndCoversOperators(t *testing.T) {
	root, err := FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	corpus := t.TempDir()
	writeDeliveredTrace(t, filepath.Join(corpus, "delivered.ndjson"))
	execute := func(context.Context, Command) ([]byte, error) {
		return []byte(tlcTraceRejectMarker + "\n"), errors.New("exit status 13")
	}
	report, err := Run(context.Background(), Config{
		Root: root, Corpus: corpus, Jobs: 2, Negative: true, Exec: execute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed != len(report.Inputs) || report.Passed < len(tracemutate.Operators) {
		t.Fatalf("report = %#v", report)
	}
	for index := 1; index < len(report.Inputs); index++ {
		if report.Inputs[index-1] >= report.Inputs[index] {
			t.Fatalf("inputs are not sorted: %v", report.Inputs)
		}
	}
}

func TestRunNegativeRejectsUnexpectedPassAndWrongFailure(t *testing.T) {
	root, err := FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	corpus := t.TempDir()
	writeDeliveredTrace(t, filepath.Join(corpus, "delivered.ndjson"))
	for _, test := range []struct {
		name     string
		output   []byte
		runErr   error
		contains string
	}{
		{"unexpected-pass", []byte(tlcInitialStateMarker + "\n" + tlcSuccessMarker), nil, "unexpectedly passed"},
		{"wrong-failure", []byte("Error: parse failed"), errors.New("exit status 13"), "without exact marker"},
	} {
		t.Run(test.name, func(t *testing.T) {
			execute := func(context.Context, Command) ([]byte, error) { return test.output, test.runErr }
			report, err := Run(context.Background(), Config{
				Root: root, Corpus: corpus, Jobs: 1, Negative: true, Exec: execute,
			})
			if err == nil || len(report.Failures) == 0 || !strings.Contains(report.Failures[0].Err.Error(), test.contains) {
				t.Fatalf("report=%#v error=%v", report, err)
			}
		})
	}
}

func TestRunNegativeRejectsVacuousOperatorCorpus(t *testing.T) {
	root, err := FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	corpus := t.TempDir()
	blocks := int32(1)
	events := []objtrace.Event{{
		V: 1, Op: "init", Result: "ok", State: "receiving", BindingOK: true, Blocks: &blocks,
	}}
	file, err := os.Create(filepath.Join(corpus, "init.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if err := objtrace.WriteNDJSON(file, events); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = Run(context.Background(), Config{Root: root, Corpus: corpus, Jobs: 1, Negative: true})
	if err == nil || !strings.Contains(err.Error(), "operators never applied") {
		t.Fatalf("error = %v", err)
	}
}

func TestWalkNegativeCasesEnforcesCaseCeiling(t *testing.T) {
	corpus := t.TempDir()
	path := filepath.Join(corpus, "delivered.ndjson")
	writeDeliveredTrace(t, path)
	inputs := []traceInput{{name: "delivered.ndjson", path: path}}
	visited := 0
	err := walkNegativeCases(inputs, 1, func(string, []objtrace.Event) error {
		visited++
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "more than 1 negative cases") {
		t.Fatalf("ceiling error = %v", err)
	}
	if visited != 1 {
		t.Fatalf("visited = %d, want 1", visited)
	}
}

func writeDeliveredTrace(t *testing.T, path string) {
	t.Helper()
	blocks := int32(1)
	events := []objtrace.Event{
		{V: 1, Op: "init", Result: "ok", State: "receiving", BindingOK: true, Blocks: &blocks},
		{V: 1, Op: "verify_enter", Result: "ok", State: "decoded", Completed: 1, Verification: 1, BindingOK: true},
		{V: 1, Op: "verified", Result: "ok", State: "verified", Completed: 1, Verification: 1, BindingOK: true},
		{V: 1, Op: "deliver_enter", Result: "ok", State: "verified", Completed: 1, Verification: 1, Delivery: 1, BindingOK: true},
		{V: 1, Op: "add_return", Result: "ok", State: "delivered", Completed: 1, Verification: 1, Delivery: 1, BindingOK: true},
		{V: 1, Op: "terminal_add", Result: "terminal", State: "delivered", Completed: 1, Verification: 1, Delivery: 1, BindingOK: true},
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := objtrace.WriteNDJSON(file, events); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
