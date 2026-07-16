package codec

import (
	"errors"
	"testing"
	"time"

	"github.com/fgn/raptorgo/internal/rfc6330"
)

func TestSourceInactivationPlansMatchDenseFigureRows(t *testing.T) {
	t.Parallel()

	for _, k := range []uint32{1, 10, 11, 13, 48, 49} {
		parameters, err := rfc6330.ParametersForSourceSymbols(k)
		if err != nil {
			t.Fatal(err)
		}
		required, err := InactivationPlanBackingBytes(parameters.KPrime, parameters.KPrime)
		if err != nil {
			t.Fatal(err)
		}
		if plan, err := PlanSourceInactivation(parameters.KPrime, required-1); !errors.Is(err, ErrHybridPlanWorkspace) || plan != nil {
			t.Fatalf("K=%d budget-1 plan=%v error=%v", k, plan, err)
		}
		plan, err := PlanSourceInactivation(parameters.KPrime, required)
		if err != nil {
			t.Fatalf("K=%d: %v", k, err)
		}
		assertPlanConservation(t, plan)
		if plan.BackingBytes() != required {
			t.Fatalf("K=%d backing=%d want=%d", k, plan.BackingBytes(), required)
		}
		if plan.FinalI()+plan.FinalU() != parameters.L() {
			t.Fatalf("K=%d i+u=%d L=%d", k, plan.FinalI()+plan.FinalU(), parameters.L())
		}

		denseBytes, err := ConstraintMatrixBackingBytes(parameters.KPrime, uint64(parameters.KPrime))
		if err != nil {
			t.Fatal(err)
		}
		dense, err := BuildSourceConstraintMatrix(parameters.KPrime, denseBytes)
		if err != nil {
			t.Fatal(err)
		}
		for row := uint32(0); row < plan.rows; row++ {
			denseRow := row
			if row >= parameters.S {
				denseRow += parameters.H
			}
			for column := uint32(0); column < parameters.L(); column++ {
				want, atErr := dense.At(denseRow, column)
				if atErr != nil {
					t.Fatal(atErr)
				}
				if got := planInitialCoefficient(plan, row, column); got != (want != 0) {
					t.Fatalf("K=%d plan (%d,%d)=%v dense=%d", k, row, column, got, want)
				}
			}
		}
	}
}

func TestInactivationPlanEventsAreBoundedAndPermutationsRemainBijective(t *testing.T) {
	t.Parallel()

	parameters, err := rfc6330.ParametersForSourceSymbols(49)
	if err != nil {
		t.Fatal(err)
	}
	required, err := InactivationPlanBackingBytes(parameters.KPrime, parameters.KPrime)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanSourceInactivation(parameters.KPrime, required)
	if err != nil {
		t.Fatal(err)
	}
	assertPlanConservation(t, plan)
	seenRows := make([]bool, plan.rows)
	seenColumns := make([]bool, parameters.W)
	for step := uint32(0); step < plan.steps; step++ {
		row := plan.pivotRows[step]
		if row >= plan.rows || seenRows[row] {
			t.Fatalf("step %d pivot row=%d repeated/out-of-range", step, row)
		}
		seenRows[row] = true
		pivot := uint32(plan.pivotColumns[step])
		if pivot >= parameters.W || seenColumns[pivot] {
			t.Fatalf("step %d pivot column=%d repeated/out-of-range", step, pivot)
		}
		seenColumns[pivot] = true
		frozen := plan.frozenOffsets[step+1] - plan.frozenOffsets[step]
		if uint32(plan.stepDegrees[step]) != frozen+1 {
			t.Fatalf("step %d r=%d frozen=%d", step, plan.stepDegrees[step], frozen)
		}
		for index := plan.frozenOffsets[step]; index < plan.frozenOffsets[step+1]; index++ {
			column := uint32(plan.frozenColumns[index])
			if column >= parameters.W || seenColumns[column] {
				t.Fatalf("step %d frozen column=%d repeated/out-of-range", step, column)
			}
			seenColumns[column] = true
		}
		for index := plan.eliminationOffsets[step]; index < plan.eliminationOffsets[step+1]; index++ {
			if destination := plan.eliminationRows[index]; destination >= plan.rows || destination == row {
				t.Fatalf("step %d elimination destination=%d", step, destination)
			}
		}
	}
	if plan.eliminationCount > plan.InitialVNonzeros() {
		t.Fatalf("elimination events=%d > initial nnz=%d", plan.eliminationCount, plan.InitialVNonzeros())
	}
	assertUint32Permutation(t, plan.logicalToPhysicalRow, plan.rows)
	assertUint16Permutation(t, plan.logicalToPhysicalCol, parameters.L())
}

func TestSourceInactivationPlanAcceptedBoundaryIsOperational(t *testing.T) {
	for _, k := range []uint32{MaxEagerSourceSymbols, rfc6330.MaxSourceSymbolsPerBlock} {
		parameters, err := rfc6330.ParametersForSourceSymbols(k)
		if err != nil {
			t.Fatal(err)
		}
		required, err := InactivationPlanBackingBytes(parameters.KPrime, parameters.KPrime)
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		plan, err := PlanSourceInactivation(parameters.KPrime, required)
		if err != nil {
			t.Fatalf("K=%d: %v", k, err)
		}
		assertPlanConservation(t, plan)
		if plan.FinalI()+plan.FinalU() != parameters.L() {
			t.Fatalf("K=%d plan i/u/L=%d/%d/%d", k, plan.FinalI(), plan.FinalU(), parameters.L())
		}
		if elapsed := time.Since(started); elapsed > 10*time.Second {
			t.Fatalf("K=%d planning took %s", k, elapsed)
		}
		t.Logf("K=%d K'=%d plan=%d bytes steps=%d final-u=%d elapsed=%s", k, parameters.KPrime, required, plan.Steps(), plan.FinalU(), time.Since(started))
	}
}

func TestPlannerProgressWitnessCoverageAcrossAllParameterRows(t *testing.T) {
	previous := uint32(0)
	rows := 0
	for k := uint32(1); k <= rfc6330.MaxSourceSymbolsPerBlock; k++ {
		parameters, err := rfc6330.ParametersForSourceSymbols(k)
		if err != nil {
			t.Fatal(err)
		}
		if parameters.KPrime == previous {
			continue
		}
		previous = parameters.KPrime
		rows++
		witnesses := make([]uint16, parameters.W)
		for column := uint32(0); column < parameters.B(); column++ {
			a := 1 + column/parameters.S
			row := column % parameters.S
			seen := make(map[uint32]bool, 3)
			for count := 0; count < 3; count++ {
				if seen[row] {
					t.Fatalf("K'=%d V column %d repeats LDPC witness row %d", parameters.KPrime, column, row)
				}
				seen[row] = true
				witnesses[column]++
				row = (row + a) % parameters.S
			}
		}
		for row := uint32(0); row < parameters.S; row++ {
			witnesses[parameters.B()+row]++ // I_S is a unique V-column witness.
		}
		for column, count := range witnesses {
			if count == 0 {
				t.Fatalf("K'=%d V column %d has no initial witness row", parameters.KPrime, column)
			}
		}
	}
	if rows != 477 {
		t.Fatalf("checked %d Table 2 rows, want 477", rows)
	}
}

func TestSourceInactivationPlannerDoesNotStallAcrossBoundedTableRows(t *testing.T) {
	previous := uint32(0)
	checked := 0
	for k := uint32(1); k <= MaxEagerSourceSymbols; k++ {
		parameters, err := rfc6330.ParametersForSourceSymbols(k)
		if err != nil {
			t.Fatal(err)
		}
		if parameters.KPrime == previous {
			continue
		}
		previous = parameters.KPrime
		required, err := InactivationPlanBackingBytes(parameters.KPrime, parameters.KPrime)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := PlanSourceInactivation(parameters.KPrime, required)
		if err != nil {
			t.Fatalf("K'=%d: %v", parameters.KPrime, err)
		}
		assertPlanConservation(t, plan)
		checked++
	}
	if checked < 50 {
		t.Fatalf("only checked %d bounded Table 2 rows", checked)
	}
}

func assertPlanConservation(t testing.TB, plan *InactivationPlan) {
	t.Helper()
	if plan.ConsumedVIncidences()+plan.ResidualVIncidences() != plan.InitialVNonzeros() {
		t.Fatalf("consumed/residual/initial=%d/%d/%d", plan.ConsumedVIncidences(), plan.ResidualVIncidences(), plan.InitialVNonzeros())
	}
	if plan.ResidualVIncidences() != 0 {
		t.Fatalf("complete binary plan retained %d V incidences", plan.ResidualVIncidences())
	}
}

func planInitialCoefficient(plan *InactivationPlan, row, column uint32) bool {
	if column < plan.parameters.W {
		start := plan.vOffsets[row]
		for index := uint32(0); index < uint32(plan.vLengths[row]); index++ {
			if uint32(plan.vEntries[start+index]) == column {
				return true
			}
		}
		return false
	}
	start := plan.uOffsets[row]
	for index := uint32(0); index < uint32(plan.uLengths[row]); index++ {
		if uint32(plan.uEntries[start+index]) == column {
			return true
		}
	}
	return false
}

func assertUint32Permutation(t testing.TB, values []uint32, size uint32) {
	t.Helper()
	seen := make([]bool, size)
	for _, value := range values {
		if value >= size || seen[value] {
			t.Fatalf("not a uint32 permutation at %d", value)
		}
		seen[value] = true
	}
}

func assertUint16Permutation(t testing.TB, values []uint16, size uint32) {
	t.Helper()
	seen := make([]bool, size)
	for _, value := range values {
		if uint32(value) >= size || seen[value] {
			t.Fatalf("not a uint16 permutation at %d", value)
		}
		seen[value] = true
	}
}
