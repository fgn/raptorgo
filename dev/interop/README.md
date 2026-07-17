# Interoperability tools

Everything below this directory is development tooling. It is not imported by
the Go module and is not part of production builds.

The Rust oracle is pinned by `Cargo.lock` to `raptorq` 2.0.1. `task
oracle:prepare` downloads the immutable crate archive into an external cache,
verifies its package SHA-256 and complete file manifest, and applies the
checked-in primitive-export patch. No upstream Rust source or build artifact is
kept in this repository. The oracle supports default-parameter encoding,
explicit OTI/SBN/ESI range encoding, frame decoding, and an
`extended-map` mode. Its `primitive-vectors` mode reproducibly compares every
valid K-to-K' and ESI-to-ISI boundary, every full Table 2 parameter row, seven
tuple IDs per row, 65,541 Rand cases, and all Deg inputs at four W values. Its
output format for encoding modes is:

```text
4 bytes   ASCII "RQO1"
12 bytes  serialized RFC 6330 OTI
4 bytes   big-endian packet count
repeated:
  4 bytes big-endian serialized packet length
  N bytes 4-byte FEC Payload ID followed by symbol bytes
```

This framing is local test infrastructure, not a RaptorQ wire format.

Parity commands use optimized Rust builds. In `raptorq` 2.0.1, debug overflow
checks can panic for valid `Rand` inputs where `y+i` crosses `uint32`; optimized
wrapping arithmetic implements the RFC's modulo-256 index calculation.

The decoder adapter independently validates grouped source/repair boundaries,
the 24-bit ESI range, distributed source padding for `N > 1`, and legal source
shortening. It normalizes accepted records into consecutive full symbols before
passing them to the Rust decoder. The cross gate compares byte-identical Go and
Rust frames and decodes both directions for full, grouped, shortened,
multi-block, and sub-block-partitioned cases. The combined Z=2, N=4 case drops
ESI zero from every block, shortens the retained padded source symbol, and uses
grouped repairs to recover both blocks.

The pinned C++ RAW adapter covers the independent libRaptorQ consensus domain
where its requested K is already an RFC Table 2 K'. It emits and consumes the
same deterministic RQO1 corpus for K=10, 12, and 48, checking repair-only sets,
mixed source/repair sets with loss, reverse order and a duplicate, and matching
rejection of a K-1 deficient set. K=49 remains an explicit quarantined repair
divergence in that historical implementation.
