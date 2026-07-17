// Command verifyoracle authenticates an ephemeral raptorq source tree while
// allowing exactly the reviewed primitive-export shim in lib.rs.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	wantPackageChecksum  = "4d4215fb79ef19442a0c71616aabb0715a386e6a16ed9031775ee3e3f20e7502"
	wantManifestChecksum = "9a38844ab4ebbb6c5cd9484b6ae25cd5574180549f2827195cb395205a47439a"
	shimPath             = "src/lib.rs"
	wantShimChecksum     = "2588cb47c3bcda375ee374fc5efdd6bf567fc1bd36579e1288458c7c01d3c962"
)

type cargoChecksum struct {
	Files   map[string]string `json:"files"`
	Package string            `json:"package"`
}

type verificationPolicy struct {
	packageChecksum  string
	manifestChecksum string
	shimPath         string
	shimChecksum     string
}

var oraclePolicy = verificationPolicy{
	packageChecksum:  wantPackageChecksum,
	manifestChecksum: wantManifestChecksum,
	shimPath:         shimPath,
	shimChecksum:     wantShimChecksum,
}

func main() {
	if len(os.Args) != 2 {
		fatalf("usage: verifyoracle <raptorq-source-directory>")
	}
	count, err := verifyTree(os.Args[1], oraclePolicy)
	if err != nil {
		fatalf("verify oracle source tree: %v", err)
	}
	fmt.Printf("verified ephemeral raptorq 2.0.1 package %s (%d files, reviewed shim applied)\n", wantPackageChecksum, count)
}

func verifyTree(directory string, policy verificationPolicy) (int, error) {
	root, err := filepath.Abs(directory)
	if err != nil {
		return 0, fmt.Errorf("resolve oracle source directory: %w", err)
	}
	manifestPath := filepath.Join(root, ".cargo-checksum.json")
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil {
		return 0, fmt.Errorf("inspect checksum manifest: %w", err)
	}
	if !manifestInfo.Mode().IsRegular() {
		return 0, fmt.Errorf("checksum manifest is not a regular file")
	}
	if err := verifyFile(manifestPath, policy.manifestChecksum); err != nil {
		return 0, fmt.Errorf("verify checksum manifest: %w", err)
	}
	encoded, err := os.ReadFile(manifestPath)
	if err != nil {
		return 0, fmt.Errorf("read checksum manifest: %w", err)
	}
	var manifest cargoChecksum
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return 0, fmt.Errorf("parse checksum manifest: %w", err)
	}
	if manifest.Package != policy.packageChecksum {
		return 0, fmt.Errorf("package checksum = %q, want %q", manifest.Package, policy.packageChecksum)
	}

	seen := make(map[string]bool, len(manifest.Files))
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected non-regular file %s", relative)
		}
		if relative == ".cargo-checksum.json" {
			return nil
		}
		want, present := manifest.Files[relative]
		if !present {
			return fmt.Errorf("unexpected file %s", relative)
		}
		if relative == policy.shimPath {
			want = policy.shimChecksum
		}
		if err := verifyFile(path, want); err != nil {
			return fmt.Errorf("%s: %w", relative, err)
		}
		seen[relative] = true
		return nil
	})
	if err != nil {
		return 0, err
	}
	for path := range manifest.Files {
		if !seen[path] {
			return 0, fmt.Errorf("manifest file is missing: %s", path)
		}
	}
	return len(seen), nil
}

func verifyFile(path, want string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	checksum := sha256.Sum256(contents)
	got := hex.EncodeToString(checksum[:])
	if got != want {
		return fmt.Errorf("SHA-256 = %s, want %s", got, want)
	}
	return nil
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(1)
}
