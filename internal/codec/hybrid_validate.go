package codec

import (
	"fmt"
	"slices"

	"github.com/fgn/raptorgo/internal/rfc6330"
)

// validateInactivationPlan rejects malformed replay artifacts before any
// executor indexing or allocation. Plans currently stay in memory and expose
// no mutators, but this boundary also keeps future persistence/caching safe.
// Validation allocates no backing.
func validateInactivationPlan(plan *InactivationPlan) error {
	if plan == nil {
		return fmt.Errorf("%w: nil plan", ErrHybridPlanInvariant)
	}
	wantParameters, err := rfc6330.ParametersForExtendedSourceBlock(plan.parameters.KPrime)
	if err != nil || plan.parameters != wantParameters {
		return fmt.Errorf("%w: noncanonical parameters", ErrHybridPlanInvariant)
	}
	if plan.rows < plan.parameters.S || uint64(plan.rows)+uint64(plan.parameters.H) != uint64(plan.totalRows) {
		return fmt.Errorf("%w: rows=%d total=%d", ErrHybridPlanInvariant, plan.rows, plan.totalRows)
	}
	encodedRows := plan.rows - plan.parameters.S
	wantBacking, err := InactivationPlanBackingBytes(plan.parameters.KPrime, encodedRows)
	if err != nil || plan.backingBytes != wantBacking {
		return fmt.Errorf("%w: backing=%d want=%d: %v", ErrHybridPlanInvariant, plan.backingBytes, wantBacking, err)
	}
	wantVCapacity := uint64(3)*uint64(plan.parameters.B()) + uint64(plan.parameters.S) + uint64(encodedRows)*30
	wantUCapacity := uint64(2)*uint64(plan.parameters.S) + uint64(encodedRows)*3
	if wantVCapacity != uint64(plan.vCapacity) || wantUCapacity != uint64(plan.uCapacity) {
		return fmt.Errorf("%w: incidence capacities", ErrHybridPlanInvariant)
	}
	rows, columns, w := uint64(plan.rows), uint64(plan.parameters.L()), uint64(plan.parameters.W)
	if !lengthsAre(
		[]int{
			len(plan.vOffsets), len(plan.vLengths), len(plan.vEntries),
			len(plan.uOffsets), len(plan.uLengths), len(plan.uEntries),
			len(plan.columnOffsets), len(plan.columnRows),
			len(plan.logicalToPhysicalRow), len(plan.physicalToLogicalRow),
			len(plan.logicalToPhysicalCol), len(plan.physicalToLogicalCol),
			len(plan.degree), len(plan.originalDegree), len(plan.active),
			len(plan.bucketHeads), len(plan.bucketNext), len(plan.bucketPrevious),
			len(plan.componentParent), len(plan.componentSize), len(plan.columnScratch),
			len(plan.pivotRows), len(plan.pivotColumns), len(plan.stepDegrees),
			len(plan.frozenOffsets), len(plan.eliminationOffsets),
			len(plan.frozenColumns), len(plan.eliminationRows),
		},
		[]uint64{
			rows + 1, rows, uint64(plan.vCapacity),
			rows + 1, rows, uint64(plan.uCapacity),
			w + 1, uint64(plan.vCapacity),
			rows, rows, columns, columns,
			rows, rows, rows,
			w + 1, rows, rows,
			w, w, w,
			w, w, w,
			w + 1, w + 1,
			w, uint64(plan.vCapacity),
		},
	) {
		return fmt.Errorf("%w: slice shape", ErrHybridPlanInvariant)
	}
	if plan.steps != plan.finalI || plan.steps > plan.parameters.W ||
		plan.frozenCount > plan.parameters.W || plan.eliminationCount > plan.vCapacity ||
		plan.finalU != plan.parameters.P()+plan.frozenCount ||
		plan.finalI+plan.finalU != plan.parameters.L() || plan.finalI > plan.rows ||
		plan.vNonzeros > plan.vCapacity || plan.consumed+plan.residual != plan.vNonzeros || plan.residual != 0 {
		return fmt.Errorf("%w: terminal counters", ErrHybridPlanInvariant)
	}
	if err := validatePlanRows(plan); err != nil {
		return err
	}
	if err := validatePlanColumns(plan); err != nil {
		return err
	}
	if err := validatePlanEvents(plan); err != nil {
		return err
	}
	return nil
}

func lengthsAre(actual []int, expected []uint64) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if uint64(actual[index]) != expected[index] {
			return false
		}
	}
	return true
}

func validatePlanRows(plan *InactivationPlan) error {
	if plan.vOffsets[0] != 0 || plan.uOffsets[0] != 0 {
		return fmt.Errorf("%w: nonzero row-offset origin", ErrHybridPlanInvariant)
	}
	var nonzeros uint64
	for row := uint32(0); row < plan.rows; row++ {
		vStart, vEnd := plan.vOffsets[row], plan.vOffsets[row+1]
		uStart, uEnd := plan.uOffsets[row], plan.uOffsets[row+1]
		if vStart > vEnd || vEnd > plan.vCapacity || uint64(vStart)+uint64(plan.vLengths[row]) > uint64(vEnd) ||
			uStart > uEnd || uEnd > plan.uCapacity || uint64(uStart)+uint64(plan.uLengths[row]) > uint64(uEnd) {
			return fmt.Errorf("%w: row %d offsets/lengths", ErrHybridPlanInvariant, row)
		}
		previous := uint16(0)
		for index := uint32(0); index < uint32(plan.vLengths[row]); index++ {
			column := plan.vEntries[vStart+index]
			if uint32(column) >= plan.parameters.W || index != 0 && column <= previous {
				return fmt.Errorf("%w: row %d V entry %d", ErrHybridPlanInvariant, row, column)
			}
			previous = column
			nonzeros++
		}
		previous = 0
		for index := uint32(0); index < uint32(plan.uLengths[row]); index++ {
			column := plan.uEntries[uStart+index]
			if uint32(column) < plan.parameters.W || uint32(column) >= plan.parameters.L() || index != 0 && column <= previous {
				return fmt.Errorf("%w: row %d U entry %d", ErrHybridPlanInvariant, row, column)
			}
			previous = column
		}
		if plan.active[row] != 0 && plan.degree[row] != 0 {
			return fmt.Errorf("%w: terminal degree row %d is %d", ErrHybridPlanInvariant, row, plan.degree[row])
		}
	}
	if plan.vOffsets[plan.rows] > plan.vCapacity || plan.uOffsets[plan.rows] > plan.uCapacity || nonzeros != uint64(plan.vNonzeros) {
		return fmt.Errorf("%w: terminal row totals", ErrHybridPlanInvariant)
	}
	return nil
}

func validatePlanColumns(plan *InactivationPlan) error {
	for logical, physical := range plan.logicalToPhysicalRow {
		if physical >= plan.rows || plan.physicalToLogicalRow[physical] != uint32(logical) {
			return fmt.Errorf("%w: row permutation at %d", ErrHybridPlanInvariant, logical)
		}
	}
	for logical, physical := range plan.logicalToPhysicalCol {
		if uint32(physical) >= plan.parameters.L() || plan.physicalToLogicalCol[physical] != uint16(logical) {
			return fmt.Errorf("%w: column permutation at %d", ErrHybridPlanInvariant, logical)
		}
	}
	if plan.columnOffsets[0] != 0 || plan.columnOffsets[plan.parameters.W] != plan.vNonzeros {
		return fmt.Errorf("%w: column-index endpoints", ErrHybridPlanInvariant)
	}
	for column := uint32(0); column < plan.parameters.W; column++ {
		start, end := plan.columnOffsets[column], plan.columnOffsets[column+1]
		if start > end || end > plan.vNonzeros {
			return fmt.Errorf("%w: column-index offsets at %d", ErrHybridPlanInvariant, column)
		}
		previous := plan.rows
		for index := start; index < end; index++ {
			row := plan.columnRows[index]
			if row >= previous {
				return fmt.Errorf("%w: column %d row order at %d", ErrHybridPlanInvariant, column, row)
			}
			previous = row
			rowStart := plan.vOffsets[row]
			rowEnd := rowStart + uint32(plan.vLengths[row])
			if _, present := slices.BinarySearch(plan.vEntries[rowStart:rowEnd], uint16(column)); !present {
				return fmt.Errorf("%w: reverse incidence column %d row %d", ErrHybridPlanInvariant, column, row)
			}
		}
	}
	return nil
}

func validatePlanEvents(plan *InactivationPlan) error {
	if plan.frozenOffsets[0] != 0 || plan.eliminationOffsets[0] != 0 ||
		plan.frozenOffsets[plan.steps] != plan.frozenCount ||
		plan.eliminationOffsets[plan.steps] != plan.eliminationCount {
		return fmt.Errorf("%w: event-offset endpoints", ErrHybridPlanInvariant)
	}
	for step := uint32(0); step < plan.steps; step++ {
		frozenStart, frozenEnd := plan.frozenOffsets[step], plan.frozenOffsets[step+1]
		eliminationStart, eliminationEnd := plan.eliminationOffsets[step], plan.eliminationOffsets[step+1]
		if frozenStart > frozenEnd || frozenEnd > plan.frozenCount ||
			eliminationStart > eliminationEnd || eliminationEnd > plan.eliminationCount ||
			plan.stepDegrees[step] == 0 || frozenEnd-frozenStart+1 != uint32(plan.stepDegrees[step]) {
			return fmt.Errorf("%w: event offsets at step %d", ErrHybridPlanInvariant, step)
		}
		pivotRow, pivotColumn := plan.pivotRows[step], uint32(plan.pivotColumns[step])
		if pivotRow >= plan.rows || pivotColumn >= plan.parameters.W ||
			plan.physicalToLogicalRow[pivotRow] != step || uint32(plan.physicalToLogicalCol[pivotColumn]) != step {
			return fmt.Errorf("%w: pivot at step %d", ErrHybridPlanInvariant, step)
		}
		for index := frozenStart; index < frozenEnd; index++ {
			column := uint32(plan.frozenColumns[index])
			if column >= plan.parameters.W || uint32(plan.physicalToLogicalCol[column]) < plan.finalI {
				return fmt.Errorf("%w: frozen column at step %d", ErrHybridPlanInvariant, step)
			}
		}
		for index := eliminationStart; index < eliminationEnd; index++ {
			row := plan.eliminationRows[index]
			if row >= plan.rows || plan.physicalToLogicalRow[row] <= step {
				return fmt.Errorf("%w: elimination row at step %d", ErrHybridPlanInvariant, step)
			}
		}
		// The planner records exactly the still-active physical rows incident
		// to the pivot's original sparse column, in column-index order. Replay
		// applies an irreversible row XOR for every record, so range and final
		// permutation checks alone are not sufficient for persisted/forged
		// plans.
		wantIndex := eliminationStart
		columnStart, columnEnd := plan.columnOffsets[pivotColumn], plan.columnOffsets[pivotColumn+1]
		for index := columnStart; index < columnEnd; index++ {
			row := plan.columnRows[index]
			if plan.physicalToLogicalRow[row] <= step {
				continue
			}
			if wantIndex == eliminationEnd || plan.eliminationRows[wantIndex] != row {
				return fmt.Errorf("%w: elimination incidence at step %d", ErrHybridPlanInvariant, step)
			}
			wantIndex++
		}
		if wantIndex != eliminationEnd {
			return fmt.Errorf("%w: extra elimination row at step %d", ErrHybridPlanInvariant, step)
		}
	}
	return nil
}
