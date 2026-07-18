# raptorgo

[![Go Reference](https://pkg.go.dev/badge/github.com/fgn/raptorgo.svg)](https://pkg.go.dev/github.com/fgn/raptorgo)
[![CI](https://github.com/fgn/raptorgo/actions/workflows/ci.yml/badge.svg)](https://github.com/fgn/raptorgo/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

RaptorQ ([RFC 6330](https://www.rfc-editor.org/rfc/rfc6330.html)) forward
error correction in pure Go. No CGo, no dependencies.

Deliver an object over a lossy one-way channel without retransmission: the
sender emits equal-sized packets, and the receiver rebuilds the object from
roughly any K of them, no matter which were lost. Typical overhead is zero to
two extra packets per block.

Compared to fixed-rate Reed-Solomon shards, RaptorQ generates fresh repair
packets on demand (so redundancy adapts to unknown or varying loss), scales to
56,403 symbols per block and 255 blocks per object, and lets multicast
receivers with different losses each complete independently. If you have a
low-latency return channel, use TCP/QUIC instead; if your shard count is small
and static, plain Reed-Solomon is simpler.

## Install

```sh
go get github.com/fgn/raptorgo
```

Go 1.26 or newer. Pure Go on every platform.

## Quickstart

Publish the 12-byte OTI (codec parameters) and a digest over an authenticated
channel; send packets over the lossy one. The decoder delivers only after
your verifier accepts the reconstructed bytes.

```go
// Sender
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

// Receiver
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

Run a complete transfer over a simulated lossy channel:

```sh
go run ./examples/roundtrip -size 1048576 -mtu 1280 -loss 10
```

Next steps: the runnable
[package examples](https://pkg.go.dev/github.com/fgn/raptorgo#pkg-examples),
[`docs/transport.md`](docs/transport.md) for framing and repair strategies,
and `PacketInto` for allocation-free packet emission on hot paths.

## Performance

Single-threaded scalar Go decodes 12.8 MB with ten percent loss in about a
second (AMD Ryzen 5 3600); systematic packets cost about 80 ns each with a
reused buffer. The opt-in AVX2 build (`GOEXPERIMENT=simd`, amd64, runtime
scalar fallback) is over 4x faster end to end: 117.5 ms encode and 112.2 ms
decode for the same 12.8 MB workload on an i9-14900HX. Measured tables and
reproduction commands are in [`docs/performance.md`](docs/performance.md).

## Safety and limits

FEC repairs erasures; it authenticates nothing. The decoder therefore
requires a verifier and a deliverer and enforces decoded, then verified, then
delivered: FEC success alone never releases bytes. All decoder memory is
preflighted against explicit `Limits`, so a forged OTI is refused before it
can consume resources. The API is in-memory (not streaming); one block spans
at most 56,403 symbols; transfers cap at 942,574,504,275 bytes. Threat model:
[SECURITY.md](SECURITY.md).

## Correctness

The full RFC surface is implemented and tested through K=56,403: parameter
derivation and OTI handling, block and sub-block partitioning, grouped and
shortened packets, systematic and repair encoding, and multi-block recovery
under loss, reordering, and duplicates. Evidence comes from independent
layers: exhaustive algebra and table tests against the pinned RFC,
byte-identical Rust and C++ cross-decoding, dense algebraic oracles against
the production solver, bounded TLA+ models with production trace replay, and
pinned mutation-testing gates. This is strong layered evidence, not a
machine-checked proof of the Go code, and no such claim is made. The exact
claim-to-evidence map is in [`docs/invariants.md`](docs/invariants.md),
recovery statistics in [`docs/recovery.md`](docs/recovery.md), and formal
scope in the [`dev/formal` documentation](https://github.com/fgn/raptorgo/blob/main/dev/formal/README.md).

## Development

`task ci` runs the local correctness suite; see the Taskfile for the full
gate list. Rust, C++, and TLA+ are development-time oracles behind the nested
`dev` module and never touch production builds or module downloads.
Contribution and evidence expectations are in
[CONTRIBUTING.md](CONTRIBUTING.md); mutation policy in
[`docs/mutation-testing.md`](docs/mutation-testing.md). End-to-end validation
lives in the companion repository
[`raptorgo-e2e`](https://github.com/fgn/raptorgo-e2e).

## Attribution and license

RaptorQ is the work of Amin Shokrollahi, Michael Luby, and their
collaborators, standardized by the IETF as RFC 6330. This repository is an
independent implementation and claims no rights in the RaptorQ name or
technology; any defects are this project's own.

Copyright 2026 FGN Research and Development B.V. Licensed under the
[Apache License 2.0](LICENSE); third-party material is listed in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

**Patent notice.** IETF IPR disclosures exist for RFC 6330 (see the
[disclosure search](https://datatracker.ietf.org/ipr/search/?rfc=6330&submit=rfc)).
Apache-2.0 grants patent rights only from this project's contributors and
cannot grant rights to third-party patents. Evaluate your own use; this is
information, not legal advice.
