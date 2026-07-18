# ObjectDecoder trace-validation evidence log

This log records the bounded evidence for the public `ObjectDecoder` lifecycle.
It is an evidence ledger, not a claim that the Go implementation is formally
verified. The measurements below were reproduced on 2026-07-18 with the pinned
TLC 1.8.0 jar and Java 17.

## Claim under test

The supported claim is deliberately narrow:

> For each edge in the two committed, bounded `ObjectDecoderModel` DOT graphs,
> the deterministic selector retains at least one path containing that edge,
> every selected path is concretized as a realizable public-API schedule, and
> the resulting production trace is accepted by `ObjectDecoderTraceSpec`.

This is **not** a claim about every model behavior, every path, every RaptorQ
parameter, every packet sequence, or every concurrent callback interleaving.
The three zero-length constructor schedules are manually authored witnesses;
they are not edges in the two packet-driven DOT graphs.

## Recorded results

| Evidence | Recorded result | What it establishes |
| --- | --- | --- |
| Harvest | 30 deterministic built-in-plus-checked-fuzz candidates, 24 retained, 6 abstract duplicates dropped | The checked-in corpus preserves all required fixture families and novelty obligations. |
| Action coverage | 24 traces, 140 classified events, 16/16 manifest actions | Every member of the closed lifecycle action alphabet occurs in the harvested corpus. The files contain 161 raw records: 21 `init` records are intentionally excluded from classification, while three `construct_final` records are classified. |
| Legal witnesses | 16 traces, 68 classified events, 16/16 manifest actions | Each manifest action also has an independently named legal witness. This is a drift guard for the action alphabet, not additional model-edge coverage. |
| Combined coverage report | 40 traces, 208 classified events, 16/16 manifest actions | `coverage/report.json` is the pinned aggregate of the harvested corpus and independently named legal witnesses. |
| Positive replay | `tracereplay: passed 24/24 traces` | Every harvested production trace was accepted by the data-driven TLA+ trace spec. |
| Negative replay | `tracereplay: passed 284/284 mutants` | Every applicable mutant from all seven operators failed with the exact `TraceComplete` rejection marker. This is negative-control evidence, not a general mutation score. 284 counts operator cases, not distinct traces: where both apply, a `skip-verify` mutant is byte-identical to the `drop-step` mutant of the first `verified` record. |
| Model A | 105 generated states, 21 distinct states, depth 8; 104 dumped edges | The `Blocks=2`, `K=2`, no-decode-fault bounded model satisfies its configured invariants and action properties. |
| Model B | 16 generated states, 8 distinct states, depth 5; 15 dumped edges | The `Blocks=1`, `K=1`, decode-fault-enabled bounded model satisfies the same configured checks. |
| Model negative control | exit 13; `Error: Action property EveryNextTransitionAllowed is violated.` | `AdversarialSkipVerify` reaches `delivered` directly from `decoded` and is rejected by the independently specified allowed-action property. |
| Edge selection | A: 29 schedules cover 104/104 dumped edges; B: 6 schedules cover 15/15 dumped edges | Every edge in each exact committed graph is retained by at least one serialized schedule. Repeated action labels at different states remain distinct structured edge triples. |
| Constructor witnesses | 3 schedules | Successful, verification-failed, and delivery-failed zero-length construction outcomes are checked separately. |
| Oracle 1 | 38/38 schedules on the production Go API | After every public operation, the driver checks state, completed-block count, the closed error-sentinel class both positively and negatively, event order, and callback binding. |
| Oracle 2 | `tracereplay: passed 38/38 traces`; 16/16 action coverage | Traces emitted by all 38 Oracle-1 executions also satisfy the TLA+ trace spec. |
| Seeded fault | seven steps reduced to four in 13 attempts; exact artifacts reproduced | An omitted `ObjectVerified` transition is caught by Oracle 1, minimized without changing its full sentinel-aware signature, and rejected by TLC at `TraceComplete`. |

The seven deterministic negative operators are `drop-step`, `forge-binding`,
`early-deliver`, `skip-verify`, `resurrect`, `forge-completion`, and
`double-delivery`. The gate also fails if any operator has no applicable corpus
member, preventing a vacuous negative total. `forge-binding` changes every
replayed record position, not only callback records, so the replay contract
requires `bindingOK` on the complete trace alphabet. It excludes the initial
record because an invalid `Init` produces an empty TLC model, not the exact
`TraceComplete` counterexample required of these operators.

## Model boundaries and realizability

Configuration A uses a fixed two-block, two-sub-block, shortened-source fixture.
Configuration B uses a one-block fixture for successful callback paths and a
fixed rank-deficient fixture only for schedules containing `DecodeFail`. The
serialized schedule records the actual fixture choice. The deficient case feeds
repair ESIs 319 through 328 under a one-attempt cap and uses ESI 329 for the
terminal failed attempt.

The path selector targets **edges**, not all paths. Its persisted evidence is a
set of structured `(source, action, destination)` triples tied to the exact
graph digest. The claim is therefore “every dumped bounded edge has a selected
realizable witness,” not “every behavior is tested.” Source-symbol fixtures are
used for successful completion so abstract cardinality completion maps to the
systematic property; the model does not claim that cardinality implies rank
sufficiency for arbitrary repair symbols.

Oracle 1 and Oracle 2 have different jobs:

- Oracle 1 runs the actual public Go decoder and checks local observations after
  every call without Java.
- Oracle 2 replays the resulting finite observation sequence through TLC. It
  checks conformance to the lifecycle abstraction; it does not execute or
  reason about Go bytecode.

Neither oracle is the RFC 6330 byte-level oracle. RFC source pinning, table
extraction, GF(256) algebra, rank checks, and Rust/C++ differential vectors own
that evidence. The trace pipeline owns lifecycle ordering, terminal behavior,
callback binding, and the model-to-production witness mapping.

All harvest, replay, mutation, DOT parsing, scheduling, and TLA+ code is inside
the nested `dev` module. The production module does not import it, ship it in
its module ZIP, link Java/Rust/C++, or run these tools during a normal import.
The evidence drives the public production API across that one-way development
boundary.

## Deterministic inputs and hashes

TLC state fingerprints affect DOT node IDs. Reproducing the committed DOT and
schedule bytes therefore requires `-workers 1 -fp 0 -seed 1` in addition to
`-noGenerateSpecTE`. Consecutive runs without fixed `-fp` and `-seed` are not a
byte-stability test, even when their reachable-state counts agree.

The recorded environment reported `go version go1.26.5 linux/amd64`, Temurin
17.0.19, and `TLC2 Version 2026.07.15.134331 (rev: 227f61b)` from the pinned
TLC 1.8.0 jar.

| Artifact | SHA-256 or logical digest |
| --- | --- |
| `dev/spec/rfc6330.txt` | `87f6da89cc325987cb2910d1c124f74eaeac5c86a83f962cf347c0cc66321ace` |
| `bin/tla2tools-v1.8.0.jar` | `58d44845a37a8d776deaf8cf3a623213b59d311bc0ec287bcdfbe148dd11bb3d` |
| `dev/formal/coverage/actions.json` | `83cf7a53825a90959380c82b068d24264fd1051e616f28170a7a44606ddfb08d` |
| `dev/formal/coverage/report.json` | `806de50ac21427eb8bd0e247f0abfaf902597544d010bccb89b6a007c936537b` |
| Model-A DOT bytes | `d6c7ca7975b70bf43c614e2c1e772ecca56b1d10a4dde625c3fd8c4dd83e6df5` |
| Model-B DOT bytes | `f9c5231d636039a7a07f484a81840d3b799fe6abc456e5adee904e087ad8f739` |
| Model-A parsed graph | `53e5e6253fe6fd30c69e38e137a206c18328a2b9a8cc1428bd7c1c98d5574a35` |
| Model-B parsed graph | `0a5bca51ec3660f2fc0834dc4c1f78f0499401f3eb2333e7e28b7cacd72b4d88` |
| Harvested-corpus hash-of-hashes | `f47a689efcd69bb22edf93d7c40f655ff8bfaccc17816466abab27789876bd6d` |
| Legal-witness hash-of-hashes | `ef8589d881303b4076f1d3844a29a9ab7d136e2b0ec4f1f5371712d725ad8407` |
| Schedule-tree hash-of-hashes | `15553bccfb6dac70a0c6377f12c61c383d4047120e10bb81573c0b68e93727c6` |
| Derived 284-mutant hash-of-hashes | `3eaaf79ca3fb530dc83bc9b66c288698ba69d6ec3b7224cd18ed0ae58c5c0e7b` |
| Seeded-fault patch | `1a92b24c957a377122cecb46225353106545fabff91f8ce8d4f1acaa904ad92a` |

The three tree hashes above include the sorted relative file names in the
`sha256sum` input. The mutant hash uses
`./<trace-fingerprint>/<mutant-id>.ndjson` names in a temporary directory.
Short 12-hex names are display identifiers; the curation code detects a
short-name collision by comparing full SHA-256 digests and fails closed.

The committed tree hashes can be recomputed from the repository root with:

```sh
find dev/formal/corpus/objectdecoder -type f -name '*.ndjson' -print0 | \
  sort -z | xargs -0 sha256sum | sha256sum
find dev/formal/corpus/witness -type f -name '*.ndjson' -print0 | \
  sort -z | xargs -0 sha256sum | sha256sum
find dev/formal/mbt/schedules -type f -name '*.json' -print0 | \
  sort -z | xargs -0 sha256sum | sha256sum
```

## Reproduction

The aggregate gates from the repository root are:

```sh
task verify:trace:coverage
task verify:trace:corpus
task verify:trace:negative
task mbt:smoke
task mbt:generate
task mbt:oracle2
```

`task mbt:smoke` runs Oracle 1 both natively and with `GOARCH=386`. The replay
and graph-generation tasks require Java 17 or newer. To choose a Java runtime
explicitly:

```sh
export TLC_JAVA=/path/to/java17/bin/java
```

The focused positive, negative, and action-coverage commands are:

```sh
go -C dev run ./internal/cmd/tracecover \
  -manifest formal/coverage/actions.json \
  -corpus formal/corpus/objectdecoder \
  -require-corpus-full -format json

go -C dev run ./internal/cmd/tracereplay \
  -corpus formal/corpus/objectdecoder -j 4
go -C dev run ./internal/cmd/tracereplay \
  -corpus formal/corpus/objectdecoder -negative -j 4
```

The focused bounded model commands, run from `dev/formal`, are:

```sh
TLC_JAVA=/path/to/java17/bin/java ../scripts/tlc.sh \
  -workers 1 -fp 0 -seed 1 -noGenerateSpecTE \
  ObjectDecoderModel.tla -config ObjectDecoderModelA.cfg
TLC_JAVA=/path/to/java17/bin/java ../scripts/tlc.sh \
  -workers 1 -fp 0 -seed 1 -noGenerateSpecTE \
  ObjectDecoderModel.tla -config ObjectDecoderModelB.cfg
TLC_JAVA=/path/to/java17/bin/java ../scripts/tlc.sh \
  -workers 1 -fp 0 -seed 1 -noGenerateSpecTE \
  ObjectDecoderModel.tla -config ObjectDecoderModelMutation.cfg
```

The final command must exit nonzero and contain exactly:

```text
Error: Action property EveryNextTransitionAllowed is violated.
```

To reproduce the pinned model-A graph, add
`-dump dot,actionlabels /tmp/object-decoder-model-a.dot` to its command and
compare the result with
`dev/internal/mbt/dotparse/testdata/object-decoder-model-a.dot`; use the
corresponding B name and configuration for model B.

## Seeded production-bug and shrinker exercise

Status: **passed on 2026-07-18; the production mutation was reverted and was
never committed.**

The exercise temporarily replaced the assignment
`decoder.state = ObjectVerified` in `object_decoder.go` with a no-op. The input
was a strict seven-step cfg-A schedule assembled from committed graph edges: it
contains three removable idempotent `AddWait` self-loops and the
delivery-reentrant finalization witness. Its SHA-256 was
`a7262cefee03d0720627d164f765e42ee2d3fa1934680e90cf5df80d2803edf7`;
with the unmodified implementation its trace fingerprint was `b6b4bb37bd42`.

With the fault present, Oracle 1 failed at `FinalizeVerifyEnter` with the stable
signature `(callback, FinalizeVerifyEnter, expected=none,
actual=callback-error)`. The concrete observation was:

```text
delivery reentrant add state=decoded error=raptorgo: object decoder is terminal: decoded
```

Deterministic ddmin reduced the schedule from seven to four steps in 13
attempts. The committed artifact declares `strictSchedule=true`; its schedule
SHA-256 is
`870ea532e0f3155bd3d72e40c7c31ba59905835ab220906b7641741ae06b1a34`,
and the committed buggy partial trace has fingerprint `c6b02e883311` and
NDJSON SHA-256
`cbba3fa181816f762de2b8e580b7372192aa647bcbc22fe7a7193d574948497f`.
The minimized schedule is a valid Oracle 1 repro, but not a connected model
path: ddmin removes operations without synthesizing replacement graph edges.

This Java-free task applies the exact fault patch only inside a temporary Git
archive, reruns Oracle 1 and ddmin, and byte-compares the regenerated schedule,
metadata, and trace with the committed evidence:

```sh
task redteam:object-verified
```

With the clean implementation, the committed minimized schedule instead passes
and produces fingerprint `066f3d9c54f7`:

```sh
go -C dev run ./internal/cmd/mbtrun \
  -schedules formal/redteam/object-verified/minimized.json
```

The full formal gate independently requires the committed faulty partial trace
to be rejected exactly once with
`Error: Temporal property TraceComplete was violated.`:

```sh
task verify:trace:redteam
```

After the original experiment restored the assignment,
`git diff --quiet -- object_decoder.go`, the root tests, and the focused dev
driver tests all passed. The isolated reproduction task never edits the live
worktree.

## Adversarial closure

Three independent read-only review passes covered the formal/TLC boundary, Go
resource and artifact safety, and RFC/interoperability/release isolation.
Their findings were remediated in the commits containing the hardenings
below; only the committed hardenings and their regression tests are
repository-verifiable evidence of that closure:

- the corpus writer rejects non-regular `.ndjson` conflicts, validates output
  names, stages every new trace before replacement, and has a regression test
  proving that a pre-existing output symlink cannot modify its target;
- negative replay bounds directory discovery to 4096 entries and total
  generated cases to 16384; mutations are planned by small descriptors and
  streamed through the fixed four-worker maximum instead of retaining every
  cloned event slice;
- every NDJSON reader is capped at 2 MiB and 4096 events, while `trace2tla`
  and `tracemutate` stream from an open file; recursive Oracle 1 schedule
  discovery is capped at 4096 entries and rejects symlinks;
- the deliberately restricted TLC DOT grammar rejects leading, doubled, and
  trailing attribute commas;
- the seeded fault patch contains only a valid Git diff, has a checked-in
  explanatory README, and is authenticated by the SHA-256 above before use.

The following focused closure commands passed on the recorded environment:

```sh
go -C dev test ./internal/objtrace ./internal/tracemutate \
  ./internal/tracereplay ./internal/tracecorpus \
  ./internal/mbt/dotparse ./internal/mbt/schedule \
  ./internal/cmd/trace2tla ./internal/cmd/tracemutate \
  ./internal/cmd/mbtrun ./internal/cmd/traceharvest
task verify:trace:mutants
task redteam:object-verified
TLC_JAVA=/path/to/java17/bin/java task verify:trace:redteam
task verify:static
```

The mutant gate remained exactly 284 cases with digest
`3eaaf79ca3fb530dc83bc9b66c288698ba69d6ec3b7224cd18ed0ae58c5c0e7b`.
The isolated production fault regenerated byte-identical minimized evidence,
and TLC rejected its partial trace exactly once at `TraceComplete`.

Two low crash-consistency limitations are explicit. Corpus generation stages
all new bytes before publishing, but publishes files with sequential renames
and then removes stale files; an external filesystem failure or process
termination during that publish can leave a mixed generation. Similarly,
arbitrary `mbtrun -failures` output replaces each artifact atomically, but the
three-file set is not a portable filesystem transaction. Normal regeneration
is guarded by whole-tree byte comparison. The committed seeded-fault set is
also authenticated file-by-file and regenerated byte-for-byte by the gate.

## Limitations and claim risks

- The models are finite abstractions with `Blocks <= 2` and `K <= 2`; the
  constructor witnesses are separate single-action cases.
- Edge coverage is over 119 edges in two exact dumped graphs. It is neither path
  coverage nor coverage of unbounded model behavior.
- The corpus is selected from deterministic built-in executions and checked Go
  source-packet round-trip fuzz seeds. It does not represent arbitrary network
  input, malformed packet ingestion by that fuzz target, or concurrency
  schedules.
- The 284 mutants are sound, structurally applicable trace transformations.
  Their rejection does not imply that every malformed trace or production bug
  will be detected.
- Oracle 2 checks projected trace fields. Data omitted by that projection must
  be justified by Oracle 1 or another independent test.
- No model or replay here proves liveness, memory safety, constant-time
  behavior, byte-level RFC interoperability, or compatibility outside the
  explicitly tested differential domains.
- Artifact counts and hashes are evidence only while the corresponding drift
  gates reproduce them. A changed model, graph, corpus, action manifest, or
  fixture requires this log to be regenerated rather than edited by hand.
