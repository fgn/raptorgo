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
the curated scope must be killed; the committed baseline contains only the
three reviewed equivalent mutants described below. The raw MSI must remain at
least 98 percent, and any new survivor fails the task.

`task mutation:changed` applies the broad operator set only to lines changed
from `HEAD`, or from the ref named by `MUTATION_DIFF_BASE`. It uses coverage
and per-test selection for speed and fails on every new survivor. Set the base
to the pull request's merge base when the branch contains multiple commits.
Git cannot select untracked lines, so add new source files to the index before
running this tier.

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

Never update `mutation/baseline.json` automatically in CI. A proposed entry
must include its exact diff and a behavioral-equivalence explanation here.
Deleting production behavior or weakening an invariant is not equivalent just
because another gate happens to catch it.

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
