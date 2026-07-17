// Command roundtrip demonstrates a complete raptorgo transfer over a
// simulated lossy channel. The sender derives an OTI and publishes it out of
// band together with a SHA-256 digest, the channel drops packets at a
// configured rate, repair packets top up the losses, and the receiver
// delivers the object only after the digest verifies.
//
// Run it with:
//
//	go run ./examples/roundtrip -size 1048576 -mtu 1280 -loss 10
package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand/v2"

	"github.com/fgn/raptorgo"
)

func main() {
	size := flag.Uint64("size", 1<<20, "object size in bytes")
	mtu := flag.Uint("mtu", 1280, "max payload bytes per packet (multiple of 4)")
	loss := flag.Float64("loss", 10, "packet loss percentage, 0 to 95")
	blockBytes := flag.Uint64("ws", 1<<20, "largest source block the receiver decodes in memory")
	seed := flag.Uint64("seed", 1, "seed for the object and the loss pattern")
	flag.Parse()
	if *loss < 0 || *loss > 95 {
		log.Fatal("-loss must be between 0 and 95")
	}
	if *mtu == 0 || *mtu > 65535 {
		log.Fatal("-mtu must be between 1 and 65535")
	}

	rng := rand.New(rand.NewPCG(*seed, *seed))
	object := make([]byte, *size)
	for index := range object {
		object[index] = byte(rng.Uint32())
	}

	// Sender: derive transmission parameters and build the encoder. The OTI
	// and the digest travel over an authenticated out-of-band channel; FEC
	// repairs erasures but never authenticates bytes.
	limits := raptorgo.DefaultLimits()
	oti, err := raptorgo.DeriveOTI(raptorgo.DerivationInput{
		TransferLength:             uint64(len(object)),
		DecoderBlockBytes:          *blockBytes,
		MaxPayloadSize:             uint16(*mtu),
		SymbolAlignment:            4,
		MinSubSymbolAlignmentUnits: 8,
	}, limits)
	if err != nil {
		log.Fatal(err)
	}
	encoder, err := raptorgo.NewObjectEncoder(object, oti, limits)
	if err != nil {
		log.Fatal(err)
	}
	otiWire, err := oti.MarshalBinary()
	if err != nil {
		log.Fatal(err)
	}
	digest := sha256.Sum256(object)

	// Receiver: parse the announced OTI against local limits and accept the
	// reconstructed object only if it matches the announced digest.
	receivedOTI, err := raptorgo.ParseOTI(otiWire, limits)
	if err != nil {
		log.Fatal(err)
	}
	deliveredBytes := 0
	decoder, err := raptorgo.NewObjectDecoder(
		receivedOTI, limits, "roundtrip",
		func(request raptorgo.VerificationRequest) error {
			hash := sha256.New()
			if _, copyErr := io.Copy(hash, request.Object); copyErr != nil {
				return fmt.Errorf("read reconstructed object: %w", copyErr)
			}
			if !bytes.Equal(hash.Sum(nil), digest[:]) {
				return errors.New("SHA-256 digest mismatch")
			}
			return nil
		},
		func(verified raptorgo.VerifiedObject) error {
			deliveredBytes = len(verified.Bytes)
			return nil
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("object: %d bytes\n", len(object))
	fmt.Printf("oti: T=%d Z=%d N=%d Al=%d\n",
		receivedOTI.SymbolSize, receivedOTI.SourceBlocks, receivedOTI.SubBlocks, receivedOTI.SymbolAlignment)
	fmt.Printf("channel: %.1f%% loss, seed %d\n", *loss, *seed)

	// Channel: send each block's source symbols, then emit repair symbols
	// (ESI >= K) until the block decodes. Losses hit both packet kinds.
	lose := func() bool { return rng.Float64()*100 < *loss }
	var sourceSent, sourceLost, repairSent, repairLost, totalSymbols uint64
	const shortenFinal = true
	for sbn := uint8(0); sbn < receivedOTI.SourceBlocks; sbn++ {
		k, symbolsErr := raptorgo.SourceBlockSymbols(receivedOTI, sbn)
		if symbolsErr != nil {
			log.Fatal(symbolsErr)
		}
		if k == 0 {
			continue
		}
		completedBefore := decoder.CompletedBlocks()
		totalSymbols += uint64(k)
		send := func(esi uint32, lostCounter *uint64) {
			packet, packetErr := encoder.Packet(sbn, esi, 1, shortenFinal)
			if packetErr != nil {
				log.Fatal(packetErr)
			}
			if lose() {
				(*lostCounter)++
				return
			}
			if _, addErr := decoder.AddPacket(packet); addErr != nil {
				log.Fatal(addErr)
			}
		}
		for esi := uint32(0); esi < k; esi++ {
			sourceSent++
			send(esi, &sourceLost)
		}
		for esi := k; decoder.CompletedBlocks() == completedBefore &&
			decoder.State() == raptorgo.ObjectReceiving; esi++ {
			repairSent++
			send(esi, &repairLost)
		}
	}

	received := (sourceSent - sourceLost) + (repairSent - repairLost)
	fmt.Printf("source: sent %d, lost %d\n", sourceSent, sourceLost)
	fmt.Printf("repair: sent %d, lost %d\n", repairSent, repairLost)
	if totalSymbols == 0 {
		fmt.Println("reception overhead: 0 symbols (empty object)")
	} else {
		fmt.Printf("reception overhead: %d symbols received for %d source symbols (%+.2f%%)\n",
			received, totalSymbols, 100*float64(int64(received)-int64(totalSymbols))/float64(totalSymbols))
	}
	if state := decoder.State(); state != raptorgo.ObjectDelivered {
		log.Fatalf("transfer failed in state %s", state)
	}
	fmt.Printf("delivered: %d bytes after digest verification\n", deliveredBytes)
}
