package codec

import (
	"bytes"
	"errors"
	"math/rand"
	"testing"

	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/matrix"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

func TestExecuteSourceInactivationPlanMatchesDenseOracle(t *testing.T) {
	for _, test := range []struct {
		k    uint32
		size uint32
	}{
		{k: 1, size: 1},
		{k: 1, size: 7},
		{k: 10, size: 7},
		{k: 11, size: 31},
		{k: 13, size: 7},
		{k: 48, size: 3},
		{k: 49, size: 7},
	} {
		t.Run(testName(test.k, test.size), func(t *testing.T) {
			source := patternedSymbols(t, test.k, test.size)
			parameters, err := rfc6330.ParametersForSourceSymbols(test.k)
			if err != nil {
				t.Fatal(err)
			}
			planBytes, err := InactivationPlanBackingBytes(parameters.KPrime, parameters.KPrime)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := PlanSourceInactivation(parameters.KPrime, planBytes)
			if err != nil {
				t.Fatal(err)
			}
			d := sourcePlanRightHandSide(t, source, plan)
			dBefore := snapshotSymbols(t, d)
			executionBytes, err := InactivationExecutionBackingBytes(plan, test.size)
			if err != nil {
				t.Fatal(err)
			}
			if got, executeErr := ExecuteInactivationPlan(plan, d, executionBytes-1); !errors.Is(executeErr, ErrHybridExecutionWorkspace) || got != nil {
				t.Fatalf("budget-1 result=%v error=%v", got, executeErr)
			}
			if !bytes.Equal(snapshotSymbols(t, d), dBefore) {
				t.Fatal("under-budget execution mutated D")
			}

			got, err := ExecuteInactivationPlan(plan, d, executionBytes)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(snapshotSymbols(t, d), dBefore) {
				t.Fatal("execution mutated D")
			}
			denseBytes, err := IntermediateWorkspaceBackingBytes(test.k, test.size)
			if err != nil {
				t.Fatal(err)
			}
			want, err := GenerateIntermediateSymbols(source, denseBytes)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(snapshotSymbols(t, got), snapshotSymbols(t, want)) {
				t.Fatal("hybrid intermediate symbols differ from dense oracle")
			}
			assertIntermediateEquations(t, source, parameters, got)
		})
	}
}

func TestExecuteCanonicalFigureFiveRightHandSideMatchesDense(t *testing.T) {
	t.Parallel()

	parameters, err := rfc6330.ParametersForSourceSymbols(10)
	if err != nil {
		t.Fatal(err)
	}
	planBytes, err := InactivationPlanBackingBytes(parameters.KPrime, parameters.KPrime)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSourceInactivation(parameters.KPrime, planBytes)
	if err != nil {
		t.Fatal(err)
	}
	d := patternedSymbols(t, plan.totalRows, 3)
	executionBytes, err := InactivationExecutionBackingBytes(plan, d.Size())
	if err != nil {
		t.Fatal(err)
	}
	got, err := ExecuteInactivationPlan(plan, d, executionBytes)
	if err != nil {
		t.Fatal(err)
	}
	matrixBytes, err := ConstraintMatrixBackingBytes(parameters.KPrime, uint64(parameters.KPrime))
	if err != nil {
		t.Fatal(err)
	}
	coefficients, err := BuildSourceConstraintMatrix(parameters.KPrime, matrixBytes)
	if err != nil {
		t.Fatal(err)
	}
	solveBytes, err := gf256.SolveWorkspaceBytes(coefficients.Rows(), coefficients.Columns(), d.Size())
	if err != nil {
		t.Fatal(err)
	}
	want, err := gf256.Solve(coefficients, d, solveBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshotSymbols(t, got), snapshotSymbols(t, want)) {
		t.Fatal("hybrid solver did not consume canonical [LDPC,HDPC,encoded] D order")
	}
}

func TestExecuteReceivedInactivationPlansMatchDenseOracle(t *testing.T) {
	t.Parallel()

	random := rand.New(rand.NewSource(0x6330))
	for _, k := range []uint32{10, 13} {
		source := patternedSymbols(t, k, 3)
		parameters, err := rfc6330.ParametersForSourceSymbols(k)
		if err != nil {
			t.Fatal(err)
		}
		intermediateBytes, err := IntermediateWorkspaceBackingBytes(k, source.Size())
		if err != nil {
			t.Fatal(err)
		}
		known, err := GenerateIntermediateSymbols(source, intermediateBytes)
		if err != nil {
			t.Fatal(err)
		}
		for iteration := 0; iteration < 200; iteration++ {
			encodedRows := parameters.KPrime + uint32(random.Intn(4))
			isis := make([]uint32, encodedRows)
			if iteration%2 == 0 {
				for index := uint32(0); index < parameters.KPrime; index++ {
					isis[index] = index
				}
				for index := parameters.KPrime; index < encodedRows; index++ {
					isis[index] = uint32(random.Intn(2_000))
				}
				random.Shuffle(len(isis), func(left, right int) { isis[left], isis[right] = isis[right], isis[left] })
			} else {
				for index := range isis {
					isis[index] = uint32(random.Intn(2_000))
				}
			}
			assertReceivedHybridMatchesDense(t, parameters, known, isis, false)
		}
	}
}

func TestExecuteReceivedInactivationDetectsOverheadConflict(t *testing.T) {
	t.Parallel()

	const k = uint32(10)
	source := patternedSymbols(t, k, 1)
	parameters, err := rfc6330.ParametersForSourceSymbols(k)
	if err != nil {
		t.Fatal(err)
	}
	intermediateBytes, err := IntermediateWorkspaceBackingBytes(k, source.Size())
	if err != nil {
		t.Fatal(err)
	}
	known, err := GenerateIntermediateSymbols(source, intermediateBytes)
	if err != nil {
		t.Fatal(err)
	}
	isis := make([]uint32, parameters.KPrime+1)
	for index := uint32(0); index < parameters.KPrime; index++ {
		isis[index] = index
	}
	isis[parameters.KPrime] = 1_000
	assertReceivedHybridMatchesDense(t, parameters, known, isis, true)
}

func assertReceivedHybridMatchesDense(
	t *testing.T,
	parameters rfc6330.Parameters,
	known *gf256.Symbols,
	isis []uint32,
	forgeLast bool,
) {
	t.Helper()
	matrixBytes, err := ConstraintMatrixBackingBytes(parameters.KPrime, uint64(len(isis)))
	if err != nil {
		t.Fatal(err)
	}
	coefficients, err := BuildConstraintMatrix(parameters.KPrime, isis, matrixBytes)
	if err != nil {
		t.Fatal(err)
	}
	dBytes := uint64(coefficients.Rows()) * uint64(known.Size())
	d, err := gf256.NewSymbols(coefficients.Rows(), known.Size(), dBytes)
	if err != nil {
		t.Fatal(err)
	}
	firstEncoded := parameters.S + parameters.H
	for index, isi := range isis {
		payload, err := EncodingSymbol(parameters.KPrime, isi, known, uint64(known.Size()))
		if err != nil {
			t.Fatal(err)
		}
		if forgeLast && index == len(isis)-1 {
			payload[0] ^= 1
		}
		if err := d.SetSymbol(firstEncoded+uint32(index), payload); err != nil {
			t.Fatal(err)
		}
	}
	solveBytes, err := gf256.SolveWorkspaceBytes(coefficients.Rows(), coefficients.Columns(), known.Size())
	if err != nil {
		t.Fatal(err)
	}
	want, denseErr := gf256.Solve(coefficients, d, solveBytes)
	planBytes, err := InactivationPlanBackingBytes(parameters.KPrime, uint32(len(isis)))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanReceivedInactivation(parameters.KPrime, isis, planBytes)
	if err != nil {
		t.Fatal(err)
	}
	executionBytes, err := InactivationExecutionBackingBytes(plan, known.Size())
	if err != nil {
		t.Fatal(err)
	}
	got, hybridErr := ExecuteInactivationPlan(plan, d, executionBytes)
	switch {
	case denseErr == nil:
		if hybridErr != nil {
			t.Fatalf("dense solved but hybrid error=%v", hybridErr)
		}
		if !bytes.Equal(snapshotSymbols(t, got), snapshotSymbols(t, want)) {
			t.Fatal("received hybrid solution differs from dense oracle")
		}
	case errors.Is(denseErr, gf256.ErrNoUniqueSolution):
		if !errors.Is(hybridErr, ErrInactivationRank) || got != nil {
			t.Fatalf("dense rank error=%v, hybrid result=%v error=%v", denseErr, got, hybridErr)
		}
	case errors.Is(denseErr, gf256.ErrInconsistentSystem):
		if !errors.Is(hybridErr, ErrInactivationConflict) || got != nil {
			t.Fatalf("dense conflict=%v, hybrid result=%v error=%v", denseErr, got, hybridErr)
		}
	default:
		t.Fatalf("unexpected dense error=%v", denseErr)
	}
}

func TestBuildHybridHDPCMatchesFigureFive(t *testing.T) {
	t.Parallel()

	for _, k := range []uint32{1, 10, 11, 49, 100} {
		parameters, err := rfc6330.ParametersForSourceSymbols(k)
		if err != nil {
			t.Fatal(err)
		}
		hdpcBytes := uint64(parameters.H) * uint64(parameters.L())
		hdpc, err := gf256.NewMatrix(parameters.H, parameters.L(), hdpcBytes)
		if err != nil {
			t.Fatal(err)
		}
		if err := buildHybridHDPC(hdpc, parameters); err != nil {
			t.Fatal(err)
		}
		denseBytes, err := ConstraintMatrixBackingBytes(parameters.KPrime, uint64(parameters.KPrime))
		if err != nil {
			t.Fatal(err)
		}
		figureFive, err := BuildSourceConstraintMatrix(parameters.KPrime, denseBytes)
		if err != nil {
			t.Fatal(err)
		}
		for row := uint32(0); row < parameters.H; row++ {
			for column := uint32(0); column < parameters.L(); column++ {
				got, err := hdpc.At(row, column)
				if err != nil {
					t.Fatal(err)
				}
				want, err := figureFive.At(parameters.S+row, column)
				if err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Fatalf("K=%d HDPC (%d,%d)=%d want=%d", k, row, column, got, want)
				}
			}
		}
	}
}

func TestInactivationExecutionValidation(t *testing.T) {
	t.Parallel()

	if _, err := InactivationExecutionBackingBytes(nil, 1); !errors.Is(err, ErrHybridExecutionWorkspace) {
		t.Fatalf("nil plan error=%v", err)
	}
	parameters, err := rfc6330.ParametersForSourceSymbols(1)
	if err != nil {
		t.Fatal(err)
	}
	planBytes, err := InactivationPlanBackingBytes(parameters.KPrime, parameters.KPrime)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSourceInactivation(parameters.KPrime, planBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InactivationExecutionBackingBytes(plan, 0); !errors.Is(err, ErrHybridExecutionWorkspace) {
		t.Fatalf("zero symbol size error=%v", err)
	}
	wrong, err := gf256.NewSymbols(plan.totalRows-1, 1, uint64(plan.totalRows-1))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := ExecuteInactivationPlan(plan, wrong, ^uint64(0)); !errors.Is(err, ErrInactivationShape) || result != nil {
		t.Fatalf("wrong D result=%v error=%v", result, err)
	}
	canonical, err := gf256.NewSymbols(plan.totalRows, 1, uint64(plan.totalRows))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*InactivationPlan){
		"row-count": func(candidate *InactivationPlan) { candidate.rows++ },
		"slice":     func(candidate *InactivationPlan) { candidate.vOffsets = candidate.vOffsets[:len(candidate.vOffsets)-1] },
		"event": func(candidate *InactivationPlan) {
			candidate.frozenOffsets[candidate.steps]++
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := *plan
			candidate.frozenOffsets = append([]uint32(nil), plan.frozenOffsets...)
			mutate(&candidate)
			if _, err := InactivationExecutionBackingBytes(&candidate, 1); !errors.Is(err, ErrHybridPlanInvariant) {
				t.Fatalf("malformed estimator error=%v", err)
			}
			if result, err := ExecuteInactivationPlan(&candidate, canonical, ^uint64(0)); !errors.Is(err, ErrHybridPlanInvariant) || result != nil {
				t.Fatalf("malformed execution result=%v error=%v", result, err)
			}
		})
	}
}

func TestInactivationExecutionRejectsForgedEliminationIncidence(t *testing.T) {
	t.Parallel()

	parameters, err := rfc6330.ParametersForSourceSymbols(10)
	if err != nil {
		t.Fatal(err)
	}
	planBytes, err := InactivationPlanBackingBytes(parameters.KPrime, parameters.KPrime)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSourceInactivation(parameters.KPrime, planBytes)
	if err != nil {
		t.Fatal(err)
	}
	forged := *plan
	forged.eliminationRows = append([]uint32(nil), plan.eliminationRows...)
	step := uint32(0)
	start, end := forged.eliminationOffsets[step], forged.eliminationOffsets[step+1]
	if start == end {
		t.Fatal("test plan has no first-step elimination")
	}
	pivot := uint32(forged.pivotColumns[step])
	columnStart, columnEnd := forged.columnOffsets[pivot], forged.columnOffsets[pivot+1]
	forgedRow := forged.rows
	for row := uint32(0); row < forged.rows; row++ {
		if forged.physicalToLogicalRow[row] <= step {
			continue
		}
		incident := false
		for index := columnStart; index < columnEnd; index++ {
			incident = incident || forged.columnRows[index] == row
		}
		if !incident {
			forgedRow = row
			break
		}
	}
	if forgedRow == forged.rows {
		t.Fatal("test plan has no active nonincident row")
	}
	forged.eliminationRows[start] = forgedRow
	if _, err := InactivationExecutionBackingBytes(&forged, 1); !errors.Is(err, ErrHybridPlanInvariant) {
		t.Fatalf("forged estimator error=%v", err)
	}
	d, err := gf256.NewSymbols(forged.totalRows, 1, uint64(forged.totalRows))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := ExecuteInactivationPlan(&forged, d, ^uint64(0)); !errors.Is(err, ErrHybridPlanInvariant) || result != nil {
		t.Fatalf("forged execution result=%v error=%v", result, err)
	}

	coordinated := *plan
	coordinated.eliminationRows = append([]uint32(nil), plan.eliminationRows...)
	coordinated.columnRows = append([]uint32(nil), plan.columnRows...)
	originalRow := coordinated.eliminationRows[start]
	coordinated.eliminationRows[start] = forgedRow
	replaced := false
	for index := columnStart; index < columnEnd; index++ {
		if coordinated.columnRows[index] == originalRow {
			coordinated.columnRows[index] = forgedRow
			replaced = true
			break
		}
	}
	if !replaced {
		t.Fatal("test plan reverse index does not contain elimination row")
	}
	if _, err := InactivationExecutionBackingBytes(&coordinated, 1); !errors.Is(err, ErrHybridPlanInvariant) {
		t.Fatalf("coordinated forged estimator error=%v", err)
	}
	if result, err := ExecuteInactivationPlan(&coordinated, d, ^uint64(0)); !errors.Is(err, ErrHybridPlanInvariant) || result != nil {
		t.Fatalf("coordinated forged execution result=%v error=%v", result, err)
	}
}

func TestObservedInactivationExecutionUsesOwnedSnapshots(t *testing.T) {
	t.Parallel()

	const k, size = uint32(10), uint32(3)
	parameters, err := rfc6330.ParametersForSourceSymbols(k)
	if err != nil {
		t.Fatal(err)
	}
	planBytes, err := InactivationPlanBackingBytes(parameters.KPrime, parameters.KPrime)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSourceInactivation(parameters.KPrime, planBytes)
	if err != nil {
		t.Fatal(err)
	}
	d := sourcePlanRightHandSide(t, patternedSymbols(t, k, size), plan)
	dBefore := snapshotSymbols(t, d)
	workspace, err := InactivationExecutionBackingBytes(plan, size)
	if err != nil {
		t.Fatal(err)
	}
	var stages []string
	result, err := ExecuteInactivationPlanObserved(plan, d, workspace, func(observation HybridExecutionObservation) error {
		stages = append(stages, observation.Stage)
		for _, values := range [][]byte{
			observation.Binary, observation.HDPC, observation.WorkingD,
			observation.ResidualCoefficients, observation.ResidualRHS,
			observation.ResidualSolution, observation.SubstitutionBefore,
			observation.SubstitutionAfter, observation.InputD, observation.Result,
		} {
			if len(values) != 0 {
				values[0] ^= 0xff
			}
		}
		if len(observation.RowMap) != 0 {
			observation.RowMap[0] = ^uint32(0)
		}
		if len(observation.RHSMap) != 0 {
			observation.RHSMap[0] = ^uint32(0)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshotSymbols(t, d), dBefore) {
		t.Fatal("observer mutation changed input D")
	}
	want, err := ExecuteInactivationPlan(plan, d, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshotSymbols(t, result), snapshotSymbols(t, want)) {
		t.Fatal("observer mutation changed execution result")
	}
	if len(stages) != int(4+2*plan.finalI) || stages[0] != "rhs" || stages[len(stages)-1] != "complete" {
		t.Fatalf("observed stages=%v", stages)
	}

	injected := errors.New("observer stopped")
	if result, err := ExecuteInactivationPlanObserved(plan, d, workspace, func(HybridExecutionObservation) error {
		return injected
	}); result != nil || !errors.Is(err, injected) || !errors.Is(err, ErrHybridPlanInvariant) {
		t.Fatalf("observer failure result=%v error=%v", result, err)
	}
	if !bytes.Equal(snapshotSymbols(t, d), dBefore) {
		t.Fatal("observer failure changed input D")
	}
}

func TestInactivationExecutionBackingBytesMatchesComponentSum(t *testing.T) {
	t.Parallel()

	parameters, err := rfc6330.ParametersForSourceSymbols(10)
	if err != nil {
		t.Fatal(err)
	}
	planBytes, err := InactivationPlanBackingBytes(parameters.KPrime, parameters.KPrime)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSourceInactivation(parameters.KPrime, planBytes)
	if err != nil {
		t.Fatal(err)
	}
	const symbolSize = uint32(3)
	got, err := InactivationExecutionBackingBytes(plan, symbolSize)
	if err != nil {
		t.Fatal(err)
	}
	hybridBytes, err := matrix.HybridBinaryBackingBytes(
		plan.rows, plan.parameters.L(), plan.parameters.P(), plan.finalU, uint64(plan.vNonzeros),
	)
	if err != nil {
		t.Fatal(err)
	}
	residualRows := plan.totalRows - plan.finalI
	residualColumns := plan.parameters.L() - plan.finalI
	solverBytes, err := gf256.SolveWorkspaceBytes(residualRows, residualColumns, symbolSize)
	if err != nil {
		t.Fatal(err)
	}
	want := hybridBytes +
		uint64(plan.parameters.H)*uint64(plan.parameters.L()) +
		uint64(plan.totalRows)*uint64(symbolSize) +
		uint64(residualRows)*uint64(residualColumns) +
		uint64(residualRows)*uint64(symbolSize) +
		solverBytes +
		uint64(residualColumns)*4 +
		uint64(plan.parameters.L())*uint64(symbolSize)
	if got != want {
		t.Fatalf("execution backing = %d, want component sum %d", got, want)
	}
}

func TestObservedSubstitutionRecordsDependencyColumns(t *testing.T) {
	t.Parallel()

	parameters, err := rfc6330.ParametersForSourceSymbols(10)
	if err != nil {
		t.Fatal(err)
	}
	planBytes, err := InactivationPlanBackingBytes(parameters.KPrime, parameters.KPrime)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSourceInactivation(parameters.KPrime, planBytes)
	if err != nil {
		t.Fatal(err)
	}
	d := sourcePlanRightHandSide(t, patternedSymbols(t, parameters.KPrime, 7), plan)
	workspace, err := InactivationExecutionBackingBytes(plan, d.Size())
	if err != nil {
		t.Fatal(err)
	}
	sawDependencies := false
	if _, err := ExecuteInactivationPlanObserved(plan, d, workspace, func(observation HybridExecutionObservation) error {
		if observation.Stage != "substitute" {
			return nil
		}
		columns := observation.SubstitutionColumns
		for index, column := range columns {
			if column < plan.finalI || column >= plan.parameters.L() {
				t.Fatalf("substitution column %d outside residual range", column)
			}
			if index > 0 && columns[index-1] >= column {
				t.Fatalf("substitution columns not strictly increasing: %v", columns)
			}
		}
		changed := !bytes.Equal(observation.SubstitutionBefore, observation.SubstitutionAfter)
		if changed && len(columns) == 0 {
			t.Fatalf("row %d changed without recorded dependencies", observation.SubstitutionRow)
		}
		if !changed && len(columns) != 0 {
			t.Fatalf("row %d recorded dependencies %v without changing", observation.SubstitutionRow, columns)
		}
		if len(columns) > 0 {
			sawDependencies = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !sawDependencies {
		t.Fatal("no substitution observation carried dependency columns; assertion is vacuous")
	}
}

func TestNilHybridSubstitutionObserverDoesNotAllocate(t *testing.T) {
	values := make([]byte, 1_024)
	allocations := testing.AllocsPerRun(1_000, func() {
		if err := emitHybridSubstitutionObservation(nil, 1, 2, nil, nil, values); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("nil substitution observer allocated %g objects per call", allocations)
	}
}

func sourcePlanRightHandSide(t testing.TB, source *gf256.Symbols, plan *InactivationPlan) *gf256.Symbols {
	t.Helper()
	bytes := uint64(plan.totalRows) * uint64(source.Size())
	d, err := gf256.NewSymbols(plan.totalRows, source.Size(), bytes)
	if err != nil {
		t.Fatal(err)
	}
	for index := uint32(0); index < source.Count(); index++ {
		values, err := source.Symbol(index)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.SetSymbol(plan.parameters.S+plan.parameters.H+index, values); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func testName(k, size uint32) string {
	return "K=" + decimal(k) + "/T=" + decimal(size)
}

func decimal(value uint32) string {
	if value == 0 {
		return "0"
	}
	var digits [10]byte
	index := len(digits)
	for value != 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
