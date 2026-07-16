package codec

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/fgn/raptorgo/internal/rfc6330"
)

var (
	ErrHybridPlanWorkspace = errors.New("codec: hybrid inactivation plan workspace limit exceeded")
	ErrHybridPlanInvariant = errors.New("codec: hybrid inactivation plan invariant failed")
)

const noPlanIndex = ^uint32(0)

// InactivationPlan is an immutable RFC-structured binary phase-one plan. It
// retains canonical non-HDPC V/U rows and bounded replay events. HDPC rows are
// deliberately absent from binary selection and are incorporated by the
// executor's unconditional dense residual completion.
type InactivationPlan struct {
	parameters rfc6330.Parameters
	rows       uint32
	totalRows  uint32
	vCapacity  uint32
	uCapacity  uint32
	vNonzeros  uint32

	vOffsets []uint32
	vLengths []uint16
	vEntries []uint16
	uOffsets []uint32
	uLengths []uint16
	uEntries []uint16

	columnOffsets []uint32
	columnRows    []uint32

	logicalToPhysicalRow []uint32
	physicalToLogicalRow []uint32
	logicalToPhysicalCol []uint16
	physicalToLogicalCol []uint16
	degree               []uint16
	originalDegree       []uint16
	active               []byte
	bucketHeads          []uint32
	bucketNext           []uint32
	bucketPrevious       []uint32
	componentParent      []uint16
	componentSize        []uint32
	columnScratch        []uint16

	pivotRows          []uint32
	pivotColumns       []uint16
	stepDegrees        []uint16
	frozenOffsets      []uint32
	eliminationOffsets []uint32
	frozenColumns      []uint16
	eliminationRows    []uint32

	steps            uint32
	frozenCount      uint32
	eliminationCount uint32
	finalI           uint32
	finalU           uint32
	consumed         uint32
	residual         uint32
	backingBytes     uint64
}

// InactivationPlanBackingBytes returns the bounded planning backing for the
// specified Figure-5 shape. Encoded rows are charged 30 canonical LT visits
// and 3 PI visits each; actual odd-visit counts may be lower but never higher.
func InactivationPlanBackingBytes(kPrime, encodedRows uint32) (uint64, error) {
	parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
	if err != nil {
		return 0, err
	}
	rows64 := uint64(parameters.S) + uint64(encodedRows)
	baseRows64 := uint64(parameters.S) + uint64(parameters.H) + uint64(encodedRows)
	if rows64 > math.MaxUint32 || baseRows64 > math.MaxUint32 {
		return 0, fmt.Errorf("%w: row count overflow", ErrHybridPlanWorkspace)
	}
	vCapacity := uint64(3)*uint64(parameters.B()) + uint64(parameters.S) + uint64(encodedRows)*30
	uCapacity := uint64(2)*uint64(parameters.S) + uint64(encodedRows)*3
	if vCapacity > math.MaxUint32 || uCapacity > math.MaxUint32 {
		return 0, fmt.Errorf("%w: incidence count overflow", ErrHybridPlanWorkspace)
	}
	rows, columns, w := rows64, uint64(parameters.L()), uint64(parameters.W)
	parts := [...]uint64{
		(rows + 1) * 4, vCapacity * 2, rows * 2, // V row storage
		(rows + 1) * 4, uCapacity * 2, rows * 2, // initial U row storage
		(w + 1) * 4, vCapacity * 4, // immutable reverse V incidence
		rows * 8, columns * 4, // row and column permutations
		rows * 4, rows, rows * 8, // degree/original, active, bucket links
		(w + 1) * 4, w * 2, w * 4, w * 2, // buckets and graph/scratch
		w * 8, (w + 1) * 8, w * 2, vCapacity * 4, // replay events
	}
	return checkedBackingSum(ErrHybridPlanWorkspace, parts[:]...)
}

// PlanSourceInactivation builds the binary phase-one plan for the square
// source system with encoded ISIs [0,K').
func PlanSourceInactivation(kPrime uint32, maxWorkspace uint64) (*InactivationPlan, error) {
	return planInactivation(kPrime, kPrime, func(index uint32) uint32 { return index }, maxWorkspace)
}

// PlanReceivedInactivation builds a plan for encoded rows in caller order.
// The ISI slice is borrowed only for construction and is never retained.
func PlanReceivedInactivation(kPrime uint32, isis []uint32, maxWorkspace uint64) (*InactivationPlan, error) {
	return planInactivation(kPrime, uint32(len(isis)), func(index uint32) uint32 { return isis[index] }, maxWorkspace)
}

func planInactivation(
	kPrime, encodedRows uint32,
	isiAt func(uint32) uint32,
	maxWorkspace uint64,
) (*InactivationPlan, error) {
	parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
	if err != nil {
		return nil, err
	}
	required, err := InactivationPlanBackingBytes(kPrime, encodedRows)
	if err != nil {
		return nil, err
	}
	if required > maxWorkspace {
		return nil, fmt.Errorf("%w: need %d bytes (limit %d)", ErrHybridPlanWorkspace, required, maxWorkspace)
	}
	rows := parameters.S + encodedRows
	totalRows := rows + parameters.H
	vCapacity := uint32(uint64(3)*uint64(parameters.B()) + uint64(parameters.S) + uint64(encodedRows)*30)
	uCapacity := uint32(uint64(2)*uint64(parameters.S) + uint64(encodedRows)*3)
	w := parameters.W
	plan := &InactivationPlan{
		parameters: parameters,
		rows:       rows,
		totalRows:  totalRows,
		vCapacity:  vCapacity,
		uCapacity:  uCapacity,
		vOffsets:   make([]uint32, int(rows)+1),
		vLengths:   make([]uint16, int(rows)),
		vEntries:   make([]uint16, int(vCapacity)),
		uOffsets:   make([]uint32, int(rows)+1),
		uLengths:   make([]uint16, int(rows)),
		uEntries:   make([]uint16, int(uCapacity)),

		columnOffsets: make([]uint32, int(w)+1),
		columnRows:    make([]uint32, int(vCapacity)),

		logicalToPhysicalRow: make([]uint32, int(rows)),
		physicalToLogicalRow: make([]uint32, int(rows)),
		logicalToPhysicalCol: make([]uint16, int(parameters.L())),
		physicalToLogicalCol: make([]uint16, int(parameters.L())),
		degree:               make([]uint16, int(rows)),
		originalDegree:       make([]uint16, int(rows)),
		active:               make([]byte, int(rows)),
		bucketHeads:          make([]uint32, int(w)+1),
		bucketNext:           make([]uint32, int(rows)),
		bucketPrevious:       make([]uint32, int(rows)),
		componentParent:      make([]uint16, int(w)),
		componentSize:        make([]uint32, int(w)),
		columnScratch:        make([]uint16, int(w)),

		pivotRows:          make([]uint32, int(w)),
		pivotColumns:       make([]uint16, int(w)),
		stepDegrees:        make([]uint16, int(w)),
		frozenOffsets:      make([]uint32, int(w)+1),
		eliminationOffsets: make([]uint32, int(w)+1),
		frozenColumns:      make([]uint16, int(w)),
		eliminationRows:    make([]uint32, int(vCapacity)),
		finalU:             parameters.P(),
		backingBytes:       required,
	}
	plan.resetSentinels()
	if err := plan.buildRows(encodedRows, isiAt); err != nil {
		return nil, err
	}
	if err := plan.buildColumnIndex(); err != nil {
		return nil, err
	}
	plan.initializeSelection()
	if err := plan.formPlan(); err != nil {
		return nil, err
	}
	return plan, nil
}

func (plan *InactivationPlan) BackingBytes() uint64        { return plan.backingBytes }
func (plan *InactivationPlan) Steps() uint32               { return plan.steps }
func (plan *InactivationPlan) FinalI() uint32              { return plan.finalI }
func (plan *InactivationPlan) FinalU() uint32              { return plan.finalU }
func (plan *InactivationPlan) InitialVNonzeros() uint32    { return plan.vNonzeros }
func (plan *InactivationPlan) ConsumedVIncidences() uint32 { return plan.consumed }
func (plan *InactivationPlan) ResidualVIncidences() uint32 { return plan.residual }

func (plan *InactivationPlan) resetSentinels() {
	for index := range plan.bucketHeads {
		plan.bucketHeads[index] = noPlanIndex
	}
	for index := range plan.bucketNext {
		plan.bucketNext[index] = noPlanIndex
		plan.bucketPrevious[index] = noPlanIndex
	}
}

func (plan *InactivationPlan) buildRows(encodedRows uint32, isiAt func(uint32) uint32) error {
	parameters := plan.parameters
	// Count fixed LDPC capacities.
	for column := uint32(0); column < parameters.B(); column++ {
		a := 1 + column/parameters.S
		row := column % parameters.S
		for count := 0; count < 3; count++ {
			plan.vLengths[row]++
			row = (row + a) % parameters.S
		}
	}
	for row := uint32(0); row < parameters.S; row++ {
		plan.vLengths[row]++ // I_S
		plan.uLengths[row] += 2
	}
	var visits [33]uint16
	var canonicalV [33]uint16
	var canonicalU [33]uint16
	for index := uint32(0); index < encodedRows; index++ {
		vCount, uCount, err := canonicalEncodingColumns(parameters.KPrime, isiAt(index), visits[:], canonicalV[:], canonicalU[:])
		if err != nil {
			return fmt.Errorf("%w: canonical G_ENC row %d: %w", ErrHybridPlanInvariant, index, err)
		}
		row := parameters.S + index
		plan.vLengths[row] = uint16(vCount)
		plan.uLengths[row] = uint16(uCount)
	}
	for row := uint32(0); row < plan.rows; row++ {
		plan.vOffsets[row+1] = plan.vOffsets[row] + uint32(plan.vLengths[row])
		plan.uOffsets[row+1] = plan.uOffsets[row] + uint32(plan.uLengths[row])
	}
	if plan.vOffsets[plan.rows] > plan.vCapacity || plan.uOffsets[plan.rows] > plan.uCapacity {
		return fmt.Errorf("%w: row capacity exceeded", ErrHybridPlanInvariant)
	}

	// degree/originalDegree are temporary per-row fill cursors here.
	for column := uint32(0); column < parameters.B(); column++ {
		a := 1 + column/parameters.S
		row := column % parameters.S
		for count := 0; count < 3; count++ {
			plan.putV(row, uint16(column))
			row = (row + a) % parameters.S
		}
	}
	for row := uint32(0); row < parameters.S; row++ {
		plan.putV(row, uint16(parameters.B()+row))
		plan.putU(row, uint16(parameters.W+row%parameters.P()))
		plan.putU(row, uint16(parameters.W+(row+1)%parameters.P()))
	}
	for index := uint32(0); index < encodedRows; index++ {
		vCount, uCount, err := canonicalEncodingColumns(parameters.KPrime, isiAt(index), visits[:], canonicalV[:], canonicalU[:])
		if err != nil {
			return err
		}
		row := parameters.S + index
		copy(plan.vEntries[plan.vOffsets[row]:], canonicalV[:vCount])
		copy(plan.uEntries[plan.uOffsets[row]:], canonicalU[:uCount])
		plan.degree[row] = uint16(vCount)
		plan.originalDegree[row] = uint16(uCount)
	}

	// Canonicalize Set-style LDPC entries and restore lengths. Encoded rows are
	// already XOR-canonical and sorted.
	for row := uint32(0); row < parameters.S; row++ {
		vCap := uint32(plan.vLengths[row])
		uCap := uint32(plan.uLengths[row])
		plan.vLengths[row] = uint16(uniqueSorted(plan.vEntries[plan.vOffsets[row] : plan.vOffsets[row]+vCap]))
		plan.uLengths[row] = uint16(uniqueSorted(plan.uEntries[plan.uOffsets[row] : plan.uOffsets[row]+uCap]))
	}
	for row := parameters.S; row < plan.rows; row++ {
		plan.vLengths[row] = plan.degree[row]
		plan.uLengths[row] = plan.originalDegree[row]
	}
	clear(plan.degree)
	clear(plan.originalDegree)
	return nil
}

func (plan *InactivationPlan) putV(row uint32, column uint16) {
	position := plan.vOffsets[row] + uint32(plan.degree[row])
	plan.vEntries[position] = column
	plan.degree[row]++
}

func (plan *InactivationPlan) putU(row uint32, column uint16) {
	position := plan.uOffsets[row] + uint32(plan.originalDegree[row])
	plan.uEntries[position] = column
	plan.originalDegree[row]++
}

func uniqueSorted(values []uint16) uint32 {
	if len(values) == 0 {
		return 0
	}
	slices.Sort(values)
	output := 1
	for index := 1; index < len(values); index++ {
		if values[index] != values[output-1] {
			values[output] = values[index]
			output++
		}
	}
	return uint32(output)
}

func canonicalEncodingColumns(
	kPrime, isi uint32,
	visits, vOutput, uOutput []uint16,
) (int, int, error) {
	parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
	if err != nil {
		return 0, 0, err
	}
	count := 0
	if err := rfc6330.VisitEncodingIndices(kPrime, isi, func(column uint32) error {
		if count == len(visits) || column > math.MaxUint16 {
			return fmt.Errorf("%w: too many or wide visits", ErrHybridPlanInvariant)
		}
		visits[count] = uint16(column)
		count++
		return nil
	}); err != nil {
		return 0, 0, err
	}
	slices.Sort(visits[:count])
	vCount, uCount := 0, 0
	for start := 0; start < count; {
		end := start + 1
		for end < count && visits[end] == visits[start] {
			end++
		}
		if (end-start)%2 == 1 {
			if uint32(visits[start]) < parameters.W {
				vOutput[vCount] = visits[start]
				vCount++
			} else {
				uOutput[uCount] = visits[start]
				uCount++
			}
		}
		start = end
	}
	return vCount, uCount, nil
}

func (plan *InactivationPlan) buildColumnIndex() error {
	for row := uint32(0); row < plan.rows; row++ {
		start := plan.vOffsets[row]
		for index := uint32(0); index < uint32(plan.vLengths[row]); index++ {
			column := uint32(plan.vEntries[start+index])
			plan.columnOffsets[column+1]++
			plan.vNonzeros++
		}
	}
	for column := 1; column < len(plan.columnOffsets); column++ {
		plan.columnOffsets[column] += plan.columnOffsets[column-1]
	}
	for row := uint32(0); row < plan.rows; row++ {
		start := plan.vOffsets[row]
		for index := uint32(0); index < uint32(plan.vLengths[row]); index++ {
			column := uint32(plan.vEntries[start+index])
			plan.columnOffsets[column+1]--
			plan.columnRows[plan.columnOffsets[column+1]] = row
		}
	}
	for column := 0; column < len(plan.columnOffsets)-1; column++ {
		plan.columnOffsets[column] = plan.columnOffsets[column+1]
	}
	plan.columnOffsets[len(plan.columnOffsets)-1] = plan.vNonzeros
	if plan.vNonzeros > plan.vCapacity {
		return fmt.Errorf("%w: V nonzeros %d > %d", ErrHybridPlanInvariant, plan.vNonzeros, plan.vCapacity)
	}
	return nil
}

func (plan *InactivationPlan) initializeSelection() {
	for row := uint32(0); row < plan.rows; row++ {
		plan.logicalToPhysicalRow[row] = row
		plan.physicalToLogicalRow[row] = row
		plan.active[row] = 1
		degree := plan.vLengths[row]
		plan.degree[row] = degree
		plan.originalDegree[row] = degree
		if degree != 0 {
			plan.bucketInsert(row, degree)
		}
	}
	for column := uint32(0); column < plan.parameters.L(); column++ {
		plan.logicalToPhysicalCol[column] = uint16(column)
		plan.physicalToLogicalCol[column] = uint16(column)
	}
}

func (plan *InactivationPlan) formPlan() error {
	l := plan.parameters.L()
	for plan.finalI+plan.finalU < l {
		chosen, r, ok, err := plan.selectRow(plan.finalI, l-plan.finalU)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf(
				"%w: planner stalled with %d active V columns",
				ErrHybridPlanInvariant, l-plan.finalI-plan.finalU,
			)
		}
		if plan.steps >= plan.parameters.W || r == 0 {
			return fmt.Errorf("%w: invalid step=%d r=%d", ErrHybridPlanInvariant, plan.steps, r)
		}
		columns, err := plan.activePhysicalColumns(chosen, plan.finalI, l-plan.finalU, plan.columnScratch[:0])
		if err != nil {
			return err
		}
		if uint32(len(columns)) != r {
			return fmt.Errorf("%w: selected row degree %d became %d", ErrHybridPlanInvariant, r, len(columns))
		}
		pivot := columns[0]
		plan.swapPhysicalColumnToLogical(pivot, plan.finalI)
		freezeStart := l - plan.finalU - (r - 1)
		for index := uint32(1); index < r; index++ {
			plan.swapPhysicalColumnToLogical(columns[index], freezeStart+index-1)
			plan.frozenColumns[plan.frozenCount] = columns[index]
			plan.frozenCount++
		}

		step := plan.steps
		plan.pivotRows[step] = chosen
		plan.pivotColumns[step] = pivot
		plan.stepDegrees[step] = uint16(r)
		plan.frozenOffsets[step] = plan.frozenCount - (r - 1)
		plan.eliminationOffsets[step] = plan.eliminationCount

		logicalChosen := plan.physicalToLogicalRow[chosen]
		plan.swapLogicalRows(plan.finalI, logicalChosen)
		plan.bucketRemove(chosen, plan.degree[chosen])
		plan.active[chosen] = 0
		plan.consumed += r

		start, end := plan.columnOffsets[pivot], plan.columnOffsets[pivot+1]
		for index := start; index < end; index++ {
			row := plan.columnRows[index]
			if plan.active[row] == 0 {
				continue
			}
			if plan.eliminationCount == plan.vCapacity {
				return fmt.Errorf("%w: elimination event capacity", ErrHybridPlanInvariant)
			}
			plan.eliminationRows[plan.eliminationCount] = row
			plan.eliminationCount++
		}
		for _, column := range columns {
			if err := plan.removeActiveColumn(column); err != nil {
				return err
			}
		}
		plan.steps++
		plan.frozenOffsets[plan.steps] = plan.frozenCount
		plan.eliminationOffsets[plan.steps] = plan.eliminationCount
		plan.finalI++
		plan.finalU += r - 1
		if plan.finalI+plan.finalU > l {
			return fmt.Errorf("%w: i+u=%d > L=%d", ErrHybridPlanInvariant, plan.finalI+plan.finalU, l)
		}
	}
	for row := uint32(0); row < plan.rows; row++ {
		if plan.active[row] != 0 {
			plan.residual += uint32(plan.degree[row])
		}
	}
	if plan.consumed+plan.residual != plan.vNonzeros {
		return fmt.Errorf(
			"%w: consumed %d + residual %d != initial %d",
			ErrHybridPlanInvariant, plan.consumed, plan.residual, plan.vNonzeros,
		)
	}
	if plan.finalI+plan.finalU != l {
		return fmt.Errorf("%w: i+u=%d L=%d", ErrHybridPlanInvariant, plan.finalI+plan.finalU, l)
	}
	return nil
}

func (plan *InactivationPlan) selectRow(start, end uint32) (uint32, uint32, bool, error) {
	var r uint32
	for degree := uint32(1); degree < uint32(len(plan.bucketHeads)); degree++ {
		if plan.bucketHeads[degree] != noPlanIndex {
			r = degree
			break
		}
	}
	if r == 0 {
		return 0, 0, false, nil
	}
	if r == 2 {
		row, err := plan.selectDegreeTwo(start, end)
		return row, r, err == nil, err
	}
	chosen := noPlanIndex
	bestOriginal := uint16(math.MaxUint16)
	for row := plan.bucketHeads[r]; row != noPlanIndex; row = plan.bucketNext[row] {
		if plan.originalDegree[row] < bestOriginal || plan.originalDegree[row] == bestOriginal && (chosen == noPlanIndex || row < chosen) {
			chosen = row
			bestOriginal = plan.originalDegree[row]
		}
	}
	if chosen == noPlanIndex {
		return 0, 0, false, fmt.Errorf("%w: empty degree bucket %d", ErrHybridPlanInvariant, r)
	}
	return chosen, r, true, nil
}

func (plan *InactivationPlan) selectDegreeTwo(start, end uint32) (uint32, error) {
	w := plan.parameters.W
	for column := uint32(0); column < w; column++ {
		plan.componentParent[column] = uint16(column)
		plan.componentSize[column] = 0
	}
	for logical := start; logical < end; logical++ {
		physical := plan.logicalToPhysicalCol[logical]
		plan.componentSize[physical] = 1
	}
	for row := plan.bucketHeads[2]; row != noPlanIndex; row = plan.bucketNext[row] {
		columns, err := plan.activePhysicalColumns(row, start, end, plan.columnScratch[:0])
		if err != nil || len(columns) != 2 {
			return 0, fmt.Errorf("%w: degree-two row %d", ErrHybridPlanInvariant, row)
		}
		plan.unionComponents(columns[0], columns[1])
	}
	chosen := noPlanIndex
	maximum := uint32(0)
	for row := plan.bucketHeads[2]; row != noPlanIndex; row = plan.bucketNext[row] {
		columns, err := plan.activePhysicalColumns(row, start, end, plan.columnScratch[:0])
		if err != nil || len(columns) != 2 {
			return 0, fmt.Errorf("%w: degree-two graph row %d", ErrHybridPlanInvariant, row)
		}
		root := plan.findComponent(columns[0])
		size := plan.componentSize[root]
		if size > maximum || size == maximum && (chosen == noPlanIndex || row < chosen) {
			chosen, maximum = row, size
		}
	}
	if chosen == noPlanIndex {
		return 0, fmt.Errorf("%w: no degree-two row", ErrHybridPlanInvariant)
	}
	return chosen, nil
}

func (plan *InactivationPlan) activePhysicalColumns(row, start, end uint32, output []uint16) ([]uint16, error) {
	if row >= plan.rows || start > end || end > plan.parameters.W {
		return nil, fmt.Errorf("%w: active columns row=%d [%d,%d)", ErrHybridPlanInvariant, row, start, end)
	}
	offset := plan.vOffsets[row]
	for index := uint32(0); index < uint32(plan.vLengths[row]); index++ {
		physical := plan.vEntries[offset+index]
		logical := uint32(plan.physicalToLogicalCol[physical])
		if logical >= start && logical < end {
			output = append(output, physical)
		}
	}
	return output, nil
}

func (plan *InactivationPlan) removeActiveColumn(column uint16) error {
	start, end := plan.columnOffsets[column], plan.columnOffsets[uint32(column)+1]
	for index := start; index < end; index++ {
		row := plan.columnRows[index]
		if plan.active[row] == 0 {
			continue
		}
		degree := plan.degree[row]
		if degree == 0 {
			return fmt.Errorf("%w: decrement zero degree row %d", ErrHybridPlanInvariant, row)
		}
		plan.bucketRemove(row, degree)
		plan.degree[row]--
		plan.consumed++
		if plan.degree[row] != 0 {
			plan.bucketInsert(row, plan.degree[row])
		}
	}
	return nil
}

func (plan *InactivationPlan) bucketInsert(row uint32, degree uint16) {
	head := plan.bucketHeads[degree]
	plan.bucketPrevious[row] = noPlanIndex
	plan.bucketNext[row] = head
	if head != noPlanIndex {
		plan.bucketPrevious[head] = row
	}
	plan.bucketHeads[degree] = row
}

func (plan *InactivationPlan) bucketRemove(row uint32, degree uint16) {
	previous, next := plan.bucketPrevious[row], plan.bucketNext[row]
	if previous == noPlanIndex {
		plan.bucketHeads[degree] = next
	} else {
		plan.bucketNext[previous] = next
	}
	if next != noPlanIndex {
		plan.bucketPrevious[next] = previous
	}
	plan.bucketPrevious[row], plan.bucketNext[row] = noPlanIndex, noPlanIndex
}

func (plan *InactivationPlan) swapLogicalRows(left, right uint32) {
	if left == right {
		return
	}
	leftPhysical := plan.logicalToPhysicalRow[left]
	rightPhysical := plan.logicalToPhysicalRow[right]
	plan.logicalToPhysicalRow[left], plan.logicalToPhysicalRow[right] = rightPhysical, leftPhysical
	plan.physicalToLogicalRow[leftPhysical], plan.physicalToLogicalRow[rightPhysical] = right, left
}

func (plan *InactivationPlan) swapPhysicalColumnToLogical(physical uint16, target uint32) {
	current := uint32(plan.physicalToLogicalCol[physical])
	if current == target {
		return
	}
	targetPhysical := plan.logicalToPhysicalCol[target]
	plan.logicalToPhysicalCol[target], plan.logicalToPhysicalCol[current] = physical, targetPhysical
	plan.physicalToLogicalCol[physical], plan.physicalToLogicalCol[targetPhysical] = uint16(target), uint16(current)
}

func (plan *InactivationPlan) findComponent(column uint16) uint16 {
	root := column
	for plan.componentParent[root] != root {
		root = plan.componentParent[root]
	}
	for plan.componentParent[column] != column {
		next := plan.componentParent[column]
		plan.componentParent[column] = root
		column = next
	}
	return root
}

func (plan *InactivationPlan) unionComponents(left, right uint16) {
	leftRoot, rightRoot := plan.findComponent(left), plan.findComponent(right)
	if leftRoot == rightRoot {
		return
	}
	if plan.componentSize[leftRoot] < plan.componentSize[rightRoot] {
		leftRoot, rightRoot = rightRoot, leftRoot
	}
	plan.componentParent[rightRoot] = leftRoot
	plan.componentSize[leftRoot] += plan.componentSize[rightRoot]
}
