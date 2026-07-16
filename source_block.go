package raptorgo

import (
	"fmt"

	"github.com/fgn/raptorgo/internal/rfc6330"
)

// SourceBlock contains one RFC 6330 source block in symbol-major order. Its
// storage owns a copy of the corresponding source-object bytes and any zero
// padding required for the final source symbol.
type SourceBlock struct {
	number     uint8
	symbolSize uint16
	symbols    uint32
	data       []byte
}

// Number returns the block's source block number (SBN).
func (block SourceBlock) Number() uint8 { return block.number }

// SymbolSize returns the symbol size T in bytes.
func (block SourceBlock) SymbolSize() uint16 { return block.symbolSize }

// SymbolCount returns K, the number of source symbols in the block.
func (block SourceBlock) SymbolCount() uint32 {
	return block.symbols
}

// Symbol returns a view into the block-owned storage. Callers must not mutate
// the returned bytes.
func (block SourceBlock) Symbol(index uint32) ([]byte, error) {
	if index >= block.symbols {
		return nil, fmt.Errorf("%w: source symbol index %d", ErrInvalidObject, index)
	}
	start := uint64(index) * uint64(block.symbolSize)
	end := start + uint64(block.symbolSize)
	return block.data[int(start):int(end):int(end)], nil
}

// BuildSourceBlock constructs one source block from a complete source object.
// For N > 1, it converts RFC sub-block-major input ordering into symbol-major
// storage. All externally controlled sizes are validated before allocation.
func BuildSourceBlock(object []byte, oti OTI, sourceBlockNumber uint8, limits Limits) (SourceBlock, error) {
	if err := oti.Validate(limits); err != nil {
		return SourceBlock{}, err
	}
	if uint64(len(object)) != oti.TransferLength {
		return SourceBlock{}, fmt.Errorf(
			"%w: object has %d bytes, OTI declares %d",
			ErrInvalidObject,
			len(object),
			oti.TransferLength,
		)
	}
	if sourceBlockNumber >= oti.SourceBlocks {
		return SourceBlock{}, fmt.Errorf("%w: SBN %d is outside Z=%d", ErrInvalidObject, sourceBlockNumber, oti.SourceBlocks)
	}

	totalSymbols := ceilDiv(oti.TransferLength, uint64(oti.SymbolSize))
	blockPartition, err := rfc6330.Partition(totalSymbols, uint64(oti.SourceBlocks))
	if err != nil {
		return SourceBlock{}, err
	}
	blockSymbols, err := blockPartition.Size(uint64(sourceBlockNumber))
	if err != nil {
		return SourceBlock{}, err
	}
	firstSymbol := sourceBlockFirstSymbol(blockPartition, uint64(sourceBlockNumber))
	blockBytes := blockSymbols * uint64(oti.SymbolSize)
	if blockBytes > limits.MaxSourceBlockBytes || blockBytes > limits.MaxWorkingMemory {
		return SourceBlock{}, fmt.Errorf("%w: source block construction requires %d bytes", ErrLimitExceeded, blockBytes)
	}
	if blockBytes > uint64(maxInt()) {
		return SourceBlock{}, fmt.Errorf("%w: source block does not fit address space", ErrLimitExceeded)
	}

	data := make([]byte, int(blockBytes))
	blockStart := firstSymbol * uint64(oti.SymbolSize)
	if oti.SubBlocks == 1 {
		copyObjectRange(data, object, blockStart)
	} else {
		subSymbolUnits := uint64(oti.SymbolSize) / uint64(oti.SymbolAlignment)
		subBlocks, partitionErr := rfc6330.Partition(subSymbolUnits, uint64(oti.SubBlocks))
		if partitionErr != nil {
			return SourceBlock{}, partitionErr
		}

		var inputOffset uint64
		var symbolOffset uint64
		for subBlock := uint64(0); subBlock < uint64(oti.SubBlocks); subBlock++ {
			units, sizeErr := subBlocks.Size(subBlock)
			if sizeErr != nil {
				return SourceBlock{}, sizeErr
			}
			subSymbolBytes := units * uint64(oti.SymbolAlignment)
			for symbol := uint64(0); symbol < blockSymbols; symbol++ {
				destination := symbol*uint64(oti.SymbolSize) + symbolOffset
				copyObjectRange(
					data[int(destination):int(destination+subSymbolBytes)],
					object,
					blockStart+inputOffset,
				)
				inputOffset += subSymbolBytes
			}
			symbolOffset += subSymbolBytes
		}
	}

	return SourceBlock{
		number:     sourceBlockNumber,
		symbolSize: oti.SymbolSize,
		symbols:    uint32(blockSymbols),
		data:       data,
	}, nil
}

func sourceBlockFirstSymbol(partition rfc6330.PartitionResult, sourceBlockNumber uint64) uint64 {
	largeBefore := sourceBlockNumber
	if largeBefore > partition.LargeCount {
		largeBefore = partition.LargeCount
	}
	smallBefore := sourceBlockNumber - largeBefore
	return largeBefore*partition.LargeSize + smallBefore*partition.SmallSize
}

func copyObjectRange(destination, object []byte, sourceStart uint64) {
	if sourceStart >= uint64(len(object)) {
		return
	}
	copy(destination, object[int(sourceStart):])
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
