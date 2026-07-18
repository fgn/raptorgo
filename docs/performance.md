# Performance

Measured numbers for user-relevant configurations, with the exact command to
reproduce them. The default implementation is single-threaded per operation
and uses exhaustively checked scalar GF(256) arithmetic; no SIMD or assembly.
An opt-in experimental amd64 SIMD path is documented separately below. Treat
these as honest baselines, not marketing claims.

## Environment

- CPU: AMD Ryzen 5 3600 (6 cores, 12 threads), linux/amd64
- Go: go1.26.5
- Date: 2026-07-18
- Command:

```sh
go test -run '^$' -bench '^BenchmarkObject' -benchmem .
# or: task bench
```

The suite also contains small-symbol conformance strata (T=7, 31, 64) that
exercise parameter-table seams; the tables below show the transport strata
with a network-realistic symbol size of T=1280 bytes and one source block.

## Experimental amd64 SIMD path

Go 1.26 exposes architecture-specific intrinsics through the experimental
`simd/archsimd` package. Raptorgo uses 256-bit AVX2 operations only when built
with `GOEXPERIMENT=simd` and when the runtime CPU check succeeds. Default,
non-amd64, and AVX2-disabled builds use the exhaustively checked scalar path.
The experimental package is outside the Go 1 compatibility promise, so the
SIMD implementation and its build tags are deliberately isolated from the
public API.

```sh
task test:simd
task bench:simd
# Equivalent benchmark environment:
GOEXPERIMENT=simd GOAMD64=v3 go test -run '^$' -bench '^BenchmarkObject' -benchmem .
```

Paired results below were collected on an Intel Core i9-14900HX with Go
1.26.5. All binaries used `GOMAXPROCS=1` and CPU 8; SIMD binaries added
`GOEXPERIMENT=simd` and `GOAMD64=v3`. Ten one-iteration samples alternated
frozen binary pairs so frequency and load drift were paired rather than
compared across separate sessions. Scalar and SIMD columns are the same
commit, so the difference is execution width alone.

| K=10,000, T=1280 workload | Scalar median (range) | SIMD median (range) | Paired speedup |
|---|---:|---:|---:|
| Construct + first repair | 487.4 ms (486.0–488.7) | 117.5 ms (116.6–119.6) | 4.15x |
| Decode, 10% loss + 4 overhead | 509.2 ms (504.1–513.1) | 112.2 ms (111.4–118.7) | 4.52x |

The 1,280-byte kernel medians from ten alternating 500 ms samples were 21.1
ns for AVX2 XOR (61 GB/s through the destination) and 46.0 ns for AVX2
GF(256) `MulAdd` via lane-duplicated nibble product tables, against 361.0 ns
and 1,002.5 ns for the exhaustively checked scalar kernels. Both kernels
remain zero allocation. Relative to the first SIMD round (233.3 ms construct
plus repair, 227.1 ms decode, 58.5 ns XOR, 223.1 ns `MulAdd`), this round is
another 1.93–1.96x end to end from replacing the polynomial-step multiply
with VPSHUFB table lookups, removing a hidden AVX-to-SSE transition stall,
and restructuring the HDPC, residual-materialization, and back-substitution
loops around bulk row views; the loop restructuring also carries the default
scalar build down from 549.4 ms and 570.2 ms (1.13x). Workspace accounting
grew by exactly one shared column-gather scratch of `4*(L-finalI)` bytes and
one steady allocation, mirrored in the formal workspace model. Full binary
hashes, commands, profiles, rejected experiments, and gate results are in
[`dev/perf/simd-optimization-log.md`](../dev/perf/simd-optimization-log.md).

## Encoding, T=1280

| K      | Object    | Construct + first repair | Cached repair packet | Systematic packet |
|-------:|----------:|-------------------------:|---------------------:|------------------:|
| 10     | 12.8 KB   | 1.0 ms (12.4 MB/s)       | 5.8 µs (220 MB/s)    | 0.35 µs            |
| 100    | 128 KB    | 6.2 ms (20.6 MB/s)       | 9.3 µs (137 MB/s)    | 0.36 µs            |
| 1,000  | 1.28 MB   | 73 ms (17.6 MB/s)        | 6.6 µs (194 MB/s)    | 0.36 µs            |
| 10,000 | 12.8 MB   | 0.93 s (13.7 MB/s)       | 4.8 µs (269 MB/s)    | 0.39 µs            |

- **Construct + first repair** is the one-time cost per source block: building
  the encoder and solving the repair plan (throughput in object bytes).
- **Cached repair packet** is the steady-state cost per repair symbol once the
  block's plan is cached: a few microseconds per 1,280-byte symbol at 2
  allocations per packet, independent of K. The per-K spread in this column is
  run-to-run scheduler noise, not a K effect.
- **Systematic packet** scatters only the requested bytes directly from the
  immutable object, including RFC sub-block transposition and shortened final
  symbols. Its cost is independent of K, with one caller-owned result
  allocation per call. `PacketInto` with a reusable buffer measured 80 ns,
  0 B/op, and 0 allocations/op in the same run. CPU-pinned paired medians
  (320.3 ns and 73.0 ns) and the alternating-binary methodology are in
  [`dev/perf/optimization-log.md`](../dev/perf/optimization-log.md).

## Decoding with ten percent source loss, T=1280

Each iteration constructs a decoder, adds all surviving source packets plus
repair packets and a duplicate, decodes, reconstructs, verifies, and delivers
an owned copy (throughput in object bytes).

| K      | Object    | Lost symbols | Overhead symbols | End-to-end decode |
|-------:|----------:|-------------:|-----------------:|------------------:|
| 10     | 12.8 KB   | 1            | 0                | 1.2 ms (10.9 MB/s) |
| 100    | 128 KB    | 10           | 1                | 7.2 ms (17.7 MB/s) |
| 1,000  | 1.28 MB   | 100          | 2                | 67 ms (19.2 MB/s)  |
| 10,000 | 12.8 MB   | 1,000        | 4                | 0.97 s (13.2 MB/s) |

The decoder completed every corpus above with at most a handful of symbols
beyond K, consistent with the bounded recovery statistics in
[recovery.md](recovery.md).

## Practical guidance

- Repair emission is cheap once a block's plan is built; the plan solve is
  the cost that scales with K. For steady streams, larger blocks amortize the
  solve; for latency-sensitive transfers, smaller blocks (or several blocks
  via `DecoderBlockBytes`) bound the per-block solve time.
- Use `Packet` when an independently owned packet is convenient. Use
  `PacketInto` with one buffer per concurrent caller when packet emission sits
  on an allocation-sensitive hot path; the returned slice aliases the buffer.
- All decoder memory is preflighted against `Limits` before allocation;
  `ObjectEncoderWorkspaceBackingBytes` and `ObjectDecoderWorkspaceBackingBytes`
  report the peaks for capacity planning.
- Numbers move with CPU, symbol size, and loss pattern. Rerun the command
  above on target hardware before making deployment decisions, and see
  `examples/roundtrip` for an end-to-end measurement including SHA-256
  verification.
