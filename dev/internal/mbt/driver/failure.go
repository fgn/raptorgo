package driver

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

// FailureKind identifies one stable oracle boundary. It intentionally omits
// schedule indexes and wrapped error prose so signatures survive step removal.
type FailureKind string

const (
	FailureFixture  FailureKind = "fixture"
	FailurePacket   FailureKind = "packet"
	FailureCallback FailureKind = "callback"
	FailureError    FailureKind = "error-class"
	FailureState    FailureKind = "state"
	FailureComplete FailureKind = "completed"
	FailureEvents   FailureKind = "events"
	FailureTrace    FailureKind = "trace"
)

// FailureSignature is the stable identity used by the minimizer. Step is kept
// on RunFailure for diagnostics but is deliberately not part of this value.
type FailureSignature struct {
	Kind     FailureKind `json:"kind"`
	Action   string      `json:"action,omitempty"`
	Expected string      `json:"expected"`
	Actual   string      `json:"actual"`
}

// RunFailure is one structured execution mismatch. Step is zero-based, or -1
// for setup/final trace validation.
type RunFailure struct {
	Step      int              `json:"step"`
	Signature FailureSignature `json:"signature"`
	Cause     error            `json:"-"`
}

func (failure *RunFailure) Error() string {
	location := "final"
	if failure.Step >= 0 {
		location = "step " + strconv.Itoa(failure.Step)
	}
	detail := fmt.Sprintf(
		"mbt: %s action %s %s: got %q, want %q",
		location, failure.Signature.Action, failure.Signature.Kind,
		failure.Signature.Actual, failure.Signature.Expected,
	)
	if failure.Cause != nil {
		return detail + ": " + failure.Cause.Error()
	}
	return detail
}

// Unwrap preserves sentinel inspection without making wrapped error text part
// of the stable signature.
func (failure *RunFailure) Unwrap() error { return failure.Cause }

// NewRunFailure constructs a structured failure. It is also the injection
// point used by deterministic model-driver tests.
func NewRunFailure(step int, signature FailureSignature, cause error) *RunFailure {
	return &RunFailure{Step: step, Signature: signature, Cause: cause}
}

// SignatureOf extracts a stable signature from a driver failure.
func SignatureOf(err error) (FailureSignature, bool) {
	var failure *RunFailure
	if !errors.As(err, &failure) {
		return FailureSignature{}, false
	}
	return failure.Signature, true
}

func stepFailure(
	step int,
	action string,
	kind FailureKind,
	expected, actual string,
	cause error,
) *RunFailure {
	return NewRunFailure(step, FailureSignature{
		Kind: kind, Action: action, Expected: expected, Actual: actual,
	}, cause)
}

func finalFailure(kind FailureKind, expected, actual string, cause error) *RunFailure {
	return stepFailure(-1, "", kind, expected, actual, cause)
}

func errorClass(err error) string {
	if err == nil {
		return "nil"
	}
	matched := make([]string, 0, len(oracleErrorSentinels))
	for _, sentinel := range oracleErrorSentinels {
		if errors.Is(err, sentinel.err) {
			matched = append(matched, sentinel.name)
		}
	}
	if len(matched) == 0 {
		return "other"
	}
	return strings.Join(matched, "+")
}

func eventSignature(events []objtrace.Event) string {
	operations := make([]string, len(events))
	for index, event := range events {
		operations[index] = event.Op
		if !event.BindingOK {
			operations[index] += "!binding"
		}
	}
	return strings.Join(operations, ",")
}
