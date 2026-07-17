package raptorgo

import (
	"fmt"
	"io"
	"testing"
)

func BenchmarkObjectEncoderPacket(b *testing.B) {
	for _, test := range []struct {
		k, symbolSize uint32
	}{
		// Conformance strata: small symbols across parameter-table seams.
		{10, 64},
		{49, 31},
		{1_025, 7},
		// Transport strata: a network-realistic 1280-byte symbol across
		// representative block sizes.
		{10, 1_280},
		{100, 1_280},
		{1_000, 1_280},
		{10_000, 1_280},
	} {
		name := fmt.Sprintf("K=%d/T=%d", test.k, test.symbolSize)
		oti := benchmarkOTI(test.k, test.symbolSize)
		object := benchmarkObject(oti.TransferLength)
		encoder, err := NewObjectEncoder(object, oti, DefaultLimits())
		if err != nil {
			b.Fatal(err)
		}
		if _, err := encoder.Packet(0, test.k, 1, false); err != nil {
			b.Fatal(err)
		}
		b.Run("systematic/"+name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(test.symbolSize))
			for b.Loop() {
				if _, err := encoder.Packet(0, test.k/2, 1, false); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("repair-cached/"+name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(test.symbolSize))
			for b.Loop() {
				if _, err := encoder.Packet(0, test.k+17, 1, false); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("construct-and-repair/"+name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(oti.TransferLength))
			for b.Loop() {
				fresh, err := NewObjectEncoder(object, oti, DefaultLimits())
				if err != nil {
					b.Fatal(err)
				}
				if _, err := fresh.Packet(0, test.k, 1, false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkObjectEncoderSystematicLayouts(b *testing.B) {
	const (
		k          = uint32(10_000)
		symbolSize = uint32(1_280)
	)
	singleBlock := benchmarkOTI(k, symbolSize)
	subBlocked := singleBlock
	subBlocked.SubBlocks = 4
	subBlocked.SymbolAlignment = 4
	shortened := subBlocked
	shortened.TransferLength -= 13
	multiBlock := subBlocked
	multiBlock.TransferLength *= 2
	multiBlock.SourceBlocks = 2

	for _, test := range []struct {
		name                      string
		oti                       OTI
		sourceBlock               uint8
		firstESI, count           uint32
		shorten, requireShortened bool
	}{
		{name: "single/N=1", oti: singleBlock, firstESI: k / 2, count: 1},
		{name: "grouped=16/N=1", oti: singleBlock, firstESI: k / 2, count: 16},
		{name: "single/N=4", oti: subBlocked, firstESI: k / 2, count: 1},
		{name: "grouped=16/N=4", oti: subBlocked, firstESI: k / 2, count: 16},
		{name: "shortened-final/N=4", oti: shortened, firstESI: k - 1, count: 1, shorten: true, requireShortened: true},
		{name: "single/Z=2/N=4/SBN=1", oti: multiBlock, sourceBlock: 1, firstESI: k / 2, count: 1},
	} {
		test := test
		b.Run(test.name, func(b *testing.B) {
			object := benchmarkObject(test.oti.TransferLength)
			encoder, err := NewObjectEncoder(object, test.oti, DefaultLimits())
			if err != nil {
				b.Fatal(err)
			}
			packet, err := encoder.Packet(test.sourceBlock, test.firstESI, test.count, test.shorten)
			if err != nil {
				b.Fatal(err)
			}
			fullBytes := PayloadIDEncodedSize + int(test.count)*int(test.oti.SymbolSize)
			if test.requireShortened && len(packet) >= fullBytes {
				b.Fatalf("shortened packet has %d bytes, full packet has %d", len(packet), fullBytes)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(packet) - PayloadIDEncodedSize))
			b.ReportMetric(float64(test.count), "symbols/op")
			for b.Loop() {
				if _, err := encoder.Packet(test.sourceBlock, test.firstESI, test.count, test.shorten); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkObjectDecoder(b *testing.B) {
	for _, test := range []struct {
		k, symbolSize, loss, overhead uint32
	}{
		// Conformance strata: small symbols across parameter-table seams.
		{10, 64, 0, 0},
		{49, 31, 5, 4},
		{1_025, 7, 7, 8},
		// Transport strata: 1280-byte symbols with ten percent source loss
		// repaired from the repair ESI range.
		{10, 1_280, 1, 0},
		{100, 1_280, 10, 1},
		{1_000, 1_280, 100, 2},
		{10_000, 1_280, 1_000, 4},
	} {
		name := fmt.Sprintf("K=%d/T=%d/loss=%d/overhead=%d", test.k, test.symbolSize, test.loss, test.overhead)
		oti := benchmarkOTI(test.k, test.symbolSize)
		object := benchmarkObject(oti.TransferLength)
		encoder, err := NewObjectEncoder(object, oti, DefaultLimits())
		if err != nil {
			b.Fatal(err)
		}
		packets := make([][]byte, 0, test.k+test.overhead+1)
		for esi := test.k; esi > test.loss; esi-- {
			packet, packetErr := encoder.Packet(0, esi-1, 1, false)
			if packetErr != nil {
				b.Fatal(packetErr)
			}
			packets = append(packets, packet)
		}
		for offset := test.loss + test.overhead; offset > 0; offset-- {
			packet, packetErr := encoder.Packet(0, test.k+offset-1, 1, false)
			if packetErr != nil {
				b.Fatal(packetErr)
			}
			packets = append(packets, packet)
		}
		if len(packets) > 0 {
			packets = append(packets, packets[0]) // Explicit duplicate after reverse order.
		}
		decode := func() error {
			decoder, decoderErr := NewObjectDecoder(
				oti, DefaultLimits(), "benchmark",
				func(request VerificationRequest) error {
					_, copyErr := io.Copy(io.Discard, request.Object)
					return copyErr
				},
				func(delivered VerifiedObject) error {
					if uint64(len(delivered.Bytes)) != oti.TransferLength {
						return fmt.Errorf("delivered %d bytes", len(delivered.Bytes))
					}
					return nil
				},
			)
			if decoderErr != nil {
				return decoderErr
			}
			for _, packet := range packets {
				state, addErr := decoder.AddPacket(packet)
				if addErr != nil {
					return addErr
				}
				if state == ObjectDelivered {
					return nil
				}
			}
			return fmt.Errorf("decoder ended in %s", decoder.State())
		}
		if err := decode(); err != nil {
			b.Fatalf("benchmark corpus is not decodable: %v", err)
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(oti.TransferLength))
			for b.Loop() {
				if err := decode(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchmarkOTI(k, symbolSize uint32) OTI {
	return OTI{
		TransferLength: uint64(k) * uint64(symbolSize), SymbolSize: uint16(symbolSize),
		SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1,
	}
}

func benchmarkObject(length uint64) []byte {
	object := make([]byte, int(length))
	for index := range object {
		object[index] = byte(uint64(index)*131 + 19)
	}
	return object
}
