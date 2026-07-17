# Implementation roadmap

## Completed codec milestones

- [x] Pin RFC 6330, verified erratum 5548, tables, extraction, and checksums.
- [x] Implement exhaustive GF(256), partition, mapping, tuple, and wire checks.
- [x] Implement bounded dense/sparse matrix substrate and algebraic oracles.
- [x] Implement full-domain hybrid intermediate-symbol generation and decoding.
- [x] Implement immutable systematic/repair encoding for the 24-bit ESI range.
- [x] Implement grouped and shortened packet handling, including `N > 1`
  distributed padding.
- [x] Implement RFC Section 4.3 example parameter derivation.
- [x] Implement incremental multi-block object decoding and authenticated
  verify-before-deliver callbacks.
- [x] Add production-derived hybrid and two-block object TLA traces with
  adversarial mutations.
- [x] Add byte-identical Rust parity and bidirectional Rust/C++ cross-decoding.
- [x] Add independent rank-sufficiency and explicitly bounded K'=10 RFC Section
  5.8 empirical evidence; retain the all-row statistical boundary in the audit.
- [x] Add benchmarks by K, T, loss, and overhead.
- [x] Emit contiguous and sub-blocked systematic packets directly in
  O(packet bytes), with a zero-allocation caller-buffer API.
- [x] Add a pinned mutation-testing CI gate plus changed-line and scheduled
  discovery campaigns with a reviewed equivalence baseline.
- [x] Pass amd64, 386, race, checkptr, fuzz-smoke, formal, oracle, and release
  gates.

## Publication checklist

- [x] Choose the final public repository/module path:
  `github.com/fgn/raptorgo`.
- [x] Select the project license: Apache License 2.0.
- [x] Complete the intended patent/legal review; the owner greenlit publication
  on 2026-07-17.
- [x] Isolate all development tooling in a nested module and enforce a small,
  production-only module ZIP in CI.
- [ ] Add public release/version automation.

## Optional optimization work

- [ ] Benchmark an immutable operation-plan cache keyed by K' before adding it.
- [ ] Evaluate pure-Go architecture-specific GF(256) acceleration behind the
  exhaustively checked scalar semantics.
- [ ] Tune sparse/dense crossover limits using representative production data.

Optimization is not part of codec completeness. Any future path must continue
to pass the same byte corpus, rank oracle, allocation bounds, race tests, and
formal negative controls.
