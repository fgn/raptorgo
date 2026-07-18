package harvest

import (
	"bytes"
	"reflect"
	"sort"
	"testing"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
	"github.com/fgn/raptorgo/dev/internal/tracecorpus"
)

func TestBuiltInCorpusIsDeterministicAndPreservesRequiredFamilies(t *testing.T) {
	first, err := Candidates()
	if err != nil {
		t.Fatal(err)
	}
	second, err := Candidates()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 20 || len(first) != len(second) {
		t.Fatalf("candidate counts = %d and %d", len(first), len(second))
	}
	for index := range first {
		left, err := objtrace.CanonicalNDJSON(first[index].Events)
		if err != nil {
			t.Fatalf("candidate %s: %v", first[index].Source, err)
		}
		right, err := objtrace.CanonicalNDJSON(second[index].Events)
		if err != nil {
			t.Fatalf("candidate %s second run: %v", second[index].Source, err)
		}
		if first[index].Source != second[index].Source || !bytes.Equal(left, right) {
			t.Fatalf("candidate %d is nondeterministic: %s vs %s", index, first[index].Source, second[index].Source)
		}
	}

	result, err := tracecorpus.Curate(first, tracecorpus.Options{
		Max: tracecorpus.DefaultMax, Required: RequiredFeatures(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Traces) < 20 {
		t.Fatalf("curated trace count = %d, want at least 20", len(result.Traces))
	}
	covered := make(map[tracecorpus.Feature]bool)
	seenFingerprints := make(map[string]bool)
	for _, trace := range result.Traces {
		if seenFingerprints[trace.Fingerprint] {
			t.Fatalf("duplicate fingerprint %s", trace.Fingerprint)
		}
		seenFingerprints[trace.Fingerprint] = true
		for _, feature := range trace.Features {
			covered[feature] = true
		}
	}
	for _, feature := range RequiredFeatures() {
		if !covered[feature] {
			t.Errorf("required feature %q is not covered", feature)
		}
	}
}

func TestCriticalScenarioShapes(t *testing.T) {
	candidates, err := Candidates()
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]tracecorpus.Candidate, len(candidates))
	for _, candidate := range candidates {
		byName[candidate.Source] = candidate
	}
	tests := []struct {
		name string
		ops  []string
	}{
		{
			name: "verifier_reentrant_reject",
			ops:  []string{"init", "verify_enter", "terminal_add", "verified", "deliver_enter", "add_return"},
		},
		{
			name: "deliverer_reentrant_reject",
			ops:  []string{"init", "verify_enter", "verified", "deliver_enter", "terminal_add", "add_return"},
		},
		{
			name: "construct_success",
			ops:  []string{"construct_final"},
		},
		{
			name: "conflict_then_deliver",
			ops:  []string{"init", "add_wait", "add_reject_conflict", "verify_enter", "verified", "deliver_enter", "add_return"},
		},
	}
	for _, test := range tests {
		candidate, ok := byName[test.name]
		if !ok {
			t.Fatalf("missing candidate %s", test.name)
		}
		ops := make([]string, len(candidate.Events))
		for index, event := range candidate.Events {
			ops[index] = event.Op
		}
		if !reflect.DeepEqual(ops, test.ops) {
			t.Errorf("%s ops = %v, want %v", test.name, ops, test.ops)
		}
	}

	trailing := byName["trailing_empty_z255"]
	if len(trailing.Events) == 0 || trailing.Events[0].Blocks == nil || *trailing.Events[0].Blocks != 1 {
		t.Fatalf("Z=255 trace does not report one nonempty block: %#v", trailing.Events)
	}
}

func TestRequiredFeaturesAndScenarioTableAreStablyOrdered(t *testing.T) {
	features := RequiredFeatures()
	if !sort.SliceIsSorted(features, func(left, right int) bool { return features[left] < features[right] }) {
		t.Fatalf("required features are not sorted: %v", features)
	}
	seen := make(map[tracecorpus.Feature]bool, len(features))
	for _, feature := range features {
		if seen[feature] {
			t.Fatalf("duplicate required feature %q", feature)
		}
		seen[feature] = true
	}
}
