// Command verifyvendor authenticates the complete vendored raptorq source
// tree while allowing exactly the reviewed primitive-export shim in lib.rs.
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
	wantShimChecksum     = "39698ec64aabed6611c2b76c54bcc2e2cbc543aa2f36bd0c027fb6f4ce5d3ef0"
)

type cargoChecksum struct {
	Files   map[string]string `json:"files"`
	Package string            `json:"package"`
}

func main() {
	if len(os.Args) != 2 {
		fatalf("usage: verifyvendor <raptorq-vendor-directory>")
	}
	root, err := filepath.Abs(os.Args[1])
	if err != nil {
		fatalf("resolve vendor directory: %v", err)
	}
	manifestPath := filepath.Join(root, ".cargo-checksum.json")
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil {
		fatalf("inspect checksum manifest: %v", err)
	}
	if !manifestInfo.Mode().IsRegular() {
		fatalf("checksum manifest is not a regular file")
	}
	if err := verifyFile(manifestPath, wantManifestChecksum); err != nil {
		fatalf("verify checksum manifest: %v", err)
	}
	encoded, err := os.ReadFile(manifestPath)
	if err != nil {
		fatalf("read checksum manifest: %v", err)
	}
	var manifest cargoChecksum
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		fatalf("parse checksum manifest: %v", err)
	}
	if manifest.Package != wantPackageChecksum {
		fatalf("package checksum = %q, want %q", manifest.Package, wantPackageChecksum)
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
		if relative == shimPath {
			want = wantShimChecksum
		}
		if err := verifyFile(path, want); err != nil {
			return fmt.Errorf("%s: %w", relative, err)
		}
		seen[relative] = true
		return nil
	})
	if err != nil {
		fatalf("verify vendor tree: %v", err)
	}
	for path := range manifest.Files {
		if !seen[path] {
			fatalf("manifest file is missing: %s", path)
		}
	}
	fmt.Printf("verified raptorq 2.0.1 package %s (%d files, reviewed shim applied)\n", manifest.Package, len(seen))
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
