# Performance

Measured numbers for user-relevant configurations, with the exact command to
reproduce them. The implementation is single-threaded per operation and uses
exhaustively checked scalar GF(256) arithmetic; no SIMD or assembly. Treat
these as honest baselines, not marketing claims.

## Environment

- CPU: AMD Ryzen 5 3600 (6 cores, 12 threads), linux/amd64
- Go: go1.26.5
- Date: 2026-07-17
- Command:

```sh
go test -run '^$' -bench '^BenchmarkObject' -benchmem .
# or: task bench
```

The suite also contains small-symbol conformance strata (T=7, 31, 64) that
exercise parameter-table seams; the tables below show the transport strata
with a network-realistic symbol size of T=1280 bytes and one source block.

## Encoding, T=1280

| K      | Object    | Construct + first repair | Cached repair packet | Systematic packet |
|-------:|----------:|-------------------------:|---------------------:|------------------:|
| 10     | 12.8 KB   | 0.92 ms (13.9 MB/s)      | 7.8 µs (164 MB/s)    | 7.3 µs            |
| 100    | 128 KB    | 5.9 ms (21.7 MB/s)       | 12.9 µs (100 MB/s)   | 70 µs             |
| 1,000  | 1.28 MB   | 65 ms (19.6 MB/s)        | 7.7 µs (167 MB/s)    | 476 µs            |
| 10,000 | 12.8 MB   | 0.96 s (13.4 MB/s)       | 7.1 µs (180 MB/s)    | 2.8 ms            |

- **Construct + first repair** is the one-time cost per source block: building
  the encoder and solving the repair plan (throughput in object bytes).
- **Cached repair packet** is the steady-state cost per repair symbol once the
  block's plan is cached: roughly 160–180 MB/s of symbol payload at 3
  allocations per packet, independent of K.
- **Systematic packet** currently rebuilds the source-block view per call, so
  its cost grows with K·T (visible as one block-sized allocation per op).
  Group multiple source symbols into one `Packet` call to amortize this, or
  emit large blocks as repair symbols from the cached plan. Removing the
  rebuild is listed in [roadmap.md](roadmap.md) optimization work.

## Decoding with ten percent source loss, T=1280

Each iteration constructs a decoder, adds all surviving source packets plus
repair packets and a duplicate, decodes, reconstructs, verifies, and delivers
an owned copy (throughput in object bytes).

| K      | Object    | Lost symbols | Overhead symbols | End-to-end decode |
|-------:|----------:|-------------:|-----------------:|------------------:|
| 10     | 12.8 KB   | 1            | 0                | 1.0 ms (12.7 MB/s) |
| 100    | 128 KB    | 10           | 1                | 7.0 ms (18.2 MB/s) |
| 1,000  | 1.28 MB   | 100          | 2                | 65 ms (19.8 MB/s)  |
| 10,000 | 12.8 MB   | 1,000        | 4                | 1.06 s (12.1 MB/s) |

The decoder completed every corpus above with at most a handful of symbols
beyond K, consistent with the bounded recovery statistics in
[recovery.md](recovery.md).

## Practical guidance

- Repair emission is cheap once a block's plan is built; the plan solve is
  the cost that scales with K. For steady streams, larger blocks amortize the
  solve; for latency-sensitive transfers, smaller blocks (or several blocks
  via `DecoderBlockBytes`) bound the per-block solve time.
- All decoder memory is preflighted against `Limits` before allocation;
  `ObjectEncoderWorkspaceBackingBytes` and `ObjectDecoderWorkspaceBackingBytes`
  report the peaks for capacity planning.
- Numbers move with CPU, symbol size, and loss pattern. Rerun the command
  above on target hardware before making deployment decisions, and see
  `examples/roundtrip` for an end-to-end measurement including SHA-256
  verification.
