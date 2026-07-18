package codec

import (
	"errors"
	"fmt"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/matrix"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

var (
	ErrHybridExecutionWorkspace = errors.New("codec: hybrid inactivation execution workspace limit exceeded")
	ErrHybridDenseWorkspace     = errors.New("codec: hybrid dense completion workspace limit exceeded")
)

// InactivationExecutionBackingBytes returns executor-owned logical backing for
// a completed plan: packed binary U, dense HDPC, an owned D clone, materialized
// residual system, dense solver workspace, and final result. The plan and
// caller-owned D are excluded.
func InactivationExecutionBackingBytes(plan *InactivationPlan, symbolSize uint32) (uint64, error) {
	if plan == nil || symbolSize == 0 {
		return 0, fmt.Errorf("%w: nil/invalid plan or symbol size", ErrHybridExecutionWorkspace)
	}
	if err := validateInactivationPlan(plan); err != nil {
		return 0, err
	}
	hybridBytes, err := matrix.HybridBinaryBackingBytes(
		plan.rows,
		plan.parameters.L(),
		plan.parameters.P(),
		plan.finalU,
		uint64(plan.vNonzeros),
	)
	if err != nil {
		return 0, fmt.Errorf("%w: binary U: %w", ErrHybridExecutionWorkspace, err)
	}
	hdpcBytes := uint64(plan.parameters.H) * uint64(plan.parameters.L())
	dBytes := uint64(plan.totalRows) * uint64(symbolSize)
	residualRows := plan.totalRows - plan.finalI
	residualColumns := plan.parameters.L() - plan.finalI
	if residualRows == 0 || residualColumns == 0 {
		return 0, fmt.Errorf(
			"%w: residual=%dx%d",
			ErrInactivationShape, residualRows, residualColumns,
		)
	}
	coefficientBytes := uint64(residualRows) * uint64(residualColumns)
	rightHandSideBytes := uint64(residualRows) * uint64(symbolSize)
	solverBytes, err := gf256.SolveWorkspaceBytes(residualRows, residualColumns, symbolSize)
	if err != nil {
		return 0, fmt.Errorf("%w: dense completion: %w", ErrHybridDenseWorkspace, err)
	}
	resultBytes := uint64(plan.parameters.L()) * uint64(symbolSize)
	return checkedBackingSum(
		ErrHybridExecutionWorkspace,
		hybridBytes,
		hdpcBytes,
		dBytes,
		coefficientBytes,
		rightHandSideBytes,
		solverBytes,
		resultBytes,
	)
}

// ExecuteInactivationPlan solves a canonical Figure-5 system. D rows must be
// ordered as S LDPC rows, H HDPC rows, then encoded rows. Inputs are never
// mutated or aliased. Binary planning events are replayed eagerly; the
// remaining V+U Schur system is completed with dense GF(256) elimination.
func ExecuteInactivationPlan(
	plan *InactivationPlan,
	d *gf256.Symbols,
	maxWorkspace uint64,
) (*gf256.Symbols, error) {
	return executeInactivationPlanObserved(plan, d, maxWorkspace, nil)
}

// ExecuteInactivationPlanObserved executes the same production path while
// emitting deep-copied development trace records. Observer storage is outside
// the logical workspace contract. This internal API exists only for generated
// verification traces and differential tests.
func ExecuteInactivationPlanObserved(
	plan *InactivationPlan,
	d *gf256.Symbols,
	maxWorkspace uint64,
	observer HybridExecutionObserver,
) (*gf256.Symbols, error) {
	return executeInactivationPlanObserved(plan, d, maxWorkspace, observer)
}

func executeInactivationPlanObserved(
	plan *InactivationPlan,
	d *gf256.Symbols,
	maxWorkspace uint64,
	observer HybridExecutionObserver,
) (*gf256.Symbols, error) {
	if plan == nil || d == nil || d.Count() != plan.totalRows || d.Size() == 0 {
		return nil, fmt.Errorf("%w: plan/D shape", ErrInactivationShape)
	}
	required, err := InactivationExecutionBackingBytes(plan, d.Size())
	if err != nil {
		return nil, err
	}
	if required > maxWorkspace {
		return nil, fmt.Errorf("%w: need %d bytes (limit %d)", ErrHybridExecutionWorkspace, required, maxWorkspace)
	}

	hybridBytes, err := matrix.HybridBinaryBackingBytes(
		plan.rows, plan.parameters.L(), plan.parameters.P(), plan.finalU, uint64(plan.vNonzeros),
	)
	if err != nil {
		return nil, err
	}
	binary, err := matrix.NewHybridBinary16(
		plan.vLengths, plan.parameters.L(), plan.parameters.P(), plan.finalU, hybridBytes,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: construct binary U: %w", ErrHybridExecutionWorkspace, err)
	}
	for row := uint32(0); row < plan.rows; row++ {
		start := plan.vOffsets[row]
		columns := plan.vEntries[start : start+uint32(plan.vLengths[row])]
		if err := binary.LoadSparseRow(row, columns); err != nil {
			return nil, fmt.Errorf("%w: load binary row %d: %w", ErrHybridPlanInvariant, row, err)
		}
		start = plan.uOffsets[row]
		for index := uint32(0); index < uint32(plan.uLengths[row]); index++ {
			if err := binary.SetInitialDense(row, uint32(plan.uEntries[start+index]), true); err != nil {
				return nil, fmt.Errorf("%w: load initial U row %d: %w", ErrHybridPlanInvariant, row, err)
			}
		}
	}
	if err := binary.Finalize(); err != nil {
		return nil, fmt.Errorf("%w: finalize binary U: %w", ErrHybridPlanInvariant, err)
	}

	hdpcBytes := uint64(plan.parameters.H) * uint64(plan.parameters.L())
	hdpc, err := gf256.NewMatrix(plan.parameters.H, plan.parameters.L(), hdpcBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: HDPC: %w", ErrHybridExecutionWorkspace, err)
	}
	if err := buildHybridHDPC(hdpc, plan.parameters); err != nil {
		return nil, err
	}
	workingD, rhsMap, err := cloneHybridRightHandSide(d, plan, observer != nil)
	if err != nil {
		return nil, fmt.Errorf("%w: D clone: %w", ErrHybridExecutionWorkspace, err)
	}
	if observer != nil {
		rowMap, rowInverse, columnMap, columnInverse, snapshotErr := snapshotHybridMappings(binary)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		binaryValues, snapshotErr := snapshotHybridBinary(binary)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		hdpcValues, snapshotErr := snapshotGFMatrix(hdpc)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		workingValues, snapshotErr := snapshotGFSymbols(workingD)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		if snapshotErr = emitHybridObservation(observer, HybridExecutionObservation{
			Stage: "rhs", DenseColumns: binary.DenseColumns(), RHSMap: rhsMap,
			RowMap: rowMap, RowInverse: rowInverse,
			ColumnMap: columnMap, ColumnInverse: columnInverse,
			Binary: binaryValues, HDPC: hdpcValues, WorkingD: workingValues,
		}); snapshotErr != nil {
			return nil, snapshotErr
		}
	}

	for step := uint32(0); step < plan.steps; step++ {
		pivotRow := plan.pivotRows[step]
		pivotColumn := uint32(plan.pivotColumns[step])
		logicalRow, rowErr := binary.LogicalRow(pivotRow)
		if rowErr != nil || logicalRow < step {
			return nil, fmt.Errorf("%w: replay pivot row %d at step %d", ErrHybridPlanInvariant, pivotRow, step)
		}
		if err := binary.SwapRows(step, logicalRow); err != nil {
			return nil, err
		}
		logicalColumn, columnErr := binary.LogicalColumn(pivotColumn)
		if columnErr != nil {
			return nil, columnErr
		}
		if err := binary.SwapSparseColumns(step, logicalColumn); err != nil {
			return nil, fmt.Errorf("%w: replay pivot column: %w", ErrHybridPlanInvariant, err)
		}

		frozenStart, frozenEnd := plan.frozenOffsets[step], plan.frozenOffsets[step+1]
		freezeCount := frozenEnd - frozenStart
		freezeLogicalStart := plan.parameters.L() - binary.DenseColumns() - freezeCount
		for index := uint32(0); index < freezeCount; index++ {
			physical := uint32(plan.frozenColumns[frozenStart+index])
			logical, logicalErr := binary.LogicalColumn(physical)
			if logicalErr != nil {
				return nil, logicalErr
			}
			if err := binary.SwapSparseColumns(freezeLogicalStart+index, logical); err != nil {
				return nil, fmt.Errorf("%w: replay frozen column: %w", ErrHybridPlanInvariant, err)
			}
		}
		for index := frozenEnd; index > frozenStart; index-- {
			want := uint32(plan.frozenColumns[index-1])
			got, freezeErr := binary.FreezeNext()
			if freezeErr != nil || got != want {
				return nil, fmt.Errorf("%w: freeze got=%d want=%d: %v", ErrHybridPlanInvariant, got, want, freezeErr)
			}
		}

		pivotSymbol, symbolErr := workingD.Symbol(pivotRow)
		if symbolErr != nil {
			return nil, symbolErr
		}
		for index := plan.eliminationOffsets[step]; index < plan.eliminationOffsets[step+1]; index++ {
			destinationPhysical := plan.eliminationRows[index]
			destinationLogical, logicalErr := binary.LogicalRow(destinationPhysical)
			if logicalErr != nil || destinationLogical <= step {
				return nil, fmt.Errorf("%w: elimination row %d at step %d", ErrHybridPlanInvariant, destinationPhysical, step)
			}
			if err := binary.XORDenseRows(destinationLogical, step); err != nil {
				return nil, err
			}
			destination, destinationErr := workingD.Symbol(destinationPhysical)
			if destinationErr != nil {
				return nil, destinationErr
			}
			if err := gf256.XOR(destination, pivotSymbol); err != nil {
				return nil, err
			}
		}

		for hdpcRow := uint32(0); hdpcRow < plan.parameters.H; hdpcRow++ {
			coefficient, atErr := hdpc.At(hdpcRow, pivotColumn)
			if atErr != nil {
				return nil, atErr
			}
			if coefficient == 0 {
				continue
			}
			var callbackErr error
			if err := binary.ForEachDenseOne(step, func(logical uint32) bool {
				physical, mappingErr := binary.PhysicalColumn(logical)
				if mappingErr != nil {
					callbackErr = mappingErr
					return false
				}
				value, valueErr := hdpc.At(hdpcRow, physical)
				if valueErr != nil {
					callbackErr = valueErr
					return false
				}
				if setErr := hdpc.Set(hdpcRow, physical, value^coefficient); setErr != nil {
					callbackErr = setErr
					return false
				}
				return true
			}); err != nil {
				return nil, err
			}
			if callbackErr != nil {
				return nil, callbackErr
			}
			if err := hdpc.Set(hdpcRow, pivotColumn, 0); err != nil {
				return nil, err
			}
			destination, destinationErr := workingD.Symbol(plan.rows + hdpcRow)
			if destinationErr != nil {
				return nil, destinationErr
			}
			if err := gf256.MulAdd(destination, coefficient, pivotSymbol); err != nil {
				return nil, err
			}
		}
		if err := observeHybridState(observer, "replay", step+1, binary, hdpc, workingD); err != nil {
			return nil, err
		}
	}
	if binary.DenseColumns() != plan.finalU {
		return nil, fmt.Errorf("%w: replay U=%d want=%d", ErrHybridPlanInvariant, binary.DenseColumns(), plan.finalU)
	}

	residualRows := plan.totalRows - plan.finalI
	residualColumns := plan.parameters.L() - plan.finalI
	coefficientBytes := uint64(residualRows) * uint64(residualColumns)
	coefficients, err := gf256.NewMatrix(residualRows, residualColumns, coefficientBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: residual A: %w", ErrHybridDenseWorkspace, err)
	}
	rightHandSideBytes := uint64(residualRows) * uint64(d.Size())
	rightHandSide, err := gf256.NewSymbols(residualRows, d.Size(), rightHandSideBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: residual D: %w", ErrHybridDenseWorkspace, err)
	}
	residualRow := uint32(0)
	for logicalRow := plan.finalI; logicalRow < plan.rows; logicalRow++ {
		for logicalColumn := plan.finalI; logicalColumn < plan.parameters.L(); logicalColumn++ {
			value, hasErr := binary.Has(logicalRow, logicalColumn)
			if hasErr != nil {
				return nil, hasErr
			}
			if value {
				if err := coefficients.Set(residualRow, logicalColumn-plan.finalI, 1); err != nil {
					return nil, err
				}
			}
		}
		physicalRow, mappingErr := binary.PhysicalRow(logicalRow)
		if mappingErr != nil {
			return nil, mappingErr
		}
		values, symbolErr := workingD.Symbol(physicalRow)
		if symbolErr != nil {
			return nil, symbolErr
		}
		if err := rightHandSide.SetSymbol(residualRow, values); err != nil {
			return nil, err
		}
		residualRow++
	}
	for hdpcRow := uint32(0); hdpcRow < plan.parameters.H; hdpcRow++ {
		for logicalColumn := plan.finalI; logicalColumn < plan.parameters.L(); logicalColumn++ {
			physicalColumn, mappingErr := binary.PhysicalColumn(logicalColumn)
			if mappingErr != nil {
				return nil, mappingErr
			}
			value, atErr := hdpc.At(hdpcRow, physicalColumn)
			if atErr != nil {
				return nil, atErr
			}
			if err := coefficients.Set(residualRow, logicalColumn-plan.finalI, value); err != nil {
				return nil, err
			}
		}
		values, symbolErr := workingD.Symbol(plan.rows + hdpcRow)
		if symbolErr != nil {
			return nil, symbolErr
		}
		if err := rightHandSide.SetSymbol(residualRow, values); err != nil {
			return nil, err
		}
		residualRow++
	}
	if residualRow != residualRows {
		return nil, fmt.Errorf("%w: materialized rows=%d want=%d", ErrHybridPlanInvariant, residualRow, residualRows)
	}
	if observer != nil {
		coefficientValues, snapshotErr := snapshotGFMatrix(coefficients)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		rightHandSideValues, snapshotErr := snapshotGFSymbols(rightHandSide)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		if snapshotErr = emitHybridObservation(observer, HybridExecutionObservation{
			Stage: "residual", Step: plan.steps, DenseColumns: binary.DenseColumns(),
			ResidualRows: residualRows, ResidualColumns: residualColumns,
			ResidualCoefficients: coefficientValues, ResidualRHS: rightHandSideValues,
		}); snapshotErr != nil {
			return nil, snapshotErr
		}
	}
	solverBytes, err := gf256.SolveWorkspaceBytes(residualRows, residualColumns, d.Size())
	if err != nil {
		return nil, err
	}
	residualSolution, err := gf256.Solve(coefficients, rightHandSide, solverBytes)
	if err != nil {
		switch {
		case errors.Is(err, gf256.ErrInconsistentSystem):
			return nil, fmt.Errorf("%w: dense completion: %w", ErrInactivationConflict, err)
		case errors.Is(err, gf256.ErrNoUniqueSolution):
			return nil, fmt.Errorf("%w: dense completion: %w", ErrInactivationRank, err)
		default:
			return nil, fmt.Errorf("%w: %w", ErrHybridDenseWorkspace, err)
		}
	}
	if observer != nil {
		solutionValues, snapshotErr := snapshotGFSymbols(residualSolution)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		if snapshotErr = emitHybridObservation(observer, HybridExecutionObservation{
			Stage: "solved", Step: plan.steps,
			ResidualRows: residualRows, ResidualColumns: residualColumns,
			ResidualSolution: solutionValues,
		}); snapshotErr != nil {
			return nil, snapshotErr
		}
	}

	for logicalRow := uint32(0); logicalRow < plan.finalI; logicalRow++ {
		physicalRow, mappingErr := binary.PhysicalRow(logicalRow)
		if mappingErr != nil {
			return nil, mappingErr
		}
		destination, symbolErr := workingD.Symbol(physicalRow)
		if symbolErr != nil {
			return nil, symbolErr
		}
		var before []byte
		var dependencies []uint32
		if observer != nil {
			before = append([]byte(nil), destination...)
		}
		var callbackErr error
		if err := binary.ForEachDenseOne(logicalRow, func(logicalColumn uint32) bool {
			if observer != nil {
				dependencies = append(dependencies, logicalColumn)
			}
			source, sourceErr := residualSolution.Symbol(logicalColumn - plan.finalI)
			if sourceErr != nil {
				callbackErr = sourceErr
				return false
			}
			if xorErr := gf256.XOR(destination, source); xorErr != nil {
				callbackErr = xorErr
				return false
			}
			return true
		}); err != nil {
			return nil, err
		}
		if callbackErr != nil {
			return nil, callbackErr
		}
		if err := emitHybridSubstitutionObservation(
			observer, plan.steps, logicalRow, dependencies, before, destination,
		); err != nil {
			return nil, err
		}
	}

	resultBytes := uint64(plan.parameters.L()) * uint64(d.Size())
	result, err := gf256.NewSymbols(plan.parameters.L(), d.Size(), resultBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: result: %w", ErrHybridExecutionWorkspace, err)
	}
	for logicalColumn := uint32(0); logicalColumn < plan.parameters.L(); logicalColumn++ {
		originalColumn, mappingErr := binary.OriginalColumn(logicalColumn)
		if mappingErr != nil {
			return nil, mappingErr
		}
		var values []byte
		if logicalColumn < plan.finalI {
			physicalRow, rowErr := binary.PhysicalRow(logicalColumn)
			if rowErr != nil {
				return nil, rowErr
			}
			values, err = workingD.Symbol(physicalRow)
		} else {
			values, err = residualSolution.Symbol(logicalColumn - plan.finalI)
		}
		if err != nil {
			return nil, err
		}
		if err := result.SetSymbol(originalColumn, values); err != nil {
			return nil, err
		}
	}
	if observer != nil {
		inputValues, snapshotErr := snapshotGFSymbols(d)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		resultValues, snapshotErr := snapshotGFSymbols(result)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		if snapshotErr = emitHybridObservation(observer, HybridExecutionObservation{
			Stage: "complete", Step: plan.steps, InputD: inputValues, Result: resultValues,
		}); snapshotErr != nil {
			return nil, snapshotErr
		}
	}
	return result, nil
}

// cloneHybridRightHandSide converts canonical Figure-5 row order
// [LDPC,HDPC,encoded] into the executor's physical non-HDPC/HDPC slabs
// [LDPC,encoded,HDPC]. Keeping the conversion here prevents callers from
// accidentally satisfying only the shape check with a reordered RHS.
func cloneHybridRightHandSide(
	source *gf256.Symbols,
	plan *InactivationPlan,
	recordMap bool,
) (*gf256.Symbols, []uint32, error) {
	bytes := uint64(source.Count()) * uint64(source.Size())
	result, err := gf256.NewSymbols(source.Count(), source.Size(), bytes)
	if err != nil {
		return nil, nil, err
	}
	var rhsMap []uint32
	if recordMap {
		rhsMap = make([]uint32, source.Count())
	}
	copyRow := func(destinationRow, sourceRow uint32) error {
		values, symbolErr := source.Symbol(sourceRow)
		if symbolErr != nil {
			return symbolErr
		}
		if rhsMap != nil {
			rhsMap[destinationRow] = sourceRow
		}
		return result.SetSymbol(destinationRow, values)
	}
	for row := uint32(0); row < plan.parameters.S; row++ {
		if err := copyRow(row, row); err != nil {
			return nil, nil, err
		}
	}
	encodedRows := plan.rows - plan.parameters.S
	for row := uint32(0); row < encodedRows; row++ {
		if err := copyRow(plan.parameters.S+row, plan.parameters.S+plan.parameters.H+row); err != nil {
			return nil, nil, err
		}
	}
	for row := uint32(0); row < plan.parameters.H; row++ {
		if err := copyRow(plan.rows+row, plan.parameters.S+row); err != nil {
			return nil, nil, err
		}
	}
	return result, rhsMap, nil
}

func buildHybridHDPC(result *gf256.Matrix, parameters rfc6330.Parameters) error {
	if result.Rows() != parameters.H || result.Columns() != parameters.L() {
		return fmt.Errorf("%w: HDPC shape", ErrHybridPlanInvariant)
	}
	nonHDPCColumns := parameters.KPrime + parameters.S
	for hdpcRow := uint32(0); hdpcRow < parameters.H; hdpcRow++ {
		value := gf256.AlphaPower(hdpcRow)
		if err := result.Set(hdpcRow, nonHDPCColumns-1, value); err != nil {
			return err
		}
		for column := nonHDPCColumns - 1; column > 0; {
			column--
			value = gf256.Mul(2, value)
			first, err := rfc6330.Rand(column+1, 6, parameters.H)
			if err != nil {
				return err
			}
			secondOffset, err := rfc6330.Rand(column+1, 7, parameters.H-1)
			if err != nil {
				return err
			}
			second := (first + secondOffset + 1) % parameters.H
			if hdpcRow == first || hdpcRow == second {
				value ^= 1
			}
			if err := result.Set(hdpcRow, column, value); err != nil {
				return err
			}
		}
		if err := result.Set(hdpcRow, nonHDPCColumns+hdpcRow, 1); err != nil {
			return err
		}
	}
	return nil
}
