# raptorgo

`raptorgo` is a bounded, pure-Go implementation of the RaptorQ forward error
correction scheme in [RFC 6330](https://www.rfc-editor.org/rfc/rfc6330.html).
The runtime package has no CGo or third-party dependencies.

```sh
go get github.com/fgn/raptorgo
```

## Attribution and research status

RaptorQ is not our invention. Raptor codes are the work of Amin
Shokrollahi, building on Michael Luby's LT fountain codes; RaptorQ was
developed at Digital Fountain and Qualcomm and standardized by the IETF as
[RFC 6330](https://www.rfc-editor.org/rfc/rfc6330.html), authored by
M. Luby, A. Shokrollahi, M. Watson, T. Stockhammer, and L. Minder. This
repository is an independent implementation of that specification and
claims no rights in the RaptorQ code, the RaptorQ name, or the underlying
technology. Credit for the code belongs to its inventors; defects in this
implementation belong to this project.

The project is a research exercise in applying formal-methods and
differential-verification techniques to a nontrivial protocol
implementation. **It should not be read as "formally verified."** The
TLA+ artifacts model-check bounded abstractions of selected subsystems
and replay production-generated traces against those models; they check
finite safety properties of models and recorded traces, not the compiled
Go code, and establish no liveness of the real implementation. The exact
claim-to-evidence map is in [`docs/invariants.md`](docs/invariants.md),
the formal scope and its limits in [`formal/README.md`](formal/README.md),
and the bounded recovery statistics in
[`docs/recovery.md`](docs/recovery.md).

## Implemented behavior

The implementation covers:

- RFC Section 4.3 parameter derivation and canonical 12-byte OTI handling;
- source-block and sub-block partitioning for `Z >= 1` and `N >= 1`;
- grouped source and repair packets, including legal shortened source symbols;
- the complete RFC parameter tables, `Rand`, `Deg`, `Tuple`, and ESI/ISI map;
- systematic and arbitrary repair encoding through K=56,403;
- bounded hybrid binary/HDPC inactivation decoding through K=56,403;
- incremental multi-block object recovery with loss, reorder, and duplicates;
- an explicit `decoded -> verified -> delivered` trust boundary; and
- checked limits for packet, retained-symbol, attempt, dense-completion, and
  aggregate logical backing allocations.

The public decoder deliberately requires both a verifier and a deliverer. FEC
repairs erasures; it does not authenticate hostile packets or reconstructed
bytes. Applications should authenticate packets when possible and must verify
the reconstructed object before treating it as trusted.

## Minimal use

```go
oti, err := raptorgo.DeriveOTI(raptorgo.DerivationInput{
    TransferLength:             uint64(len(object)),
    DecoderBlockBytes:          1 << 20,
    MaxPayloadSize:             1280,
    SymbolAlignment:            4,
    MinSubSymbolAlignmentUnits: 8,
}, raptorgo.DefaultLimits())
if err != nil {
    return err
}

encoder, err := raptorgo.NewObjectEncoder(object, oti, raptorgo.DefaultLimits())
```

Call `encoder.Packet(sbn, firstESI, count, shorten)` for systematic or repair
packets. At the receiver, construct `NewObjectDecoder` with a generation
context, verifier, and delivery callback, then pass complete RFC encoding
packets to `AddPacket`. See [`example_test.go`](example_test.go) for a runnable
round trip.

Callers may also supply an OTI chosen by their content-delivery protocol;
`DeriveOTI` implements the RFC's recommended example algorithm rather than a
mandatory transport policy. `DefaultLimits` is conservative application
policy, while `RFCWireLimits` admits the full wire domain for parsing and
conformance work.

## Correctness evidence and methodology

The implementation is checked in independent layers:

1. strict extraction and SHA-256 pinning of the RFC and its normative tables;
2. exhaustive GF(256), mapping, tuple-domain, and structural invariants;
3. dense algebraic oracles against the bounded production hybrid solver;
4. byte-identical Rust parity through K=56,403 and selected `T=255` cases;
5. bidirectional Rust cross-decoding for grouped/shortened, multi-block,
   multi-sub-block, source-loss, and grouped-repair frames;
6. bidirectional C++ cross-decoding in the K=10/12/48 consensus domain for
   repair-only, mixed loss/reorder/duplicate, and deficient sets;
7. independent rank-sufficiency checks and a bounded preregistered Section 5.8
   K'=10 fixed-pool recovery profile, explicitly not an all-row proof; and
8. bounded TLA+ models plus production-derived positive traces and forged or
   deleted negative traces; and
9. a pinned mutation-testing gate over critical resource, wire, GF(256), and
   partition kernels, plus broader changed-line and scheduled audits.

RFC Editor erratum 5548 is applied: the maximum transfer length is
`942574504275`.

To be precise about what that evidence does and does not establish: the
TLA+ models and trace replays establish bounded safety properties of
abstractions and of recorded executions, with negative controls proving
the checks are not vacuous; differential vectors establish byte-identical
behavior on the domains actually compared; exhaustive tests establish
exactly the finite domains they enumerate (GF(256) algebra, parameter
tables, ESI/ISI maps); and the Section 5.8 recovery profile is a
preregistered, bounded statistical measurement — explicitly not a proof
over every RFC row and ESI. None of this amounts to a machine-checked
proof of functional correctness of the Go implementation, and no such
claim is made.

The compact evidence index is in [`docs/invariants.md`](docs/invariants.md).
Recovery statistics and their explicit limits are in
[`docs/recovery.md`](docs/recovery.md). TLA scope is documented in
[`formal/README.md`](formal/README.md).

## Development

```sh
task test
task test:386
task test:race
task mutation:gate
task verify
task oracle:test
task test:release
task bench
```

`task ci` runs the ordinary local correctness suite. Rust and C++ are
development-time oracles only and are never linked into production Go code.
See [CONTRIBUTING.md](CONTRIBUTING.md) for the change-evidence expectations
and [SECURITY.md](SECURITY.md) for the threat model and reporting channel.
Mutation scope and baseline policy are in
[`docs/mutation-testing.md`](docs/mutation-testing.md).

End-to-end dogfooding — a file-transfer CLI and lossy-network test suite
that consume only this module's public API — lives in the companion
repository [`raptorgo-e2e`](https://github.com/fgn/raptorgo-e2e).

## License

Copyright 2026 FGN Research and Development B.V. Licensed under the
[Apache License 2.0](LICENSE). Third-party material distributed with this
repository is listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

**Patent notice.** RaptorQ is specified by RFC 6330, for which intellectual
property rights disclosures have been filed with the IETF (see the
[IETF IPR disclosures for RFC 6330](https://datatracker.ietf.org/ipr/search/?rfc=6330&submit=rfc)).
The Apache-2.0 license grants patent rights only from this project's
contributors; it cannot and does not grant rights to any third-party
patents. Evaluate your own use accordingly — this notice is information,
not legal advice.
