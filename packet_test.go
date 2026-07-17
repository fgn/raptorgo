package raptorgo

import (
	"bytes"
	"errors"
	"testing"
)

func packetBytes(t *testing.T, id PayloadID, payload []byte) []byte {
	t.Helper()
	header, err := id.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return append(header, payload...)
}

func TestParseGroupedSourcePacketWithShortFinalSymbol(t *testing.T) {
	oti := OTI{TransferLength: 10, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	id, _ := NewPayloadID(0, 1)
	encoded := packetBytes(t, id, []byte{4, 5, 6, 7, 8, 9})
	packet, err := ParseEncodingPacket(encoded, oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !packet.IsSource() || packet.SymbolCount() != 2 || packet.PayloadID() != id {
		t.Fatalf("unexpected packet metadata: %+v", packet)
	}
	first, err := packet.Symbol(0)
	if err != nil {
		t.Fatal(err)
	}
	last, err := packet.Symbol(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 4 || len(last) != 2 {
		t.Fatalf("symbol lengths = %d, %d", len(first), len(last))
	}
	canonical := make([]byte, oti.SymbolSize)
	if err := packet.CopySymbol(1, canonical); err != nil {
		t.Fatal(err)
	}
	if string(canonical) != string([]byte{8, 9, 0, 0}) {
		t.Fatalf("canonical final symbol = %v", canonical)
	}
}

func TestParseFullFinalSourceSymbolRequiresZeroPadding(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 6, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	id, _ := NewPayloadID(0, 1)
	if _, err := ParseEncodingPacket(packetBytes(t, id, []byte{4, 5, 99, 0}), oti, DefaultLimits()); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("nonzero padding returned %v", err)
	}
	packet, err := ParseEncodingPacket(packetBytes(t, id, []byte{4, 5, 0, 0}), oti, DefaultLimits())
	if err != nil {
		t.Fatalf("zero padding rejected: %v", err)
	}
	symbol, err := packet.Symbol(0)
	if err != nil {
		t.Fatal(err)
	}
	if cap(symbol) != len(symbol) {
		t.Fatalf("symbol capacity = %d, want %d", cap(symbol), len(symbol))
	}
}

func TestSourcePacketPaddingFollowsSubBlockLayout(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 6, SymbolSize: 5, SourceBlocks: 1, SubBlocks: 3, SymbolAlignment: 1}
	object := []byte("abcdef")
	encoder, err := NewObjectEncoder(object, oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	grouped, err := encoder.Packet(0, 0, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(grouped) != PayloadIDEncodedSize+7 {
		t.Fatalf("shortened grouped length=%d, want=%d", len(grouped), PayloadIDEncodedSize+7)
	}
	view, err := ParseEncodingPacket(grouped, oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{{'a', 'b', 'e', 'f', 0}, {'c', 'd', 0, 0, 0}}
	canonical := make([]byte, oti.SymbolSize)
	for index := range want {
		if err := view.CopySymbol(uint32(index), canonical); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(canonical, want[index]) {
			t.Fatalf("symbol %d=%q want=%q", index, canonical, want[index])
		}
	}

	first, err := encoder.Packet(0, 0, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != PayloadIDEncodedSize+4 {
		t.Fatalf("shortened first-symbol length=%d", len(first))
	}
	firstView, err := ParseEncodingPacket(first, oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err := firstView.CopySymbol(0, canonical); err != nil || !bytes.Equal(canonical, want[0]) {
		t.Fatalf("canonical first=%q error=%v", canonical, err)
	}

	id, err := NewPayloadID(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseEncodingPacket(packetBytes(t, id, []byte{'c', 'd', 0}), oti, DefaultLimits()); err != nil {
		t.Fatalf("partially retained zero padding rejected: %v", err)
	}
	if _, err := ParseEncodingPacket(packetBytes(t, id, []byte{'c', 'd', 9}), oti, DefaultLimits()); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("nonzero distributed padding returned %v", err)
	}
	id, err = NewPayloadID(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseEncodingPacket(packetBytes(t, id, []byte{'a', 'b', 'e', 'f', 9}), oti, DefaultLimits()); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("nonzero earlier-symbol padding returned %v", err)
	}
}

func TestSourcePacketPaddingLayoutInvariant(t *testing.T) {
	for symbolSize := uint16(1); symbolSize <= 12; symbolSize++ {
		for subBlocks := uint16(1); subBlocks <= symbolSize; subBlocks++ {
			for sourceBlocks := uint8(1); sourceBlocks <= 4; sourceBlocks++ {
				for transferLength := uint64(1); transferLength <= 48; transferLength++ {
					oti := OTI{
						TransferLength: transferLength, SymbolSize: symbolSize,
						SourceBlocks: sourceBlocks, SubBlocks: subBlocks, SymbolAlignment: 1,
					}
					object := patternedObject(transferLength)
					encoder, err := NewObjectEncoder(object, oti, DefaultLimits())
					if err != nil {
						t.Fatalf("encoder %+v: %v", oti, err)
					}
					for sourceBlock := uint8(0); sourceBlock < sourceBlocks; sourceBlock++ {
						block, buildErr := BuildSourceBlock(object, oti, sourceBlock, DefaultLimits())
						if buildErr != nil {
							t.Fatalf("block %+v SBN=%d: %v", oti, sourceBlock, buildErr)
						}
						k := block.SymbolCount()
						if k == 0 {
							continue
						}
						for esi := uint32(0); esi < k; esi++ {
							for _, shorten := range []bool{false, true} {
								encoded, packetErr := encoder.Packet(sourceBlock, esi, 1, shorten)
								if packetErr != nil {
									t.Fatalf("packet %+v SBN=%d ESI=%d shorten=%v: %v", oti, sourceBlock, esi, shorten, packetErr)
								}
								view, parseErr := ParseEncodingPacket(encoded, oti, DefaultLimits())
								if parseErr != nil {
									t.Fatalf("parse %+v SBN=%d ESI=%d shorten=%v: %v", oti, sourceBlock, esi, shorten, parseErr)
								}
								canonical := make([]byte, symbolSize)
								if copyErr := view.CopySymbol(0, canonical); copyErr != nil {
									t.Fatal(copyErr)
								}
								want, symbolErr := block.Symbol(esi)
								if symbolErr != nil {
									t.Fatal(symbolErr)
								}
								if !bytes.Equal(canonical, want) {
									t.Fatalf("canonical %+v SBN=%d ESI=%d shorten=%v: %x want %x", oti, sourceBlock, esi, shorten, canonical, want)
								}
							}
						}
						grouped, packetErr := encoder.Packet(sourceBlock, 0, k, true)
						if packetErr != nil {
							t.Fatal(packetErr)
						}
						view, parseErr := ParseEncodingPacket(grouped, oti, DefaultLimits())
						if parseErr != nil || view.SymbolCount() != k {
							t.Fatalf("grouped %+v SBN=%d count=%d parse=%v", oti, sourceBlock, view.SymbolCount(), parseErr)
						}
					}
				}
			}
		}
	}
}

func TestObjectEncoderSourcePacketMatchesIndependentBlockLayouts(t *testing.T) {
	for symbolSize := uint16(1); symbolSize <= 12; symbolSize++ {
		for alignment := uint8(1); uint16(alignment) <= symbolSize; alignment++ {
			if symbolSize%uint16(alignment) != 0 {
				continue
			}
			units := symbolSize / uint16(alignment)
			subBlockCandidates := []uint16{1, (units + 1) / 2, units, units + 1}
			seenSubBlocks := make(map[uint16]bool, len(subBlockCandidates))
			for _, subBlocks := range subBlockCandidates {
				if seenSubBlocks[subBlocks] {
					continue
				}
				seenSubBlocks[subBlocks] = true
				for sourceBlocks := uint8(1); sourceBlocks <= 3; sourceBlocks++ {
					lengthCandidates := []uint64{
						1,
						uint64(symbolSize),
						uint64(symbolSize) + 1,
						uint64(symbolSize)*uint64(2*sourceBlocks+1) - 1,
						uint64(symbolSize)*uint64(3*sourceBlocks) - 1,
					}
					if symbolSize > 1 {
						lengthCandidates = append(lengthCandidates, uint64(symbolSize-1))
					}
					seenLengths := make(map[uint64]bool, len(lengthCandidates))
					for _, transferLength := range lengthCandidates {
						if seenLengths[transferLength] {
							continue
						}
						seenLengths[transferLength] = true
						oti := OTI{
							TransferLength: transferLength, SymbolSize: symbolSize,
							SourceBlocks: sourceBlocks, SubBlocks: subBlocks, SymbolAlignment: alignment,
						}
						object := patternedObject(transferLength)
						encoder, err := NewObjectEncoder(object, oti, DefaultLimits())
						if err != nil {
							t.Fatalf("encoder %+v: %v", oti, err)
						}
						for sourceBlockIndex := 0; sourceBlockIndex < int(sourceBlocks); sourceBlockIndex++ {
							sourceBlock := uint8(sourceBlockIndex)
							block, err := BuildSourceBlock(object, oti, sourceBlock, DefaultLimits())
							if err != nil {
								t.Fatalf("block %+v SBN=%d: %v", oti, sourceBlock, err)
							}
							k := block.SymbolCount()
							if k == 0 {
								continue
							}
							groups := [][2]uint32{{0, 1}, {k - 1, 1}, {0, k}}
							if k > 2 {
								groups = append(groups, [2]uint32{1, min(uint32(2), k-1)})
								middle := k / 2
								groups = append(groups, [2]uint32{middle, min(uint32(3), k-middle)})
							}
							for _, group := range groups {
								for _, shorten := range []bool{false, true} {
									got, err := encoder.Packet(sourceBlock, group[0], group[1], shorten)
									if err != nil {
										t.Fatalf("packet %+v SBN=%d ESI=%d count=%d shorten=%v: %v", oti, sourceBlock, group[0], group[1], shorten, err)
									}
									want := expectedSourcePacketFromBlock(t, block, oti, group[0], group[1], shorten)
									if !bytes.Equal(got, want) {
										t.Fatalf("packet %+v SBN=%d ESI=%d count=%d shorten=%v: got %x, want %x", oti, sourceBlock, group[0], group[1], shorten, got, want)
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

func expectedSourcePacketFromBlock(
	t *testing.T,
	block SourceBlock,
	oti OTI,
	firstESI, count uint32,
	shorten bool,
) []byte {
	t.Helper()
	header, err := (PayloadID{SourceBlockNumber: block.Number(), EncodingSymbolID: firstESI}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	result := append([]byte(nil), header...)
	for offset := uint32(0); offset < count; offset++ {
		symbol, err := block.Symbol(firstESI + offset)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, symbol...)
	}
	if shorten {
		layout, err := paddingLayoutForSourceBlock(oti, block.Number())
		if err != nil {
			t.Fatal(err)
		}
		padding, err := layout.trailingPadding(firstESI + count - 1)
		if err != nil {
			t.Fatal(err)
		}
		result = result[:uint64(len(result))-padding]
	}
	return result
}

func TestObjectEncoderPacketLimitsAtExactBoundaries(t *testing.T) {
	oti := OTI{TransferLength: 12, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	object := patternedObject(oti.TransferLength)
	exact := DefaultLimits()
	exact.MaxPacketBytes = PayloadIDEncodedSize + 8
	exact.MaxSymbolsPerPacket = 2
	encoder, err := NewObjectEncoder(object, oti, exact)
	if err != nil {
		t.Fatal(err)
	}
	if packet, err := encoder.Packet(0, 0, 2, false); err != nil || len(packet) != PayloadIDEncodedSize+8 {
		t.Fatalf("exact packet=%x error=%v", packet, err)
	}
	if packet, err := encoder.Packet(0, 0, 3, false); !errors.Is(err, ErrInvalidPacket) || packet != nil {
		t.Fatalf("over-count packet=%x error=%v", packet, err)
	}

	short := exact
	short.MaxPacketBytes--
	encoder, err = NewObjectEncoder(object, oti, short)
	if err != nil {
		t.Fatal(err)
	}
	if packet, err := encoder.Packet(0, 0, 2, false); !errors.Is(err, ErrLimitExceeded) || packet != nil {
		t.Fatalf("over-byte packet=%x error=%v", packet, err)
	}
}

func TestFinalSourceSymbolPrecedesTrailingEmptyBlocks(t *testing.T) {
	t.Parallel()

	for _, sourceBlocks := range []uint8{2, 4} {
		oti := OTI{
			TransferLength:  1,
			SymbolSize:      4,
			SourceBlocks:    sourceBlocks,
			SubBlocks:       1,
			SymbolAlignment: 1,
		}
		id, _ := NewPayloadID(0, 0)
		short, err := ParseEncodingPacket(packetBytes(t, id, []byte{7}), oti, RFCWireLimits())
		if err != nil {
			t.Fatalf("Z=%d shortened final symbol: %v", sourceBlocks, err)
		}
		canonical := make([]byte, oti.SymbolSize)
		if err := short.CopySymbol(0, canonical); err != nil {
			t.Fatal(err)
		}
		if string(canonical) != string([]byte{7, 0, 0, 0}) {
			t.Fatalf("Z=%d canonical symbol = %v", sourceBlocks, canonical)
		}
		if _, err := ParseEncodingPacket(packetBytes(t, id, []byte{7, 1, 0, 0}), oti, RFCWireLimits()); !errors.Is(err, ErrInvalidPacket) {
			t.Fatalf("Z=%d nonzero full padding returned %v", sourceBlocks, err)
		}
	}
}

func TestParseGroupedRepairPacket(t *testing.T) {
	oti := OTI{TransferLength: 10, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	id, _ := NewPayloadID(0, 3)
	packet, err := ParseEncodingPacket(packetBytes(t, id, make([]byte, 8)), oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if packet.IsSource() || packet.SymbolCount() != 2 {
		t.Fatalf("unexpected repair packet metadata: %+v", packet)
	}
}

func TestPacketRejectsInvalidGrouping(t *testing.T) {
	oti := OTI{TransferLength: 10, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	tests := []struct {
		name    string
		id      PayloadID
		payload []byte
	}{
		{"cross source repair boundary", PayloadID{EncodingSymbolID: 2}, make([]byte, 8)},
		{"wrong shortened final size", PayloadID{EncodingSymbolID: 1}, make([]byte, 5)},
		{"partial repair", PayloadID{EncodingSymbolID: 3}, make([]byte, 3)},
		{"ESI overflow", PayloadID{EncodingSymbolID: MaxEncodingSymbolID}, make([]byte, 8)},
		{"unknown source block", PayloadID{SourceBlockNumber: 1}, make([]byte, 4)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseEncodingPacket(packetBytes(t, test.id, test.payload), oti, DefaultLimits())
			if !errors.Is(err, ErrInvalidPacket) {
				t.Fatalf("returned %v", err)
			}
		})
	}
}

func TestPacketLimitsAreCheckedBeforeUse(t *testing.T) {
	oti := OTI{TransferLength: 12, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	limits := DefaultLimits()
	limits.MaxSymbolsPerPacket = 1
	id, _ := NewPayloadID(0, 0)
	_, err := ParseEncodingPacket(packetBytes(t, id, make([]byte, 8)), oti, limits)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("symbol-count limit returned %v", err)
	}
}

func TestSourceBlockSymbols(t *testing.T) {
	oti := OTI{TransferLength: 17, SymbolSize: 1, SourceBlocks: 5, SubBlocks: 1, SymbolAlignment: 1}
	want := []uint32{4, 4, 3, 3, 3}
	for block, wantSymbols := range want {
		got, err := SourceBlockSymbols(oti, uint8(block))
		if err != nil {
			t.Fatal(err)
		}
		if got != wantSymbols {
			t.Fatalf("block %d has %d symbols, want %d", block, got, wantSymbols)
		}
	}
}

func TestSourceBlockSymbolsValidatesOTI(t *testing.T) {
	t.Parallel()

	oti := OTI{
		TransferLength:  MaxTransferLength,
		SymbolSize:      1,
		SourceBlocks:    1,
		SubBlocks:       1,
		SymbolAlignment: 1,
	}
	if _, err := SourceBlockSymbols(oti, 0); !errors.Is(err, ErrInvalidOTI) {
		t.Fatalf("oversized block returned %v", err)
	}

	empty := OTI{TransferLength: 0, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	if got, err := SourceBlockSymbols(empty, 0); err != nil || got != 0 {
		t.Fatalf("empty block symbols = %d, %v", got, err)
	}
}

func TestEncodingPacketSymbolCapacityIsBounded(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 8, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	id, _ := NewPayloadID(0, 0)
	packet, err := ParseEncodingPacket(packetBytes(t, id, make([]byte, 8)), oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	symbol, err := packet.Symbol(0)
	if err != nil {
		t.Fatal(err)
	}
	if cap(symbol) != len(symbol) {
		t.Fatalf("symbol capacity = %d, want %d", cap(symbol), len(symbol))
	}
	if err := packet.CopySymbol(0, make([]byte, 3)); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("short destination returned %v", err)
	}
}

func TestEncodingPacketPayloadCapacityIsBounded(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 1, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	id, err := NewPayloadID(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	backing := make([]byte, PayloadIDEncodedSize+2)
	header, err := id.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	copy(backing, header)
	backing[PayloadIDEncodedSize] = 7
	backing[PayloadIDEncodedSize+1] = 99
	packet, err := ParseEncodingPacket(backing[:PayloadIDEncodedSize+1], oti, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	payload := packet.Payload()
	if cap(payload) != len(payload) {
		t.Fatalf("payload len=%d cap=%d", len(payload), cap(payload))
	}
}

func TestEncodingPacketRejectsEmptySourceBlock(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 0, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	id, _ := NewPayloadID(0, 0)
	if _, err := ParseEncodingPacket(packetBytes(t, id, []byte{1}), oti, RFCWireLimits()); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("empty source block packet returned %v", err)
	}
}

func FuzzParseEncodingPacket(f *testing.F) {
	validOTI := OTI{TransferLength: 10, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	id, _ := NewPayloadID(0, 1)
	header, _ := id.MarshalBinary()
	seed := append(header, 4, 5, 6, 7, 8, 9)
	f.Add(seed,
		validOTI.TransferLength, validOTI.SymbolSize, validOTI.SourceBlocks,
		validOTI.SubBlocks, validOTI.SymbolAlignment)
	f.Fuzz(func(
		t *testing.T,
		encoded []byte,
		transferLength uint64,
		symbolSize uint16,
		sourceBlocks uint8,
		subBlocks uint16,
		symbolAlignment uint8,
	) {
		oti := OTI{
			TransferLength:  transferLength,
			SymbolSize:      symbolSize,
			SourceBlocks:    sourceBlocks,
			SubBlocks:       subBlocks,
			SymbolAlignment: symbolAlignment,
		}
		packet, err := ParseEncodingPacket(encoded, oti, DefaultLimits())
		if err != nil {
			return
		}
		if packet.SymbolCount() == 0 {
			t.Fatal("accepted packet has no symbols")
		}
		canonical := make([]byte, oti.SymbolSize)
		for index := uint32(0); index < packet.SymbolCount(); index++ {
			symbol, symbolErr := packet.Symbol(index)
			if symbolErr != nil {
				t.Fatal(symbolErr)
			}
			if len(symbol) == 0 || len(symbol) > int(oti.SymbolSize) || cap(symbol) != len(symbol) {
				t.Fatalf("invalid symbol view len=%d cap=%d T=%d", len(symbol), cap(symbol), oti.SymbolSize)
			}
			if copyErr := packet.CopySymbol(index, canonical); copyErr != nil {
				t.Fatal(copyErr)
			}
			for _, padding := range canonical[len(symbol):] {
				if padding != 0 {
					t.Fatalf("canonical padding is nonzero: %v", canonical)
				}
			}
		}
	})
}
