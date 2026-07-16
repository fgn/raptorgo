package codec

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/matrix"
)

func TestInactivationRandomFullRankSystemsAgainstDenseSolve(t *testing.T) {
	t.Parallel()

	for seed := int64(0); seed < 96; seed++ {
		random := rand.New(rand.NewSource(seed)) //nolint:gosec -- deterministic algebraic corpus
		order := uint32(1 + random.Intn(8))
		overhead := uint32(random.Intn(4))
		rows := order + overhead
		symbolSize := uint32(1 + random.Intn(9))
		dense := randomInvertibleMatrix(t, random, order)
		allRows := make([][]byte, rows)
		for row := uint32(0); row < order; row++ {
			allRows[row] = denseMatrixRow(t, dense, row)
		}
		for row := order; row < rows; row++ {
			allRows[row] = make([]byte, order)
			for source := uint32(0); source < order; source++ {
				coefficient := byte(random.Intn(256))
				denseAddScaled(allRows[row], allRows[source], coefficient)
			}
		}
		coefficients := sparseOctetFromRows(t, allRows, uint64(rows)*uint64(order))
		unknown := randomSymbols(t, random, order, symbolSize)
		rightHandSide := multiplyOctetRowsBySymbols(t, allRows, unknown)
		beforeA := snapshotSparseOctet(t, coefficients)
		beforeD := snapshotSymbols(t, rightHandSide)
		initialU := uint32(random.Intn(int(order + 1)))
		required, err := InactivationWorkspaceBackingBytes(rows, order, coefficients.MaxNonzeros(), symbolSize)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		got, err := SolveInactivation(coefficients, rightHandSide, initialU, 0, 0, required)
		if err != nil {
			t.Fatalf("seed %d A=%dx%d u=%d: %v\nA=%v", seed, rows, order, initialU, err, allRows)
		}
		if !bytes.Equal(snapshotSymbols(t, got), snapshotSymbols(t, unknown)) {
			t.Fatalf("seed %d solution=%x want=%x", seed, snapshotSymbols(t, got), snapshotSymbols(t, unknown))
		}
		if !bytes.Equal(snapshotSparseOctet(t, coefficients), beforeA) || !bytes.Equal(snapshotSymbols(t, rightHandSide), beforeD) {
			t.Fatalf("seed %d solver mutated an input", seed)
		}

		denseAll := denseGF256MatrixFromRows(t, allRows)
		denseBudget, err := gf256.SolveWorkspaceBytes(rows, order, symbolSize)
		if err != nil {
			t.Fatal(err)
		}
		denseResult, err := gf256.Solve(denseAll, rightHandSide, denseBudget)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(snapshotSymbols(t, got), snapshotSymbols(t, denseResult)) {
			t.Fatalf("seed %d sparse and dense solver disagree", seed)
		}
	}
}

func TestInactivationHDPCRelocationAndFivePhaseTrace(t *testing.T) {
	t.Parallel()

	rows := [][]byte{
		{1, 1, 0, 0}, // non-HDPC
		{7, 3, 5, 9}, // HDPC row in Figure 5 position
		{0, 1, 1, 0}, // encoded
		{0, 0, 1, 1}, // encoded
		{1, 0, 0, 1}, // overhead
	}
	coefficients := sparseOctetFromRows(t, rows, 20)
	unknown := symbolsFromRows(t, [][]byte{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}, {10, 11, 12}})
	rightHandSide := multiplyOctetRowsBySymbols(t, rows, unknown)
	required, err := InactivationWorkspaceBackingBytes(5, 4, 20, 3)
	if err != nil {
		t.Fatal(err)
	}
	var states []InactivationState
	got, err := SolveInactivationObserved(coefficients, rightHandSide, 1, 1, 1, required, func(state InactivationState) error {
		states = append(states, state)
		if state.I+state.U > state.L {
			return fmt.Errorf("i+u exceeded L")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshotSymbols(t, got), snapshotSymbols(t, unknown)) {
		t.Fatalf("solution=%x want=%x", snapshotSymbols(t, got), snapshotSymbols(t, unknown))
	}
	var phases []InactivationPhase
	for _, state := range states {
		if len(phases) == 0 || phases[len(phases)-1] != state.Phase {
			phases = append(phases, state.Phase)
		}
		if state.Phase >= InactivationPhaseTwo && state.Discarded != 1 {
			t.Fatalf("phase %d discarded=%d, want 1", state.Phase, state.Discarded)
		}
	}
	if !slices.Equal(phases, []InactivationPhase{
		InactivationPhaseOne,
		InactivationPhaseTwo,
		InactivationPhaseThree,
		InactivationPhaseFour,
		InactivationPhaseFive,
	}) {
		t.Fatalf("phase sequence = %v", phases)
	}
	if final := states[len(states)-1]; final.I+final.U != final.L || final.Discarded != final.M-final.L {
		t.Fatalf("final state = %+v", final)
	}
}

func TestInactivationDegreeTwoChoosesMaximumComponent(t *testing.T) {
	t.Parallel()

	rows := [][]byte{
		{1, 1, 0, 0, 0, 0},
		{0, 1, 1, 0, 0, 0},
		{0, 0, 0, 1, 1, 0},
		{0, 0, 0, 1, 1, 0},
	}
	working := sparseOctetFromRows(t, rows, 24)
	degrees := []uint32{2, 2, 1, 1} // must be ignored for the r=2 graph choice
	parents, sizes, columns := make([]uint32, 6), make([]uint32, 6), make([]uint32, 6)
	chosen, r, err := selectInactivationRow(working, 0, 4, 6, degrees, make([]byte, 4), parents, sizes, columns)
	if err != nil {
		t.Fatal(err)
	}
	if r != 2 || chosen != 0 {
		t.Fatalf("chosen row/r = %d/%d, want 0/2 from maximum component", chosen, r)
	}
}

func TestInactivationSelectionDefersHDPCAndExcludesItFromDegreeTwoGraph(t *testing.T) {
	t.Parallel()

	working := sparseOctetFromRows(t, [][]byte{
		{1, 0, 0, 0}, // eligible non-HDPC degree one
		{0, 1, 1, 0}, // HDPC degree two
	}, 8)
	parents, sizes, columns := make([]uint32, 4), make([]uint32, 4), make([]uint32, 4)
	chosen, r, err := selectInactivationRow(
		working, 0, 2, 4, []uint32{1, 2}, []byte{0, 1}, parents, sizes, columns,
	)
	if err != nil {
		t.Fatal(err)
	}
	if chosen != 0 || r != 1 {
		t.Fatalf("non-HDPC-first chosen/r=%d/%d, want 0/1", chosen, r)
	}

	allHDPC := sparseOctetFromRows(t, [][]byte{
		{1, 1, 0, 0, 0},
		{0, 1, 1, 0, 0},
		{0, 0, 0, 1, 1},
	}, 15)
	parents, sizes, columns = make([]uint32, 5), make([]uint32, 5), make([]uint32, 5)
	chosen, r, err = selectInactivationRow(
		allHDPC, 0, 3, 5, []uint32{5, 5, 1}, []byte{1, 1, 1}, parents, sizes, columns,
	)
	if err != nil {
		t.Fatal(err)
	}
	if chosen != 2 || r != 2 {
		t.Fatalf("HDPC fallback chosen/r=%d/%d, want 2/2 by original degree without graph edges", chosen, r)
	}
}

func TestInactivationBudgetRankConflictFillAndObserverRejections(t *testing.T) {
	t.Parallel()

	coefficients := sparseOctetFromRows(t, [][]byte{{1, 0}, {0, 1}}, 4)
	rightHandSide := symbolsFromRows(t, [][]byte{{7}, {9}})
	required, err := InactivationWorkspaceBackingBytes(2, 2, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := SolveInactivation(coefficients, rightHandSide, 0, 0, 0, required-1); !errors.Is(err, ErrInactivationWorkspace) || result != nil {
		t.Fatalf("budget-1 result=%v error=%v", result, err)
	}
	if result, err := SolveInactivationObserved(coefficients, rightHandSide, 0, 0, 0, required, func(InactivationState) error {
		return errors.New("stop")
	}); !errors.Is(err, ErrInactivationObserver) || result != nil {
		t.Fatalf("observer result=%v error=%v", result, err)
	}

	singular := sparseOctetFromRows(t, [][]byte{{1, 0}, {1, 0}}, 4)
	if result, err := SolveInactivation(singular, rightHandSide, 0, 0, 0, required); !errors.Is(err, ErrInactivationConflict) || result != nil {
		t.Fatalf("inconsistent rank-deficient result=%v error=%v", result, err)
	}
	consistentRightHandSide := symbolsFromRows(t, [][]byte{{7}, {7}})
	if result, err := SolveInactivation(singular, consistentRightHandSide, 0, 0, 0, required); !errors.Is(err, ErrInactivationRank) || result != nil {
		t.Fatalf("consistent rank-deficient result=%v error=%v", result, err)
	}

	conflictA := sparseOctetFromRows(t, [][]byte{{1}, {1}}, 2)
	conflictD := symbolsFromRows(t, [][]byte{{0}, {1}})
	conflictBudget, err := InactivationWorkspaceBackingBytes(2, 1, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := SolveInactivation(conflictA, conflictD, 1, 0, 0, conflictBudget); !errors.Is(err, ErrInactivationConflict) || result != nil {
		t.Fatalf("conflict result=%v error=%v", result, err)
	}
	hiddenConflictA := sparseOctetFromRows(t, [][]byte{{0, 1}, {0, 1}}, 4)
	hiddenConflictD := symbolsFromRows(t, [][]byte{{0}, {1}})
	if result, err := SolveInactivation(hiddenConflictA, hiddenConflictD, 1, 0, 0, required); !errors.Is(err, ErrInactivationConflict) || result != nil {
		t.Fatalf("dependent conflict result=%v error=%v", result, err)
	}

	fillRows := [][]byte{
		{1, 1, 1, 0, 0},
		{1, 0, 0, 1, 1},
		{0, 1, 0, 1, 1},
		{0, 0, 1, 1, 1},
		{1, 1, 0, 0, 1},
	}
	var nonzeros uint64
	for _, row := range fillRows {
		for _, value := range row {
			if value != 0 {
				nonzeros++
			}
		}
	}
	fillA := sparseOctetFromRows(t, fillRows, nonzeros)
	fillD := symbolsFromRows(t, [][]byte{{0}, {0}, {0}, {0}, {0}})
	fillBudget, err := InactivationWorkspaceBackingBytes(5, 5, nonzeros, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := SolveInactivation(fillA, fillD, 0, 0, 0, fillBudget); !errors.Is(err, ErrInactivationFill) || result != nil {
		t.Fatalf("fill result=%v error=%v", result, err)
	}
}

func TestInactivationUsesHDPCFallbackWhenNoNonHDPCCandidateExists(t *testing.T) {
	t.Parallel()

	rows := [][]byte{{0, 1}, {1, 0}}
	coefficients := sparseOctetFromRows(t, rows, 4)
	unknown := symbolsFromRows(t, [][]byte{{7}, {9}})
	rightHandSide := multiplyOctetRowsBySymbols(t, rows, unknown)
	required, err := InactivationWorkspaceBackingBytes(2, 2, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	got, err := SolveInactivation(coefficients, rightHandSide, 1, 1, 1, required)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshotSymbols(t, got), snapshotSymbols(t, unknown)) {
		t.Fatalf("solution=%x want=%x", snapshotSymbols(t, got), snapshotSymbols(t, unknown))
	}
}

func FuzzInactivationAgainstKnownSolution(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9})
	f.Add([]byte{255, 0, 19, 31, 127, 63})
	f.Fuzz(func(t *testing.T, data []byte) {
		const order = uint32(4)
		rows := [][]byte{
			{1, dataByte(data, 0), dataByte(data, 1), dataByte(data, 2)},
			{0, 1, dataByte(data, 3), dataByte(data, 4)},
			{0, 0, 1, dataByte(data, 5)},
			{0, 0, 0, 1},
			{1, 1, 1, 1},
		}
		coefficients := sparseOctetFromRows(t, rows, 20)
		unknown := symbolsFromRows(t, [][]byte{{dataByte(data, 6), 1}, {dataByte(data, 7), 2}, {dataByte(data, 8), 3}, {17, 4}})
		rightHandSide := multiplyOctetRowsBySymbols(t, rows, unknown)
		required, err := InactivationWorkspaceBackingBytes(5, order, 20, 2)
		if err != nil {
			t.Fatal(err)
		}
		for initialU := uint32(0); initialU <= order; initialU++ {
			got, err := SolveInactivation(coefficients, rightHandSide, initialU, 0, 0, required)
			if err != nil {
				t.Fatalf("u=%d: %v", initialU, err)
			}
			if !bytes.Equal(snapshotSymbols(t, got), snapshotSymbols(t, unknown)) {
				t.Fatalf("u=%d got=%x want=%x", initialU, snapshotSymbols(t, got), snapshotSymbols(t, unknown))
			}
		}
	})
}

func sparseOctetFromRows(t testing.TB, rows [][]byte, capacity uint64) *matrix.SparseOctet {
	t.Helper()
	if len(rows) == 0 || len(rows[0]) == 0 {
		t.Fatal("empty rows")
	}
	required, err := matrix.SparseOctetBackingBytes(uint32(len(rows)), uint32(len(rows[0])), capacity)
	if err != nil {
		t.Fatal(err)
	}
	result, err := matrix.NewSparseOctet(uint32(len(rows)), uint32(len(rows[0])), capacity, required)
	if err != nil {
		t.Fatal(err)
	}
	for row := range rows {
		if len(rows[row]) != len(rows[0]) {
			t.Fatal("ragged rows")
		}
		for column, value := range rows[row] {
			if err := result.Set(uint32(row), uint32(column), value); err != nil {
				t.Fatal(err)
			}
		}
	}
	return result
}

func randomInvertibleMatrix(t testing.TB, random *rand.Rand, order uint32) *gf256.Matrix {
	t.Helper()
	rows := make([][]byte, order)
	for row := uint32(0); row < order; row++ {
		rows[row] = make([]byte, order)
		rows[row][row] = 1
	}
	for step := uint32(0); step < order*8+1; step++ {
		left, right := uint32(random.Intn(int(order))), uint32(random.Intn(int(order)))
		if left == right {
			coefficient := byte(random.Intn(255) + 1)
			for column := range rows[left] {
				rows[left][column] = gf256.Mul(rows[left][column], coefficient)
			}
			continue
		}
		if random.Intn(2) == 0 {
			rows[left], rows[right] = rows[right], rows[left]
		} else {
			denseAddScaled(rows[left], rows[right], byte(random.Intn(255)+1))
		}
	}
	return denseGF256MatrixFromRows(t, rows)
}

func denseAddScaled(destination, source []byte, coefficient byte) {
	for column := range destination {
		destination[column] ^= gf256.Mul(coefficient, source[column])
	}
}

func denseGF256MatrixFromRows(t testing.TB, rows [][]byte) *gf256.Matrix {
	t.Helper()
	required := uint64(len(rows)) * uint64(len(rows[0]))
	result, err := gf256.NewMatrix(uint32(len(rows)), uint32(len(rows[0])), required)
	if err != nil {
		t.Fatal(err)
	}
	for row := range rows {
		for column, value := range rows[row] {
			if err := result.Set(uint32(row), uint32(column), value); err != nil {
				t.Fatal(err)
			}
		}
	}
	return result
}

func denseMatrixRow(t testing.TB, source *gf256.Matrix, row uint32) []byte {
	t.Helper()
	result := make([]byte, source.Columns())
	for column := uint32(0); column < source.Columns(); column++ {
		value, err := source.At(row, column)
		if err != nil {
			t.Fatal(err)
		}
		result[column] = value
	}
	return result
}

func randomSymbols(t testing.TB, random *rand.Rand, count, size uint32) *gf256.Symbols {
	t.Helper()
	result, err := gf256.NewSymbols(count, size, uint64(count)*uint64(size))
	if err != nil {
		t.Fatal(err)
	}
	for row := uint32(0); row < count; row++ {
		values, err := result.Symbol(row)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := random.Read(values); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func symbolsFromRows(t testing.TB, rows [][]byte) *gf256.Symbols {
	t.Helper()
	result, err := gf256.NewSymbols(uint32(len(rows)), uint32(len(rows[0])), uint64(len(rows))*uint64(len(rows[0])))
	if err != nil {
		t.Fatal(err)
	}
	for row, values := range rows {
		if err := result.SetSymbol(uint32(row), values); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func multiplyOctetRowsBySymbols(t testing.TB, rows [][]byte, symbols *gf256.Symbols) *gf256.Symbols {
	t.Helper()
	result, err := gf256.NewSymbols(uint32(len(rows)), symbols.Size(), uint64(len(rows))*uint64(symbols.Size()))
	if err != nil {
		t.Fatal(err)
	}
	for row := range rows {
		destination, err := result.Symbol(uint32(row))
		if err != nil {
			t.Fatal(err)
		}
		for column, coefficient := range rows[row] {
			source, err := symbols.Symbol(uint32(column))
			if err != nil {
				t.Fatal(err)
			}
			if err := gf256.MulAdd(destination, coefficient, source); err != nil {
				t.Fatal(err)
			}
		}
	}
	return result
}

func snapshotSparseOctet(t testing.TB, source *matrix.SparseOctet) []byte {
	t.Helper()
	result := make([]byte, int(uint64(source.Rows())*uint64(source.Columns())))
	for row := uint32(0); row < source.Rows(); row++ {
		for column := uint32(0); column < source.Columns(); column++ {
			value, err := source.At(row, column)
			if err != nil {
				t.Fatal(err)
			}
			result[uint64(row)*uint64(source.Columns())+uint64(column)] = value
		}
	}
	return result
}

func dataByte(data []byte, index int) byte {
	if len(data) == 0 {
		return byte(index*29 + 1)
	}
	return data[index%len(data)]
}
