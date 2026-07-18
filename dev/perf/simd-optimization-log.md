# Experimental SIMD optimization log

This log records the evidence for the opt-in Go 1.26 `simd/archsimd` path.
The default build remains scalar. The SIMD implementation is selected only by
the `goexperiment.simd` build tag, checks AVX2 at runtime, and falls back to the
same scalar kernels when AVX2 is unavailable.

Two optimization rounds are recorded. Round 1 introduced the AVX2 kernels.
Round 2 replaced the multiply kernel with nibble-table lookups, removed a
hidden AVX-to-SSE transition stall, and restructured the executor's hot loops;
it also explains and reverses round 1's rejection of the nibble-shuffle
design.

## Scope and invariants

The optimized operations are the two byte-vector primitives used by hybrid
inactivation execution:

- XOR, including exact aliasing and partial-overlap snapshot semantics;
- `destination ^= coefficient * source` over RFC 6330 GF(256), whose reduction
  polynomial is 0x11d rather than the AES/GFNI polynomial 0x11b.

The public functions still reject length mismatches without a partial write.
Hazardous forward overlap retains the backward scalar traversal. Non-amd64,
default, and runtime AVX2-disabled builds retain scalar execution. The hottest
executor loops call the checked XOR/MulAdd primitives, so scalar and SIMD
builds exercise one semantic boundary.

## Current implementation

XOR walks the operands as running slices, 128 bytes (four `Uint8x32`
vectors) per main-loop iteration, then 32 bytes, then a scalar tail.

`MulAdd` uses lane-duplicated nibble product tables. Package init builds
`mulTableLow[c][n] = c*n` and `mulTableHigh[c][n] = c*(n<<4)` for every
coefficient and nibble with an independent shift-and-reduce product (no
dependence on the exp/log tables or package init order). Each call loads the
two 32-byte tables once, and each 32-byte step computes
`VPSHUFB(low, v & 0x0f) XOR VPSHUFB(high, (v >> 4) & 0x0f)` via
`PermuteOrZeroGrouped`, processing 64 bytes per main-loop iteration.

The byte-wise right shift by four is implemented as a 32-bit variable shift
(`Uint32x8.ShiftRight`, VPSRLVD) with a hoisted vector count followed by a
per-byte mask. `Uint16x16.ShiftAllRight`'s scalar count is deliberately not
used: the Go 1.26 compiler materializes that count through a legacy (non-VEX)
SSE register move inside the loop, and mixing legacy SSE writes with dirty
YMM upper state stalls every iteration (measured below).

`Uint8x32.GaloisFieldMul` is deliberately not used. In Go 1.26 it requires
AVX-512 GFNI and implements the incompatible polynomial 0x11b. The
i9-14900HX test host supports AVX2 but not AVX-512.

## Round 2 executor restructuring (build-independent)

The round 1 profile left the dense traversal path at 50% cumulative. Round 2
removed the per-element accessor traffic around it:

- the HDPC update gathers the pivot row's dense one-columns once per step
  (`HybridBinary.AppendDenseOnes` into a shared scratch), maps them to
  physical columns once, and applies them to each HDPC row through a
  bounds-checked-once `Matrix.RowView` instead of one `At`/`Set` pair per
  element per HDPC row;
- residual materialization enumerates each row's nonzero columns
  (`VColumns` plus `AppendDenseOnes`) instead of probing `Has` once per
  residual cell, and writes rows through `RowView`;
- the HDPC residual rows share one materialized logical-to-physical column
  map instead of one mapping call per cell;
- back-substitution consumes the same bulk gather instead of a callback per
  set bit.

The shared scratch is the only workspace change: exactly `4*(L-finalI)` bytes
and one steady allocation, added to `InactivationExecutionBackingBytes` with
a capacity proof in the comment, and mirrored independently in the formal
model (`ColumnGatherScratch` in `dev/formal/HybridTraceSpec.tla`, regenerated
`dev/formal/HybridTraceData.tla`). The regenerated trace diff touches only
the workspace figures; permutations, coefficients, and symbol values are
byte-identical.

## The AVX-to-SSE transition stall

The first round 2 `MulAdd` attempt used `Uint16x16.ShiftAllRight(4)` for the
high nibble and measured 2,274 ns for the 1,280-byte kernel — ten times
slower than round 1 despite replacing ~48 vector operations per 32 bytes with
~8. Instruction-level pprof attributed ~89% of samples to the two
instructions following the shift. Disassembly showed the scalar shift count
being moved into an XMM register with a legacy (non-VEX) `MOVQ` inside the
loop; with dirty YMM upper halves, each such write pays an AVX/SSE state
transition on Raptor Lake. Replacing the shift with VPSRLVD and a hoisted
vector count dropped the kernel to 78.7 ns, and running-slice loop
restructuring (which also removed per-iteration slice-header arithmetic in
XOR) reached the final numbers below.

This also explains round 1's rejected nibble-shuffle experiment: its measured
4.44 microseconds was the same class of stall, not a property of the
VPSHUFB design. The design is retained in round 2 with the stall removed.

## Environment and paired methodology

- CPU: Intel Core i9-14900HX, CPU 8 (P-core), linux/amd64;
- Go: go1.26.5;
- build: `GOEXPERIMENT=simd GOAMD64=v3` for SIMD binaries, default scalar
  build otherwise;
- execution: `taskset -c 8`, `GOMAXPROCS=1`;
- end-to-end samples: ten alternating binary pairs, one benchmark iteration;
- kernel samples: ten alternating binary pairs, 500 ms minimum.

Round 2 frozen binary hashes (base is the round 1 commit, candidate is round
2; the scalar pair isolates the build-independent executor restructuring, and
the width pair compares the same round 2 commit at both execution widths):

```text
7bed73efa0f7b39551c04203fb388ba6e8638bbcefe1cd432cc97ddb004b0d1b  pair-base-e2e.test
d1a5d994ba3fd45f2f83afbbde61e37d6d7e4decbd1b132d98d5a10c39354318  pair-cand-e2e.test
1c86d5d2a60744e3c8a157bc20b47b0957ca3c2b61d713963b9f87a613ee121a  pair-base-gf.test
d22217db1872601313fed7ef1d7b89307ce824eda2cc36bf45d542b517f170bf  pair-cand-gf.test
cb706ddf849502784bdc76048fd286a8549c185b4e504f2254f321e8d7ba9704  pair-base-scalar.test
01fbef5ae0eb9c0a4342f23d835ede07f1cdcf1857cee04844411a02639cbf67  pair-cand-scalar.test
```

Equivalent sample commands:

```sh
taskset -c 8 env GOMAXPROCS=1 ./pair-cand-e2e.test \
  -test.run '^$' \
  -test.bench '^BenchmarkObjectEncoderPacket/construct-and-repair/K=10000/T=1280$' \
  -test.benchmem -test.benchtime=1x -test.count=1

taskset -c 8 env GOMAXPROCS=1 ./pair-cand-gf.test \
  -test.run '^$' -test.bench 'BenchmarkXOR1280|BenchmarkMulAdd1280' \
  -test.benchtime=500ms -test.count=1
```

## Round 2 results

Kernels, 1,280 bytes, ten alternating 500 ms pairs against the round 1 SIMD
binary:

| Kernel | Round 1 median | Round 2 median | Round 2 range | Median paired speedup | B/op |
|---|---:|---:|---:|---:|---:|
| XOR | 58.6 ns | 21.1 ns | 21.1–21.3 ns | 2.78x | 0 |
| GF(256) `MulAdd` | 223.2 ns | 46.0 ns | 45.4–46.9 ns | 4.86x | 0 |

End-to-end, K=10,000, T=1280, ten alternating one-iteration pairs:

| Pairing | Workload | Base median (range) | Candidate median (range) | Median paired speedup |
|---|---|---:|---:|---:|
| Round 1 SIMD vs round 2 SIMD | Construct + first repair | 233.3 ms (231.0–234.9) | 119.8 ms (118.4–121.5) | 1.93x |
| Round 1 SIMD vs round 2 SIMD | Decode, 10% loss + 4 overhead | 230.8 ms (229.4–234.6) | 117.6 ms (112.2–120.7) | 1.96x |
| Round 1 scalar vs round 2 scalar | Construct + first repair | 549.4 ms (548.0–551.2) | 487.0 ms (484.9–494.4) | 1.13x |
| Round 1 scalar vs round 2 scalar | Decode, 10% loss + 4 overhead | 570.2 ms (564.1–575.4) | 508.6 ms (505.0–512.4) | 1.12x |
| Round 2 scalar vs round 2 SIMD | Construct + first repair | 487.4 ms (486.0–488.7) | 117.5 ms (116.6–119.6) | 4.15x |
| Round 2 scalar vs round 2 SIMD | Decode, 10% loss + 4 overhead | 509.2 ms (504.1–513.1) | 112.2 ms (111.4–118.7) | 4.52x |

Construct-plus-repair moved from 105,204,320 B/op and 125 steady allocations
to 105,205,216 B/op and 126; decode moved from 122,861,984 B/op and 10,073 to
122,862,880 B/op and 10,074. The delta is exactly the accounted 896-byte
(`4*(L-finalI)`, L-finalI=224 here) column-gather scratch and its one
allocation.

Profiles after round 2 show the round 1 hotspots resolved: the
`ForEachDenseOne`-driven HDPC update (45.7% cumulative, with `Matrix.Set`,
`Matrix.index`, and `PhysicalColumn` totaling ~21% flat) no longer appears;
remaining time is dominated by the symbol XOR/MulAdd algebra itself and by
contract-required copies (`Symbols.SetSymbol` under the D clone, residual
materialization, and result assembly), with plan construction around 8%.

## Round 1 results (historical)

Round 1 measured, against the pre-SIMD scalar binary:

| Kernel, 1,280 bytes | Scalar median | SIMD median | Median paired speedup |
|---|---:|---:|---:|
| XOR | 361.0 ns | 58.48 ns | 6.18x |
| GF(256) `MulAdd` | 1,002.5 ns | 223.1 ns | 4.49x |

| K=10,000, T=1280 workload | Scalar median | SIMD median | Median paired speedup |
|---|---:|---:|---:|
| Construct + first repair | 644.8 ms | 233.3 ms | 2.77x |
| Decode, 10% loss + 4 overhead | 669.7 ms | 227.1 ms | 2.95x |

Round 1 frozen binary hashes:

```text
969d23e831c24869a39a66093a0beb3f074f6089f79ca5383bf8cefb601ece40  raptorgo-simd-base.test
2e5443284dff1b3804e4fefd50aadd06e6d948d3da687ca273ba2fa5a3d364fc  raptorgo-simd-candidate-final.test
61d7fb08b4a164bdbf8953dc4fa55e9c4e87bd760cdfafa101154755571fbabf  gf256-scalar.test
41b499d8a20672db7d0263e136915e3373fd9c871719dcc90db70aff8cb9db61  gf256-simd-final.test
```

## Validation

The round 2 validation matrix:

- exhaustive coefficient/value comparison against independent polynomial
  arithmetic in scalar and SIMD builds, now at length 295 so every
  coefficient crosses the unrolled loop, the single-vector loop, and the
  scalar tail;
- vector-boundary and tail lengths from 0 through 1,280 bytes across
  separate, exact-alias, destination-before-source, and
  destination-after-source layouts;
- new unit coverage for `Matrix.RowView` aliasing/bounds and
  `HybridBinary.AppendDenseOnes` equivalence with `ForEachDenseOne` across
  freeze and mutation states;
- `task lint`, `task test` (production and dev modules), `task test:simd`
  including the `GODEBUG=cpu.avx2=off` runtime fallback, `task test:race`,
  `task test:386`, `task test:checkptr`, `task fuzz:smoke`,
  `task bench:simd:smoke`;
- `task verify:generated` and `task verify:static` after regenerating
  `HybridTraceData.tla`; `task verify:tla` and `task verify:tla:negative`
  after extending `ExpectedExecutionWorkspace` with `ColumnGatherScratch`;
  `task verify:trace:corpus` and `task verify:trace:negative`;
- `task mutation:gate` over the curated boundary and algebra kernels.

Reproduction entry points:

```sh
task test:simd
task bench:simd:smoke
task bench:simd
```
