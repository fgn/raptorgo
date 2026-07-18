package tracecover

import (
	"strings"
	"testing"
)

func TestLoadTracesRejectsEmptyInputDirectory(t *testing.T) {
	_, err := LoadTraces(SourceCorpus, []string{t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "contains no .ndjson traces") {
		t.Fatalf("empty directory error = %v", err)
	}
}
