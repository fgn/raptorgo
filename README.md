# raptorgo

[![Go Reference](https://pkg.go.dev/badge/github.com/fgn/raptorgo.svg)](https://pkg.go.dev/github.com/fgn/raptorgo)
[![CI](https://github.com/fgn/raptorgo/actions/workflows/ci.yml/badge.svg)](https://github.com/fgn/raptorgo/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

`raptorgo` is a bounded, pure-Go implementation of the RaptorQ forward error
correction scheme in [RFC 6330](https://www.rfc-editor.org/rfc/rfc6330.html).
The runtime package has no CGo or third-party dependencies.

RaptorQ solves one problem well: delivering an object over a lossy one-way
channel without retransmission. The sender turns the object into a stream of
equal-sized encoding packets; the receiver reconstructs the object from
(almost exactly) any subset whose size matches the original — regardless of
which packets were lost. Reception overhead is typically zero to two extra
packets per block.

## When to use it

Reach for RaptorQ instead of fixed-rate erasure codes such as Reed–Solomon
shards when:

- **The loss rate is unknown or varies.** Reed–Solomon fixes n and k up
  front; RaptorQ generates fresh repair packets on demand (a 24-bit symbol ID
  space per block), so the sender adapts redundancy live or cycles a data
  carousel indefinitely.
- **Blocks are large.** GF(256) Reed–Solomon tops out at 256 shards; a
  RaptorQ source block spans up to 56,403 symbols, and an object up to 255
  blocks.
- **Receivers join independently.** Multicast or broadcast receivers each
  lose different packets; any of them completes once it has collected
  roughly K packets of each block.

Prefer plain retransmission (TCP/QUIC) when you have a low-latency return
channel, and fixed Reed–Solomon shards when the shard count is small and
static (for example, striping across disks).

## Install

```sh
go get github.com/fgn/raptorgo
```

Requires Go 1.26 or newer. Pure Go on every platform; CI runs amd64, 386,
race, and checkptr gates.

## Quickstart

A complete transfer: the 12-byte OTI (the codec parameters) and a SHA-256
digest travel over an authenticated out-of-band channel, encoding packets
travel over the lossy channel, and the receiver delivers the object only
after the digest verifies.

```go
// Sender: derive parameters, build the encoder, publish OTI + digest.
limits := raptorgo.DefaultLimits()
oti, err := raptorgo.DeriveOTI(raptorgo.DerivationInput{
    TransferLength:             uint64(len(object)),
    DecoderBlockBytes:          1 << 20, // largest block decoded in memory
    MaxPayloadSize:             1280,    // symbol size; fit your path MTU
    SymbolAlignment:            4,
    MinSubSymbolAlignmentUnits: 8,
}, limits)
encoder, err := raptorgo.NewObjectEncoder(object, oti, limits)
otiWire, err := oti.MarshalBinary()
digest := sha256.Sum256(object)

// Receiver: parse the OTI against local limits; verify before delivery.
receivedOTI, err := raptorgo.ParseOTI(otiWire, limits)
decoder, err := raptorgo.NewObjectDecoder(receivedOTI, limits, "transfer-42",
    func(request raptorgo.VerificationRequest) error {
        hash := sha256.New()
        if _, err := io.Copy(hash, request.Object); err != nil {
            return err
        }
        if !bytes.Equal(hash.Sum(nil), digest[:]) {
            return errors.New("digest mismatch")
        }
        return nil
    },
    func(verified raptorgo.VerifiedObject) error {
        result = verified.Bytes // owned copy, delivered exactly once
        return nil
    },
)

// Transfer: source symbols are ESI 0..K-1; repair symbols start at ESI K.
k, err := raptorgo.SourceBlockSymbols(receivedOTI, 0)
for esi := uint32(0); esi < k; esi++ {
    packet, err := encoder.Packet(0, esi, 1, true)
    // ... transmit; lost packets are simply never added ...
    _, err = decoder.AddPacket(packet)
}
for esi := k; decoder.State() == raptorgo.ObjectReceiving; esi++ {
    packet, err := encoder.Packet(0, esi, 1, true) // repair symbol
    _, err = decoder.AddPacket(packet)
}
// decoder.State() == raptorgo.ObjectDelivered
```

Runnable versions live in the
[package examples](https://pkg.go.dev/github.com/fgn/raptorgo#pkg-examples)
and in [`examples/roundtrip`](examples/roundtrip/main.go), a standalone
program that simulates a lossy channel end to end:

```sh
go run ./examples/roundtrip -size 1048576 -mtu 1280 -loss 10
```

[`docs/transport.md`](docs/transport.md) covers integration with a real
transport: framing, ESI ranges, repair emission strategies, and resource
limits.

Callers may also supply an OTI chosen by their content-delivery protocol;
`DeriveOTI` implements the RFC's recommended example algorithm rather than a
mandatory transport policy. `DefaultLimits` is conservative application
policy, while `RFCWireLimits` admits the full wire domain for parsing and
conformance work.

## Trust boundary

FEC repairs erasures; it does not authenticate hostile packets or
reconstructed bytes. The public decoder deliberately requires both a verifier
and a deliverer and enforces `decoded -> verified -> delivered`: FEC success
alone never triggers delivery. Authenticate the OTI and the object digest out
of band, protect packet integrity in transit when the network is hostile, and
treat the verifier as the last line of defense. All decoder resources are
bounded by explicit `Limits` checked before allocation, so a forged OTI is
refused before it can consume memory. See [SECURITY.md](SECURITY.md) for the
threat model.

## Performance and practical limits

Steady-state repair generation runs at roughly 160–180 MB/s of symbol
payload per core at T=1280 independent of block size, the one-time repair
plan per block builds at 13–21 MB/s of object bytes, and end-to-end decoding
with ten percent loss runs at 12–20 MB/s (AMD Ryzen 5 3600, go1.26.5,
single-threaded, scalar GF(256)). Measured tables, the exact reproduction
command, and block-size guidance are in
[`docs/performance.md`](docs/performance.md).

Practical limits: the API is in-memory — object-sized buffers on both ends,
preflighted against `Limits` — not a streaming file API; one source block
spans at most 56,403 symbols; transfers are capped at 942,574,504,275 bytes
(RFC erratum 5548 applied). Systematic packets are emitted directly from the
immutable object in O(packet bytes); `PacketInto` accepts a reusable caller
buffer for a zero-allocation source-packet path.

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
   K'=10 fixed-pool recovery profile, explicitly not an all-row proof;
8. bounded TLA+ models plus production-derived positive traces and forged or
   deleted negative traces; and
9. a pinned mutation-testing gate over critical resource, wire, GF(256), and
   partition kernels, plus broader changed-line and scheduled audits.

The current public-object-decoder evidence set contains 24 harvested traces,
140 classified lifecycle events, and all 16 actions in the closed manifest.
All 24 traces pass TLC replay and all 284 applicable deterministic trace
mutants are rejected by the exact `TraceComplete` property. Two bounded model graphs
contain 104 and 15 edges; 35 generated schedules retain every one of those
edges, and three separate constructor schedules bring the production witness
set to 38. All 38 pass the Java-free local oracle and trace replay. The precise
claim is that every edge in those two exact bounded dumps has a selected,
realizable witness—not that every model behavior or production execution has
been explored.

RFC Editor erratum 5548 is applied: the maximum transfer length is
`942574504275`.

That evidence has limits. The TLA+ models and trace replays establish
bounded safety properties of abstractions and of recorded executions,
with negative controls proving the checks are not vacuous; differential
vectors establish byte-identical behavior on the domains actually
compared; exhaustive tests cover exactly the finite domains they
enumerate (GF(256) algebra, parameter tables, ESI/ISI maps); and the
Section 5.8 recovery profile is a
preregistered, bounded statistical measurement, explicitly not a proof
over every RFC row and ESI. None of this amounts to a machine-checked
proof of functional correctness of the Go implementation, and no such
claim is made.

The lifecycle trace/model oracles are also not RFC 6330 byte-level oracles.
RFC source pinning, table extraction, algebraic checks, rank tests, and the
Rust/C++ comparisons own that boundary; trace validation owns ordering,
terminal-state, callback-binding, and model-to-production witness claims.

The compact evidence index is in [`docs/invariants.md`](docs/invariants.md).
Recovery statistics and their explicit limits are in
[`docs/recovery.md`](docs/recovery.md). TLA scope is documented in
the [`dev/formal` repository documentation](https://github.com/fgn/raptorgo/blob/main/dev/formal/README.md),
and the reproducible ObjectDecoder measurements, hashes, and exact failure
messages are in
[`dev/formal/trace-validation-log.md`](dev/formal/trace-validation-log.md).

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
All formal, oracle, mutation, generator, and specification inputs live behind
the nested `dev` module boundary, so normal imports and module downloads do not
include them or invoke Cargo, C++, Java, TLA+, Task, or network fetches.
See [CONTRIBUTING.md](CONTRIBUTING.md) for the change-evidence expectations
and [SECURITY.md](SECURITY.md) for the threat model and reporting channel.
Mutation scope and baseline policy are in
[`docs/mutation-testing.md`](docs/mutation-testing.md).

End-to-end validation is maintained in the companion repository
[`raptorgo-e2e`](https://github.com/fgn/raptorgo-e2e), which builds a
file-transfer tool and a lossy-network test suite exclusively on this
module's public API.

## Attribution and research status

RaptorQ is not our invention. Raptor codes are the work of Amin
Shokrollahi, building on Michael Luby's LT fountain codes; RaptorQ was
developed at Digital Fountain and Qualcomm and standardized by the IETF as
[RFC 6330](https://www.rfc-editor.org/rfc/rfc6330.html), authored by
M. Luby, A. Shokrollahi, M. Watson, T. Stockhammer, and L. Minder. This
repository is an independent implementation of that specification and
claims no rights in the RaptorQ code, the RaptorQ name, or the underlying
technology. Any defects in this implementation are this project's own.

The project is a research effort in applying formal-methods and
differential-verification techniques to a protocol implementation. It
should not be read as "formally verified." The TLA+ artifacts
model-check bounded abstractions of selected subsystems and replay
production-generated traces against those models; they check finite
safety properties of models and recorded traces, not the compiled Go
code, and establish no liveness of the real implementation. The exact
claim-to-evidence map is in [`docs/invariants.md`](docs/invariants.md),
the formal scope and its limits in the
[`dev/formal` repository documentation](https://github.com/fgn/raptorgo/blob/main/dev/formal/README.md),
and the bounded recovery statistics in
[`docs/recovery.md`](docs/recovery.md).

## License

Copyright 2026 FGN Research and Development B.V. Licensed under the
[Apache License 2.0](LICENSE). Third-party material distributed with this
repository is listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

**Patent notice.** RaptorQ is specified by RFC 6330, for which intellectual
property rights disclosures have been filed with the IETF (see the
[IETF IPR disclosures for RFC 6330](https://datatracker.ietf.org/ipr/search/?rfc=6330&submit=rfc)).
The Apache-2.0 license grants patent rights only from this project's
contributors; it cannot and does not grant rights to any third-party
patents. Evaluate your own use accordingly; this notice is information,
not legal advice.
