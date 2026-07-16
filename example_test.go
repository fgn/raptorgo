package raptorgo_test

import (
	"bytes"
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
	for sourceBlock := uint8(0); sourceBlock < oti.SourceBlocks; sourceBlock++ {
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
