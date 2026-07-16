package matrix

import (
	"errors"
	"math/rand"
	"slices"
	"testing"

	"github.com/fgn/raptorgo/internal/gf256"
)

func TestSparseOctetExactBudgetAndShape(t *testing.T) {
	const rows, columns, capacity = uint32(3), uint32(5), uint64(7)
	want := uint64(8*(rows+1)) + 5*capacity + 5*uint64(columns)
	required, err := SparseOctetBackingBytes(rows, columns, capacity)
	if err != nil {
		t.Fatal(err)
	}
	if required != want {
		t.Fatalf("backing = %d, want %d", required, want)
	}
	matrix, err := NewSparseOctet(rows, columns, capacity, required)
	if err != nil {
		t.Fatalf("exact budget: %v", err)
	}
	if matrix.Rows() != rows || matrix.Columns() != columns || matrix.MaxNonzeros() != capacity || matrix.Nonzeros() != 0 {
		t.Fatalf("unexpected shape: %+v", matrix)
	}
	if _, err := NewSparseOctet(rows, columns, capacity, required-1); !errors.Is(err, ErrBackingLimitExceeded) {
		t.Fatalf("budget-1 error = %v", err)
	}
}

func TestSparseOctetSetRemoveAndIteration(t *testing.T) {
	matrix := mustSparseOctet(t, 2, 6, 6)
	for _, entry := range []struct {
		column uint32
		value  byte
	}{{5, 9}, {1, 7}, {3, 11}} {
		if err := matrix.Set(0, entry.column, entry.value); err != nil {
			t.Fatal(err)
		}
	}
	if err := matrix.Set(0, 3, 0); err != nil {
		t.Fatal(err)
	}
	if err := matrix.Set(0, 5, 13); err != nil {
		t.Fatal(err)
	}
	var columns []uint32
	var values []byte
	if err := matrix.ForEachInRow(0, func(column uint32, value byte) bool {
		columns = append(columns, column)
		values = append(values, value)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(columns, []uint32{1, 5}) || !slices.Equal(values, []byte{7, 13}) {
		t.Fatalf("row = (%v,%v)", columns, values)
	}
	if matrix.Nonzeros() != 2 {
		t.Fatalf("nonzeros = %d, want 2", matrix.Nonzeros())
	}
	assertSparseOctetMatches(t, matrix, [][]byte{{0, 7, 0, 0, 0, 13}, {0, 0, 0, 0, 0, 0}})
}

func TestSparseOctetKnownRowAndColumnOperations(t *testing.T) {
	matrix := mustSparseOctet(t, 3, 4, 12)
	dense := [][]byte{
		{1, 2, 0, 4},
		{0, 3, 5, 0},
		{7, 0, 0, 9},
	}
	populateSparseOctet(t, matrix, dense)

	if err := matrix.ScaleRow(0, 17); err != nil {
		t.Fatal(err)
	}
	denseScale(dense[0], 17)
	assertSparseOctetMatches(t, matrix, dense)

	if err := matrix.AddScaledRow(1, 0, 23); err != nil {
		t.Fatal(err)
	}
	denseAddScaled(dense[1], dense[0], 23)
	assertSparseOctetMatches(t, matrix, dense)

	if err := matrix.SwapRows(0, 2); err != nil {
		t.Fatal(err)
	}
	dense[0], dense[2] = dense[2], dense[0]
	assertSparseOctetMatches(t, matrix, dense)

	if err := matrix.SwapColumns(0, 3); err != nil {
		t.Fatal(err)
	}
	for row := range dense {
		dense[row][0], dense[row][3] = dense[row][3], dense[row][0]
	}
	assertSparseOctetMatches(t, matrix, dense)
}

func TestSparseOctetCapacityRejectionIsTransactional(t *testing.T) {
	matrix := mustSparseOctet(t, 2, 4, 3)
	for _, entry := range []struct {
		row, column uint32
		value       byte
	}{{0, 0, 1}, {1, 1, 2}, {1, 2, 3}} {
		if err := matrix.Set(entry.row, entry.column, entry.value); err != nil {
			t.Fatal(err)
		}
	}
	want := [][]byte{{1, 0, 0, 0}, {0, 2, 3, 0}}
	if err := matrix.Set(0, 3, 4); !errors.Is(err, ErrNonzeroLimitExceeded) {
		t.Fatalf("Set error = %v", err)
	}
	assertSparseOctetMatches(t, matrix, want)
	if err := matrix.AddScaledRow(0, 1, 1); !errors.Is(err, ErrNonzeroLimitExceeded) {
		t.Fatalf("AddScaledRow error = %v", err)
	}
	assertSparseOctetMatches(t, matrix, want)
}

func TestSparseOctetRandomOperationTracesAgainstDense(t *testing.T) {
	for seed := int64(0); seed < 64; seed++ {
		random := rand.New(rand.NewSource(seed)) //nolint:gosec -- deterministic test generator
		const rows, columns = uint32(5), uint32(9)
		matrix := mustSparseOctet(t, rows, columns, uint64(rows)*uint64(columns))
		dense := makeDenseOctet(rows, columns)
		for step := 0; step < 500; step++ {
			switch random.Intn(5) {
			case 0:
				row, column := uint32(random.Intn(int(rows))), uint32(random.Intn(int(columns)))
				value := byte(random.Intn(256))
				if err := matrix.Set(row, column, value); err != nil {
					t.Fatalf("seed %d step %d: %v", seed, step, err)
				}
				dense[row][column] = value
			case 1:
				row := uint32(random.Intn(int(rows)))
				coefficient := byte(random.Intn(255) + 1)
				if err := matrix.ScaleRow(row, coefficient); err != nil {
					t.Fatalf("seed %d step %d: %v", seed, step, err)
				}
				denseScale(dense[row], coefficient)
			case 2:
				destination := uint32(random.Intn(int(rows)))
				source := uint32(random.Intn(int(rows - 1)))
				if source >= destination {
					source++
				}
				coefficient := byte(random.Intn(256))
				if err := matrix.AddScaledRow(destination, source, coefficient); err != nil {
					t.Fatalf("seed %d step %d: %v", seed, step, err)
				}
				denseAddScaled(dense[destination], dense[source], coefficient)
			case 3:
				left, right := uint32(random.Intn(int(rows))), uint32(random.Intn(int(rows)))
				if err := matrix.SwapRows(left, right); err != nil {
					t.Fatalf("seed %d step %d: %v", seed, step, err)
				}
				dense[left], dense[right] = dense[right], dense[left]
			case 4:
				left, right := uint32(random.Intn(int(columns))), uint32(random.Intn(int(columns)))
				if err := matrix.SwapColumns(left, right); err != nil {
					t.Fatalf("seed %d step %d: %v", seed, step, err)
				}
				for row := range dense {
					dense[row][left], dense[row][right] = dense[row][right], dense[row][left]
				}
			}
			assertSparseOctetMatches(t, matrix, dense)
		}
	}
}

func TestSparseOctetCloneOwnershipAndNoOperationAllocations(t *testing.T) {
	matrix := mustSparseOctet(t, 3, 5, 15)
	populateSparseOctet(t, matrix, [][]byte{{1, 0, 2, 0, 3}, {0, 4, 0, 5, 0}, {6, 0, 7, 0, 8}})
	required, err := SparseOctetBackingBytes(3, 5, 15)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := matrix.Clone(required)
	if err != nil {
		t.Fatal(err)
	}
	if err := clone.Set(0, 0, 99); err != nil {
		t.Fatal(err)
	}
	value, err := matrix.At(0, 0)
	if err != nil || value != 1 {
		t.Fatalf("original after clone mutation = %d, %v", value, err)
	}

	allocations := testing.AllocsPerRun(100, func() {
		if err := matrix.ScaleRow(0, 1); err != nil {
			panic(err)
		}
		if err := matrix.AddScaledRow(0, 1, 0); err != nil {
			panic(err)
		}
		if err := matrix.SwapRows(0, 0); err != nil {
			panic(err)
		}
		if err := matrix.SwapColumns(0, 0); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("no-op mutations allocated %v times", allocations)
	}
}

func TestSparseOctetValidation(t *testing.T) {
	if _, err := SparseOctetBackingBytes(0, 1, 0); !errors.Is(err, ErrInvalidDimensions) {
		t.Fatalf("zero rows error = %v", err)
	}
	if _, err := SparseOctetBackingBytes(2, 3, 7); !errors.Is(err, ErrInvalidSparseArgument) {
		t.Fatalf("excess capacity error = %v", err)
	}
	matrix := mustSparseOctet(t, 2, 3, 6)
	if err := matrix.ScaleRow(0, 0); !errors.Is(err, ErrNonInvertibleRowOp) {
		t.Fatalf("zero scale error = %v", err)
	}
	if err := matrix.AddScaledRow(0, 0, 1); !errors.Is(err, ErrNonInvertibleRowOp) {
		t.Fatalf("same row error = %v", err)
	}
	if err := matrix.ForEachInRow(0, nil); !errors.Is(err, ErrInvalidSparseArgument) {
		t.Fatalf("nil visitor error = %v", err)
	}
}

func FuzzSparseOctetAgainstDense(f *testing.F) {
	f.Add([]byte{2, 4, 0, 0, 1, 0, 1, 1, 9, 2, 0, 1, 17, 3, 0, 1, 4, 0, 3})
	f.Add([]byte{1, 1, 0, 0, 255, 2, 0, 0, 1})
	f.Fuzz(func(t *testing.T, operations []byte) {
		const rows, columns = uint32(4), uint32(7)
		matrix := mustSparseOctet(t, rows, columns, uint64(rows)*uint64(columns))
		dense := makeDenseOctet(rows, columns)
		for offset := 0; offset+4 < len(operations); offset += 5 {
			operation := operations[offset] % 5
			left := uint32(operations[offset+1])
			right := uint32(operations[offset+2])
			coefficient := operations[offset+3]
			value := operations[offset+4]
			switch operation {
			case 0:
				row, column := left%rows, right%columns
				if err := matrix.Set(row, column, value); err != nil {
					t.Fatal(err)
				}
				dense[row][column] = value
			case 1:
				row := left % rows
				coefficient |= 1
				if err := matrix.ScaleRow(row, coefficient); err != nil {
					t.Fatal(err)
				}
				denseScale(dense[row], coefficient)
			case 2:
				destination, source := left%rows, right%rows
				if source == destination {
					source = (source + 1) % rows
				}
				if err := matrix.AddScaledRow(destination, source, coefficient); err != nil {
					t.Fatal(err)
				}
				denseAddScaled(dense[destination], dense[source], coefficient)
			case 3:
				left, right = left%rows, right%rows
				if err := matrix.SwapRows(left, right); err != nil {
					t.Fatal(err)
				}
				dense[left], dense[right] = dense[right], dense[left]
			case 4:
				left, right = left%columns, right%columns
				if err := matrix.SwapColumns(left, right); err != nil {
					t.Fatal(err)
				}
				for row := range dense {
					dense[row][left], dense[row][right] = dense[row][right], dense[row][left]
				}
			}
			assertSparseOctetMatches(t, matrix, dense)
		}
	})
}

func mustSparseOctet(t testing.TB, rows, columns uint32, capacity uint64) *SparseOctet {
	t.Helper()
	required, err := SparseOctetBackingBytes(rows, columns, capacity)
	if err != nil {
		t.Fatal(err)
	}
	matrix, err := NewSparseOctet(rows, columns, capacity, required)
	if err != nil {
		t.Fatal(err)
	}
	return matrix
}

func makeDenseOctet(rows, columns uint32) [][]byte {
	dense := make([][]byte, rows)
	for row := range dense {
		dense[row] = make([]byte, columns)
	}
	return dense
}

func populateSparseOctet(t testing.TB, matrix *SparseOctet, dense [][]byte) {
	t.Helper()
	for row := range dense {
		for column, value := range dense[row] {
			if err := matrix.Set(uint32(row), uint32(column), value); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func assertSparseOctetMatches(t testing.TB, matrix *SparseOctet, dense [][]byte) {
	t.Helper()
	var nonzeros uint64
	for row := range dense {
		lastColumn := uint32(0)
		first := true
		visited := uint32(0)
		err := matrix.ForEachInRow(uint32(row), func(column uint32, value byte) bool {
			if !first && column <= lastColumn {
				t.Fatalf("row %d columns not strictly ordered: %d after %d", row, column, lastColumn)
			}
			first = false
			lastColumn = column
			visited++
			if value == 0 || dense[row][column] != value {
				t.Fatalf("row %d column %d sparse=%d dense=%d", row, column, value, dense[row][column])
			}
			return true
		})
		if err != nil {
			t.Fatal(err)
		}
		var wantWeight uint32
		for column, want := range dense[row] {
			got, err := matrix.At(uint32(row), uint32(column))
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("row %d column %d = %d, want %d", row, column, got, want)
			}
			if want != 0 {
				wantWeight++
				nonzeros++
			}
		}
		weight, err := matrix.RowWeight(uint32(row))
		if err != nil {
			t.Fatal(err)
		}
		if weight != wantWeight || visited != wantWeight {
			t.Fatalf("row %d weight=%d visited=%d want=%d", row, weight, visited, wantWeight)
		}
	}
	if matrix.Nonzeros() != nonzeros {
		t.Fatalf("nonzeros = %d, want %d", matrix.Nonzeros(), nonzeros)
	}
}

func denseScale(row []byte, coefficient byte) {
	for column := range row {
		row[column] = gf256.Mul(row[column], coefficient)
	}
}

func denseAddScaled(destination, source []byte, coefficient byte) {
	for column := range destination {
		destination[column] ^= gf256.Mul(coefficient, source[column])
	}
}
