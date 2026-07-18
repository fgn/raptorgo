package driver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fgn/raptorgo/dev/internal/mbt/schedule"
	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

// ExecuteFunc executes a strictly validated shrink candidate.
type ExecuteFunc func(schedule.Schedule) (Result, error)

// ShrinkOptions exposes the execution boundary for deterministic failure
// injection tests. Nil Execute uses the production driver.
type ShrinkOptions struct {
	Execute ExecuteFunc
}

// ShrinkResult is the deterministic one-minimal diagnostic candidate and the
// final reproduction's partial implementation trace, when one was observed.
type ShrinkResult struct {
	Schedule      schedule.Schedule
	Signature     FailureSignature
	Failure       *RunFailure
	Execution     Result
	OriginalSteps int
	Attempts      int
}

// ArtifactPaths names the optional deterministic failure outputs.
type ArtifactPaths struct {
	Failure        string
	Schedule       string
	Trace          string
	StrictSchedule bool
}

// MinimizeFailure applies deterministic ddmin deletion from left to right. A
// candidate is retained only when it produces exactly target; step indexes are
// diagnostic and are deliberately absent from the target signature.
func MinimizeFailure(
	value schedule.Schedule,
	target FailureSignature,
	options ShrinkOptions,
) (ShrinkResult, error) {
	if err := ValidateSchedule(value); err != nil {
		return ShrinkResult{}, fmt.Errorf("mbt: shrink source: %w", err)
	}
	execute := options.Execute
	if execute == nil {
		execute = Run
	}
	current := cloneSchedule(value)
	result, failure, matches := reproduce(current, target, execute)
	attempts := 1
	if !matches {
		return ShrinkResult{}, fmt.Errorf("mbt: shrink source does not reproduce signature %#v", target)
	}

	granularity := 2
	for len(current.Steps) > 0 {
		if granularity > len(current.Steps) {
			granularity = len(current.Steps)
		}
		chunkSize := (len(current.Steps) + granularity - 1) / granularity
		reduced := false
		for start := 0; start < len(current.Steps); start += chunkSize {
			end := start + chunkSize
			if end > len(current.Steps) {
				end = len(current.Steps)
			}
			candidate := withoutSteps(current, start, end)
			if ValidateSchedule(candidate) != nil {
				continue
			}
			candidateResult, candidateFailure, candidateMatches := reproduce(candidate, target, execute)
			attempts++
			if !candidateMatches {
				continue
			}
			current = candidate
			result = candidateResult
			failure = candidateFailure
			if granularity > 2 {
				granularity--
			}
			reduced = true
			break
		}
		if reduced {
			continue
		}
		if granularity >= len(current.Steps) {
			break
		}
		granularity *= 2
		if granularity > len(current.Steps) {
			granularity = len(current.Steps)
		}
	}

	return ShrinkResult{
		Schedule: current, Signature: target, Failure: failure, Execution: result,
		OriginalSteps: len(value.Steps), Attempts: attempts,
	}, nil
}

func reproduce(
	value schedule.Schedule,
	target FailureSignature,
	execute ExecuteFunc,
) (Result, *RunFailure, bool) {
	result, err := execute(cloneSchedule(value))
	var failure *RunFailure
	if !errors.As(err, &failure) || failure.Signature != target {
		return result, nil, false
	}
	copy := *failure
	return result, &copy, true
}

func withoutSteps(value schedule.Schedule, start, end int) schedule.Schedule {
	result := value
	result.Steps = make([]schedule.Step, 0, len(value.Steps)-(end-start))
	for index, step := range value.Steps {
		if index >= start && index < end {
			continue
		}
		result.Steps = append(result.Steps, cloneStep(step))
	}
	return result
}

func cloneSchedule(value schedule.Schedule) schedule.Schedule {
	result := value
	result.Steps = make([]schedule.Step, len(value.Steps))
	for index, step := range value.Steps {
		result.Steps[index] = cloneStep(step)
	}
	return result
}

func cloneStep(step schedule.Step) schedule.Step {
	result := step
	if step.SBN != nil {
		value := *step.SBN
		result.SBN = &value
	}
	if step.ESI != nil {
		value := *step.ESI
		result.ESI = &value
	}
	if step.Callbacks != nil {
		value := *step.Callbacks
		result.Callbacks = &value
	}
	result.Expect.Events = append([]string(nil), step.Expect.Events...)
	result.ModelEdges = append([]schedule.ModelEdge(nil), step.ModelEdges...)
	return result
}

type failureArtifact struct {
	V              int              `json:"v"`
	Signature      FailureSignature `json:"signature"`
	FailureStep    int              `json:"failureStep"`
	OriginalSteps  int              `json:"originalSteps"`
	MinimizedSteps int              `json:"minimizedSteps"`
	Attempts       int              `json:"attempts"`
	StrictSchedule bool             `json:"strictSchedule"`
	TraceAvailable bool             `json:"traceAvailable"`
}

// WriteFailureArtifacts writes an executable minimized schedule, stable
// failure metadata, and canonical NDJSON when the failed execution produced a
// valid partial trace.
func WriteFailureArtifacts(directory string, result ShrinkResult) (ArtifactPaths, error) {
	if result.Failure == nil {
		return ArtifactPaths{}, errors.New("mbt: failure artifact has no reproduced failure")
	}
	if err := ValidateSchedule(result.Schedule); err != nil {
		return ArtifactPaths{}, fmt.Errorf("mbt: minimized schedule is not replayable: %w", err)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return ArtifactPaths{}, fmt.Errorf("mbt: create failure artifact directory: %w", err)
	}
	paths := ArtifactPaths{
		Failure:  filepath.Join(directory, "failure.json"),
		Schedule: filepath.Join(directory, "minimized.json"),
		Trace:    filepath.Join(directory, "trace.ndjson"),
	}
	paths.StrictSchedule = true
	traceAvailable := len(result.Execution.Events) > 0 && objtrace.ValidateTrace(result.Execution.Events) == nil
	metadata := failureArtifact{
		V: 1, Signature: result.Signature, FailureStep: result.Failure.Step,
		OriginalSteps: result.OriginalSteps, MinimizedSteps: len(result.Schedule.Steps),
		Attempts: result.Attempts, StrictSchedule: paths.StrictSchedule, TraceAvailable: traceAvailable,
	}
	encodedMetadata, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return ArtifactPaths{}, fmt.Errorf("mbt: encode failure metadata: %w", err)
	}
	encodedMetadata = append(encodedMetadata, '\n')
	encodedSchedule, err := schedule.Marshal(result.Schedule)
	if err != nil {
		return ArtifactPaths{}, err
	}
	if err := writeAtomic(paths.Failure, encodedMetadata); err != nil {
		return ArtifactPaths{}, err
	}
	if err := writeAtomic(paths.Schedule, encodedSchedule); err != nil {
		return ArtifactPaths{}, err
	}
	if traceAvailable {
		encodedTrace, err := objtrace.CanonicalNDJSON(result.Execution.Events)
		if err != nil {
			return ArtifactPaths{}, err
		}
		if err := writeAtomic(paths.Trace, encodedTrace); err != nil {
			return ArtifactPaths{}, err
		}
	} else if err := os.Remove(paths.Trace); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ArtifactPaths{}, fmt.Errorf("mbt: remove stale failure trace: %w", err)
	}
	if !traceAvailable {
		paths.Trace = ""
	}
	return paths, nil
}

func writeAtomic(path string, content []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".mbt-failure-*")
	if err != nil {
		return fmt.Errorf("mbt: create artifact temp file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return fmt.Errorf("mbt: write failure artifact: %w", err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("mbt: chmod failure artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("mbt: close failure artifact: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("mbt: replace failure artifact %s: %w", path, err)
	}
	return nil
}
