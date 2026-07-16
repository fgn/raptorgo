package matrix

import (
	"errors"
	"math"
	"math/rand"
	"reflect"
	"slices"
	"testing"
)

func TestSparseBinaryAllocationBounds(t *testing.T) {
	t.Parallel()

	if _, err := NewSparseBinary(0, 1, 0, 100); !errors.Is(err, ErrInvalidDimensions) {
		t.Fatalf("zero-row error = %v", err)
	}
	if _, err := NewSparseBinary(1, 0, 0, 100); !errors.Is(err, ErrInvalidDimensions) {
		t.Fatalf("zero-column error = %v", err)
	}
	if _, err := NewSparseBinary(2, 3, 7, 100); !errors.Is(err, ErrInvalidSparseArgument) {
		t.Fatalf("excess-nonzero error = %v", err)
	}
	required, err := SparseBinaryBackingBytes(7, 11, 37)
	if err != nil {
		t.Fatal(err)
	}
	if required != 8*8+37*4+11*4 {
		t.Fatalf("backing bytes = %d", required)
	}
	if _, err := NewSparseBinary(7, 11, 37, required-1); !errors.Is(err, ErrBackingLimitExceeded) {
		t.Fatalf("under-budget error = %v", err)
	}
	matrix, err := NewSparseBinary(7, 11, 37, required)
	if err != nil {
		t.Fatal(err)
	}
	if matrix.Rows() != 7 || matrix.Columns() != 11 || matrix.MaxNonzeros() != 37 || matrix.Nonzeros() != 0 {
		t.Fatalf("matrix metadata = %dx%d nnz=%d/%d", matrix.Rows(), matrix.Columns(), matrix.Nonzeros(), matrix.MaxNonzeros())
	}
	if _, err := SparseBinaryBackingBytes(math.MaxUint32, math.MaxUint32, uint64(math.MaxUint32)*uint64(math.MaxUint32)); !errors.Is(err, ErrBackingLimitExceeded) {
		t.Fatalf("overflowing backing error = %v", err)
	}
}

func TestSparseBinarySetAndSortedIteration(t *testing.T) {
	t.Parallel()

	matrix := mustSparse(t, 3, 8, 10)
	for _, column := range []uint32{7, 1, 5, 0, 3} {
		if err := matrix.Set(1, column, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := matrix.Set(1, 5, true); err != nil {
		t.Fatal(err)
	}
	if err := matrix.Set(1, 3, false); err != nil {
		t.Fatal(err)
	}
	if err := matrix.Set(1, 3, false); err != nil {
		t.Fatal(err)
	}
	if matrix.Nonzeros() != 4 {
		t.Fatalf("nonzeros = %d", matrix.Nonzeros())
	}
	weight, err := matrix.RowWeight(1)
	if err != nil {
		t.Fatal(err)
	}
	if weight != 4 {
		t.Fatalf("weight = %d", weight)
	}
	var got []uint32
	if err := matrix.ForEachInRow(1, func(column uint32) bool {
		got = append(got, column)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if want := []uint32{0, 1, 5, 7}; !reflect.DeepEqual(got, want) {
		t.Fatalf("row = %v, want %v", got, want)
	}
	for column := uint32(0); column < matrix.Columns(); column++ {
		has, err := matrix.Has(1, column)
		if err != nil {
			t.Fatal(err)
		}
		want := column == 0 || column == 1 || column == 5 || column == 7
		if has != want {
			t.Fatalf("Has(1,%d) = %t, want %t", column, has, want)
		}
	}
}

func TestSparseBinaryRejectedOperationsAreTransactional(t *testing.T) {
	t.Parallel()

	matrix := mustSparse(t, 2, 3, 2)
	if err := matrix.Set(0, 0, true); err != nil {
		t.Fatal(err)
	}
	if err := matrix.Set(1, 1, true); err != nil {
		t.Fatal(err)
	}
	assertRejectedWithoutSparseMutation(t, matrix, ErrNonzeroLimitExceeded, func() error {
		return matrix.Set(0, 2, true)
	})
	assertRejectedWithoutSparseMutation(t, matrix, ErrIndex, func() error {
		return matrix.Set(2, 0, true)
	})
	assertRejectedWithoutSparseMutation(t, matrix, ErrNonInvertibleRowOp, func() error {
		return matrix.AddRow(0, 0)
	})
	assertRejectedWithoutSparseMutation(t, matrix, ErrNonzeroLimitExceeded, func() error {
		return matrix.AddRow(0, 1)
	})
	assertRejectedWithoutSparseMutation(t, matrix, ErrIndex, func() error {
		return matrix.SwapRows(0, 2)
	})
	assertRejectedWithoutSparseMutation(t, matrix, ErrIndex, func() error {
		return matrix.SwapColumns(0, 3)
	})
	assertRejectedWithoutSparseMutation(t, matrix, ErrInvalidSparseArgument, func() error {
		return matrix.ForEachInRow(0, nil)
	})
}

func TestSparseBinaryRowAndColumnOperations(t *testing.T) {
	t.Parallel()

	matrix := mustSparse(t, 5, 7, 35)
	dense := newDenseBinary(5, 7)
	initial := [][2]uint32{{0, 0}, {0, 3}, {1, 1}, {2, 2}, {2, 4}, {2, 6}, {3, 0}, {3, 1}, {3, 6}, {4, 5}}
	for _, cell := range initial {
		if err := matrix.Set(cell[0], cell[1], true); err != nil {
			t.Fatal(err)
		}
		dense[cell[0]][cell[1]] = true
	}
	assertSparseEqualsDense(t, matrix, dense)

	if err := matrix.SwapRows(0, 2); err != nil {
		t.Fatal(err)
	}
	dense[0], dense[2] = dense[2], dense[0]
	assertSparseEqualsDense(t, matrix, dense)

	if err := matrix.SwapRows(4, 1); err != nil {
		t.Fatal(err)
	}
	dense[4], dense[1] = dense[1], dense[4]
	assertSparseEqualsDense(t, matrix, dense)

	if err := matrix.SwapColumns(0, 6); err != nil {
		t.Fatal(err)
	}
	swapDenseColumns(dense, 0, 6)
	assertSparseEqualsDense(t, matrix, dense)

	if err := matrix.AddRow(3, 0); err != nil {
		t.Fatal(err)
	}
	addDenseBinaryRow(dense, 3, 0)
	assertSparseEqualsDense(t, matrix, dense)

	if err := matrix.AddRow(1, 4); err != nil {
		t.Fatal(err)
	}
	addDenseBinaryRow(dense, 1, 4)
	assertSparseEqualsDense(t, matrix, dense)
}

func TestSparseBinaryRandomOperationTraceMatchesDense(t *testing.T) {
	t.Parallel()

	random := rand.New(rand.NewSource(0x5a6330))
	for scenario := 0; scenario < 50; scenario++ {
		rows := uint32(random.Intn(11) + 2)
		columns := uint32(random.Intn(15) + 1)
		matrix := mustSparse(t, rows, columns, uint64(rows)*uint64(columns))
		dense := newDenseBinary(rows, columns)
		for step := 0; step < 500; step++ {
			switch random.Intn(4) {
			case 0:
				row := uint32(random.Intn(int(rows)))
				column := uint32(random.Intn(int(columns)))
				value := random.Intn(2) == 1
				if err := matrix.Set(row, column, value); err != nil {
					t.Fatalf("scenario %d step %d: %v", scenario, step, err)
				}
				dense[row][column] = value
			case 1:
				left := uint32(random.Intn(int(rows)))
				right := uint32(random.Intn(int(rows) - 1))
				if right >= left {
					right++
				}
				if err := matrix.AddRow(left, right); err != nil {
					t.Fatalf("scenario %d step %d: %v", scenario, step, err)
				}
				addDenseBinaryRow(dense, left, right)
			case 2:
				left := uint32(random.Intn(int(rows)))
				right := uint32(random.Intn(int(rows)))
				if err := matrix.SwapRows(left, right); err != nil {
					t.Fatalf("scenario %d step %d: %v", scenario, step, err)
				}
				dense[left], dense[right] = dense[right], dense[left]
			case 3:
				left := uint32(random.Intn(int(columns)))
				right := uint32(random.Intn(int(columns)))
				if err := matrix.SwapColumns(left, right); err != nil {
					t.Fatalf("scenario %d step %d: %v", scenario, step, err)
				}
				swapDenseColumns(dense, left, right)
			}
			assertSparseEqualsDense(t, matrix, dense)
		}
	}
}

func TestSparseBinaryLowCapacityRandomTraceMatchesDense(t *testing.T) {
	t.Parallel()

	random := rand.New(rand.NewSource(0x6330cafe))
	for scenario := 0; scenario < 100; scenario++ {
		rows := uint32(random.Intn(5) + 2)
		columns := uint32(random.Intn(8) + 1)
		capacity := uint64(random.Intn(int(rows*columns) + 1))
		matrix := mustSparse(t, rows, columns, capacity)
		dense := newDenseBinary(rows, columns)
		for step := 0; step < 300; step++ {
			beforeNonzeros := matrix.nonzeros
			beforeOffsets := append([]uint64(nil), matrix.offsets...)
			beforeEntries := append([]uint32(nil), matrix.entries...)
			denseBefore := cloneDenseBinary(dense)
			switch random.Intn(4) {
			case 0:
				row := uint32(random.Intn(int(rows)))
				column := uint32(random.Intn(int(columns)))
				value := random.Intn(2) == 1
				err := matrix.Set(row, column, value)
				wouldAdd := value && !dense[row][column]
				if wouldAdd && denseNonzeros(dense) == capacity {
					if !errors.Is(err, ErrNonzeroLimitExceeded) {
						t.Fatalf("scenario %d step %d Set error = %v", scenario, step, err)
					}
					assertSparseSnapshot(t, matrix, beforeNonzeros, beforeOffsets, beforeEntries)
				} else {
					if err != nil {
						t.Fatalf("scenario %d step %d Set: %v", scenario, step, err)
					}
					dense[row][column] = value
				}
			case 1:
				destination := uint32(random.Intn(int(rows)))
				source := uint32(random.Intn(int(rows) - 1))
				if source >= destination {
					source++
				}
				candidate := cloneDenseBinary(dense)
				addDenseBinaryRow(candidate, destination, source)
				err := matrix.AddRow(destination, source)
				if denseNonzeros(candidate) > capacity {
					if !errors.Is(err, ErrNonzeroLimitExceeded) {
						t.Fatalf("scenario %d step %d AddRow error = %v", scenario, step, err)
					}
					assertSparseSnapshot(t, matrix, beforeNonzeros, beforeOffsets, beforeEntries)
				} else {
					if err != nil {
						t.Fatalf("scenario %d step %d AddRow: %v", scenario, step, err)
					}
					dense = candidate
				}
			case 2:
				left := uint32(random.Intn(int(rows)))
				right := uint32(random.Intn(int(rows)))
				if err := matrix.SwapRows(left, right); err != nil {
					t.Fatal(err)
				}
				dense[left], dense[right] = dense[right], dense[left]
			case 3:
				left := uint32(random.Intn(int(columns)))
				right := uint32(random.Intn(int(columns)))
				if err := matrix.SwapColumns(left, right); err != nil {
					t.Fatal(err)
				}
				swapDenseColumns(dense, left, right)
			}
			if reflect.DeepEqual(dense, denseBefore) && matrix.nonzeros != beforeNonzeros {
				// A state may have the same dense value after a same-position swap,
				// but its nonzero count can never change in that case.
				t.Fatalf("scenario %d step %d changed nnz without changing dense state", scenario, step)
			}
			assertSparseEqualsDense(t, matrix, dense)
		}
	}
}

func TestSparseBinaryCloneIsIndependent(t *testing.T) {
	t.Parallel()

	matrix := mustSparse(t, 2, 3, 4)
	if err := matrix.Set(0, 1, true); err != nil {
		t.Fatal(err)
	}
	required, err := SparseBinaryBackingBytes(2, 3, 4)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := matrix.Clone(required)
	if err != nil {
		t.Fatal(err)
	}
	if err := clone.Set(1, 2, true); err != nil {
		t.Fatal(err)
	}
	has, err := matrix.Has(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("clone aliases original")
	}
	if _, err := matrix.Clone(required - 1); !errors.Is(err, ErrBackingLimitExceeded) {
		t.Fatalf("under-budget clone error = %v", err)
	}
}

func TestSparseBinaryMutationsDoNotAllocate(t *testing.T) {
	matrix := mustSparse(t, 4, 8, 32)
	for _, cell := range [][2]uint32{{0, 0}, {0, 2}, {1, 1}, {1, 3}, {2, 4}, {3, 5}} {
		if err := matrix.Set(cell[0], cell[1], true); err != nil {
			t.Fatal(err)
		}
	}
	allocations := testing.AllocsPerRun(100, func() {
		if err := matrix.AddRow(0, 1); err != nil {
			panic(err)
		}
		if err := matrix.AddRow(0, 1); err != nil {
			panic(err)
		}
		if err := matrix.SwapRows(2, 3); err != nil {
			panic(err)
		}
		if err := matrix.SwapRows(2, 3); err != nil {
			panic(err)
		}
		if err := matrix.SwapColumns(6, 7); err != nil {
			panic(err)
		}
		if err := matrix.SwapColumns(6, 7); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("mutations allocated %.1f times per run", allocations)
	}
}

func mustSparse(t *testing.T, rows, columns uint32, maxNonzeros uint64) *SparseBinary {
	t.Helper()
	required, err := SparseBinaryBackingBytes(rows, columns, maxNonzeros)
	if err != nil {
		t.Fatal(err)
	}
	matrix, err := NewSparseBinary(rows, columns, maxNonzeros, required)
	if err != nil {
		t.Fatal(err)
	}
	return matrix
}

func newDenseBinary(rows, columns uint32) [][]bool {
	dense := make([][]bool, rows)
	for row := range dense {
		dense[row] = make([]bool, columns)
	}
	return dense
}

func addDenseBinaryRow(dense [][]bool, destination, source uint32) {
	for column := range dense[destination] {
		dense[destination][column] = dense[destination][column] != dense[source][column]
	}
}

func swapDenseColumns(dense [][]bool, left, right uint32) {
	for row := range dense {
		dense[row][left], dense[row][right] = dense[row][right], dense[row][left]
	}
}

func assertSparseEqualsDense(t *testing.T, matrix *SparseBinary, dense [][]bool) {
	t.Helper()
	var expectedNonzeros uint64
	for row := uint32(0); row < matrix.Rows(); row++ {
		last := uint32(0)
		first := true
		visited := uint32(0)
		err := matrix.ForEachInRow(row, func(column uint32) bool {
			if column >= matrix.Columns() {
				t.Fatalf("row %d contains out-of-range column %d", row, column)
			}
			if !first && column <= last {
				t.Fatalf("row %d is not strictly sorted: %d after %d", row, column, last)
			}
			if !dense[row][column] {
				t.Fatalf("sparse (%d,%d)=1, dense=0", row, column)
			}
			first = false
			last = column
			visited++
			return true
		})
		if err != nil {
			t.Fatal(err)
		}
		var denseWeight uint32
		for column, value := range dense[row] {
			if value {
				denseWeight++
				has, err := matrix.Has(row, uint32(column))
				if err != nil {
					t.Fatal(err)
				}
				if !has {
					t.Fatalf("sparse (%d,%d)=0, dense=1", row, column)
				}
			}
		}
		if visited != denseWeight {
			t.Fatalf("row %d weight = %d, want %d", row, visited, denseWeight)
		}
		expectedNonzeros += uint64(denseWeight)
	}
	if matrix.Nonzeros() != expectedNonzeros {
		t.Fatalf("nonzeros = %d, want %d", matrix.Nonzeros(), expectedNonzeros)
	}
	if matrix.offsets[0] != 0 || matrix.offsets[len(matrix.offsets)-1] != matrix.nonzeros {
		t.Fatalf("invalid boundary offsets %v for nnz %d", matrix.offsets, matrix.nonzeros)
	}
	for index := 1; index < len(matrix.offsets); index++ {
		if matrix.offsets[index] < matrix.offsets[index-1] {
			t.Fatalf("offsets not monotonic: %v", matrix.offsets)
		}
	}
}

func assertRejectedWithoutSparseMutation(t *testing.T, matrix *SparseBinary, want error, operation func() error) {
	t.Helper()
	beforeNonzeros := matrix.nonzeros
	beforeOffsets := append([]uint64(nil), matrix.offsets...)
	beforeEntries := append([]uint32(nil), matrix.entries...)
	if err := operation(); !errors.Is(err, want) {
		t.Fatalf("operation error = %v, want %v", err, want)
	}
	if matrix.nonzeros != beforeNonzeros || !reflect.DeepEqual(matrix.offsets, beforeOffsets) || !reflect.DeepEqual(matrix.entries, beforeEntries) {
		t.Fatal("rejected operation mutated matrix")
	}
}

func cloneDenseBinary(dense [][]bool) [][]bool {
	clone := make([][]bool, len(dense))
	for row := range dense {
		clone[row] = append([]bool(nil), dense[row]...)
	}
	return clone
}

func denseNonzeros(dense [][]bool) uint64 {
	var count uint64
	for row := range dense {
		for _, value := range dense[row] {
			if value {
				count++
			}
		}
	}
	return count
}

func assertSparseSnapshot(t *testing.T, matrix *SparseBinary, nonzeros uint64, offsets []uint64, entries []uint32) {
	t.Helper()
	if matrix.nonzeros != nonzeros || !slices.Equal(matrix.offsets, offsets) || !slices.Equal(matrix.entries, entries) {
		t.Fatalf("rejected sparse operation changed observable state: nnz %d/%d offsets %v/%v entries %v/%v", matrix.nonzeros, nonzeros, matrix.offsets, offsets, matrix.entries, entries)
	}
}
