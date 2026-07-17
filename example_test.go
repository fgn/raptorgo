package raptorgo_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/fgn/raptorgo"
)

func Example() {
	object := []byte("hello, RaptorQ")
	limits := raptorgo.DefaultLimits()
	oti, err := raptorgo.DeriveOTI(raptorgo.DerivationInput{
		TransferLength: uint64(len(object)), DecoderBlockBytes: 1 << 20,
		MaxPayloadSize: 8, SymbolAlignment: 1, MinSubSymbolAlignmentUnits: 1,
	}, limits)
	if err != nil {
		panic(err)
	}
	encoder, err := raptorgo.NewObjectEncoder(object, oti, limits)
	if err != nil {
		panic(err)
	}

	decoder, err := raptorgo.NewObjectDecoder(
		oti, limits, "generation-7",
		func(request raptorgo.VerificationRequest) error {
			candidate, readErr := io.ReadAll(request.Object)
			if readErr != nil {
				return readErr
			}
			if request.Context != "generation-7" || !bytes.Equal(candidate, object) {
				return fmt.Errorf("object authentication failed")
			}
			return nil
		},
		func(verified raptorgo.VerifiedObject) error {
			fmt.Println(string(verified.Bytes))
			return nil
		},
	)
	if err != nil {
		panic(err)
	}
	const shortenFinal = true
	for sourceBlockIndex := 0; sourceBlockIndex < int(oti.SourceBlocks); sourceBlockIndex++ {
		sourceBlock := uint8(sourceBlockIndex)
		k, symbolsErr := raptorgo.SourceBlockSymbols(oti, sourceBlock)
		if symbolsErr != nil {
			panic(symbolsErr)
		}
		packet, packetErr := encoder.Packet(sourceBlock, 0, k, shortenFinal)
		if packetErr != nil {
			panic(packetErr)
		}
		if _, addErr := decoder.AddPacket(packet); addErr != nil {
			panic(addErr)
		}
	}

	// Output:
	// hello, RaptorQ
}

func ExampleObjectEncoder_PacketInto() {
	object := []byte("abcdefgh")
	oti := raptorgo.OTI{
		TransferLength: 8, SymbolSize: 4,
		SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1,
	}
	encoder, err := raptorgo.NewObjectEncoder(object, oti, raptorgo.DefaultLimits())
	if err != nil {
		panic(err)
	}

	// Reuse this buffer for successive source packets. PacketInto returns the
	// exact packet-length prefix and performs no heap allocation on this path.
	buffer := make([]byte, raptorgo.PayloadIDEncodedSize+int(oti.SymbolSize))
	packet, err := encoder.PacketInto(buffer, 0, 1, 1, false)
	if err != nil {
		panic(err)
	}
	view, err := raptorgo.ParseEncodingPacket(packet, oti, raptorgo.DefaultLimits())
	if err != nil {
		panic(err)
	}
	fmt.Printf("payload=%q aliases-buffer=%v\n", view.Payload(), &packet[0] == &buffer[0])

	// Output:
	// payload="efgh" aliases-buffer=true
}

// Example_authenticatedTransfer is the recommended receiver pattern. The
// sender transmits the 12-byte OTI and a SHA-256 digest over an authenticated
// out-of-band channel, the network loses one source packet in five, repair
// packets (ESI >= K) stand in for the losses, and the receiver delivers the
// object only after the digest verifies. FEC repairs erasures; it never
// authenticates bytes, so the verifier compares against the out-of-band
// digest rather than trusting FEC success.
func Example_authenticatedTransfer() {
	// Sender: derive an OTI, build the encoder, and publish the OTI and
	// object digest through an authenticated out-of-band channel.
	object := bytes.Repeat([]byte("raptorgo"), 512)
	limits := raptorgo.DefaultLimits()
	oti, err := raptorgo.DeriveOTI(raptorgo.DerivationInput{
		TransferLength:             uint64(len(object)),
		DecoderBlockBytes:          1 << 20,
		MaxPayloadSize:             64,
		SymbolAlignment:            4,
		MinSubSymbolAlignmentUnits: 16,
	}, limits)
	if err != nil {
		panic(err)
	}
	encoder, err := raptorgo.NewObjectEncoder(object, oti, limits)
	if err != nil {
		panic(err)
	}
	otiWire, err := oti.MarshalBinary()
	if err != nil {
		panic(err)
	}
	digest := sha256.Sum256(object)

	// Receiver: parse the announced OTI against local limits and refuse
	// delivery unless the reconstructed object matches the announced digest.
	receivedOTI, err := raptorgo.ParseOTI(otiWire, limits)
	if err != nil {
		panic(err)
	}
	decoder, err := raptorgo.NewObjectDecoder(
		receivedOTI, limits, "transfer-42",
		func(request raptorgo.VerificationRequest) error {
			hash := sha256.New()
			if _, copyErr := io.Copy(hash, request.Object); copyErr != nil {
				return copyErr
			}
			if !bytes.Equal(hash.Sum(nil), digest[:]) {
				return errors.New("SHA-256 digest mismatch")
			}
			return nil
		},
		func(verified raptorgo.VerifiedObject) error {
			fmt.Printf("delivered %d verified bytes\n", len(verified.Bytes))
			return nil
		},
	)
	if err != nil {
		panic(err)
	}

	// Network: every fifth source packet is lost in transit.
	k, err := raptorgo.SourceBlockSymbols(receivedOTI, 0)
	if err != nil {
		panic(err)
	}
	const shortenFinal = true
	lost := uint32(0)
	for esi := uint32(0); esi < k; esi++ {
		if esi%5 == 4 {
			lost++
			continue
		}
		packet, packetErr := encoder.Packet(0, esi, 1, shortenFinal)
		if packetErr != nil {
			panic(packetErr)
		}
		if _, addErr := decoder.AddPacket(packet); addErr != nil {
			panic(addErr)
		}
	}
	// Repair symbols begin at ESI K; emit them until the decoder finishes.
	repairs := uint32(0)
	for esi := k; decoder.State() == raptorgo.ObjectReceiving; esi++ {
		packet, packetErr := encoder.Packet(0, esi, 1, shortenFinal)
		if packetErr != nil {
			panic(packetErr)
		}
		if _, addErr := decoder.AddPacket(packet); addErr != nil {
			panic(addErr)
		}
		repairs++
	}
	fmt.Printf("state=%s lost=%d repairs=%d\n", decoder.State(), lost, repairs)

	// Output:
	// delivered 4096 verified bytes
	// state=delivered lost=12 repairs=12
}

// Example_lossRecovery loses one source packet in transit and recovers the
// object from a repair packet (ESI >= K) instead.
func Example_lossRecovery() {
	object := []byte("recovered by RaptorQ")
	limits := raptorgo.DefaultLimits()
	oti, err := raptorgo.DeriveOTI(raptorgo.DerivationInput{
		TransferLength: uint64(len(object)), DecoderBlockBytes: 1 << 20,
		MaxPayloadSize: 8, SymbolAlignment: 1, MinSubSymbolAlignmentUnits: 1,
	}, limits)
	if err != nil {
		panic(err)
	}
	encoder, err := raptorgo.NewObjectEncoder(object, oti, limits)
	if err != nil {
		panic(err)
	}

	decoder, err := raptorgo.NewObjectDecoder(
		oti, limits, "generation-8",
		func(request raptorgo.VerificationRequest) error {
			candidate, readErr := io.ReadAll(request.Object)
			if readErr != nil {
				return readErr
			}
			if request.Context != "generation-8" || !bytes.Equal(candidate, object) {
				return fmt.Errorf("object authentication failed")
			}
			return nil
		},
		func(verified raptorgo.VerifiedObject) error {
			fmt.Println(string(verified.Bytes))
			return nil
		},
	)
	if err != nil {
		panic(err)
	}

	// This small object fits one source block with K source symbols.
	k, err := raptorgo.SourceBlockSymbols(oti, 0)
	if err != nil {
		panic(err)
	}
	const shortenFinal = true
	// Deliver all source packets except the last one, which is lost.
	for esi := uint32(0); esi+1 < k; esi++ {
		packet, packetErr := encoder.Packet(0, esi, 1, shortenFinal)
		if packetErr != nil {
			panic(packetErr)
		}
		if _, addErr := decoder.AddPacket(packet); addErr != nil {
			panic(addErr)
		}
	}
	// Repair packets (ESI >= K) stand in for the lost source packet.
	for esi := k; decoder.State() == raptorgo.ObjectReceiving; esi++ {
		packet, packetErr := encoder.Packet(0, esi, 1, shortenFinal)
		if packetErr != nil {
			panic(packetErr)
		}
		if _, addErr := decoder.AddPacket(packet); addErr != nil {
			panic(addErr)
		}
	}
	fmt.Println(decoder.State())

	// Output:
	// recovered by RaptorQ
	// delivered
}
