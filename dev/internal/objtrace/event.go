// Package objtrace records deterministic, replayable observations of the
// public raptorgo ObjectDecoder API.
package objtrace

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"

	"github.com/fgn/raptorgo/dev/internal/strictjson"
)

const (
	// Version is the only event schema version understood by this package.
	Version = 1
	// MaxEvents bounds traces before they are handed to TLC.
	MaxEvents = 4096
	// MaxEncodedBytes bounds an NDJSON trace before decoding or mutation.
	MaxEncodedBytes = 2 << 20
)

var (
	tokenPattern  = regexp.MustCompile(`^[a-z0-9_-]+$`)
	modulePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
)

var validOperations = map[string]struct{}{
	"init":                {},
	"construct_final":     {},
	"add_wait":            {},
	"block_complete":      {},
	"verify_enter":        {},
	"verified":            {},
	"deliver_enter":       {},
	"add_return":          {},
	"decode_failure":      {},
	"terminal_add":        {},
	"add_reject_invalid":  {},
	"add_reject_limit":    {},
	"add_reject_conflict": {},
}

var validResults = map[string]struct{}{
	"ok":                 {},
	"verification_error": {},
	"delivery_error":     {},
	"decode_error":       {},
	"terminal":           {},
	"invalid":            {},
	"conflict":           {},
	"limit":              {},
}

var validStates = map[string]struct{}{
	"receiving":           {},
	"decoded":             {},
	"verified":            {},
	"delivered":           {},
	"verification-failed": {},
	"delivery-failed":     {},
	"decode-failed":       {},
}

// Event is one schema-v1 ObjectDecoder observation. Blocks is present only on
// the first init or construct_final event. Packet attribution is optional and
// diagnostic; it is intentionally absent from TLA records and fingerprints.
type Event struct {
	V            int    `json:"v"`
	Op           string `json:"op"`
	Result       string `json:"result"`
	State        string `json:"state"`
	Completed    uint32 `json:"completed"`
	Verification uint32 `json:"verification"`
	Delivery     uint32 `json:"delivery"`
	BindingOK    bool   `json:"bindingOK"`
	Blocks       *int32 `json:"blocks,omitempty"`
	SBN          *int32 `json:"sbn,omitempty"`
	ESI          *int32 `json:"esi,omitempty"`
	Symbols      *int32 `json:"symbols,omitempty"`
}

// Validate rejects values outside the deliberately closed v1 schema.
func (event Event) Validate() error {
	if event.V != Version {
		return fmt.Errorf("objtrace: unsupported schema version %d", event.V)
	}
	if !tokenPattern.MatchString(event.Op) {
		return fmt.Errorf("objtrace: invalid operation token %q", event.Op)
	}
	if _, ok := validOperations[event.Op]; !ok {
		return fmt.Errorf("objtrace: unknown operation %q", event.Op)
	}
	if !tokenPattern.MatchString(event.Result) {
		return fmt.Errorf("objtrace: invalid result token %q", event.Result)
	}
	if _, ok := validResults[event.Result]; !ok {
		return fmt.Errorf("objtrace: unknown result %q", event.Result)
	}
	if !tokenPattern.MatchString(event.State) {
		return fmt.Errorf("objtrace: invalid state token %q", event.State)
	}
	if _, ok := validStates[event.State]; !ok {
		return fmt.Errorf("objtrace: unknown state %q", event.State)
	}
	if event.Completed > math.MaxInt32 || event.Verification > math.MaxInt32 || event.Delivery > math.MaxInt32 {
		return errors.New("objtrace: counter exceeds int32")
	}
	if event.Blocks != nil && *event.Blocks < 0 {
		return fmt.Errorf("objtrace: blocks must be nonnegative, got %d", *event.Blocks)
	}
	if event.SBN != nil && *event.SBN < 0 {
		return fmt.Errorf("objtrace: negative SBN %d", *event.SBN)
	}
	if event.ESI != nil && *event.ESI < 0 {
		return fmt.Errorf("objtrace: negative ESI %d", *event.ESI)
	}
	if event.Symbols != nil && *event.Symbols < 1 {
		return fmt.Errorf("objtrace: symbols must be positive, got %d", *event.Symbols)
	}
	return nil
}

// ValidateTrace validates structural constraints that span event lines.
func ValidateTrace(events []Event) error {
	if len(events) == 0 {
		return errors.New("objtrace: empty trace")
	}
	if len(events) > MaxEvents {
		return fmt.Errorf("objtrace: trace has %d events, limit %d", len(events), MaxEvents)
	}
	for index, event := range events {
		if err := event.Validate(); err != nil {
			return fmt.Errorf("objtrace: event %d: %w", index, err)
		}
		if index == 0 {
			if event.Op != "init" && event.Op != "construct_final" {
				return fmt.Errorf("objtrace: first operation is %q, want init or construct_final", event.Op)
			}
			if event.Blocks == nil {
				return errors.New("objtrace: first event has no blocks field")
			}
			continue
		}
		if event.Op == "init" || event.Op == "construct_final" {
			return fmt.Errorf("objtrace: operation %q appears after first event", event.Op)
		}
		if event.Blocks != nil {
			return fmt.Errorf("objtrace: event %d has unexpected blocks field", index)
		}
	}
	return nil
}

// ReadNDJSON decodes one strict JSON object per nonempty line.
func ReadNDJSON(reader io.Reader) ([]Event, error) {
	counted := &countingReader{reader: io.LimitReader(reader, MaxEncodedBytes+1)}
	scanner := bufio.NewScanner(counted)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	events := make([]Event, 0)
	for scanner.Scan() {
		if counted.bytes > MaxEncodedBytes {
			return nil, fmt.Errorf("objtrace: trace exceeds %d bytes", MaxEncodedBytes)
		}
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			return nil, fmt.Errorf("objtrace: empty line %d", len(events)+1)
		}
		if err := strictjson.Check(line); err != nil {
			return nil, fmt.Errorf("objtrace: line %d: %w", len(events)+1, err)
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		var event Event
		if err := decoder.Decode(&event); err != nil {
			return nil, fmt.Errorf("objtrace: line %d: %w", len(events)+1, err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			if err == nil {
				err = errors.New("multiple JSON values")
			}
			return nil, fmt.Errorf("objtrace: line %d: %w", len(events)+1, err)
		}
		events = append(events, event)
		if len(events) > MaxEvents {
			return nil, fmt.Errorf("objtrace: trace has more than %d events", MaxEvents)
		}
	}
	if counted.bytes > MaxEncodedBytes {
		return nil, fmt.Errorf("objtrace: trace exceeds %d bytes", MaxEncodedBytes)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("objtrace: read: %w", err)
	}
	if err := ValidateTrace(events); err != nil {
		return nil, err
	}
	return events, nil
}

type countingReader struct {
	reader io.Reader
	bytes  int
}

func (reader *countingReader) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer)
	reader.bytes += count
	return count, err
}

// WriteNDJSON writes the canonical schema field order, one event per line.
func WriteNDJSON(writer io.Writer, events []Event) error {
	if err := ValidateTrace(events); err != nil {
		return err
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return fmt.Errorf("objtrace: encode: %w", err)
		}
	}
	return nil
}

// CanonicalNDJSON returns the byte representation used for committed traces.
func CanonicalNDJSON(events []Event) ([]byte, error) {
	var output bytes.Buffer
	if err := WriteNDJSON(&output, events); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// Fingerprint returns SHA-256 over canonical NDJSON with packet attribution
// removed, so equivalent control-flow traces deduplicate regardless of packet.
func Fingerprint(events []Event) ([sha256.Size]byte, error) {
	canonical := make([]Event, len(events))
	copy(canonical, events)
	for index := range canonical {
		canonical[index].SBN = nil
		canonical[index].ESI = nil
		canonical[index].Symbols = nil
	}
	encoded, err := CanonicalNDJSON(canonical)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

// ShortFingerprint returns the first 12 lowercase hexadecimal fingerprint
// characters used as committed corpus file names.
func ShortFingerprint(events []Event) (string, error) {
	digest, err := Fingerprint(events)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest[:])[:12], nil
}

// RenderTLA renders a data module accepted by ObjectDecoderTraceSpec.
func RenderTLA(module string, blocks int32, events []Event) ([]byte, error) {
	if !modulePattern.MatchString(module) {
		return nil, fmt.Errorf("objtrace: invalid TLA module token %q", module)
	}
	if blocks < 0 {
		return nil, fmt.Errorf("objtrace: blocks must be nonnegative, got %d", blocks)
	}
	if err := ValidateTrace(events); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	fmt.Fprintf(&output, "---------------- MODULE %s ----------------\n", module)
	fmt.Fprintln(&output, "EXTENDS Naturals, Sequences")
	fmt.Fprintf(&output, "\nImplementationBlocks == %d\n", blocks)
	fmt.Fprintln(&output, "\nImplementationTrace == <<")
	for index, event := range events {
		comma := ","
		if index+1 == len(events) {
			comma = ""
		}
		fmt.Fprintf(
			&output,
			"    [op |-> %q, result |-> %q, state |-> %q, completed |-> %d, verification |-> %d, delivery |-> %d, bindingOK |-> %s]%s\n",
			event.Op, event.Result, event.State, event.Completed,
			event.Verification, event.Delivery, tlaBool(event.BindingOK), comma,
		)
	}
	fmt.Fprintln(&output, ">>\n\n=====================================================================")
	return output.Bytes(), nil
}

func tlaBool(value bool) string {
	if value {
		return "TRUE"
	}
	return "FALSE"
}
