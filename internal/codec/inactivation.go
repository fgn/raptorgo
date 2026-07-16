package codec

import (
	"errors"
	"fmt"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/matrix"
)

var (
	ErrInactivationShape     = errors.New("codec: invalid inactivation system shape")
	ErrInactivationWorkspace = errors.New("codec: inactivation workspace limit exceeded")
	ErrInactivationRank      = errors.New("codec: inactivation system lacks full column rank")
	ErrInactivationConflict  = errors.New("codec: inconsistent inactivation system")
	ErrInactivationFill      = errors.New("codec: inactivation sparse fill limit exceeded")
	ErrInactivationObserver  = errors.New("codec: inactivation observer rejected state")
)

// InactivationPhase identifies an RFC 6330 Section 5.4 decoding phase.
type InactivationPhase uint8

const (
	InactivationPhaseOne InactivationPhase = iota + 1
	InactivationPhaseTwo
	InactivationPhaseThree
	InactivationPhaseFour
	InactivationPhaseFive
)

// InactivationState is a projection of the live production solver state for
// invariant tests and implementation-trace verification. Step is the number of
// completed phase-one pivot steps. R is the V-region degree selected by the
// latest phase-one step (zero before the first step). Discarded becomes
// nonzero only after phase two has selected exactly L rows.
type InactivationState struct {
	Phase     InactivationPhase
	Step      uint32
	I         uint32
	U         uint32
	R         uint32
	L         uint32
	M         uint32
	Discarded uint32
}

// InactivationObserver receives immutable scalar state snapshots. It must not
// call back into the same solver invocation.
type InactivationObserver func(InactivationState) error

// InactivationWorkspaceBackingBytes returns the peak logical backing retained
// by the solver: sparse coefficient clone and its scratch, D clone, result,
// column permutation, original row degrees, HDPC flags, pivot-column scratch, and two
// union-find arrays. It excludes caller-owned inputs, object headers, callback
// storage, and allocator rounding.
func InactivationWorkspaceBackingBytes(rows, columns uint32, maxNonzeros uint64, symbolSize uint32) (uint64, error) {
	if rows == 0 || columns == 0 || rows < columns || symbolSize == 0 {
		return 0, fmt.Errorf("%w: A=%dx%d T=%d", ErrInactivationShape, rows, columns, symbolSize)
	}
	matrixBytes, err := matrix.SparseOctetBackingBytes(rows, columns, maxNonzeros)
	if err != nil {
		return 0, fmt.Errorf("%w: coefficients: %w", ErrInactivationWorkspace, err)
	}
	dBytes := uint64(rows) * uint64(symbolSize)
	resultBytes := uint64(columns) * uint64(symbolSize)
	rowArrays := uint64(rows) * 5        // original degrees plus HDPC flags
	columnArrays := uint64(columns) * 16 // c, pivot columns, union parents and sizes
	return checkedBackingSum(ErrInactivationWorkspace, matrixBytes, dBytes, resultBytes, rowArrays, columnArrays)
}

// SolveInactivation solves coefficients*C=rightHandSide using an eager
// algebraic equivalent of the RFC 6330 five-phase inactivation order. HDPC
// rows are deferred during phase-one selection while any non-HDPC pivot is
// available, then used as the RFC fallback. Inputs are never mutated or aliased.
func SolveInactivation(
	coefficients *matrix.SparseOctet,
	rightHandSide *gf256.Symbols,
	initialU, firstHDPCRow, hdpcRows uint32,
	maxWorkspace uint64,
) (*gf256.Symbols, error) {
	return SolveInactivationObserved(coefficients, rightHandSide, initialU, firstHDPCRow, hdpcRows, maxWorkspace, nil)
}

// SolveInactivationObserved is SolveInactivation with production-state
// snapshots emitted after each phase-one step and at every later phase
// boundary. Observer rejection terminates without changing caller inputs.
func SolveInactivationObserved(
	coefficients *matrix.SparseOctet,
	rightHandSide *gf256.Symbols,
	initialU, firstHDPCRow, hdpcRows uint32,
	maxWorkspace uint64,
	observer InactivationObserver,
) (*gf256.Symbols, error) {
	if coefficients == nil || rightHandSide == nil {
		return nil, fmt.Errorf("%w: nil coefficients or right-hand side", ErrInactivationShape)
	}
	m, l := coefficients.Rows(), coefficients.Columns()
	if m < l || rightHandSide.Count() != m || rightHandSide.Size() == 0 || initialU > l || firstHDPCRow > m || hdpcRows > m-firstHDPCRow {
		return nil, fmt.Errorf(
			"%w: A=%dx%d D=%dx%d u=%d HDPC=[%d,%d)",
			ErrInactivationShape, m, l, rightHandSide.Count(), rightHandSide.Size(), initialU, firstHDPCRow, firstHDPCRow+hdpcRows,
		)
	}
	required, err := InactivationWorkspaceBackingBytes(m, l, coefficients.MaxNonzeros(), rightHandSide.Size())
	if err != nil {
		return nil, err
	}
	if required > maxWorkspace {
		return nil, fmt.Errorf("%w: need %d bytes (limit %d)", ErrInactivationWorkspace, required, maxWorkspace)
	}

	working, err := coefficients.Clone(coefficients.BackingBytes())
	if err != nil {
		return nil, fmt.Errorf("%w: clone coefficients: %w", ErrInactivationWorkspace, err)
	}
	dBytes := uint64(m) * uint64(rightHandSide.Size())
	d, err := gf256.NewSymbols(m, rightHandSide.Size(), dBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: clone D: %w", ErrInactivationWorkspace, err)
	}
	for row := uint32(0); row < m; row++ {
		values, symbolErr := rightHandSide.Symbol(row)
		if symbolErr != nil {
			return nil, fmt.Errorf("%w: D row %d: %w", ErrInactivationShape, row, symbolErr)
		}
		if symbolErr = d.SetSymbol(row, values); symbolErr != nil {
			return nil, fmt.Errorf("%w: clone D row %d: %w", ErrInactivationWorkspace, row, symbolErr)
		}
	}

	c, err := matrix.NewIdentityPermutation(l, uint64(l)*4)
	if err != nil {
		return nil, fmt.Errorf("%w: column mapping: %w", ErrInactivationWorkspace, err)
	}
	originalDegree := make([]uint32, int(m))
	hdpc := make([]byte, int(m))
	for row := firstHDPCRow; row < firstHDPCRow+hdpcRows; row++ {
		hdpc[row] = 1
	}
	pivotColumns := make([]uint32, int(l))
	unionParents := make([]uint32, int(l))
	unionSizes := make([]uint32, int(l))

	for row := uint32(0); row < m; row++ {
		degree, degreeErr := octetWeightInRange(working, row, 0, l-initialU)
		if degreeErr != nil {
			return nil, degreeErr
		}
		originalDegree[row] = degree
	}

	state := InactivationState{Phase: InactivationPhaseOne, U: initialU, L: l, M: m}
	if err := observeInactivation(observer, state); err != nil {
		return nil, err
	}
	for state.I+state.U < l {
		vEnd := l - state.U
		chosen, r, selectionErr := selectInactivationRow(
			working, state.I, m, vEnd, originalDegree, hdpc, unionParents, unionSizes, pivotColumns,
		)
		if selectionErr != nil {
			return nil, classifyInactivationFailure(working, d, state.I, vEnd, selectionErr)
		}
		if chosen != state.I {
			if err := swapInactivationRows(working, d, state.I, chosen); err != nil {
				return nil, err
			}
			originalDegree[state.I], originalDegree[chosen] = originalDegree[chosen], originalDegree[state.I]
			hdpc[state.I], hdpc[chosen] = hdpc[chosen], hdpc[state.I]
		}
		count, collectErr := collectOctetColumns(working, state.I, state.I, vEnd, pivotColumns)
		if collectErr != nil {
			return nil, collectErr
		}
		if count != r {
			return nil, fmt.Errorf("%w: selected row weight changed from %d to %d", ErrInactivationShape, r, count)
		}
		if err := arrangePivotColumns(working, c, state.I, vEnd, pivotColumns[:count]); err != nil {
			return nil, err
		}

		pivot, atErr := working.At(state.I, state.I)
		if atErr != nil || pivot == 0 {
			return nil, fmt.Errorf("%w: phase-one pivot (%d,%d)=%d: %v", ErrInactivationRank, state.I, state.I, pivot, atErr)
		}
		if pivot != 1 {
			inverse, ok := gf256.Inv(pivot)
			if !ok {
				return nil, fmt.Errorf("%w: phase-one pivot %d has no inverse", ErrInactivationRank, pivot)
			}
			if err := scaleInactivationRow(working, d, state.I, inverse); err != nil {
				return nil, err
			}
		}
		for row := state.I + 1; row < m; row++ {
			coefficient, atErr := working.At(row, state.I)
			if atErr != nil {
				return nil, atErr
			}
			if coefficient != 0 {
				if err := addInactivationRow(working, d, row, state.I, coefficient); err != nil {
					return nil, err
				}
			}
		}
		state.I++
		state.U += r - 1
		state.R = r
		state.Step++
		if err := observeInactivation(observer, state); err != nil {
			return nil, err
		}
	}

	// Phase two reduces U_lower to I_u, using surplus rows only as pivot
	// candidates. Every operation remains coupled to D.
	state.Phase = InactivationPhaseTwo
	for offset := uint32(0); offset < state.U; offset++ {
		pivotRow, column := state.I+offset, state.I+offset
		found := m
		for row := pivotRow; row < m; row++ {
			value, atErr := working.At(row, column)
			if atErr != nil {
				return nil, atErr
			}
			if value != 0 {
				found = row
				break
			}
		}
		if found == m {
			return nil, classifyInactivationFailure(working, d, pivotRow, column, fmt.Errorf("%w: phase-two column %d", ErrInactivationRank, column))
		}
		if found != pivotRow {
			if err := swapInactivationRows(working, d, pivotRow, found); err != nil {
				return nil, err
			}
			hdpc[pivotRow], hdpc[found] = hdpc[found], hdpc[pivotRow]
		}
		pivot, atErr := working.At(pivotRow, column)
		if atErr != nil {
			return nil, atErr
		}
		if pivot != 1 {
			inverse, ok := gf256.Inv(pivot)
			if !ok {
				return nil, fmt.Errorf("%w: phase-two pivot %d has no inverse", ErrInactivationRank, pivot)
			}
			if err := scaleInactivationRow(working, d, pivotRow, inverse); err != nil {
				return nil, err
			}
		}
		for row := pivotRow + 1; row < m; row++ {
			coefficient, atErr := working.At(row, column)
			if atErr != nil {
				return nil, atErr
			}
			if coefficient != 0 {
				if err := addInactivationRow(working, d, row, pivotRow, coefficient); err != nil {
					return nil, err
				}
			}
		}
	}
	for offset := state.U; offset > 0; offset-- {
		pivotRow, column := state.I+offset-1, state.I+offset-1
		for row := state.I; row < pivotRow; row++ {
			coefficient, atErr := working.At(row, column)
			if atErr != nil {
				return nil, atErr
			}
			if coefficient != 0 {
				if err := addInactivationRow(working, d, row, pivotRow, coefficient); err != nil {
					return nil, err
				}
			}
		}
	}
	state.Discarded = m - l
	for row := l; row < m; row++ {
		weight, weightErr := working.RowWeight(row)
		if weightErr != nil {
			return nil, weightErr
		}
		if weight == 0 && symbolRowNonzero(d, row) {
			return nil, fmt.Errorf("%w: surplus row %d", ErrInactivationConflict, row)
		}
	}
	if err := observeInactivation(observer, state); err != nil {
		return nil, err
	}

	// The eager schedule does not materialize RFC matrix X or perform the
	// compensating phase-three/phase-five transformations. Its earlier full-row
	// elimination has already produced the equivalent block state; this boundary
	// checks that state before continuing with U_upper.
	state.Phase = InactivationPhaseThree
	if err := verifyInactivationLeftBlocks(working, state.I, state.U); err != nil {
		return nil, err
	}
	if err := observeInactivation(observer, state); err != nil {
		return nil, err
	}

	// Phase four clears U_upper using the I_u rows selected in phase two.
	state.Phase = InactivationPhaseFour
	for row := uint32(0); row < state.I; row++ {
		for column := state.I; column < l; column++ {
			coefficient, atErr := working.At(row, column)
			if atErr != nil {
				return nil, atErr
			}
			if coefficient != 0 {
				if err := addInactivationRow(working, d, row, column, coefficient); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := observeInactivation(observer, state); err != nil {
		return nil, err
	}

	// Phase five normalizes X and clears its lower triangle. It is normally a
	// no-op in the eager schedule but is executed and verified, not assumed.
	state.Phase = InactivationPhaseFive
	for row := uint32(0); row < state.I; row++ {
		pivot, atErr := working.At(row, row)
		if atErr != nil || pivot == 0 {
			return nil, fmt.Errorf("%w: phase-five pivot %d=%d: %v", ErrInactivationRank, row, pivot, atErr)
		}
		if pivot != 1 {
			inverse, ok := gf256.Inv(pivot)
			if !ok {
				return nil, fmt.Errorf("%w: phase-five pivot %d has no inverse", ErrInactivationRank, pivot)
			}
			if err := scaleInactivationRow(working, d, row, inverse); err != nil {
				return nil, err
			}
		}
		count, collectErr := collectOctetColumns(working, row, 0, row, pivotColumns)
		if collectErr != nil {
			return nil, collectErr
		}
		for _, column := range pivotColumns[:count] {
			coefficient, atErr := working.At(row, column)
			if atErr != nil {
				return nil, atErr
			}
			if err := addInactivationRow(working, d, row, column, coefficient); err != nil {
				return nil, err
			}
		}
	}
	if err := verifyInactivationIdentity(working, l); err != nil {
		return nil, err
	}
	if err := observeInactivation(observer, state); err != nil {
		return nil, err
	}

	resultBytes := uint64(l) * uint64(d.Size())
	result, err := gf256.NewSymbols(l, d.Size(), resultBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: result: %w", ErrInactivationWorkspace, err)
	}
	for current := uint32(0); current < l; current++ {
		original, mappingErr := c.At(current)
		if mappingErr != nil {
			return nil, mappingErr
		}
		values, symbolErr := d.Symbol(current)
		if symbolErr != nil {
			return nil, symbolErr
		}
		if symbolErr = result.SetSymbol(original, values); symbolErr != nil {
			return nil, symbolErr
		}
	}
	return result, nil
}

func selectInactivationRow(
	working *matrix.SparseOctet,
	startRow, endRow, endColumn uint32,
	originalDegree []uint32,
	hdpc []byte,
	parents, sizes, columns []uint32,
) (uint32, uint32, error) {
	minimum := ^uint32(0)
	chosen := endRow
	allowHDPC := false
	for pass := 0; pass < 2 && chosen == endRow; pass++ {
		allowHDPC = pass == 1
		for row := startRow; row < endRow; row++ {
			if !allowHDPC && hdpc[row] != 0 {
				continue
			}
			weight, err := octetWeightInRange(working, row, startRow, endColumn)
			if err != nil {
				return 0, 0, err
			}
			if weight != 0 && (weight < minimum || weight == minimum && (chosen == endRow || originalDegree[row] < originalDegree[chosen])) {
				minimum, chosen = weight, row
			}
		}
	}
	if chosen == endRow {
		return 0, 0, fmt.Errorf("%w: phase one V has no nonzero row at i=%d", ErrInactivationRank, startRow)
	}
	if minimum != 2 {
		return chosen, minimum, nil
	}

	for column := startRow; column < endColumn; column++ {
		parents[column], sizes[column] = column, 1
	}
	for row := startRow; row < endRow; row++ {
		if hdpc[row] != 0 {
			continue
		}
		count, err := collectUnitOctetColumns(working, row, startRow, endColumn, columns[:2])
		if err != nil {
			return 0, 0, err
		}
		weight, err := octetWeightInRange(working, row, startRow, endColumn)
		if err != nil {
			return 0, 0, err
		}
		if weight == 2 && count == 2 {
			unionColumns(parents, sizes, columns[0], columns[1])
		}
	}
	maximumComponent := uint32(0)
	graphChosen := endRow
	for row := startRow; row < endRow; row++ {
		if hdpc[row] != 0 {
			continue
		}
		weight, err := octetWeightInRange(working, row, startRow, endColumn)
		if err != nil || weight != 2 {
			if err != nil {
				return 0, 0, err
			}
			continue
		}
		count, err := collectUnitOctetColumns(working, row, startRow, endColumn, columns[:2])
		if err != nil {
			return 0, 0, err
		}
		if count != 2 {
			continue
		}
		component := sizes[findColumnRoot(parents, columns[0])]
		if component > maximumComponent {
			maximumComponent, graphChosen = component, row
		}
	}
	if graphChosen != endRow {
		return graphChosen, minimum, nil
	}
	return chosen, minimum, nil
}

func arrangePivotColumns(working *matrix.SparseOctet, mapping *matrix.Permutation, start, end uint32, nonzeros []uint32) error {
	if len(nonzeros) == 0 {
		return fmt.Errorf("%w: empty pivot row", ErrInactivationRank)
	}
	if err := swapInactivationColumns(working, mapping, start, nonzeros[0]); err != nil {
		return err
	}
	for index := 1; index < len(nonzeros); index++ {
		if nonzeros[index] == start {
			nonzeros[index] = nonzeros[0]
		}
	}
	nonzeros[0] = start
	tailStart := end - uint32(len(nonzeros)-1)
	for index := 1; index < len(nonzeros); index++ {
		destination := tailStart + uint32(index-1)
		source := nonzeros[index]
		if err := swapInactivationColumns(working, mapping, destination, source); err != nil {
			return err
		}
		for later := index + 1; later < len(nonzeros); later++ {
			if nonzeros[later] == destination {
				nonzeros[later] = source
			}
		}
		nonzeros[index] = destination
	}
	return nil
}

func swapInactivationRows(working *matrix.SparseOctet, symbols *gf256.Symbols, left, right uint32) error {
	if err := working.SwapRows(left, right); err != nil {
		return err
	}
	if err := swapSymbolRows(symbols, left, right); err != nil {
		return err
	}
	return nil
}

func swapInactivationColumns(working *matrix.SparseOctet, mapping *matrix.Permutation, left, right uint32) error {
	if err := working.SwapColumns(left, right); err != nil {
		return err
	}
	return mapping.Swap(left, right)
}

func scaleInactivationRow(working *matrix.SparseOctet, symbols *gf256.Symbols, row uint32, coefficient byte) error {
	if err := working.ScaleRow(row, coefficient); err != nil {
		return err
	}
	values, err := symbols.Symbol(row)
	if err != nil {
		return err
	}
	gf256.Scale(values, coefficient)
	return nil
}

func addInactivationRow(working *matrix.SparseOctet, symbols *gf256.Symbols, destination, source uint32, coefficient byte) error {
	if err := working.AddScaledRow(destination, source, coefficient); err != nil {
		if errors.Is(err, matrix.ErrNonzeroLimitExceeded) {
			return fmt.Errorf("%w: %w", ErrInactivationFill, err)
		}
		return err
	}
	destinationValues, err := symbols.Symbol(destination)
	if err != nil {
		return err
	}
	sourceValues, err := symbols.Symbol(source)
	if err != nil {
		return err
	}
	return gf256.MulAdd(destinationValues, coefficient, sourceValues)
}

func swapSymbolRows(symbols *gf256.Symbols, left, right uint32) error {
	leftValues, err := symbols.Symbol(left)
	if err != nil {
		return err
	}
	rightValues, err := symbols.Symbol(right)
	if err != nil {
		return err
	}
	for index := range leftValues {
		leftValues[index], rightValues[index] = rightValues[index], leftValues[index]
	}
	return nil
}

func octetWeightInRange(working *matrix.SparseOctet, row, start, end uint32) (uint32, error) {
	var weight uint32
	err := working.ForEachInRow(row, func(column uint32, _ byte) bool {
		if column >= end {
			return false
		}
		if column >= start {
			weight++
		}
		return true
	})
	return weight, err
}

func collectOctetColumns(working *matrix.SparseOctet, row, start, end uint32, columns []uint32) (uint32, error) {
	var count uint32
	err := working.ForEachInRow(row, func(column uint32, _ byte) bool {
		if column >= end {
			return false
		}
		if column >= start {
			columns[count] = column
			count++
		}
		return true
	})
	return count, err
}

func collectUnitOctetColumns(working *matrix.SparseOctet, row, start, end uint32, columns []uint32) (uint32, error) {
	var count uint32
	err := working.ForEachInRow(row, func(column uint32, value byte) bool {
		if column >= end {
			return false
		}
		if column >= start && value == 1 && count < uint32(len(columns)) {
			columns[count] = column
			count++
		}
		return true
	})
	return count, err
}

func unionColumns(parents, sizes []uint32, left, right uint32) {
	leftRoot, rightRoot := findColumnRoot(parents, left), findColumnRoot(parents, right)
	if leftRoot == rightRoot {
		return
	}
	if sizes[leftRoot] < sizes[rightRoot] {
		leftRoot, rightRoot = rightRoot, leftRoot
	}
	parents[rightRoot] = leftRoot
	sizes[leftRoot] += sizes[rightRoot]
}

func findColumnRoot(parents []uint32, column uint32) uint32 {
	root := column
	for parents[root] != root {
		root = parents[root]
	}
	for parents[column] != column {
		next := parents[column]
		parents[column] = root
		column = next
	}
	return root
}

func verifyInactivationLeftBlocks(working *matrix.SparseOctet, i, u uint32) error {
	l := working.Columns()
	if i+u != l {
		return fmt.Errorf("%w: i+u=%d, L=%d", ErrInactivationShape, i+u, l)
	}
	for row := uint32(0); row < l; row++ {
		diagonalSeen := false
		var invalid error
		err := working.ForEachInRow(row, func(column uint32, value byte) bool {
			switch {
			case column < i:
				if row < i && column == row && value == 1 {
					diagonalSeen = true
					return true
				}
				invalid = fmt.Errorf("%w: left block (%d,%d)=%d", ErrInactivationShape, row, column, value)
				return false
			case row >= i:
				if column == row && value == 1 {
					diagonalSeen = true
					return true
				}
				invalid = fmt.Errorf("%w: I_u (%d,%d)=%d", ErrInactivationShape, row, column, value)
				return false
			default:
				// U_upper is the only arbitrary region before phase four.
				return true
			}
		})
		if err != nil {
			return err
		}
		if invalid != nil {
			return invalid
		}
		if !diagonalSeen {
			return fmt.Errorf("%w: missing identity diagonal in row %d", ErrInactivationShape, row)
		}
	}
	return nil
}

func verifyInactivationIdentity(working *matrix.SparseOctet, order uint32) error {
	for row := uint32(0); row < order; row++ {
		weight, err := working.RowWeight(row)
		if err != nil {
			return err
		}
		value, err := working.At(row, row)
		if err != nil {
			return err
		}
		if weight != 1 || value != 1 {
			return fmt.Errorf("%w: final row %d weight=%d diagonal=%d", ErrInactivationShape, row, weight, value)
		}
	}
	return nil
}

func symbolRowNonzero(symbols *gf256.Symbols, row uint32) bool {
	values, err := symbols.Symbol(row)
	if err != nil {
		return false
	}
	for _, value := range values {
		if value != 0 {
			return true
		}
	}
	return false
}

func classifyInactivationFailure(
	working *matrix.SparseOctet,
	symbols *gf256.Symbols,
	startRow, startColumn uint32,
	rankError error,
) error {
	pivotRow := startRow
	for column := startColumn; column < working.Columns() && pivotRow < working.Rows(); column++ {
		found := working.Rows()
		for row := pivotRow; row < working.Rows(); row++ {
			value, err := working.At(row, column)
			if err != nil {
				return err
			}
			if value != 0 {
				found = row
				break
			}
		}
		if found == working.Rows() {
			continue
		}
		if found != pivotRow {
			if err := swapInactivationRows(working, symbols, pivotRow, found); err != nil {
				return err
			}
		}
		pivot, err := working.At(pivotRow, column)
		if err != nil {
			return err
		}
		if pivot != 1 {
			inverse, ok := gf256.Inv(pivot)
			if !ok {
				return rankError
			}
			if err := scaleInactivationRow(working, symbols, pivotRow, inverse); err != nil {
				return err
			}
		}
		for row := pivotRow + 1; row < working.Rows(); row++ {
			coefficient, err := working.At(row, column)
			if err != nil {
				return err
			}
			if coefficient != 0 {
				if err := addInactivationRow(working, symbols, row, pivotRow, coefficient); err != nil {
					return err
				}
			}
		}
		pivotRow++
	}
	for row := pivotRow; row < working.Rows(); row++ {
		weight, err := working.RowWeight(row)
		if err != nil {
			return err
		}
		if weight == 0 && symbolRowNonzero(symbols, row) {
			return fmt.Errorf("%w: zero coefficient row %d: %w", ErrInactivationConflict, row, rankError)
		}
	}
	return rankError
}

func observeInactivation(observer InactivationObserver, state InactivationState) error {
	if observer == nil {
		return nil
	}
	if err := observer(state); err != nil {
		return fmt.Errorf("%w: phase %d step %d: %w", ErrInactivationObserver, state.Phase, state.Step, err)
	}
	return nil
}
