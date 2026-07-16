# Contributing

Thanks for your interest in raptorgo. Before opening a pull request, please
open an issue describing the change you have in mind — this is a
conformance-focused codec, and most changes need correctness evidence, not
just passing tests.

## Ground rules

- The production module stays pure Go: no CGo and no third-party
  dependencies in any Go package. `task lint` enforces this.
- RFC 6330 behavior changes must come with evidence at the layer they touch:
  unit invariants, differential vectors against the pinned Rust oracle, or
  an updated formal model. `docs/invariants.md` maps behavior to evidence.
- Generated artifacts (RFC tables, TLA+ trace data, corpus checksums) are
  regenerated, never hand-edited. `task verify:generated` must pass.

## Development workflow

```sh
task ci             # lint + tests + fuzz/bench smoke + verification gates
task test           # just the Go tests
task verify         # RFC pinning, generated artifacts, TLA+ models
task mutation:gate  # curated no-new-survivor mutation gate
```

`task ci` requires Go (see `go.mod` for the version), plus `curl`, `java`,
and a Rust toolchain for the verification and oracle gates. The heavier
release gates (`task test:release`) are not required for routine changes.
Mutation policy, broad audits, and baseline review are documented in
[`docs/mutation-testing.md`](docs/mutation-testing.md).

## Commit expectations

- Keep commits focused; describe *why* in the commit body.
- Run `task format` before committing.
- CI must be green before review.
