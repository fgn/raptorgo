package harvest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFuzzCandidates(t *testing.T) {
	candidates, err := FuzzCandidates(
		filepath.Join("..", "..", "testdata", "fuzz", "FuzzObjectCodecSourcePacketRoundTrip"),
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 {
		t.Fatalf("candidates=%d, want 3", len(candidates))
	}
	for index, candidate := range candidates {
		if len(candidate.Events) == 0 {
			t.Fatalf("candidate %d has no events", index)
		}
	}
}

func TestReadFuzzSeedRejectsMalformedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed")
	if err := os.WriteFile(path, []byte("go test fuzz v1\n[]byte(\"x\")\nuint8(1)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readFuzzSeed(path); err == nil {
		t.Fatal("malformed fuzz seed unexpectedly accepted")
	}
}

func TestFuzzCorpusBoundsFilesBeforeReading(t *testing.T) {
	directory := t.TempDir()
	oversize := filepath.Join(directory, "oversize")
	if err := os.WriteFile(oversize, make([]byte, maxFuzzCorpusBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readFuzzSeed(oversize); err == nil {
		t.Fatal("oversize fuzz seed unexpectedly accepted")
	}

	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "linked-seed")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := filesBelow(directory); err == nil {
		t.Fatal("symlinked fuzz seed unexpectedly accepted")
	}
}
