package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"slices"

	"github.com/fgn/raptorgo/internal/codec"
	"github.com/fgn/raptorgo/internal/gf256"
	"github.com/fgn/raptorgo/internal/matrix"
	"github.com/fgn/raptorgo/internal/rfc6330"
)

const (
	kPrime     = uint32(10)
	symbolSize = uint32(3)
)

type event struct {
	pivotRow     uint32
	pivotColumn  uint32
	degree       uint32
	frozen       []uint32
	eliminations []uint32
}

type record struct {
	op, result, stage                                   string
	planBudget, executionBudget                         uint64
	planReserved, executionReserved                     uint64
	event, planCursor, replayCursor, substitutionCursor uint32
	i, u, consumed, denseColumns                        uint32
	residualRows, residualColumns, substitutionRow      uint32
	rowMap, rowInverse, columnMap, columnInverse        []uint32
	substitutionColumns                                 []uint32
	binary, workingD                                    []uint32
	residualCoefficients, residualRHS, residualSolution []uint32
	inputD, resultBytes                                 []uint32
}

type corpus struct {
	parameters                        rfc6330.Parameters
	encodedRows                       uint32
	vCapacity, uCapacity              uint32
	vRows, uRows                      [][]uint32
	vNonzeros                         uint32
	events                            []event
	rhsMap                            []uint32
	residualCoefficients              []uint32
	residualRHS                       []uint32
	residualSolution                  []uint32
	inputD, result                    []uint32
	planWorkspace, executionWorkspace uint64
	trace                             []record
}

func main() {
	output := flag.String("output", "", "output TLA+ data module")
	flag.Parse()
	if *output == "" {
		flag.Usage()
		os.Exit(2)
	}
	value := buildCorpus()
	if err := os.WriteFile(*output, render(value), 0o644); err != nil {
		panic(err)
	}
}

func buildCorpus() corpus {
	parameters, err := rfc6330.ParametersForExtendedSourceBlock(kPrime)
	if err != nil {
		panic(err)
	}
	isis := make([]uint32, parameters.KPrime+2)
	for index := range isis {
		isis[index] = uint32(index)
	}
	encodedRows := uint32(len(isis))
	planWorkspace, err := codec.InactivationPlanBackingBytes(kPrime, encodedRows)
	if err != nil {
		panic(err)
	}
	if plan, planErr := codec.PlanReceivedInactivation(kPrime, isis, planWorkspace-1); !errors.Is(planErr, codec.ErrHybridPlanWorkspace) || plan != nil {
		panic(fmt.Sprintf("one-byte-short plan = %v, %v", plan, planErr))
	}
	plan, err := codec.PlanReceivedInactivation(kPrime, isis, planWorkspace)
	if err != nil {
		panic(err)
	}
	view := reflect.ValueOf(plan).Elem()
	vRows := rowsFromPlan(view, "vOffsets", "vLengths", "vEntries")
	uRows := rowsFromPlan(view, "uOffsets", "uLengths", "uEntries")
	events := eventsFromPlan(view)
	vNonzeros := uint32Field(view, "vNonzeros")
	vCapacity := uint32Field(view, "vCapacity")
	uCapacity := uint32Field(view, "uCapacity")
	if uint32(len(events)) != plan.Steps() || vNonzeros != plan.InitialVNonzeros() || plan.FinalI()+plan.FinalU() != parameters.L() {
		panic("public/private plan projection mismatch")
	}

	wantRHSMap := canonicalRHSMap(parameters, encodedRows)
	known, d := knownSystem(parameters, isis)
	executionWorkspace, err := codec.InactivationExecutionBackingBytes(plan, symbolSize)
	if err != nil {
		panic(err)
	}
	dBefore := symbolBytes(d)
	if result, executeErr := codec.ExecuteInactivationPlan(plan, d, executionWorkspace-1); !errors.Is(executeErr, codec.ErrHybridExecutionWorkspace) || result != nil {
		panic(fmt.Sprintf("one-byte-short execution = %v, %v", result, executeErr))
	}
	if !bytes.Equal(symbolBytes(d), dBefore) {
		panic("under-budget execution mutated D")
	}
	var observations []codec.HybridExecutionObservation
	result, err := codec.ExecuteInactivationPlanObserved(plan, d, executionWorkspace, func(observation codec.HybridExecutionObservation) error {
		observations = append(observations, observation)
		return nil
	})
	if err != nil {
		panic(err)
	}
	if !bytes.Equal(symbolBytes(result), symbolBytes(known)) || !bytes.Equal(symbolBytes(d), dBefore) {
		panic("exact execution did not preserve D and recover the known solution")
	}
	observed := validateExecutionObservations(parameters, plan.FinalI(), known, d, observations, wantRHSMap)

	trace := buildTrace(
		parameters, encodedRows, planWorkspace, executionWorkspace,
		vRows, uRows, vNonzeros, events, view, observations,
	)
	return corpus{
		parameters: parameters, encodedRows: encodedRows,
		vCapacity: vCapacity, uCapacity: uCapacity,
		vRows: vRows, uRows: uRows, vNonzeros: vNonzeros,
		events: events, rhsMap: observed.rhsMap,
		residualCoefficients: bytesAsUint32(observed.residual.ResidualCoefficients),
		residualRHS:          bytesAsUint32(observed.residual.ResidualRHS),
		residualSolution:     bytesAsUint32(observed.solved.ResidualSolution),
		inputD:               bytesAsUint32(observed.complete.InputD), result: bytesAsUint32(observed.complete.Result),
		planWorkspace: planWorkspace, executionWorkspace: executionWorkspace,
		trace: trace,
	}
}

func rowsFromPlan(plan reflect.Value, offsetsName, lengthsName, entriesName string) [][]uint32 {
	offsets := uintSlice(plan.FieldByName(offsetsName))
	lengths := uintSlice(plan.FieldByName(lengthsName))
	entries := uintSlice(plan.FieldByName(entriesName))
	rows := make([][]uint32, len(lengths))
	for row := range lengths {
		start := offsets[row]
		end := start + lengths[row]
		rows[row] = append([]uint32(nil), entries[start:end]...)
		for index := range rows[row] {
			rows[row][index]++
		}
	}
	return rows
}

func eventsFromPlan(plan reflect.Value) []event {
	steps := uint32Field(plan, "steps")
	pivotRows := uintSlice(plan.FieldByName("pivotRows"))
	pivotColumns := uintSlice(plan.FieldByName("pivotColumns"))
	degrees := uintSlice(plan.FieldByName("stepDegrees"))
	frozenOffsets := uintSlice(plan.FieldByName("frozenOffsets"))
	frozenColumns := uintSlice(plan.FieldByName("frozenColumns"))
	eliminationOffsets := uintSlice(plan.FieldByName("eliminationOffsets"))
	eliminationRows := uintSlice(plan.FieldByName("eliminationRows"))
	result := make([]event, steps)
	for step := uint32(0); step < steps; step++ {
		result[step] = event{
			pivotRow:     pivotRows[step] + 1,
			pivotColumn:  pivotColumns[step] + 1,
			degree:       degrees[step],
			frozen:       oneBased(frozenColumns[frozenOffsets[step]:frozenOffsets[step+1]]),
			eliminations: oneBased(eliminationRows[eliminationOffsets[step]:eliminationOffsets[step+1]]),
		}
	}
	return result
}

func uint32Field(value reflect.Value, name string) uint32 {
	return uint32(value.FieldByName(name).Uint())
}

func uintSlice(value reflect.Value) []uint32 {
	result := make([]uint32, value.Len())
	for index := range result {
		result[index] = uint32(value.Index(index).Uint())
	}
	return result
}

func oneBased(values []uint32) []uint32 {
	result := append([]uint32(nil), values...)
	for index := range result {
		result[index]++
	}
	return result
}

func canonicalRHSMap(parameters rfc6330.Parameters, encodedRows uint32) []uint32 {
	rows := parameters.S + encodedRows
	result := make([]uint32, rows+parameters.H)
	for row := uint32(0); row < parameters.S; row++ {
		result[row] = row + 1
	}
	for row := uint32(0); row < encodedRows; row++ {
		result[parameters.S+row] = parameters.S + parameters.H + row + 1
	}
	for row := uint32(0); row < parameters.H; row++ {
		result[rows+row] = parameters.S + row + 1
	}
	return result
}

func knownSystem(parameters rfc6330.Parameters, isis []uint32) (*gf256.Symbols, *gf256.Symbols) {
	knownBytes := uint64(parameters.L()) * uint64(symbolSize)
	known, err := gf256.NewSymbols(parameters.L(), symbolSize, knownBytes)
	if err != nil {
		panic(err)
	}
	for row := uint32(0); row < parameters.L(); row++ {
		value := []byte{byte(row*17 + 3), byte(row*29 + 11), byte(255 - row)}
		if err := known.SetSymbol(row, value); err != nil {
			panic(err)
		}
	}
	matrixBytes, err := codec.ConstraintMatrixBackingBytes(kPrime, uint64(len(isis)))
	if err != nil {
		panic(err)
	}
	coefficients, err := codec.BuildConstraintMatrix(kPrime, isis, matrixBytes)
	if err != nil {
		panic(err)
	}
	dBytes := uint64(coefficients.Rows()) * uint64(symbolSize)
	d, err := gf256.NewSymbols(coefficients.Rows(), symbolSize, dBytes)
	if err != nil {
		panic(err)
	}
	for row := uint32(0); row < coefficients.Rows(); row++ {
		value := make([]byte, symbolSize)
		for column := uint32(0); column < coefficients.Columns(); column++ {
			coefficient, atErr := coefficients.At(row, column)
			if atErr != nil {
				panic(atErr)
			}
			if coefficient == 0 {
				continue
			}
			symbol, symbolErr := known.Symbol(column)
			if symbolErr != nil {
				panic(symbolErr)
			}
			if err := gf256.MulAdd(value, coefficient, symbol); err != nil {
				panic(err)
			}
		}
		if err := d.SetSymbol(row, value); err != nil {
			panic(err)
		}
	}
	return known, d
}

func symbolBytes(symbols *gf256.Symbols) []byte {
	var result []byte
	for row := uint32(0); row < symbols.Count(); row++ {
		value, err := symbols.Symbol(row)
		if err != nil {
			panic(err)
		}
		result = append(result, value...)
	}
	return result
}

type executionSummary struct {
	rhsMap                          []uint32
	rhs, residual, solved, complete codec.HybridExecutionObservation
}

func validateExecutionObservations(
	parameters rfc6330.Parameters,
	finalI uint32,
	known, d *gf256.Symbols,
	observations []codec.HybridExecutionObservation,
	wantRHSMap []uint32,
) executionSummary {
	if len(observations) != int(4+2*finalI) {
		panic(fmt.Sprintf("executor emitted %d observations, want %d", len(observations), 4+2*finalI))
	}
	summary := executionSummary{rhs: observations[0]}
	if summary.rhs.Stage != "rhs" {
		panic("executor did not begin with the RHS observation")
	}
	summary.rhsMap = oneBased(summary.rhs.RHSMap)
	if !slices.Equal(summary.rhsMap, wantRHSMap) {
		panic(fmt.Sprintf("executor RHS map %v want %v", summary.rhsMap, wantRHSMap))
	}
	if len(summary.rhs.WorkingD) != int(d.Bytes()) {
		panic("executor RHS clone has wrong size")
	}
	for destination, source := range summary.rhs.RHSMap {
		want, err := d.Symbol(source)
		if err != nil {
			panic(err)
		}
		start := destination * int(d.Size())
		if !bytes.Equal(summary.rhs.WorkingD[start:start+int(d.Size())], want) {
			panic(fmt.Sprintf("executor RHS row %d was not copied from canonical row %d", destination, source))
		}
	}
	rows := d.Count() - parameters.H
	validateObservedHDPC(parameters, rows, known, summary.rhs)
	validateObservedBinaryEquations(parameters, rows, known, summary.rhs)

	for step := uint32(0); step < finalI; step++ {
		observation := observations[1+step]
		if observation.Stage != "replay" || observation.Step != step+1 {
			panic(fmt.Sprintf("executor replay observation %d = %q/%d", step, observation.Stage, observation.Step))
		}
		validateObservedHDPC(parameters, rows, known, observation)
		validateObservedBinaryEquations(parameters, rows, known, observation)
	}
	summary.residual = observations[1+finalI]
	summary.solved = observations[2+finalI]
	if summary.residual.Stage != "residual" || summary.solved.Stage != "solved" ||
		summary.residual.ResidualRows != rows+parameters.H-finalI ||
		summary.residual.ResidualColumns != parameters.L()-finalI {
		panic("executor residual/solution observation shape")
	}
	validateObservedResidual(parameters, finalI, known, observations[finalI], summary.residual, summary.solved)

	for logicalRow := uint32(0); logicalRow < finalI; logicalRow++ {
		observation := observations[3+finalI+logicalRow]
		if observation.Stage != "substitute" || observation.SubstitutionRow != logicalRow {
			panic(fmt.Sprintf("executor substitution %d = %q/%d", logicalRow, observation.Stage, observation.SubstitutionRow))
		}
		want := append([]byte(nil), observation.SubstitutionBefore...)
		for _, logicalColumn := range observation.SubstitutionColumns {
			if logicalColumn < finalI || logicalColumn >= parameters.L() {
				panic("executor substitution dependency outside residual")
			}
			offset := (logicalColumn - finalI) * symbolSize
			for index := uint32(0); index < symbolSize; index++ {
				want[index] ^= summary.solved.ResidualSolution[offset+index]
			}
		}
		if !bytes.Equal(want, observation.SubstitutionAfter) {
			panic(fmt.Sprintf("executor substitution row %d is not the observed XOR", logicalRow))
		}
	}
	summary.complete = observations[len(observations)-1]
	if summary.complete.Stage != "complete" ||
		!bytes.Equal(summary.complete.InputD, symbolBytes(d)) ||
		!bytes.Equal(summary.complete.Result, symbolBytes(known)) {
		panic("executor completion observation differs from immutable input/known solution")
	}
	return summary
}

func validateObservedBinaryEquations(
	parameters rfc6330.Parameters,
	rows uint32,
	known *gf256.Symbols,
	observation codec.HybridExecutionObservation,
) {
	if len(observation.Binary) != int(uint64(rows)*uint64(parameters.L())) ||
		len(observation.RowMap) != int(rows) || len(observation.ColumnMap) != int(parameters.L()) ||
		len(observation.WorkingD) != int(uint64(rows+parameters.H)*uint64(symbolSize)) {
		panic(fmt.Sprintf("%s binary observation shape", observation.Stage))
	}
	for logicalRow := uint32(0); logicalRow < rows; logicalRow++ {
		firstColumn := observation.Step
		if logicalRow < observation.Step {
			firstColumn = logicalRow
		}
		want := make([]byte, symbolSize)
		for logicalColumn := firstColumn; logicalColumn < parameters.L(); logicalColumn++ {
			coefficient := observation.Binary[logicalRow*parameters.L()+logicalColumn]
			if coefficient == 0 {
				continue
			}
			if coefficient != 1 {
				panic(fmt.Sprintf("%s non-binary coefficient row=%d column=%d", observation.Stage, logicalRow, logicalColumn))
			}
			originalColumn := observation.ColumnMap[logicalColumn]
			symbol, err := known.Symbol(originalColumn)
			if err != nil {
				panic(err)
			}
			for offset := range want {
				want[offset] ^= symbol[offset]
			}
		}
		physicalRow := observation.RowMap[logicalRow]
		start := physicalRow * symbolSize
		if !bytes.Equal(want, observation.WorkingD[start:start+symbolSize]) {
			panic(fmt.Sprintf("%s binary row %d is not coupled to WorkingD", observation.Stage, logicalRow))
		}
	}
}

func validateObservedHDPC(
	parameters rfc6330.Parameters,
	rows uint32,
	known *gf256.Symbols,
	observation codec.HybridExecutionObservation,
) {
	if len(observation.HDPC) != int(uint64(parameters.H)*uint64(parameters.L())) ||
		len(observation.WorkingD) != int(uint64(rows+parameters.H)*uint64(symbolSize)) {
		panic(fmt.Sprintf("%s HDPC observation shape", observation.Stage))
	}
	for hdpcRow := uint32(0); hdpcRow < parameters.H; hdpcRow++ {
		want := make([]byte, symbolSize)
		for column := uint32(0); column < parameters.L(); column++ {
			coefficient := observation.HDPC[hdpcRow*parameters.L()+column]
			if coefficient == 0 {
				continue
			}
			symbol, err := known.Symbol(column)
			if err != nil {
				panic(err)
			}
			if err := gf256.MulAdd(want, coefficient, symbol); err != nil {
				panic(err)
			}
		}
		start := (rows + hdpcRow) * symbolSize
		if !bytes.Equal(want, observation.WorkingD[start:start+symbolSize]) {
			panic(fmt.Sprintf("%s HDPC row %d does not preserve its equation", observation.Stage, hdpcRow))
		}
	}
}

func validateObservedResidual(
	parameters rfc6330.Parameters,
	finalI uint32,
	known *gf256.Symbols,
	lastReplay, residual, solved codec.HybridExecutionObservation,
) {
	rows, columns := residual.ResidualRows, residual.ResidualColumns
	if len(lastReplay.ColumnMap) != int(parameters.L()) ||
		len(residual.ResidualCoefficients) != int(uint64(rows)*uint64(columns)) ||
		len(residual.ResidualRHS) != int(uint64(rows)*uint64(symbolSize)) ||
		len(solved.ResidualSolution) != int(uint64(columns)*uint64(symbolSize)) {
		panic("executor residual byte shape")
	}
	for row := uint32(0); row < rows; row++ {
		want := make([]byte, symbolSize)
		for column := uint32(0); column < columns; column++ {
			coefficient := residual.ResidualCoefficients[row*columns+column]
			if coefficient == 0 {
				continue
			}
			originalColumn := lastReplay.ColumnMap[finalI+column]
			symbol, err := known.Symbol(originalColumn)
			if err != nil {
				panic(err)
			}
			if err := gf256.MulAdd(want, coefficient, symbol); err != nil {
				panic(err)
			}
		}
		start := row * symbolSize
		if !bytes.Equal(want, residual.ResidualRHS[start:start+symbolSize]) {
			panic(fmt.Sprintf("executor residual row %d does not hold for the known solution", row))
		}
	}
	for column := uint32(0); column < columns; column++ {
		originalColumn := lastReplay.ColumnMap[finalI+column]
		want, err := known.Symbol(originalColumn)
		if err != nil {
			panic(err)
		}
		start := column * symbolSize
		if !bytes.Equal(want, solved.ResidualSolution[start:start+symbolSize]) {
			panic(fmt.Sprintf("executor dense solution column %d differs", column))
		}
	}
}

func bytesAsUint32(values []byte) []uint32 {
	result := make([]uint32, len(values))
	for index, value := range values {
		result[index] = uint32(value)
	}
	return result
}

func buildTrace(
	parameters rfc6330.Parameters,
	encodedRows uint32,
	planWorkspace, executionWorkspace uint64,
	vRows, uRows [][]uint32,
	vNonzeros uint32,
	events []event,
	planView reflect.Value,
	observations []codec.HybridExecutionObservation,
) []record {
	rows := parameters.S + encodedRows
	identityRows, identityColumns := identity(rows), identity(parameters.L())
	base := record{
		result: "ok", stage: "init",
		rowMap: identityRows, rowInverse: identityRows,
		columnMap: identityColumns, columnInverse: identityColumns,
	}
	trace := []record{cloneRecord(base)}
	trace[0].op = "init"
	limit := cloneRecord(base)
	limit.op, limit.result, limit.planBudget = "plan_limit", "limit", planWorkspace-1
	trace = append(trace, limit)

	planStart := cloneRecord(base)
	planStart.op, planStart.stage = "plan_start", "plan"
	planStart.planBudget, planStart.planReserved, planStart.u = planWorkspace, planWorkspace, parameters.P()
	trace = append(trace, planStart)

	initialSnapshot, snapshots, consumed := replayPlan(parameters, vRows, uRows, vNonzeros, events)
	for index, snapshot := range snapshots {
		record := cloneRecord(planStart)
		record.op = "plan_step"
		record.event = uint32(index + 1)
		record.planCursor = uint32(index + 1)
		record.i = uint32(index + 1)
		record.u = snapshot.u
		record.consumed = consumed[index]
		record.rowMap, record.rowInverse = snapshot.rowMap, snapshot.rowInverse
		record.columnMap, record.columnInverse = snapshot.columnMap, snapshot.columnInverse
		trace = append(trace, record)
	}
	last := snapshots[len(snapshots)-1]
	planDone := cloneRecord(trace[len(trace)-1])
	planDone.op, planDone.stage = "plan_done", "planned"
	trace = append(trace, planDone)
	if !slices.Equal(last.rowMap, oneBased(uintSlice(planView.FieldByName("logicalToPhysicalRow")))) ||
		!slices.Equal(last.columnMap, oneBased(uintSlice(planView.FieldByName("logicalToPhysicalCol")))) {
		panic("replayed event permutations do not match the immutable plan")
	}

	executionLimit := cloneRecord(planDone)
	executionLimit.op, executionLimit.result = "execution_limit", "limit"
	executionLimit.executionBudget = executionWorkspace - 1
	trace = append(trace, executionLimit)

	executionStart := cloneRecord(planDone)
	executionStart.op, executionStart.stage = "execution_start", "replay"
	executionStart.executionBudget, executionStart.executionReserved = executionWorkspace, executionWorkspace
	executionStart.replayCursor, executionStart.i, executionStart.u, executionStart.consumed = 0, 0, parameters.P(), vNonzeros
	rhsObservation := observations[0]
	rhsRowMap, rhsRowInverse := oneBased(rhsObservation.RowMap), oneBased(rhsObservation.RowInverse)
	rhsColumnMap, rhsColumnInverse := oneBased(rhsObservation.ColumnMap), oneBased(rhsObservation.ColumnInverse)
	if !slices.Equal(rhsRowMap, initialSnapshot.rowMap) || !slices.Equal(rhsRowInverse, initialSnapshot.rowInverse) ||
		!slices.Equal(rhsColumnMap, initialSnapshot.columnMap) || !slices.Equal(rhsColumnInverse, initialSnapshot.columnInverse) ||
		rhsObservation.DenseColumns != initialSnapshot.u || !bytes.Equal(rhsObservation.Binary, initialSnapshot.binary) {
		panic("production initial binary observation differs from independent construction")
	}
	executionStart.denseColumns = rhsObservation.DenseColumns
	executionStart.rowMap, executionStart.rowInverse = rhsRowMap, rhsRowInverse
	executionStart.columnMap, executionStart.columnInverse = rhsColumnMap, rhsColumnInverse
	executionStart.binary = bytesAsUint32(rhsObservation.Binary)
	executionStart.workingD = bytesAsUint32(rhsObservation.WorkingD)
	trace = append(trace, executionStart)

	for index, snapshot := range snapshots {
		observation := observations[index+1]
		rowMap, rowInverse := oneBased(observation.RowMap), oneBased(observation.RowInverse)
		columnMap, columnInverse := oneBased(observation.ColumnMap), oneBased(observation.ColumnInverse)
		if !slices.Equal(rowMap, snapshot.rowMap) || !slices.Equal(rowInverse, snapshot.rowInverse) ||
			!slices.Equal(columnMap, snapshot.columnMap) || !slices.Equal(columnInverse, snapshot.columnInverse) ||
			observation.DenseColumns != snapshot.u || !bytes.Equal(observation.Binary, snapshot.binary) {
			panic(fmt.Sprintf("production replay observation %d differs from independent replay", index+1))
		}
		record := cloneRecord(executionStart)
		record.op = "replay_step"
		record.event = uint32(index + 1)
		record.replayCursor = uint32(index + 1)
		record.i = uint32(index + 1)
		record.u, record.denseColumns = observation.DenseColumns, observation.DenseColumns
		record.rowMap, record.rowInverse = rowMap, rowInverse
		record.columnMap, record.columnInverse = columnMap, columnInverse
		record.binary = bytesAsUint32(observation.Binary)
		record.workingD = bytesAsUint32(observation.WorkingD)
		trace = append(trace, record)
	}

	residualObservation := observations[1+len(events)]
	residual := cloneRecord(trace[len(trace)-1])
	residual.op, residual.stage = "dense_residual", "dense"
	residual.residualRows = residualObservation.ResidualRows
	residual.residualColumns = residualObservation.ResidualColumns
	residual.binary, residual.workingD = nil, nil
	residual.residualCoefficients = bytesAsUint32(residualObservation.ResidualCoefficients)
	residual.residualRHS = bytesAsUint32(residualObservation.ResidualRHS)
	trace = append(trace, residual)
	solvedObservation := observations[2+len(events)]
	denseSolved := cloneRecord(residual)
	denseSolved.op, denseSolved.stage = "dense_solved", "substitute"
	denseSolved.residualCoefficients, denseSolved.residualRHS = nil, nil
	denseSolved.residualSolution = bytesAsUint32(solvedObservation.ResidualSolution)
	trace = append(trace, denseSolved)

	for logicalRow := uint32(0); logicalRow < uint32(len(events)); logicalRow++ {
		observation := observations[3+len(events)+int(logicalRow)]
		record := cloneRecord(denseSolved)
		record.op = "substitute"
		record.residualSolution = nil
		record.substitutionCursor = logicalRow + 1
		record.substitutionRow = observation.SubstitutionRow + 1
		record.substitutionColumns = oneBased(observation.SubstitutionColumns)
		trace = append(trace, record)
		denseSolved = record
	}
	completeObservation := observations[len(observations)-1]
	complete := cloneRecord(trace[len(trace)-1])
	complete.op, complete.stage = "complete", "complete"
	complete.residualSolution = nil
	complete.inputD = bytesAsUint32(completeObservation.InputD)
	complete.resultBytes = bytesAsUint32(completeObservation.Result)
	trace = append(trace, complete)
	return trace
}

type replaySnapshot struct {
	u                                            uint32
	rowMap, rowInverse, columnMap, columnInverse []uint32
	binary                                       []byte
}

func replayPlan(
	parameters rfc6330.Parameters,
	vRows, uRows [][]uint32,
	vNonzeros uint32,
	events []event,
) (replaySnapshot, []replaySnapshot, []uint32) {
	counts := make([]uint32, len(vRows))
	for row := range vRows {
		counts[row] = uint32(len(vRows[row]))
	}
	hybridBytes, err := matrix.HybridBinaryBackingBytes(uint32(len(vRows)), parameters.L(), parameters.P(), parameters.P()+frozenTotal(events), uint64(vNonzeros))
	if err != nil {
		panic(err)
	}
	replay, err := matrix.NewHybridBinary(counts, parameters.L(), parameters.P(), parameters.P()+frozenTotal(events), hybridBytes)
	if err != nil {
		panic(err)
	}
	for row := range vRows {
		columns := make([]uint16, len(vRows[row]))
		for index, column := range vRows[row] {
			columns[index] = uint16(column - 1)
		}
		if err := replay.LoadSparseRow(uint32(row), columns); err != nil {
			panic(err)
		}
		for _, column := range uRows[row] {
			if err := replay.SetInitialDense(uint32(row), column-1, true); err != nil {
				panic(err)
			}
		}
	}
	if err := replay.Finalize(); err != nil {
		panic(err)
	}
	initial := observeReplay(replay)

	activeRows := make([]bool, len(vRows))
	for index := range activeRows {
		activeRows[index] = true
	}
	activeColumns := make([]bool, parameters.W)
	for index := range activeColumns {
		activeColumns[index] = true
	}
	snapshots := make([]replaySnapshot, 0, len(events))
	consumed := make([]uint32, 0, len(events))
	for step, event := range events {
		pivotRow, pivotColumn := event.pivotRow-1, event.pivotColumn-1
		logicalRow, err := replay.LogicalRow(pivotRow)
		if err != nil {
			panic(err)
		}
		if err := replay.SwapRows(uint32(step), logicalRow); err != nil {
			panic(err)
		}
		logicalColumn, err := replay.LogicalColumn(pivotColumn)
		if err != nil {
			panic(err)
		}
		if err := replay.SwapSparseColumns(uint32(step), logicalColumn); err != nil {
			panic(err)
		}
		freezeStart := parameters.L() - replay.DenseColumns() - uint32(len(event.frozen))
		for index, physical := range event.frozen {
			logical, logicalErr := replay.LogicalColumn(physical - 1)
			if logicalErr != nil {
				panic(logicalErr)
			}
			if err := replay.SwapSparseColumns(freezeStart+uint32(index), logical); err != nil {
				panic(err)
			}
		}
		for index := len(event.frozen); index > 0; index-- {
			got, freezeErr := replay.FreezeNext()
			if freezeErr != nil || got+1 != event.frozen[index-1] {
				panic("frozen replay mismatch")
			}
		}
		for _, destination := range event.eliminations {
			logical, logicalErr := replay.LogicalRow(destination - 1)
			if logicalErr != nil {
				panic(logicalErr)
			}
			if err := replay.XORDenseRows(logical, uint32(step)); err != nil {
				panic(err)
			}
		}

		activeRows[pivotRow] = false
		for _, column := range vRows[pivotRow] {
			if activeColumns[column-1] {
				activeColumns[column-1] = false
			}
		}
		remaining := uint32(0)
		for row, active := range activeRows {
			if !active {
				continue
			}
			for _, column := range vRows[row] {
				if activeColumns[column-1] {
					remaining++
				}
			}
		}
		consumed = append(consumed, vNonzeros-remaining)
		snapshots = append(snapshots, observeReplay(replay))
	}
	return initial, snapshots, consumed
}

func observeReplay(value *matrix.HybridBinary) replaySnapshot {
	rowMap, rowInverse, columnMap, columnInverse := observeMaps(value)
	binary := make([]byte, uint64(value.Rows())*uint64(value.Columns()))
	for row := uint32(0); row < value.Rows(); row++ {
		for column := uint32(0); column < value.Columns(); column++ {
			set, err := value.Has(row, column)
			if err != nil {
				panic(err)
			}
			if set {
				binary[uint64(row)*uint64(value.Columns())+uint64(column)] = 1
			}
		}
	}
	return replaySnapshot{
		u: value.DenseColumns(), rowMap: rowMap, rowInverse: rowInverse,
		columnMap: columnMap, columnInverse: columnInverse, binary: binary,
	}
}

func frozenTotal(events []event) uint32 {
	var total uint32
	for _, event := range events {
		total += uint32(len(event.frozen))
	}
	return total
}

func observeMaps(value *matrix.HybridBinary) ([]uint32, []uint32, []uint32, []uint32) {
	rowMap, rowInverse := make([]uint32, value.Rows()), make([]uint32, value.Rows())
	for index := uint32(0); index < value.Rows(); index++ {
		physical, err := value.PhysicalRow(index)
		if err != nil {
			panic(err)
		}
		logical, err := value.LogicalRow(index)
		if err != nil {
			panic(err)
		}
		rowMap[index], rowInverse[index] = physical+1, logical+1
	}
	columnMap, columnInverse := make([]uint32, value.Columns()), make([]uint32, value.Columns())
	for index := uint32(0); index < value.Columns(); index++ {
		physical, err := value.PhysicalColumn(index)
		if err != nil {
			panic(err)
		}
		logical, err := value.LogicalColumn(index)
		if err != nil {
			panic(err)
		}
		columnMap[index], columnInverse[index] = physical+1, logical+1
	}
	return rowMap, rowInverse, columnMap, columnInverse
}

func identity(size uint32) []uint32 {
	result := make([]uint32, size)
	for index := range result {
		result[index] = uint32(index) + 1
	}
	return result
}

func cloneRecord(value record) record {
	value.rowMap = append([]uint32(nil), value.rowMap...)
	value.rowInverse = append([]uint32(nil), value.rowInverse...)
	value.columnMap = append([]uint32(nil), value.columnMap...)
	value.columnInverse = append([]uint32(nil), value.columnInverse...)
	value.substitutionColumns = append([]uint32(nil), value.substitutionColumns...)
	value.binary = append([]uint32(nil), value.binary...)
	value.workingD = append([]uint32(nil), value.workingD...)
	value.residualCoefficients = append([]uint32(nil), value.residualCoefficients...)
	value.residualRHS = append([]uint32(nil), value.residualRHS...)
	value.residualSolution = append([]uint32(nil), value.residualSolution...)
	value.inputD = append([]uint32(nil), value.inputD...)
	value.resultBytes = append([]uint32(nil), value.resultBytes...)
	return value
}

func cloneTrace(values []record) []record {
	result := make([]record, len(values))
	for index := range values {
		result[index] = cloneRecord(values[index])
	}
	return result
}

func cloneEvents(values []event) []event {
	result := make([]event, len(values))
	for index, value := range values {
		result[index] = value
		result[index].frozen = append([]uint32(nil), value.frozen...)
		result[index].eliminations = append([]uint32(nil), value.eliminations...)
	}
	return result
}

func render(value corpus) []byte {
	missing := cloneTrace(value.trace)
	for index, record := range missing {
		if record.op == "replay_step" && record.event == 8 {
			missing = append(missing[:index], missing[index+1:]...)
			break
		}
	}
	forgedPermutation := cloneTrace(value.trace)
	for index := range forgedPermutation {
		if forgedPermutation[index].op == "replay_step" {
			forgedPermutation[index].rowMap[0], forgedPermutation[index].rowMap[1] = forgedPermutation[index].rowMap[1], forgedPermutation[index].rowMap[0]
			forgedPermutation[index].rowInverse = inverse(forgedPermutation[index].rowMap)
			break
		}
	}
	forgedBinary := cloneTrace(value.trace)
	for index := range forgedBinary {
		if forgedBinary[index].op == "replay_step" && len(forgedBinary[index].binary) != 0 {
			forgedBinary[index].binary[0] ^= 1
			break
		}
	}
	premature := cloneTrace(value.trace)
	lastReplay, dense := -1, -1
	for index, record := range premature {
		if record.op == "replay_step" {
			lastReplay = index
		}
		if record.op == "dense_residual" {
			dense = index
		}
	}
	premature[lastReplay], premature[dense] = premature[dense], premature[lastReplay]
	wrongSubstitution := cloneTrace(value.trace)
	for index := range wrongSubstitution {
		if wrongSubstitution[index].op == "substitute" {
			wrongSubstitution[index].substitutionRow++
			break
		}
	}
	forgedSubstitution := cloneTrace(value.trace)
	for index := range forgedSubstitution {
		if forgedSubstitution[index].op != "substitute" {
			continue
		}
		if len(forgedSubstitution[index].substitutionColumns) == 0 {
			forgedSubstitution[index].substitutionColumns = []uint32{uint32(len(value.events)) + 1}
		} else {
			forgedSubstitution[index].substitutionColumns = forgedSubstitution[index].substitutionColumns[1:]
		}
		break
	}
	forgedPlanBudget := cloneTrace(value.trace)
	for index := range forgedPlanBudget {
		if forgedPlanBudget[index].op == "plan_limit" {
			forgedPlanBudget[index].result = "ok"
			break
		}
	}
	forgedExecutionBudget := cloneTrace(value.trace)
	for index := range forgedExecutionBudget {
		if forgedExecutionBudget[index].op == "execution_limit" {
			forgedExecutionBudget[index].result = "ok"
			break
		}
	}
	forgedFrozen := cloneEvents(value.events)
	for index := range forgedFrozen {
		if len(forgedFrozen[index].frozen) != 0 {
			forgedFrozen[index].frozen[0] = forgedFrozen[index].pivotColumn
			break
		}
	}
	forgedElimination := cloneEvents(value.events)
	for index := range forgedElimination {
		if len(forgedElimination[index].eliminations) != 0 {
			forgedElimination[index].eliminations[0] = forgedElimination[index].pivotRow
			break
		}
	}
	forgedRHS := append([]uint32(nil), value.rhsMap...)
	forgedRHS[0], forgedRHS[1] = forgedRHS[1], forgedRHS[0]
	forgedResidual := append([]uint32(nil), value.residualCoefficients...)
	forgedResidual[0] ^= 1
	forgedSolution := append([]uint32(nil), value.residualSolution...)
	forgedSolution[0] ^= 1
	forgedResult := append([]uint32(nil), value.result...)
	forgedResult[0] ^= 1

	var output bytes.Buffer
	output.WriteString("---------------- MODULE HybridTraceData ----------------\n")
	output.WriteString("EXTENDS Integers, Sequences\n\n")
	fmt.Fprintf(&output, "ImplementationS == %d\nImplementationH == %d\nImplementationW == %d\nImplementationP == %d\nImplementationL == %d\n", value.parameters.S, value.parameters.H, value.parameters.W, value.parameters.P(), value.parameters.L())
	fmt.Fprintf(&output, "ImplementationEncodedRows == %d\nImplementationVCapacity == %d\nImplementationUCapacity == %d\nImplementationVNonzeros == %d\nImplementationSymbolSize == %d\n", value.encodedRows, value.vCapacity, value.uCapacity, value.vNonzeros, symbolSize)
	fmt.Fprintf(&output, "ImplementationPlanWorkspace == %d\nImplementationExecutionWorkspace == %d\n\n", value.planWorkspace, value.executionWorkspace)
	renderRows(&output, "ImplementationVRows", value.vRows)
	renderRows(&output, "ImplementationURows", value.uRows)
	fmt.Fprintf(&output, "ImplementationRHSMap == %s\nForgedRHSMap == %s\n\n", sequence(value.rhsMap), sequence(forgedRHS))
	fmt.Fprintf(&output, "ImplementationResidualCoefficients == %s\n", sequence(value.residualCoefficients))
	fmt.Fprintf(&output, "ForgedResidualCoefficients == %s\n", sequence(forgedResidual))
	fmt.Fprintf(&output, "ImplementationResidualRHS == %s\n", sequence(value.residualRHS))
	fmt.Fprintf(&output, "ImplementationResidualSolution == %s\n", sequence(value.residualSolution))
	fmt.Fprintf(&output, "ImplementationInputD == %s\n", sequence(value.inputD))
	fmt.Fprintf(&output, "ImplementationResult == %s\n", sequence(value.result))
	output.WriteString("ImplementationExecutionCorpus == [coefficients |-> ImplementationResidualCoefficients, rhs |-> ImplementationResidualRHS, solution |-> ImplementationResidualSolution, inputD |-> ImplementationInputD, result |-> ImplementationResult]\n")
	output.WriteString("ForgedExecutionCorpus == [coefficients |-> ForgedResidualCoefficients, rhs |-> ImplementationResidualRHS, solution |-> ImplementationResidualSolution, inputD |-> ImplementationInputD, result |-> ImplementationResult]\n\n")
	fmt.Fprintf(&output, "ForgedResidualSolution == %s\n", sequence(forgedSolution))
	fmt.Fprintf(&output, "ForgedResult == %s\n", sequence(forgedResult))
	output.WriteString("ForgedSolutionExecutionCorpus == [coefficients |-> ImplementationResidualCoefficients, rhs |-> ImplementationResidualRHS, solution |-> ForgedResidualSolution, inputD |-> ImplementationInputD, result |-> ImplementationResult]\n")
	output.WriteString("ForgedResultExecutionCorpus == [coefficients |-> ImplementationResidualCoefficients, rhs |-> ImplementationResidualRHS, solution |-> ImplementationResidualSolution, inputD |-> ImplementationInputD, result |-> ForgedResult]\n\n")
	renderEvents(&output, "ImplementationPlanEvents", value.events)
	renderEvents(&output, "ForgedFrozenPlanEvents", forgedFrozen)
	renderEvents(&output, "ForgedEliminationPlanEvents", forgedElimination)
	renderTrace(&output, "ImplementationTrace", value.trace)
	renderTrace(&output, "MissingEventTrace", missing)
	renderTrace(&output, "ForgedPermutationTrace", forgedPermutation)
	renderTrace(&output, "ForgedBinaryTrace", forgedBinary)
	renderTrace(&output, "PrematureDenseTrace", premature)
	renderTrace(&output, "WrongSubstitutionTrace", wrongSubstitution)
	renderTrace(&output, "ForgedSubstitutionTrace", forgedSubstitution)
	renderTrace(&output, "ForgedPlanBudgetTrace", forgedPlanBudget)
	renderTrace(&output, "ForgedExecutionBudgetTrace", forgedExecutionBudget)
	output.WriteString("=====================================================================\n")
	return output.Bytes()
}

func inverse(mapping []uint32) []uint32 {
	result := make([]uint32, len(mapping))
	for logical, physical := range mapping {
		result[physical-1] = uint32(logical) + 1
	}
	return result
}

func renderRows(output *bytes.Buffer, name string, rows [][]uint32) {
	fmt.Fprintf(output, "%s == <<\n", name)
	for index, row := range rows {
		comma := ","
		if index == len(rows)-1 {
			comma = ""
		}
		fmt.Fprintf(output, "    %s%s\n", set(row), comma)
	}
	output.WriteString(">>\n\n")
}

func renderEvents(output *bytes.Buffer, name string, events []event) {
	fmt.Fprintf(output, "%s == <<\n", name)
	for index, event := range events {
		comma := ","
		if index == len(events)-1 {
			comma = ""
		}
		fmt.Fprintf(output, "    [pivotRow |-> %d, pivotColumn |-> %d, degree |-> %d,\n", event.pivotRow, event.pivotColumn, event.degree)
		fmt.Fprintf(output, "     frozen |-> %s, eliminations |-> %s]%s\n", sequence(event.frozen), sequence(event.eliminations), comma)
	}
	output.WriteString(">>\n\n")
}

func renderTrace(output *bytes.Buffer, name string, trace []record) {
	fmt.Fprintf(output, "%s == <<\n", name)
	for index, record := range trace {
		comma := ","
		if index == len(trace)-1 {
			comma = ""
		}
		fmt.Fprintf(output, "    [op |-> %q, result |-> %q, stage |-> %q, planBudget |-> %d, executionBudget |-> %d,\n", record.op, record.result, record.stage, record.planBudget, record.executionBudget)
		fmt.Fprintf(output, "     planReserved |-> %d, executionReserved |-> %d, event |-> %d, planCursor |-> %d, replayCursor |-> %d, substitutionCursor |-> %d,\n", record.planReserved, record.executionReserved, record.event, record.planCursor, record.replayCursor, record.substitutionCursor)
		fmt.Fprintf(output, "     i |-> %d, u |-> %d, consumed |-> %d, denseColumns |-> %d, residualRows |-> %d, residualColumns |-> %d, substitutionRow |-> %d,\n", record.i, record.u, record.consumed, record.denseColumns, record.residualRows, record.residualColumns, record.substitutionRow)
		fmt.Fprintf(output, "     rowMap |-> %s, rowInverse |-> %s, columnMap |-> %s, columnInverse |-> %s,\n", sequence(record.rowMap), sequence(record.rowInverse), sequence(record.columnMap), sequence(record.columnInverse))
		fmt.Fprintf(output, "     binary |-> %s, workingD |-> %s,\n", sequence(record.binary), sequence(record.workingD))
		fmt.Fprintf(output, "     substitutionColumns |-> %s, residualCoefficients |-> %s, residualRHS |-> %s, residualSolution |-> %s,\n", sequence(record.substitutionColumns), sequence(record.residualCoefficients), sequence(record.residualRHS), sequence(record.residualSolution))
		fmt.Fprintf(output, "     inputD |-> %s, resultBytes |-> %s]%s\n", sequence(record.inputD), sequence(record.resultBytes), comma)
	}
	output.WriteString(">>\n\n")
}

func sequence(values []uint32) string {
	var output bytes.Buffer
	output.WriteString("<<")
	for index, value := range values {
		if index != 0 {
			output.WriteString(", ")
		}
		fmt.Fprint(&output, value)
	}
	output.WriteString(">>")
	return output.String()
}

func set(values []uint32) string {
	if len(values) == 0 {
		return "{}"
	}
	var output bytes.Buffer
	output.WriteString("{")
	for index, value := range values {
		if index != 0 {
			output.WriteString(", ")
		}
		fmt.Fprint(&output, value)
	}
	output.WriteString("}")
	return output.String()
}
