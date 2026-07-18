package tracecorpus

import (
	"bytes"
	"crypto/sha256"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

func testTrace(ops ...string) []objtrace.Event {
	blocks := int32(1)
	events := []objtrace.Event{{
		V: objtrace.Version, Op: "init", Result: "ok", State: "receiving", BindingOK: true, Blocks: &blocks,
	}}
	for _, op := range ops {
		event := objtrace.Event{
			V: objtrace.Version, Op: op, Result: "ok", State: "receiving", BindingOK: true,
		}
		switch op {
		case "add_reject_invalid":
			event.Result = "invalid"
		case "add_reject_limit":
			event.Result = "limit"
		case "add_reject_conflict":
			event.Result = "conflict"
		}
		events = append(events, event)
	}
	return events
}

func TestCurateIsIndependentOfCandidateOrder(t *testing.T) {
	candidates := []Candidate{
		{Source: "wait", Features: []Feature{"wait"}, Events: testTrace("add_wait")},
		{Source: "invalid", Features: []Feature{"invalid"}, Events: testTrace("add_reject_invalid")},
		{Source: "limit", Features: []Feature{"limit"}, Events: testTrace("add_reject_limit")},
		{Source: "conflict", Events: testTrace("add_reject_conflict")},
	}
	want, err := Curate(candidates, Options{Max: 3, Required: []Feature{"wait", "invalid"}})
	if err != nil {
		t.Fatal(err)
	}
	shuffled := []Candidate{candidates[2], candidates[0], candidates[3], candidates[1]}
	got, err := Curate(shuffled, Options{Max: 3, Required: []Feature{"invalid", "wait"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("shuffled curation differs:\ngot  %#v\nwant %#v", got, want)
	}
}

func TestCuratePreservesFamiliesBeforeCap(t *testing.T) {
	candidates := []Candidate{
		{Source: "both", Features: []Feature{"alpha", "beta"}, Events: testTrace("add_wait")},
		{Source: "novel", Events: testTrace("add_reject_invalid")},
	}
	result, err := Curate(candidates, Options{Max: 1, Required: []Feature{"alpha", "beta"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Traces) != 1 || result.Traces[0].Source != "both" {
		t.Fatalf("selected traces = %#v", result.Traces)
	}
	if len(result.Dropped) != 1 || result.Dropped[0].Reason != "maximum reached" {
		t.Fatalf("drop decisions = %#v", result.Dropped)
	}

	_, err = Curate([]Candidate{
		{Source: "alpha", Features: []Feature{"alpha"}, Events: testTrace("add_wait")},
		{Source: "beta", Features: []Feature{"beta"}, Events: testTrace("add_reject_limit")},
	}, Options{Max: 1, Required: []Feature{"alpha", "beta"}})
	if err == nil || !strings.Contains(err.Error(), "feature obligations") {
		t.Fatalf("cap error = %v", err)
	}
}

func TestCurateRejectsMissingFamilyAndShortCollision(t *testing.T) {
	_, err := Curate([]Candidate{{Source: "one", Events: testTrace("add_wait")}}, Options{
		Required: []Feature{"missing"},
	})
	if err == nil || !strings.Contains(err.Error(), "no candidates") {
		t.Fatalf("missing-family error = %v", err)
	}

	var calls byte
	fingerprint := func([]objtrace.Event) ([sha256.Size]byte, error) {
		var digest [sha256.Size]byte
		copy(digest[:6], []byte{1, 2, 3, 4, 5, 6})
		digest[6] = calls
		calls++
		return digest, nil
	}
	_, err = curate([]Candidate{
		{Source: "left", Events: testTrace("add_wait")},
		{Source: "right", Events: testTrace("add_reject_limit")},
	}, Options{}, fingerprint)
	if err == nil || !strings.Contains(err.Error(), "truncated fingerprint collision") {
		t.Fatalf("collision error = %v", err)
	}
}

func TestAbstractDuplicateCannotHideIncompatibleFamilyObligations(t *testing.T) {
	left := testTrace("add_wait")
	right := testTrace("add_wait")
	sbn := int32(7)
	right[1].SBN = &sbn
	_, err := Curate([]Candidate{
		{Source: "left", Features: []Feature{"left"}, Events: left},
		{Source: "right", Features: []Feature{"right"}, Events: right},
	}, Options{Required: []Feature{"left", "right"}})
	if err == nil || !strings.Contains(err.Error(), "no candidates") {
		t.Fatalf("incompatible obligation error = %v", err)
	}
}

func TestAbstractDuplicatePrefersRequiredFamilyRepresentative(t *testing.T) {
	optional := testTrace("add_wait")
	required := testTrace("add_wait")
	optionalSBN, requiredSBN := int32(1), int32(9)
	optional[1].SBN = &optionalSBN
	required[1].SBN = &requiredSBN
	result, err := Curate([]Candidate{
		{Source: "a-optional", Features: []Feature{"optional"}, Events: optional},
		{Source: "z-required", Features: []Feature{"required"}, Events: required},
	}, Options{Required: []Feature{"required"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Traces) != 1 || result.Traces[0].Source != "z-required" ||
		result.Traces[0].Events[1].SBN == nil || *result.Traces[0].Events[1].SBN != requiredSBN {
		t.Fatalf("representative = %#v", result.Traces)
	}
}

func TestWriteDirectoryRemovesStaleNDJSONAndLogsDeterministically(t *testing.T) {
	result, err := Curate([]Candidate{
		{Source: "wait", Features: []Feature{"wait"}, Events: testTrace("add_wait")},
		{Source: "duplicate", Events: testTrace("add_wait")},
	}, Options{Required: []Feature{"wait"}})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.WriteFile(directory+"/stale.ndjson", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(directory+"/keep.txt", []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	if err := WriteDirectory(directory, result, &log); err != nil {
		t.Fatal(err)
	}
	directoryEntries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]string, len(directoryEntries))
	for index, entry := range directoryEntries {
		entries[index] = entry.Name()
	}
	sort.Strings(entries)
	wantNames := []string{"keep.txt", result.Traces[0].Fingerprint + ".ndjson"}
	sort.Strings(wantNames)
	if !reflect.DeepEqual(entries, wantNames) {
		t.Fatalf("directory entries = %v, want %v", entries, wantNames)
	}
	if !strings.Contains(log.String(), "keep ") || !strings.Contains(log.String(), "drop ") {
		t.Fatalf("log = %q", log.String())
	}
}
