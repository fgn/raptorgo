package matrix

import (
	"errors"
	"math/rand"
	"reflect"
	"testing"
)

func TestSparseScheduleAggregateBudgetAndIdentity(t *testing.T) {
	t.Parallel()

	required, err := SparseScheduleBackingBytes(5, 7, 20)
	if err != nil {
		t.Fatal(err)
	}
	matrixBytes, err := SparseBinaryBackingBytes(5, 7, 20)
	if err != nil {
		t.Fatal(err)
	}
	if want := matrixBytes + 5*4 + 7*4; required != want {
		t.Fatalf("schedule bytes = %d, want %d", required, want)
	}
	if schedule, err := NewSparseSchedule(5, 7, 20, required-1); !errors.Is(err, ErrBackingLimitExceeded) || schedule != nil {
		t.Fatalf("under-budget schedule = %v, %v", schedule, err)
	}
	schedule, err := NewSparseSchedule(5, 7, 20, required)
	if err != nil {
		t.Fatal(err)
	}
	if schedule.BackingBytes() != required {
		t.Fatalf("retained backing = %d, want %d", schedule.BackingBytes(), required)
	}
	assertScheduleMappings(t, schedule, []uint32{0, 1, 2, 3, 4}, []uint32{0, 1, 2, 3, 4, 5, 6})
}

func TestSparseScheduleCouplesSwapsAndPreservesMappingsOnRejection(t *testing.T) {
	t.Parallel()

	required, err := SparseScheduleBackingBytes(4, 5, 20)
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := NewSparseSchedule(4, 5, 20, required)
	if err != nil {
		t.Fatal(err)
	}
	dense := newDenseBinary(4, 5)
	for _, cell := range [][2]uint32{{0, 0}, {0, 2}, {1, 1}, {2, 3}, {3, 0}, {3, 4}} {
		if err := schedule.Set(cell[0], cell[1], true); err != nil {
			t.Fatal(err)
		}
		dense[cell[0]][cell[1]] = true
	}
	if err := schedule.SwapRows(0, 3); err != nil {
		t.Fatal(err)
	}
	dense[0], dense[3] = dense[3], dense[0]
	assertScheduleMappings(t, schedule, []uint32{3, 1, 2, 0}, []uint32{0, 1, 2, 3, 4})
	assertSparseEqualsDense(t, schedule.matrix, dense)

	if err := schedule.SwapColumns(1, 4); err != nil {
		t.Fatal(err)
	}
	swapDenseColumns(dense, 1, 4)
	assertScheduleMappings(t, schedule, []uint32{3, 1, 2, 0}, []uint32{0, 4, 2, 3, 1})
	assertSparseEqualsDense(t, schedule.matrix, dense)

	if err := schedule.AddRow(2, 0); err != nil {
		t.Fatal(err)
	}
	addDenseBinaryRow(dense, 2, 0)
	assertScheduleMappings(t, schedule, []uint32{3, 1, 2, 0}, []uint32{0, 4, 2, 3, 1})
	assertSparseEqualsDense(t, schedule.matrix, dense)

	rowBefore, columnBefore := snapshotScheduleMappings(t, schedule)
	if err := schedule.SwapRows(0, 4); !errors.Is(err, ErrIndex) {
		t.Fatalf("invalid row swap error = %v", err)
	}
	if err := schedule.SwapColumns(0, 5); !errors.Is(err, ErrIndex) {
		t.Fatalf("invalid column swap error = %v", err)
	}
	rowAfter, columnAfter := snapshotScheduleMappings(t, schedule)
	if !reflect.DeepEqual(rowAfter, rowBefore) || !reflect.DeepEqual(columnAfter, columnBefore) {
		t.Fatal("rejected schedule operation mutated a mapping")
	}
	assertSparseEqualsDense(t, schedule.matrix, dense)
}

func TestSparseScheduleRandomMappingRoundTrips(t *testing.T) {
	t.Parallel()

	const rows, columns = uint32(17), uint32(23)
	required, err := SparseScheduleBackingBytes(rows, columns, 0)
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := NewSparseSchedule(rows, columns, 0, required)
	if err != nil {
		t.Fatal(err)
	}
	random := rand.New(rand.NewSource(731)) //nolint:gosec -- deterministic permutation trace
	for step := 0; step < 2_000; step++ {
		if random.Intn(2) == 0 {
			if err := schedule.SwapRows(uint32(random.Intn(int(rows))), uint32(random.Intn(int(rows)))); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := schedule.SwapColumns(uint32(random.Intn(int(columns))), uint32(random.Intn(int(columns)))); err != nil {
				t.Fatal(err)
			}
		}
		for current := uint32(0); current < rows; current++ {
			original, err := schedule.OriginalRow(current)
			if err != nil {
				t.Fatal(err)
			}
			roundTrip, err := schedule.CurrentRow(original)
			if err != nil || roundTrip != current {
				t.Fatalf("step %d row %d -> %d -> %d, %v", step, current, original, roundTrip, err)
			}
		}
		for current := uint32(0); current < columns; current++ {
			original, err := schedule.OriginalColumn(current)
			if err != nil {
				t.Fatal(err)
			}
			roundTrip, err := schedule.CurrentColumn(original)
			if err != nil || roundTrip != current {
				t.Fatalf("step %d column %d -> %d -> %d, %v", step, current, original, roundTrip, err)
			}
		}
	}
}

func assertScheduleMappings(t *testing.T, schedule *SparseSchedule, wantRows, wantColumns []uint32) {
	t.Helper()
	rows, columns := snapshotScheduleMappings(t, schedule)
	if !reflect.DeepEqual(rows, wantRows) || !reflect.DeepEqual(columns, wantColumns) {
		t.Fatalf("schedule mappings rows=%v columns=%v, want rows=%v columns=%v", rows, columns, wantRows, wantColumns)
	}
	assertBijection(t, rows)
	assertBijection(t, columns)
}

func snapshotScheduleMappings(t *testing.T, schedule *SparseSchedule) ([]uint32, []uint32) {
	t.Helper()
	rows := make([]uint32, schedule.Rows())
	for row := range rows {
		value, err := schedule.OriginalRow(uint32(row))
		if err != nil {
			t.Fatal(err)
		}
		rows[row] = value
	}
	columns := make([]uint32, schedule.Columns())
	for column := range columns {
		value, err := schedule.OriginalColumn(uint32(column))
		if err != nil {
			t.Fatal(err)
		}
		columns[column] = value
	}
	return rows, columns
}

func assertBijection(t *testing.T, values []uint32) {
	t.Helper()
	seen := make([]bool, len(values))
	for _, value := range values {
		if value >= uint32(len(values)) || seen[value] {
			t.Fatalf("not a bijection: %v", values)
		}
		seen[value] = true
	}
}
