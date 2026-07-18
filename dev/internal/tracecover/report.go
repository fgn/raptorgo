package tracecover

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

// Source identifies whether a trace came from the harvested corpus or the
// hand-curated model-legality witnesses.
type Source string

const (
	SourceCorpus  Source = "corpus"
	SourceWitness Source = "witness"
)

// Scope selects a coverage gate.
type Scope string

const (
	ScopeAll     Scope = "all"
	ScopeCorpus  Scope = "corpus"
	ScopeWitness Scope = "witness"
)

// Trace is one named input execution.
type Trace struct {
	Name   string
	Source Source
	Events []objtrace.Event
}

// ActionCoverage is the deterministic report row for one action.
type ActionCoverage struct {
	Name           string   `json:"name"`
	Op             string   `json:"op"`
	Result         string   `json:"result"`
	CompletedDelta int32    `json:"completedDelta"`
	Count          int      `json:"count"`
	Corpus         int      `json:"corpus"`
	Witness        int      `json:"witness"`
	Fingerprints   []string `json:"fingerprints"`
}

// SourceCoverage summarizes one independently gateable input family.
type SourceCoverage struct {
	Traces    int      `json:"traces"`
	Events    int      `json:"events"`
	Covered   int      `json:"covered"`
	Total     int      `json:"total"`
	Uncovered []string `json:"uncovered"`
}

// Report is the stable machine-readable and human-readable coverage result.
type Report struct {
	V         int              `json:"v"`
	Traces    int              `json:"traces"`
	Events    int              `json:"events"`
	Covered   int              `json:"covered"`
	Total     int              `json:"total"`
	Actions   []ActionCoverage `json:"actions"`
	Uncovered []string         `json:"uncovered"`
	Corpus    SourceCoverage   `json:"corpus"`
	Witness   SourceCoverage   `json:"witness"`
}

type preparedTrace struct {
	Trace
	fingerprint string
}

// Analyze validates and classifies every non-init event. construct_final is
// intentionally classified at index zero; only events whose op is exactly init
// are skipped.
func Analyze(manifest Manifest, traces []Trace) (Report, error) {
	if err := manifest.Validate(); err != nil {
		return Report{}, err
	}
	if len(traces) == 0 {
		return Report{}, errors.New("tracecover: empty trace corpus")
	}

	prepared := make([]preparedTrace, len(traces))
	for index, trace := range traces {
		if trace.Source != SourceCorpus && trace.Source != SourceWitness {
			return Report{}, fmt.Errorf("tracecover: trace %q has invalid source %q", trace.Name, trace.Source)
		}
		if err := objtrace.ValidateTrace(trace.Events); err != nil {
			return Report{}, fmt.Errorf("tracecover: trace %q: %w", trace.Name, err)
		}
		fingerprint, err := objtrace.ShortFingerprint(trace.Events)
		if err != nil {
			return Report{}, fmt.Errorf("tracecover: fingerprint trace %q: %w", trace.Name, err)
		}
		prepared[index] = preparedTrace{Trace: trace, fingerprint: fingerprint}
	}
	sort.Slice(prepared, func(left, right int) bool {
		if prepared[left].fingerprint != prepared[right].fingerprint {
			return prepared[left].fingerprint < prepared[right].fingerprint
		}
		if prepared[left].Source != prepared[right].Source {
			return prepared[left].Source < prepared[right].Source
		}
		return prepared[left].Name < prepared[right].Name
	})

	actions := ExpectedActions()
	report := Report{
		V: ManifestVersion, Total: len(actions),
		Actions:   make([]ActionCoverage, len(actions)),
		Corpus:    SourceCoverage{Total: len(actions), Uncovered: make([]string, 0, len(actions))},
		Witness:   SourceCoverage{Total: len(actions), Uncovered: make([]string, 0, len(actions))},
		Uncovered: make([]string, 0, len(actions)),
	}
	predicateIndex := make(map[string]int, len(actions))
	for index, action := range actions {
		report.Actions[index] = ActionCoverage{
			Name: action.Name, Op: action.Op, Result: action.Result,
			CompletedDelta: action.CompletedDelta, Fingerprints: []string{},
		}
		predicateIndex[predicateKey(action.Op, action.Result, action.CompletedDelta)] = index
	}

	for _, trace := range prepared {
		report.Traces++
		source := reportPointer(&report, trace.Source)
		source.Traces++
		var previousCompleted uint32
		for eventIndex, event := range trace.Events {
			if event.Completed < previousCompleted {
				return Report{}, fmt.Errorf(
					"tracecover: trace %q event %d completed regressed from %d to %d",
					trace.Name, eventIndex, previousCompleted, event.Completed,
				)
			}
			delta := uint64(event.Completed - previousCompleted)
			previousCompleted = event.Completed
			if event.Op == "init" {
				continue
			}
			if delta > math.MaxInt32 {
				return Report{}, fmt.Errorf("tracecover: trace %q event %d completed delta %d exceeds int32", trace.Name, eventIndex, delta)
			}
			key := predicateKey(event.Op, event.Result, int32(delta))
			actionIndex, exists := predicateIndex[key]
			if !exists {
				return Report{}, fmt.Errorf(
					"tracecover: trace %q event %d matches no action: op=%s result=%s completedDelta=%d",
					trace.Name, eventIndex, event.Op, event.Result, delta,
				)
			}
			coverage := &report.Actions[actionIndex]
			coverage.Count++
			switch trace.Source {
			case SourceCorpus:
				coverage.Corpus++
			case SourceWitness:
				coverage.Witness++
			}
			appendFingerprint(coverage, trace.fingerprint)
			report.Events++
			source.Events++
		}
	}

	for _, coverage := range report.Actions {
		if coverage.Count == 0 {
			report.Uncovered = append(report.Uncovered, coverage.Name)
		} else {
			report.Covered++
		}
		if coverage.Corpus == 0 {
			report.Corpus.Uncovered = append(report.Corpus.Uncovered, coverage.Name)
		} else {
			report.Corpus.Covered++
		}
		if coverage.Witness == 0 {
			report.Witness.Uncovered = append(report.Witness.Uncovered, coverage.Name)
		} else {
			report.Witness.Covered++
		}
	}
	return report, nil
}

func reportPointer(report *Report, source Source) *SourceCoverage {
	if source == SourceCorpus {
		return &report.Corpus
	}
	return &report.Witness
}

func appendFingerprint(coverage *ActionCoverage, fingerprint string) {
	for _, existing := range coverage.Fingerprints {
		if existing == fingerprint {
			return
		}
	}
	if len(coverage.Fingerprints) < 3 {
		coverage.Fingerprints = append(coverage.Fingerprints, fingerprint)
	}
}

// RequireFull fails when the selected family is empty or does not cover the
// complete golden action alphabet.
func (report Report) RequireFull(scope Scope) error {
	var traces int
	var uncovered []string
	switch scope {
	case ScopeAll:
		traces, uncovered = report.Traces, report.Uncovered
	case ScopeCorpus:
		traces, uncovered = report.Corpus.Traces, report.Corpus.Uncovered
	case ScopeWitness:
		traces, uncovered = report.Witness.Traces, report.Witness.Uncovered
	default:
		return fmt.Errorf("tracecover: unknown coverage scope %q", scope)
	}
	if traces == 0 {
		return fmt.Errorf("tracecover: %s input is empty", scope)
	}
	if len(uncovered) != 0 {
		return fmt.Errorf("tracecover: %s coverage missing: %s", scope, strings.Join(uncovered, ", "))
	}
	return nil
}

// RenderJSON emits canonical indented JSON with a trailing newline.
func (report Report) RenderJSON() ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return nil, fmt.Errorf("tracecover: encode report: %w", err)
	}
	return output.Bytes(), nil
}

// RenderHuman emits a stable table followed by independently gateable source
// summaries.
func (report Report) RenderHuman() []byte {
	var output bytes.Buffer
	fmt.Fprintln(&output, "ACTION                      OP                    RESULT               DELTA   TOTAL  CORPUS  WITNESS  FINGERPRINTS")
	for _, action := range report.Actions {
		fmt.Fprintf(
			&output, "%-27s %-21s %-20s %5d %7d %7d %8d  %s\n",
			action.Name, action.Op, action.Result, action.CompletedDelta,
			action.Count, action.Corpus, action.Witness, strings.Join(action.Fingerprints, ","),
		)
	}
	fmt.Fprintf(
		&output, "all: traces=%d events=%d covered=%d/%d uncovered=%s\n",
		report.Traces, report.Events, report.Covered, report.Total, uncoveredText(report.Uncovered),
	)
	// A scope with zero traces had no inputs of that kind; listing every
	// action as uncovered there would misread as a failing gate.
	if report.Corpus.Traces > 0 {
		fmt.Fprintf(
			&output, "corpus: traces=%d events=%d covered=%d/%d uncovered=%s\n",
			report.Corpus.Traces, report.Corpus.Events, report.Corpus.Covered,
			report.Corpus.Total, uncoveredText(report.Corpus.Uncovered),
		)
	}
	if report.Witness.Traces > 0 {
		fmt.Fprintf(
			&output, "witness: traces=%d events=%d covered=%d/%d uncovered=%s\n",
			report.Witness.Traces, report.Witness.Events, report.Witness.Covered,
			report.Witness.Total, uncoveredText(report.Witness.Uncovered),
		)
	}
	return output.Bytes()
}

func uncoveredText(uncovered []string) string {
	if len(uncovered) == 0 {
		return "none"
	}
	return strings.Join(uncovered, ",")
}
