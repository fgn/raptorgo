package dotparse

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParsePinnedFixture(t *testing.T) {
	encoded, err := os.ReadFile("testdata/object-decoder-model-a.dot")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	if got, want := hex.EncodeToString(digest[:]), "d6c7ca7975b70bf43c614e2c1e772ecca56b1d10a4dde625c3fd8c4dd83e6df5"; got != want {
		t.Fatalf("raw fixture digest = %s, want %s", got, want)
	}
	graph, err := Parse(strings.NewReader(string(encoded)))
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
	wantVariables := map[string]Kind{
		"accepted0": IntegerSet, "accepted1": IntegerSet, "state": String,
		"completed": Integer, "verification": Integer, "delivery": Integer,
	}
	for id, state := range graph.States {
		if len(state) != len(wantVariables) {
			t.Fatalf("state %s variables = %#v", id, state)
		}
		for name, kind := range wantVariables {
			if state[name].Kind != kind {
				t.Fatalf("state %s variable %s kind=%v, want %v", id, name, state[name].Kind, kind)
			}
		}
	}
	wantActions := map[string]int{
		"AddWait": 32, "BlockComplete": 12, "FinalizeVerifyEnter": 4,
		"Verified": 1, "VerifyFailed": 1, "DeliverEnter": 1, "Delivered": 1,
		"DeliveryFailed": 1, "RejectInvalid": 15, "RejectLimit": 15,
		"RejectConflict": 16, "TerminalAdd": 5,
	}
	gotActions := make(map[string]int)
	for _, edge := range graph.Edges {
		name := strings.SplitN(edge.Action, "(", 2)[0]
		gotActions[name]++
	}
	if !reflect.DeepEqual(gotActions, wantActions) {
		t.Fatalf("action counts = %v, want %v", gotActions, wantActions)
	}
}

func TestParseAcceptsQuotedAndUnquotedSignedIDs(t *testing.T) {
	input := `strict digraph DiskGraph {
node [shape=box,style=rounded]
nodesep=0.35;
subgraph cluster_graph {
color="white";
"-1" [label="/\\ state = \"receiving\"\n/\\ accepted0 = {}\n/\\ completed = 0",style = filled]
-1 -> 2 [label="AddWait(0,0)"];
2 [label="/\\ state = \"receiving\"\n/\\ accepted0 = {0}\n/\\ completed = 0"];
}
}`
	graph, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(graph.Inits, []string{"-1"}) || graph.Edges[0].Action != "AddWait(0,0)" {
		t.Fatalf("graph = %#v", graph)
	}
}

func TestParseRequiresCompleteEnvelopeAndExactAttributes(t *testing.T) {
	encoded, err := os.ReadFile("testdata/object-decoder-model-a.dot")
	if err != nil {
		t.Fatal(err)
	}
	lastBrace := strings.LastIndex(string(encoded), "}")
	truncated := string(encoded[:lastBrace])
	if _, err := Parse(strings.NewReader(truncated)); err == nil {
		t.Fatal("truncated graph unexpectedly parsed")
	}
	xlabel := `strict digraph DiskGraph {
subgraph cluster_graph {
1 [xlabel="/\\ state = \"receiving\""];
}
}`
	if _, err := Parse(strings.NewReader(xlabel)); err == nil || !strings.Contains(err.Error(), "missing label") {
		t.Fatalf("xlabel-only error = %v", err)
	}
	notStyle := `strict digraph DiskGraph {
subgraph cluster_graph {
1 [label="/\\ state = \"receiving\"",notstyle=filled];
}
}`
	if _, err := Parse(strings.NewReader(notStyle)); err == nil || !strings.Contains(err.Error(), "unsupported node attribute") {
		t.Fatalf("notstyle error = %v", err)
	}
}

func TestParseRejectsUnsupportedDOTSurface(t *testing.T) {
	encoded, err := os.ReadFile("testdata/object-decoder-model-a.dot")
	if err != nil {
		t.Fatal(err)
	}
	valid := string(encoded)
	tests := []struct {
		name, old, replacement string
	}{
		{"node defaults", "node [shape=box,style=rounded]", "node [shape=box,evil=true]"},
		{"node separation", "nodesep=0.35;", "nodesep=99;"},
		{"cluster color", `color="white";`, `color="red";`},
		{"rank grammar", "{rank = same; -7026174232243225298;}", "{rank = maybe; -7026174232243225298;}"},
		{"node attribute", ",style = filled]", ",style = filled,evil=true]"},
		{"edge attribute", `,color="black",fontcolor="black"]`, `,color="black",fontcolor="black",evil="true"]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := strings.Replace(valid, test.old, test.replacement, 1)
			if input == valid {
				t.Fatalf("fixture does not contain %q", test.old)
			}
			if _, err := Parse(strings.NewReader(input)); err == nil {
				t.Fatal("unsupported DOT syntax unexpectedly parsed")
			}
		})
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
