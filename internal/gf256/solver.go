package gf256

import (
	"errors"
	"fmt"
)

var (
	ErrMatrixNotSquare       = errors.New("gf256: matrix is not square")
	ErrRightHandSideShape    = errors.New("gf256: right-hand side shape mismatch")
	ErrSingularMatrix        = errors.New("gf256: singular matrix")
	ErrNoUniqueSolution      = errors.New("gf256: system has no unique solution")
	ErrInconsistentSystem    = errors.New("gf256: inconsistent system")
	ErrSolverWorkspaceLimit  = errors.New("gf256: solver workspace limit exceeded")
	ErrInvalidSolverArgument = errors.New("gf256: invalid solver argument")
)

// SolveWorkspaceBytes returns the peak backing-store bytes Solve owns: one
// m*n coefficient clone, one m*symbolSize right-hand-side clone, and the
// n*symbolSize result. It excludes Go object headers and allocator rounding.
func SolveWorkspaceBytes(rows, columns, symbolSize uint32) (uint64, error) {
	if rows == 0 || columns == 0 || symbolSize == 0 {
		return 0, fmt.Errorf("%w: matrix %dx%d, symbol size %d", ErrInvalidSolverArgument, rows, columns, symbolSize)
	}
	coefficientBytes, err := solverAllocationBytes(rows, columns)
	if err != nil {
		return 0, err
	}
	rightHandSideBytes, err := solverAllocationBytes(rows, symbolSize)
	if err != nil {
		return 0, err
	}
	resultBytes, err := solverAllocationBytes(columns, symbolSize)
	if err != nil {
		return 0, err
	}
	return checkedWorkspaceSum(coefficientBytes, rightHandSideBytes, resultBytes)
}

// RankWorkspaceBytes returns the exact number of bytes Rank uses for its
// private matrix clone.
func RankWorkspaceBytes(rows, columns uint32) (uint64, error) {
	if rows == 0 || columns == 0 {
		return 0, fmt.Errorf("%w: matrix %dx%d", ErrInvalidSolverArgument, rows, columns)
	}
	return solverAllocationBytes(rows, columns)
}

// InvertWorkspaceBytes returns the peak number of bytes Invert owns: its
// identity right-hand side plus Solve's coefficient, right-hand-side, and
// result allocations.
func InvertWorkspaceBytes(order uint32) (uint64, error) {
	if order == 0 {
		return 0, fmt.Errorf("%w: matrix order %d", ErrInvalidSolverArgument, order)
	}
	identityBytes, err := solverAllocationBytes(order, order)
	if err != nil {
		return 0, err
	}
	solveBytes, err := SolveWorkspaceBytes(order, order, order)
	if err != nil {
		return 0, err
	}
	return checkedWorkspaceSum(identityBytes, solveBytes)
}

// Solve computes X in coefficients*X=rightHandSide using deterministic
// Gauss-Jordan elimination over GF(256). Coefficients may be rectangular. The
// result is returned only when the system is consistent and has full column
// rank. Solve never mutates or aliases its inputs on any outcome.
func Solve(coefficients *Matrix, rightHandSide *Symbols, maxWorkspace uint64) (*Symbols, error) {
	if coefficients == nil {
		return nil, fmt.Errorf("%w: nil coefficient matrix", ErrInvalidSolverArgument)
	}
	if rightHandSide == nil {
		return nil, fmt.Errorf("%w: nil", ErrRightHandSideShape)
	}
	if rightHandSide.count != coefficients.rows {
		return nil, fmt.Errorf("%w: %d rows, want %d", ErrRightHandSideShape, rightHandSide.count, coefficients.rows)
	}

	required, err := SolveWorkspaceBytes(coefficients.rows, coefficients.columns, rightHandSide.size)
	if err != nil {
		return nil, err
	}
	if required > maxWorkspace {
		return nil, fmt.Errorf("%w: need %d bytes (limit %d)", ErrSolverWorkspaceLimit, required, maxWorkspace)
	}

	coefficientClone, err := NewMatrix(coefficients.rows, coefficients.columns, uint64(len(coefficients.values)))
	if err != nil {
		return nil, fmt.Errorf("clone coefficients: %w", err)
	}
	copy(coefficientClone.values, coefficients.values)
	rightHandSideClone, err := NewSymbols(rightHandSide.count, rightHandSide.size, uint64(len(rightHandSide.values)))
	if err != nil {
		return nil, fmt.Errorf("clone right-hand side: %w", err)
	}
	copy(rightHandSideClone.values, rightHandSide.values)

	var pivotRow uint32
	for column := uint32(0); column < coefficientClone.columns && pivotRow < coefficientClone.rows; column++ {
		pivot := firstNonzeroRow(coefficientClone, pivotRow, column)
		if pivot == coefficientClone.rows {
			continue
		}
		if pivot != pivotRow {
			swapRowsUnchecked(coefficientClone, pivotRow, pivot)
			swapSymbolRowsUnchecked(rightHandSideClone, pivotRow, pivot)
		}

		pivotValue := coefficientClone.values[matrixOffset(coefficientClone, pivotRow, column)]
		inverse, ok := Inv(pivotValue)
		if !ok {
			return nil, fmt.Errorf("%w: zero pivot at (%d,%d)", ErrSingularMatrix, pivotRow, column)
		}
		scaleCoupledRowsUnchecked(coefficientClone, rightHandSideClone, pivotRow, inverse)
		for row := uint32(0); row < coefficientClone.rows; row++ {
			if row == pivotRow {
				continue
			}
			coefficient := coefficientClone.values[matrixOffset(coefficientClone, row, column)]
			if coefficient != 0 {
				addCoupledRowsUnchecked(coefficientClone, rightHandSideClone, row, pivotRow, coefficient)
			}
		}
		pivotRow++
	}

	for row := pivotRow; row < coefficientClone.rows; row++ {
		if rowIsZero(coefficientClone, row) && symbolRowIsNonzero(rightHandSideClone, row) {
			return nil, fmt.Errorf("%w: row %d", ErrInconsistentSystem, row)
		}
	}
	if pivotRow < coefficientClone.columns {
		return nil, fmt.Errorf("%w: rank %d, need %d", ErrNoUniqueSolution, pivotRow, coefficientClone.columns)
	}

	resultBytes := uint64(coefficients.columns) * uint64(rightHandSide.size)
	result, err := NewSymbols(coefficients.columns, rightHandSide.size, resultBytes)
	if err != nil {
		return nil, fmt.Errorf("allocate result: %w", err)
	}
	copy(result.values, rightHandSideClone.values[:int(resultBytes)])
	return result, nil
}

// Rank returns the number of linearly independent rows in matrix. It uses a
// private clone and never mutates matrix.
func Rank(matrix *Matrix, maxWorkspace uint64) (uint32, error) {
	if matrix == nil {
		return 0, fmt.Errorf("%w: nil matrix", ErrInvalidSolverArgument)
	}
	required, err := RankWorkspaceBytes(matrix.rows, matrix.columns)
	if err != nil {
		return 0, err
	}
	if required > maxWorkspace {
		return 0, fmt.Errorf("%w: need %d bytes (limit %d)", ErrSolverWorkspaceLimit, required, maxWorkspace)
	}
	clone, err := NewMatrix(matrix.rows, matrix.columns, required)
	if err != nil {
		return 0, fmt.Errorf("clone matrix: %w", err)
	}
	copy(clone.values, matrix.values)

	var pivotRow uint32
	for column := uint32(0); column < clone.columns && pivotRow < clone.rows; column++ {
		pivot := firstNonzeroRow(clone, pivotRow, column)
		if pivot == clone.rows {
			continue
		}
		if pivot != pivotRow {
			swapRowsUnchecked(clone, pivotRow, pivot)
		}
		pivotValue := clone.values[matrixOffset(clone, pivotRow, column)]
		inverse, ok := Inv(pivotValue)
		if !ok {
			return 0, fmt.Errorf("%w: zero rank pivot", ErrSingularMatrix)
		}
		pivotStart, pivotEnd := matrixRowRange(clone, pivotRow)
		Scale(clone.values[pivotStart:pivotEnd:pivotEnd], inverse)
		for row := pivotRow + 1; row < clone.rows; row++ {
			coefficient := clone.values[matrixOffset(clone, row, column)]
			if coefficient != 0 {
				rowStart, rowEnd := matrixRowRange(clone, row)
				_ = MulAdd(clone.values[rowStart:rowEnd:rowEnd], coefficient, clone.values[pivotStart:pivotEnd:pivotEnd])
			}
		}
		pivotRow++
	}
	return pivotRow, nil
}

// Invert returns the inverse of a square matrix without mutating matrix.
func Invert(matrix *Matrix, maxWorkspace uint64) (*Matrix, error) {
	if matrix == nil {
		return nil, fmt.Errorf("%w: nil matrix", ErrInvalidSolverArgument)
	}
	if matrix.rows != matrix.columns {
		return nil, fmt.Errorf("%w: %dx%d", ErrMatrixNotSquare, matrix.rows, matrix.columns)
	}
	required, err := InvertWorkspaceBytes(matrix.rows)
	if err != nil {
		return nil, err
	}
	if required > maxWorkspace {
		return nil, fmt.Errorf("%w: need %d bytes (limit %d)", ErrSolverWorkspaceLimit, required, maxWorkspace)
	}
	identityBytes := uint64(matrix.rows) * uint64(matrix.rows)
	identity, err := NewSymbols(matrix.rows, matrix.rows, identityBytes)
	if err != nil {
		return nil, fmt.Errorf("allocate identity: %w", err)
	}
	for index := uint32(0); index < matrix.rows; index++ {
		identity.values[uint64(index)*uint64(matrix.rows)+uint64(index)] = 1
	}
	solution, err := Solve(matrix, identity, maxWorkspace-identityBytes)
	if err != nil {
		if errors.Is(err, ErrInconsistentSystem) || errors.Is(err, ErrNoUniqueSolution) {
			return nil, fmt.Errorf("%w: %v", ErrSingularMatrix, err)
		}
		return nil, err
	}
	return &Matrix{rows: matrix.rows, columns: matrix.columns, values: solution.values}, nil
}

func solverAllocationBytes(left, right uint32) (uint64, error) {
	bytes := uint64(left) * uint64(right)
	if bytes > uint64(^uint(0)>>1) {
		return 0, fmt.Errorf("%w: %dx%d allocation exceeds platform limit", ErrSolverWorkspaceLimit, left, right)
	}
	return bytes, nil
}

func checkedWorkspaceSum(values ...uint64) (uint64, error) {
	var total uint64
	for _, value := range values {
		if total > ^uint64(0)-value {
			return 0, fmt.Errorf("%w: workspace size overflow", ErrSolverWorkspaceLimit)
		}
		total += value
	}
	if total > uint64(^uint(0)>>1) {
		return 0, fmt.Errorf("%w: aggregate %d exceeds platform limit", ErrSolverWorkspaceLimit, total)
	}
	return total, nil
}

func firstNonzeroRow(matrix *Matrix, startRow, column uint32) uint32 {
	for row := startRow; row < matrix.rows; row++ {
		if matrix.values[matrixOffset(matrix, row, column)] != 0 {
			return row
		}
	}
	return matrix.rows
}

func matrixOffset(matrix *Matrix, row, column uint32) int {
	return int(uint64(row)*uint64(matrix.columns) + uint64(column))
}

func matrixRowRange(matrix *Matrix, row uint32) (int, int) {
	start := int(uint64(row) * uint64(matrix.columns))
	return start, start + int(matrix.columns)
}

func symbolRowRange(symbols *Symbols, row uint32) (int, int) {
	start := int(uint64(row) * uint64(symbols.size))
	return start, start + int(symbols.size)
}

func swapRowsUnchecked(matrix *Matrix, left, right uint32) {
	leftStart, leftEnd := matrixRowRange(matrix, left)
	rightStart, rightEnd := matrixRowRange(matrix, right)
	leftValues := matrix.values[leftStart:leftEnd:leftEnd]
	rightValues := matrix.values[rightStart:rightEnd:rightEnd]
	for index := range leftValues {
		leftValues[index], rightValues[index] = rightValues[index], leftValues[index]
	}
}

func swapSymbolRowsUnchecked(symbols *Symbols, left, right uint32) {
	leftStart, leftEnd := symbolRowRange(symbols, left)
	rightStart, rightEnd := symbolRowRange(symbols, right)
	leftValues := symbols.values[leftStart:leftEnd:leftEnd]
	rightValues := symbols.values[rightStart:rightEnd:rightEnd]
	for index := range leftValues {
		leftValues[index], rightValues[index] = rightValues[index], leftValues[index]
	}
}

func scaleCoupledRowsUnchecked(matrix *Matrix, symbols *Symbols, row uint32, coefficient byte) {
	matrixStart, matrixEnd := matrixRowRange(matrix, row)
	symbolStart, symbolEnd := symbolRowRange(symbols, row)
	Scale(matrix.values[matrixStart:matrixEnd:matrixEnd], coefficient)
	Scale(symbols.values[symbolStart:symbolEnd:symbolEnd], coefficient)
}

func addCoupledRowsUnchecked(matrix *Matrix, symbols *Symbols, destination, source uint32, coefficient byte) {
	destinationStart, destinationEnd := matrixRowRange(matrix, destination)
	sourceStart, sourceEnd := matrixRowRange(matrix, source)
	_ = MulAdd(matrix.values[destinationStart:destinationEnd:destinationEnd], coefficient, matrix.values[sourceStart:sourceEnd:sourceEnd])
	destinationStart, destinationEnd = symbolRowRange(symbols, destination)
	sourceStart, sourceEnd = symbolRowRange(symbols, source)
	_ = MulAdd(symbols.values[destinationStart:destinationEnd:destinationEnd], coefficient, symbols.values[sourceStart:sourceEnd:sourceEnd])
}

func rowIsZero(matrix *Matrix, row uint32) bool {
	start, end := matrixRowRange(matrix, row)
	for _, value := range matrix.values[start:end] {
		if value != 0 {
			return false
		}
	}
	return true
}

func symbolRowIsNonzero(symbols *Symbols, row uint32) bool {
	start, end := symbolRowRange(symbols, row)
	for _, value := range symbols.values[start:end] {
		if value != 0 {
			return true
		}
	}
	return false
}
