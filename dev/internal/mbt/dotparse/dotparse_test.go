package dotparse

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParsePinnedFixture(t *testing.T) {
	fixture, err := os.Open("testdata/object-decoder-model-a.dot")
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()
	graph, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.States) != 21 || len(graph.Edges) != 104 || len(graph.Inits) != 1 {
		t.Fatalf("graph states=%d edges=%d inits=%v", len(graph.States), len(graph.Edges), graph.Inits)
	}
	initial := graph.States[graph.Inits[0]]
	if initial["state"].String != "receiving" || initial["completed"].Int != 0 || len(initial["accepted0"].Set) != 0 {
		t.Fatalf("initial state = %#v", initial)
	}
	foundParameters, foundParallel := false, false
	for index, edge := range graph.Edges {
		if edge.Action == "FinalizeVerifyEnter(1,1)" {
			foundParameters = true
		}
		if index > 0 && edge.Src == graph.Edges[index-1].Src && edge.Dst == graph.Edges[index-1].Dst && edge.Action != graph.Edges[index-1].Action {
			foundParallel = true
		}
	}
	if !foundParameters || !foundParallel {
		t.Fatalf("parameterized=%v parallel=%v", foundParameters, foundParallel)
	}
}

func TestParseAcceptsQuotedAndUnquotedSignedIDs(t *testing.T) {
	input := `strict digraph DiskGraph {
"-1" [label="/\\ state = \"receiving\"\n/\\ accepted0 = {}\n/\\ completed = 0",style = filled]
-1 -> 2 [label="AddWait(0,0)"];
2 [label="/\\ state = \"receiving\"\n/\\ accepted0 = {0}\n/\\ completed = 0"];
}`
	graph, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(graph.Inits, []string{"-1"}) || graph.Edges[0].Action != "AddWait(0,0)" {
		t.Fatalf("graph = %#v", graph)
	}
}

func TestParseValueRestrictedGrammar(t *testing.T) {
	tests := []struct {
		input string
		kind  Kind
	}{
		{"-17", Integer},
		{`"decoded"`, String},
		{"{}", IntegerSet},
		{"{0, 2, 7}", IntegerSet},
		{"TRUE", Boolean},
		{"FALSE", Boolean},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			value, err := ParseValue(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if value.Kind != test.kind {
				t.Fatalf("kind = %v, want %v", value.Kind, test.kind)
			}
		})
	}
	for _, input := range []string{"[a |-> 1]", "<<1>>", "0 :> 1", "{2, 1}", "1 trailing"} {
		t.Run("reject-"+input, func(t *testing.T) {
			if _, err := ParseValue(input); err == nil {
				t.Fatalf("ParseValue(%q) unexpectedly passed", input)
			}
		})
	}
}

func TestParseRejectsMalformedGraph(t *testing.T) {
	for _, input := range []string{
		`strict digraph DiskGraph {` + "\n" + `1 [label="/\\ state = <<1>>",style = filled]` + "\n}",
		`strict digraph DiskGraph {` + "\n" + `1 -> 2 [label="Bad.Action"];` + "\n}",
		`strict digraph DiskGraph {` + "\n" + `1 [label="/\\ state = \"receiving\"",style = filled]` + "\n" + `1 [label="/\\ state = \"receiving\""];` + "\n}",
	} {
		if _, err := Parse(strings.NewReader(input)); err == nil {
			t.Fatalf("malformed graph unexpectedly parsed:\n%s", input)
		}
	}
}
