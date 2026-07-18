package schedule

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/fgn/raptorgo/dev/internal/mbt/dotparse"
)

func loadGraph(t *testing.T) dotparse.Graph {
	t.Helper()
	file, err := os.Open("../dotparse/testdata/object-decoder-model-a.dot")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	graph, err := dotparse.Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

func TestSelectPathsCoversEveryEdgeDeterministically(t *testing.T) {
	graph := loadGraph(t)
	first, err := SelectPaths(graph)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SelectPaths(graph)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("path selection is nondeterministic")
	}
	covered := make(map[dotparse.Edge]bool)
	for _, path := range first {
		if len(path) > DepthCap {
			t.Fatalf("path has %d edges", len(path))
		}
		for _, edge := range path {
			covered[edge] = true
		}
		last := graph.States[path[len(path)-1].Dst]["state"].String
		if last == "decoded" || last == "verified" {
			t.Fatalf("path ends in transient state %q", last)
		}
	}
	if len(covered) != len(graph.Edges) {
		t.Fatalf("covered %d of %d edges", len(covered), len(graph.Edges))
	}
	if len(first) >= len(graph.Edges) {
		t.Fatalf("expected path compounding, got %d paths for %d edges", len(first), len(graph.Edges))
	}
}

func TestBuildAndMarshalAreDeterministic(t *testing.T) {
	graph := loadGraph(t)
	first, err := Build(graph, "A", "two-block")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(graph, "A", "two-block")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("schedule build is nondeterministic")
	}
	seenNames := make(map[string]bool)
	covered := CoveredEdges(first)
	if len(covered) != len(graph.Edges) {
		t.Fatalf("persisted coverage has %d edges, want %d", len(covered), len(graph.Edges))
	}
	wantEdges := make([]string, len(graph.Edges))
	for index, edge := range graph.Edges {
		wantEdges[index] = EdgeKey(modelEdge(edge))
	}
	sort.Strings(wantEdges)
	if !reflect.DeepEqual(covered, wantEdges) {
		t.Fatal("persisted schedules do not retain the exact graph edge set")
	}
	for _, named := range first {
		if seenNames[named.Name] {
			t.Fatalf("duplicate name %s", named.Name)
		}
		seenNames[named.Name] = true
		encoded, err := Marshal(named.Schedule)
		if err != nil {
			t.Fatal(err)
		}
		var roundTrip Schedule
		if err := json.Unmarshal(encoded, &roundTrip); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(named.Schedule, roundTrip) {
			t.Fatalf("schedule round trip mismatch\nwant=%#v\ngot=%#v", named.Schedule, roundTrip)
		}
	}
}

func TestBuildAssignsTruthfulConfigBFixtures(t *testing.T) {
	file, err := os.Open("../dotparse/testdata/object-decoder-model-b.dot")
	if err != nil {
		t.Fatal(err)
	}
	graph, err := dotparse.Parse(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	schedules, err := Build(graph, "B", "one-block")
	if err != nil {
		t.Fatal(err)
	}
	foundDecode, foundSuccess := false, false
	for _, named := range schedules {
		hasDecode := false
		for _, step := range named.Schedule.Steps {
			hasDecode = hasDecode || step.Action == "DecodeFail"
		}
		if hasDecode {
			foundDecode = true
			if named.Schedule.Fixture != "rank-deficient" {
				t.Fatalf("decode schedule fixture = %q", named.Schedule.Fixture)
			}
		} else {
			foundSuccess = true
			if named.Schedule.Fixture != "one-block" {
				t.Fatalf("non-decode schedule fixture = %q", named.Schedule.Fixture)
			}
		}
	}
	if !foundDecode || !foundSuccess {
		t.Fatalf("decode=%v success=%v", foundDecode, foundSuccess)
	}
}

func TestSelectPathsRejectsEmptyGraph(t *testing.T) {
	graph := dotparse.Graph{
		States:   map[string]dotparse.State{"0": state("receiving", 0, 0, 0)},
		StateIDs: []string{"0"}, Inits: []string{"0"},
	}
	if _, err := SelectPaths(graph); err == nil {
		t.Fatal("empty graph unexpectedly generated schedules")
	}
}

func TestFoldAllFinalizationShapes(t *testing.T) {
	states := map[string]dotparse.State{
		"decoded":         state("decoded", 2, 1, 0),
		"verified0":       state("verified", 2, 1, 0),
		"verified1":       state("verified", 2, 1, 1),
		"delivered":       state("delivered", 2, 1, 1),
		"verify-failed":   state("verification-failed", 2, 1, 0),
		"delivery-failed": state("delivery-failed", 2, 1, 1),
	}
	tests := []struct {
		name     string
		path     []dotparse.Edge
		result   string
		events   []string
		verifyR  int
		deliverR int
	}{
		{"delivered", edges("FinalizeVerifyEnter(1,1):decoded", "Verified:verified0", "DeliverEnter:verified1", "Delivered:delivered"), "ok", []string{"verify_enter", "verified", "deliver_enter", "add_return"}, 0, 0},
		{"verify-fail", edges("FinalizeVerifyEnter(1,1):decoded", "VerifyFailed:verify-failed"), "verification_error", []string{"verify_enter", "add_return"}, 0, 0},
		{"delivery-fail", edges("FinalizeVerifyEnter(1,1):decoded", "Verified:verified0", "DeliverEnter:verified1", "DeliveryFailed:delivery-failed"), "delivery_error", []string{"verify_enter", "verified", "deliver_enter", "add_return"}, 0, 0},
		{"verify-reentrant", edges("FinalizeVerifyEnter(1,1):decoded", "TerminalAdd:decoded", "Verified:verified0", "DeliverEnter:verified1", "Delivered:delivered"), "ok", []string{"verify_enter", "terminal_add", "verified", "deliver_enter", "add_return"}, 1, 0},
		{"deliver-reentrant", edges("FinalizeVerifyEnter(1,1):decoded", "Verified:verified0", "DeliverEnter:verified1", "TerminalAdd:verified1", "DeliveryFailed:delivery-failed"), "delivery_error", []string{"verify_enter", "verified", "deliver_enter", "terminal_add", "add_return"}, 0, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			steps, err := Fold(test.path, states)
			if err != nil {
				t.Fatal(err)
			}
			if len(steps) != 1 || steps[0].Expect.Result != test.result ||
				!reflect.DeepEqual(steps[0].Expect.Events, test.events) ||
				steps[0].Callbacks.VerifyReentrantAdds != test.verifyR ||
				steps[0].Callbacks.DeliverReentrantAdds != test.deliverR ||
				steps[0].Expect.State != states[test.path[len(test.path)-1].Dst]["state"].String ||
				steps[0].Expect.Completed != 2 || !steps[0].Expect.CompletedPresent ||
				len(steps[0].ModelEdges) != len(test.path) {
				t.Fatalf("step = %#v", steps)
			}
			wantVerify, wantDeliver := "ok", "ok"
			if test.result == "verification_error" {
				wantVerify, wantDeliver = "fail", ""
			} else if test.result == "delivery_error" {
				wantDeliver = "fail"
			}
			if steps[0].Callbacks.Verify != wantVerify || steps[0].Callbacks.Deliver != wantDeliver {
				t.Fatalf("callbacks = %#v, want verify=%s deliver=%s", steps[0].Callbacks, wantVerify, wantDeliver)
			}
		})
	}
}

func TestFoldRejectsTransientPrefix(t *testing.T) {
	states := map[string]dotparse.State{"decoded": state("decoded", 2, 1, 0)}
	if _, err := Fold(edges("FinalizeVerifyEnter(1,1):decoded"), states); err == nil {
		t.Fatal("transient prefix unexpectedly folded")
	}
}

func TestMinimumCompletionRejectsUnknownPhase(t *testing.T) {
	graph := dotparse.Graph{States: map[string]dotparse.State{"bad": state("decodedd", 0, 0, 0)}}
	if _, err := minimumCompletion("bad", graph, nil); err == nil {
		t.Fatal("unknown phase unexpectedly accepted as an API boundary")
	}
}

func TestOrdinaryStepRejectsESIOverflow(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("overflow label does not fit host int")
	}
	states := map[string]dotparse.State{"dst": state("receiving", 0, 0, 0)}
	edge := dotparse.Edge{Src: "src", Dst: "dst", Action: "AddWait(0,4294967296)"}
	name, parameters, err := ParseAction(edge.Action)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ordinaryStep(edge, name, parameters, states); err == nil {
		t.Fatal("ESI overflow unexpectedly accepted")
	}
}

func state(phase string, completed, verification, delivery int64) dotparse.State {
	return dotparse.State{
		"state":        {Kind: dotparse.String, String: phase},
		"completed":    {Kind: dotparse.Integer, Int: completed},
		"verification": {Kind: dotparse.Integer, Int: verification},
		"delivery":     {Kind: dotparse.Integer, Int: delivery},
	}
}

func edges(specifications ...string) []dotparse.Edge {
	result := make([]dotparse.Edge, 0, len(specifications))
	source := "receiving"
	for _, specification := range specifications {
		parts := strings.SplitN(specification, ":", 2)
		result = append(result, dotparse.Edge{Src: source, Dst: parts[1], Action: parts[0]})
		source = parts[1]
	}
	return result
}
