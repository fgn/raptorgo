package gf256

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidMatrixDimensions   = errors.New("gf256: invalid matrix dimensions")
	ErrMatrixLimitExceeded       = errors.New("gf256: matrix allocation limit exceeded")
	ErrMatrixIndex               = errors.New("gf256: matrix index out of range")
	ErrNonInvertibleRowOperation = errors.New("gf256: row operation would not preserve the system")
)

// Matrix is a row-major dense GF(256) matrix used as the auditable reference
// representation for bounded solver and sparse-matrix tests. A Matrix is not
// safe for concurrent mutation.
type Matrix struct {
	rows    uint32
	columns uint32
	values  []byte
}

// NewMatrix allocates a matrix only after its dimensions have been checked
// against maxBytes and the platform's slice-size limit.
func NewMatrix(rows, columns uint32, maxBytes uint64) (*Matrix, error) {
	if rows == 0 || columns == 0 {
		return nil, fmt.Errorf("%w: %dx%d", ErrInvalidMatrixDimensions, rows, columns)
	}
	size := uint64(rows) * uint64(columns)
	maxInt := uint64(^uint(0) >> 1)
	if size > maxBytes || size > maxInt {
		return nil, fmt.Errorf("%w: %dx%d requires %d bytes (limit %d)", ErrMatrixLimitExceeded, rows, columns, size, maxBytes)
	}
	return &Matrix{
		rows:    rows,
		columns: columns,
		values:  make([]byte, int(size)),
	}, nil
}

func (matrix *Matrix) Rows() uint32    { return matrix.rows }
func (matrix *Matrix) Columns() uint32 { return matrix.columns }
func (matrix *Matrix) Bytes() uint64   { return uint64(len(matrix.values)) }

// At returns one matrix element.
func (matrix *Matrix) At(row, column uint32) (byte, error) {
	index, err := matrix.index(row, column)
	if err != nil {
		return 0, err
	}
	return matrix.values[index], nil
}

// Set replaces one matrix element.
func (matrix *Matrix) Set(row, column uint32, value byte) error {
	index, err := matrix.index(row, column)
	if err != nil {
		return err
	}
	matrix.values[index] = value
	return nil
}

func (matrix *Matrix) index(row, column uint32) (int, error) {
	if row >= matrix.rows || column >= matrix.columns {
		return 0, fmt.Errorf("%w: (%d,%d) in %dx%d", ErrMatrixIndex, row, column, matrix.rows, matrix.columns)
	}
	return int(uint64(row)*uint64(matrix.columns) + uint64(column)), nil
}

func (matrix *Matrix) row(row uint32) ([]byte, error) {
	if row >= matrix.rows {
		return nil, fmt.Errorf("%w: row %d in %dx%d", ErrMatrixIndex, row, matrix.rows, matrix.columns)
	}
	start := int(uint64(row) * uint64(matrix.columns))
	end := start + int(matrix.columns)
	return matrix.values[start:end:end], nil
}

// SwapRows exchanges two complete rows.
func (matrix *Matrix) SwapRows(left, right uint32) error {
	leftValues, err := matrix.row(left)
	if err != nil {
		return err
	}
	rightValues, err := matrix.row(right)
	if err != nil {
		return err
	}
	for index := range leftValues {
		leftValues[index], rightValues[index] = rightValues[index], leftValues[index]
	}
	return nil
}

// ScaleRow multiplies a row by coefficient.
func (matrix *Matrix) ScaleRow(row uint32, coefficient byte) error {
	values, err := matrix.row(row)
	if err != nil {
		return err
	}
	if coefficient == 0 {
		return ErrNonInvertibleRowOperation
	}
	Scale(values, coefficient)
	return nil
}

// AddScaledRow adds coefficient times source to destination.
func (matrix *Matrix) AddScaledRow(destination, source uint32, coefficient byte) error {
	destinationValues, err := matrix.row(destination)
	if err != nil {
		return err
	}
	sourceValues, err := matrix.row(source)
	if err != nil {
		return err
	}
	if destination == source {
		return ErrNonInvertibleRowOperation
	}
	return MulAdd(destinationValues, coefficient, sourceValues)
}
