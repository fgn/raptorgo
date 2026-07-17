# Conformance and differential testing

## Authority order

Disagreements are resolved in this order:

1. RFC 6330 plus verified RFC Editor errata.
2. Direct algebraic derivation from normative formulas.
3. Reproducible agreement between independently evolved implementations.
4. A single implementation, treated only as evidence.

Round trips alone are never sufficient because an encoder and decoder can share
the same defect.

## Pinned external implementations

### Rust `cberner/raptorq`

- package: `raptorq` 2.0.1;
- package checksum:
  `4d4215fb79ef19442a0c71616aabb0715a386e6a16ed9031775ee3e3f20e7502`;
- corresponding tag commit:
  `29253866ed07b6d7de67a23597ba00b1fbb36bab`;
- license: Apache-2.0; and
- role: full-domain primitive/raw parity plus object cross-decoding.

`task oracle:prepare` downloads the immutable crate archive into an external
cache, verifies its package checksum and complete file manifest, and applies
the reviewed primitive-export shim. No upstream Rust source is checked into the
repository or included in the production Go module.

### C++ `LucaFulchir/libRaptorQ`

- repository: `https://github.com/LucaFulchir/libRaptorQ`;
- commit: `a394b22af6e8fc742ce88eacf9af5343ecded9d7`;
- archive SHA-256:
  `2cc35b7b68fb3d7624d1be2660099be75373c018e6d3cb84203a231b37485892`;
- role: third-implementation raw parity and bidirectional decoding; and
- limitation: its historical RAW API agrees where requested K is already K'.

K=10, 12, and 48 form the consensus set. K=49 source symbols agree but repair
symbols diverge from both Go and Rust, so that behavior is pinned as an expected
negative control and never used to overrule the RFC consensus.

Neither external implementation is imported, linked, or required by the Go
runtime package.

## Reproducible corpora

`dev/interop/corpus` contains compact SHA-256 manifests rather than generated
payload blobs. Tasks regenerate complete artifacts in a temporary directory,
compare independent bytes, verify the manifest, and discard the files.

- Ordinary Go/Rust raw corpus: 212 K/T/ESI vectors, including K/K' transitions
  and ESI 16,777,215.
- Release Go/Rust raw corpus: 66 vectors through K=56,403 with `T=1`, selected
  `T=7`, and `T=255`.
- Go/Rust object corpus: full source, repair-only, grouped source/repair,
  shortened `N > 1` source, multi-block/sub-block frames, and a combined
  `Z=2,N=4` frame with source loss plus grouped repairs in every block, decoded
  both ways.
- Go/C++ corpus: byte-identical K=10/12/48 repair-only and mixed frames, reverse
  order, source loss, a duplicate, and matching K-1 deficient rejection.
- Section 5.8 report: the bounded K'=10 fixed-pool profile is committed in full
  rather than represented by a digest-only artifact.

RQO1 is only a deterministic length-delimited corpus container. It is not an
additional wire protocol.

## Normative and algebraic gates

- OTI and payload-ID network byte order, reserved-octet canonicalization, and
  erratum 5548 boundaries.
- RFC Section 4.3 derivation, source/sub-block partition, and exact object
  reconstruction for combined `Z > 1`, `N > 1`, alignment, and padding.
- Strict table extraction, cardinalities, endpoints, and SHA-256 checks.
- Exhaustive K-to-K', ESI-to-ISI, GF(256), and degree-domain checks; broad
  `Rand`, `Tuple`, and index visitation checks over all 477 rows.
- Dense/sparse mutation equivalence, two-sided inversion, equation satisfaction,
  exact workspace acceptance, and transactional one-byte-short rejection.
- Independent polynomial-rank comparison with the production decoder over
  eleven Table 2 sizes at overheads 0, 1, and 2.
- Maximum-K loss/repair decoding with a Go/Rust parity-checked repair symbol and
  K=1025 multi-loss recovery in the release suite.

## Packet and parser robustness

Grouped packets must remain wholly source or repair, stay inside the 24-bit ESI
domain, and fit caller packet/symbol limits. Repair payloads are always a whole
number of T-byte symbols. Source shortening can omit only a zero suffix that is
real FEC padding for the packet's final symbol; padding in every transmitted
source symbol is validated, including redistributed padding under `N > 1`.

Parsers use capacity-bounded views, so callers cannot append through a returned
payload into bytes outside the accepted packet. Parser fuzzing and bounded
cross-product invariants cover malformed lengths, nonzero padding, source/
repair crossings, range overflow, and policy-limit rejection.

RFC-wire validity and local operational policy are separate. `RFCWireLimits`
accepts zero-length objects and empty partition results permitted by the wire
domain. Operations that need an encoding symbol reject an empty block.
`DefaultLimits` may reject otherwise syntactically valid but expensive values
with `ErrLimitExceeded`.

## Cross-decoding matrix

```text
Go encode   -> Go, Rust, C++ decode
Rust encode -> Rust, Go decode
C++ encode  -> C++, Go decode
```

The Rust leg covers complete object partitioning and packet grouping, including
combined multi-block/sub-block loss repaired in every block. The C++ leg
intentionally stays inside its independently agreeing K=10/12/48 RAW domain.
Both legs include repair-only recovery; mixed C++ sets additionally exercise
loss, reverse order, duplicates, and deficient rejection.

## Formal scope

TLC exhaustively explores small abstract models and replays implementation
traces. It checks transition order, coverage, ownership/resource counters,
permutations, planner progress, authenticated delivery, and exact trace
completion. Negative modules delete required records, corrupt bindings or
coefficients, enter phases early, or accept one-byte-short budgets; each must
fail its named invariant or temporal property.

TLA+ does not reimplement GF(256) or establish general byte-level
interoperability. Those claims remain owned by exhaustive polynomial arithmetic,
rank/equation checks, and external corpora. See `dev/formal/README.md` for the
module-by-module boundary.

## CI split

`task ci` runs formatting, vet, pure-Go dependency checks, tests, 386, race,
benchmark corpus smoke, TLA positive/negative checks, and external oracles.
`task test:release` adds maximum-domain solves, large Rust vectors, K=1025
repair recovery, and the powered recovery profile. `govulncheck` is available
as `task lint:govulncheck` and is run at final/release checkpoints.
