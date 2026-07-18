package gf256

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestMatrixAllocationBounds(t *testing.T) {
	t.Parallel()

	for _, dimensions := range [][2]uint32{{0, 0}, {0, 1}, {1, 0}} {
		if _, err := NewMatrix(dimensions[0], dimensions[1], 100); !errors.Is(err, ErrInvalidMatrixDimensions) {
			t.Errorf("NewMatrix(%d, %d) error = %v", dimensions[0], dimensions[1], err)
		}
	}
	if _, err := NewMatrix(10, 20, 199); !errors.Is(err, ErrMatrixLimitExceeded) {
		t.Fatalf("under-budget matrix error = %v", err)
	}
	matrix, err := NewMatrix(10, 20, 200)
	if err != nil {
		t.Fatal(err)
	}
	if matrix.Rows() != 10 || matrix.Columns() != 20 || matrix.Bytes() != 200 {
		t.Fatalf("matrix dimensions = %dx%d/%d bytes", matrix.Rows(), matrix.Columns(), matrix.Bytes())
	}
	if _, err := NewMatrix(math.MaxUint32, math.MaxUint32, math.MaxUint64); !errors.Is(err, ErrMatrixLimitExceeded) {
		t.Fatalf("platform-oversize matrix error = %v", err)
	}
}

func TestMatrixAtSetAndRowOperations(t *testing.T) {
	t.Parallel()

	matrix := mustMatrix(t, 3, 4)
	for row := uint32(0); row < matrix.Rows(); row++ {
		for column := uint32(0); column < matrix.Columns(); column++ {
			if err := matrix.Set(row, column, byte(row*10+column)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := matrix.SwapRows(0, 2); err != nil {
		t.Fatal(err)
	}
	if err := matrix.ScaleRow(1, 7); err != nil {
		t.Fatal(err)
	}
	if err := matrix.AddScaledRow(2, 1, 19); err != nil {
		t.Fatal(err)
	}

	want := [][]byte{
		{20, 21, 22, 23},
		{polynomialMul(7, 10), polynomialMul(7, 11), polynomialMul(7, 12), polynomialMul(7, 13)},
		{
			polynomialMul(19, polynomialMul(7, 10)),
			1 ^ polynomialMul(19, polynomialMul(7, 11)),
			2 ^ polynomialMul(19, polynomialMul(7, 12)),
			3 ^ polynomialMul(19, polynomialMul(7, 13)),
		},
	}
	assertMatrixEquals(t, matrix, want)
}

func TestRejectedMatrixOperationDoesNotMutate(t *testing.T) {
	t.Parallel()

	matrix := mustMatrix(t, 2, 2)
	for index, value := range []byte{1, 2, 3, 4} {
		matrix.values[index] = value
	}
	before := bytes.Clone(matrix.values)
	operations := []struct {
		name string
		run  func() error
	}{
		{"At", func() error { _, err := matrix.At(2, 0); return err }},
		{"Set", func() error { return matrix.Set(0, 2, 9) }},
		{"SwapRows-left", func() error { return matrix.SwapRows(2, 0) }},
		{"SwapRows-right", func() error { return matrix.SwapRows(0, 2) }},
		{"ScaleRow", func() error { return matrix.ScaleRow(2, 9) }},
		{"AddScaledRow-destination", func() error { return matrix.AddScaledRow(2, 0, 9) }},
		{"AddScaledRow-source", func() error { return matrix.AddScaledRow(0, 2, 9) }},
		{"ScaleRow-zero", func() error { return matrix.ScaleRow(0, 0) }},
		{"AddScaledRow-same", func() error { return matrix.AddScaledRow(0, 0, 9) }},
	}
	for _, operation := range operations {
		err := operation.run()
		if !errors.Is(err, ErrMatrixIndex) && !errors.Is(err, ErrNonInvertibleRowOperation) {
			t.Errorf("%s error = %v", operation.name, err)
		}
		if !bytes.Equal(matrix.values, before) {
			t.Fatalf("%s mutated the matrix on rejection", operation.name)
		}
	}
}

func TestRowOperationTracePreservesKnownSolution(t *testing.T) {
	t.Parallel()

	// The last column is the right-hand side for A*x=b.
	solution := []byte{7, 19, 201}
	coefficients := [][]byte{
		{1, 2, 3},
		{5, 8, 13},
		{21, 34, 55},
		{89, 144, 233},
	}
	matrix := mustMatrix(t, uint32(len(coefficients)), uint32(len(solution)+1))
	for row, values := range coefficients {
		var rightHandSide byte
		for column, coefficient := range values {
			if err := matrix.Set(uint32(row), uint32(column), coefficient); err != nil {
				t.Fatal(err)
			}
			rightHandSide ^= polynomialMul(coefficient, solution[column])
		}
		if err := matrix.Set(uint32(row), uint32(len(solution)), rightHandSide); err != nil {
			t.Fatal(err)
		}
	}
	initial := cloneMatrixRows(matrix)
	assertAugmentedSystemHasSolution(t, matrix, solution, "initial")
	assertEquivalentRowSpace(t, initial, matrix, "initial")

	trace := []struct {
		name string
		run  func() error
	}{
		{"swap 0 3", func() error { return matrix.SwapRows(0, 3) }},
		{"scale 1", func() error { return matrix.ScaleRow(1, 117) }},
		{"add 1 to 2", func() error { return matrix.AddScaledRow(2, 1, 203) }},
		{"add 3 to 0", func() error { return matrix.AddScaledRow(0, 3, 1) }},
		{"swap 1 2", func() error { return matrix.SwapRows(1, 2) }},
	}
	for _, step := range trace {
		if err := step.run(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		assertAugmentedSystemHasSolution(t, matrix, solution, step.name)
		assertEquivalentRowSpace(t, initial, matrix, step.name)
	}
}

func TestDegenerateRowOperationsAreRejected(t *testing.T) {
	t.Parallel()

	matrix := mustMatrix(t, 1, 4)
	copy(matrix.values, []byte{1, 2, 3, 4})
	if err := matrix.SwapRows(0, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(matrix.values, []byte{1, 2, 3, 4}) {
		t.Fatalf("same-row swap changed matrix: %x", matrix.values)
	}
	before := bytes.Clone(matrix.values)
	if err := matrix.ScaleRow(0, 0); !errors.Is(err, ErrNonInvertibleRowOperation) {
		t.Fatalf("zero scale error = %v", err)
	}
	if !bytes.Equal(matrix.values, before) {
		t.Fatal("zero scale mutated the matrix")
	}
	if err := matrix.AddScaledRow(0, 0, 37); !errors.Is(err, ErrNonInvertibleRowOperation) {
		t.Fatalf("same-row addition error = %v", err)
	}
	if !bytes.Equal(matrix.values, before) {
		t.Fatal("same-row addition mutated the matrix")
	}
}

func TestMatrixRowViewCannotAppendIntoNextRow(t *testing.T) {
	t.Parallel()

	matrix := mustMatrix(t, 2, 3)
	row, err := matrix.row(0)
	if err != nil {
		t.Fatal(err)
	}
	if cap(row) != len(row) {
		t.Fatalf("row capacity = %d, want %d", cap(row), len(row))
	}
}

func TestMatrixRowViewAliasesStorageAndChecksBounds(t *testing.T) {
	t.Parallel()

	matrix := mustMatrix(t, 2, 3)
	view, err := matrix.RowView(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(view) != 3 || cap(view) != 3 {
		t.Fatalf("row view len=%d cap=%d, want 3/3", len(view), cap(view))
	}
	view[2] = 0x5a
	got, err := matrix.At(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0x5a {
		t.Fatalf("write through view read back %#x, want 0x5a", got)
	}
	if err := matrix.Set(1, 0, 0xa5); err != nil {
		t.Fatal(err)
	}
	if view[0] != 0xa5 {
		t.Fatalf("view saw %#x after Set, want 0xa5", view[0])
	}
	if _, err := matrix.RowView(2); !errors.Is(err, ErrMatrixIndex) {
		t.Fatalf("out-of-range row view error = %v, want ErrMatrixIndex", err)
	}
}

func FuzzMatrixRowOperations(f *testing.F) {
	f.Add(uint8(3), uint8(4), uint8(0), uint8(2), uint8(37))
	f.Fuzz(func(t *testing.T, rowSeed, columnSeed, leftSeed, rightSeed, coefficient uint8) {
		rows := uint32(rowSeed%16) + 1
		columns := uint32(columnSeed%16) + 1
		matrix, err := NewMatrix(rows, columns, uint64(rows)*uint64(columns))
		if err != nil {
			t.Fatal(err)
		}
		for index := range matrix.values {
			matrix.values[index] = byte(index*31 + 7)
		}
		left := uint32(leftSeed) % rows
		right := uint32(rightSeed) % rows
		before := bytes.Clone(matrix.values)
		err = matrix.AddScaledRow(left, right, coefficient)
		if left == right {
			if !errors.Is(err, ErrNonInvertibleRowOperation) || !bytes.Equal(matrix.values, before) {
				t.Fatalf("same-row operation error=%v matrix=%x before=%x", err, matrix.values, before)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		for row := uint32(0); row < rows; row++ {
			for column := uint32(0); column < columns; column++ {
				index := int(row*columns + column)
				want := before[index]
				if row == left {
					sourceIndex := int(right*columns + column)
					want ^= polynomialMul(coefficient, before[sourceIndex])
				}
				if matrix.values[index] != want {
					t.Fatalf("(%d,%d) = %d, want %d", row, column, matrix.values[index], want)
				}
			}
		}
	})
}

func cloneMatrixRows(matrix *Matrix) [][]byte {
	rows := make([][]byte, matrix.Rows())
	for row := range rows {
		start := row * int(matrix.Columns())
		rows[row] = bytes.Clone(matrix.values[start : start+int(matrix.Columns())])
	}
	return rows
}

func assertEquivalentRowSpace(t *testing.T, initial [][]byte, matrix *Matrix, step string) {
	t.Helper()
	want := canonicalRowSpace(initial)
	got := canonicalRowSpace(cloneMatrixRows(matrix))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s changed the augmented row space:\n got %v\nwant %v", step, got, want)
	}
}

// canonicalRowSpace is an independent reduced-row-echelon implementation
// based on polynomialMul rather than the production log/exp arithmetic.
func canonicalRowSpace(rows [][]byte) [][]byte {
	if len(rows) == 0 {
		return rows
	}
	pivotRow := 0
	for column := 0; column < len(rows[0]) && pivotRow < len(rows); column++ {
		pivot := pivotRow
		for pivot < len(rows) && rows[pivot][column] == 0 {
			pivot++
		}
		if pivot == len(rows) {
			continue
		}
		rows[pivotRow], rows[pivot] = rows[pivot], rows[pivotRow]
		inverse := polynomialInverse(rows[pivotRow][column])
		for index := range rows[pivotRow] {
			rows[pivotRow][index] = polynomialMul(inverse, rows[pivotRow][index])
		}
		for row := range rows {
			if row == pivotRow || rows[row][column] == 0 {
				continue
			}
			coefficient := rows[row][column]
			for index := range rows[row] {
				rows[row][index] ^= polynomialMul(coefficient, rows[pivotRow][index])
			}
		}
		pivotRow++
	}
	return rows
}

func polynomialInverse(value byte) byte {
	for candidate := 1; candidate <= math.MaxUint8; candidate++ {
		if polynomialMul(value, byte(candidate)) == 1 {
			return byte(candidate)
		}
	}
	panic("nonzero GF(256) value has no inverse")
}

func mustMatrix(t *testing.T, rows, columns uint32) *Matrix {
	t.Helper()
	matrix, err := NewMatrix(rows, columns, uint64(rows)*uint64(columns))
	if err != nil {
		t.Fatal(err)
	}
	return matrix
}

func assertMatrixEquals(t *testing.T, matrix *Matrix, want [][]byte) {
	t.Helper()
	for row := range want {
		for column, expected := range want[row] {
			got, err := matrix.At(uint32(row), uint32(column))
			if err != nil {
				t.Fatal(err)
			}
			if got != expected {
				t.Fatalf("matrix[%d,%d] = %d, want %d", row, column, got, expected)
			}
		}
	}
}

func assertAugmentedSystemHasSolution(t *testing.T, matrix *Matrix, solution []byte, step string) {
	t.Helper()
	if matrix.Columns() != uint32(len(solution)+1) {
		t.Fatalf("%s: matrix is not augmented for the supplied solution", step)
	}
	for row := uint32(0); row < matrix.Rows(); row++ {
		var leftHandSide byte
		for column, value := range solution {
			coefficient, err := matrix.At(row, uint32(column))
			if err != nil {
				t.Fatal(err)
			}
			leftHandSide ^= polynomialMul(coefficient, value)
		}
		rightHandSide, err := matrix.At(row, uint32(len(solution)))
		if err != nil {
			t.Fatal(err)
		}
		if leftHandSide != rightHandSide {
			t.Fatalf("%s: row %d no longer holds: %d != %d", step, row, leftHandSide, rightHandSide)
		}
	}
}
