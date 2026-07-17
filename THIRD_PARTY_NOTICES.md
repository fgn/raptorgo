# Third-party notices

The production Go module (`github.com/fgn/raptorgo` and its `internal`
packages) has no third-party dependencies. Development tooling is isolated in
a nested module and is excluded from production module downloads.

## RFC 6330 text

- Paths: `dev/spec/rfc6330.txt`, `dev/spec/`
- Source: <https://www.rfc-editor.org/rfc/rfc6330.txt>
- The RFC text is Copyright (c) 2011 IETF Trust and the persons identified
  as the document authors, and is reproduced here unmodified under the IETF
  Trust Legal Provisions (<https://trustee.ietf.org/license-info>), which
  permit unlimited reproduction and distribution of RFCs in full. The
  normative tables in `internal/rfc6330` are extracted from this text by
  `dev/internal/rfc6330/cmd/extracttables` and pinned by
  `dev/spec/SHA256SUMS`.

## Downloaded at development time (not distributed)

The following upstream source trees and binaries are fetched by `Taskfile.yml`
targets onto the developer's machine and are neither vendored in this
repository nor distributed with it:

- **raptorq 2.0.1** (<https://github.com/cberner/raptorq>, Apache-2.0):
  fetched as the crates.io archive with package checksum
  `4d4215fb79ef19442a0c71616aabb0715a386e6a16ed9031775ee3e3f20e7502`.
  `task oracle:prepare` verifies the archive and complete file manifest, then
  applies the reviewed `dev/interop/patches/raptorq-2.0.1-oracle.patch` in an
  external cache. The patch only exports existing deterministic RFC primitives
  for differential testing; it changes no algorithm or table.
- **libRaptorQ** (<https://github.com/LucaFulchir/libRaptorQ>, LGPL-3.0):
  compiled at a pinned commit as an independent C++ cross-decoding oracle by
  `task oracle:cpp`. Only the raptorgo-authored adapter
  `dev/interop/cpp-oracle/main.cpp` is distributed here.
- **tla2tools.jar** (<https://github.com/tlaplus/tlaplus>, MIT): the TLA+
  model checker used by `task verify:tla`.
- **gofumpt** (<https://github.com/mvdan/gofumpt>, BSD-3-Clause) and
  **govulncheck** (<https://golang.org/x/vuln>, BSD-3-Clause): lint
  tooling installed into the gitignored `bin/` directory.
