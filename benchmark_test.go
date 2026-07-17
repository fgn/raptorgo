package raptorgo

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

var benchmarkPacketSink []byte

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
		b.Run("systematic/"+name, func(b *testing.B) {
			encoder, err := NewObjectEncoder(object, oti, DefaultLimits())
			if err != nil {
				b.Fatal(err)
			}
			firstESI := test.k / 2
			packet, err := encoder.Packet(0, firstESI, 1, false)
			if err != nil {
				b.Fatal(err)
			}
			validateBenchmarkSourcePacket(b, packet, object, oti, 0, firstESI, 1)
			b.ReportAllocs()
			b.SetBytes(int64(test.symbolSize))
			lastESI := firstESI
			for b.Loop() {
				lastESI = firstESI
				benchmarkPacketSink, err = encoder.Packet(0, firstESI, 1, false)
				if err != nil {
					b.Fatal(err)
				}
				firstESI++
				if firstESI == test.k {
					firstESI = 0
				}
			}
			validateBenchmarkSourcePacket(b, benchmarkPacketSink, object, oti, 0, lastESI, 1)
		})
		b.Run("systematic-after-repair/"+name, func(b *testing.B) {
			encoder, err := NewObjectEncoder(object, oti, DefaultLimits())
			if err != nil {
				b.Fatal(err)
			}
			if _, err := encoder.Packet(0, test.k, 1, false); err != nil {
				b.Fatal(err)
			}
			firstESI := test.k / 2
			b.ReportAllocs()
			b.SetBytes(int64(test.symbolSize))
			lastESI := firstESI
			for b.Loop() {
				lastESI = firstESI
				benchmarkPacketSink, err = encoder.Packet(0, firstESI, 1, false)
				if err != nil {
					b.Fatal(err)
				}
				firstESI++
				if firstESI == test.k {
					firstESI = 0
				}
			}
			validateBenchmarkSourcePacket(b, benchmarkPacketSink, object, oti, 0, lastESI, 1)
		})
		b.Run("repair-cached/"+name, func(b *testing.B) {
			encoder, err := NewObjectEncoder(object, oti, DefaultLimits())
			if err != nil {
				b.Fatal(err)
			}
			if _, err := encoder.Packet(0, test.k, 1, false); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(test.symbolSize))
			for b.Loop() {
				benchmarkPacketSink, err = encoder.Packet(0, test.k+17, 1, false)
				if err != nil {
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
				benchmarkPacketSink, err = fresh.Packet(0, test.k, 1, false)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("construct-and-systematic/"+name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(oti.TransferLength))
			for b.Loop() {
				fresh, err := NewObjectEncoder(object, oti, DefaultLimits())
				if err != nil {
					b.Fatal(err)
				}
				benchmarkPacketSink, err = fresh.Packet(0, test.k/2, 1, false)
				if err != nil {
					b.Fatal(err)
				}
			}
			validateBenchmarkSourcePacket(b, benchmarkPacketSink, object, oti, 0, test.k/2, 1)
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
			validateBenchmarkSourcePacket(
				b, packet, object, test.oti, test.sourceBlock, test.firstESI, test.count,
			)
			b.ReportAllocs()
			b.SetBytes(int64(len(packet) - PayloadIDEncodedSize))
			b.ReportMetric(float64(test.count), "symbols/op")
			firstESI := test.firstESI
			k, err := SourceBlockSymbols(test.oti, test.sourceBlock)
			if err != nil {
				b.Fatal(err)
			}
			lastESI := firstESI
			for b.Loop() {
				lastESI = firstESI
				benchmarkPacketSink, err = encoder.Packet(test.sourceBlock, firstESI, test.count, test.shorten)
				if err != nil {
					b.Fatal(err)
				}
				if !test.shorten {
					firstESI++
					if uint64(firstESI)+uint64(test.count) > uint64(k) {
						firstESI = 0
					}
				}
			}
			validateBenchmarkSourcePacket(
				b, benchmarkPacketSink, object, test.oti, test.sourceBlock, lastESI, test.count,
			)
		})
	}
}

func validateBenchmarkSourcePacket(
	b testing.TB,
	packet, object []byte,
	oti OTI,
	sourceBlock uint8,
	firstESI, count uint32,
) {
	b.Helper()
	view, err := ParseEncodingPacket(packet, oti, DefaultLimits())
	if err != nil {
		b.Fatal(err)
	}
	if view.PayloadID() != (PayloadID{SourceBlockNumber: sourceBlock, EncodingSymbolID: firstESI}) ||
		!view.IsSource() || view.SymbolCount() != count {
		b.Fatalf("unexpected benchmark packet metadata: %+v", view)
	}
	block, err := BuildSourceBlock(object, oti, sourceBlock, DefaultLimits())
	if err != nil {
		b.Fatal(err)
	}
	canonical := make([]byte, oti.SymbolSize)
	for index := uint32(0); index < count; index++ {
		if err := view.CopySymbol(index, canonical); err != nil {
			b.Fatal(err)
		}
		want, err := block.Symbol(firstESI + index)
		if err != nil {
			b.Fatal(err)
		}
		if !bytes.Equal(canonical, want) {
			b.Fatalf("source symbol %d differs: got %x, want %x", firstESI+index, canonical, want)
		}
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
