package tracereplay

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

func TestTLCPositiveReplaySlice(t *testing.T) {
	if os.Getenv("RAPTORGO_TLC_TESTS") != "1" {
		t.Skip("set RAPTORGO_TLC_TESTS=1 with TLC_JAVA pointing to Java 17+")
	}
	root, err := FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	corpus := t.TempDir()
	writeEvents(t, filepath.Join(corpus, "construct-delivered.ndjson"), constructEvent("ok", "delivered", 1, 1))
	writeEvents(t, filepath.Join(corpus, "construct-verification-failed.ndjson"), constructEvent("verification_error", "verification-failed", 1, 0))
	writeEvents(t, filepath.Join(corpus, "construct-delivery-failed.ndjson"), constructEvent("delivery_error", "delivery-failed", 1, 1))
	writeEvents(t, filepath.Join(corpus, "receiving-rejections.ndjson"), receivingRejections())
	writeEvents(t, filepath.Join(corpus, "reentrant-decoded.ndjson"), reentrantDecoded())
	writeEvents(t, filepath.Join(corpus, "reentrant-verified.ndjson"), reentrantVerified())

	report, err := Run(context.Background(), Config{Root: root, Corpus: corpus, Jobs: 2})
	if err != nil {
		for _, failure := range report.Failures {
			t.Logf("%s: %v\n%s", failure.Input, failure.Err, failure.Output)
		}
		t.Fatal(err)
	}
	if report.Passed != 6 {
		t.Fatalf("passed = %d, want 6", report.Passed)
	}
}

func TestTLCRejectsMismatchedRejectAndTransientTerminalAdd(t *testing.T) {
	if os.Getenv("RAPTORGO_TLC_TESTS") != "1" {
		t.Skip("set RAPTORGO_TLC_TESTS=1 with TLC_JAVA pointing to Java 17+")
	}
	root, err := FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	corpus := t.TempDir()
	blocks := int32(1)
	writeEvents(t, filepath.Join(corpus, "mismatched-reject.ndjson"), []objtrace.Event{
		event("init", "ok", "receiving", 0, 0, 0, &blocks),
		event("add_reject_invalid", "conflict", "receiving", 0, 0, 0, nil),
	})
	writeEvents(t, filepath.Join(corpus, "early-verified-terminal.ndjson"), []objtrace.Event{
		event("init", "ok", "receiving", 0, 0, 0, &blocks),
		event("verify_enter", "ok", "decoded", 1, 1, 0, nil),
		event("verified", "ok", "verified", 1, 1, 0, nil),
		event("terminal_add", "terminal", "verified", 1, 1, 0, nil),
	})
	zeroBlocks := int32(0)
	writeEvents(t, filepath.Join(corpus, "zero-block-init.ndjson"), []objtrace.Event{
		event("init", "ok", "receiving", 0, 0, 0, &zeroBlocks),
	})
	writeEvents(t, filepath.Join(corpus, "mismatched-construct.ndjson"), []objtrace.Event{
		event("construct_final", "ok", "delivery-failed", 0, 1, 1, &zeroBlocks),
	})

	report, err := Run(context.Background(), Config{Root: root, Corpus: corpus, Jobs: 2})
	var failures *FailuresError
	if !errors.As(err, &failures) {
		t.Fatalf("error = %v, want FailuresError", err)
	}
	if len(report.Failures) != 4 {
		t.Fatalf("failures = %d, want 4", len(report.Failures))
	}
	for _, failure := range report.Failures {
		want := "Error: Temporal property TraceComplete was violated."
		if failure.Input == "mismatched-construct.ndjson" || failure.Input == "zero-block-init.ndjson" {
			want = "Finished computing initial states: 0 distinct states generated"
			if !strings.Contains(failure.Err.Error(), "did not generate exactly one initial state") {
				t.Fatalf("%s runner error = %v", failure.Input, failure.Err)
			}
		}
		if !strings.Contains(string(failure.Output), want) {
			t.Fatalf("%s failed unexpectedly:\n%s", failure.Input, failure.Output)
		}
	}
}

func constructEvent(result, state string, verification, delivery uint32) []objtrace.Event {
	blocks := int32(0)
	return []objtrace.Event{event("construct_final", result, state, 0, verification, delivery, &blocks)}
}

func receivingRejections() []objtrace.Event {
	blocks := int32(1)
	return []objtrace.Event{
		event("init", "ok", "receiving", 0, 0, 0, &blocks),
		event("add_reject_invalid", "invalid", "receiving", 0, 0, 0, nil),
		event("add_reject_limit", "limit", "receiving", 0, 0, 0, nil),
		event("add_reject_conflict", "conflict", "receiving", 0, 0, 0, nil),
	}
}

func reentrantDecoded() []objtrace.Event {
	blocks := int32(1)
	return []objtrace.Event{
		event("init", "ok", "receiving", 0, 0, 0, &blocks),
		event("verify_enter", "ok", "decoded", 1, 1, 0, nil),
		event("terminal_add", "terminal", "decoded", 1, 1, 0, nil),
		event("add_return", "verification_error", "verification-failed", 1, 1, 0, nil),
	}
}

func reentrantVerified() []objtrace.Event {
	blocks := int32(1)
	return []objtrace.Event{
		event("init", "ok", "receiving", 0, 0, 0, &blocks),
		event("verify_enter", "ok", "decoded", 1, 1, 0, nil),
		event("verified", "ok", "verified", 1, 1, 0, nil),
		event("deliver_enter", "ok", "verified", 1, 1, 1, nil),
		event("terminal_add", "terminal", "verified", 1, 1, 1, nil),
		event("add_return", "ok", "delivered", 1, 1, 1, nil),
	}
}

func event(
	op, result, state string,
	completed, verification, delivery uint32,
	blocks *int32,
) objtrace.Event {
	return objtrace.Event{
		V: objtrace.Version, Op: op, Result: result, State: state,
		Completed: completed, Verification: verification, Delivery: delivery,
		BindingOK: true, Blocks: blocks,
	}
}

func writeEvents(t *testing.T, path string, events []objtrace.Event) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := objtrace.WriteNDJSON(file, events); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
