# Mutation testing

Raptorgo uses mutation testing to answer a narrower question than statement
coverage: would the tests reject a plausible implementation defect on a path
they exercise? The pinned runner is
[`go-mutesting/v2`](https://github.com/jonbaldie/go-mutesting), selected for
Go 1.26 support, stable mutant IDs, coverage-aware and per-test execution,
timeouts, changed-line filtering, score gates, and agent-ready reports.

Mutation testing complements the existing independent algebra, Rust/C++
interoperability, fuzzing, and TLA+ evidence. It does not replace any of them,
and the expensive release, oracle, and model-checking suites are not rerun for
every source mutation.

## Three execution tiers

`task mutation:gate` is part of ordinary CI. It mutates four compact kernels:

- resource-limit profiles and validation;
- FEC Payload ID construction and wire encoding;
- GF(256) table construction and scalar arithmetic; and
- RFC partition arithmetic.

The gate deliberately does not use coverage filtering or per-test selection.
Go coverage cannot mark constant declarations as executed, and these constants
include security and wire bounds worth mutating. Every non-equivalent mutant in
the curated scope must be killed; within the curated files the committed
baseline contains only the three reviewed equivalent mutants described below.
The raw MSI must remain at least 98 percent, and any new survivor fails the
task.

`task mutation:changed` applies the broad operator set only to lines changed
from `HEAD`, or from the ref named by `MUTATION_DIFF_BASE`. It uses coverage
and per-test selection for speed and fails on every new survivor. Set the base
to the pull request's merge base when the branch contains multiple commits.
Git cannot select untracked lines, so add new source files to the index before
running this tier. `mutation.sh` forces standard `a/`-`b/` git diff prefixes
because the changed-line filter parses `+++ b/<path>` headers; user-level
`diff.mnemonicPrefix` or `diff.noprefix` configuration would otherwise empty
the filter and silently pass the gate. Equivalent survivors accepted from this
tier are recorded in the baseline and documented below by class.

`task mutation:audit` is a scheduled/manual discovery campaign over all
hand-written runtime source. It excludes tests, command-line development
tools, formal generators, oracles, and generated RFC tables. It is
coverage-aware and writes ignored `go-mutesting-agentic.json` and
`go-mutesting-summary.json` reports. Survivors from this tier are reviewed and
turned into tests when they represent observable behavior; the audit is not a
blanket score gate because large stateful solvers produce more equivalent
mutants and require independent oracle/formal interpretation.

Use `task mutation:list` to preview the candidate population. Set
`MUTATION_WORKERS` to bound concurrency.

## Accepted equivalent mutants

The baseline is intentionally tiny and reviewed by stable mutant ID:

| Stable ID | Mutation | Why behavior is unchanged |
| --- | --- | --- |
| `6d9dedf7e37fbc705d359febc2a865da` | Extend `copy(exp[255:], exp[:255])` source to `exp[:256]`. | The destination length is 255, and Go `copy` always copies the smaller length, so the same 255 octets are copied. |
| `64a676bc87479142a23d42c2f00c50e0` | Return octet 1 instead of 0 with `ok=false` from `Inv(0)`. | The octet is explicitly undefined when `ok` is false and must not be consumed. |
| `0c01f436f4aa833019d778a3b235504a` | Return size 1 instead of 0 with a non-nil partition-index error. | The size is undefined on error and must not be consumed. |

## Accepted changed-line survivors (July 2026 hot-loop optimization)

The 2026-07 kernel/executor optimization round added 38 reviewed entries,
grouped here by class because the underlying diffs are mechanical single-token
mutations of the listed lines:

| Location | Classes | Why behavior is unchanged |
| --- | --- | --- |
| `internal/matrix/hybrid_binary.go` `AppendDenseOnes` word loop (lines 389-395, 26 entries) | arithmetic, bitwise, numbers, branch/conditional, expression | The first-word and last-word masks and their index arithmetic guard bits below `firstSlot` and at or above `maxDense`. Those bits are provably always zero: `SetInitialDense` only writes the initial dense suffix, `FreezeNext` only writes slots at or above the current `firstSlot`, and XOR preserves zeros. The masks are defensive against future write paths; mutating them cannot change output today. `ForEachDenseOne` carries the identical defensive masks. |
| `internal/codec/hybrid_execute.go` lines 254, 344 (4 entries) | comparison, numbers | `plan.parameters.H > 0` fast-path guards. Valid RFC 6330 parameters have `H >= 10`, so `>= 0` gathers redundantly with identical results and `> 1` can never skip a reachable case. |
| `internal/codec/hybrid_execute.go` lines 256, 262, 270, 316, 325 (5 entries) | error-guard | Guards on `AppendDenseOnes`, `PhysicalColumn`, and `RowView` calls that cannot fail for a validated plan on a finalized matrix; the guards are defensive depth. |
| `internal/codec/hybrid_execute.go` line 280 (2 entries) | statement, numbers | `hdpcValues[pivotColumn] = 0` canonicalizes an eliminated column. Pivot columns occupy logical positions below `finalI` after their step, and later reads touch only later pivot columns and logical columns at or above `finalI`, so the zeroed cell is never read again. The canonical state is separately pinned by the dev-module differential trace gates. |
| `internal/codec/hybrid_execute.go` line 188 (1 entry) | arithmetic | The gather scratch capacity hint. A larger capacity changes only the size of one allocation; correctness never depends on it, and a too-small capacity is killed by the equivalence and workspace tests. |

Never update `dev/mutation/baseline.json` automatically in CI. A proposed
curated-scope entry must include its exact diff and a behavioral-equivalence
explanation here; mechanically related changed-line survivors may be
documented as a class with their lines and one shared equivalence argument.
Deleting production behavior or weakening an invariant is not equivalent
merely because another gate happens to catch it.

## Interpreting results

- **Killed:** at least one selected test rejected the mutated implementation.
- **Escaped:** tests passed; inspect the diff and normally add an assertion or
  boundary case.
- **Not covered:** the broad audit found no covering test. Add coverage if the
  path is supported behavior; otherwise remove or explicitly justify dead
  code.
- **Errored/timed out:** treat as a tool or test-harness problem. Do not count
  it as a killed mutant until the cause is understood.

Run the clean test suite before investigating a survivor. Reproduce one mutant
by passing its stable ID to `./bin/go-mutesting --run-mutant-id=<id> <file>`.
