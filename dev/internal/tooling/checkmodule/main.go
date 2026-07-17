// Command checkmodule builds the exact production module ZIP from tracked and
// intended files, then enforces its dev-content and size invariants.
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"
)

const (
	modulePath    = "github.com/fgn/raptorgo"
	moduleVersion = "v0.0.0"
	maximumBytes  = 256 << 10
)

func main() {
	root, err := filepath.Abs("..")
	if err != nil {
		fatalf("resolve production module root: %v", err)
	}
	staging, err := os.MkdirTemp("", "raptorgo-module-source.*")
	if err != nil {
		fatalf("create staging directory: %v", err)
	}
	defer os.RemoveAll(staging)

	files := repositoryFiles(root)
	for _, relative := range files {
		source := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(source)
		if os.IsNotExist(err) {
			continue // Staged deletion.
		}
		if err != nil {
			fatalf("inspect %s: %v", relative, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		destination := filepath.Join(staging, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			fatalf("create parent for %s: %v", relative, err)
		}
		if err := copyFile(destination, source, info.Mode().Perm()); err != nil {
			fatalf("stage %s: %v", relative, err)
		}
	}

	archive, err := os.CreateTemp("", "raptorgo-module.*.zip")
	if err != nil {
		fatalf("create module archive: %v", err)
	}
	archivePath := archive.Name()
	defer os.Remove(archivePath)
	if err := modzip.CreateFromDir(archive, module.Version{Path: modulePath, Version: moduleVersion}, staging); err != nil {
		fatalf("create module ZIP: %v", err)
	}
	if err := archive.Close(); err != nil {
		fatalf("close module ZIP: %v", err)
	}

	info, err := os.Stat(archivePath)
	if err != nil {
		fatalf("inspect module ZIP: %v", err)
	}
	if info.Size() > maximumBytes {
		fatalf("production module ZIP is %d bytes; limit is %d", info.Size(), maximumBytes)
	}

	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		fatalf("open module ZIP: %v", err)
	}
	defer reader.Close()
	prefix := modulePath + "@" + moduleVersion + "/"
	present := make(map[string]bool, len(reader.File))
	for _, file := range reader.File {
		relative := strings.TrimPrefix(file.Name, prefix)
		present[relative] = true
		if forbidden(relative) {
			fatalf("production module ZIP contains dev-only path %s", relative)
		}
		if !allowedProductionPath(relative) {
			fatalf("production module ZIP contains path outside the explicit allowlist: %s", relative)
		}
	}
	for _, required := range []string{
		"CONTRIBUTING.md",
		"LICENSE",
		"NOTICE",
		"README.md",
		"SECURITY.md",
		"THIRD_PARTY_NOTICES.md",
		"go.mod",
		"doc.go",
		"object_encoder.go",
		"object_decoder.go",
	} {
		if !present[required] {
			fatalf("production module ZIP is missing %s", required)
		}
	}
	fmt.Printf("verified production module ZIP: %d files, %d bytes, no dev-only content\n", len(reader.File), info.Size())
}

func allowedProductionPath(path string) bool {
	switch path {
	case ".foo/layout.yaml", ".foo/workspace.yaml", ".github/dependabot.yml", ".github/workflows/ci.yml",
		".github/workflows/nightly.yml", ".gitignore", "examples/roundtrip/main.go":
		return true
	}
	segments := strings.Split(path, "/")
	if len(segments) == 1 {
		switch path {
		case "CONTRIBUTING.md", "LICENSE", "NOTICE", "README.md", "SECURITY.md",
			"THIRD_PARTY_NOTICES.md", "Taskfile.yml", "go.mod":
			return true
		default:
			return filepath.Ext(path) == ".go"
		}
	}
	if len(segments) == 2 && segments[0] == "docs" {
		return filepath.Ext(path) == ".md"
	}
	if len(segments) != 3 || segments[0] != "internal" {
		return false
	}
	switch segments[1] {
	case "codec", "gf256", "matrix":
		return filepath.Ext(path) == ".go"
	case "rfc6330":
		return filepath.Ext(path) == ".go" || path == "internal/rfc6330/RFC6330-CODE-COMPONENT-LICENSE.txt"
	default:
		return false
	}
}

func repositoryFiles(root string) []string {
	command := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	encoded, err := command.Output()
	if err != nil {
		fatalf("list repository files: %v", err)
	}
	fields := bytes.Split(encoded, []byte{0})
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) != 0 {
			result = append(result, string(field))
		}
	}
	return result
}

func forbidden(path string) bool {
	segments := strings.Split(path, "/")
	for _, segment := range segments {
		switch segment {
		case "dev", "formal", "interop":
			return true
		}
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".rs", ".cpp", ".tla":
		return true
	}
	base := filepath.Base(path)
	return base == "Cargo.toml" || base == "Cargo.lock"
}

func copyFile(destination, source string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(1)
}
