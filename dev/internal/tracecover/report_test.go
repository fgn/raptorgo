package tracecover

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

func int32Pointer(value int32) *int32 { return &value }

func initEvent(blocks int32) objtrace.Event {
	return objtrace.Event{
		V: objtrace.Version, Op: "init", Result: "ok", State: "receiving",
		BindingOK: true, Blocks: int32Pointer(blocks),
	}
}

func actionCoverage(report Report, name string) ActionCoverage {
	for _, action := range report.Actions {
		if action.Name == name {
			return action
		}
	}
	panic("missing action " + name)
}

func TestAnalyzeSkipsInitByOperationAndClassifiesConstructAtIndexZero(t *testing.T) {
	traces := []Trace{
		{
			Name: "ordinary", Source: SourceCorpus,
			Events: []objtrace.Event{
				initEvent(1),
				{V: 1, Op: "add_wait", Result: "ok", State: "receiving", BindingOK: true},
			},
		},
		{
			Name: "construct", Source: SourceWitness,
			Events: []objtrace.Event{{
				V: 1, Op: "construct_final", Result: "ok", State: "delivered",
				Verification: 1, Delivery: 1, BindingOK: true, Blocks: int32Pointer(0),
			}},
		},
	}
	report, err := Analyze(validManifest(), traces)
	if err != nil {
		t.Fatal(err)
	}
	if report.Events != 2 || report.Traces != 2 {
		t.Fatalf("report events=%d traces=%d", report.Events, report.Traces)
	}
	addWait := actionCoverage(report, "AddWait")
	if addWait.Count != 1 || addWait.Corpus != 1 || addWait.Witness != 0 {
		t.Fatalf("AddWait coverage = %#v", addWait)
	}
	construct := actionCoverage(report, "ConstructDelivered")
	if construct.Count != 1 || construct.Corpus != 0 || construct.Witness != 1 {
		t.Fatalf("construct coverage = %#v", construct)
	}
	if report.Covered != 2 || report.Corpus.Covered != 1 || report.Witness.Covered != 1 {
		t.Fatalf("covered totals = %#v", report)
	}
}

func TestAnalyzeRejectsWrongCompletedDelta(t *testing.T) {
	trace := Trace{
		Name: "bad-delta", Source: SourceCorpus,
		Events: []objtrace.Event{
			initEvent(1),
			{V: 1, Op: "verify_enter", Result: "ok", State: "decoded", Verification: 1, BindingOK: true},
		},
	}
	if _, err := Analyze(validManifest(), []Trace{trace}); err == nil || !strings.Contains(err.Error(), "matches no action") {
		t.Fatalf("wrong delta error = %v", err)
	}
}

func TestReportRenderingIsDeterministic(t *testing.T) {
	traces := []Trace{
		{
			Name: "ordinary", Source: SourceCorpus,
			Events: []objtrace.Event{
				initEvent(1),
				{V: 1, Op: "add_wait", Result: "ok", State: "receiving", BindingOK: true},
			},
		},
		{
			Name: "construct", Source: SourceWitness,
			Events: []objtrace.Event{{
				V: 1, Op: "construct_final", Result: "delivery_error", State: "delivery-failed",
				Verification: 1, Delivery: 1, BindingOK: true, Blocks: int32Pointer(0),
			}},
		},
	}
	forward, err := Analyze(validManifest(), traces)
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := Analyze(validManifest(), []Trace{traces[1], traces[0]})
	if err != nil {
		t.Fatal(err)
	}
	forwardJSON, err := forward.RenderJSON()
	if err != nil {
		t.Fatal(err)
	}
	reversedJSON, err := reversed.RenderJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(forwardJSON, reversedJSON) {
		t.Fatalf("JSON depends on input order:\n%s\n---\n%s", forwardJSON, reversedJSON)
	}
	if !bytes.Equal(forward.RenderHuman(), reversed.RenderHuman()) {
		t.Fatalf("human report depends on input order:\n%s\n---\n%s", forward.RenderHuman(), reversed.RenderHuman())
	}
	if !bytes.Contains(forwardJSON, []byte(`"name": "ConstructDeliveryFailed"`)) ||
		!bytes.Contains(forward.RenderHuman(), []byte("covered=2/16")) {
		t.Fatalf("rendered report missing expected coverage:\n%s\n%s", forwardJSON, forward.RenderHuman())
	}
}

func TestReportRetainsFirstThreeSortedUniqueFingerprints(t *testing.T) {
	traces := make([]Trace, 0, 4)
	for blocks := int32(1); blocks <= 4; blocks++ {
		traces = append(traces, Trace{
			Name: "add-wait", Source: SourceCorpus,
			Events: []objtrace.Event{
				initEvent(blocks),
				{V: 1, Op: "add_wait", Result: "ok", State: "receiving", BindingOK: true},
			},
		})
	}
	report, err := Analyze(validManifest(), traces)
	if err != nil {
		t.Fatal(err)
	}
	fingerprints := actionCoverage(report, "AddWait").Fingerprints
	if len(fingerprints) != 3 {
		t.Fatalf("fingerprints = %v, want first 3", fingerprints)
	}
	if !slices.IsSorted(fingerprints) {
		t.Fatalf("fingerprints are not sorted: %v", fingerprints)
	}
}

func TestEmptyCorpusAndSeparateFullCoverageGatesFailClosed(t *testing.T) {
	if _, err := Analyze(validManifest(), nil); err == nil || !strings.Contains(err.Error(), "empty trace corpus") {
		t.Fatalf("empty analyze error = %v", err)
	}
	report, err := Analyze(validManifest(), []Trace{{
		Name: "one", Source: SourceCorpus,
		Events: []objtrace.Event{
			initEvent(1),
			{V: 1, Op: "add_wait", Result: "ok", State: "receiving", BindingOK: true},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []Scope{ScopeAll, ScopeCorpus, ScopeWitness} {
		if err := report.RequireFull(scope); err == nil {
			t.Fatalf("scope %s unexpectedly passed", scope)
		}
	}
}
