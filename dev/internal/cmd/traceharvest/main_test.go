package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/fgn/raptorgo/dev/internal/objtrace"
)

func TestRunWritesStableFingerprintNamedCorpus(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	var firstLog, secondLog bytes.Buffer
	if err := run([]string{"-output", first}, &firstLog, &firstLog); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-output", second}, &secondLog, &secondLog); err != nil {
		t.Fatal(err)
	}
	if firstLog.String() != secondLog.String() {
		t.Fatalf("logs differ:\nfirst:\n%s\nsecond:\n%s", firstLog.String(), secondLog.String())
	}
	firstFiles := corpusFiles(t, first)
	secondFiles := corpusFiles(t, second)
	if len(firstFiles) < minimumCuratedTraces || !reflect.DeepEqual(firstFiles, secondFiles) {
		t.Fatalf("corpus files differ: %v vs %v", firstFiles, secondFiles)
	}
	for _, name := range firstFiles {
		left, err := os.ReadFile(filepath.Join(first, name))
		if err != nil {
			t.Fatal(err)
		}
		right, err := os.ReadFile(filepath.Join(second, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(left, right) {
			t.Fatalf("%s differs between runs", name)
		}
		events, err := objtrace.ReadNDJSON(bytes.NewReader(left))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		fingerprint, err := objtrace.ShortFingerprint(events)
		if err != nil {
			t.Fatal(err)
		}
		if name != fingerprint+".ndjson" {
			t.Fatalf("file %s has fingerprint %s", name, fingerprint)
		}
	}
	if !strings.Contains(firstLog.String(), "summary candidates=") || !strings.Contains(firstLog.String(), "drop ") {
		t.Fatalf("incomplete log:\n%s", firstLog.String())
	}
}

func TestRunRejectsMissingOutputAndTooSmallCap(t *testing.T) {
	if err := run(nil, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "-output") {
		t.Fatalf("missing-output error = %v", err)
	}
	if err := run([]string{"-output", t.TempDir(), "-max", "1"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil ||
		!strings.Contains(err.Error(), "feature obligations") {
		t.Fatalf("small-cap error = %v", err)
	}
	if err := run(
		[]string{"-output", t.TempDir(), "-cache", t.TempDir()},
		&bytes.Buffer{}, &bytes.Buffer{},
	); err == nil || !strings.Contains(err.Error(), "-cache requires -fuzz-corpus") {
		t.Fatalf("cache-without-fuzz error = %v", err)
	}
}

func TestRunIncludesCheckedFuzzCorpus(t *testing.T) {
	output := t.TempDir()
	fuzzDir := filepath.Join("..", "..", "..", "testdata", "fuzz", "FuzzObjectCodecSourcePacketRoundTrip")
	var log bytes.Buffer
	if err := run(
		[]string{"-output", output, "-fuzz-corpus", "-fuzz-dir", fuzzDir},
		&log, &log,
	); err != nil {
		t.Fatal(err)
	}
	if len(corpusFiles(t, output)) < minimumCuratedTraces {
		t.Fatal("fuzz-corpus harvest produced too few curated traces")
	}
	if !strings.Contains(log.String(), "summary candidates=30") {
		t.Fatalf("fuzz candidates absent from log:\n%s", log.String())
	}
}

func corpusFiles(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".ndjson" {
			t.Fatalf("unexpected corpus entry %s", entry.Name())
		}
		result = append(result, entry.Name())
	}
	sort.Strings(result)
	return result
}
