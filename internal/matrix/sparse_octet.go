package matrix

import (
	"fmt"
	"sort"

	"github.com/fgn/raptorgo/internal/gf256"
)

// SparseOctet is a bounded sparse GF(256) matrix. Each row is stored as a
// strictly increasing CSR segment with a parallel coefficient segment. Entry
// capacity and one column-wide operation scratch row are allocated up front,
// so coefficient and row/column operations allocate no additional backing.
//
// SparseOctet is not safe for concurrent mutation.
type SparseOctet struct {
	rows          uint32
	columns       uint32
	maxNonzeros   uint64
	nonzeros      uint64
	offsets       []uint64
	entryColumns  []uint32
	entryValues   []byte
	scratchColumn []uint32
	scratchValue  []byte
}

// SparseOctetBackingBytes returns the logical bytes retained by the CSR
// offsets, fixed column/coefficient capacity, and column-wide scratch row. It
// excludes Go object headers and allocator rounding.
func SparseOctetBackingBytes(rows, columns uint32, maxNonzeros uint64) (uint64, error) {
	if rows == 0 || columns == 0 {
		return 0, fmt.Errorf("%w: %dx%d", ErrInvalidDimensions, rows, columns)
	}
	maximumPossible := uint64(rows) * uint64(columns)
	if maxNonzeros > maximumPossible {
		return 0, fmt.Errorf("%w: %d nonzeros exceed %dx%d", ErrInvalidSparseArgument, maxNonzeros, rows, columns)
	}

	offsetBytes := (uint64(rows) + 1) * 8
	entryColumnBytes, overflow := multiplyBytes(maxNonzeros, 4)
	if overflow {
		return 0, fmt.Errorf("%w: sparse column size overflow", ErrBackingLimitExceeded)
	}
	entryValueBytes := maxNonzeros
	scratchColumnBytes := uint64(columns) * 4
	scratchValueBytes := uint64(columns)
	total, overflow := sumBytes(offsetBytes, entryColumnBytes, entryValueBytes, scratchColumnBytes, scratchValueBytes)
	if overflow {
		return 0, fmt.Errorf("%w: sparse octet size overflow", ErrBackingLimitExceeded)
	}
	maxInt := uint64(^uint(0) >> 1)
	for _, bytes := range []uint64{offsetBytes, entryColumnBytes, entryValueBytes, scratchColumnBytes, scratchValueBytes, total} {
		if bytes > maxInt {
			return 0, fmt.Errorf("%w: sparse octet backing exceeds platform limit", ErrBackingLimitExceeded)
		}
	}
	return total, nil
}

// NewSparseOctet constructs an empty matrix after preflighting every retained
// allocation against maxBytes and the platform slice-size limit.
func NewSparseOctet(rows, columns uint32, maxNonzeros, maxBytes uint64) (*SparseOctet, error) {
	required, err := SparseOctetBackingBytes(rows, columns, maxNonzeros)
	if err != nil {
		return nil, err
	}
	if required > maxBytes {
		return nil, fmt.Errorf("%w: need %d bytes (limit %d)", ErrBackingLimitExceeded, required, maxBytes)
	}
	return &SparseOctet{
		rows:          rows,
		columns:       columns,
		maxNonzeros:   maxNonzeros,
		offsets:       make([]uint64, int(uint64(rows)+1)),
		entryColumns:  make([]uint32, int(maxNonzeros)),
		entryValues:   make([]byte, int(maxNonzeros)),
		scratchColumn: make([]uint32, int(columns)),
		scratchValue:  make([]byte, int(columns)),
	}, nil
}

func (matrix *SparseOctet) Rows() uint32        { return matrix.rows }
func (matrix *SparseOctet) Columns() uint32     { return matrix.columns }
func (matrix *SparseOctet) Nonzeros() uint64    { return matrix.nonzeros }
func (matrix *SparseOctet) MaxNonzeros() uint64 { return matrix.maxNonzeros }
func (matrix *SparseOctet) BackingBytes() uint64 {
	bytes, _ := SparseOctetBackingBytes(matrix.rows, matrix.columns, matrix.maxNonzeros)
	return bytes
}

// At returns one coefficient.
func (matrix *SparseOctet) At(row, column uint32) (byte, error) {
	if err := matrix.validateOctetIndex(row, column); err != nil {
		return 0, err
	}
	index, found := matrix.findOctet(row, column)
	if !found {
		return 0, nil
	}
	return matrix.entryValues[index], nil
}

// Set assigns one coefficient. Zero coefficients are not stored. Capacity and
// index rejections leave the matrix unchanged.
func (matrix *SparseOctet) Set(row, column uint32, value byte) error {
	if err := matrix.validateOctetIndex(row, column); err != nil {
		return err
	}
	index, found := matrix.findOctet(row, column)
	if found {
		if value != 0 {
			matrix.entryValues[index] = value
			return nil
		}
		copy(matrix.entryColumns[index:int(matrix.nonzeros)-1], matrix.entryColumns[index+1:int(matrix.nonzeros)])
		copy(matrix.entryValues[index:int(matrix.nonzeros)-1], matrix.entryValues[index+1:int(matrix.nonzeros)])
		matrix.nonzeros--
		matrix.entryColumns[matrix.nonzeros] = 0
		matrix.entryValues[matrix.nonzeros] = 0
		matrix.adjustOctetOffsetsAfter(row, -1)
		return nil
	}
	if value == 0 {
		return nil
	}
	if matrix.nonzeros == matrix.maxNonzeros {
		return fmt.Errorf("%w: capacity %d", ErrNonzeroLimitExceeded, matrix.maxNonzeros)
	}
	copy(matrix.entryColumns[index+1:int(matrix.nonzeros)+1], matrix.entryColumns[index:int(matrix.nonzeros)])
	copy(matrix.entryValues[index+1:int(matrix.nonzeros)+1], matrix.entryValues[index:int(matrix.nonzeros)])
	matrix.entryColumns[index] = column
	matrix.entryValues[index] = value
	matrix.nonzeros++
	matrix.adjustOctetOffsetsAfter(row, 1)
	return nil
}

// RowWeight returns the stored nonzero count in one row.
func (matrix *SparseOctet) RowWeight(row uint32) (uint32, error) {
	if row >= matrix.rows {
		return 0, fmt.Errorf("%w: row %d of %d", ErrIndex, row, matrix.rows)
	}
	return uint32(matrix.offsets[row+1] - matrix.offsets[row]), nil
}

// ForEachInRow visits nonzero coefficients in increasing column order. A
// visitor must not mutate matrix because mutation invalidates the live cursor.
func (matrix *SparseOctet) ForEachInRow(row uint32, visit func(column uint32, value byte) bool) error {
	if row >= matrix.rows {
		return fmt.Errorf("%w: row %d of %d", ErrIndex, row, matrix.rows)
	}
	if visit == nil {
		return fmt.Errorf("%w: nil row visitor", ErrInvalidSparseArgument)
	}
	for index := matrix.offsets[row]; index < matrix.offsets[row+1]; index++ {
		if !visit(matrix.entryColumns[index], matrix.entryValues[index]) {
			break
		}
	}
	return nil
}

// ScaleRow multiplies one row by a nonzero coefficient. A zero scale is
// rejected because it would not preserve equation equivalence.
func (matrix *SparseOctet) ScaleRow(row uint32, coefficient byte) error {
	if row >= matrix.rows {
		return fmt.Errorf("%w: row %d of %d", ErrIndex, row, matrix.rows)
	}
	if coefficient == 0 {
		return ErrNonInvertibleRowOp
	}
	if coefficient == 1 {
		return nil
	}
	for index := matrix.offsets[row]; index < matrix.offsets[row+1]; index++ {
		matrix.entryValues[index] = gf256.Mul(matrix.entryValues[index], coefficient)
	}
	return nil
}

// AddScaledRow adds coefficient*source to destination. Its merge is computed
// completely in the preallocated scratch row before capacity is checked, so a
// rejected fill-in leaves the matrix unchanged.
func (matrix *SparseOctet) AddScaledRow(destination, source uint32, coefficient byte) error {
	if destination >= matrix.rows || source >= matrix.rows {
		return fmt.Errorf("%w: rows (%d,%d) of %d", ErrIndex, destination, source, matrix.rows)
	}
	if destination == source {
		return ErrNonInvertibleRowOp
	}
	if coefficient == 0 {
		return nil
	}
	destinationStart, destinationEnd := matrix.offsets[destination], matrix.offsets[destination+1]
	sourceStart, sourceEnd := matrix.offsets[source], matrix.offsets[source+1]
	resultLength := matrix.mergeScaled(destinationStart, destinationEnd, sourceStart, sourceEnd, coefficient)
	newNonzeros := matrix.nonzeros - (destinationEnd - destinationStart) + uint64(resultLength)
	if newNonzeros > matrix.maxNonzeros {
		return fmt.Errorf("%w: result needs %d entries (capacity %d)", ErrNonzeroLimitExceeded, newNonzeros, matrix.maxNonzeros)
	}

	oldLength := int(destinationEnd - destinationStart)
	delta := resultLength - oldLength
	start := int(destinationStart)
	end := int(destinationEnd)
	if delta > 0 {
		copy(matrix.entryColumns[end+delta:int(matrix.nonzeros)+delta], matrix.entryColumns[end:int(matrix.nonzeros)])
		copy(matrix.entryValues[end+delta:int(matrix.nonzeros)+delta], matrix.entryValues[end:int(matrix.nonzeros)])
	} else if delta < 0 {
		copy(matrix.entryColumns[end+delta:int(matrix.nonzeros)+delta], matrix.entryColumns[end:int(matrix.nonzeros)])
		copy(matrix.entryValues[end+delta:int(matrix.nonzeros)+delta], matrix.entryValues[end:int(matrix.nonzeros)])
		clear(matrix.entryColumns[int(newNonzeros):int(matrix.nonzeros)])
		clear(matrix.entryValues[int(newNonzeros):int(matrix.nonzeros)])
	}
	copy(matrix.entryColumns[start:start+resultLength], matrix.scratchColumn[:resultLength])
	copy(matrix.entryValues[start:start+resultLength], matrix.scratchValue[:resultLength])
	matrix.nonzeros = newNonzeros
	matrix.adjustOctetOffsetsAfter(destination, int64(delta))
	return nil
}

// SwapRows physically exchanges two rows without allocating.
func (matrix *SparseOctet) SwapRows(left, right uint32) error {
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
	reverseOctetRow(matrix.entryColumns, matrix.entryValues, leftStart, leftEnd)
	reverseOctetRow(matrix.entryColumns, matrix.entryValues, leftEnd, rightStart)
	reverseOctetRow(matrix.entryColumns, matrix.entryValues, rightStart, rightEnd)
	reverseOctetRow(matrix.entryColumns, matrix.entryValues, leftStart, rightEnd)

	delta := int64(rightLength) - int64(leftLength)
	matrix.offsets[left+1] = leftStart + rightLength
	for row := left + 2; row <= right; row++ {
		matrix.offsets[row] = addSigned(matrix.offsets[row], delta)
	}
	return nil
}

// SwapColumns physically exchanges two columns and restores each row's sorted
// CSR order without allocating.
func (matrix *SparseOctet) SwapColumns(left, right uint32) error {
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
			switch matrix.entryColumns[index] {
			case left:
				matrix.entryColumns[index] = right
				changed = true
			case right:
				matrix.entryColumns[index] = left
				changed = true
			}
		}
		if changed {
			matrix.sortOctetRow(start, end)
		}
	}
	return nil
}

// Clone returns an independent matrix retaining the same fixed capacity.
func (matrix *SparseOctet) Clone(maxBytes uint64) (*SparseOctet, error) {
	if matrix == nil {
		return nil, fmt.Errorf("%w: nil matrix", ErrInvalidSparseArgument)
	}
	clone, err := NewSparseOctet(matrix.rows, matrix.columns, matrix.maxNonzeros, maxBytes)
	if err != nil {
		return nil, err
	}
	clone.nonzeros = matrix.nonzeros
	copy(clone.offsets, matrix.offsets)
	copy(clone.entryColumns[:matrix.nonzeros], matrix.entryColumns[:matrix.nonzeros])
	copy(clone.entryValues[:matrix.nonzeros], matrix.entryValues[:matrix.nonzeros])
	return clone, nil
}

func (matrix *SparseOctet) validateOctetIndex(row, column uint32) error {
	if row >= matrix.rows || column >= matrix.columns {
		return fmt.Errorf("%w: (%d,%d) in %dx%d", ErrIndex, row, column, matrix.rows, matrix.columns)
	}
	return nil
}

func (matrix *SparseOctet) findOctet(row, column uint32) (int, bool) {
	start, end := matrix.offsets[row], matrix.offsets[row+1]
	relative := sort.Search(int(end-start), func(index int) bool {
		return matrix.entryColumns[start+uint64(index)] >= column
	})
	absolute := int(start) + relative
	return absolute, absolute < int(end) && matrix.entryColumns[absolute] == column
}

func (matrix *SparseOctet) adjustOctetOffsetsAfter(row uint32, delta int64) {
	for index := row + 1; index <= matrix.rows; index++ {
		matrix.offsets[index] = addSigned(matrix.offsets[index], delta)
	}
}

func (matrix *SparseOctet) mergeScaled(destinationStart, destinationEnd, sourceStart, sourceEnd uint64, coefficient byte) int {
	destination, source, output := destinationStart, sourceStart, 0
	for destination < destinationEnd && source < sourceEnd {
		destinationColumn, sourceColumn := matrix.entryColumns[destination], matrix.entryColumns[source]
		switch {
		case destinationColumn < sourceColumn:
			matrix.scratchColumn[output] = destinationColumn
			matrix.scratchValue[output] = matrix.entryValues[destination]
			destination++
			output++
		case destinationColumn > sourceColumn:
			matrix.scratchColumn[output] = sourceColumn
			matrix.scratchValue[output] = gf256.Mul(coefficient, matrix.entryValues[source])
			source++
			output++
		default:
			value := matrix.entryValues[destination] ^ gf256.Mul(coefficient, matrix.entryValues[source])
			destination++
			source++
			if value != 0 {
				matrix.scratchColumn[output] = destinationColumn
				matrix.scratchValue[output] = value
				output++
			}
		}
	}
	for ; destination < destinationEnd; destination++ {
		matrix.scratchColumn[output] = matrix.entryColumns[destination]
		matrix.scratchValue[output] = matrix.entryValues[destination]
		output++
	}
	for ; source < sourceEnd; source++ {
		matrix.scratchColumn[output] = matrix.entryColumns[source]
		matrix.scratchValue[output] = gf256.Mul(coefficient, matrix.entryValues[source])
		output++
	}
	return output
}

func (matrix *SparseOctet) sortOctetRow(start, end uint64) {
	for index := start + 1; index < end; index++ {
		column, value := matrix.entryColumns[index], matrix.entryValues[index]
		position := index
		for position > start && matrix.entryColumns[position-1] > column {
			matrix.entryColumns[position] = matrix.entryColumns[position-1]
			matrix.entryValues[position] = matrix.entryValues[position-1]
			position--
		}
		matrix.entryColumns[position] = column
		matrix.entryValues[position] = value
	}
}

func reverseOctetRow(columns []uint32, values []byte, start, end uint64) {
	if end <= start+1 {
		return
	}
	for left, right := start, end-1; left < right; left, right = left+1, right-1 {
		columns[left], columns[right] = columns[right], columns[left]
		values[left], values[right] = values[right], values[left]
	}
}

func multiplyBytes(left, right uint64) (uint64, bool) {
	if left != 0 && right > ^uint64(0)/left {
		return 0, true
	}
	return left * right, false
}

func sumBytes(values ...uint64) (uint64, bool) {
	var total uint64
	for _, value := range values {
		if total > ^uint64(0)-value {
			return 0, true
		}
		total += value
	}
	return total, false
}
