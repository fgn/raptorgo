package matrix

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidPermutationSize = errors.New("matrix: invalid permutation size")
	ErrPermutationLimit       = errors.New("matrix: permutation backing limit exceeded")
	ErrPermutationIndex       = errors.New("matrix: permutation index out of range")
)

// Permutation maps current positions to their original indices.
type Permutation struct {
	values []uint32
}

// NewIdentityPermutation constructs [0,size) after checking its backing bytes.
func NewIdentityPermutation(size uint32, maxBytes uint64) (*Permutation, error) {
	if size == 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidPermutationSize, size)
	}
	bytes := uint64(size) * 4
	if bytes > maxBytes || bytes > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("%w: need %d bytes (limit %d)", ErrPermutationLimit, bytes, maxBytes)
	}
	permutation := &Permutation{values: make([]uint32, int(size))}
	for index := range permutation.values {
		permutation.values[index] = uint32(index)
	}
	return permutation, nil
}

func (permutation *Permutation) Len() uint32 { return uint32(len(permutation.values)) }

// At returns the original index currently occupying position.
func (permutation *Permutation) At(position uint32) (uint32, error) {
	if position >= uint32(len(permutation.values)) {
		return 0, fmt.Errorf("%w: %d of %d", ErrPermutationIndex, position, len(permutation.values))
	}
	return permutation.values[position], nil
}

// Position returns the current position occupied by an original index.
func (permutation *Permutation) Position(original uint32) (uint32, error) {
	if original >= uint32(len(permutation.values)) {
		return 0, fmt.Errorf("%w: original %d of %d", ErrPermutationIndex, original, len(permutation.values))
	}
	for position, value := range permutation.values {
		if value == original {
			return uint32(position), nil
		}
	}
	return 0, fmt.Errorf("%w: original %d is missing", ErrPermutationIndex, original)
}

// Swap exchanges two current positions.
func (permutation *Permutation) Swap(left, right uint32) error {
	if left >= uint32(len(permutation.values)) || right >= uint32(len(permutation.values)) {
		return fmt.Errorf("%w: (%d,%d) of %d", ErrPermutationIndex, left, right, len(permutation.values))
	}
	permutation.values[left], permutation.values[right] = permutation.values[right], permutation.values[left]
	return nil
}

// Inverse returns a mapping from each original index to its current position.
func (permutation *Permutation) Inverse(maxBytes uint64) (*Permutation, error) {
	if permutation == nil || len(permutation.values) == 0 {
		return nil, fmt.Errorf("%w: nil or empty", ErrInvalidPermutationSize)
	}
	inverse, err := NewIdentityPermutation(uint32(len(permutation.values)), maxBytes)
	if err != nil {
		return nil, err
	}
	for index := range inverse.values {
		inverse.values[index] = ^uint32(0)
	}
	for position, original := range permutation.values {
		if original >= uint32(len(permutation.values)) {
			return nil, fmt.Errorf("%w: value %d of %d", ErrPermutationIndex, original, len(permutation.values))
		}
		if inverse.values[original] != ^uint32(0) {
			return nil, fmt.Errorf("%w: duplicate value %d", ErrPermutationIndex, original)
		}
		inverse.values[original] = uint32(position)
	}
	return inverse, nil
}

// Clone returns an independent copy.
func (permutation *Permutation) Clone(maxBytes uint64) (*Permutation, error) {
	if permutation == nil || len(permutation.values) == 0 {
		return nil, fmt.Errorf("%w: nil or empty", ErrInvalidPermutationSize)
	}
	clone, err := NewIdentityPermutation(uint32(len(permutation.values)), maxBytes)
	if err != nil {
		return nil, err
	}
	copy(clone.values, permutation.values)
	return clone, nil
}
