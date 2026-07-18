# Trace-validation bridge: implementation plan

Audience: a coding agent implementing this without further context. Read
`dev/formal/README.md`, `docs/invariants.md`, and this file before writing
code. Everything in this plan lives under `dev/` (the nested development
module); the production module API and behavior must not change.

## Goal

Upgrade the formal evidence from "a handful of hand-authored Go traces
conform to the TLA+ models" to:

1. every harvested execution of the public `ObjectDecoder` (tests, fuzz
   corpus, e2e) can be replayed against the models (WS1, WS2);
2. non-vacuity of the replay is machine-generated, not hand-maintained (WS2);
3. model-action coverage of the harvested corpus is measured and gated (WS3);
4. the reverse direction is tested: behaviors the model allows are shown
   realizable by the production code (WS4).

Explicit non-goal: this is systematic conformance **testing**, not proof.
The README/`docs/invariants.md` caveat language is updated at the end, not
removed.

## House rules for the implementing agent

- All new code lives in the `dev` module (`dev/internal/...`,
  `dev/formal/...`, `dev/plans/...`). `task verify:module` must keep passing
  (the production module ZIP must not grow).
- No new third-party dependencies. `encoding/json`, `crypto/sha256`, and
  hand-rolled parsers only.
- Determinism everywhere: no wall-clock timestamps, no `math/rand` without a
  fixed seed, no map-iteration-order dependence in any generated artifact.
  Generated files must be byte-identical across runs (the repo's
  `verify:generated` philosophy).
- Negative controls follow the existing house style: each forged artifact is
  pinned to an **exact** TLC failure message, checked with `grep -F`, and a
  forged artifact that passes TLC fails the gate (see
  `Taskfile.yml` `verify:tla:negative`).
- TLC runs only through `dev/scripts/tlc.sh` (pinned jar, Java 17+, temp
  metadir). Java-dependent steps go in nightly tasks; `task ci` stays
  Java-free (this boundary already exists: ci.yml never runs TLA).
- Formatting: `gofumpt` clean; TLA+ files follow the style of
  `dev/formal/ObjectDecoderTraceSpec.tla`.

## Current state (inventory)

| Piece | Location | Role |
|---|---|---|
| Abstract lifecycle model | `dev/formal/DecoderLifecycle.tla` + 3 cfgs + mutation cfg | exhaustive bounded exploration; actions Accept/Decode/VerifySuccess/VerifyFailure/Deliver/Abort; `AdversarialDeliver` fault sentinel |
| Abstract-twin trace family | `dev/internal/verifytrace` → `dev/formal/DecoderTrace*.tla` | Go twin (not production code) records events; TLC replays via `DecoderTraceSpec.tla` |
| Production object-decoder traces | `dev/internal/cmd/genobjecttrace` → `dev/formal/ObjectDecoder*Trace*.tla` | drives the real `raptorgo.ObjectDecoder` through 4 scenarios; ops: `init, add_wait, block_complete, verify_enter, verified, deliver_enter, decode_failure, add_return, terminal_add` |
| Replay spec | `dev/formal/ObjectDecoderTraceSpec.tla` | data-driven `Apply(record)`; invariants `ExactlyOnce`, `NoDeliveryWithoutVerification`, `NoSuccessBeforeCompletion`; temporal `TraceComplete` |
| Negative controls | hand-written forged `*.tla` data + `verify:tla:negative` | pinned to `Error: Temporal property TraceComplete was violated.` |
| TLC launcher | `dev/scripts/tlc.sh` | pinned jar `bin/tla2tools-v1.8.0.jar` |
| Drift gate | `task verify:generated` | regenerates trace data, diffs against committed files |

Key production facts the plan depends on (verified against
`object_decoder.go` at time of writing):

- `AddPacket` state machine: only `ObjectReceiving` accepts packets; every
  other state returns `ErrObjectTerminal` without mutation.
- Duplicate symbol with identical content: accepted, no error. Duplicate ESI
  with different content: error wrapping `ErrObjectDecoder` and
  `codec.ErrDecoderDuplicateConflict`, **state stays `ObjectReceiving`**.
  Same for `codec.ErrDecoderSymbol`. Any other block-decoder error moves to
  `ObjectDecodeFailed`.
- Finalization is fused into the completing `AddPacket` call:
  completed-blocks counter increments → state `ObjectDecoded` → mutex
  released → verifier runs → `ObjectVerified` → deliverer runs →
  `ObjectDelivered`. Callbacks run without the decoder mutex and may call
  back into the decoder (a reentrant `AddPacket` observes a terminal
  rejection in the transient `decoded`/`verified` state).
- Zero-length objects (`TransferLength == 0`): `NewObjectDecoder` runs
  verify+deliver synchronously and returns a **non-nil** decoder even on
  callback failure, in the corresponding terminal state, with an error
  wrapping `ErrObjectVerification`/`ErrObjectDelivery`.
- A deterministic rank-deficient fixture exists: K'=10 object
  (`TransferLength:10, T:1, Z:1, N:1, Al:1`), repair ESIs 319..328, limits
  `MaxRetainedSymbols:11, MaxRepairSymbolsPerBlock:1, MaxDecodeAttempts:1`;
  ESI 329 then produces `ObjectDecodeFailed` wrapping
  `codec.ErrDecoderAttemptLimit` (see `executeDecodeFailure` in
  `genobjecttrace`).

---

## WS1 — Trace harvesting pipeline (NDJSON)

Split "observe an execution" from "render TLA+ data" so any execution can
produce a checkable trace. The existing four `genobjecttrace` scenarios
become the first consumers; harvesting from tests/fuzz/e2e becomes possible
without new TLA work per trace.

### 1.1 Event schema v1

New package `dev/internal/objtrace`. One NDJSON line per event:

```json
{"v":1,"op":"add_wait","result":"ok","state":"receiving","completed":1,
 "verification":0,"delivery":0,"bindingOK":true,"sbn":0,"esi":1,"symbols":1}
```

Field rules:

- `v` (int, required): schema version, starts at 1. The converter rejects
  unknown versions.
- `op` (string): one of the closed set below. Charset `[a-z_]+` enforced.
- `result` (string): `ok | verification_error | delivery_error |
  decode_error | terminal | invalid | conflict | limit`.
- `state`: `raptorgo.ObjectDecoderState.String()` output verbatim.
- `completed`, `verification`, `delivery` (ints ≥ 0): observed
  `CompletedBlocks()` and recorder-side callback counters, exactly as
  `genobjecttrace` records them today.
- `bindingOK` (bool): callback-side binding check (OTI/context/length/bytes
  equality); `true` for non-callback events.
- `sbn`, `esi`, `symbols` (ints, optional, default -1/-1/0): packet
  attribution for debugging and WS4 oracles. The TLA converter **drops**
  them (the spec is content-agnostic); they never affect fingerprints.

Op set = existing nine ops plus four new ones needed for arbitrary
executions:

| op | meaning | state change |
|---|---|---|
| `init` | constructor returned, state receiving | — |
| `construct_final` | zero-length fast path finished during construction; `result` is `ok`, `verification_error`, or `delivery_error` | terminal at init |
| `add_wait` | accepted packet, no block completed | none |
| `block_complete` | accepted packet completed a non-final block | completed+1 |
| `verify_enter` / `verified` / `deliver_enter` | callback observations (unchanged semantics) | per existing spec |
| `add_return` | final AddPacket returned; result `ok`/`verification_error`/`delivery_error` | → delivered / verification-failed / delivery-failed |
| `decode_failure` | AddPacket moved decoder to decode-failed | → decode-failed |
| `terminal_add` | AddPacket rejected with `ErrObjectTerminal` | none |
| `add_reject_invalid` (new) | `ErrInvalidPacket`/`ErrInvalidPayloadID`/`ErrInvalidOTI` while receiving; `result:"invalid"` | none |
| `add_reject_limit` (new) | `ErrLimitExceeded` while receiving; `result:"limit"` | none |
| `add_reject_conflict` (new) | duplicate-ESI conflict (`ErrObjectDecoder` + `codec.ErrDecoderDuplicateConflict` or `codec.ErrDecoderSymbol`); `result:"conflict"` | none |

### 1.2 Recorder

```go
type Recorder struct { /* mu sync.Mutex; decoder *raptorgo.ObjectDecoder; events []Event; verification, delivery uint32 */ }

func NewRecorder(oti raptorgo.OTI, context string, want []byte) *Recorder
// Verifier / Deliverer return wrapped callbacks that perform the binding
// check, count invocations, emit verify_enter/verified/deliver_enter, and
// then delegate to an injectable outcome (ok / fail) chosen per run.
func (r *Recorder) Verifier(outcome error) raptorgo.ObjectVerifier
func (r *Recorder) Deliverer(outcome error) raptorgo.ObjectDeliverer
func (r *Recorder) BindDecoder(d *raptorgo.ObjectDecoder)   // also emits init or construct_final
func (r *Recorder) AddPacket(packet []byte) (raptorgo.ObjectDecoderState, error)
// classifies the (state, err) pair into exactly one op per the table below
func (r *Recorder) Events() []Event
func (r *Recorder) WriteNDJSON(w io.Writer) error
```

`AddPacket` classification order (first match wins; get this exactly right,
it is the heart of the harvester):

1. `errors.Is(err, raptorgo.ErrObjectTerminal)` → `terminal_add`.
2. `err == nil && state == ObjectDelivered` → `add_return`/`ok`.
3. `errors.Is(err, raptorgo.ErrObjectVerification)` → `add_return`/`verification_error`.
4. `errors.Is(err, raptorgo.ErrObjectDelivery)` → `add_return`/`delivery_error`.
5. `state == ObjectDecodeFailed && errors.Is(err, raptorgo.ErrObjectDecoder)` → `decode_failure`.
6. `errors.Is(err, raptorgo.ErrObjectDecoder)` and state still receiving → `add_reject_conflict`.
7. `errors.Is(err, raptorgo.ErrInvalidPacket) || errors.Is(err, raptorgo.ErrInvalidPayloadID)` → `add_reject_invalid`.
8. `errors.Is(err, raptorgo.ErrLimitExceeded)` → `add_reject_limit`.
9. `err == nil` and `CompletedBlocks()` increased → `block_complete`, else `add_wait`.

Edge cases the recorder must handle:

- **Concurrency**: the recorder serializes with its own mutex. Harvesting is
  only defined for single-goroutine drivers in this plan; document that
  races are out of scope (a future concurrency model owns them).
- **Grouped packets**: allowed, recorded as one event (`symbols > 1`). A
  grouped add can complete a block *and* keep symbols unconsumed (the block
  short-circuits when complete); the event still classifies by observable
  outcome only. A grouped add can also complete **more than one** state
  step only in the sense of `completed` jumping by >1 — impossible today
  (packets target one SBN), assert `completedDelta <= 1` and fail loudly if
  violated.
- **Reentrant callbacks**: `Recorder.AddPacket` called from inside a
  callback must not deadlock (use a plain mutex around event append only,
  never around the decoder call) and emits its event *between* the
  surrounding callback events — this ordering is correct and the replay
  spec must accept it (see 2.2 spec extension).
- **Zero-length objects**: `BindDecoder` inspects the state; if it is
  already terminal, emit `construct_final` with the mapped result instead
  of `init`.

### 1.3 Converter: `dev/internal/cmd/trace2tla`

`trace2tla -input trace.ndjson -module <Name> -blocks <Z> -output <file>`
renders exactly the format `genobjecttrace.render` produces today
(`ImplementationBlocks`, `ImplementationTrace` as a TLA sequence of
records). Rules:

- Strings pass through a whitelist regex `^[a-z0-9_-]+$`; anything else is
  an error (no escaping business).
- Ints must fit int32 (TLC).
- Trace length cap 4096 events (error above; TLC replay is linear but keep
  a bound).
- Reject traces that do not start with `init`/`construct_final` or contain
  ops outside the closed set.

### 1.4 Refactor `genobjecttrace` (no behavior change)

`genobjecttrace` keeps its scenarios and CLI, but internally: drive via
`objtrace.Recorder`, then render via the same code path as `trace2tla`.
`task verify:generated` must produce byte-identical committed `.tla` files
before and after this refactor — that is the acceptance test. (The existing
`add_wait`/`block_complete` op choice at fixed packet indexes is reproduced
by the classifier automatically.)

### 1.5 Harvest drivers

1. **Test harvest**: build-tag-free, env-var driven. In the *dev module*,
   add `dev/internal/harvest` with table-driven drivers that re-run the
   public-API scenarios from the root package's tests (loss patterns,
   duplicates, grouped packets, invalid packets, multi-block, zero-length,
   verify/delivery failure, conflict). Do **not** instrument the production
   tests themselves — that would couple the root module to dev tooling.
   Command: `dev/internal/cmd/traceharvest -output <dir>` writes one
   `.ndjson` per execution, named by fingerprint (1.6).
2. **Fuzz-corpus harvest**: `traceharvest -fuzz-corpus` replays the checked
   in `testdata/fuzz/FuzzObjectCodecSourcePacketRoundTrip` corpus (and the
   go-fuzz cache dir if `-cache` is passed) through the recorder-wrapped
   decoder. Corrupted inputs that fail `ParseEncodingPacket` become
   `add_reject_invalid` events — those traces are valuable, keep them.
3. **e2e harvest** (follow-up, separate repo): `raptorgo-e2e` gets the same
   recorder wrapper and ships `.ndjson` artifacts; out of scope here except
   that the schema above is the contract.

### 1.6 Fingerprint, dedupe, curation

- Fingerprint = SHA-256 over the canonical NDJSON rendering with `sbn`,
  `esi`, `symbols` zeroed (packet attribution must not affect identity).
  File name: `<first-12-hex>.ndjson`.
- Curation algorithm (in `traceharvest`): greedy novelty selection.
  Maintain a set of seen `(op, result, state, completedDelta)` tuples;
  a trace is kept if it contributes at least one unseen tuple or an unseen
  `(prevOp, op)` bigram; hard cap `-max` (default 64) with deterministic
  priority: fewest events first, then lexicographic fingerprint. Log every
  dropped trace with the reason (no silent caps).
- Committed corpus lives in `dev/formal/corpus/objectdecoder/*.ndjson`.
  Regeneration is deterministic; a nightly drift check re-runs the
  harvester and diffs (same pattern as `verify:generated`).

### 1.7 Acceptance criteria (WS1)

- `task verify:generated` byte-identical after the genobjecttrace refactor.
- `traceharvest` produces a committed corpus of ≥ 20 traces including at
  least one each of: multi-block delivery, verify-fail, delivery-fail,
  decode-fail, conflict-reject, invalid-reject, terminal-reject,
  zero-length construct, grouped-packet delivery, reentrant-callback
  rejection.
- Round-trip: every corpus trace converts with `trace2tla` and passes TLC
  replay (via WS2).

---

## WS2 — Parametric replay harness

### 2.1 The instantiation trick

TLC requires module name = file name, so "parametric data" is done with a
**fixed-name workdir**, not config magic. New static files:

- `dev/formal/replay/ObjectDecoderReplay.tla` — identical in shape to
  today's `ObjectDecoderTrace.tla`: `EXTENDS ObjectDecoderReplayData`,
  `INSTANCE ObjectDecoderTraceSpec WITH Trace <- ImplementationTrace,
  Blocks <- ImplementationBlocks, ...`.
- `dev/formal/replay/ObjectDecoderReplay.cfg` — copy of
  `ObjectDecoderTrace.cfg`.

Runner `dev/internal/cmd/tracereplay`:

```
tracereplay -corpus dev/formal/corpus/objectdecoder [-j 2] [-keep-failed dir]
```

For each `.ndjson`: create temp dir; write `ObjectDecoderReplayData.tla`
(via the trace2tla library) with `-blocks` read from a `blocks` field in
the trace's first record (add it to the `init`/`construct_final` event in
WS1); copy `ObjectDecoderReplay.tla`, `ObjectDecoderReplay.cfg`, and
`ObjectDecoderTraceSpec.tla` into it; run
`dev/scripts/tlc.sh -noGenerateSpecTE ObjectDecoderReplay.tla -config
ObjectDecoderReplay.cfg` with the temp dir as working directory. Exit
nonzero listing every failing fingerprint; `-keep-failed` preserves the
workdir for debugging. Default `-j 1` (TLC is memory-hungry); allow small
parallelism.

### 2.2 Spec extension (one careful TLA change)

`ObjectDecoderTraceSpec.tla` gains `Apply` disjuncts for the new ops — all
three are strict no-ops guarded on `state = "receiving"`:

```tla
\/ /\ record.op \in {"add_reject_invalid", "add_reject_limit", "add_reject_conflict"}
   /\ record.result \in {"invalid", "limit", "conflict"}
   /\ state = "receiving"
   /\ UNCHANGED <<state, completed, verification, delivery>>
```

plus a `construct_final` initial-record alternative in `Init` (terminal
state at index 1 with `verification`/`delivery` counters matching the
result), and a relaxation for the reentrant case: `terminal_add` must also
be accepted when `state \in {"decoded", "verified"}` — extend
`TerminalStates` usage accordingly (introduce
`RejectingStates == TerminalStates` — note `decoded`/`verified` are already
in the spec's `TerminalStates` set; verify this covers the reentrant
ordering where `terminal_add` appears between `verify_enter` and
`verified`, and add a dedicated positive witness trace for it).

Every spec change must keep all four existing committed scenario traces and
all five existing forged traces behaving exactly as today (`verify:tla` and
`verify:tla:negative` unchanged and green).

### 2.3 Machine-generated negative controls: `dev/internal/cmd/tracemutate`

For each corpus trace, apply every applicable operator; each mutant must
make TLC fail with the exact pinned message
`Error: Temporal property TraceComplete was violated.` (all operators
below violate trace-step matching, which manifests as TraceComplete):

| operator | transformation | applicable when |
|---|---|---|
| `drop-step` | delete event i (for each non-init i, capped at 8 mutants/trace by stable stride) | always |
| `forge-binding` | set `bindingOK` FALSE on a `verify_enter`/`verified`/`deliver_enter` | callback events exist |
| `early-deliver` | move first `deliver_enter` immediately before `verified` | delivered traces |
| `skip-verify` | delete `verified` event but keep `deliver_enter` | delivered traces |
| `resurrect` | change a `terminal_add` into `add_wait` with state receiving | terminal-reject traces |
| `forge-completion` | on final `verify_enter`, decrement `completed` | multi-block traces |
| `double-delivery` | duplicate the `deliver_enter` event | delivered traces |

Runner mode `tracereplay -negative` applies operators in-memory, replays,
and asserts failure. Gate rule: every operator must have applied to at
least one corpus trace, otherwise the gate itself fails (prevents a vacuous
mutator).

### 2.4 Task wiring

```yaml
verify:trace:corpus:      # nightly (Java): tracereplay over committed corpus
verify:trace:negative:    # nightly (Java): tracereplay -negative
harvest:traces:           # local/nightly: regenerate corpus, diff for drift
```

Add both `verify:trace:*` tasks to the nightly `verify` aggregate, after
`verify:tla:negative`. `task ci` gains only the Java-free pieces: corpus
NDJSON well-formedness + fingerprint-name check + classifier unit tests.

### 2.5 Acceptance criteria (WS2)

- One spec file, one cfg, N traces; the four legacy scenario `.tla`
  wrappers remain until WS3 lands, then are retired in a follow-up (keep
  the committed Data modules; `verify:generated` still guards them).
- `tracereplay` over the WS1 corpus: all pass; `-negative`: all mutants
  fail with the pinned message; operator-coverage rule enforced.

---

## WS3 — Action coverage

### 3.1 Manifest

`dev/formal/coverage/actions.json` — the abstract action alphabet, one
entry per `Apply` disjunct of `ObjectDecoderTraceSpec`:

```json
{"v":1,"actions":[
 {"name":"AddWait","op":"add_wait","result":"ok"},
 {"name":"BlockComplete","op":"block_complete","result":"ok","completedDelta":1},
 {"name":"VerifyEnter","op":"verify_enter","result":"ok"},
 {"name":"Verified","op":"verified","result":"ok"},
 {"name":"DeliverEnter","op":"deliver_enter","result":"ok"},
 {"name":"FinalizeDelivered","op":"add_return","result":"ok"},
 {"name":"FinalizeVerifyFailed","op":"add_return","result":"verification_error"},
 {"name":"FinalizeDeliveryFailed","op":"add_return","result":"delivery_error"},
 {"name":"DecodeFailure","op":"decode_failure","result":"decode_error"},
 {"name":"TerminalReject","op":"terminal_add","result":"terminal"},
 {"name":"RejectInvalid","op":"add_reject_invalid","result":"invalid"},
 {"name":"RejectLimit","op":"add_reject_limit","result":"limit"},
 {"name":"RejectConflict","op":"add_reject_conflict","result":"conflict"},
 {"name":"ConstructDelivered","op":"construct_final","result":"ok"},
 {"name":"ConstructVerifyFailed","op":"construct_final","result":"verification_error"},
 {"name":"ConstructDeliveryFailed","op":"construct_final","result":"delivery_error"}]}
```

### 3.2 Classifier and report: `dev/internal/cmd/tracecover`

For each corpus trace, classify every event (skip index 1) by exact
`(op, result[, completedDelta])` match against the manifest. Rules:

- An event matching **zero** manifest actions is a hard error — either the
  harvester emitted garbage or the manifest is stale; both must fail.
- An event matching **more than one** action is a manifest bug; fail.
- Output: per-action counts, per-action witness fingerprints (first 3),
  uncovered action list. `--require-full` exits nonzero on any uncovered
  action. Machine-readable JSON plus a human table.

### 3.3 Manifest legality gate (keeps the manifest honest)

Coverage must not drift from the spec. Two controls:

1. **Witness replay**: `dev/formal/corpus/witness/` holds one minimal
   hand-curated trace per manifest action (most come straight from the WS1
   corpus). Nightly: `tracereplay` over witnesses must pass, and
   `tracecover --require-full` over witnesses alone must report exactly
   100% — proving every manifest action is model-legal and classifiable.
2. **Negative control**: a fixture manifest with a bogus action
   (`{"name":"Bogus","op":"add_wait","result":"conflict"}`) must make the
   witness gate fail (no witness can cover it); asserted in a Go unit test
   of `tracecover`.

### 3.4 Gates

- `task ci`: `tracecover` over committed corpus with a warning threshold
  (report only).
- Nightly: `--require-full` over corpus ∪ witnesses. Full coverage is
  achievable because WS1 acceptance already demands one trace per family.
- The report JSON is written to `dev/formal/coverage/report.json` by the
  nightly harvest job for the docs update (see final section).

### 3.5 Acceptance criteria (WS3)

100% action coverage on corpus ∪ witnesses; both negative controls red-team
tested; `docs/invariants.md` row added (see final section).

---

## WS4 — Model-based testing (model ⇒ implementation)

### 4.1 Why a new model, and the exact refinement claim

Trace replay checks *implementation ⇒ model*. WS4 checks the converse on a
bounded domain: every action sequence the model admits is **realized** by
the production decoder with matching observations. This catches
over-permissive models (a spec bug trace replay can never see) and
under-capable implementations.

`DecoderLifecycle.tla` is the wrong target: it deliberately over-abstracts
(explicit `Decode` may be delayed arbitrarily after `Required` symbols;
`Abort` fires spontaneously). The real decoder fuses
decode/verify/deliver into the completing `AddPacket`. Driving the
implementation from `DecoderLifecycle` behaviors would generate mostly
unrealizable schedules. Do **not** bend the abstract model to the API — it
is exploration evidence. Instead:

**Deliverable: `dev/formal/ObjectDecoderModel.tla`** — a new explorable
model whose every action corresponds to exactly one public-API-triggerable
step, at the same abstraction level as `ObjectDecoderTraceSpec`. The MBT
claim then is: every behavior of `ObjectDecoderModel` (bounded constants)
maps to a driver run whose observed event sequence classifies to the same
action sequence (WS3 classifier) and whose harvested trace passes WS2
replay. Additionally, a TLC refinement check
(`ObjectDecoderModel => DecoderLifecycle`-style mapping) is desirable but
optional; note it as stretch.

### 4.2 `ObjectDecoderModel.tla` (precise definition)

Constants (two cfgs, see 4.5):

```tla
CONSTANTS Blocks,          \* 1 or 2
          K,               \* symbols per block, fixed 2
          AllowDecodeFail, \* BOOLEAN, cfg B only
          InjectFault      \* BOOLEAN, mutation gate sentinel
```

Variables (deliberately flat — no function-valued variables, to keep the
dot parser trivial; Blocks ≤ 2 is enforced by ASSUME):

```tla
VARIABLES state,        \* "receiving" | trace-spec States
          accepted0,    \* SUBSET 0..K-1, block 0 accepted source ESIs
          accepted1,    \* SUBSET 0..K-1 (empty and frozen when Blocks = 1)
          completed,    \* 0..Blocks
          verification, delivery   \* 0..1
```

Actions (each one names its API realization; keep this comment discipline
in the module):

| action | guard | effect | realization |
|---|---|---|---|
| `AddWait(b, e)` | state=receiving, adding e to block b leaves it incomplete OR e already accepted (idempotent duplicate) | accepted_b' = accepted_b ∪ {e} | single-symbol source packet (b,e); duplicate = resend same packet |
| `BlockComplete(b, e)` | receiving, add completes block b, completed' < Blocks (non-final) | accepted update, completed+1 | same packet kind |
| `FinalizeVerifyEnter(b, e)` | receiving, add completes the **last** incomplete block | state'="decoded", completed'=Blocks, verification'=1 | the completing packet; recorder emits `verify_enter` |
| `Verified` | state="decoded", verifier outcome ok | state'="verified" | verifier callback returns nil |
| `VerifyFailed` | state="decoded" | state'="verification-failed" | verifier returns error → surfaces as `add_return`/`verification_error` |
| `DeliverEnter` | state="verified" | delivery'=1 | deliverer invoked |
| `Delivered` | state="verified", delivery=1 | state'="delivered" | deliverer returns nil → `add_return`/`ok` |
| `DeliveryFailed` | state="verified", delivery=1 | state'="delivery-failed" | deliverer returns error |
| `RejectInvalid` | state="receiving" | UNCHANGED | packet with SBN ≥ Blocks (`ErrInvalidPacket`) |
| `RejectConflict(b, e)` | receiving, e ∈ accepted_b | UNCHANGED | resend (b,e) packet with one payload byte flipped |
| `TerminalAdd` | state ∈ TerminalStates (incl. transient decoded/verified via reentrancy) | UNCHANGED | resend any packet after terminal; reentrant variant from inside callbacks |
| `DecodeFail` | AllowDecodeFail, state=receiving, Blocks=1 | state'="decode-failed" | rank-deficient fixture, 11th deficient add |
| `AdversarialSkipVerify` | InjectFault, state="decoded" | state'="delivered" | mutation-gate sentinel only |

Invariants: reuse the trace-spec invariant names (`ExactlyOnce`,
`NoDeliveryWithoutVerification`, `NoSuccessBeforeCompletion`, `TypeOK`)
plus `NonTerminalCanProgress` and `EveryNextTransitionAllowed` in the
`DecoderLifecycle` house style, and a `TerminalStable` action property.
`ObjectDecoderModelMutation.cfg` sets `InjectFault = TRUE` and must fail
exactly `EveryNextTransitionAllowed` (extend `verify:tla:negative`).

Note the model intentionally splits the fused finalization into the same
micro-steps the recorder observes (`FinalizeVerifyEnter → Verified →
DeliverEnter → Delivered`); this makes model behaviors and event traces
share one alphabet, which is what makes the whole pipeline composable.
The model must force the micro-step order with its guards (verification
and delivery counters) so no spurious interleavings exist.

State-space sanity: Blocks=2, K=2 → ≤ 4·4·3·7·2·2 ≈ 2.7k states; trivial
for TLC.

### 4.3 Behavior extraction: TLC dot dump + parser

Generation command (nightly, Java):

```
dev/scripts/tlc.sh -noGenerateSpecTE -dump dot,actionlabels /tmp/.../model.dot \
    ObjectDecoderModel.tla -config ObjectDecoderModelMBT.cfg
```

Facts about the pinned TLC v1.8.0 dot output the parser may rely on
(verify once against the pinned jar and encode in parser tests):

- Nodes: `"<fingerprint>" [label="<state conjunction>"...]` where the label
  is `/\ var = value` lines separated by `\n`.
- Edges: `"<fp1>" -> "<fp2>" [label="<ActionName>"...]` when
  `actionlabels` is on; the label is the action's definition name without
  parameters.
- Init states are marked with `[style = filled]`.

Parser package `dev/internal/mbt/dotparse`:

- Recursive-descent value parser for the restricted grammar only:
  `int`, `"string"`, `{int, ...}` (int sets), `TRUE|FALSE`. Anything else
  (functions, records, sequences) is a hard error — the model definition
  above guarantees they cannot appear; the error keeps the model and
  parser honest if someone extends the model.
- Output: `map[fp]State`, `[]Edge{Src, Dst, Action string}`, `[]fp` inits.
- Determinize immediately: sort states by fingerprint, edges by
  `(src, action, dst)`.
- Table-driven tests against a checked-in dot fixture generated once from
  the pinned jar (committed under `dev/internal/mbt/dotparse/testdata/`).

**Parameter recovery** (dot labels carry no parameters): for `AddWait` /
`BlockComplete` / `FinalizeVerifyEnter` / `RejectConflict`, recover `(b,e)`
from the state delta (`accepted_b' \ accepted_b`, or for idempotent
duplicates and conflicts pick the smallest member of `accepted_b`,
deterministic). For parameterless actions nothing to recover. Encode the
recovery rules per action in one table; unknown action name → error.

### 4.4 Schedule generation: edge-coverage path selection

Target: cover every **edge** (state, action, state') of the dumped graph —
strictly stronger than action-name coverage, and the honest statement of
what WS4 achieves (not all paths; say so in docs).

Algorithm (`dev/internal/mbt/schedule`):

```
parse graph; assert exactly one init state per cfg
uncovered := all edges (stable order)
paths := []
for each edge E in stable order, skip if covered:
    p := BFS-shortest-path(init → E.src)  (tie-break: lexicographic edge order)
    p := p + [E]
    while len(p) < DepthCap(32):          # greedy extension eats more edges
        pick the stable-first uncovered out-edge of p.end; break if none
        append it
    mark all edges of p covered
    paths.append(p)
emit paths (stable order)
```

Properties: terminates (every iteration covers ≥1 edge), deterministic,
path count ≤ edge count (in practice ~15–30 schedules for cfg A). A
`DecodeFail` edge only exists in cfg B's graph; the generator runs per cfg
and tags schedules with their fixture.

Each path becomes a schedule JSON
(`dev/formal/mbt/schedules/<cfg>/<seq>-<pathhash>.json`):

```json
{"v":1,"model":"ObjectDecoderModel","cfg":"A","fixture":"two-block",
 "steps":[
  {"action":"AddWait","sbn":0,"esi":1,
   "expect":{"state":"receiving","completed":0,"result":"ok","event":"add_wait"}},
  {"action":"FinalizeVerifyEnter","sbn":1,"esi":0,
   "callbacks":{"verify":"ok","deliver":"fail"},
   "expect":{"state":"delivery-failed","completed":2,"result":"delivery_error",
             "events":["verify_enter","verified","deliver_enter","add_return"]}}
 ]}
```

Schedule assembly detail: the micro-step actions (`Verified`,
`DeliverEnter`, …) are **folded into** the preceding
`FinalizeVerifyEnter` step's `callbacks` + `expect.events`, because one
API call realizes the whole run of micro-steps. The folding is a pure
function of the path suffix after `FinalizeVerifyEnter`
(`Verified→DeliverEnter→Delivered` ⇒ verify ok, deliver ok;
`VerifyFailed` ⇒ verify fail; `Verified→DeliverEnter→DeliveryFailed` ⇒
verify ok, deliver fail). A `TerminalAdd` edge appearing *between*
micro-steps (reachable in the model, e.g. decoded→decoded) is folded as a
reentrancy instruction: `"callbacks":{"verify":"ok+reentrantAdd", ...}`
and expected event `terminal_add` inside the fused window. This folding
function is the trickiest code in WS4 — unit-test it against hand-written
path fixtures for all five finalization shapes (delivered, verify-fail,
delivery-fail, reentrant-during-verify, reentrant-during-deliver).

### 4.5 Fixtures and concretization (the refinement mapping, executable)

`dev/internal/mbt/driver`:

**Fixture A ("two-block")** — reuse `genobjecttrace`'s finalization
fixture verbatim: `OTI{TransferLength:13, SymbolSize:4, SourceBlocks:2,
SubBlocks:2, SymbolAlignment:1}`, object = the same 13 bytes, K=2 per
block, `DefaultLimits()`. Pre-generate all four source packets with
`shortenFinal=true`. Realization table:

| action | packet / call |
|---|---|
| `AddWait/BlockComplete/FinalizeVerifyEnter (b,e)` | pre-generated source packet (b,e) |
| `RejectInvalid` | 5-byte packet `[0x02, 0, 0, 0, 0x00]` (SBN 2 ≥ Z=2 → `ErrInvalidPacket`); rotate variants: short packet (`len < 5`), SBN=3, to spread parser coverage |
| `RejectConflict (b,e)` | copy of packet (b,e) with payload byte 0 XOR 0xFF. Constraint: **only ESIs with no trailing FEC padding** may be corrupted, else `ParseEncodingPacket` rejects nonzero padding and the event misclassifies as invalid. With this fixture the padded symbol is block 1's final symbol; the generator's parameter-recovery must therefore prefer `(b,e)` ≠ the padded ESI; assert at driver level that the conflict packet parses. |
| `TerminalAdd` | resend packet (0,0) after terminal; reentrant variant: verifier/deliverer closure calls `recorder.AddPacket(packet00)` and asserts `ErrObjectTerminal` |
| callbacks | recorder-wrapped verifier/deliverer with outcome per schedule (`ok`, `fail`, `ok+reentrantAdd`) |

**Fixture B ("rank-deficient")** — exactly `executeDecodeFailure`'s setup
(K'=10, repair ESIs 319..328 pre-fed as ten `AddWait` steps baked into the
fixture prologue, limits with `MaxDecodeAttempts:1`). The model's
`DecodeFail` action realizes as the ESI 329 add. Model cfg B uses
`Blocks=1, K=1` abstractly; the prologue adds are recorded as `add_wait`
events and are **prepended to every cfg-B schedule's expected events** by
the driver, not the generator (the model doesn't know about them — an
abstraction-mapping note in the module comment must say so).

**Nondeterminism rule (critical):** all schedules use source-symbol or
pinned-deficient-repair packets only. Source-only sets complete a block
deterministically at exactly K distinct ESIs (systematic property —
symbols are copied through, no matrix solve). Never generate schedules
with arbitrary repair ESIs; decodability at K would depend on rank and the
oracle would flake. If a future extension wants repair-symbol schedules,
it must preflight each candidate ESI set by running encoder+decoder once
at generation time and pin the validated set into the schedule.

**Zero-length**: one hand-written schedule per construct outcome
(`ConstructDelivered/…VerifyFailed/…DeliveryFailed`) with
`OTI{TransferLength:0, SymbolSize:4, SourceBlocks:1, SubBlocks:1,
SymbolAlignment:1}` — the model treats these as single-action behaviors;
add them to cfg A's graph via an `InitFinalize` action or keep them as
manually-authored schedules outside the generator (simpler; choose this,
document it).

### 4.6 Driver, oracle, shrinking

`dev/internal/mbt/driver` (invoked by a dev-module test
`TestMBTSchedules` and by `dev/internal/cmd/mbtrun`):

Per schedule: build fixture → construct recorder + decoder → for each
step: build the packet, call `recorder.AddPacket`, then assert
**Oracle 1** (local, Java-free):

- `decoder.State().String() == expect.state`;
- `decoder.CompletedBlocks() == expect.completed`;
- error class: map `expect.result` → sentinel (`ok`→nil,
  `verification_error`→`ErrObjectVerification`, `delivery_error`→
  `ErrObjectDelivery`, `decode_error`→`ErrObjectDecoder`,
  `terminal`→`ErrObjectTerminal`, `invalid`→`ErrInvalidPacket`,
  `conflict`→`ErrObjectDecoder`+`codec.ErrDecoderDuplicateConflict`) and
  check `errors.Is` both positively and negatively (`ok` asserts nil);
- recorder event tail since the previous step equals `expect.events`
  (op sequence exactly — this is where the fused-window folding is
  validated against reality);
- callback binding flags all true.

**Oracle 2** (nightly, Java): write each run's NDJSON trace next to the
schedule and `tracereplay` it — the implementation trace of every
model-generated schedule must itself satisfy the trace spec. This closes
the loop model → schedule → implementation → trace → model and will catch
drift between `ObjectDecoderModel` and `ObjectDecoderTraceSpec` (the two
formal artifacts most likely to diverge silently).

**Coverage assertion**: after running all schedules, `tracecover` over the
produced traces must cover every manifest action except the construct
family when only cfg A ran — wire the expected-coverage list into the test
table rather than hardcoding "all".

**Shrinking**: on any Oracle-1 mismatch, ddmin over the schedule's steps:
repeatedly try removing chunks (halving granularity) and re-running; keep
a removal if the same step-index-relative failure signature
(`action, expect vs got` tuple) persists. Deterministic (fixtures are
deterministic). Write the minimized schedule + trace to
`dev/formal/mbt/failures/<pathhash>/` and print the repro command.

### 4.7 Regeneration and drift policy

Committed artifacts: `ObjectDecoderModel.tla`, both cfgs + mutation cfg,
`dev/formal/mbt/schedules/**` (generated, deterministic), the dotparse
test fixture. Nightly job:

1. `verify:tla` additions: model-check `ObjectDecoderModelMBT.cfg` (cfg A)
   and cfg B; mutation cfg in `verify:tla:negative` pinned to
   `Error: Temporal property EveryNextTransitionAllowed is violated.`
   (match the existing PlannerProgress pinning style; confirm exact TLC
   message wording on first run and pin whatever v1.8.0 emits).
2. `mbt:generate` — dot dump + schedule regeneration; `git diff
   --exit-code dev/formal/mbt/schedules` (drift gate, mirrors
   `verify:generated`).
3. `mbt:run` — driver + Oracle 2 + coverage assertion.

`task ci` runs `mbt:smoke`: driver + Oracle 1 only, over a pinned subset
(one schedule per finalization shape + one reject of each kind + fixture
B + zero-length trio ≈ 10 schedules), no Java.

### 4.8 Acceptance criteria (WS4)

- `ObjectDecoderModel` passes all its invariants/properties under both
  cfgs; mutation cfg fails exactly the pinned property.
- Generated schedules achieve 100% edge coverage of both dumped graphs;
  the generator logs `edges=N covered=N paths=P`.
- All schedules pass Oracle 1 on every CI platform (amd64 + 386 — packet
  fixtures are word-size independent, but run it under `task test:386`'s
  environment once to confirm).
- Nightly Oracle 2 green; coverage assertion green.
- A seeded implementation bug is caught end-to-end at least once as a
  red-team exercise: temporarily patch `object_decoder.go` to skip the
  `decoder.state = ObjectVerified` assignment (or deliver on
  verification failure), confirm a schedule fails with a shrunk repro,
  then revert. Record the exercise's output in the PR description; do not
  commit the patch.

### 4.9 Known limitations to state in docs (do not oversell)

- Edge coverage of a bounded model (Blocks ≤ 2, K = 2, source-only +
  one pinned deficient set), not all paths, not all parameters.
- Single-goroutine drivers; the concurrency protocol (mutex-free
  callbacks, racing `AddPacket`) remains future work (a separate
  interleaving model).
- Grouped packets, sub-block variety beyond the fixed fixture, and
  arbitrary repair sets are exercised by WS1 harvesting (replay
  direction) but not by WS4 schedules.

---

## File layout summary (new)

```
dev/internal/objtrace/              # WS1 recorder + event schema + fingerprint
dev/internal/cmd/trace2tla/         # WS1 converter
dev/internal/cmd/traceharvest/      # WS1 harvest drivers + curation
dev/internal/cmd/tracereplay/       # WS2 workdir runner (+ -negative)
dev/internal/cmd/tracemutate/       # WS2 mutation operators (library + CLI)
dev/internal/cmd/tracecover/        # WS3 classifier + report
dev/internal/mbt/dotparse/          # WS4 dot parser
dev/internal/mbt/schedule/          # WS4 path selection + folding + JSON
dev/internal/mbt/driver/            # WS4 fixtures, oracle, shrinking
dev/formal/replay/                  # WS2 fixed-name replay spec + cfg
dev/formal/corpus/objectdecoder/    # WS1 committed corpus (*.ndjson)
dev/formal/corpus/witness/          # WS3 per-action witnesses
dev/formal/coverage/actions.json    # WS3 manifest
dev/formal/ObjectDecoderModel.tla   # WS4 model (+ 3 cfgs)
dev/formal/mbt/schedules/{A,B}/     # WS4 committed schedules
```

## Implementation order (each step lands green on `task ci` + nightly)

1. WS1 recorder + classifier + unit tests; refactor `genobjecttrace` on
   top; prove `verify:generated` byte-identical.
2. WS2 replay spec/cfg + `tracereplay`; migrate the 4 scenarios to it
   (keep legacy wrappers running in parallel for one step).
3. WS1 harvest drivers + committed corpus; WS2 spec extension for new ops
   with positive witnesses; corpus replay green.
4. WS2 `tracemutate` + negative gate.
5. WS3 manifest + `tracecover` + witness gate.
6. WS4 model + cfgs + mutation gate (pure TLA step, no Go).
7. WS4 dotparse + schedule generator + committed schedules.
8. WS4 driver + Oracle 1 + `mbt:smoke` in ci.
9. WS4 Oracle 2 + drift gates in nightly; red-team exercise.
10. Docs update (below); retire legacy per-scenario replay wrappers.

Estimated sizes: steps 1–2 ≈ 1.5k LoC dev-module Go; 3–5 ≈ 1k; 6 ≈ 300
lines TLA; 7–9 ≈ 1.5k Go. No production-module diffs anywhere.

## Documentation updates (last step)

- `docs/invariants.md`: add rows for corpus replay, machine-generated
  negative controls, action coverage, and MBT edge coverage, each with its
  exact gate name and limitation sentence.
- `README.md` "Correctness evidence" list: replace item 8's trailing
  clause with harvested-corpus + MBT phrasing, e.g. "bounded TLA+ models,
  replay of every harvested CI/fuzz execution trace with machine-generated
  negative controls and measured 100% model-action coverage, and
  model-derived schedules driven against the production decoder with
  edge-coverage of the bounded object-decoder model". Keep the honesty
  paragraph: still testing, not proof, and say what the bounds are.
- `dev/formal/README.md`: new sections for the replay harness, the
  coverage manifest, and `ObjectDecoderModel` (mirror the level of detail
  of the existing `HybridTrace` section, including every negative control
  and its pinned failure).
