package raptorgo

import (
	"fmt"

	"github.com/fgn/raptorgo/internal/rfc6330"
)

// sourcePaddingLayout describes how the single object-padding suffix is
// redistributed when a final source block is converted from sub-block-major
// object order to symbol-major FEC order (RFC 6330 Section 4.4.1.2).
type sourcePaddingLayout struct {
	symbols         uint32
	common          uint64
	boundarySize    uint64
	boundaryPadding uint64
}

func paddingLayoutForSourceBlock(oti OTI, sourceBlockNumber uint8) (sourcePaddingLayout, error) {
	k, err := SourceBlockSymbols(oti, sourceBlockNumber)
	if err != nil {
		return sourcePaddingLayout{}, err
	}
	layout := sourcePaddingLayout{symbols: k}
	totalSymbols := ceilDiv(oti.TransferLength, uint64(oti.SymbolSize))
	totalPadding := totalSymbols*uint64(oti.SymbolSize) - oti.TransferLength
	if totalPadding == 0 || k == 0 {
		return layout, nil
	}
	finalBlock, err := finalNonemptySourceBlock(oti)
	if err != nil {
		return sourcePaddingLayout{}, err
	}
	if sourceBlockNumber != finalBlock {
		return layout, nil
	}

	subSymbolUnits := uint64(oti.SymbolSize) / uint64(oti.SymbolAlignment)
	partition, err := rfc6330.Partition(subSymbolUnits, uint64(oti.SubBlocks))
	if err != nil {
		return sourcePaddingLayout{}, err
	}
	remaining := totalPadding
	for subBlock := uint64(oti.SubBlocks); subBlock > 0 && remaining > 0; subBlock-- {
		units, sizeErr := partition.Size(subBlock - 1)
		if sizeErr != nil {
			return sourcePaddingLayout{}, sizeErr
		}
		size := units * uint64(oti.SymbolAlignment)
		subBlockBytes := uint64(k) * size
		if remaining >= subBlockBytes {
			layout.common += size
			remaining -= subBlockBytes
			continue
		}
		layout.boundarySize = size
		layout.boundaryPadding = remaining
		remaining = 0
	}
	if remaining != 0 || layout.common >= uint64(oti.SymbolSize) {
		return sourcePaddingLayout{}, fmt.Errorf("%w: invalid source padding layout", ErrInvalidOTI)
	}
	return layout, nil
}

func (layout sourcePaddingLayout) trailingPadding(esi uint32) (uint64, error) {
	if esi >= layout.symbols {
		return 0, fmt.Errorf("%w: source ESI %d is outside K=%d", ErrInvalidPacket, esi, layout.symbols)
	}
	padding := layout.common
	if layout.boundaryPadding == 0 {
		return padding, nil
	}
	boundaryBytes := uint64(layout.symbols) * layout.boundarySize
	paddingStart := boundaryBytes - layout.boundaryPadding
	segmentStart := uint64(esi) * layout.boundarySize
	segmentEnd := segmentStart + layout.boundarySize
	if segmentEnd > paddingStart {
		padding += segmentEnd - max(segmentStart, paddingStart)
	}
	return padding, nil
}
