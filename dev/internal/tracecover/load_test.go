package tracecover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTracesRejectsEmptyInputDirectory(t *testing.T) {
	_, err := LoadTraces(SourceCorpus, []string{t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "contains no .ndjson traces") {
		t.Fatalf("empty directory error = %v", err)
	}
}

func TestLoadTracesRejectsSymlinks(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.ndjson")
	if err := os.WriteFile(target, []byte("not reached"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "linked.ndjson")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTraces(SourceCorpus, []string{link}); err == nil || !strings.Contains(err.Error(), "not an .ndjson file") {
		t.Fatalf("symlink trace error = %v", err)
	}
}
