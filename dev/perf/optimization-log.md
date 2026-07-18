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

## Hardened pre-optimization baseline: `550acdb`

Before production optimization, adversarial review hardened the measurements:

- systematic and repair subbenchmarks now use independent encoders, so repair
  setup cannot warm or retain state for the source path;
- timed calls cycle ESIs, retain their result in a package-level sink, and the
  final timed result is checked byte-for-byte after the timer stops;
- a distinct systematic-after-repair workload detects hidden coupling to the
  repair cache;
- construct-plus-first-systematic records any work moved into construction or
  a lazy first call; and
- N=1/N=4, grouped, shortened, and two-block layouts have dedicated strata.

The direct single/N=1 layout workload was repeated five times with the same
CPU pin, `GOMAXPROCS`, and 500 ms minimum sample time:

```sh
taskset -c 4 env GOMAXPROCS=1 go test -run '^$' \
  -bench '^BenchmarkObjectEncoderSystematicLayouts$' \
  -benchmem -benchtime=500ms -count=5 .
```

| Workload | Median ns/op | Observed range | B/op | allocs/op |
|---|---:|---:|---:|---:|
| single/N=1 | 1,939,159 | 1,918,640–2,052,592 | 12,805,511 | 3 |
| grouped=16/N=1 | 1,949,905 | 1,939,757–1,986,401 | 12,825,863 | 3 |
| single/N=4 | 3,442,214 | 3,409,731–3,619,563 | 12,805,511 | 3 |
| grouped=16/N=4 | 3,413,698 | 3,273,778–3,455,286 | 12,825,863 | 3 |
| shortened-final/N=4 | 3,266,804 | 3,171,199–3,319,318 | 12,805,383 | 3 |
| single/Z=2/N=4/SBN=1 | 3,184,620 | 3,182,139–3,606,894 | 12,805,508 | 3 |

The final comparison will alternate binaries built from `550acdb` and the
candidate head on the same CPU. This avoids claiming a ratio from runs made at
different `schedutil` frequencies or under different sibling-core load. The
candidate must satisfy both the original absolute threshold of 155,177 ns/op
and a paired median improvement of at least 13x. Against the hardened
single/N=1 median, the latter threshold is 149,166 ns/op.

The Z=255 loop-termination correction between the original and hardened
baselines does not execute in any Z=1 benchmark above and therefore does not
affect the performance comparison.

## Implemented sequence

Production changes were kept separate so each optimization could be reviewed
or reverted independently:

| Commit | Change |
|---|---|
| `294f9d6` | copy contiguous N=1 source ranges directly from the immutable object |
| `4fcd791` | scatter only requested N>1 sub-symbol ranges into packet order |
| `80e103e` | write the four-byte payload ID into the result allocation |
| `3ab021b` | include retained repair-cache backing in source-packet peaks |
| `db53642` | cache the two immutable RFC partition scalars at construction |
| `82e3903` | replace deferred mutex cleanup with an explicit checked wrapper |
| `dd3b759` | add caller-owned `PacketInto` output for zero-allocation source emission |
| `18893b7` | reject short output buffers before any repair-cache mutation |
| `ceb06f5` | dirty-buffer/layout and maximum-ESI adversarial coverage |

The N=1 and N>1 paths preserve the existing `Packet` ownership contract and
mutex/cache serialization. No pool, unsafe code, CGo, SIMD, assembly, mutable
layout slice, or hidden lazy work was introduced.

One experiment was rejected and reverted: a subtraction-based fast success
path for source peak checks changed a ten-sample median from about 321.0 to
318.2 ns/op, below a convincing effect on this host. It also produced
correctness-equivalent mutation survivors because the original checked path
remained as the error fallback. The marginal branch was removed instead of
weakening the mutation gate or accepting an equivalence baseline.

## Final alternating-binary comparison

Final production code was measured after the adversarial short-buffer/cache
fix. The candidate test binary was built from `ceb06f5`; that commit differs
from production commit `18893b7` only by tests. The hardened baseline was
`550acdb`. Binary SHA-256 values were:

```text
6d1a81853577d10465a554d7b640bd006e57fce994c8545e26ff909ba50f0542  base.test
4a52e90438b51e9cf9cffb7c4f7c37cf3686b5987d888c0963fc80d952fbec14  final.test
```

Both binaries were built with Go 1.26.5. After one discarded warm-up, ten
samples were run on CPU 4 with `GOMAXPROCS=1` and a 500 ms minimum benchmark
time. Odd samples ran baseline, `Packet`, then `PacketInto`; even samples ran
the reverse order. This controls frequency/load drift instead of comparing
separate benchmark sessions.

Equivalent reproduction commands for each sample are:

```sh
taskset -c 4 env GOMAXPROCS=1 ./base.test \
  -test.run '^$' \
  -test.bench 'BenchmarkObjectEncoderSystematicLayouts/single/N=1$' \
  -test.benchmem -test.benchtime=500ms -test.count=1

taskset -c 4 env GOMAXPROCS=1 ./final.test \
  -test.run '^$' \
  -test.bench 'BenchmarkObjectEncoderSystematicLayouts/single/N=1$' \
  -test.benchmem -test.benchtime=500ms -test.count=1

taskset -c 4 env GOMAXPROCS=1 ./final.test \
  -test.run '^$' \
  -test.bench 'BenchmarkObjectEncoderSystematicInto/N=1$' \
  -test.benchmem -test.benchtime=500ms -test.count=1
```

| Path | Median ns/op | Observed range | B/op | allocs/op | Median paired speedup |
|---|---:|---:|---:|---:|---:|
| hardened `Packet` | 1,984,068 | 1,860,947–2,208,502 | 12,805,511 | 3 | reference |
| final `Packet` | 320.3 | 314.5–370.1 | 1,408 | 1 | 6,122.8x |
| final `PacketInto` | 73.0 | 66.66–75.90 | 0 | 0 | 27,069.2x |

The final owned-result API is 484 times below the absolute 155,177 ns/op
threshold and reduces allocated bytes by 99.989%. Its one allocation is the
documented independently owned result, so allocation-event count falls from
three to one. `PacketInto` supplies the literal zero-allocation path: it
reduces both bytes and allocation events by 100%, while exceeding the 13x
latency requirement by more than three orders of magnitude.

The paired ratios above are the medians of the ten within-sample ratios, not
the ratio of independently selected best cases. The first baseline/`Packet`
pair ran at visibly lower frequency (2,208,502 ns versus 370.1 ns); retaining
it in the range and paired statistic avoids cherry-picking.

## Correctness and adversarial evidence

The architectural paths are checked against an independently materialized
`BuildSourceBlock` result across T=1..12, every valid tested alignment,
N=1, balanced N, N=units, and N=units+1 (zero-sized sub-block), Z=1..3,
boundary object lengths, arbitrary groups, and shortened/full packets.
Every case is also replayed through `PacketInto` using a dirty buffer and a
tail sentinel. Separate tests cover exact/one-byte-short packet and working
memory limits, returned ownership, zero allocations, concurrent mixed
source/repair calls, maximum repair ESI, repair-cache replacement failure,
and Z=255 full-object round trip.

Three adversarial reviewers independently inspected Go/runtime behavior,
formal/resource obligations, and RFC/interop mapping. They found one release
blocker: a short cold repair `PacketInto` request originally validated its
buffer after building/replacing the repair cache. Commit `18893b7` moves that
check before all source/repair work and pins cross-block cache atomicity. The
reviewers found no remaining high/medium correctness, API, formal, runtime,
or byte-interoperability issue. Additional reviewer-run Go/Rust comparisons
covered Al=2 grouping/shortening, Z=255, T=65,535/N=255/Al=255, and repair
ESI 0xFFFFFF.

The optimization does not add an encoder lifecycle transition: `Packet` and
`PacketInto` remain fully serialized under the existing mutex, and direct
source output is a stateless mapping. Therefore no new TLA state machine was
invented. The complete existing generated-trace drift gate, positive TLC
models/traces, and pinned negative controls were rerun to rule out regressions;
caller-buffer aliasing and cache atomicity are covered at the Go invariant
level where those properties exist.

## Final validation matrix

The final committed tree was validated with Go 1.26.5. This host's default
`java` is Java 8, so the already-installed Temurin 17 runtime was selected
explicitly for TLC:

```sh
TLC_JAVA=/home/fgn/.local/opt/temurin-17-jre/bin/java task ci
MUTATION_DIFF_BASE=550acdb task mutation:changed
task test:release
```

| Gate | Final result |
|---|---|
| format, tidy, vet, pure-Go/no-dependency lint | pass |
| reachable vulnerability scan | no vulnerabilities found |
| root and nested-development Go tests | pass |
| statement coverage | root 85.8%; codec 78.0%; GF(256) 93.1%; matrix 87.2%; RFC primitives 92.6% |
| curated mutation CI gate | 299 killed, 3 reviewed baseline survivors; 99.01% |
| complete optimization diff mutation gate versus `550acdb` | 98/98 killed; 100% |
| broad scheduled mutation discovery audit | 7,084 generated; 4,656 killed, 1,079 escaped, 1,349 not covered; 81.19% covered-code score |
| linux/386, strict checkptr, full race detector | pass |
| five parser/object/codec/GF(256)/matrix fuzz smoke targets | pass |
| every object benchmark corpus at one iteration | pass |
| RFC checksum, generated-artifact drift, production module ZIP | pass; 91 files, 217,823 bytes, no dev-only content |
| all positive TLA models and implementation traces | pass with pinned TLC 1.8.0 and Java 17 |
| all TLA invalid-trace and mutation negative controls | rejected for their pinned property |
| Rust primitives/raw/object parity and bidirectional cross-decode | pass |
| independently fetched/checksummed C++ consensus and cross-decode | pass, including pinned K=49 divergence |
| RFC-maximum hybrid/recovery release tests and release vectors | pass |

The first `task ci` attempt stopped at TLC because `/usr/bin/java` on this
machine is Java 8. No formal check was skipped: the complete command was rerun
from the beginning with `TLC_JAVA` pointing at Java 17 and exited successfully.

The broad audit is deliberately diagnostic rather than a release gate. Its
existing whole-repository survivors remain backlog candidates; the narrower
baseline-aware gate proves that none lies on the 98 mutated lines introduced
or changed by this optimization. The audit JSON and 201 MB raw mutation report
were inspected and then deleted; their root-level names are ignored so they
cannot reproduce the historical repository-bloat incident.
