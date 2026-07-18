package matrix

import (
	"fmt"
	"math"
	"math/bits"
	"sort"
)

// HybridBinary stores an immutable sparse binary prefix and a mutable dense
// trailing window. Logical row and sparse-column swaps update permutations,
// while freezing the next prefix column materializes it into the preallocated
// dense window. This matches the monotone V-to-U transition used by RFC 6330
// inactivation decoding.
//
// Sparse prefix coefficients must not be changed after Finalize. Dense row
// additions and row/column permutations allocate no backing. HybridBinary is
// not safe for concurrent mutation.
type HybridBinary struct {
	rows               uint32
	columns            uint32
	initialDense       uint32
	maxDense           uint32
	denseColumns       uint32
	denseWords         uint32
	nonzeros           uint32
	rowOffsets         []uint32
	sparseEntries      []uint16
	columnOffsets      []uint32
	columnRows         []uint32
	logicalToPhysicalR []uint32
	physicalToLogicalR []uint32
	logicalToPhysicalC []uint16
	physicalToLogicalC []uint16
	dense              []uint64
	nextLoadRow        uint32
	finalized          bool
}

// HybridBinaryBackingBytes returns retained logical backing for CSR rows, an
// immutable sparse-column index, row/column permutations, and the fixed dense
// trailing window. It excludes caller-owned rowCounts, object headers, and
// allocator rounding.
func HybridBinaryBackingBytes(rows, columns, initialDense, maxDense uint32, nonzeros uint64) (uint64, error) {
	if rows == 0 || columns == 0 || initialDense > maxDense || maxDense > columns || columns > math.MaxUint16 {
		return 0, fmt.Errorf(
			"%w: rows=%d columns=%d initial-dense=%d max-dense=%d",
			ErrInvalidDimensions, rows, columns, initialDense, maxDense,
		)
	}
	if nonzeros > math.MaxUint32 || nonzeros > uint64(rows)*uint64(columns-initialDense) {
		return 0, fmt.Errorf("%w: nonzeros=%d", ErrInvalidSparseArgument, nonzeros)
	}
	denseWords := (uint64(maxDense) + 63) / 64
	parts := [...]uint64{
		(uint64(rows) + 1) * 4,                 // row offsets
		nonzeros * 2,                           // sparse u16 columns
		(uint64(columns-initialDense) + 1) * 4, // column offsets
		nonzeros * 4,                           // column row index
		uint64(rows) * 8,                       // forward/inverse row maps
		uint64(columns) * 4,                    // forward/inverse column maps
		uint64(rows) * denseWords * 8,          // trailing bit rows
	}
	var total uint64
	for _, part := range parts {
		if total > math.MaxUint64-part {
			return 0, fmt.Errorf("%w: hybrid backing overflow", ErrBackingLimitExceeded)
		}
		total += part
		if part > uint64(^uint(0)>>1) || total > uint64(^uint(0)>>1) {
			return 0, fmt.Errorf("%w: hybrid backing exceeds platform limit", ErrBackingLimitExceeded)
		}
	}
	return total, nil
}

// NewHybridBinary preallocates a matrix from exact sparse counts for each
// physical row. Rows must subsequently be loaded once in increasing order and
// Finalize must be called before the matrix is queried or mutated.
func NewHybridBinary(
	rowCounts []uint32,
	columns, initialDense, maxDense uint32,
	maxBytes uint64,
) (*HybridBinary, error) {
	return newHybridBinary(uint32(len(rowCounts)), func(row uint32) uint32 {
		return rowCounts[row]
	}, columns, initialDense, maxDense, maxBytes)
}

// NewHybridBinary16 is NewHybridBinary for exact uint16 row counts. It avoids
// allocating a converted count slice when an RFC matrix already stores bounded
// row degrees in that form.
func NewHybridBinary16(
	rowCounts []uint16,
	columns, initialDense, maxDense uint32,
	maxBytes uint64,
) (*HybridBinary, error) {
	return newHybridBinary(uint32(len(rowCounts)), func(row uint32) uint32 {
		return uint32(rowCounts[row])
	}, columns, initialDense, maxDense, maxBytes)
}

func newHybridBinary(
	rows uint32,
	countAt func(uint32) uint32,
	columns, initialDense, maxDense uint32,
	maxBytes uint64,
) (*HybridBinary, error) {
	var nonzeros uint64
	for row := uint32(0); row < rows; row++ {
		count := countAt(row)
		if count > columns-initialDense {
			return nil, fmt.Errorf("%w: row %d count %d", ErrInvalidSparseArgument, row, count)
		}
		nonzeros += uint64(count)
	}
	required, err := HybridBinaryBackingBytes(rows, columns, initialDense, maxDense, nonzeros)
	if err != nil {
		return nil, err
	}
	if required > maxBytes {
		return nil, fmt.Errorf("%w: need %d bytes (limit %d)", ErrBackingLimitExceeded, required, maxBytes)
	}
	denseWords := (maxDense + 63) / 64
	matrix := &HybridBinary{
		rows:               rows,
		columns:            columns,
		initialDense:       initialDense,
		maxDense:           maxDense,
		denseColumns:       initialDense,
		denseWords:         denseWords,
		nonzeros:           uint32(nonzeros),
		rowOffsets:         make([]uint32, int(rows)+1),
		sparseEntries:      make([]uint16, int(nonzeros)),
		columnOffsets:      make([]uint32, int(columns-initialDense)+1),
		columnRows:         make([]uint32, int(nonzeros)),
		logicalToPhysicalR: make([]uint32, int(rows)),
		physicalToLogicalR: make([]uint32, int(rows)),
		logicalToPhysicalC: make([]uint16, int(columns)),
		physicalToLogicalC: make([]uint16, int(columns)),
		dense:              make([]uint64, int(uint64(rows)*uint64(denseWords))),
	}
	for row := uint32(0); row < rows; row++ {
		count := countAt(row)
		matrix.rowOffsets[row+1] = matrix.rowOffsets[row] + count
		matrix.logicalToPhysicalR[row] = row
		matrix.physicalToLogicalR[row] = row
	}
	for column := uint32(0); column < columns; column++ {
		matrix.logicalToPhysicalC[column] = uint16(column)
		matrix.physicalToLogicalC[column] = uint16(column)
	}
	return matrix, nil
}

func (matrix *HybridBinary) Rows() uint32           { return matrix.rows }
func (matrix *HybridBinary) Columns() uint32        { return matrix.columns }
func (matrix *HybridBinary) InitialDense() uint32   { return matrix.initialDense }
func (matrix *HybridBinary) MaxDense() uint32       { return matrix.maxDense }
func (matrix *HybridBinary) DenseColumns() uint32   { return matrix.denseColumns }
func (matrix *HybridBinary) SparseNonzeros() uint32 { return matrix.nonzeros }

func (matrix *HybridBinary) BackingBytes() uint64 {
	bytes, _ := HybridBinaryBackingBytes(
		matrix.rows, matrix.columns, matrix.initialDense, matrix.maxDense, uint64(matrix.nonzeros),
	)
	return bytes
}

// LoadSparseRow loads the next physical row. Columns must be unique, strictly
// increasing, and in the initial sparse prefix.
func (matrix *HybridBinary) LoadSparseRow(row uint32, columns []uint16) error {
	if matrix == nil {
		return fmt.Errorf("%w: load row %d on nil matrix", ErrInvalidSparseArgument, row)
	}
	if matrix.finalized || row != matrix.nextLoadRow || row >= matrix.rows {
		return fmt.Errorf("%w: load row %d next=%d", ErrInvalidSparseArgument, row, matrix.nextLoadRow)
	}
	start, end := matrix.rowOffsets[row], matrix.rowOffsets[row+1]
	if uint32(len(columns)) != end-start {
		return fmt.Errorf("%w: row %d has %d columns, expected %d", ErrInvalidSparseArgument, row, len(columns), end-start)
	}
	sparseColumns := matrix.columns - matrix.initialDense
	for index, column := range columns {
		if uint32(column) >= sparseColumns || index > 0 && columns[index-1] >= column {
			return fmt.Errorf("%w: row %d column sequence at %d", ErrInvalidSparseArgument, row, index)
		}
	}
	copy(matrix.sparseEntries[start:end], columns)
	matrix.nextLoadRow++
	return nil
}

// SetInitialDense assigns an initial trailing coefficient before or after
// Finalize. Only columns in the initial dense suffix are accepted.
func (matrix *HybridBinary) SetInitialDense(row, column uint32, value bool) error {
	if matrix == nil || row >= matrix.rows || column < matrix.columns-matrix.initialDense || column >= matrix.columns {
		return fmt.Errorf("%w: initial dense (%d,%d)", ErrIndex, row, column)
	}
	physicalRow := row
	if matrix.finalized {
		physicalRow = matrix.logicalToPhysicalR[row]
	}
	matrix.setDensePhysical(physicalRow, column, value)
	return nil
}

// Finalize builds the immutable physical-column index without extra scratch.
func (matrix *HybridBinary) Finalize() error {
	if matrix == nil {
		return fmt.Errorf("%w: finalize nil matrix", ErrInvalidSparseArgument)
	}
	if matrix.finalized || matrix.nextLoadRow != matrix.rows {
		return fmt.Errorf("%w: finalize loaded=%d rows=%d", ErrInvalidSparseArgument, matrix.nextLoadRow, matrix.rows)
	}
	for _, column := range matrix.sparseEntries {
		matrix.columnOffsets[uint32(column)+1]++
	}
	for column := 1; column < len(matrix.columnOffsets); column++ {
		matrix.columnOffsets[column] += matrix.columnOffsets[column-1]
	}
	for row := uint32(0); row < matrix.rows; row++ {
		for index := matrix.rowOffsets[row]; index < matrix.rowOffsets[row+1]; index++ {
			column := uint32(matrix.sparseEntries[index])
			matrix.columnOffsets[column+1]--
			matrix.columnRows[matrix.columnOffsets[column+1]] = row
		}
	}
	for column := 0; column < len(matrix.columnOffsets)-1; column++ {
		matrix.columnOffsets[column] = matrix.columnOffsets[column+1]
	}
	matrix.columnOffsets[len(matrix.columnOffsets)-1] = matrix.nonzeros
	matrix.finalized = true
	return nil
}

// Has returns the current logical binary coefficient. Sparse coefficients are
// immutable; dense coefficients include all row additions performed so far.
func (matrix *HybridBinary) Has(row, column uint32) (bool, error) {
	if err := matrix.validateReadyIndex(row, column); err != nil {
		return false, err
	}
	if column >= matrix.columns-matrix.denseColumns {
		return matrix.densePhysical(matrix.logicalToPhysicalR[row], column), nil
	}
	physicalRow := matrix.logicalToPhysicalR[row]
	physicalColumn := matrix.logicalToPhysicalC[column]
	start, end := matrix.rowOffsets[physicalRow], matrix.rowOffsets[physicalRow+1]
	index := sort.Search(int(end-start), func(index int) bool {
		return matrix.sparseEntries[start+uint32(index)] >= physicalColumn
	})
	return index < int(end-start) && matrix.sparseEntries[start+uint32(index)] == physicalColumn, nil
}

// VColumns appends the nonzero logical columns in [start,end) to output and
// returns the extended slice. The range must lie wholly in the active sparse
// prefix.
func (matrix *HybridBinary) VColumns(row, start, end uint32, output []uint32) ([]uint32, error) {
	if !matrix.ready() || row >= matrix.rows || start > end || end > matrix.columns-matrix.denseColumns {
		return nil, fmt.Errorf("%w: row=%d range=[%d,%d)", ErrIndex, row, start, end)
	}
	physicalRow := matrix.logicalToPhysicalR[row]
	for index := matrix.rowOffsets[physicalRow]; index < matrix.rowOffsets[physicalRow+1]; index++ {
		logical := uint32(matrix.physicalToLogicalC[matrix.sparseEntries[index]])
		if logical >= start && logical < end {
			output = append(output, logical)
		}
	}
	return output, nil
}

func (matrix *HybridBinary) LogicalRow(physical uint32) (uint32, error) {
	if !matrix.ready() || physical >= matrix.rows {
		return 0, fmt.Errorf("%w: physical row %d", ErrIndex, physical)
	}
	return matrix.physicalToLogicalR[physical], nil
}

func (matrix *HybridBinary) PhysicalRow(logical uint32) (uint32, error) {
	if !matrix.ready() || logical >= matrix.rows {
		return 0, fmt.Errorf("%w: logical row %d", ErrIndex, logical)
	}
	return matrix.logicalToPhysicalR[logical], nil
}

func (matrix *HybridBinary) PhysicalColumn(logical uint32) (uint32, error) {
	if !matrix.ready() || logical >= matrix.columns {
		return 0, fmt.Errorf("%w: logical column %d", ErrIndex, logical)
	}
	return uint32(matrix.logicalToPhysicalC[logical]), nil
}

func (matrix *HybridBinary) LogicalColumn(physical uint32) (uint32, error) {
	if !matrix.ready() || physical >= matrix.columns {
		return 0, fmt.Errorf("%w: physical column %d", ErrIndex, physical)
	}
	return uint32(matrix.physicalToLogicalC[physical]), nil
}

func (matrix *HybridBinary) OriginalColumn(logical uint32) (uint32, error) {
	return matrix.PhysicalColumn(logical)
}

// SwapRows exchanges two logical row identities in O(1).
func (matrix *HybridBinary) SwapRows(left, right uint32) error {
	if !matrix.ready() || left >= matrix.rows || right >= matrix.rows {
		return fmt.Errorf("%w: rows (%d,%d)", ErrIndex, left, right)
	}
	if left == right {
		return nil
	}
	leftPhysical, rightPhysical := matrix.logicalToPhysicalR[left], matrix.logicalToPhysicalR[right]
	matrix.logicalToPhysicalR[left], matrix.logicalToPhysicalR[right] = rightPhysical, leftPhysical
	matrix.physicalToLogicalR[leftPhysical], matrix.physicalToLogicalR[rightPhysical] = right, left
	return nil
}

// SwapSparseColumns exchanges two logical columns in the active sparse V
// prefix in O(1).
func (matrix *HybridBinary) SwapSparseColumns(left, right uint32) error {
	if !matrix.ready() {
		return fmt.Errorf("%w: active sparse columns (%d,%d) on unready matrix", ErrIndex, left, right)
	}
	activeEnd := matrix.columns - matrix.denseColumns
	if left >= activeEnd || right >= activeEnd {
		return fmt.Errorf("%w: active sparse columns (%d,%d) before %d", ErrIndex, left, right, activeEnd)
	}
	if left == right {
		return nil
	}
	leftPhysical, rightPhysical := matrix.logicalToPhysicalC[left], matrix.logicalToPhysicalC[right]
	matrix.logicalToPhysicalC[left], matrix.logicalToPhysicalC[right] = rightPhysical, leftPhysical
	matrix.physicalToLogicalC[leftPhysical], matrix.physicalToLogicalC[rightPhysical] = uint16(right), uint16(left)
	return nil
}

// FreezeNext materializes the rightmost active sparse column as the new
// leftmost dense column. A dense-cap rejection is transactional.
func (matrix *HybridBinary) FreezeNext() (uint32, error) {
	if !matrix.ready() {
		return 0, fmt.Errorf("%w: matrix not finalized", ErrInvalidSparseArgument)
	}
	if matrix.denseColumns == matrix.maxDense {
		return 0, fmt.Errorf("%w: dense window capacity %d", ErrNonzeroLimitExceeded, matrix.maxDense)
	}
	logical := matrix.columns - matrix.denseColumns - 1
	physical := uint32(matrix.logicalToPhysicalC[logical])
	if physical >= matrix.columns-matrix.initialDense {
		return 0, fmt.Errorf("%w: sparse physical column %d", ErrIndex, physical)
	}
	start, end := matrix.columnOffsets[physical], matrix.columnOffsets[physical+1]
	for _, row := range matrix.columnRows[start:end] {
		matrix.setDensePhysical(row, logical, true)
	}
	matrix.denseColumns++
	return physical, nil
}

// XORDenseRows adds the active dense suffix of source to destination in GF(2).
func (matrix *HybridBinary) XORDenseRows(destination, source uint32) error {
	if !matrix.ready() || destination >= matrix.rows || source >= matrix.rows || destination == source {
		return fmt.Errorf("%w: dense rows (%d,%d)", ErrIndex, destination, source)
	}
	destinationPhysical := matrix.logicalToPhysicalR[destination]
	sourcePhysical := matrix.logicalToPhysicalR[source]
	destinationStart := uint64(destinationPhysical) * uint64(matrix.denseWords)
	sourceStart := uint64(sourcePhysical) * uint64(matrix.denseWords)
	for word := uint32(0); word < matrix.denseWords; word++ {
		matrix.dense[destinationStart+uint64(word)] ^= matrix.dense[sourceStart+uint64(word)]
	}
	return nil
}

// AppendDenseOnes appends the row's current logical one-columns to output in
// increasing order and returns the extended slice. It is the bulk form of
// ForEachDenseOne for hot paths that reuse a scratch slice instead of paying
// one callback invocation per set bit.
func (matrix *HybridBinary) AppendDenseOnes(row uint32, output []uint32) ([]uint32, error) {
	if !matrix.ready() || row >= matrix.rows {
		return nil, fmt.Errorf("%w: dense row %d", ErrInvalidSparseArgument, row)
	}
	if matrix.denseColumns == 0 {
		return output, nil
	}
	physical := matrix.logicalToPhysicalR[row]
	rowStart := uint64(physical) * uint64(matrix.denseWords)
	firstSlot := matrix.maxDense - matrix.denseColumns
	lastSlot := matrix.maxDense
	for wordIndex := firstSlot / 64; wordIndex <= (lastSlot-1)/64; wordIndex++ {
		word := matrix.dense[rowStart+uint64(wordIndex)]
		if wordIndex == firstSlot/64 {
			word &= math.MaxUint64 << (firstSlot % 64)
		}
		if wordIndex == (lastSlot-1)/64 && lastSlot%64 != 0 {
			word &= (uint64(1) << (lastSlot % 64)) - 1
		}
		for word != 0 {
			slot := wordIndex*64 + uint32(bits.TrailingZeros64(word))
			output = append(output, matrix.columns-(matrix.maxDense-slot))
			word &= word - 1
		}
	}
	return output, nil
}

// ForEachDenseOne visits current logical one-columns in increasing order.
func (matrix *HybridBinary) ForEachDenseOne(row uint32, visit func(column uint32) bool) error {
	if !matrix.ready() || row >= matrix.rows || visit == nil {
		return fmt.Errorf("%w: dense row %d", ErrInvalidSparseArgument, row)
	}
	if matrix.denseColumns == 0 {
		return nil
	}
	physical := matrix.logicalToPhysicalR[row]
	firstSlot := matrix.maxDense - matrix.denseColumns
	lastSlot := matrix.maxDense
	for wordIndex := firstSlot / 64; wordIndex <= (lastSlot-1)/64; wordIndex++ {
		word := matrix.dense[uint64(physical)*uint64(matrix.denseWords)+uint64(wordIndex)]
		if wordIndex == firstSlot/64 {
			word &= math.MaxUint64 << (firstSlot % 64)
		}
		if wordIndex == (lastSlot-1)/64 && lastSlot%64 != 0 {
			word &= (uint64(1) << (lastSlot % 64)) - 1
		}
		for word != 0 {
			bit := uint32(bits.TrailingZeros64(word))
			slot := wordIndex*64 + bit
			column := matrix.columns - (matrix.maxDense - slot)
			if !visit(column) {
				return nil
			}
			word &= word - 1
		}
	}
	return nil
}

func (matrix *HybridBinary) validateReadyIndex(row, column uint32) error {
	if !matrix.ready() || row >= matrix.rows || column >= matrix.columns {
		return fmt.Errorf("%w: (%d,%d)", ErrIndex, row, column)
	}
	return nil
}

func (matrix *HybridBinary) ready() bool { return matrix != nil && matrix.finalized }

func (matrix *HybridBinary) densePosition(physicalRow, logicalColumn uint32) (uint64, uint32) {
	distance := matrix.columns - 1 - logicalColumn
	slot := matrix.maxDense - 1 - distance
	return uint64(physicalRow)*uint64(matrix.denseWords) + uint64(slot/64), slot % 64
}

func (matrix *HybridBinary) setDensePhysical(physicalRow, logicalColumn uint32, value bool) {
	word, bit := matrix.densePosition(physicalRow, logicalColumn)
	if value {
		matrix.dense[word] |= uint64(1) << bit
	} else {
		matrix.dense[word] &^= uint64(1) << bit
	}
}

func (matrix *HybridBinary) densePhysical(physicalRow, logicalColumn uint32) bool {
	word, bit := matrix.densePosition(physicalRow, logicalColumn)
	return matrix.dense[word]&(uint64(1)<<bit) != 0
}
