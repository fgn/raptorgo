# ObjectVerified seeded-fault evidence

These development-only artifacts make the seeded production-bug experiment in
`trace-validation-log.md` reproducible from a clean checkout. The production
fault was never committed.

- `seeded-seven-step.json` is the original strict Oracle 1 schedule.
- `minimized.json` is the deterministic four-step ddmin result. It is an
  executable Oracle 1 repro, not a complete path through the dumped model: step
  deletion deliberately retains exact edge labels without reconnecting them.
- `failure.json` is the stable failure metadata produced by the faulty run.
- `trace/bug.ndjson` is the faulty partial implementation trace rejected by TLC.
- `fault.patch` is the exact temporary source mutation used by
  `task redteam:object-verified`; that task applies it only to a temporary Git
  archive and byte-compares regenerated evidence.

The clean implementation must execute both schedules successfully with trace
fingerprints `b6b4bb37bd42` and `066f3d9c54f7`. The faulty partial trace has
fingerprint `c6b02e883311` and must fail the temporal property `TraceComplete`.
