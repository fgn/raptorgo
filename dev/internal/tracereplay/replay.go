// Package tracereplay replays canonical ObjectDecoder NDJSON traces through
// the repository's pinned TLC launcher.
package tracereplay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

const (
	// MaxJobs prevents callers from accidentally starting an unsafe number of
	// memory-hungry TLC processes.
	MaxJobs = 4
	// MaxCorpusEntries bounds flat-directory discovery before allocation.
	MaxCorpusEntries = 4096

	replayDataModule = "ObjectDecoderReplayData"
	replayModuleFile = "ObjectDecoderReplay.tla"
	replayConfigFile = "ObjectDecoderReplay.cfg"
	traceSpecFile    = "ObjectDecoderTraceSpec.tla"

	tlcInitialStateMarker = "Finished computing initial states: 1 distinct state generated"
	tlcSuccessMarker      = "Model checking completed. No error has been found."
	tlcTraceRejectMarker  = "Error: Temporal property TraceComplete was violated."
)

// Command is one process invocation. Path and Dir are always absolute when
// produced by Run.
type Command struct {
	Path string
	Args []string
	Dir  string
}

// ExecFunc runs one TLC command and returns its combined output. Tests inject
// this boundary to exercise replay orchestration without Java.
type ExecFunc func(context.Context, Command) ([]byte, error)

// OSExec runs a command using the host process environment.
func OSExec(ctx context.Context, command Command) ([]byte, error) {
	process := exec.CommandContext(ctx, command.Path, command.Args...)
	process.Dir = command.Dir
	return process.CombinedOutput()
}

// Config describes a positive corpus replay. Root is the repository root and
// Corpus is a flat directory of .ndjson files. Relative paths are resolved
// before any temporary working-directory change.
type Config struct {
	Root       string
	Corpus     string
	KeepFailed string
	Jobs       int
	Negative   bool
	Exec       ExecFunc
}

// Failure describes one trace that could not be replayed successfully.
type Failure struct {
	Input  string
	Err    error
	Output []byte
	KeptAt string
}

// Report is deterministically ordered by input file name regardless of worker
// completion order.
type Report struct {
	Inputs   []string
	Passed   int
	Failures []Failure
}

// FailuresError reports all failed inputs in deterministic order.
type FailuresError struct {
	Failures []Failure
}

func (failure *FailuresError) Error() string {
	names := make([]string, len(failure.Failures))
	for index, item := range failure.Failures {
		names[index] = item.Input
	}
	return fmt.Sprintf("tracereplay: %d trace(s) failed: %s", len(names), strings.Join(names, ", "))
}

type resolvedConfig struct {
	corpus, keepFailed        string
	tlc, replay, config, spec string
	jobs                      int
	execute                   ExecFunc
}

type traceInput struct {
	name, path string
}

type traceResult struct {
	index   int
	failure *Failure
}

// Run replays every corpus trace and continues after individual failures so a
// single invocation reports the complete, sorted failure set.
func Run(ctx context.Context, config Config) (Report, error) {
	resolved, err := resolveConfig(config)
	if err != nil {
		return Report{}, err
	}
	inputs, err := discoverInputs(resolved.corpus)
	if err != nil {
		return Report{}, err
	}
	if len(inputs) == 0 {
		return Report{}, fmt.Errorf("tracereplay: corpus %s contains no .ndjson files", resolved.corpus)
	}
	if config.Negative {
		return runNegative(ctx, resolved, inputs)
	}

	report := Report{Inputs: make([]string, len(inputs))}
	for index, input := range inputs {
		report.Inputs[index] = input.name
	}

	work := make(chan int, len(inputs))
	results := make(chan traceResult, len(inputs))
	for index := range inputs {
		work <- index
	}
	close(work)

	var workers sync.WaitGroup
	for range resolved.jobs {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range work {
				failure := replayOne(ctx, resolved, inputs[index])
				results <- traceResult{index: index, failure: failure}
			}
		}()
	}
	workers.Wait()
	close(results)

	ordered := make([]*Failure, len(inputs))
	for result := range results {
		ordered[result.index] = result.failure
	}
	for _, failure := range ordered {
		if failure == nil {
			report.Passed++
			continue
		}
		report.Failures = append(report.Failures, *failure)
	}
	if len(report.Failures) != 0 {
		return report, &FailuresError{Failures: append([]Failure(nil), report.Failures...)}
	}
	return report, nil
}

func resolveConfig(config Config) (resolvedConfig, error) {
	if config.Jobs < 1 || config.Jobs > MaxJobs {
		return resolvedConfig{}, fmt.Errorf("tracereplay: jobs must be in 1..%d, got %d", MaxJobs, config.Jobs)
	}
	root, err := filepath.Abs(config.Root)
	if err != nil {
		return resolvedConfig{}, fmt.Errorf("tracereplay: resolve root: %w", err)
	}
	corpus, err := filepath.Abs(config.Corpus)
	if err != nil {
		return resolvedConfig{}, fmt.Errorf("tracereplay: resolve corpus: %w", err)
	}
	keepFailed := ""
	if config.KeepFailed != "" {
		keepFailed, err = filepath.Abs(config.KeepFailed)
		if err != nil {
			return resolvedConfig{}, fmt.Errorf("tracereplay: resolve keep-failed: %w", err)
		}
		if err := os.MkdirAll(keepFailed, 0o755); err != nil {
			return resolvedConfig{}, fmt.Errorf("tracereplay: create keep-failed: %w", err)
		}
	}

	formal := filepath.Join(root, "dev", "formal")
	resolved := resolvedConfig{
		corpus: corpus, keepFailed: keepFailed, jobs: config.Jobs,
		tlc:     filepath.Join(root, "dev", "scripts", "tlc.sh"),
		replay:  filepath.Join(formal, "replay", replayModuleFile),
		config:  filepath.Join(formal, "replay", replayConfigFile),
		spec:    filepath.Join(formal, traceSpecFile),
		execute: config.Exec,
	}
	if resolved.execute == nil {
		resolved.execute = OSExec
	}
	files := []struct {
		label, path string
	}{
		{"TLC launcher", resolved.tlc},
		{"replay module", resolved.replay},
		{"replay config", resolved.config},
		{"trace spec", resolved.spec},
	}
	for _, file := range files {
		info, statErr := os.Stat(file.path)
		if statErr != nil {
			return resolvedConfig{}, fmt.Errorf("tracereplay: %s %s: %w", file.label, file.path, statErr)
		}
		if !info.Mode().IsRegular() {
			return resolvedConfig{}, fmt.Errorf("tracereplay: %s %s is not a regular file", file.label, file.path)
		}
	}
	return resolved, nil
}

func discoverInputs(corpus string) ([]traceInput, error) {
	directory, err := os.Open(corpus)
	if err != nil {
		return nil, fmt.Errorf("tracereplay: open corpus %s: %w", corpus, err)
	}
	entries, readErr := directory.ReadDir(MaxCorpusEntries + 1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, fmt.Errorf("tracereplay: read corpus %s: %w", corpus, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("tracereplay: close corpus %s: %w", corpus, closeErr)
	}
	if len(entries) > MaxCorpusEntries {
		return nil, fmt.Errorf("tracereplay: corpus has more than %d entries", MaxCorpusEntries)
	}
	inputs := make([]traceInput, 0, len(entries))
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".ndjson" {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil, fmt.Errorf("tracereplay: inspect %s: %w", entry.Name(), infoErr)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("tracereplay: input %s is not a regular file", entry.Name())
		}
		inputs = append(inputs, traceInput{name: entry.Name(), path: filepath.Join(corpus, entry.Name())})
	}
	sort.Slice(inputs, func(left, right int) bool { return inputs[left].name < inputs[right].name })
	return inputs, nil
}

func replayOne(ctx context.Context, config resolvedConfig, input traceInput) *Failure {
	file, err := os.Open(input.path)
	if err != nil {
		return &Failure{Input: input.name, Err: fmt.Errorf("open: %w", err)}
	}
	events, readErr := objtrace.ReadNDJSON(file)
	closeErr := file.Close()
	if readErr != nil {
		return &Failure{Input: input.name, Err: readErr}
	}
	if closeErr != nil {
		return &Failure{Input: input.name, Err: fmt.Errorf("close: %w", closeErr)}
	}
	return replayEvents(ctx, config, input.name, events, expectPositive)
}

type outcomeCheck func([]byte, error) error

func expectPositive(output []byte, runErr error) error {
	if runErr != nil {
		return runErr
	}
	return validateTLCOutput(output)
}

func expectTraceRejected(output []byte, runErr error) error {
	if runErr == nil {
		return errors.New("mutated trace unexpectedly passed TLC")
	}
	if !bytes.Contains(output, []byte(tlcTraceRejectMarker)) {
		return fmt.Errorf("mutated trace failed without exact marker %q: %w", tlcTraceRejectMarker, runErr)
	}
	return nil
}

func replayEvents(
	ctx context.Context,
	config resolvedConfig,
	name string,
	events []objtrace.Event,
	check outcomeCheck,
) *Failure {
	blocks := *events[0].Blocks
	data, err := objtrace.RenderTLA(replayDataModule, blocks, events)
	if err != nil {
		return &Failure{Input: name, Err: err}
	}

	workdir, err := os.MkdirTemp("", "raptorgo-object-replay-")
	if err != nil {
		return &Failure{Input: name, Err: fmt.Errorf("create workdir: %w", err)}
	}
	defer os.RemoveAll(workdir)
	for _, source := range []string{config.replay, config.config, config.spec} {
		if err := copyFile(source, filepath.Join(workdir, filepath.Base(source))); err != nil {
			return &Failure{Input: name, Err: err}
		}
	}
	if err := os.WriteFile(filepath.Join(workdir, replayDataModule+".tla"), data, 0o644); err != nil {
		return &Failure{Input: name, Err: fmt.Errorf("write replay data: %w", err)}
	}

	command := Command{
		Path: config.tlc,
		Args: []string{"-noGenerateSpecTE", replayModuleFile, "-config", replayConfigFile},
		Dir:  workdir,
	}
	output, runErr := config.execute(ctx, command)
	outcomeErr := check(output, runErr)
	if outcomeErr == nil {
		return nil
	}
	failure := &Failure{Input: name, Err: fmt.Errorf("TLC: %w", outcomeErr), Output: append([]byte(nil), output...)}
	if config.keepFailed == "" {
		return failure
	}
	if writeErr := os.WriteFile(filepath.Join(workdir, "tlc.out"), output, 0o644); writeErr != nil {
		failure.Err = errors.Join(failure.Err, fmt.Errorf("write TLC output: %w", writeErr))
		return failure
	}
	keptAt, keepErr := preserveWorkdir(workdir, config.keepFailed, name+".replay")
	if keepErr != nil {
		failure.Err = errors.Join(failure.Err, keepErr)
		return failure
	}
	failure.KeptAt = keptAt
	return failure
}

func validateTLCOutput(output []byte) error {
	if !bytes.Contains(output, []byte(tlcInitialStateMarker)) {
		return errors.New("successful TLC process did not generate exactly one initial state")
	}
	if !bytes.Contains(output, []byte(tlcSuccessMarker)) {
		return errors.New("successful TLC process did not report completed model checking")
	}
	return nil
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("copy %s: %w", filepath.Base(source), err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("copy %s: %w", filepath.Base(source), err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return fmt.Errorf("copy %s: %w", filepath.Base(source), copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("copy %s: %w", filepath.Base(source), closeErr)
	}
	return nil
}

func preserveWorkdir(workdir, keepRoot, name string) (string, error) {
	destination := filepath.Join(keepRoot, name)
	if err := os.Mkdir(destination, 0o755); err != nil {
		return "", fmt.Errorf("preserve failed replay %s: %w", name, err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(destination)
		}
	}()
	entries, err := os.ReadDir(workdir)
	if err != nil {
		return "", fmt.Errorf("preserve failed replay %s: %w", name, err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			return "", fmt.Errorf("preserve failed replay %s: workdir entry %s is not regular", name, entry.Name())
		}
		if err := copyFile(filepath.Join(workdir, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
			return "", fmt.Errorf("preserve failed replay %s: %w", name, err)
		}
	}
	complete = true
	return destination, nil
}

// FindRoot walks upward from start until it finds this replay slice and the
// pinned TLC launcher.
func FindRoot(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("tracereplay: resolve start: %w", err)
	}
	for {
		launcher := filepath.Join(current, "dev", "scripts", "tlc.sh")
		replay := filepath.Join(current, "dev", "formal", "replay", replayModuleFile)
		if regularFile(launcher) && regularFile(replay) {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("tracereplay: repository root not found above %s", start)
		}
		current = parent
	}
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
