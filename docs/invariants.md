# Verification invariant catalog

Every important property needs both an owner and a checking mechanism. An
invariant without an executable check is only documentation; a test without a
named property is difficult to audit.

## Arithmetic and partitioning

| ID | Property | Enforcement |
| --- | --- | --- |
| GF-1 | Addition is XOR and is self-inverse. | Exhaustive Go test over all octets. |
| GF-2 | Multiplication agrees with independent polynomial reduction modulo `0x11d`. | Exhaustive Go test over all octet pairs. |
| GF-3 | Nonzero octets form a multiplicative group. | Exhaustive identity, inverse, associativity tests. |
| GF-4 | Multiplication distributes over addition. | Exhaustive Go test over all octet triples. |
| PART-1 | `JL + JS = J`. | Go bounded/exhaustive tests and TLC. |
| PART-2 | `IL*JL + IS*JS = I`. | Go bounded/exhaustive tests and TLC. |
| PART-3 | Partition sizes differ by at most one. | Go bounded/exhaustive tests and TLC. |
| PART-4 | Large partitions precede small partitions and `JL = I % J`. | Go indexed materialization test and TLC `PartSize`. |
| PART-5 | Integer arithmetic cannot overflow accepted RFC domains. | OTI boundary tests, quotient/remainder formulas, and checked constructors. |

## Wire, packet, and source-layout boundaries

| ID | Property | Enforcement |
| --- | --- | --- |
| WIRE-1 | OTI and FEC Payload IDs use canonical network-byte-order serialization; the reserved OTI octet is ignored on input and written as zero. | Known-byte vectors, round trips, malformed lengths, and parser fuzzing. |
| WIRE-2 | RFC-invalid fields return `ErrInvalidOTI`; syntactically valid configurations rejected by application policy return `ErrLimitExceeded`. | Table-driven structural and policy-limit tests. |
| WIRE-3 | Public source-block counts are validated before narrowing and never exceed K'_max. | Maximum-F invalid-block and RFC-edge partition tests. |
| WIRE-4 | RFC Section 4.3 derivation selects `T=P'`, the specified `Z`, and the least eligible `N`, while preserving positive wire partitions. | Exact examples, all Table-2 predecessor lookup boundaries, invalid-input tests, and policy-limit rejection. |
| PACKET-1 | Grouped packets contain consecutive source-only or repair-only ESIs within the 24-bit domain. | Grouped source/repair vectors and boundary rejection tests. |
| PACKET-2 | A shortened final source symbol canonicalizes to `T` bytes with zero padding; transmitted full padding must already be zero. | Raw-view, canonical-copy, and poisoning-rejection tests. |
| LAYOUT-1 | Large source blocks and sub-blocks precede small ones; `N > 1` symbols concatenate the same-index sub-symbol from each sub-block. | Fixed vectors plus a bounded cross-product round-trip invariant. |
| LAYOUT-2 | Multi-`Z`, multi-`N`, alignment, final padding, empty RFC partitions, and per-operation memory limits preserve exact layout without out-of-bounds allocation. | Combined fixed vector, bounded invariant sweep, and limit tests. |

## Decoder lifecycle

The abstract lifecycle is complemented by traces emitted from the concrete
public object decoder and its unlocked callbacks.

| ID | Property | Enforcement |
| --- | --- | --- |
| DEC-1 | Accepted symbol IDs are always within the configured domain. | Go state-machine tests and TLC `AcceptedInDomain`. |
| DEC-2 | Accepted symbols only accumulate; duplicates are idempotent. | Go tests and separate TLC properties `AcceptedMonotonic`, `AcceptTransitionExact`, and `DuplicateAcceptIsIdempotent`. |
| DEC-3 | Decode cannot succeed before the abstract readiness predicate holds. | Go tests, TLC `NoPrematureDecode`, negative trace. |
| DEC-4 | Delivery is reachable only after successful decode and authenticated verification. | Go tests, historical trace invariant, early-delivery negative control, and TLC `DeliveredAfterVerification`. |
| DEC-5 | Delivered, aborted, and verification-failed states are terminal. | Go tests and TLC `TerminalStable`. |
| DEC-6 | Rejected IDs do not mutate decoder state. | Go tests and positive trace containing a rejection. |
| DEC-7 | Recorded Go transitions are allowed by the TLA+ model. | Generated trace replay through TLC. |
| DEC-8 | Every required action remains in `Next`, and every `Next` transition is one of the independently specified exact outcomes. | TLC `NonTerminalCanProgress`, `EveryNextTransitionAllowed`, and a forged-delivery fault-injection control that must fail the exact action property. |
| DEC-9 | Failed pre-ready, wrong-phase, and post-terminal calls preserve the projected state. | Go-generated rejected-call trace replay through TLC. |
| DEC-10 | The public object decoder exposes `decoded` before verification, `verified` before delivery, and a terminal `decode-failed` state after an unrecoverable bounded attempt. | Production-generated success/failure traces, exact callback counters, terminal re-add checks, and forged-state negative controls. |

The lifecycle model uses `Cardinality(accepted) >= Required` only as an abstract
readiness predicate. The concrete codec uses matrix rank/solvability and is
checked independently against a polynomial rank oracle; cardinality alone is
never treated as RaptorQ sufficiency.

## RFC primitives and codec

The following invariants are executable gates:

| ID | Property | Enforcement |
| --- | --- | --- |
| TAB-1 | V0-V3 contain exactly 256 entries each, Table 1 contains all 31 thresholds, and Table 2 contains 477 ordered rows. | Compile-time-sized arrays, known endpoints, canonical SHA-256 tests, strict extraction from the pinned RFC, and a generated-artifact freshness gate. |
| GEN-1 | `Rand`, `Deg`, and `Tuple` are deterministic, wrap at the RFC boundaries, and stay within every specified field domain. | Reproducible pinned-Rust comparison over 65,541 Rand cases, every Deg input at four W values, seven tuple IDs for all 477 rows, Go known answers/domain checks, and fuzz tests. |
| GEN-2 | K maps to the least Table 2 K' at least K; source ESI equals ISI and repair ESI skips exactly K'-K padding IDs. | Exhaustive Go tests, reproducible Go/Rust comparison of all 56,403 mappings and their source/repair boundaries, bounded TLC exploration, Go-generated traces across all Table 2 transitions, and a forged-mapping negative trace. |
| SYM-1 | Symbol XOR, scaling, and multiply-add agree with independent polynomial reduction and reject unequal lengths before mutation. | Exhaustive coefficient/octet comparison, alias tests, mutation-on-error tests, and fuzzing. |
| MAT-1 | Every row operation preserves the represented linear system. | Byte-budgeted dense reference matrix, randomized sparse mutation properties, and implementation-derived hybrid residual/substitution equation checks. |
| MAT-2 | Successful inversion produces both left and right identity. | Randomized and exhaustive small-matrix tests. |
| CODEC-1 | Systematic ESI payloads equal source symbols exactly. | Direct and cross-implementation tests. |
| CODEC-2 | A reported successful decode satisfies every received equation. | Independent rank/equation corpora, production-observed residual checks, dense differential tests, and external cross-decoding. |
| CODEC-3 | Go/Rust outputs match over the committed ordinary, release, and object corpora; Go/Rust/C++ output equality is limited to the C++ K=10/12/48 consensus domain, with K=49 repair divergence retained as a negative control. | Committed differential corpora with exact artifact digests. |
| CODEC-4 | Every independently rank-sufficient received system decodes. | Independent polynomial-rank oracle across eleven bounded Table 2 sizes and overheads 0/1/2, plus large hybrid loss/repair release cases. |
| CODEC-5 | Observed recovery rates are characterized against RFC 6330 Section 5.8 within an explicit bounded population. | Golden preregistered K'=10 fixed-65,536-ESI-pool profile with 100,000/100,000/3,000,000 trials and one-sided 95% confidence bounds; it does not establish the all-477-row SHALL property. |
| MUT-1 | Tests reject plausible boundary, arithmetic, branch, return-value, and error-handling defects in the curated resource/wire/GF/partition kernels. | Pinned `go-mutesting/v2` CI gate with all non-equivalent mutants killed, a reviewed three-mutant equivalence baseline, and separate changed-line and broad agent-reporting campaigns. |

## Evidence policy

- Each production algorithm cites its RFC section.
- Tests use an independent formulation whenever practical; tests that repeat
  production logic do not count as independent evidence.
- TLC models are bounded and do not prove unbounded arithmetic or codec
  interoperability.
- Trace validation proves only that recorded executions conform to the model.
- Every trace checker has a malformed negative control that must be rejected.
