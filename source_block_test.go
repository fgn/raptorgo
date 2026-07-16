package raptorgo

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/fgn/raptorgo/internal/rfc6330"
)

func TestBuildSourceBlockSingleSubBlock(t *testing.T) {
	t.Parallel()

	oti := OTI{
		TransferLength:  10,
		SymbolSize:      4,
		SourceBlocks:    1,
		SubBlocks:       1,
		SymbolAlignment: 1,
	}
	block, err := BuildSourceBlock([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, oti, 0, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if block.SymbolCount() != 3 || block.SymbolSize() != 4 || block.Number() != 0 {
		t.Fatalf("unexpected block metadata: %+v", block)
	}
	want := [][]byte{{0, 1, 2, 3}, {4, 5, 6, 7}, {8, 9, 0, 0}}
	assertSourceSymbols(t, block, want)
}

func TestBuildSourceBlockMultipleSubBlocks(t *testing.T) {
	t.Parallel()

	oti := OTI{
		TransferLength:  12,
		SymbolSize:      4,
		SourceBlocks:    1,
		SubBlocks:       2,
		SymbolAlignment: 1,
	}
	block, err := BuildSourceBlock([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, oti, 0, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	// The object is sub-block-major: two bytes for every symbol from the
	// first sub-block, followed by two bytes for every symbol from the second.
	want := [][]byte{{0, 1, 6, 7}, {2, 3, 8, 9}, {4, 5, 10, 11}}
	assertSourceSymbols(t, block, want)
}

func TestBuildSourceBlockUnequalSubBlocksAndPadding(t *testing.T) {
	t.Parallel()

	oti := OTI{
		TransferLength:  18,
		SymbolSize:      6,
		SourceBlocks:    1,
		SubBlocks:       4,
		SymbolAlignment: 1,
	}
	object := make([]byte, oti.TransferLength)
	for i := range object {
		object[i] = byte(i)
	}
	block, err := BuildSourceBlock(object, oti, 0, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	// Partition[6,4] yields 2,2,1,1 byte sub-symbols, large first.
	want := [][]byte{
		{0, 1, 6, 7, 12, 15},
		{2, 3, 8, 9, 13, 16},
		{4, 5, 10, 11, 14, 17},
	}
	assertSourceSymbols(t, block, want)
}

func TestBuildSourceBlockOffsetsAcrossOrderedPartition(t *testing.T) {
	t.Parallel()

	oti := OTI{
		TransferLength:  19,
		SymbolSize:      4,
		SourceBlocks:    2,
		SubBlocks:       1,
		SymbolAlignment: 1,
	}
	object := make([]byte, oti.TransferLength)
	for i := range object {
		object[i] = byte(i)
	}

	first, err := BuildSourceBlock(object, oti, 0, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildSourceBlock(object, oti, 1, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	assertSourceSymbols(t, first, [][]byte{{0, 1, 2, 3}, {4, 5, 6, 7}, {8, 9, 10, 11}})
	assertSourceSymbols(t, second, [][]byte{{12, 13, 14, 15}, {16, 17, 18, 0}})
}

func TestBuildSourceBlockCombinedFixedVector(t *testing.T) {
	t.Parallel()

	oti := OTI{
		TransferLength:  29,
		SymbolSize:      6,
		SourceBlocks:    2,
		SubBlocks:       4,
		SymbolAlignment: 1,
	}
	object := make([]byte, oti.TransferLength)
	for index := range object {
		object[index] = byte(index)
	}
	first, err := BuildSourceBlock(object, oti, 0, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildSourceBlock(object, oti, 1, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	assertSourceSymbols(t, first, [][]byte{
		{0, 1, 6, 7, 12, 15},
		{2, 3, 8, 9, 13, 16},
		{4, 5, 10, 11, 14, 17},
	})
	assertSourceSymbols(t, second, [][]byte{
		{18, 19, 22, 23, 26, 28},
		{20, 21, 24, 25, 27, 0},
	})
	last, err := second.Symbol(1)
	if err != nil {
		t.Fatal(err)
	}
	if cap(last) != len(last) {
		t.Fatalf("source symbol capacity = %d, want %d", cap(last), len(last))
	}
}

func TestBuildSourceBlockRFCZeroSizedPartitions(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 1, SymbolSize: 1, SourceBlocks: 2, SubBlocks: 2, SymbolAlignment: 1}
	first, err := BuildSourceBlock([]byte{42}, oti, 0, RFCWireLimits())
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildSourceBlock([]byte{42}, oti, 1, RFCWireLimits())
	if err != nil {
		t.Fatal(err)
	}
	assertSourceSymbols(t, first, [][]byte{{42}})
	if second.SymbolCount() != 0 {
		t.Fatalf("empty partition has %d symbols", second.SymbolCount())
	}

	emptyOTI := OTI{TransferLength: 0, SymbolSize: 1, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	empty, err := BuildSourceBlock(nil, emptyOTI, 0, RFCWireLimits())
	if err != nil {
		t.Fatal(err)
	}
	if empty.SymbolCount() != 0 {
		t.Fatalf("empty object block has %d symbols", empty.SymbolCount())
	}
}

func TestSourceBlockLayoutInvariant(t *testing.T) {
	t.Parallel()

	for symbolSize := uint16(1); symbolSize <= 12; symbolSize++ {
		for subBlocks := uint16(1); subBlocks <= symbolSize; subBlocks++ {
			for sourceBlocks := uint8(1); sourceBlocks <= 4; sourceBlocks++ {
				for transferLength := uint64(sourceBlocks); transferLength <= 48; transferLength++ {
					totalSymbols := ceilDiv(transferLength, uint64(symbolSize))
					if totalSymbols < uint64(sourceBlocks) {
						continue
					}
					oti := OTI{
						TransferLength:  transferLength,
						SymbolSize:      symbolSize,
						SourceBlocks:    sourceBlocks,
						SubBlocks:       subBlocks,
						SymbolAlignment: 1,
					}
					object := make([]byte, transferLength)
					for index := range object {
						object[index] = byte(index*31 + 7)
					}

					blockPartition, err := rfc6330.Partition(totalSymbols, uint64(sourceBlocks))
					if err != nil {
						t.Fatal(err)
					}
					for sourceBlock := uint8(0); sourceBlock < sourceBlocks; sourceBlock++ {
						block, buildErr := BuildSourceBlock(object, oti, sourceBlock, DefaultLimits())
						if buildErr != nil {
							t.Fatalf("BuildSourceBlock(%+v, %d): %v", oti, sourceBlock, buildErr)
						}
						got := serializeSubBlockMajor(t, block, oti)
						firstSymbol := sourceBlockFirstSymbol(blockPartition, uint64(sourceBlock))
						start := firstSymbol * uint64(symbolSize)
						want := make([]byte, len(got))
						copyObjectRange(want, object, start)
						if !bytes.Equal(got, want) {
							t.Fatalf("sub-block-major round trip %+v SBN=%d: got %v, want %v", oti, sourceBlock, got, want)
						}
					}
				}
			}
		}
	}
}

func TestBuildSourceBlockRejectsInvalidInputsBeforeAllocation(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 8, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	if _, err := BuildSourceBlock([]byte{1}, oti, 0, DefaultLimits()); !errors.Is(err, ErrInvalidObject) {
		t.Fatalf("short object error = %v", err)
	}
	if _, err := BuildSourceBlock(make([]byte, 8), oti, 1, DefaultLimits()); !errors.Is(err, ErrInvalidObject) {
		t.Fatalf("invalid SBN error = %v", err)
	}

	limits := DefaultLimits()
	limits.MaxWorkingMemory = 4
	if _, err := BuildSourceBlock(make([]byte, 8), oti, 0, limits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("working-memory error = %v", err)
	}
}

func TestSourceBlockSymbolRejectsOutOfRangeIndex(t *testing.T) {
	t.Parallel()

	oti := OTI{TransferLength: 4, SymbolSize: 4, SourceBlocks: 1, SubBlocks: 1, SymbolAlignment: 1}
	block, err := BuildSourceBlock(make([]byte, 4), oti, 0, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := block.Symbol(1); !errors.Is(err, ErrInvalidObject) {
		t.Fatalf("Symbol error = %v", err)
	}
}

func assertSourceSymbols(t *testing.T, block SourceBlock, want [][]byte) {
	t.Helper()
	got := make([][]byte, block.SymbolCount())
	for index := range got {
		var err error
		got[index], err = block.Symbol(uint32(index))
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("symbols = %v, want %v", got, want)
	}
}

func serializeSubBlockMajor(t *testing.T, block SourceBlock, oti OTI) []byte {
	t.Helper()
	partition, err := rfc6330.Partition(
		uint64(oti.SymbolSize)/uint64(oti.SymbolAlignment),
		uint64(oti.SubBlocks),
	)
	if err != nil {
		t.Fatal(err)
	}
	serialized := make([]byte, 0, int(block.SymbolCount())*int(block.SymbolSize()))
	var offset uint64
	for subBlock := uint64(0); subBlock < uint64(oti.SubBlocks); subBlock++ {
		units, sizeErr := partition.Size(subBlock)
		if sizeErr != nil {
			t.Fatal(sizeErr)
		}
		size := units * uint64(oti.SymbolAlignment)
		for index := uint32(0); index < block.SymbolCount(); index++ {
			symbol, symbolErr := block.Symbol(index)
			if symbolErr != nil {
				t.Fatal(symbolErr)
			}
			serialized = append(serialized, symbol[int(offset):int(offset+size)]...)
		}
		offset += size
	}
	return serialized
}
