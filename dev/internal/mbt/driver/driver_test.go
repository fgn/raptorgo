package driver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fgn/raptorgo/dev/internal/mbt/schedule"
)

func TestRepresentativeSchedules(t *testing.T) {
	tests := []schedule.Schedule{
		{
			V: 1, Model: "ObjectDecoderModel", Cfg: "A", Fixture: "two-block", GraphDigest: testGraphDigest,
			Steps: []schedule.Step{
				packetStep("AddWait", 0, 0, "receiving", 0, "ok", "add_wait"),
				packetStep("AddWait", 1, 0, "receiving", 0, "ok", "add_wait"),
				packetStep("BlockComplete", 0, 1, "receiving", 1, "ok", "block_complete"),
				finalStep(1, 1, schedule.Callbacks{Verify: "ok", Deliver: "ok", VerifyReentrantAdds: 1, DeliverReentrantAdds: 1}, "delivered", "ok",
					"verify_enter", "terminal_add", "verified", "deliver_enter", "terminal_add", "add_return"),
			},
		},
		{
			V: 1, Model: "ObjectDecoderModel", Cfg: "A", Fixture: "two-block", GraphDigest: testGraphDigest,
			Steps: []schedule.Step{
				packetStep("AddWait", 0, 0, "receiving", 0, "ok", "add_wait"),
				packetStep("AddWait", 1, 0, "receiving", 0, "ok", "add_wait"),
				packetStep("BlockComplete", 0, 1, "receiving", 1, "ok", "block_complete"),
				finalStep(1, 1, schedule.Callbacks{Verify: "fail", VerifyReentrantAdds: 1}, "verification-failed", "verification_error",
					"verify_enter", "terminal_add", "add_return"),
			},
		},
		{
			V: 1, Model: "ObjectDecoderModel", Cfg: "B", Fixture: "one-block", GraphDigest: testGraphDigest,
			Steps: []schedule.Step{
				manualStep("RejectInvalid", "receiving", "invalid", "add_reject_invalid"),
				manualStep("RejectLimit", "receiving", "limit", "add_reject_limit"),
			},
		},
		{
			V: 1, Model: "ObjectDecoderModel", Cfg: "B", Fixture: "rank-deficient", GraphDigest: testGraphDigest,
			Steps: []schedule.Step{
				manualStep("DecodeFail", "decode-failed", "decode_error", "decode_failure"),
				manualStep("TerminalAdd", "decode-failed", "terminal", "terminal_add"),
			},
		},
	}
	for index, test := range tests {
		if _, err := Run(test); err != nil {
			t.Fatalf("schedule %d: %v", index, err)
		}
	}
}

func TestZeroLengthSchedules(t *testing.T) {
	tests := []struct {
		action, state, result string
	}{
		{"ConstructDelivered", "delivered", "ok"},
		{"ConstructVerifyFailed", "verification-failed", "verification_error"},
		{"ConstructDeliveryFailed", "delivery-failed", "delivery_error"},
	}
	for _, test := range tests {
		value := schedule.Schedule{
			V: 1, Model: "ObjectDecoderModel", Cfg: "construct", Fixture: "zero-length", GraphDigest: testGraphDigest,
			Steps: []schedule.Step{{
				Action: test.action,
				Expect: schedule.Expect{
					State: test.state, Result: test.result, Events: []string{"construct_final"}, CompletedPresent: true,
				},
				ModelEdges: []schedule.ModelEdge{{Src: "manual", Action: test.action, Dst: test.state}},
			}},
		}
		result, err := Run(value)
		if err != nil {
			t.Fatalf("%s: %v", test.action, err)
		}
		if len(result.Events) != 1 || result.Events[0].Blocks == nil || *result.Events[0].Blocks != 0 {
			t.Fatalf("%s events = %#v", test.action, result.Events)
		}
	}
}

func TestCommittedSchedules(t *testing.T) {
	paths, err := filepath.Glob("../../../formal/mbt/schedules/*/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Skip("committed schedules are generated in the next implementation slice")
	}
	for _, path := range paths {
		value, err := ReadScheduleFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if _, err := Run(value); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
}

func TestReadScheduleRequiresExplicitCompleted(t *testing.T) {
	input := `{"v":1,"model":"ObjectDecoderModel","cfg":"B","fixture":"one-block","graphDigest":"` + testGraphDigest + `","steps":[{"action":"RejectInvalid","expect":{"state":"receiving","result":"invalid","events":["add_reject_invalid"]},"modelEdges":[{"src":"a","action":"RejectInvalid","dst":"a"}]}]}`
	if _, err := ReadSchedule(strings.NewReader(input)); err == nil || !strings.Contains(err.Error(), "expect.completed is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateScheduleBindsDecodeFixture(t *testing.T) {
	value := schedule.Schedule{
		V: 1, Model: "ObjectDecoderModel", Cfg: "B", Fixture: "one-block", GraphDigest: testGraphDigest,
		Steps: []schedule.Step{manualStep("DecodeFail", "decode-failed", "decode_error", "decode_failure")},
	}
	if err := ValidateSchedule(value); err == nil || !strings.Contains(err.Error(), "must coincide") {
		t.Fatalf("error = %v", err)
	}
}

func packetStep(action string, sbn uint8, esi uint32, state string, completed uint32, result, event string) schedule.Step {
	return schedule.Step{
		Action: action, SBN: &sbn, ESI: &esi,
		Expect: schedule.Expect{
			State: state, Completed: completed, Result: result, Events: []string{event}, CompletedPresent: true,
		},
		ModelEdges: []schedule.ModelEdge{{Src: "test-src", Action: action, Dst: "test-dst"}},
	}
}

func finalStep(sbn uint8, esi uint32, callbacks schedule.Callbacks, state, result string, events ...string) schedule.Step {
	return schedule.Step{
		Action: "FinalizeVerifyEnter", SBN: &sbn, ESI: &esi, Callbacks: &callbacks,
		Expect: schedule.Expect{
			State: state, Completed: 2, Result: result, Events: events, CompletedPresent: true,
		},
		ModelEdges: []schedule.ModelEdge{{Src: "test-src", Action: "FinalizeVerifyEnter", Dst: "test-dst"}},
	}
}

func manualStep(action, state, result, event string) schedule.Step {
	return schedule.Step{
		Action: action,
		Expect: schedule.Expect{
			State: state, Result: result, Events: []string{event}, CompletedPresent: true,
		},
		ModelEdges: []schedule.ModelEdge{{Src: "test-src", Action: action, Dst: "test-dst"}},
	}
}

const testGraphDigest = "0000000000000000000000000000000000000000000000000000000000000000"
