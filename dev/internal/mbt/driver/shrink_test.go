package driver

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/fgn/raptorgo/dev/internal/mbt/schedule"
	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

func TestRunReturnsStructuredStableFailure(t *testing.T) {
	value := schedule.Schedule{
		V: 1, Model: "ObjectDecoderModel", Cfg: "B", Fixture: "one-block", GraphDigest: testGraphDigest,
		Steps: []schedule.Step{manualStep("RejectInvalid", "delivered", "invalid", "add_reject_invalid")},
	}
	result, err := Run(value)
	var failure *RunFailure
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want RunFailure", err)
	}
	want := FailureSignature{
		Kind: FailureState, Action: "RejectInvalid", Expected: "delivered", Actual: "receiving",
	}
	if failure.Step != 0 || failure.Signature != want {
		t.Fatalf("failure = %#v, want step 0 signature %#v", failure, want)
	}
	got, ok := SignatureOf(err)
	if !ok || got != want {
		t.Fatalf("SignatureOf = %#v/%v, want %#v/true", got, ok, want)
	}
	if len(result.Events) != 2 || result.Events[1].Op != "add_reject_invalid" {
		t.Fatalf("partial trace = %#v", result.Events)
	}
}

func TestMinimizeFailureDeterministicSeededInjection(t *testing.T) {
	value := seededShrinkSchedule()
	const seed = 6333
	trigger := value.Steps[seed%len(value.Steps)].Action
	if trigger != "RejectInvalid" {
		t.Fatalf("seed selected %s", trigger)
	}
	target := FailureSignature{
		Kind: FailureState, Action: trigger,
		Expected: "seed-6333-expected", Actual: "seed-6333-actual",
	}
	other := FailureSignature{
		Kind: FailureEvents, Action: "RejectLimit", Expected: "other", Actual: "other-seed",
	}
	execute := func(candidate schedule.Schedule) (Result, error) {
		for index, step := range candidate.Steps {
			if step.Action == trigger {
				return Result{Events: seededPartialTrace()}, NewRunFailure(index, target, errors.New("seeded failure"))
			}
		}
		for index, step := range candidate.Steps {
			if step.Action == "RejectLimit" {
				return Result{}, NewRunFailure(index, other, errors.New("different seeded failure"))
			}
		}
		return Result{}, nil
	}

	first, err := MinimizeFailure(value, target, ShrinkOptions{Execute: execute})
	if err != nil {
		t.Fatal(err)
	}
	second, err := MinimizeFailure(value, target, ShrinkOptions{Execute: execute})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Schedule, second.Schedule) || first.Attempts != second.Attempts {
		t.Fatalf("nondeterministic shrink:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if len(first.Schedule.Steps) != 1 || first.Schedule.Steps[0].Action != trigger {
		t.Fatalf("minimized steps = %#v", first.Schedule.Steps)
	}
	if first.Failure == nil || first.Failure.Step != 0 || first.Signature != target || first.OriginalSteps != 4 {
		t.Fatalf("shrink result = %#v", first)
	}
	if err := ValidateSchedule(first.Schedule); err != nil {
		t.Fatalf("minimized schedule is not replayable: %v", err)
	}
}

func TestWriteFailureArtifactsDeterministicAndExplicitlyDiagnostic(t *testing.T) {
	value := seededShrinkSchedule()
	target := FailureSignature{
		Kind: FailureState, Action: "RejectInvalid", Expected: "expected", Actual: "actual",
	}
	execute := func(candidate schedule.Schedule) (Result, error) {
		for index, step := range candidate.Steps {
			if step.Action == "RejectInvalid" {
				return Result{Events: seededPartialTrace()}, NewRunFailure(index, target, errors.New("seeded"))
			}
		}
		return Result{}, nil
	}
	shrunk, err := MinimizeFailure(value, target, ShrinkOptions{Execute: execute})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	paths, err := WriteFailureArtifacts(directory, shrunk)
	if err != nil {
		t.Fatal(err)
	}
	if !paths.StrictSchedule || paths.Trace == "" {
		t.Fatalf("paths = %#v", paths)
	}
	firstFailure := readFile(t, paths.Failure)
	firstSchedule := readFile(t, paths.Schedule)
	firstTrace := readFile(t, paths.Trace)
	pathsAgain, err := WriteFailureArtifacts(directory, shrunk)
	if err != nil {
		t.Fatal(err)
	}
	if paths != pathsAgain || !bytes.Equal(firstFailure, readFile(t, paths.Failure)) ||
		!bytes.Equal(firstSchedule, readFile(t, paths.Schedule)) ||
		!bytes.Equal(firstTrace, readFile(t, paths.Trace)) {
		t.Fatal("failure artifacts are not byte-deterministic")
	}

	var metadata failureArtifact
	if err := json.Unmarshal(firstFailure, &metadata); err != nil {
		t.Fatal(err)
	}
	if !metadata.StrictSchedule || !metadata.TraceAvailable || metadata.Signature != target ||
		metadata.OriginalSteps != 4 || metadata.MinimizedSteps != 1 || metadata.FailureStep != 0 {
		t.Fatalf("metadata = %#v", metadata)
	}
	if _, err := ReadSchedule(bytes.NewReader(firstSchedule)); err != nil {
		t.Fatalf("minimized schedule is not a strict replay: %v", err)
	}
	events, err := objtrace.ReadNDJSON(bytes.NewReader(firstTrace))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, seededPartialTrace()) {
		t.Fatalf("trace = %#v", events)
	}
	withoutTrace := shrunk
	withoutTrace.Execution = Result{}
	noTracePaths, err := WriteFailureArtifacts(directory, withoutTrace)
	if err != nil {
		t.Fatal(err)
	}
	if noTracePaths.Trace != "" {
		t.Fatalf("unavailable trace path = %q", noTracePaths.Trace)
	}
	if _, err := os.Stat(paths.Trace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale trace stat error = %v", err)
	}
}

func seededShrinkSchedule() schedule.Schedule {
	return schedule.Schedule{
		V: 1, Model: "ObjectDecoderModel", Cfg: "B", Fixture: "one-block", GraphDigest: testGraphDigest,
		Steps: []schedule.Step{
			manualStep("RejectLimit", "receiving", "limit", "add_reject_limit"),
			manualStep("RejectInvalid", "receiving", "invalid", "add_reject_invalid"),
			manualStep("RejectLimit", "receiving", "limit", "add_reject_limit"),
			manualStep("RejectLimit", "receiving", "limit", "add_reject_limit"),
		},
	}
}

func seededPartialTrace() []objtrace.Event {
	blocks := int32(1)
	return []objtrace.Event{
		{
			V: objtrace.Version, Op: "init", Result: "ok", State: "receiving",
			BindingOK: true, Blocks: &blocks,
		},
		{
			V: objtrace.Version, Op: "add_reject_invalid", Result: "invalid", State: "receiving",
			BindingOK: true,
		},
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
