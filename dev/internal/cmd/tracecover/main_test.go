package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

func writeConstructTrace(t *testing.T, directory string) {
	t.Helper()
	blocks := int32(0)
	var encoded bytes.Buffer
	if err := objtrace.WriteNDJSON(&encoded, []objtrace.Event{{
		V: objtrace.Version, Op: "construct_final", Result: "ok", State: "delivered",
		Verification: 1, Delivery: 1, BindingOK: true, Blocks: &blocks,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "construct.ndjson"), encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func manifestPath() string {
	return filepath.Join("..", "..", "..", "formal", "coverage", "actions.json")
}

func TestRunSupportsIndependentWitnessInputAndGate(t *testing.T) {
	directory := t.TempDir()
	writeConstructTrace(t, directory)
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{
		"-manifest", manifestPath(), "-witness", directory, "-format", "json",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, &stderr)
	}
	if !strings.Contains(stdout.String(), `"name": "ConstructDelivered"`) ||
		!strings.Contains(stdout.String(), `"witness": 1`) {
		t.Fatalf("JSON report = %s", &stdout)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = run([]string{
		"-manifest", manifestPath(), "-witness", directory, "-require-witness-full",
	}, &stdout, &stderr)
	if exitCode != 1 || !strings.Contains(stderr.String(), "witness coverage missing") {
		t.Fatalf("gated exit=%d stdout=%s stderr=%s", exitCode, &stdout, &stderr)
	}
}

func TestRunFailsOnEmptyCorpusAndMismatchedGate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run([]string{
		"-manifest", manifestPath(), "-corpus", t.TempDir(),
	}, &stdout, &stderr)
	if exitCode != 1 || !strings.Contains(stderr.String(), "contains no .ndjson traces") {
		t.Fatalf("empty exit=%d stderr=%s", exitCode, &stderr)
	}

	stdout.Reset()
	stderr.Reset()
	directory := t.TempDir()
	writeConstructTrace(t, directory)
	exitCode = run([]string{
		"-manifest", manifestPath(), "-witness", directory, "-require-corpus-full",
	}, &stdout, &stderr)
	if exitCode != 2 || !strings.Contains(stderr.String(), "requires -corpus") {
		t.Fatalf("mismatched exit=%d stderr=%s", exitCode, &stderr)
	}
}
