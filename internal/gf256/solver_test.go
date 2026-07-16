package gf256

import (
	"bytes"
	"errors"
	"math"
	"math/rand"
	"testing"
)

func TestSolveKnownMultiByteSystemWithPivotSwaps(t *testing.T) {
	t.Parallel()

	coefficients := matrixFromRows(t, [][]byte{
		{0, 1, 2},
		{0, 0, 3},
		{4, 5, 6},
	})
	want := symbolsFromRows(t, [][]byte{
		{1, 2, 3, 4},
		{5, 6, 7, 8},
		{9, 10, 11, 12},
	})
	rightHandSide := multiplySystem(t, coefficients, want)
	coefficientBefore := bytes.Clone(coefficients.values)
	rightHandSideBefore := bytes.Clone(rightHandSide.values)
	required, err := SolveWorkspaceBytes(3, 3, 4)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Solve(coefficients, rightHandSide, required)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.values, want.values) {
		t.Fatalf("solution = %v, want %v", got.values, want.values)
	}
	assertSystemEquals(t, coefficients, got, rightHandSide)
	if !bytes.Equal(coefficients.values, coefficientBefore) || !bytes.Equal(rightHandSide.values, rightHandSideBefore) {
		t.Fatal("Solve mutated an input")
	}
}

func TestSolveRejectsInvalidShapeAndBudgetWithoutMutation(t *testing.T) {
	t.Parallel()

	square := matrixFromRows(t, [][]byte{{1, 2}, {3, 4}})
	nonsquare := matrixFromRows(t, [][]byte{{1, 2, 3}, {4, 5, 6}})
	rightHandSide := symbolsFromRows(t, [][]byte{{7, 8}, {9, 10}})
	wrongRows := symbolsFromRows(t, [][]byte{{7, 8}})
	squareBefore := bytes.Clone(square.values)
	rightHandSideBefore := bytes.Clone(rightHandSide.values)

	cases := []struct {
		name         string
		coefficients *Matrix
		rhs          *Symbols
		budget       uint64
		want         error
	}{
		{"nil coefficients", nil, rightHandSide, math.MaxUint64, ErrInvalidSolverArgument},
		{"wide non-unique", nonsquare, rightHandSide, math.MaxUint64, ErrNoUniqueSolution},
		{"nil rhs", square, nil, math.MaxUint64, ErrRightHandSideShape},
		{"wrong rows", square, wrongRows, math.MaxUint64, ErrRightHandSideShape},
		{"one byte short", square, rightHandSide, 11, ErrSolverWorkspaceLimit},
	}
	for _, test := range cases {
		_, err := Solve(test.coefficients, test.rhs, test.budget)
		if !errors.Is(err, test.want) {
			t.Errorf("%s error = %v, want %v", test.name, err, test.want)
		}
		if test.name == "wide non-unique" && errors.Is(err, ErrSingularMatrix) {
			t.Errorf("wide full-row-rank system was misclassified as singular: %v", err)
		}
	}
	if !bytes.Equal(square.values, squareBefore) || !bytes.Equal(rightHandSide.values, rightHandSideBefore) {
		t.Fatal("rejected Solve mutated an input")
	}
	if _, err := SolveWorkspaceBytes(0, 1, 1); !errors.Is(err, ErrInvalidSolverArgument) {
		t.Fatalf("zero-order workspace error = %v", err)
	}
	if _, err := SolveWorkspaceBytes(1, 0, 1); !errors.Is(err, ErrInvalidSolverArgument) {
		t.Fatalf("zero-column workspace error = %v", err)
	}
	if _, err := SolveWorkspaceBytes(1, 1, 0); !errors.Is(err, ErrInvalidSolverArgument) {
		t.Fatalf("zero-size workspace error = %v", err)
	}
	if _, err := SolveWorkspaceBytes(math.MaxUint32, math.MaxUint32, math.MaxUint32); !errors.Is(err, ErrSolverWorkspaceLimit) {
		t.Fatalf("overflowing workspace error = %v", err)
	}
}

func TestSolveRejectsConsistentAndInconsistentSingularSystems(t *testing.T) {
	t.Parallel()

	coefficients := matrixFromRows(t, [][]byte{{1, 2}, {2, 4}})
	consistent := symbolsFromRows(t, [][]byte{{5}, {10}})
	inconsistent := symbolsFromRows(t, [][]byte{{5}, {11}})
	coefficientBefore := bytes.Clone(coefficients.values)

	for name, rhs := range map[string]*Symbols{"consistent": consistent, "inconsistent": inconsistent} {
		rightHandSideBefore := bytes.Clone(rhs.values)
		_, err := Solve(coefficients, rhs, 8)
		want := ErrNoUniqueSolution
		if name == "inconsistent" {
			want = ErrInconsistentSystem
		}
		if !errors.Is(err, want) {
			t.Errorf("%s singular error = %v, want %v", name, err, want)
		}
		if !bytes.Equal(coefficients.values, coefficientBefore) || !bytes.Equal(rhs.values, rightHandSideBefore) {
			t.Fatalf("%s singular Solve mutated an input", name)
		}
	}
}

func TestSolveExhaustiveTwoByTwoSubset(t *testing.T) {
	t.Parallel()

	fieldSubset := []byte{0, 1, 2, 3}
	want := symbolsFromRows(t, [][]byte{{17, 31}, {93, 201}})
	for _, a := range fieldSubset {
		for _, b := range fieldSubset {
			for _, c := range fieldSubset {
				for _, d := range fieldSubset {
					coefficients := matrixFromRows(t, [][]byte{{a, b}, {c, d}})
					rightHandSide := multiplySystem(t, coefficients, want)
					got, err := Solve(coefficients, rightHandSide, 12)
					determinant := polynomialMul(a, d) ^ polynomialMul(b, c)
					if determinant == 0 {
						if !errors.Is(err, ErrNoUniqueSolution) {
							t.Fatalf("matrix %v: error = %v, want singular", coefficients.values, err)
						}
						continue
					}
					if err != nil {
						t.Fatalf("matrix %v: %v", coefficients.values, err)
					}
					if !bytes.Equal(got.values, want.values) {
						t.Fatalf("matrix %v: solution = %v, want %v", coefficients.values, got.values, want.values)
					}
					assertSystemEquals(t, coefficients, got, rightHandSide)
				}
			}
		}
	}
}

func TestSolveRandomConstructedInvertibleSystems(t *testing.T) {
	t.Parallel()

	random := rand.New(rand.NewSource(0x6330))
	for iteration := 0; iteration < 100; iteration++ {
		order := uint32(random.Intn(15) + 1)
		symbolSize := uint32(random.Intn(31) + 1)
		coefficients := identityMatrix(t, order)
		for step := 0; step < int(order)*4; step++ {
			left := uint32(random.Intn(int(order)))
			right := uint32(random.Intn(int(order)))
			if left == right {
				coefficient := byte(random.Intn(255) + 1)
				if err := coefficients.ScaleRow(left, coefficient); err != nil {
					t.Fatal(err)
				}
				continue
			}
			if err := coefficients.AddScaledRow(left, right, byte(random.Intn(256))); err != nil {
				t.Fatal(err)
			}
		}
		want, err := NewSymbols(order, symbolSize, uint64(order)*uint64(symbolSize))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := random.Read(want.values); err != nil {
			t.Fatal(err)
		}
		rightHandSide := multiplySystem(t, coefficients, want)
		required, err := SolveWorkspaceBytes(order, order, symbolSize)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Solve(coefficients, rightHandSide, required)
		if err != nil {
			t.Fatalf("iteration %d, order %d: %v", iteration, order, err)
		}
		if !bytes.Equal(got.values, want.values) {
			t.Fatalf("iteration %d, order %d: wrong solution", iteration, order)
		}
		assertSystemEquals(t, coefficients, got, rightHandSide)
	}
}

func TestSolveOverdeterminedAndMixedLaneInconsistency(t *testing.T) {
	t.Parallel()

	coefficients := matrixFromRows(t, [][]byte{
		{1, 0},
		{0, 1},
		{1, 1},
		{2, 3},
	})
	want := symbolsFromRows(t, [][]byte{{7, 11, 13}, {17, 19, 23}})
	rightHandSide := multiplySystem(t, coefficients, want)
	required, err := SolveWorkspaceBytes(4, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Solve(coefficients, rightHandSide, required)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.values, want.values) {
		t.Fatalf("solution = %v, want %v", got.values, want.values)
	}

	// Perturb only one lane of a dependent equation. The system as a whole is
	// inconsistent even though the other two lanes still have solutions.
	inconsistent := symbolsFromRows(t, [][]byte{
		bytes.Clone(rightHandSide.values[0:3]),
		bytes.Clone(rightHandSide.values[3:6]),
		bytes.Clone(rightHandSide.values[6:9]),
		bytes.Clone(rightHandSide.values[9:12]),
	})
	inconsistent.values[10] ^= 1
	before := bytes.Clone(inconsistent.values)
	if _, err := Solve(coefficients, inconsistent, required); !errors.Is(err, ErrInconsistentSystem) {
		t.Fatalf("mixed-lane error = %v", err)
	}
	if !bytes.Equal(inconsistent.values, before) {
		t.Fatal("inconsistent Solve mutated right-hand side")
	}
}

func TestSolveClonesSharedInputBackingAndReturnsIndependentOutput(t *testing.T) {
	t.Parallel()

	backing := []byte{1, 0, 0, 1, 7, 8}
	coefficients := &Matrix{rows: 2, columns: 2, values: backing[0:4]}
	rightHandSide := &Symbols{count: 2, size: 2, values: backing[2:6]}
	coefficientBefore := bytes.Clone(coefficients.values)
	rightHandSideBefore := bytes.Clone(rightHandSide.values)
	got, err := Solve(coefficients, rightHandSide, 12)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.values, rightHandSideBefore) {
		t.Fatalf("solution = %v, want %v", got.values, rightHandSideBefore)
	}
	got.values[0] ^= 0xff
	if !bytes.Equal(coefficients.values, coefficientBefore) || !bytes.Equal(rightHandSide.values, rightHandSideBefore) {
		t.Fatal("output aliases an input")
	}
}

func TestRankRectangularAndLimits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		rows [][]byte
		want uint32
	}{
		{"wide full row rank", [][]byte{{1, 0, 2}, {0, 1, 3}}, 2},
		{"tall full column rank", [][]byte{{1, 0}, {0, 1}, {1, 1}}, 2},
		{"duplicate and zero", [][]byte{{1, 2, 3}, {1, 2, 3}, {0, 0, 0}}, 1},
		{"late pivots", [][]byte{{0, 0, 1}, {0, 2, 0}, {0, 0, 0}}, 2},
	}
	for _, test := range cases {
		matrix := matrixFromRows(t, test.rows)
		before := bytes.Clone(matrix.values)
		got, err := Rank(matrix, matrix.Bytes())
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if got != test.want {
			t.Errorf("%s: rank = %d, want %d", test.name, got, test.want)
		}
		if !bytes.Equal(matrix.values, before) {
			t.Fatalf("%s: Rank mutated input", test.name)
		}
		if _, err := Rank(matrix, matrix.Bytes()-1); !errors.Is(err, ErrSolverWorkspaceLimit) {
			t.Fatalf("%s: budget error = %v", test.name, err)
		}
	}
	if _, err := Rank(nil, math.MaxUint64); !errors.Is(err, ErrInvalidSolverArgument) {
		t.Fatalf("nil Rank error = %v", err)
	}
}

func TestRankElementaryInvariantsAndTranspose(t *testing.T) {
	t.Parallel()

	original := matrixFromRows(t, [][]byte{
		{1, 2, 3, 4},
		{5, 6, 7, 8},
		{9, 10, 11, 12},
	})
	want, err := Rank(original, original.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	transposed := transposeMatrix(t, original)
	transposedRank, err := Rank(transposed, transposed.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if transposedRank != want {
		t.Fatalf("rank(transpose) = %d, want %d", transposedRank, want)
	}
	transformed := matrixFromRows(t, [][]byte{
		{1, 2, 3, 4},
		{5, 6, 7, 8},
		{9, 10, 11, 12},
	})
	if err := transformed.SwapRows(0, 2); err != nil {
		t.Fatal(err)
	}
	if err := transformed.ScaleRow(1, 37); err != nil {
		t.Fatal(err)
	}
	if err := transformed.AddScaledRow(2, 0, 201); err != nil {
		t.Fatal(err)
	}
	got, err := Rank(transformed, transformed.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("rank after elementary operations = %d, want %d", got, want)
	}
}

func TestInvertExhaustiveOneByOne(t *testing.T) {
	t.Parallel()

	for value := 0; value <= math.MaxUint8; value++ {
		matrix := matrixFromRows(t, [][]byte{{byte(value)}})
		before := bytes.Clone(matrix.values)
		inverse, err := Invert(matrix, 4)
		if value == 0 {
			if !errors.Is(err, ErrSingularMatrix) || inverse != nil {
				t.Fatalf("Invert([0]) = %v, %v", inverse, err)
			}
			if !bytes.Equal(matrix.values, before) {
				t.Fatal("singular Invert mutated input")
			}
			continue
		}
		if err != nil {
			t.Fatalf("Invert([%d]): %v", value, err)
		}
		if polynomialMul(byte(value), inverse.values[0]) != 1 {
			t.Fatalf("%d * %d != 1", value, inverse.values[0])
		}
		if !bytes.Equal(matrix.values, before) {
			t.Fatalf("Invert([%d]) mutated input", value)
		}
	}
}

func TestInvertTwoSidedIdentityAndLimits(t *testing.T) {
	t.Parallel()

	matrix := matrixFromRows(t, [][]byte{
		{0, 1, 2, 3},
		{0, 0, 4, 5},
		{0, 0, 0, 6},
		{7, 8, 9, 10},
	})
	required, err := InvertWorkspaceBytes(4)
	if err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(matrix.values)
	if inverse, err := Invert(matrix, required-1); !errors.Is(err, ErrSolverWorkspaceLimit) || inverse != nil {
		t.Fatalf("under-budget Invert = %v, %v", inverse, err)
	}
	if !bytes.Equal(matrix.values, before) {
		t.Fatal("under-budget Invert mutated input")
	}
	inverse, err := Invert(matrix, required)
	if err != nil {
		t.Fatal(err)
	}
	assertIdentityMatrix(t, multiplyMatrices(t, matrix, inverse))
	assertIdentityMatrix(t, multiplyMatrices(t, inverse, matrix))
	if !bytes.Equal(matrix.values, before) {
		t.Fatal("Invert mutated input")
	}

	nonsquare := matrixFromRows(t, [][]byte{{1, 2, 3}, {4, 5, 6}})
	if _, err := Invert(nonsquare, math.MaxUint64); !errors.Is(err, ErrMatrixNotSquare) {
		t.Fatalf("nonsquare Invert error = %v", err)
	}
	if _, err := Invert(nil, math.MaxUint64); !errors.Is(err, ErrInvalidSolverArgument) {
		t.Fatalf("nil Invert error = %v", err)
	}
}

func FuzzSolveConstructedInvertible(f *testing.F) {
	f.Add(uint8(4), uint8(8), []byte("raptorq solver seed"))
	f.Fuzz(func(t *testing.T, orderSeed, sizeSeed uint8, data []byte) {
		order := uint32(orderSeed%8) + 1
		symbolSize := uint32(sizeSeed%32) + 1
		coefficients := identityMatrix(t, order)
		for index, value := range data {
			left := uint32(index) % order
			right := uint32(value) % order
			if left == right {
				coefficient := value
				if coefficient == 0 {
					coefficient = 1
				}
				if err := coefficients.ScaleRow(left, coefficient); err != nil {
					t.Fatal(err)
				}
				continue
			}
			if err := coefficients.AddScaledRow(left, right, byte(index)); err != nil {
				t.Fatal(err)
			}
		}
		want, err := NewSymbols(order, symbolSize, uint64(order)*uint64(symbolSize))
		if err != nil {
			t.Fatal(err)
		}
		for index := range want.values {
			if len(data) == 0 {
				want.values[index] = byte(index)
			} else {
				want.values[index] = data[index%len(data)]
			}
		}
		rightHandSide := multiplySystem(t, coefficients, want)
		required, err := SolveWorkspaceBytes(order, order, symbolSize)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Solve(coefficients, rightHandSide, required)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.values, want.values) {
			t.Fatal("wrong solution")
		}
		assertSystemEquals(t, coefficients, got, rightHandSide)
	})
}

func FuzzSolveArbitraryRectangularAgainstPolynomialOracle(f *testing.F) {
	f.Add(uint8(2), uint8(2), uint8(2), []byte{1, 0, 0, 1, 7, 8, 9, 10})
	f.Add(uint8(2), uint8(3), uint8(1), []byte{1, 2, 1, 2, 0, 0, 5, 10, 1})
	f.Add(uint8(3), uint8(2), uint8(3), []byte{1, 0, 0, 1, 1, 1, 7, 8, 9, 10, 11, 12})
	f.Fuzz(func(t *testing.T, rowSeed, columnSeed, sizeSeed uint8, data []byte) {
		rows := uint32(rowSeed%8) + 1
		columns := uint32(columnSeed%8) + 1
		symbolSize := uint32(sizeSeed%8) + 1
		coefficients := mustMatrix(t, rows, columns)
		rightHandSide, err := NewSymbols(rows, symbolSize, uint64(rows)*uint64(symbolSize))
		if err != nil {
			t.Fatal(err)
		}
		for index := range coefficients.values {
			coefficients.values[index] = fuzzByte(data, index)
		}
		for index := range rightHandSide.values {
			rightHandSide.values[index] = fuzzByte(data, len(coefficients.values)+index)
		}
		coefficientBefore := bytes.Clone(coefficients.values)
		rightHandSideBefore := bytes.Clone(rightHandSide.values)
		required, err := SolveWorkspaceBytes(rows, columns, symbolSize)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := Solve(coefficients, rightHandSide, required-1); !errors.Is(err, ErrSolverWorkspaceLimit) || result != nil {
			t.Fatalf("one-byte-short Solve = %v, %v", result, err)
		}
		want, wantError := polynomialSolveOracle(coefficients, rightHandSide)
		first, firstError := Solve(coefficients, rightHandSide, required)
		second, secondError := Solve(coefficients, rightHandSide, required)
		if !errors.Is(firstError, wantError) || !errors.Is(secondError, wantError) {
			t.Fatalf("Solve errors = %v, %v; want %v", firstError, secondError, wantError)
		}
		if wantError == nil {
			if !bytes.Equal(first.values, want.values) || !bytes.Equal(second.values, want.values) {
				t.Fatalf("solutions = %v, %v; want %v", first.values, second.values, want.values)
			}
			assertSystemEquals(t, coefficients, first, rightHandSide)
		} else if first != nil || second != nil {
			t.Fatalf("failed Solve returned results %v, %v", first, second)
		}
		if !bytes.Equal(coefficients.values, coefficientBefore) || !bytes.Equal(rightHandSide.values, rightHandSideBefore) {
			t.Fatal("Solve mutated an arbitrary input")
		}
	})
}

func matrixFromRows(t *testing.T, rows [][]byte) *Matrix {
	t.Helper()
	matrix, err := NewMatrix(uint32(len(rows)), uint32(len(rows[0])), uint64(len(rows)*len(rows[0])))
	if err != nil {
		t.Fatal(err)
	}
	for row, values := range rows {
		if len(values) != len(rows[0]) {
			t.Fatal("ragged test matrix")
		}
		copy(matrix.values[row*len(values):(row+1)*len(values)], values)
	}
	return matrix
}

func symbolsFromRows(t *testing.T, rows [][]byte) *Symbols {
	t.Helper()
	symbols, err := NewSymbols(uint32(len(rows)), uint32(len(rows[0])), uint64(len(rows)*len(rows[0])))
	if err != nil {
		t.Fatal(err)
	}
	for row, values := range rows {
		if err := symbols.SetSymbol(uint32(row), values); err != nil {
			t.Fatal(err)
		}
	}
	return symbols
}

func identityMatrix(t *testing.T, order uint32) *Matrix {
	t.Helper()
	matrix := mustMatrix(t, order, order)
	for index := uint32(0); index < order; index++ {
		if err := matrix.Set(index, index, 1); err != nil {
			t.Fatal(err)
		}
	}
	return matrix
}

func multiplySystem(t *testing.T, coefficients *Matrix, solution *Symbols) *Symbols {
	t.Helper()
	if coefficients.Columns() != solution.Count() {
		t.Fatal("test system shape mismatch")
	}
	rightHandSide, err := NewSymbols(coefficients.Rows(), solution.Size(), uint64(coefficients.Rows())*uint64(solution.Size()))
	if err != nil {
		t.Fatal(err)
	}
	for row := uint32(0); row < coefficients.Rows(); row++ {
		destination, err := rightHandSide.Symbol(row)
		if err != nil {
			t.Fatal(err)
		}
		for column := uint32(0); column < coefficients.Columns(); column++ {
			coefficient, err := coefficients.At(row, column)
			if err != nil {
				t.Fatal(err)
			}
			source, err := solution.Symbol(column)
			if err != nil {
				t.Fatal(err)
			}
			for index, value := range source {
				destination[index] ^= polynomialMul(coefficient, value)
			}
		}
	}
	return rightHandSide
}

func assertSystemEquals(t *testing.T, coefficients *Matrix, solution, rightHandSide *Symbols) {
	t.Helper()
	got := multiplySystem(t, coefficients, solution)
	if !bytes.Equal(got.values, rightHandSide.values) {
		t.Fatalf("A*x = %v, want %v", got.values, rightHandSide.values)
	}
}

func transposeMatrix(t *testing.T, matrix *Matrix) *Matrix {
	t.Helper()
	transposed := mustMatrix(t, matrix.Columns(), matrix.Rows())
	for row := uint32(0); row < matrix.Rows(); row++ {
		for column := uint32(0); column < matrix.Columns(); column++ {
			transposed.values[matrixOffset(transposed, column, row)] = matrix.values[matrixOffset(matrix, row, column)]
		}
	}
	return transposed
}

func multiplyMatrices(t *testing.T, left, right *Matrix) *Matrix {
	t.Helper()
	if left.Columns() != right.Rows() {
		t.Fatal("test matrix product shape mismatch")
	}
	result := mustMatrix(t, left.Rows(), right.Columns())
	for row := uint32(0); row < left.Rows(); row++ {
		for column := uint32(0); column < right.Columns(); column++ {
			var value byte
			for shared := uint32(0); shared < left.Columns(); shared++ {
				value ^= polynomialMul(left.values[matrixOffset(left, row, shared)], right.values[matrixOffset(right, shared, column)])
			}
			result.values[matrixOffset(result, row, column)] = value
		}
	}
	return result
}

func assertIdentityMatrix(t *testing.T, matrix *Matrix) {
	t.Helper()
	if matrix.Rows() != matrix.Columns() {
		t.Fatal("identity candidate is not square")
	}
	for row := uint32(0); row < matrix.Rows(); row++ {
		for column := uint32(0); column < matrix.Columns(); column++ {
			want := byte(0)
			if row == column {
				want = 1
			}
			if got := matrix.values[matrixOffset(matrix, row, column)]; got != want {
				t.Fatalf("product[%d,%d] = %d, want %d", row, column, got, want)
			}
		}
	}
}

func polynomialSolveOracle(coefficients *Matrix, rightHandSide *Symbols) (*Symbols, error) {
	coefficientRows := make([][]byte, coefficients.Rows())
	augmentedRows := make([][]byte, coefficients.Rows())
	for row := uint32(0); row < coefficients.Rows(); row++ {
		coefficientRows[row] = bytes.Clone(coefficients.values[row*coefficients.Columns() : (row+1)*coefficients.Columns()])
		augmentedRows[row] = make([]byte, coefficients.Columns()+rightHandSide.Size())
		copy(augmentedRows[row], coefficientRows[row])
		start := row * rightHandSide.Size()
		copy(augmentedRows[row][coefficients.Columns():], rightHandSide.values[start:start+rightHandSide.Size()])
	}
	coefficientRREF := canonicalRowSpace(coefficientRows)
	augmentedRREF := canonicalRowSpace(augmentedRows)
	coefficientRank := nonzeroRowCount(coefficientRREF)
	augmentedRank := nonzeroRowCount(augmentedRREF)
	if augmentedRank > coefficientRank {
		return nil, ErrInconsistentSystem
	}
	if coefficientRank < int(coefficients.Columns()) {
		return nil, ErrNoUniqueSolution
	}
	result := &Symbols{
		count:  coefficients.Columns(),
		size:   rightHandSide.Size(),
		values: make([]byte, int(coefficients.Columns()*rightHandSide.Size())),
	}
	for row := uint32(0); row < result.Count(); row++ {
		copy(result.values[row*result.Size():(row+1)*result.Size()], augmentedRREF[row][coefficients.Columns():])
	}
	return result, nil
}

func nonzeroRowCount(rows [][]byte) int {
	count := 0
	for _, row := range rows {
		for _, value := range row {
			if value != 0 {
				count++
				break
			}
		}
	}
	return count
}

func fuzzByte(data []byte, index int) byte {
	if len(data) == 0 {
		return byte(index*37 + 11)
	}
	return data[index%len(data)]
}
