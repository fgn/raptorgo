package tracecover

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func validManifest() Manifest {
	return Manifest{V: ManifestVersion, Actions: ExpectedActions()}
}

func parseTestManifest(t *testing.T, manifest Manifest) error {
	t.Helper()
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParseManifest(bytes.NewReader(encoded))
	return err
}

func TestCheckedInManifestMatchesGoldenAlphabetBidirectionally(t *testing.T) {
	manifest, err := LoadManifest(filepath.Join("..", "..", "formal", "coverage", "actions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Actions) != ExpectedActionCount {
		t.Fatalf("action count = %d, want %d", len(manifest.Actions), ExpectedActionCount)
	}
	want := ExpectedActions()
	for index, action := range manifest.Actions {
		if action != want[index] {
			t.Fatalf("action %d = %#v, want %#v", index, action, want[index])
		}
	}
}

func TestManifestRejectsBogusMissingDuplicateAndAmbiguousActions(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*Manifest)
		contains string
	}{
		{
			name: "bogus",
			mutate: func(manifest *Manifest) {
				manifest.Actions = append(manifest.Actions, Action{
					Name: "Bogus", Op: "add_wait", Result: "conflict", CompletedDelta: 0,
				})
			},
			contains: "unexpected action",
		},
		{
			name: "missing semantic branch",
			mutate: func(manifest *Manifest) {
				manifest.Actions = manifest.Actions[:len(manifest.Actions)-1]
			},
			contains: "missing expected action",
		},
		{
			name: "duplicate name",
			mutate: func(manifest *Manifest) {
				manifest.Actions = append(manifest.Actions, manifest.Actions[0])
			},
			contains: "duplicate action name",
		},
		{
			name: "ambiguous predicate",
			mutate: func(manifest *Manifest) {
				duplicate := manifest.Actions[0]
				duplicate.Name = "Ambiguous"
				manifest.Actions = append(manifest.Actions, duplicate)
			},
			contains: "ambiguous predicate",
		},
		{
			name: "changed expected predicate",
			mutate: func(manifest *Manifest) {
				manifest.Actions[2].CompletedDelta = 0
			},
			contains: "predicate is",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := validManifest()
			test.mutate(&manifest)
			err := parseTestManifest(t, manifest)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("error = %v, want substring %q", err, test.contains)
			}
		})
	}
}

func TestManifestDecoderIsStrict(t *testing.T) {
	tests := []struct {
		name, input, contains string
	}{
		{"unknown field", `{"v":1,"actions":[],"extra":true}`, "unknown field"},
		{"duplicate top-level field", `{"v":999,"v":1,"actions":[]}`, "duplicate object name"},
		{
			"duplicate nested field",
			`{"v":1,"actions":[{"name":"Bogus","name":"AddWait","op":"add_wait","result":"ok","completedDelta":0}]}`,
			"duplicate object name",
		},
		{
			"missing completed delta",
			`{"v":1,"actions":[{"name":"AddWait","op":"add_wait","result":"ok"}]}`,
			"has no completedDelta field",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseManifest(strings.NewReader(test.input)); err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("strict decode error = %v", err)
			}
		})
	}
}

func TestManifestDecoderBoundsInput(t *testing.T) {
	input := strings.NewReader(`{"v":1,"padding":"` + strings.Repeat("x", maxManifestBytes) + `"}`)
	if _, err := ParseManifest(input); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize manifest error = %v", err)
	}
}
