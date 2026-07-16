# Third-party notices

The production Go module (`github.com/fgn/raptorgo` and its `internal`
packages) has no third-party dependencies. The repository additionally
distributes the following development-time material.

## raptorq 2.0.1 (Rust crate)

- Path: `interop/vendor/raptorq-2.0.1`
- Upstream: <https://github.com/cberner/raptorq> (crates.io package
  checksum `4d4215fb79ef19442a0c71616aabb0715a386e6a16ed9031775ee3e3f20e7502`)
- License: Apache License 2.0 (see
  `interop/vendor/raptorq-2.0.1/LICENSE`)
- Modifications: `src/lib.rs` carries a prominent modification notice; the
  raptorgo project added public re-exports of existing deterministic RFC
  primitives for differential testing. No algorithm or table is changed.
  `task verify:oracle-source` authenticates the rest of the tree against the
  upstream checksum manifest.

This crate is a development-time differential oracle only. It is never
compiled into, linked against, or otherwise part of the Go module.

## RFC 6330 text

- Paths: `rfc6330.txt`, `spec/`
- Source: <https://www.rfc-editor.org/rfc/rfc6330.txt>
- The RFC text is Copyright (c) 2011 IETF Trust and the persons identified
  as the document authors, and is reproduced here unmodified under the IETF
  Trust Legal Provisions (<https://trustee.ietf.org/license-info>), which
  permit unlimited reproduction and distribution of RFCs in full. The
  normative tables in `internal/rfc6330` are extracted from this text by
  `internal/rfc6330/cmd/extracttables` and pinned by `spec/SHA256SUMS`.

## Downloaded at development time (not distributed)

The following are fetched by `Taskfile.yml` targets onto the developer's
machine and are neither vendored in this repository nor distributed with it:

- **libRaptorQ** (<https://github.com/LucaFulchir/libRaptorQ>, LGPL-3.0) —
  compiled at a pinned commit as an independent C++ cross-decoding oracle by
  `task oracle:cpp`. Only the raptorgo-authored adapter
  `interop/cpp-oracle/main.cpp` is distributed here.
- **tla2tools.jar** (<https://github.com/tlaplus/tlaplus>, MIT) — the TLA+
  model checker used by `task verify:tla`.
- **gofumpt** (<https://github.com/mvdan/gofumpt>, BSD-3-Clause) and
  **govulncheck** (<https://golang.org/x/vuln>, BSD-3-Clause) — lint
  tooling installed into the gitignored `bin/` directory.
