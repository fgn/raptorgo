# Encoder optimization log

This log records the evidence for the `perf/systematic-packet-pipeline`
branch. Measurements are retained even when an experiment is rejected. Runtime
changes are kept in separate commits so their individual effects can be
measured and reverted.

## Acceptance gates

The primary workload is one unshortened systematic symbol from the middle of a
K=10,000, T=1,280, N=1 source block. This is the large-block case behind the
documented O(K*T) caveat. Relative to the frozen baseline below, the final
branch must:

- reduce median `ns/op` by at least 13x;
- reduce `B/op` by at least 90%;
- report allocation events separately: `Packet` must retain one caller-owned
  result allocation unless the public ownership contract changes, while any
  caller-buffer API must demonstrate a genuine zero-allocation steady state;
- introduce no statistically material regression in the other K strata,
  grouped source packets, N>1 transposition, shortened final symbols, cached
  repair emission, plan construction, or end-to-end decoding; and
- preserve the complete correctness, resource, formal, mutation, race, fuzz,
  module-release, and differential-oracle gates.

The 13x latency threshold is 155,177 ns/op and the 90%-reduction allocation
threshold is 1,280,550 B/op for the primary workload. The small-K strata are
reported rather than hidden; the fixed cost of allocating and returning a
1,284-byte owned packet makes a 13x improvement physically implausible at
K=10, where the baseline is already about 1.36 microseconds.

## Baseline: `0a689172a70920247139b450c8a2a7bf3a5b32ae`

Environment:

- AMD Ryzen 5 3600, linux/amd64, Linux 6.8.0-124-generic;
- Go 1.26.5;
- CPU 4 only, `GOMAXPROCS=1`; CPU 4 is physical core 4 and SMT sibling 10 was
  not used by the benchmark process;
- `schedutil` governor, frequency boost enabled, advertised maximum
  4,208 MHz;
- load average immediately before the run: 1.85, 1.51, 1.74;
- ten samples per workload, 500 ms minimum benchmark time per sample.

Exact command:

```sh
taskset -c 4 env GOMAXPROCS=1 go test -run '^$' \
  -bench '^BenchmarkObjectEncoderPacket/systematic/K=(10|100|1000|10000)/T=1280$' \
  -benchmem -benchtime=500ms -count=10 .
```

| K | Median ns/op | Observed range | B/op | allocs/op |
|---:|-------------:|---------------:|-----:|----------:|
| 10 | 1,358 | 1,316–1,451 | 14,980 | 3 |
| 100 | 10,497 | 10,347–14,158 | 132,484 | 3 |
| 1,000 | 200,429 | 197,390–204,580 | 1,287,557 | 3 |
| 10,000 | 2,017,296 | 1,974,745–2,089,564 | 12,805,508 | 3 |

The monotonic block-sized `B/op` confirms that each one-symbol call allocates
and rebuilds the entire source block.

Profile command:

```sh
taskset -c 4 env GOMAXPROCS=1 go test -run '^$' \
  -bench '^BenchmarkObjectEncoderPacket/systematic/K=10000/T=1280$' \
  -benchmem -benchtime=5s -count=1 \
  -o /tmp/raptorgo-systematic-baseline.test \
  -cpuprofile=/tmp/raptorgo-systematic-baseline.cpu.pprof \
  -memprofile=/tmp/raptorgo-systematic-baseline.mem.pprof .
go tool pprof -top /tmp/raptorgo-systematic-baseline.cpu.pprof
go tool pprof -top -alloc_space /tmp/raptorgo-systematic-baseline.mem.pprof
```

Observed profile evidence:

- timed result: 2,095,960 ns/op, 12,805,508 B/op, 3 allocs/op;
- `BuildSourceBlock` accounted for 6.41 s cumulative CPU in the 7.51 s
  profile, dominated by `runtime.memmove` (3.69 s) and zeroing
  (`runtime.memclrNoHeapPointers`, 2.57 s);
- `BuildSourceBlock` accounted for 37.50 GiB, or 99.69%, of allocated space;
- the profile also contains the parent benchmark's one-time repair-cache
  warm-up, so only the benchmark-reported `ns/op`, `B/op`, and `allocs/op` are
  used for acceptance comparisons.

No production files were changed for this baseline.
