// Package rfc6330 contains deterministic formulas and constants from RFC 6330.
package rfc6330

import "errors"

var (
	errZeroPartitions = errors.New("rfc6330: partition count must be positive")
	errPartitionIndex = errors.New("rfc6330: partition index out of range")
)

// PartitionResult is the sequence (IL, IS, JL, JS) returned by Partition[I,J]
// in RFC 6330 Section 4.4.1.2.
type PartitionResult struct {
	LargeSize  uint64
	SmallSize  uint64
	LargeCount uint64
	SmallCount uint64
}

// Size returns the size of the indexed block. RFC ordering places all large
// blocks before all small blocks.
func (result PartitionResult) Size(index uint64) (uint64, error) {
	if index >= result.LargeCount+result.SmallCount {
		return 0, errPartitionIndex
	}
	if index < result.LargeCount {
		return result.LargeSize, nil
	}
	return result.SmallSize, nil
}

// Partition divides total into parts approximately equal-sized blocks. Large
// blocks precede small blocks when materializing the partition.
func Partition(total, parts uint64) (PartitionResult, error) {
	if parts == 0 {
		return PartitionResult{}, errZeroPartitions
	}

	smallSize := total / parts
	largeCount := total % parts
	largeSize := smallSize
	if largeCount != 0 {
		largeSize++
	}

	return PartitionResult{
		LargeSize:  largeSize,
		SmallSize:  smallSize,
		LargeCount: largeCount,
		SmallCount: parts - largeCount,
	}, nil
}
