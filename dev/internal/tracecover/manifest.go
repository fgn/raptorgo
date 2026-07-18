// Package tracecover classifies ObjectDecoder trace events against the exact
// action alphabet accepted by the formal replay specification.
package tracecover

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/fgn/raptorgo/dev/internal/strictjson"
)

const (
	// ManifestVersion is the only coverage-manifest version understood here.
	ManifestVersion = 1
	// ExpectedActionCount is deliberately pinned so deleting a semantic branch
	// from both a manifest and a report cannot silently reduce the denominator.
	ExpectedActionCount = 16
	maxManifestBytes    = 256 << 10
)

var (
	actionNamePattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	tokenPattern      = regexp.MustCompile(`^[a-z][a-z_]*$`)
)

// Action is one exact event predicate. CompletedDelta is the increase from the
// previous observation, treating the first observation as following completed
// count zero.
type Action struct {
	Name           string `json:"name"`
	Op             string `json:"op"`
	Result         string `json:"result"`
	CompletedDelta int32  `json:"completedDelta"`
}

// Manifest is the checked-in action alphabet.
type Manifest struct {
	V       int      `json:"v"`
	Actions []Action `json:"actions"`
}

var expectedActions = [...]Action{
	{Name: "AddWait", Op: "add_wait", Result: "ok", CompletedDelta: 0},
	{Name: "BlockComplete", Op: "block_complete", Result: "ok", CompletedDelta: 1},
	{Name: "VerifyEnter", Op: "verify_enter", Result: "ok", CompletedDelta: 1},
	{Name: "Verified", Op: "verified", Result: "ok", CompletedDelta: 0},
	{Name: "DeliverEnter", Op: "deliver_enter", Result: "ok", CompletedDelta: 0},
	{Name: "FinalizeDelivered", Op: "add_return", Result: "ok", CompletedDelta: 0},
	{Name: "FinalizeVerifyFailed", Op: "add_return", Result: "verification_error", CompletedDelta: 0},
	{Name: "FinalizeDeliveryFailed", Op: "add_return", Result: "delivery_error", CompletedDelta: 0},
	{Name: "DecodeFailure", Op: "decode_failure", Result: "decode_error", CompletedDelta: 0},
	{Name: "TerminalReject", Op: "terminal_add", Result: "terminal", CompletedDelta: 0},
	{Name: "RejectInvalid", Op: "add_reject_invalid", Result: "invalid", CompletedDelta: 0},
	{Name: "RejectLimit", Op: "add_reject_limit", Result: "limit", CompletedDelta: 0},
	{Name: "RejectConflict", Op: "add_reject_conflict", Result: "conflict", CompletedDelta: 0},
	{Name: "ConstructDelivered", Op: "construct_final", Result: "ok", CompletedDelta: 0},
	{Name: "ConstructVerifyFailed", Op: "construct_final", Result: "verification_error", CompletedDelta: 0},
	{Name: "ConstructDeliveryFailed", Op: "construct_final", Result: "delivery_error", CompletedDelta: 0},
}

// ExpectedActions returns a defensive copy of the golden action alphabet.
func ExpectedActions() []Action {
	result := make([]Action, len(expectedActions))
	copy(result, expectedActions[:])
	return result
}

// LoadManifest reads and strictly validates a manifest file.
func LoadManifest(path string) (Manifest, error) {
	input, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("tracecover: open manifest %s: %w", path, err)
	}
	defer input.Close()
	manifest, err := ParseManifest(input)
	if err != nil {
		return Manifest{}, fmt.Errorf("tracecover: manifest %s: %w", path, err)
	}
	return manifest, nil
}

// ParseManifest decodes one strict JSON manifest and validates it against the
// golden alphabet in both directions: every supplied action is expected, and
// every expected action is supplied with its exact predicate.
func ParseManifest(reader io.Reader) (Manifest, error) {
	type wireAction struct {
		Name           string `json:"name"`
		Op             string `json:"op"`
		Result         string `json:"result"`
		CompletedDelta *int32 `json:"completedDelta"`
	}
	type wireManifest struct {
		V       int          `json:"v"`
		Actions []wireAction `json:"actions"`
	}

	encoded, err := io.ReadAll(io.LimitReader(reader, maxManifestBytes+1))
	if err != nil {
		return Manifest{}, fmt.Errorf("read: %w", err)
	}
	if len(encoded) > maxManifestBytes {
		return Manifest{}, fmt.Errorf("manifest exceeds %d bytes", maxManifestBytes)
	}
	if err := strictjson.Check(encoded); err != nil {
		return Manifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var wire wireManifest
	if err := decoder.Decode(&wire); err != nil {
		return Manifest{}, fmt.Errorf("decode: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return Manifest{}, fmt.Errorf("trailing data: %w", err)
	}
	manifest := Manifest{V: wire.V, Actions: make([]Action, len(wire.Actions))}
	for index, action := range wire.Actions {
		if action.CompletedDelta == nil {
			return Manifest{}, fmt.Errorf("tracecover: action %d has no completedDelta field", index)
		}
		manifest.Actions[index] = Action{
			Name: action.Name, Op: action.Op, Result: action.Result,
			CompletedDelta: *action.CompletedDelta,
		}
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate rejects malformed, duplicate, ambiguous, missing, unexpected, or
// semantically changed actions.
func (manifest Manifest) Validate() error {
	if manifest.V != ManifestVersion {
		return fmt.Errorf("tracecover: unsupported manifest version %d", manifest.V)
	}
	if len(manifest.Actions) == 0 {
		return errors.New("tracecover: manifest has no actions")
	}

	byName := make(map[string]Action, len(manifest.Actions))
	byPredicate := make(map[string]string, len(manifest.Actions))
	for index, action := range manifest.Actions {
		if !actionNamePattern.MatchString(action.Name) {
			return fmt.Errorf("tracecover: action %d has invalid name %q", index, action.Name)
		}
		if !tokenPattern.MatchString(action.Op) {
			return fmt.Errorf("tracecover: action %s has invalid op %q", action.Name, action.Op)
		}
		if !tokenPattern.MatchString(action.Result) {
			return fmt.Errorf("tracecover: action %s has invalid result %q", action.Name, action.Result)
		}
		if action.CompletedDelta < 0 {
			return fmt.Errorf("tracecover: action %s has negative completed delta %d", action.Name, action.CompletedDelta)
		}
		if _, exists := byName[action.Name]; exists {
			return fmt.Errorf("tracecover: duplicate action name %q", action.Name)
		}
		key := predicateKey(action.Op, action.Result, action.CompletedDelta)
		if previous, exists := byPredicate[key]; exists {
			return fmt.Errorf(
				"tracecover: ambiguous predicate %s is shared by %s and %s",
				key, previous, action.Name,
			)
		}
		byName[action.Name] = action
		byPredicate[key] = action.Name
	}

	expectedByName := make(map[string]Action, len(expectedActions))
	for _, expected := range expectedActions {
		expectedByName[expected.Name] = expected
	}
	for _, supplied := range manifest.Actions {
		expected, exists := expectedByName[supplied.Name]
		if !exists {
			return fmt.Errorf("tracecover: unexpected action %q", supplied.Name)
		}
		if supplied != expected {
			return fmt.Errorf(
				"tracecover: action %s predicate is (%s,%s,%d), want (%s,%s,%d)",
				supplied.Name,
				supplied.Op, supplied.Result, supplied.CompletedDelta,
				expected.Op, expected.Result, expected.CompletedDelta,
			)
		}
	}
	for _, expected := range expectedActions {
		if _, exists := byName[expected.Name]; !exists {
			return fmt.Errorf("tracecover: missing expected action %q", expected.Name)
		}
	}
	if len(manifest.Actions) != ExpectedActionCount {
		return fmt.Errorf(
			"tracecover: manifest has %d actions, want exactly %d",
			len(manifest.Actions), ExpectedActionCount,
		)
	}
	return nil
}

func predicateKey(op, result string, completedDelta int32) string {
	var output bytes.Buffer
	fmt.Fprintf(&output, "%s/%s/%d", op, result, completedDelta)
	return output.String()
}
