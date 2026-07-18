package schedule

import (
	"os"
	"reflect"
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
	for _, named := range first {
		if seenNames[named.Name] {
			t.Fatalf("duplicate name %s", named.Name)
		}
		seenNames[named.Name] = true
		if _, err := Marshal(named.Schedule); err != nil {
			t.Fatal(err)
		}
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
				steps[0].Callbacks.DeliverReentrantAdds != test.deliverR {
				t.Fatalf("step = %#v", steps)
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
