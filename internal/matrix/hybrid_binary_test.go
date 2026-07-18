package matrix

import (
	"errors"
	"math"
	"math/rand"
	"reflect"
	"slices"
	"testing"
)

func TestHybridBinaryExactBackingAndConstructionValidation(t *testing.T) {
	t.Parallel()

	counts := []uint32{2, 1, 0}
	required, err := HybridBinaryBackingBytes(3, 8, 2, 5, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := uint64(4*4 + 3*2 + 7*4 + 3*4 + 3*8 + 8*4 + 3*8)
	if required != want {
		t.Fatalf("backing=%d, want %d", required, want)
	}
	if result, err := NewHybridBinary(counts, 8, 2, 5, required-1); !errors.Is(err, ErrBackingLimitExceeded) || result != nil {
		t.Fatalf("budget-1 matrix=%v error=%v", result, err)
	}
	matrix, err := NewHybridBinary(counts, 8, 2, 5, required)
	if err != nil {
		t.Fatal(err)
	}
	if matrix.BackingBytes() != required || matrix.DenseColumns() != 2 || matrix.MaxDense() != 5 {
		t.Fatalf("metadata backing=%d dense=%d/%d", matrix.BackingBytes(), matrix.DenseColumns(), matrix.MaxDense())
	}
	if err := matrix.LoadSparseRow(0, []uint16{1, 0}); !errors.Is(err, ErrInvalidSparseArgument) {
		t.Fatalf("unsorted load error=%v", err)
	}
	if err := matrix.LoadSparseRow(0, []uint16{0, 4}); err != nil {
		t.Fatal(err)
	}
	if err := matrix.LoadSparseRow(2, nil); !errors.Is(err, ErrInvalidSparseArgument) {
		t.Fatalf("out-of-order load error=%v", err)
	}
	if err := matrix.LoadSparseRow(1, []uint16{3}); err != nil {
		t.Fatal(err)
	}
	if err := matrix.Finalize(); !errors.Is(err, ErrInvalidSparseArgument) {
		t.Fatalf("incomplete finalize error=%v", err)
	}
	if err := matrix.LoadSparseRow(2, nil); err != nil {
		t.Fatal(err)
	}
	if err := matrix.SetInitialDense(0, 6, true); err != nil {
		t.Fatal(err)
	}
	if err := matrix.SetInitialDense(0, 5, true); !errors.Is(err, ErrIndex) {
		t.Fatalf("non-suffix dense error=%v", err)
	}
	if err := matrix.Finalize(); err != nil {
		t.Fatal(err)
	}
	if err := matrix.Finalize(); !errors.Is(err, ErrInvalidSparseArgument) {
		t.Fatalf("second finalize error=%v", err)
	}
	if _, err := HybridBinaryBackingBytes(1, math.MaxUint16+1, 0, 0, 0); !errors.Is(err, ErrInvalidDimensions) {
		t.Fatalf("wide matrix error=%v", err)
	}
}

func TestHybridBinaryPermutationFreezeAndDenseRowsMatchOracle(t *testing.T) {
	t.Parallel()

	rows := [][]uint16{
		{0, 2, 5},
		{1, 2, 4},
		{0, 3, 5},
		{1, 4},
	}
	matrix := newTestHybridBinary(t, rows, 8, 2, 5, [][2]uint32{{0, 6}, {1, 7}, {2, 6}, {2, 7}})
	oracle := make([][]bool, len(rows))
	for row := range oracle {
		oracle[row] = make([]bool, 8)
		for _, column := range rows[row] {
			oracle[row][column] = true
		}
	}
	for _, cell := range [][2]uint32{{0, 6}, {1, 7}, {2, 6}, {2, 7}} {
		oracle[cell[0]][cell[1]] = true
	}
	columnOrder := []uint32{0, 1, 2, 3, 4, 5, 6, 7}
	assertHybridEqualsOracle(t, matrix, oracle, columnOrder)

	if err := matrix.SwapRows(0, 2); err != nil {
		t.Fatal(err)
	}
	oracle[0], oracle[2] = oracle[2], oracle[0]
	if err := matrix.SwapSparseColumns(1, 5); err != nil {
		t.Fatal(err)
	}
	for row := range oracle {
		oracle[row][1], oracle[row][5] = oracle[row][5], oracle[row][1]
	}
	columnOrder[1], columnOrder[5] = columnOrder[5], columnOrder[1]
	assertHybridEqualsOracle(t, matrix, oracle, columnOrder)

	physical, err := matrix.FreezeNext()
	if err != nil {
		t.Fatal(err)
	}
	if physical != columnOrder[5] || matrix.DenseColumns() != 3 {
		t.Fatalf("freeze physical=%d dense=%d", physical, matrix.DenseColumns())
	}
	assertHybridEqualsOracle(t, matrix, oracle, columnOrder)
	if err := matrix.XORDenseRows(3, 0); err != nil {
		t.Fatal(err)
	}
	for column := uint32(5); column < 8; column++ {
		oracle[3][column] = oracle[3][column] != oracle[0][column]
	}
	assertHybridEqualsOracle(t, matrix, oracle, columnOrder)

	if err := matrix.SwapSparseColumns(0, 4); err != nil {
		t.Fatal(err)
	}
	for row := range oracle {
		oracle[row][0], oracle[row][4] = oracle[row][4], oracle[row][0]
	}
	columnOrder[0], columnOrder[4] = columnOrder[4], columnOrder[0]
	if _, err := matrix.FreezeNext(); err != nil {
		t.Fatal(err)
	}
	if _, err := matrix.FreezeNext(); err != nil {
		t.Fatal(err)
	}
	assertHybridEqualsOracle(t, matrix, oracle, columnOrder)

	before := snapshotHybrid(t, matrix)
	if _, err := matrix.FreezeNext(); !errors.Is(err, ErrNonzeroLimitExceeded) {
		t.Fatalf("over-cap freeze error=%v", err)
	}
	if after := snapshotHybrid(t, matrix); !reflect.DeepEqual(after, before) {
		t.Fatal("rejected freeze mutated matrix")
	}
}

func TestHybridBinaryColumnIndexAndDenseIteration(t *testing.T) {
	t.Parallel()

	matrix := newTestHybridBinary(t, [][]uint16{{0, 2}, {2}, {0, 1}}, 5, 1, 3, [][2]uint32{{1, 4}})
	start, end := matrix.columnOffsets[0], matrix.columnOffsets[1]
	rows := append([]uint32(nil), matrix.columnRows[start:end]...)
	slices.Sort(rows)
	if !slices.Equal(rows, []uint32{0, 2}) {
		t.Fatalf("column rows=%v", rows)
	}
	if _, err := matrix.FreezeNext(); err != nil {
		t.Fatal(err)
	}
	var columns []uint32
	if err := matrix.ForEachDenseOne(1, func(column uint32) bool {
		columns = append(columns, column)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(columns, []uint32{4}) {
		t.Fatalf("dense columns=%v", columns)
	}
}

func TestHybridBinaryAppendDenseOnesMatchesForEachDenseOne(t *testing.T) {
	t.Parallel()

	matrix := newTestHybridBinary(
		t, [][]uint16{{0, 2}, {1, 3}, {0, 1, 3}, {2, 3}}, 6, 2, 5,
		[][2]uint32{{0, 4}, {1, 5}, {2, 4}, {3, 5}},
	)
	checkAllRows := func(stage string) {
		t.Helper()
		scratch := []uint32{99}
		for row := uint32(0); row < matrix.Rows(); row++ {
			var want []uint32
			if err := matrix.ForEachDenseOne(row, func(column uint32) bool {
				want = append(want, column)
				return true
			}); err != nil {
				t.Fatal(err)
			}
			got, err := matrix.AppendDenseOnes(row, scratch[:1])
			if err != nil {
				t.Fatal(err)
			}
			if got[0] != 99 {
				t.Fatalf("%s row %d: append overwrote existing prefix: %v", stage, row, got)
			}
			if !slices.Equal(got[1:], want) {
				t.Fatalf("%s row %d: append=%v foreach=%v", stage, row, got[1:], want)
			}
		}
	}
	checkAllRows("initial")
	for freeze := 0; freeze < 3; freeze++ {
		if _, err := matrix.FreezeNext(); err != nil {
			t.Fatal(err)
		}
		if err := matrix.SwapRows(0, matrix.Rows()-1); err != nil {
			t.Fatal(err)
		}
		if err := matrix.XORDenseRows(1, 2); err != nil {
			t.Fatal(err)
		}
		checkAllRows("after freeze")
	}
	if _, err := matrix.AppendDenseOnes(matrix.Rows(), nil); !errors.Is(err, ErrInvalidSparseArgument) {
		t.Fatalf("out-of-range append error=%v", err)
	}
	var nilMatrix *HybridBinary
	if _, err := nilMatrix.AppendDenseOnes(0, nil); !errors.Is(err, ErrInvalidSparseArgument) {
		t.Fatalf("nil append error=%v", err)
	}
}

func TestHybridBinaryNilMutationReceiversRejectWithoutPanic(t *testing.T) {
	t.Parallel()

	var matrix *HybridBinary
	if err := matrix.LoadSparseRow(0, nil); !errors.Is(err, ErrInvalidSparseArgument) {
		t.Fatalf("nil load error=%v", err)
	}
	if err := matrix.Finalize(); !errors.Is(err, ErrInvalidSparseArgument) {
		t.Fatalf("nil finalize error=%v", err)
	}
	if err := matrix.SwapSparseColumns(0, 0); !errors.Is(err, ErrIndex) {
		t.Fatalf("nil sparse-column swap error=%v", err)
	}
}

func TestHybridBinarySetInitialDenseUsesLogicalRowAfterFinalize(t *testing.T) {
	t.Parallel()

	matrix := newTestHybridBinary(t, [][]uint16{{0}, {1}}, 4, 1, 2, nil)
	if err := matrix.SwapRows(0, 1); err != nil {
		t.Fatal(err)
	}
	if err := matrix.SetInitialDense(0, 3, true); err != nil {
		t.Fatal(err)
	}
	for row, want := range []bool{true, false} {
		got, err := matrix.Has(uint32(row), 3)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("row %d dense coefficient=%v want=%v", row, got, want)
		}
	}
}

func TestHybridBinaryMutationsDoNotAllocate(t *testing.T) {
	matrix := newTestHybridBinary(t, [][]uint16{{0, 2}, {1, 3}, {0, 1}}, 6, 2, 4, nil)
	if _, err := matrix.FreezeNext(); err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(100, func() {
		if err := matrix.SwapRows(0, 1); err != nil {
			panic(err)
		}
		if err := matrix.SwapRows(0, 1); err != nil {
			panic(err)
		}
		if err := matrix.SwapSparseColumns(0, 1); err != nil {
			panic(err)
		}
		if err := matrix.SwapSparseColumns(0, 1); err != nil {
			panic(err)
		}
		if err := matrix.XORDenseRows(1, 2); err != nil {
			panic(err)
		}
		if err := matrix.XORDenseRows(1, 2); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("hybrid mutations allocated %.1f times", allocations)
	}
}

func TestHybridBinaryRandomOperationTracesMatchDense(t *testing.T) {
	t.Parallel()

	random := rand.New(rand.NewSource(0x6330b17)) //nolint:gosec -- deterministic algebraic corpus
	for scenario := 0; scenario < 80; scenario++ {
		rowCount := 2 + random.Intn(8)
		columnCount := uint32(4 + random.Intn(13))
		initialDense := uint32(1 + random.Intn(min(3, int(columnCount-1))))
		maxDense := initialDense + uint32(random.Intn(int(columnCount-initialDense)+1))
		rows := make([][]uint16, rowCount)
		oracle := make([][]bool, rowCount)
		for row := range rows {
			oracle[row] = make([]bool, columnCount)
			for column := uint32(0); column < columnCount-initialDense; column++ {
				if random.Intn(4) == 0 {
					rows[row] = append(rows[row], uint16(column))
					oracle[row][column] = true
				}
			}
		}
		var initial [][2]uint32
		for row := range rows {
			for column := columnCount - initialDense; column < columnCount; column++ {
				if random.Intn(2) == 0 {
					initial = append(initial, [2]uint32{uint32(row), column})
					oracle[row][column] = true
				}
			}
		}
		matrix := newTestHybridBinary(t, rows, columnCount, initialDense, maxDense, initial)
		columnOrder := make([]uint32, columnCount)
		for column := range columnOrder {
			columnOrder[column] = uint32(column)
		}
		for step := 0; step < 200; step++ {
			switch random.Intn(4) {
			case 0:
				left, right := uint32(random.Intn(rowCount)), uint32(random.Intn(rowCount))
				if err := matrix.SwapRows(left, right); err != nil {
					t.Fatal(err)
				}
				oracle[left], oracle[right] = oracle[right], oracle[left]
			case 1:
				active := columnCount - matrix.DenseColumns()
				if active == 0 {
					continue
				}
				left, right := uint32(random.Intn(int(active))), uint32(random.Intn(int(active)))
				if err := matrix.SwapSparseColumns(left, right); err != nil {
					t.Fatal(err)
				}
				for row := range oracle {
					oracle[row][left], oracle[row][right] = oracle[row][right], oracle[row][left]
				}
				columnOrder[left], columnOrder[right] = columnOrder[right], columnOrder[left]
			case 2:
				if matrix.DenseColumns() == matrix.MaxDense() {
					continue
				}
				if _, err := matrix.FreezeNext(); err != nil {
					t.Fatal(err)
				}
			case 3:
				destination := uint32(random.Intn(rowCount))
				source := uint32(random.Intn(rowCount - 1))
				if source >= destination {
					source++
				}
				if err := matrix.XORDenseRows(destination, source); err != nil {
					t.Fatal(err)
				}
				for column := columnCount - matrix.DenseColumns(); column < columnCount; column++ {
					oracle[destination][column] = oracle[destination][column] != oracle[source][column]
				}
			}
			assertHybridEqualsOracle(t, matrix, oracle, columnOrder)
		}
	}
}

func newTestHybridBinary(
	t testing.TB,
	rows [][]uint16,
	columns, initialDense, maxDense uint32,
	dense [][2]uint32,
) *HybridBinary {
	t.Helper()
	counts := make([]uint32, len(rows))
	var nonzeros uint64
	for row := range rows {
		counts[row] = uint32(len(rows[row]))
		nonzeros += uint64(len(rows[row]))
	}
	required, err := HybridBinaryBackingBytes(uint32(len(rows)), columns, initialDense, maxDense, nonzeros)
	if err != nil {
		t.Fatal(err)
	}
	matrix, err := NewHybridBinary(counts, columns, initialDense, maxDense, required)
	if err != nil {
		t.Fatal(err)
	}
	for row := range rows {
		if err := matrix.LoadSparseRow(uint32(row), rows[row]); err != nil {
			t.Fatal(err)
		}
	}
	for _, cell := range dense {
		if err := matrix.SetInitialDense(cell[0], cell[1], true); err != nil {
			t.Fatal(err)
		}
	}
	if err := matrix.Finalize(); err != nil {
		t.Fatal(err)
	}
	return matrix
}

func assertHybridEqualsOracle(t testing.TB, matrix *HybridBinary, oracle [][]bool, columnOrder []uint32) {
	t.Helper()
	for row := uint32(0); row < matrix.Rows(); row++ {
		for column := uint32(0); column < matrix.Columns(); column++ {
			got, err := matrix.Has(row, column)
			if err != nil {
				t.Fatal(err)
			}
			if got != oracle[row][column] {
				t.Fatalf("cell (%d,%d)=%v want=%v", row, column, got, oracle[row][column])
			}
		}
	}
	for logical, original := range columnOrder {
		got, err := matrix.OriginalColumn(uint32(logical))
		if err != nil {
			t.Fatal(err)
		}
		if got != original {
			t.Fatalf("column mapping[%d]=%d want=%d", logical, got, original)
		}
	}
}

func snapshotHybrid(t testing.TB, matrix *HybridBinary) [][]bool {
	t.Helper()
	result := make([][]bool, matrix.Rows())
	for row := uint32(0); row < matrix.Rows(); row++ {
		result[row] = make([]bool, matrix.Columns())
		for column := uint32(0); column < matrix.Columns(); column++ {
			value, err := matrix.Has(row, column)
			if err != nil {
				t.Fatal(err)
			}
			result[row][column] = value
		}
	}
	return result
}
