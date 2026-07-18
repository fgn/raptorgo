package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgn/raptorgo/dev/internal/mbt/driver"
	"github.com/fgn/raptorgo/dev/internal/mbt/schedule"
)

func TestWriteFailureArtifactsUsesProductionShrinker(t *testing.T) {
	value := schedule.Schedule{
		V: 1, Model: "ObjectDecoderModel", Cfg: "B", Fixture: "one-block",
		GraphDigest: "0000000000000000000000000000000000000000000000000000000000000000",
		Steps: []schedule.Step{
			commandStep("RejectLimit", "receiving", "limit", "add_reject_limit"),
			commandStep("RejectInvalid", "delivered", "invalid", "add_reject_invalid"),
		},
	}
	_, runErr := driver.Run(value)
	if _, ok := driver.SignatureOf(runErr); !ok {
		t.Fatalf("run error = %v, want structured signature", runErr)
	}
	root := t.TempDir()
	if err := writeFailureArtifacts(root, "/input/007-seeded.json", value, runErr); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "B-007-seeded")
	for _, name := range []string{"failure.json", "minimized.json", "trace.ndjson"} {
		if info, err := os.Stat(filepath.Join(directory, name)); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("artifact %s: info=%v error=%v", name, info, err)
		}
	}
}

func TestSchedulePathsBoundsTreeAndRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "one.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "two.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := schedulePathsLimit(root, 2); err == nil || !strings.Contains(err.Error(), "more than 2") {
		t.Fatalf("tree ceiling error = %v", err)
	}

	target := filepath.Join(root, "one.json")
	link := filepath.Join(t.TempDir(), "linked.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := schedulePaths(link); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink error = %v", err)
	}
}

func commandStep(action, state, result, event string) schedule.Step {
	return schedule.Step{
		Action: action,
		Expect: schedule.Expect{
			State: state, Result: result, Events: []string{event}, CompletedPresent: true,
		},
		ModelEdges: []schedule.ModelEdge{{Src: "src", Action: action, Dst: "dst"}},
	}
}
