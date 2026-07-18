package tracereplay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

func TestRunSortsInputsFailuresAndPreservesWorkdirs(t *testing.T) {
	root, err := FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	corpus := t.TempDir()
	for _, input := range []struct {
		name   string
		blocks int32
	}{
		{"zeta.ndjson", 3},
		{"alpha.ndjson", 1},
		{"middle.ndjson", 2},
	} {
		writeReceivingTrace(t, filepath.Join(corpus, input.name), input.blocks)
	}
	if err := os.WriteFile(filepath.Join(corpus, "ignored.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := t.TempDir()

	var mu sync.Mutex
	var invoked []int
	execute := func(_ context.Context, command Command) ([]byte, error) {
		if !filepath.IsAbs(command.Path) || !filepath.IsAbs(command.Dir) {
			return nil, errors.New("command path and dir must be absolute")
		}
		wantPath := filepath.Join(root, "dev", "scripts", "tlc.sh")
		if command.Path != wantPath {
			return nil, fmt.Errorf("launcher = %s, want %s", command.Path, wantPath)
		}
		wantArgs := []string{"-noGenerateSpecTE", replayModuleFile, "-config", replayConfigFile}
		if !reflect.DeepEqual(command.Args, wantArgs) {
			return nil, fmt.Errorf("args = %v, want %v", command.Args, wantArgs)
		}
		for _, name := range []string{replayModuleFile, replayConfigFile, traceSpecFile, replayDataModule + ".tla"} {
			if !regularFile(filepath.Join(command.Dir, name)) {
				return nil, fmt.Errorf("missing replay workdir file %s", name)
			}
		}
		data, err := os.ReadFile(filepath.Join(command.Dir, replayDataModule+".tla"))
		if err != nil {
			return nil, err
		}
		blocks, err := renderedBlocks(data)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		invoked = append(invoked, blocks)
		mu.Unlock()
		if blocks == 1 || blocks == 3 {
			return []byte(fmt.Sprintf("blocks %d failed\n", blocks)), errors.New("exit status 1")
		}
		return []byte(tlcInitialStateMarker + "\n" + tlcSuccessMarker + "\n"), nil
	}

	report, err := Run(context.Background(), Config{
		Root: root, Corpus: corpus, KeepFailed: keep, Jobs: 2, Exec: execute,
	})
	var failures *FailuresError
	if !errors.As(err, &failures) {
		t.Fatalf("error = %v, want FailuresError", err)
	}
	if want := []string{"alpha.ndjson", "middle.ndjson", "zeta.ndjson"}; !reflect.DeepEqual(report.Inputs, want) {
		t.Fatalf("inputs = %v, want %v", report.Inputs, want)
	}
	if report.Passed != 1 {
		t.Fatalf("passed = %d, want 1", report.Passed)
	}
	gotFailures := make([]string, len(report.Failures))
	for index, failure := range report.Failures {
		gotFailures[index] = failure.Input
		if failure.KeptAt == "" || !filepath.IsAbs(failure.KeptAt) {
			t.Fatalf("failure %s kept at %q", failure.Input, failure.KeptAt)
		}
		output, readErr := os.ReadFile(filepath.Join(failure.KeptAt, "tlc.out"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(output, failure.Output) {
			t.Fatalf("preserved output = %q, report = %q", output, failure.Output)
		}
	}
	if want := []string{"alpha.ndjson", "zeta.ndjson"}; !reflect.DeepEqual(gotFailures, want) {
		t.Fatalf("failures = %v, want %v", gotFailures, want)
	}
	if got, want := err.Error(), "tracereplay: 2 trace(s) failed: alpha.ndjson, zeta.ndjson"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	sort.Ints(invoked)
	if want := []int{1, 2, 3}; !reflect.DeepEqual(invoked, want) {
		t.Fatalf("invoked blocks = %v, want %v", invoked, want)
	}
}

func TestRunRejectsUnsafeJobCountsAndEmptyCorpus(t *testing.T) {
	root, err := FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	corpus := t.TempDir()
	for _, jobs := range []int{0, MaxJobs + 1} {
		_, err := Run(context.Background(), Config{Root: root, Corpus: corpus, Jobs: jobs})
		if err == nil || !strings.Contains(err.Error(), "jobs must be") {
			t.Fatalf("jobs %d error = %v", jobs, err)
		}
	}
	_, err = Run(context.Background(), Config{Root: root, Corpus: corpus, Jobs: 1})
	if err == nil || !strings.Contains(err.Error(), "contains no .ndjson files") {
		t.Fatalf("empty corpus error = %v", err)
	}
}

func TestDiscoverInputsBoundsDirectoryBeforeAllocation(t *testing.T) {
	corpus := t.TempDir()
	for index := 0; index <= MaxCorpusEntries; index++ {
		if err := os.Mkdir(filepath.Join(corpus, fmt.Sprintf("ignored-%04d", index)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_, err := discoverInputs(corpus)
	if err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("discovery ceiling error = %v", err)
	}
}

func TestFindRootFromNestedDirectory(t *testing.T) {
	root, err := FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "dev", "internal", "tracereplay")
	got, err := FindRoot(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("root = %s, want %s", got, root)
	}
}

func TestValidateTLCOutputRejectsVacuousSuccess(t *testing.T) {
	valid := []byte(tlcInitialStateMarker + "\n" + tlcSuccessMarker + "\n")
	if err := validateTLCOutput(valid); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		output []byte
	}{
		{"no-initial-state", []byte("Finished computing initial states: 0 distinct states generated\n" + tlcSuccessMarker)},
		{"no-completion", []byte(tlcInitialStateMarker)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateTLCOutput(test.output); err == nil {
				t.Fatal("output unexpectedly accepted")
			}
		})
	}
}

func writeReceivingTrace(t *testing.T, path string, blocks int32) {
	t.Helper()
	events := []objtrace.Event{{
		V: objtrace.Version, Op: "init", Result: "ok", State: "receiving", BindingOK: true, Blocks: &blocks,
	}}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := objtrace.WriteNDJSON(file, events); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func renderedBlocks(data []byte) (int, error) {
	const prefix = "ImplementationBlocks == "
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		var blocks int
		if _, err := fmt.Sscanf(line, prefix+"%d", &blocks); err != nil {
			return 0, err
		}
		return blocks, nil
	}
	return 0, fmt.Errorf("rendered data has no ImplementationBlocks:\n%s", data)
}
