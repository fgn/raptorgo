// Package matrix implements bounded sparse matrix and permutation primitives
// used by the RFC 6330 constraint builder and inactivation schedule.
package matrix

import (
	"errors"
	"fmt"
	"slices"
	"sort"
)

var (
	ErrInvalidDimensions     = errors.New("matrix: invalid dimensions")
	ErrBackingLimitExceeded  = errors.New("matrix: backing storage limit exceeded")
	ErrIndex                 = errors.New("matrix: index out of range")
	ErrNonzeroLimitExceeded  = errors.New("matrix: nonzero limit exceeded")
	ErrNonInvertibleRowOp    = errors.New("matrix: non-invertible row operation")
	ErrInvalidSparseArgument = errors.New("matrix: invalid sparse matrix argument")
)

// SparseBinary is a bounded sparse GF(2) matrix. Each row is stored as a
// strictly increasing CSR segment. The entry and operation-scratch buffers are
// fully allocated at construction, so Set and row/column operations allocate
// no additional backing storage.
//
// SparseBinary is not safe for concurrent mutation.
type SparseBinary struct {
	rows        uint32
	columns     uint32
	maxNonzeros uint64
	nonzeros    uint64
	offsets     []uint64
	entries     []uint32
	scratch     []uint32
}

// SparseBinaryBackingBytes returns the bytes in the CSR offsets, fixed entry
// capacity, and one column-wide row-operation scratch buffer.
func SparseBinaryBackingBytes(rows, columns uint32, maxNonzeros uint64) (uint64, error) {
	if rows == 0 || columns == 0 {
		return 0, fmt.Errorf("%w: %dx%d", ErrInvalidDimensions, rows, columns)
	}
	maximumPossible := uint64(rows) * uint64(columns)
	if maxNonzeros > maximumPossible {
		return 0, fmt.Errorf("%w: %d nonzeros exceed %dx%d", ErrInvalidSparseArgument, maxNonzeros, rows, columns)
	}
	offsetBytes := (uint64(rows) + 1) * 8
	if maxNonzeros > (^uint64(0)-offsetBytes)/4 {
		return 0, fmt.Errorf("%w: sparse entry size overflow", ErrBackingLimitExceeded)
	}
	entryBytes := maxNonzeros * 4
	total := offsetBytes + entryBytes
	scratchBytes := uint64(columns) * 4
	if total > ^uint64(0)-scratchBytes {
		return 0, fmt.Errorf("%w: sparse scratch size overflow", ErrBackingLimitExceeded)
	}
	total += scratchBytes
	maxInt := uint64(^uint(0) >> 1)
	if offsetBytes > maxInt || entryBytes > maxInt || scratchBytes > maxInt || total > maxInt {
		return 0, fmt.Errorf("%w: sparse backing exceeds platform limit", ErrBackingLimitExceeded)
	}
	return total, nil
}

// NewSparseBinary constructs an empty matrix after preflighting all backing
// allocations against maxBytes and the platform slice-size limit.
func NewSparseBinary(rows, columns uint32, maxNonzeros, maxBytes uint64) (*SparseBinary, error) {
	required, err := SparseBinaryBackingBytes(rows, columns, maxNonzeros)
	if err != nil {
		return nil, err
	}
	if required > maxBytes {
		return nil, fmt.Errorf("%w: need %d bytes (limit %d)", ErrBackingLimitExceeded, required, maxBytes)
	}
	return &SparseBinary{
		rows:        rows,
		columns:     columns,
		maxNonzeros: maxNonzeros,
		offsets:     make([]uint64, int(uint64(rows)+1)),
		entries:     make([]uint32, int(maxNonzeros)),
		scratch:     make([]uint32, int(columns)),
	}, nil
}

func (matrix *SparseBinary) Rows() uint32        { return matrix.rows }
func (matrix *SparseBinary) Columns() uint32     { return matrix.columns }
func (matrix *SparseBinary) Nonzeros() uint64    { return matrix.nonzeros }
func (matrix *SparseBinary) MaxNonzeros() uint64 { return matrix.maxNonzeros }

// Has reports whether one coefficient is one.
func (matrix *SparseBinary) Has(row, column uint32) (bool, error) {
	if err := matrix.validateIndex(row, column); err != nil {
		return false, err
	}
	_, found := matrix.find(row, column)
	return found, nil
}

// Set assigns one binary coefficient. Rejections are transactional.
func (matrix *SparseBinary) Set(row, column uint32, value bool) error {
	if err := matrix.validateIndex(row, column); err != nil {
		return err
	}
	index, found := matrix.find(row, column)
	if found == value {
		return nil
	}
	if value {
		if matrix.nonzeros == matrix.maxNonzeros {
			return fmt.Errorf("%w: capacity %d", ErrNonzeroLimitExceeded, matrix.maxNonzeros)
		}
		copy(matrix.entries[index+1:int(matrix.nonzeros)+1], matrix.entries[index:int(matrix.nonzeros)])
		matrix.entries[index] = column
		matrix.nonzeros++
		matrix.adjustOffsetsAfter(row, 1)
		return nil
	}
	copy(matrix.entries[index:int(matrix.nonzeros)-1], matrix.entries[index+1:int(matrix.nonzeros)])
	matrix.nonzeros--
	matrix.entries[matrix.nonzeros] = 0
	matrix.adjustOffsetsAfter(row, -1)
	return nil
}

// RowWeight returns the number of nonzeros in one row.
func (matrix *SparseBinary) RowWeight(row uint32) (uint32, error) {
	if row >= matrix.rows {
		return 0, fmt.Errorf("%w: row %d of %d", ErrIndex, row, matrix.rows)
	}
	return uint32(matrix.offsets[row+1] - matrix.offsets[row]), nil
}

// ForEachInRow visits a row's nonzero columns in increasing order. Returning
// false stops iteration. A visitor must not mutate matrix; doing so is outside
// this method's contract because it invalidates the live CSR cursor.
func (matrix *SparseBinary) ForEachInRow(row uint32, visit func(column uint32) bool) error {
	if row >= matrix.rows {
		return fmt.Errorf("%w: row %d of %d", ErrIndex, row, matrix.rows)
	}
	if visit == nil {
		return fmt.Errorf("%w: nil row visitor", ErrInvalidSparseArgument)
	}
	for index := matrix.offsets[row]; index < matrix.offsets[row+1]; index++ {
		if !visit(matrix.entries[index]) {
			break
		}
	}
	return nil
}

// AddRow adds source to destination in GF(2). It rejects same-row addition
// because that operation destroys equation equivalence.
func (matrix *SparseBinary) AddRow(destination, source uint32) error {
	if destination >= matrix.rows || source >= matrix.rows {
		return fmt.Errorf("%w: rows (%d,%d) of %d", ErrIndex, destination, source, matrix.rows)
	}
	if destination == source {
		return ErrNonInvertibleRowOp
	}
	destinationStart, destinationEnd := matrix.offsets[destination], matrix.offsets[destination+1]
	sourceStart, sourceEnd := matrix.offsets[source], matrix.offsets[source+1]
	resultLength := matrix.symmetricDifference(destinationStart, destinationEnd, sourceStart, sourceEnd)
	newNonzeros := matrix.nonzeros - (destinationEnd - destinationStart) + uint64(resultLength)
	if newNonzeros > matrix.maxNonzeros {
		return fmt.Errorf("%w: result needs %d entries (capacity %d)", ErrNonzeroLimitExceeded, newNonzeros, matrix.maxNonzeros)
	}

	oldLength := int(destinationEnd - destinationStart)
	delta := resultLength - oldLength
	start := int(destinationStart)
	end := int(destinationEnd)
	if delta > 0 {
		copy(matrix.entries[end+delta:int(matrix.nonzeros)+delta], matrix.entries[end:int(matrix.nonzeros)])
	} else if delta < 0 {
		copy(matrix.entries[end+delta:int(matrix.nonzeros)+delta], matrix.entries[end:int(matrix.nonzeros)])
		clear(matrix.entries[int(newNonzeros):int(matrix.nonzeros)])
	}
	copy(matrix.entries[start:start+resultLength], matrix.scratch[:resultLength])
	matrix.nonzeros = newNonzeros
	matrix.adjustOffsetsAfter(destination, int64(delta))
	return nil
}

// SwapRows physically exchanges two rows without allocating.
func (matrix *SparseBinary) SwapRows(left, right uint32) error {
	if left >= matrix.rows || right >= matrix.rows {
		return fmt.Errorf("%w: rows (%d,%d) of %d", ErrIndex, left, right, matrix.rows)
	}
	if left == right {
		return nil
	}
	if left > right {
		left, right = right, left
	}
	leftStart, leftEnd := matrix.offsets[left], matrix.offsets[left+1]
	rightStart, rightEnd := matrix.offsets[right], matrix.offsets[right+1]
	leftLength := leftEnd - leftStart
	rightLength := rightEnd - rightStart
	reverseColumns(matrix.entries[leftStart:leftEnd])
	reverseColumns(matrix.entries[leftEnd:rightStart])
	reverseColumns(matrix.entries[rightStart:rightEnd])
	reverseColumns(matrix.entries[leftStart:rightEnd])

	delta := int64(rightLength) - int64(leftLength)
	matrix.offsets[left+1] = leftStart + rightLength
	for row := left + 2; row <= right; row++ {
		matrix.offsets[row] = addSigned(matrix.offsets[row], delta)
	}
	return nil
}

// SwapColumns physically exchanges two columns and restores sorted CSR rows.
func (matrix *SparseBinary) SwapColumns(left, right uint32) error {
	if left >= matrix.columns || right >= matrix.columns {
		return fmt.Errorf("%w: columns (%d,%d) of %d", ErrIndex, left, right, matrix.columns)
	}
	if left == right {
		return nil
	}
	for row := uint32(0); row < matrix.rows; row++ {
		start, end := matrix.offsets[row], matrix.offsets[row+1]
		changed := false
		for index := start; index < end; index++ {
			switch matrix.entries[index] {
			case left:
				matrix.entries[index] = right
				changed = true
			case right:
				matrix.entries[index] = left
				changed = true
			}
		}
		if changed {
			slices.Sort(matrix.entries[start:end])
		}
	}
	return nil
}

// Clone returns an independent matrix with the same fixed capacity.
func (matrix *SparseBinary) Clone(maxBytes uint64) (*SparseBinary, error) {
	if matrix == nil {
		return nil, fmt.Errorf("%w: nil matrix", ErrInvalidSparseArgument)
	}
	clone, err := NewSparseBinary(matrix.rows, matrix.columns, matrix.maxNonzeros, maxBytes)
	if err != nil {
		return nil, err
	}
	clone.nonzeros = matrix.nonzeros
	copy(clone.offsets, matrix.offsets)
	copy(clone.entries[:matrix.nonzeros], matrix.entries[:matrix.nonzeros])
	return clone, nil
}

func (matrix *SparseBinary) validateIndex(row, column uint32) error {
	if row >= matrix.rows || column >= matrix.columns {
		return fmt.Errorf("%w: (%d,%d) in %dx%d", ErrIndex, row, column, matrix.rows, matrix.columns)
	}
	return nil
}

func (matrix *SparseBinary) find(row, column uint32) (int, bool) {
	start, end := matrix.offsets[row], matrix.offsets[row+1]
	relative := sort.Search(int(end-start), func(index int) bool {
		return matrix.entries[start+uint64(index)] >= column
	})
	absolute := int(start) + relative
	return absolute, absolute < int(end) && matrix.entries[absolute] == column
}

func (matrix *SparseBinary) adjustOffsetsAfter(row uint32, delta int64) {
	for index := row + 1; index <= matrix.rows; index++ {
		matrix.offsets[index] = addSigned(matrix.offsets[index], delta)
	}
}

func (matrix *SparseBinary) symmetricDifference(leftStart, leftEnd, rightStart, rightEnd uint64) int {
	left, right, output := leftStart, rightStart, 0
	for left < leftEnd && right < rightEnd {
		leftValue, rightValue := matrix.entries[left], matrix.entries[right]
		switch {
		case leftValue < rightValue:
			matrix.scratch[output] = leftValue
			left++
			output++
		case leftValue > rightValue:
			matrix.scratch[output] = rightValue
			right++
			output++
		default:
			left++
			right++
		}
	}
	for ; left < leftEnd; left++ {
		matrix.scratch[output] = matrix.entries[left]
		output++
	}
	for ; right < rightEnd; right++ {
		matrix.scratch[output] = matrix.entries[right]
		output++
	}
	return output
}

func reverseColumns(values []uint32) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func addSigned(value uint64, delta int64) uint64 {
	if delta < 0 {
		return value - uint64(-delta)
	}
	return value + uint64(delta)
}
