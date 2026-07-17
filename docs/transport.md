# Transport integration

How to put raptorgo behind a real transport. The package implements RFC 6330
object encoding and decoding; it deliberately does not implement session
setup, packet framing, acknowledgement, or authentication. This guide states
what the transport must supply.

## Scope: an in-memory object API

`ObjectEncoder` owns a complete in-memory copy of the source object, and
`ObjectDecoder` reconstructs the complete object in memory before
verification. There is no streaming or file-backed mode. Size transfers so
that the object, its block decoders, and one delivery copy fit the
`Limits.MaxWorkingMemory` budget; `ObjectEncoderWorkspaceBackingBytes` and
`ObjectDecoderWorkspaceBackingBytes` report the preflighted peaks for a given
OTI without allocating. Larger payloads should be split into multiple objects
by the application.

## What travels where

Two different channels carry a transfer:

- **Out of band, authenticated:** the 12-byte OTI (`OTI.MarshalBinary`) and
  whatever the receiver will verify the object against — normally a SHA-256
  digest or a signature. The OTI dictates decoder memory consumption, so a
  forged OTI is a resource-consumption attack; parse it with `ParseOTI`
  against local `Limits`, and never accept it unauthenticated.
- **In band, lossy:** encoding packets from `ObjectEncoder.Packet`. Each
  packet is the 4-byte FEC Payload ID followed by one or more symbols. FEC
  corrects erasures only: a corrupted packet that still parses will poison
  decoding undetectably, so packets need integrity protection (a transport
  checksum at minimum, a MAC when the network is hostile) and the object
  digest is the final backstop.

The decoder requires both a verifier and a deliverer for exactly this reason:
`decoded -> verified -> delivered` is enforced, and FEC success alone never
triggers delivery.

## Packet framing

Encoding packets are not self-delimiting. The receiver must know each
packet's byte length before calling `AddPacket`:

- **Datagram transports (UDP, QUIC datagrams):** send one encoding packet per
  datagram; the datagram length delimits the packet. Choose
  `DerivationInput.MaxPayloadSize` so the 4-byte payload ID plus one symbol
  fits the path MTU.
- **Stream transports (TCP, QUIC streams):** add explicit length framing,
  for example a 2-byte length prefix. A stream transport already retransmits,
  so FEC there usually only makes sense as application-layer redundancy
  across streams or sessions.

Symbol size is fixed per transfer (`OTI.SymbolSize`, one exception below), so
`payload length / SymbolSize` yields the symbol count of a grouped packet.

## Source and repair ESI ranges

Every packet names a source block (SBN) and its first encoding symbol ID
(ESI). For a block with K source symbols — obtained per block from
`SourceBlockSymbols(oti, sbn)`, since blocks differ by one symbol —

- ESI `0 .. K-1` are the source symbols: the object bytes themselves;
- ESI `K ..` are repair symbols: any of them can stand in for any lost
  source symbol of the same block.

The systematic property means a loss-free transfer costs no decoding work:
send ESI `0 .. K-1` and the decoder copies the bytes through. Emit repair
ESIs starting at K, never reusing an ESI within a transfer. The wire field is
24 bits (`MaxEncodingSymbolID`), which allows over 16 million repair symbols
per block.

## Grouping and shortening

`Packet(sbn, firstESI, count, shortenFinal)` can group several consecutive
symbols into one packet. A group must lie entirely in the source range or
entirely in the repair range; crossing the boundary is rejected. Grouping
amortizes the 4-byte header over more payload but loses more data per
dropped packet; single-symbol packets are the common choice on lossy paths.

With `shortenFinal` true, the packet's last symbol omits its trailing FEC
zero padding on the wire when RFC 6330 permits it. Only source symbols of
the final nonempty block carry such padding: with one sub-block that is just
the final source symbol, while sub-blocking (`N > 1`) redistributes the
padding suffix so several trailing symbols may be shortened.
`ParseEncodingPacket` accepts exactly the legal shortened forms and restores
the canonical padded symbols; illegal shortening or nonzero padding bytes
are rejected.

## Emitting repairs until acknowledgement

The encoder is stateless per request and caches the repair plan for one block
at a time, so iterate block by block rather than interleaving repair requests
across blocks. Two proven patterns:

- **Feedback loop:** send all source symbols, then repair symbols at the
  estimated loss rate plus margin, until the receiver acknowledges delivery
  or reports how many symbols each block still needs (K minus received
  counts).
- **Data carousel (no return channel):** cycle source symbols and fresh
  repair ESIs continuously; receivers join at any point and complete once
  they have collected roughly K symbols per block, regardless of which ones.

RaptorQ needs barely any overhead: the decoder usually completes with exactly
K received symbols per block, and one or two extras make failure negligible
(RFC 6330 Section 1). The `examples/roundtrip` program measures this
end-to-end.

## Decoder limits and failure states

All decoder resources are bounded up front by `Limits`:

- `MaxRetainedSymbols` and `MaxRepairSymbolsPerBlock` cap how many symbols a
  block retains; symbols arriving beyond the cap are rejected rather than
  buffered.
- `MaxDecodeAttempts` caps bounded decode attempts per block. Each attempt
  consumes budget, so on very lossy links prefer batching arrivals over
  calling `AddPacket` per symbol once K symbols are near.
- `MaxWorkingMemory` bounds the preflighted peak of construction, decoding,
  reconstruction, and delivery. `NewObjectDecoder` fails immediately if the
  OTI cannot fit, which is why the OTI must be validated against local limits
  before resources are committed.

`AddPacket` returns the decoder state. `ObjectReceiving` accepts more
packets; every other state is terminal or transient-final:
`ObjectDecodeFailed`, `ObjectVerificationFailed`, and `ObjectDeliveryFailed`
end the transfer, and a terminal decoder rejects further packets with
`ErrObjectTerminal`. Duplicate symbols with identical content are tolerated;
a duplicate ESI carrying different content is rejected with an error while
the decoder keeps receiving. All errors wrap the package's sentinel errors
and match with `errors.Is`.

## Checklist

- [ ] OTI transported out of band and authenticated before `ParseOTI`.
- [ ] Object digest or signature transported out of band; verifier compares
      against it and delivery happens only through the deliverer callback.
- [ ] One packet per datagram, or explicit length framing on streams.
- [ ] `MaxPayloadSize` fits the path MTU minus transport headers, and is a
      multiple of `SymbolAlignment`.
- [ ] Repair ESIs allocated from K upward per block, never reused.
- [ ] `Limits` sized to the deployment's memory budget; workspace
      estimators consulted for capacity planning.
- [ ] Terminal decoder states surfaced to the session layer so senders stop
      transmitting.
