# Codec completion audit

## Implemented path

The pure-Go path is end-to-end:

1. Parse or derive OTI parameters and validate RFC structure plus caller limits.
2. Partition the object into source blocks and sub-blocks, including `N > 1`
   symbol-major FEC layout and distributed final padding.
3. Construct Figure 5 constraints, generate intermediate symbols with the
   bounded hybrid planner/executor, and emit systematic or arbitrary repair
   ESIs.
4. Parse full, grouped, and shortened packets without copying their payload;
   canonicalize each accepted symbol to exactly T bytes.
5. Retain bounded receive equations, retry rank-aware decoding, reconstruct all
   blocks, verify the exact OTI/context/length/object binding, and deliver once.

The accepted source-block domain is K=1 through K=56,403. The ordinary suite
covers eager/hybrid boundaries and small algebraic oracles; the release suite
covers maximum-K loss/repair decoding with a parity-checked repair symbol,
K=1025 multi-loss recovery, large symbol sizes, and full-domain Rust parity.

## Closed correctness risks

- K maps to the least eligible Table 2 K'; repair ESI translation skips only
  implicit padding ISIs.
- `G_ENC` row construction and encoded-symbol generation share one canonical
  index visitor, with independent dense and Rust checks.
- Every reported bounded-corpus decode agrees with an independent
  polynomial-rank oracle and returns the original source bytes.
- Phase-one planner progress follows from exhaustive initial V-column coverage;
  a generic bounded TLA model proves witness preservation for every covered
  incidence relation and rejects a forged removal.
- Hybrid trace observations come directly from production mutation points and
  bind independently replayed binary state, coupled working right-hand sides,
  residual coefficients/RHS, dense solution, substitution operations,
  immutable input, and final result; nil observers allocate no trace storage.
- Source padding under `N > 1` is redistributed in sub-block order, validated
  in every affected symbol, and cross-decoded externally in grouped shortened
  frames.
- Decoder callbacks run unlocked at explicit states; decode-attempt,
  verification, and delivery failures, empty objects, reentry, and concurrent
  completion are tested and modeled.
- The curated mutation gate exercises every candidate in resource-limit,
  Payload-ID, scalar GF(256), and partition kernels: 281 mutants are killed and
  three behaviorally equivalent mutants are independently documented. Broader
  changed-line and scheduled campaigns cover hand-written runtime source
  without treating generated tables or formal/oracle tools as production
  mutation targets.

## Interoperability evidence

- Rust `raptorq` 2.0.1: exhaustive primitive boundary corpus, 212 ordinary raw
  vectors, 66 release vectors through K=56,403, and bidirectional full/grouped/
  shortened source and repair decoding, including a combined Z=2, N=4 frame
  with source loss and grouped repairs in every block.
- C++ libRaptorQ commit `a394b22af6e8fc742ce88eacf9af5343ecded9d7`:
  45 consensus vectors plus bidirectional repair-only and mixed loss/reorder/
  duplicate decoding at K=10/12/48. Its K=49 repair divergence is pinned as a
  negative compatibility control rather than treated as an oracle.
- Every generated corpus is checked against a committed digest or complete
  golden report.

## Recovery-evidence boundary

The powered Section 5.8 statistical profile is deliberately bounded to K'=10
and distinct sampling from a fixed preregistered 65,536-ESI pool. Its confidence
bounds apply only to that population. It is not presented as a statistical
proof of the RFC's recovery-rate requirement across all 477 Table 2 rows or the
entire 24-bit ESI domain. Exact RFC construction, full-domain primitive checks,
external encoded-symbol parity, independent bounded rank tests, and large
release solves are complementary evidence, not a replacement claim for that
unmeasured population.

## Formal boundary

TLA+ is used for finite transition properties: partitioning, mapping,
authenticated lifecycle, sparse schedule ownership/permutations, inactivation
phase accounting, planner progress, hybrid execution traces, and a two-block
public object trace. Each trace family has a deletion or forged-event control
that must fail a specific property. GF(256) arithmetic and general matrix rank
remain under exhaustive or independent Go algebra plus external parity; model
checking is not presented as a proof of byte-level interoperability.

## Publication decisions outside codec completeness

The source uses the public `github.com/fgn/raptorgo` module path and is
licensed under the Apache License 2.0. The owner completed the intended legal
review and greenlit publication on 2026-07-17. The RFC-extracted code-component
notice is retained in `internal/rfc6330`.

Operation-plan caching and architecture-specific acceleration remain optional
future performance work. They are not correctness prerequisites and must
preserve every existing oracle, invariant, race, resource, and trace gate.
