# raptorgo

`raptorgo` is a bounded, pure-Go implementation of the RaptorQ forward error
correction scheme in [RFC 6330](https://www.rfc-editor.org/rfc/rfc6330.html).
The runtime package has no CGo or third-party dependencies.

```sh
go get github.com/fgn/raptorgo
```

Implemented behavior includes:

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

## Correctness evidence

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
