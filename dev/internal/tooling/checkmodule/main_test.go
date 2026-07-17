package main

import "testing"

func TestForbidden(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		{"dev/go.mod", true},
		{"nested/formal/model.txt", true},
		{"interop/corpus/README.md", true},
		{"oracle/main.rs", true},
		{"oracle/main.RS", true},
		{"adapter/main.cpp", true},
		{"model/Codec.tla", true},
		{"oracle/Cargo.toml", true},
		{"oracle/Cargo.lock", true},
		{"internal/development/package.go", false},
		{"docs/formality.md", false},
		{"object_encoder.go", false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()
			if got := forbidden(test.path); got != test.want {
				t.Fatalf("forbidden(%q) = %v, want %v", test.path, got, test.want)
			}
		})
	}
}

func TestAllowedProductionPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		{".github/workflows/ci.yml", true},
		{".github/dependabot.yml", true},
		{".foo/layout.yaml", true},
		{".gitignore", true},
		{"README.md", true},
		{"Taskfile.yml", true},
		{"packet.go", true},
		{"packet_test.go", true},
		{"examples/roundtrip/main.go", true},
		{"docs/invariants.md", true},
		{"internal/codec/decoder.go", true},
		{"internal/rfc6330/RFC6330-CODE-COMPONENT-LICENSE.txt", true},
		{"oracle/model.cfg", false},
		{"fixtures/corpus.json", false},
		{"internal/codec/cmd/oracle.go", false},
		{"internal/tooling/check.go", false},
		{"docs/generated/report.json", false},
		{".github/workflows/unreviewed.yml", false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()
			if got := allowedProductionPath(test.path); got != test.want {
				t.Fatalf("allowedProductionPath(%q) = %v, want %v", test.path, got, test.want)
			}
		})
	}
}
