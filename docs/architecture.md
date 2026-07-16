# Architecture

The package follows the RFC dependency direction without exposing matrices or
intermediate symbols in the public API.

```text
ObjectEncoder / ObjectDecoder
  -> OTI, packet, source-block and sub-block layout
  -> immutable raw encoder / incremental rank-aware block decoder
  -> hybrid inactivation planner and executor
  -> sparse binary incidence plus bounded dense HDPC residual
  -> GF(256) symbols and scalar arithmetic
```

## Package ownership

- Root `raptorgo`: OTI derivation and wire encoding, resource policy, grouped
  packet views, object encoder/decoder, trust-boundary callbacks, public errors.
- `internal/rfc6330`: normative integer formulas, tables, random generators,
  tuple construction, partitioning, and ESI/ISI mapping.
- `internal/gf256`: exhaustively checked scalar arithmetic, flat symbol slabs,
  dense reference matrices, solving, rank, and inversion.
- `internal/matrix`: bounded sparse binary/GF(256) containers and permutation
  operations used by reference and trace checks.
- `internal/codec`: Figure 5 constraints, intermediate symbols, immutable
  source-block encoding, hybrid planning/execution, and incremental decoding.

## Hybrid solve

The production planner records immutable phase-one pivots, frozen columns,
elimination incidence, and row/column permutations over non-HDPC binary rows.
The executor replays that plan, materializes only the bounded residual containing
the HDPC contribution, solves it over GF(256), and substitutes in reverse.
Normal execution passes a nil observer and retains no trace data. Verification
generators use the same executor with a deep-copy observer and bind real
coefficients, right-hand sides, substitutions, input, and result into TLA
traces.

Every workspace owner exposes or internally uses a logical-backing estimator.
Constructors reject aggregate limits before retaining caller data; exact-budget
and one-byte-short tests cover the important ownership boundaries. This is a
logical backing model, not a promise about the Go runtime's allocator overhead.

## Object lifecycle

Each nonempty source block has an independent incremental decoder. A block is
complete only after a rank-sufficient solve, not merely after receiving K
packets. Once all blocks complete, reconstruction sets `ObjectDecoded`; the
mutex is released before invoking the verifier. Successful verification sets
`ObjectVerified`; only then is an owned copy passed once to the deliverer.
Failure states and successful delivery are terminal.

Callbacks may inspect or reenter the decoder because they run without its mutex
held. Concurrent final packets cannot create a second finalizer: state changes
to `ObjectDecoded` before the winning call releases the lock.

## Design constraints

- Pure Go runtime; foreign codecs are disposable development oracles.
- Deterministic bytes for a fixed OTI, source object, SBN, and ESI.
- RFC structural validity and local allocation policy return distinct errors.
- Caller-owned authentication policy and generation binding.
- Scalar semantics remain the authority for any future acceleration.
- Benchmarks must justify caching or architecture-specific complexity; neither
  is required for correctness.
