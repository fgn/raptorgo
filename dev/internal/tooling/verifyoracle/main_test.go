package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyTree(t *testing.T) {
	t.Parallel()
	root, policy := oracleFixture(t, "package-checksum")
	count, err := verifyTree(root, policy)
	if err != nil {
		t.Fatalf("verify valid fixture: %v", err)
	}
	if count != 2 {
		t.Fatalf("verified file count = %d, want 2", count)
	}
}

func TestVerifyTreeRejectsTampering(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		prepare func(t *testing.T, root string, policy *verificationPolicy)
		want    string
	}{
		{
			name: "modified file",
			prepare: func(t *testing.T, root string, _ *verificationPolicy) {
				writeTestFile(t, filepath.Join(root, "ordinary.txt"), []byte("modified"))
			},
			want: "ordinary.txt: SHA-256",
		},
		{
			name: "missing file",
			prepare: func(t *testing.T, root string, _ *verificationPolicy) {
				removeTestFile(t, filepath.Join(root, "ordinary.txt"))
			},
			want: "manifest file is missing: ordinary.txt",
		},
		{
			name: "extra file",
			prepare: func(t *testing.T, root string, _ *verificationPolicy) {
				writeTestFile(t, filepath.Join(root, "extra.txt"), []byte("extra"))
			},
			want: "unexpected file extra.txt",
		},
		{
			name: "symlinked file",
			prepare: func(t *testing.T, root string, _ *verificationPolicy) {
				removeTestFile(t, filepath.Join(root, "ordinary.txt"))
				if err := os.Symlink("src/lib.rs", filepath.Join(root, "ordinary.txt")); err != nil {
					t.Fatal(err)
				}
			},
			want: "unexpected non-regular file ordinary.txt",
		},
		{
			name: "modified shim",
			prepare: func(t *testing.T, root string, _ *verificationPolicy) {
				writeTestFile(t, filepath.Join(root, "src/lib.rs"), []byte("modified"))
			},
			want: "src/lib.rs: SHA-256",
		},
		{
			name: "bad manifest hash",
			prepare: func(_ *testing.T, _ string, policy *verificationPolicy) {
				policy.manifestChecksum = strings.Repeat("0", 64)
			},
			want: "verify checksum manifest: SHA-256",
		},
		{
			name: "bad package checksum",
			prepare: func(t *testing.T, root string, policy *verificationPolicy) {
				manifest := readTestManifest(t, root)
				manifest.Package = "attacker-package"
				encoded, err := json.Marshal(manifest)
				if err != nil {
					t.Fatal(err)
				}
				writeTestFile(t, filepath.Join(root, ".cargo-checksum.json"), encoded)
				policy.manifestChecksum = testChecksum(encoded)
			},
			want: "package checksum = \"attacker-package\"",
		},
		{
			name: "symlinked manifest",
			prepare: func(t *testing.T, root string, _ *verificationPolicy) {
				removeTestFile(t, filepath.Join(root, ".cargo-checksum.json"))
				if err := os.Symlink("ordinary.txt", filepath.Join(root, ".cargo-checksum.json")); err != nil {
					t.Fatal(err)
				}
			},
			want: "checksum manifest is not a regular file",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root, policy := oracleFixture(t, "package-checksum")
			test.prepare(t, root, &policy)
			_, err := verifyTree(root, policy)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("verifyTree error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func oracleFixture(t *testing.T, packageChecksum string) (string, verificationPolicy) {
	t.Helper()
	root := t.TempDir()
	ordinary := []byte("ordinary")
	shim := []byte("reviewed shim")
	writeTestFile(t, filepath.Join(root, "ordinary.txt"), ordinary)
	writeTestFile(t, filepath.Join(root, "src/lib.rs"), shim)
	manifest := cargoChecksum{
		Files: map[string]string{
			"ordinary.txt": testChecksum(ordinary),
			"src/lib.rs":   testChecksum([]byte("upstream lib")),
		},
		Package: packageChecksum,
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, ".cargo-checksum.json"), encoded)
	return root, verificationPolicy{
		packageChecksum:  packageChecksum,
		manifestChecksum: testChecksum(encoded),
		shimPath:         "src/lib.rs",
		shimChecksum:     testChecksum(shim),
	}
}

func readTestManifest(t *testing.T, root string) cargoChecksum {
	t.Helper()
	encoded, err := os.ReadFile(filepath.Join(root, ".cargo-checksum.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest cargoChecksum
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func writeTestFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatal(err)
	}
}

func removeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func testChecksum(contents []byte) string {
	checksum := sha256.Sum256(contents)
	return hex.EncodeToString(checksum[:])
}
