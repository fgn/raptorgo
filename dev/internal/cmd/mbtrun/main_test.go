package main

import (
	"os"
	"path/filepath"
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

func commandStep(action, state, result, event string) schedule.Step {
	return schedule.Step{
		Action: action,
		Expect: schedule.Expect{
			State: state, Result: result, Events: []string{event}, CompletedPresent: true,
		},
		ModelEdges: []schedule.ModelEdge{{Src: "src", Action: action, Dst: "dst"}},
	}
}
