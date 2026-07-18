package main

import (
	"path/filepath"
	"testing"

	"github.com/fgn/raptorgo/dev/internal/tracecover"
)

func TestCommittedCorpusSelectsEveryWitnessDeterministically(t *testing.T) {
	manifest, err := tracecover.LoadManifest("../../../formal/coverage/actions.json")
	if err != nil {
		t.Fatal(err)
	}
	traces, err := tracecover.LoadTraces(
		tracecover.SourceCorpus,
		[]string{"../../../formal/corpus/objectdecoder"},
	)
	if err != nil {
		t.Fatal(err)
	}
	first, err := selectWitnesses(manifest, traces)
	if err != nil {
		t.Fatal(err)
	}
	second, err := selectWitnesses(manifest, traces)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != tracecover.ExpectedActionCount || len(second) != len(first) {
		t.Fatalf("witness counts = %d/%d", len(first), len(second))
	}
	for index := range first {
		if first[index].name != second[index].name || filepath.Ext(first[index].name) != ".ndjson" {
			t.Fatalf("witness %d drifted: %#v / %#v", index, first[index], second[index])
		}
	}
}
