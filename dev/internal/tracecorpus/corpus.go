// Package tracecorpus deterministically curates and writes ObjectDecoder
// traces. Packet attribution is diagnostic; abstract control-flow identity is
// supplied by objtrace.Fingerprint.
package tracecorpus

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

const (
	// DefaultMax bounds the committed corpus while leaving room for new
	// control-flow witnesses.
	DefaultMax = 64
)

// Feature names a semantic family that curation may require independently of
// abstract trace novelty, such as grouped packets or callback reentrancy.
type Feature string

// Candidate is one harvested execution before deduplication and selection.
type Candidate struct {
	Source   string
	Features []Feature
	Events   []objtrace.Event
}

// Options controls deterministic selection.
type Options struct {
	Max      int
	Required []Feature
}

// Trace is one selected corpus member.
type Trace struct {
	Source      string
	Features    []Feature
	Events      []objtrace.Event
	Fingerprint string
	Digest      [sha256.Size]byte
	NDJSON      []byte
}

// Decision records why a candidate was not selected.
type Decision struct {
	Source      string
	Fingerprint string
	Reason      string
}

// Result is a deterministic curation result. Traces and Dropped are sorted by
// fingerprint and then source, independent of candidate input order.
type Result struct {
	Traces  []Trace
	Dropped []Decision
}

type fingerprintFunc func([]objtrace.Event) ([sha256.Size]byte, error)

// Curate selects required semantic families first, then greedily retains
// traces that contribute a new event tuple or operation bigram.
func Curate(candidates []Candidate, options Options) (Result, error) {
	return curate(candidates, options, objtrace.Fingerprint)
}

func curate(candidates []Candidate, options Options, fingerprint fingerprintFunc) (Result, error) {
	if options.Max == 0 {
		options.Max = DefaultMax
	}
	if options.Max < 1 {
		return Result{}, fmt.Errorf("tracecorpus: maximum must be positive, got %d", options.Max)
	}
	required, err := normalizeFeatures(options.Required)
	if err != nil {
		return Result{}, fmt.Errorf("tracecorpus: required features: %w", err)
	}

	normalized := make([]Trace, 0, len(candidates))
	for index, candidate := range candidates {
		if candidate.Source == "" {
			return Result{}, fmt.Errorf("tracecorpus: candidate %d has an empty source", index)
		}
		features, featureErr := normalizeFeatures(candidate.Features)
		if featureErr != nil {
			return Result{}, fmt.Errorf("tracecorpus: candidate %q: %w", candidate.Source, featureErr)
		}
		if err := validateCompleted(candidate.Events); err != nil {
			return Result{}, fmt.Errorf("tracecorpus: candidate %q: %w", candidate.Source, err)
		}
		encoded, encodeErr := objtrace.CanonicalNDJSON(candidate.Events)
		if encodeErr != nil {
			return Result{}, fmt.Errorf("tracecorpus: candidate %q: %w", candidate.Source, encodeErr)
		}
		digest, digestErr := fingerprint(candidate.Events)
		if digestErr != nil {
			return Result{}, fmt.Errorf("tracecorpus: candidate %q fingerprint: %w", candidate.Source, digestErr)
		}
		normalized = append(normalized, Trace{
			Source: candidate.Source, Features: features, Events: cloneEvents(candidate.Events),
			Fingerprint: hex.EncodeToString(digest[:])[:12], Digest: digest, NDJSON: encoded,
		})
	}

	featureDigests := make(map[Feature]map[[sha256.Size]byte]struct{}, len(required))
	for _, candidate := range normalized {
		for _, feature := range candidate.Features {
			if !slices.Contains(required, feature) {
				continue
			}
			if featureDigests[feature] == nil {
				featureDigests[feature] = make(map[[sha256.Size]byte]struct{})
			}
			featureDigests[feature][candidate.Digest] = struct{}{}
		}
	}
	sort.Slice(normalized, func(left, right int) bool {
		if len(normalized[left].Events) != len(normalized[right].Events) {
			return len(normalized[left].Events) < len(normalized[right].Events)
		}
		if comparison := bytes.Compare(normalized[left].Digest[:], normalized[right].Digest[:]); comparison != 0 {
			return comparison < 0
		}
		leftExclusive := exclusiveFeatureCount(normalized[left].Features, featureDigests)
		rightExclusive := exclusiveFeatureCount(normalized[right].Features, featureDigests)
		if leftExclusive != rightExclusive {
			return leftExclusive > rightExclusive
		}
		leftRequired := featureIntersectionCount(normalized[left].Features, required)
		rightRequired := featureIntersectionCount(normalized[right].Features, required)
		if leftRequired != rightRequired {
			return leftRequired > rightRequired
		}
		if normalized[left].Source != normalized[right].Source {
			return normalized[left].Source < normalized[right].Source
		}
		return bytes.Compare(normalized[left].NDJSON, normalized[right].NDJSON) < 0
	})

	unique := make([]Trace, 0, len(normalized))
	dropped := make([]Decision, 0)
	byShort := make(map[string][sha256.Size]byte)
	byDigest := make(map[[sha256.Size]byte]int)
	for _, candidate := range normalized {
		if previous, ok := byShort[candidate.Fingerprint]; ok && previous != candidate.Digest {
			return Result{}, fmt.Errorf(
				"tracecorpus: truncated fingerprint collision %s between %x and %x",
				candidate.Fingerprint, previous, candidate.Digest,
			)
		}
		byShort[candidate.Fingerprint] = candidate.Digest
		if previous, ok := byDigest[candidate.Digest]; ok {
			dropped = append(dropped, Decision{
				Source: candidate.Source, Fingerprint: candidate.Fingerprint,
				Reason: "abstract duplicate of " + unique[previous].Source,
			})
			continue
		}
		byDigest[candidate.Digest] = len(unique)
		unique = append(unique, candidate)
	}

	selected := make([]bool, len(unique))
	coveredRequired := make(map[Feature]bool, len(required))
	for len(coveredRequired) < len(required) {
		best, bestGain := -1, 0
		for index, candidate := range unique {
			if selected[index] {
				continue
			}
			gain := 0
			for _, feature := range candidate.Features {
				if slices.Contains(required, feature) && !coveredRequired[feature] {
					gain++
				}
			}
			if gain > bestGain {
				best, bestGain = index, gain
			}
		}
		if best < 0 {
			missing := missingFeatures(required, coveredRequired)
			return Result{}, fmt.Errorf("tracecorpus: required features have no candidates: %s", joinFeatures(missing))
		}
		selected[best] = true
		for _, feature := range unique[best].Features {
			if slices.Contains(required, feature) {
				coveredRequired[feature] = true
			}
		}
	}

	selectedCount := 0
	seenTuples := make(map[string]struct{})
	seenBigrams := make(map[string]struct{})
	for index, candidate := range unique {
		if !selected[index] {
			continue
		}
		selectedCount++
		addNovelty(candidate.Events, seenTuples, seenBigrams)
	}
	if selectedCount > options.Max {
		return Result{}, fmt.Errorf(
			"tracecorpus: %d traces are required to preserve feature obligations, maximum is %d",
			selectedCount, options.Max,
		)
	}

	for index, candidate := range unique {
		if selected[index] {
			continue
		}
		if selectedCount >= options.Max {
			dropped = append(dropped, Decision{
				Source: candidate.Source, Fingerprint: candidate.Fingerprint, Reason: "maximum reached",
			})
			continue
		}
		if !hasNovelty(candidate.Events, seenTuples, seenBigrams) {
			dropped = append(dropped, Decision{
				Source: candidate.Source, Fingerprint: candidate.Fingerprint, Reason: "no new event tuple or operation bigram",
			})
			continue
		}
		selected[index] = true
		selectedCount++
		addNovelty(candidate.Events, seenTuples, seenBigrams)
	}

	result := Result{Dropped: dropped}
	for index, candidate := range unique {
		if selected[index] {
			result.Traces = append(result.Traces, candidate)
		}
	}
	sort.Slice(result.Traces, func(left, right int) bool {
		if result.Traces[left].Fingerprint != result.Traces[right].Fingerprint {
			return result.Traces[left].Fingerprint < result.Traces[right].Fingerprint
		}
		return result.Traces[left].Source < result.Traces[right].Source
	})
	sort.Slice(result.Dropped, func(left, right int) bool {
		if result.Dropped[left].Fingerprint != result.Dropped[right].Fingerprint {
			return result.Dropped[left].Fingerprint < result.Dropped[right].Fingerprint
		}
		if result.Dropped[left].Source != result.Dropped[right].Source {
			return result.Dropped[left].Source < result.Dropped[right].Source
		}
		return result.Dropped[left].Reason < result.Dropped[right].Reason
	})
	return result, nil
}

// WriteDirectory stages the complete selected corpus before replacing only
// regular NDJSON files in directory. It never follows a conflicting symlink
// and writes one deterministic log line for every kept and dropped candidate.
func WriteDirectory(directory string, result Result, log io.Writer) error {
	if directory == "" {
		return errors.New("tracecorpus: output directory is empty")
	}
	wanted := make(map[string]struct{}, len(result.Traces))
	for _, trace := range result.Traces {
		if !validFingerprint(trace.Fingerprint) {
			return fmt.Errorf("tracecorpus: invalid fingerprint %q", trace.Fingerprint)
		}
		name := trace.Fingerprint + ".ndjson"
		if _, duplicate := wanted[name]; duplicate {
			return fmt.Errorf("tracecorpus: duplicate output %s", name)
		}
		wanted[name] = struct{}{}
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("tracecorpus: create output directory: %w", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("tracecorpus: list output directory: %w", err)
	}
	existing := make([]string, 0)
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".ndjson" {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return fmt.Errorf("tracecorpus: inspect existing %s: %w", entry.Name(), infoErr)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("tracecorpus: existing %s is not a regular file", entry.Name())
		}
		existing = append(existing, entry.Name())
	}

	type stagedTrace struct {
		name, path string
	}
	staged := make([]stagedTrace, 0, len(result.Traces))
	defer func() {
		for _, item := range staged {
			_ = os.Remove(item.path)
		}
	}()
	for _, trace := range result.Traces {
		name := trace.Fingerprint + ".ndjson"
		path, stageErr := stageFile(directory, trace.NDJSON)
		if stageErr != nil {
			return fmt.Errorf("tracecorpus: stage %s: %w", name, stageErr)
		}
		staged = append(staged, stagedTrace{name: name, path: path})
	}
	for index := range staged {
		item := &staged[index]
		if err := os.Rename(item.path, filepath.Join(directory, item.name)); err != nil {
			return fmt.Errorf("tracecorpus: write %s: %w", item.name, err)
		}
		item.path = ""
	}
	for _, name := range existing {
		if _, keep := wanted[name]; keep {
			continue
		}
		if err := os.Remove(filepath.Join(directory, name)); err != nil {
			return fmt.Errorf("tracecorpus: remove stale %s: %w", name, err)
		}
	}
	for _, trace := range result.Traces {
		name := trace.Fingerprint + ".ndjson"
		if log != nil {
			fmt.Fprintf(log, "keep %s source=%s events=%d features=%s\n", name, trace.Source, len(trace.Events), joinFeatures(trace.Features))
		}
	}
	if log != nil {
		for _, decision := range result.Dropped {
			fmt.Fprintf(log, "drop %s source=%s reason=%s\n", decision.Fingerprint, decision.Source, decision.Reason)
		}
	}
	return nil
}

func validFingerprint(value string) bool {
	if len(value) != 12 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func stageFile(directory string, contents []byte) (path string, err error) {
	file, err := os.CreateTemp(directory, ".tracecorpus-*.tmp")
	if err != nil {
		return "", err
	}
	path = file.Name()
	defer func() {
		if err != nil {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()
	if err = file.Chmod(0o644); err != nil {
		return "", err
	}
	if _, err = file.Write(contents); err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func normalizeFeatures(features []Feature) ([]Feature, error) {
	result := append([]Feature(nil), features...)
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	result = slices.Compact(result)
	for _, feature := range result {
		if feature == "" || strings.TrimSpace(string(feature)) != string(feature) {
			return nil, fmt.Errorf("invalid feature %q", feature)
		}
	}
	return result, nil
}

func missingFeatures(required []Feature, covered map[Feature]bool) []Feature {
	var result []Feature
	for _, feature := range required {
		if !covered[feature] {
			result = append(result, feature)
		}
	}
	return result
}

func featureIntersectionCount(features, required []Feature) int {
	count := 0
	for _, feature := range features {
		if slices.Contains(required, feature) {
			count++
		}
	}
	return count
}

func exclusiveFeatureCount(features []Feature, featureDigests map[Feature]map[[sha256.Size]byte]struct{}) int {
	count := 0
	for _, feature := range features {
		if len(featureDigests[feature]) == 1 {
			count++
		}
	}
	return count
}

func joinFeatures(features []Feature) string {
	values := make([]string, len(features))
	for index, feature := range features {
		values[index] = string(feature)
	}
	return strings.Join(values, ",")
}

func validateCompleted(events []objtrace.Event) error {
	if err := objtrace.ValidateTrace(events); err != nil {
		return err
	}
	previous := events[0].Completed
	for index := 1; index < len(events); index++ {
		if events[index].Completed < previous {
			return fmt.Errorf("completed counter decreases at event %d", index)
		}
		previous = events[index].Completed
	}
	return nil
}

func hasNovelty(events []objtrace.Event, tuples, bigrams map[string]struct{}) bool {
	for _, key := range noveltyTuples(events) {
		if _, ok := tuples[key]; !ok {
			return true
		}
	}
	for _, key := range operationBigrams(events) {
		if _, ok := bigrams[key]; !ok {
			return true
		}
	}
	return false
}

func addNovelty(events []objtrace.Event, tuples, bigrams map[string]struct{}) {
	for _, key := range noveltyTuples(events) {
		tuples[key] = struct{}{}
	}
	for _, key := range operationBigrams(events) {
		bigrams[key] = struct{}{}
	}
}

func noveltyTuples(events []objtrace.Event) []string {
	result := make([]string, len(events))
	var previous uint32
	for index, event := range events {
		delta := event.Completed - previous
		result[index] = fmt.Sprintf("%s\x00%s\x00%s\x00%d", event.Op, event.Result, event.State, delta)
		previous = event.Completed
	}
	return result
}

func operationBigrams(events []objtrace.Event) []string {
	if len(events) < 2 {
		return nil
	}
	result := make([]string, 0, len(events)-1)
	for index := 1; index < len(events); index++ {
		result = append(result, events[index-1].Op+"\x00"+events[index].Op)
	}
	return result
}

func cloneEvents(events []objtrace.Event) []objtrace.Event {
	result := make([]objtrace.Event, len(events))
	for index, event := range events {
		result[index] = event
		result[index].Blocks = cloneInt32(event.Blocks)
		result[index].SBN = cloneInt32(event.SBN)
		result[index].ESI = cloneInt32(event.ESI)
		result[index].Symbols = cloneInt32(event.Symbols)
	}
	return result
}

func cloneInt32(value *int32) *int32 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
