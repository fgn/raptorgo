package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunConvertsTrace(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "trace.ndjson")
	output := filepath.Join(directory, "TraceData.tla")
	const trace = `{"v":1,"op":"init","result":"ok","state":"receiving","completed":0,"verification":0,"delivery":0,"bindingOK":true,"blocks":2}` + "\n"
	if err := os.WriteFile(input, []byte(trace), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-input", input, "-module", "TraceData", "-blocks", "2", "-output", output}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "ImplementationBlocks == 2") ||
		!strings.Contains(string(got), `op |-> "init"`) {
		t.Fatalf("output:\n%s", got)
	}
}

func TestRunRejectsBlockMismatch(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "trace.ndjson")
	const trace = `{"v":1,"op":"init","result":"ok","state":"receiving","completed":0,"verification":0,"delivery":0,"bindingOK":true,"blocks":2}` + "\n"
	if err := os.WriteFile(input, []byte(trace), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"-input", input, "-module", "TraceData", "-blocks", "1", "-output", filepath.Join(directory, "out")})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v", err)
	}
}
