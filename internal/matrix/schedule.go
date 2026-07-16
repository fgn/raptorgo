package matrix

import "fmt"

// SparseSchedule owns a sparse binary matrix together with the RFC row and
// column mappings. Physical swaps and their mapping updates occur through this
// owner as one sequential logical transaction. SparseSchedule is not safe for
// concurrent access when any goroutine may mutate it.
type SparseSchedule struct {
	matrix        *SparseBinary
	rowMapping    *Permutation
	columnMapping *Permutation
	backingBytes  uint64
}

// SparseScheduleBackingBytes returns the aggregate logical backing bytes for
// the sparse matrix, its operation scratch, and both permutations. It excludes
// Go object headers and allocator rounding.
func SparseScheduleBackingBytes(rows, columns uint32, maxNonzeros uint64) (uint64, error) {
	matrixBytes, err := SparseBinaryBackingBytes(rows, columns, maxNonzeros)
	if err != nil {
		return 0, err
	}
	rowBytes := uint64(rows) * 4
	columnBytes := uint64(columns) * 4
	if matrixBytes > ^uint64(0)-rowBytes || matrixBytes+rowBytes > ^uint64(0)-columnBytes {
		return 0, fmt.Errorf("%w: schedule backing overflow", ErrBackingLimitExceeded)
	}
	total := matrixBytes + rowBytes + columnBytes
	if total > uint64(^uint(0)>>1) {
		return 0, fmt.Errorf("%w: schedule backing exceeds platform limit", ErrBackingLimitExceeded)
	}
	return total, nil
}

// NewSparseSchedule allocates only after the aggregate backing requirement has
// been checked against maxBytes.
func NewSparseSchedule(rows, columns uint32, maxNonzeros, maxBytes uint64) (*SparseSchedule, error) {
	required, err := SparseScheduleBackingBytes(rows, columns, maxNonzeros)
	if err != nil {
		return nil, err
	}
	if required > maxBytes {
		return nil, fmt.Errorf("%w: schedule needs %d bytes (limit %d)", ErrBackingLimitExceeded, required, maxBytes)
	}
	matrixBytes, err := SparseBinaryBackingBytes(rows, columns, maxNonzeros)
	if err != nil {
		return nil, err
	}
	matrix, err := NewSparseBinary(rows, columns, maxNonzeros, matrixBytes)
	if err != nil {
		return nil, err
	}
	rowMapping, err := NewIdentityPermutation(rows, uint64(rows)*4)
	if err != nil {
		return nil, err
	}
	columnMapping, err := NewIdentityPermutation(columns, uint64(columns)*4)
	if err != nil {
		return nil, err
	}
	return &SparseSchedule{
		matrix:        matrix,
		rowMapping:    rowMapping,
		columnMapping: columnMapping,
		backingBytes:  required,
	}, nil
}

func (schedule *SparseSchedule) Rows() uint32         { return schedule.matrix.Rows() }
func (schedule *SparseSchedule) Columns() uint32      { return schedule.matrix.Columns() }
func (schedule *SparseSchedule) Nonzeros() uint64     { return schedule.matrix.Nonzeros() }
func (schedule *SparseSchedule) MaxNonzeros() uint64  { return schedule.matrix.MaxNonzeros() }
func (schedule *SparseSchedule) BackingBytes() uint64 { return schedule.backingBytes }

func (schedule *SparseSchedule) Has(row, column uint32) (bool, error) {
	return schedule.matrix.Has(row, column)
}

func (schedule *SparseSchedule) Set(row, column uint32, value bool) error {
	return schedule.matrix.Set(row, column, value)
}

func (schedule *SparseSchedule) RowWeight(row uint32) (uint32, error) {
	return schedule.matrix.RowWeight(row)
}

func (schedule *SparseSchedule) ForEachInRow(row uint32, visit func(column uint32) bool) error {
	return schedule.matrix.ForEachInRow(row, visit)
}

// AddRow applies a mapping-neutral GF(2) row operation.
func (schedule *SparseSchedule) AddRow(destination, source uint32) error {
	return schedule.matrix.AddRow(destination, source)
}

// SwapRows logically couples a physical row swap with the row mapping.
func (schedule *SparseSchedule) SwapRows(left, right uint32) error {
	if left >= schedule.Rows() || right >= schedule.Rows() {
		return fmt.Errorf("%w: rows (%d,%d) of %d", ErrIndex, left, right, schedule.Rows())
	}
	if err := schedule.matrix.SwapRows(left, right); err != nil {
		return err
	}
	schedule.rowMapping.values[left], schedule.rowMapping.values[right] = schedule.rowMapping.values[right], schedule.rowMapping.values[left]
	return nil
}

// SwapColumns logically couples a physical column swap with the column
// mapping.
func (schedule *SparseSchedule) SwapColumns(left, right uint32) error {
	if left >= schedule.Columns() || right >= schedule.Columns() {
		return fmt.Errorf("%w: columns (%d,%d) of %d", ErrIndex, left, right, schedule.Columns())
	}
	if err := schedule.matrix.SwapColumns(left, right); err != nil {
		return err
	}
	schedule.columnMapping.values[left], schedule.columnMapping.values[right] = schedule.columnMapping.values[right], schedule.columnMapping.values[left]
	return nil
}

// OriginalRow returns the original equation index occupying a current row.
func (schedule *SparseSchedule) OriginalRow(row uint32) (uint32, error) {
	return schedule.rowMapping.At(row)
}

// OriginalColumn returns the original intermediate-symbol index occupying a
// current column.
func (schedule *SparseSchedule) OriginalColumn(column uint32) (uint32, error) {
	return schedule.columnMapping.At(column)
}

// CurrentRow returns the current row occupied by an original equation index.
func (schedule *SparseSchedule) CurrentRow(original uint32) (uint32, error) {
	return schedule.rowMapping.Position(original)
}

// CurrentColumn returns the current column occupied by an original
// intermediate-symbol index.
func (schedule *SparseSchedule) CurrentColumn(original uint32) (uint32, error) {
	return schedule.columnMapping.Position(original)
}
