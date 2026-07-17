----------------------- MODULE HybridTraceSpec -----------------------
EXTENDS FiniteSets, Integers, Naturals, Sequences

CONSTANTS Trace, PlanEvents, VRows, URows, RHSMap, ExecutionCorpus,
          S, H, W, P, L, EncodedRows,
          VCapacity, UCapacity, VNonzeros, SymbolSize,
          PlanWorkspace, ExecutionWorkspace

VARIABLES stage, planCursor, replayCursor, substitutionCursor,
          i, u, consumed, denseColumns, residualRows, residualColumns,
          activeRows, activeColumns, rowMap, columnMap, denseRows,
          planReserved, executionReserved, traceIndex

vars == <<stage, planCursor, replayCursor, substitutionCursor,
          i, u, consumed, denseColumns, residualRows, residualColumns,
          activeRows, activeColumns, rowMap, columnMap, denseRows,
          planReserved, executionReserved, traceIndex>>

Rows == Len(VRows)
TotalRows == Rows + H
B == W - S
FinalI == Len(PlanEvents)

RECURSIVE FrozenCount(_)
FrozenCount(cursor) ==
    IF cursor = 0
    THEN 0
    ELSE FrozenCount(cursor - 1) + Len(PlanEvents[cursor].frozen)

FinalU == P + FrozenCount(FinalI)

SeqSet(sequence) == {sequence[index] : index \in DOMAIN sequence}
UniqueSequence(sequence) == Len(sequence) = Cardinality(SeqSet(sequence))
Identity(size) == [index \in 1..size |-> index]

OctetSequence(sequence) == \A index \in DOMAIN sequence: sequence[index] \in 0..255

RECURSIVE RowIncidences(_)
RowIncidences(cursor) ==
    IF cursor = 0
    THEN 0
    ELSE RowIncidences(cursor - 1) + Cardinality(VRows[cursor])

RECURSIVE UIncidences(_)
UIncidences(cursor) ==
    IF cursor = 0
    THEN 0
    ELSE UIncidences(cursor - 1) + Cardinality(URows[cursor])

RECURSIVE ActiveIncidences(_, _)
ActiveIncidences(row, columns) ==
    IF row > Rows
    THEN 0
    ELSE IF row \in activeRows
         THEN Cardinality(VRows[row] \intersect columns)
              + ActiveIncidences(row + 1, columns)
         ELSE ActiveIncidences(row + 1, columns)

Permutation(mapping, size) ==
    /\ Len(mapping) = size
    /\ UniqueSequence(mapping)
    /\ SeqSet(mapping) = 1..size

InverseMapping(mapping) ==
    [physical \in 1..Len(mapping) |->
        CHOOSE logical \in 1..Len(mapping): mapping[logical] = physical]

MovePhysical(mapping, targetLogical, physical) ==
    LET sourceLogical ==
            CHOOSE logical \in 1..Len(mapping): mapping[logical] = physical
    IN [logical \in 1..Len(mapping) |->
            IF logical = targetLogical
            THEN physical
            ELSE IF logical = sourceLogical
                 THEN mapping[targetLogical]
                 ELSE mapping[logical]]

RECURSIVE MovePhysicalSequence(_, _, _, _)
MovePhysicalSequence(mapping, physicals, targetStart, offset) ==
    IF offset > Len(physicals)
    THEN mapping
    ELSE MovePhysicalSequence(
            MovePhysical(mapping, targetStart + offset - 1, physicals[offset]),
            physicals,
            targetStart,
            offset + 1)

NextRowMap(event) == MovePhysical(rowMap, i + 1, event.pivotRow)

NextColumnMap(event) ==
    LET withPivot == MovePhysical(columnMap, i + 1, event.pivotColumn)
        freezeStart == L - u - Len(event.frozen) + 1
    IN MovePhysicalSequence(withPivot, event.frozen, freezeStart, 1)

SymmetricDifference(left, right) ==
    (left \union right) \ (left \intersect right)

DenseAfterFreeze(event) ==
    [row \in 1..Rows |->
        denseRows[row] \union
        (VRows[row] \intersect SeqSet(event.frozen))]

DenseAfterElimination(event) ==
    LET frozen == DenseAfterFreeze(event)
        destinations == SeqSet(event.eliminations)
    IN [row \in 1..Rows |->
            IF row \in destinations
            THEN SymmetricDifference(frozen[row], frozen[event.pivotRow])
            ELSE frozen[row]]

BinaryCoefficient(rowMapping, columnMapping, rows, denseCount,
                  logicalRow, logicalColumn) ==
    LET physicalRow == rowMapping[logicalRow]
        physicalColumn == columnMapping[logicalColumn]
    IN IF logicalColumn > L - denseCount
       THEN IF physicalColumn \in rows[physicalRow] THEN 1 ELSE 0
       ELSE IF physicalColumn \in VRows[physicalRow] THEN 1 ELSE 0

BinarySnapshot(rowMapping, columnMapping, rows, denseCount) ==
    [index \in 1..(Rows * L) |->
        LET logicalRow == ((index - 1) \div L) + 1
            logicalColumn == ((index - 1) % L) + 1
        IN BinaryCoefficient(rowMapping, columnMapping, rows, denseCount,
                             logicalRow, logicalColumn)]

RECURSIVE DenseLogicalColumns(_, _)
DenseLogicalColumns(physicalRow, logicalColumn) ==
    IF logicalColumn > L
    THEN <<>>
    ELSE IF columnMap[logicalColumn] \in denseRows[physicalRow]
         THEN <<logicalColumn>> \o DenseLogicalColumns(physicalRow, logicalColumn + 1)
         ELSE DenseLogicalColumns(physicalRow, logicalColumn + 1)

ExpectedSubstitutionColumns(logicalRow) ==
    DenseLogicalColumns(rowMap[logicalRow], FinalI + 1)

ExpectedRHSMap ==
    [position \in 1..TotalRows |->
        IF position <= S
        THEN position
        ELSE IF position <= Rows
             THEN position + H
             ELSE S + position - Rows]

ExpectedPlanWorkspace ==
      (Rows + 1) * 4 + VCapacity * 2 + Rows * 2
    + (Rows + 1) * 4 + UCapacity * 2 + Rows * 2
    + (W + 1) * 4 + VCapacity * 4
    + Rows * 8 + L * 4
    + Rows * 4 + Rows + Rows * 8
    + (W + 1) * 4 + W * 2 + W * 4 + W * 2
    + W * 8 + (W + 1) * 8 + W * 2 + VCapacity * 4

DenseWords == (FinalU + 63) \div 64
HybridBacking ==
      (Rows + 1) * 4 + VNonzeros * 2
    + (L - P + 1) * 4 + VNonzeros * 4
    + Rows * 8 + L * 4 + Rows * DenseWords * 8
ExpectedResidualRows == TotalRows - FinalI
ExpectedResidualColumns == L - FinalI
DenseSolverBacking ==
      ExpectedResidualRows * ExpectedResidualColumns
    + ExpectedResidualRows * SymbolSize
    + ExpectedResidualColumns * SymbolSize
ExpectedExecutionWorkspace ==
      HybridBacking
    + H * L
    + TotalRows * SymbolSize
    + ExpectedResidualRows * ExpectedResidualColumns
    + ExpectedResidualRows * SymbolSize
    + DenseSolverBacking
    + L * SymbolSize

ObservationValid(record) ==
    /\ Permutation(record.rowMap, Rows)
    /\ record.rowInverse = InverseMapping(record.rowMap)
    /\ Permutation(record.columnMap, L)
    /\ record.columnInverse = InverseMapping(record.columnMap)

Matches(record) ==
    /\ stage = record.stage
    /\ planCursor = record.planCursor
    /\ replayCursor = record.replayCursor
    /\ substitutionCursor = record.substitutionCursor
    /\ i = record.i
    /\ u = record.u
    /\ consumed = record.consumed
    /\ denseColumns = record.denseColumns
    /\ residualRows = record.residualRows
    /\ residualColumns = record.residualColumns
    /\ rowMap = record.rowMap
    /\ columnMap = record.columnMap
    /\ planReserved = record.planReserved
    /\ executionReserved = record.executionReserved
    /\ ObservationValid(record)

MatchesNext(record) ==
    /\ stage' = record.stage
    /\ planCursor' = record.planCursor
    /\ replayCursor' = record.replayCursor
    /\ substitutionCursor' = record.substitutionCursor
    /\ i' = record.i
    /\ u' = record.u
    /\ consumed' = record.consumed
    /\ denseColumns' = record.denseColumns
    /\ residualRows' = record.residualRows
    /\ residualColumns' = record.residualColumns
    /\ rowMap' = record.rowMap
    /\ columnMap' = record.columnMap
    /\ planReserved' = record.planReserved
    /\ executionReserved' = record.executionReserved
    /\ ObservationValid(record)

Increasing(sequence) ==
    \A index \in 1..(Len(sequence) - 1):
        sequence[index] < sequence[index + 1]

Decreasing(sequence) ==
    \A index \in 1..(Len(sequence) - 1):
        sequence[index] > sequence[index + 1]

ActiveDegree(row) ==
    Cardinality(VRows[row] \intersect activeColumns)

DegreeTwoExpansion(columns) ==
    columns \union
    UNION {VRows[row] \intersect activeColumns:
        row \in {candidate \in activeRows:
            ActiveDegree(candidate) = 2
            /\ (VRows[candidate] \intersect activeColumns)
                 \intersect columns # {}}}

RECURSIVE DegreeTwoClosure(_, _)
DegreeTwoClosure(columns, rounds) ==
    IF rounds = 0
    THEN columns
    ELSE DegreeTwoClosure(DegreeTwoExpansion(columns), rounds - 1)

DegreeTwoComponentSize(row) ==
    Cardinality(DegreeTwoClosure(
        VRows[row] \intersect activeColumns, W))

PlanSelectionValid(event) ==
    /\ \A row \in activeRows:
          ActiveDegree(row) = 0 \/ event.degree <= ActiveDegree(row)
    /\ IF event.degree = 2
       THEN \A row \in activeRows:
                ActiveDegree(row) = 2 =>
                    /\ DegreeTwoComponentSize(event.pivotRow)
                         >= DegreeTwoComponentSize(row)
                    /\ (DegreeTwoComponentSize(event.pivotRow)
                          = DegreeTwoComponentSize(row)) =>
                         event.pivotRow <= row
       ELSE \A row \in activeRows:
                ActiveDegree(row) = event.degree =>
                    /\ Cardinality(VRows[event.pivotRow])
                         <= Cardinality(VRows[row])
                    /\ (Cardinality(VRows[event.pivotRow])
                          = Cardinality(VRows[row])) =>
                         event.pivotRow <= row

Init ==
    /\ Len(Trace) > 0
    /\ Trace[1].op = "init"
    /\ Trace[1].result = "ok"
    /\ traceIndex = 1
    /\ stage = "init"
    /\ planCursor = 0
    /\ replayCursor = 0
    /\ substitutionCursor = 0
    /\ i = 0
    /\ u = 0
    /\ consumed = 0
    /\ denseColumns = 0
    /\ residualRows = 0
    /\ residualColumns = 0
    /\ activeRows = 1..Rows
    /\ activeColumns = 1..W
    /\ rowMap = Identity(Rows)
    /\ columnMap = Identity(L)
    /\ denseRows = [row \in 1..Rows |-> URows[row]]
    /\ planReserved = 0
    /\ executionReserved = 0
    /\ Matches(Trace[1])

PlanEventValid(event) ==
    LET selected == VRows[event.pivotRow] \intersect activeColumns
        eliminations ==
            {row \in activeRows \ {event.pivotRow}:
                event.pivotColumn \in VRows[row]}
    IN /\ event.pivotRow \in activeRows
       /\ event.pivotColumn \in selected
       /\ event.degree = Cardinality(selected)
       /\ PlanSelectionValid(event)
       /\ \A column \in selected: event.pivotColumn <= column
       /\ UniqueSequence(event.frozen)
       /\ Increasing(event.frozen)
       /\ SeqSet(event.frozen) = selected \ {event.pivotColumn}
       /\ UniqueSequence(event.eliminations)
       /\ Decreasing(event.eliminations)
       /\ SeqSet(event.eliminations) = eliminations

RECURSIVE ActiveIncidencesAfter(_, _, _)
ActiveIncidencesAfter(rows, columns, row) ==
    IF row > Rows
    THEN 0
    ELSE IF row \in rows
         THEN Cardinality(VRows[row] \intersect columns)
              + ActiveIncidencesAfter(rows, columns, row + 1)
         ELSE ActiveIncidencesAfter(rows, columns, row + 1)

PlanStep(record) ==
    /\ stage = "plan"
    /\ planCursor < FinalI
    /\ record.op = "plan_step"
    /\ record.result = "ok"
    /\ record.event = planCursor + 1
    /\ LET event == PlanEvents[planCursor + 1]
           selected == VRows[event.pivotRow] \intersect activeColumns
           nextRows == activeRows \ {event.pivotRow}
           nextColumns == activeColumns \ selected
       IN /\ PlanEventValid(event)
          /\ record.planCursor = planCursor + 1
          /\ record.replayCursor = 0
          /\ record.substitutionCursor = 0
          /\ record.i = i + 1
          /\ record.u = u + Cardinality(selected) - 1
          /\ record.consumed =
                VNonzeros -
                ActiveIncidencesAfter(nextRows, nextColumns, 1)
          /\ record.denseColumns = 0
          /\ record.residualRows = 0
          /\ record.residualColumns = 0
          /\ record.rowMap = NextRowMap(event)
          /\ record.columnMap = NextColumnMap(event)
          /\ stage' = "plan"
          /\ planCursor' = planCursor + 1
          /\ replayCursor' = 0
          /\ substitutionCursor' = 0
          /\ i' = i + 1
          /\ u' = u + Cardinality(selected) - 1
          /\ consumed' = record.consumed
          /\ denseColumns' = 0
          /\ residualRows' = 0
          /\ residualColumns' = 0
          /\ activeRows' = nextRows
          /\ activeColumns' = nextColumns
          /\ rowMap' = NextRowMap(event)
          /\ columnMap' = NextColumnMap(event)
          /\ UNCHANGED <<denseRows, planReserved, executionReserved>>

ReplayStep(record) ==
    /\ stage = "replay"
    /\ replayCursor < FinalI
    /\ record.op = "replay_step"
    /\ record.result = "ok"
    /\ record.event = replayCursor + 1
    /\ LET event == PlanEvents[replayCursor + 1]
           nextRowMap == NextRowMap(event)
           nextColumnMap == NextColumnMap(event)
           nextDenseRows == DenseAfterElimination(event)
           nextDenseColumns == denseColumns + Len(event.frozen)
       IN /\ record.planCursor = FinalI
          /\ record.replayCursor = replayCursor + 1
          /\ record.substitutionCursor = 0
          /\ record.i = i + 1
          /\ record.u = u + Len(event.frozen)
          /\ record.consumed = VNonzeros
          /\ record.denseColumns = nextDenseColumns
          /\ record.residualRows = 0
          /\ record.residualColumns = 0
          /\ record.rowMap = nextRowMap
          /\ record.columnMap = nextColumnMap
          /\ record.binary = BinarySnapshot(
                nextRowMap, nextColumnMap, nextDenseRows, nextDenseColumns)
          /\ Len(record.workingD) = TotalRows * SymbolSize
          /\ OctetSequence(record.workingD)
          /\ stage' = "replay"
          /\ planCursor' = FinalI
          /\ replayCursor' = replayCursor + 1
          /\ substitutionCursor' = 0
          /\ i' = i + 1
          /\ u' = u + Len(event.frozen)
          /\ consumed' = VNonzeros
          /\ denseColumns' = nextDenseColumns
          /\ residualRows' = 0
          /\ residualColumns' = 0
          /\ rowMap' = nextRowMap
          /\ columnMap' = nextColumnMap
          /\ denseRows' = nextDenseRows
          /\ UNCHANGED <<activeRows, activeColumns,
                         planReserved, executionReserved>>

Apply(record) ==
    \/ /\ record.op = "plan_limit"
       /\ record.result = "limit"
       /\ stage = "init"
       /\ record.planBudget = ExpectedPlanWorkspace - 1
       /\ UNCHANGED <<stage, planCursor, replayCursor, substitutionCursor,
                      i, u, consumed, denseColumns,
                      residualRows, residualColumns,
                      activeRows, activeColumns, rowMap, columnMap, denseRows,
                      planReserved, executionReserved>>
    \/ /\ record.op = "plan_start"
       /\ record.result = "ok"
       /\ stage = "init"
       /\ record.planBudget = ExpectedPlanWorkspace
       /\ record.planReserved = ExpectedPlanWorkspace
       /\ record.planCursor = 0
       /\ record.replayCursor = 0
       /\ record.substitutionCursor = 0
       /\ record.i = 0
       /\ record.u = P
       /\ record.consumed = 0
       /\ record.denseColumns = 0
       /\ record.residualRows = 0
       /\ record.residualColumns = 0
       /\ record.rowMap = Identity(Rows)
       /\ record.columnMap = Identity(L)
       /\ stage' = "plan"
       /\ u' = P
       /\ planReserved' = ExpectedPlanWorkspace
       /\ UNCHANGED <<planCursor, replayCursor, substitutionCursor,
                      i, consumed, denseColumns,
                      residualRows, residualColumns,
                      activeRows, activeColumns, rowMap, columnMap, denseRows,
                      executionReserved>>
    \/ PlanStep(record)
    \/ /\ record.op = "plan_done"
       /\ record.result = "ok"
       /\ stage = "plan"
       /\ planCursor = FinalI
       /\ i + u = L
       /\ record.stage = "planned"
       /\ stage' = "planned"
       /\ UNCHANGED <<planCursor, replayCursor, substitutionCursor,
                      i, u, consumed, denseColumns,
                      residualRows, residualColumns,
                      activeRows, activeColumns, rowMap, columnMap, denseRows,
                      planReserved, executionReserved>>
    \/ /\ record.op = "execution_limit"
       /\ record.result = "limit"
       /\ stage = "planned"
       /\ record.executionBudget = ExpectedExecutionWorkspace - 1
       /\ UNCHANGED <<stage, planCursor, replayCursor, substitutionCursor,
                      i, u, consumed, denseColumns,
                      residualRows, residualColumns,
                      activeRows, activeColumns, rowMap, columnMap, denseRows,
                      planReserved, executionReserved>>
    \/ /\ record.op = "execution_start"
       /\ record.result = "ok"
       /\ stage = "planned"
       /\ record.executionBudget = ExpectedExecutionWorkspace
       /\ record.executionReserved = ExpectedExecutionWorkspace
       /\ record.stage = "replay"
       /\ record.planCursor = FinalI
       /\ record.replayCursor = 0
       /\ record.substitutionCursor = 0
       /\ record.i = 0
       /\ record.u = P
       /\ record.consumed = VNonzeros
       /\ record.denseColumns = P
       /\ record.residualRows = 0
       /\ record.residualColumns = 0
       /\ record.rowMap = Identity(Rows)
       /\ record.columnMap = Identity(L)
       /\ record.binary = BinarySnapshot(
            Identity(Rows), Identity(L),
            [row \in 1..Rows |-> URows[row]], P)
       /\ Len(record.workingD) = TotalRows * SymbolSize
       /\ OctetSequence(record.workingD)
       /\ stage' = "replay"
       /\ replayCursor' = 0
       /\ substitutionCursor' = 0
       /\ i' = 0
       /\ u' = P
       /\ denseColumns' = P
       /\ residualRows' = 0
       /\ residualColumns' = 0
       /\ rowMap' = Identity(Rows)
       /\ columnMap' = Identity(L)
       /\ denseRows' = [row \in 1..Rows |-> URows[row]]
       /\ executionReserved' = ExpectedExecutionWorkspace
       /\ UNCHANGED <<planCursor, consumed, activeRows, activeColumns,
                      planReserved>>
    \/ ReplayStep(record)
    \/ /\ record.op = "dense_residual"
       /\ record.result = "ok"
       /\ stage = "replay"
       /\ replayCursor = FinalI
       /\ record.stage = "dense"
       /\ record.residualRows = ExpectedResidualRows
       /\ record.residualColumns = ExpectedResidualColumns
       /\ Len(ExecutionCorpus.coefficients) = ExpectedResidualRows * ExpectedResidualColumns
       /\ Len(ExecutionCorpus.rhs) = ExpectedResidualRows * SymbolSize
       /\ OctetSequence(ExecutionCorpus.coefficients)
       /\ OctetSequence(ExecutionCorpus.rhs)
       /\ record.residualCoefficients = ExecutionCorpus.coefficients
       /\ record.residualRHS = ExecutionCorpus.rhs
       /\ stage' = "dense"
       /\ residualRows' = ExpectedResidualRows
       /\ residualColumns' = ExpectedResidualColumns
       /\ UNCHANGED <<planCursor, replayCursor, substitutionCursor,
                      i, u, consumed, denseColumns,
                      activeRows, activeColumns, rowMap, columnMap, denseRows,
                      planReserved, executionReserved>>
    \/ /\ record.op = "dense_solved"
       /\ record.result = "ok"
       /\ stage = "dense"
       /\ record.stage = "substitute"
       /\ Len(ExecutionCorpus.solution) = ExpectedResidualColumns * SymbolSize
       /\ OctetSequence(ExecutionCorpus.solution)
       /\ record.residualSolution = ExecutionCorpus.solution
       /\ stage' = "substitute"
       /\ UNCHANGED <<planCursor, replayCursor, substitutionCursor,
                      i, u, consumed, denseColumns,
                      residualRows, residualColumns,
                      activeRows, activeColumns, rowMap, columnMap, denseRows,
                      planReserved, executionReserved>>
    \/ /\ record.op = "substitute"
       /\ record.result = "ok"
       /\ stage = "substitute"
       /\ substitutionCursor < FinalI
       /\ record.substitutionCursor = substitutionCursor + 1
       /\ record.substitutionRow = substitutionCursor + 1
       /\ record.substitutionColumns =
            ExpectedSubstitutionColumns(substitutionCursor + 1)
       /\ substitutionCursor' = substitutionCursor + 1
       /\ UNCHANGED <<stage, planCursor, replayCursor,
                      i, u, consumed, denseColumns,
                      residualRows, residualColumns,
                      activeRows, activeColumns, rowMap, columnMap, denseRows,
                      planReserved, executionReserved>>
    \/ /\ record.op = "complete"
       /\ record.result = "ok"
       /\ stage = "substitute"
       /\ substitutionCursor = FinalI
       /\ record.stage = "complete"
       /\ Len(ExecutionCorpus.inputD) = TotalRows * SymbolSize
       /\ Len(ExecutionCorpus.result) = L * SymbolSize
       /\ OctetSequence(ExecutionCorpus.inputD)
       /\ OctetSequence(ExecutionCorpus.result)
       /\ record.inputD = ExecutionCorpus.inputD
       /\ record.resultBytes = ExecutionCorpus.result
       /\ stage' = "complete"
       /\ UNCHANGED <<planCursor, replayCursor, substitutionCursor,
                      i, u, consumed, denseColumns,
                      residualRows, residualColumns,
                      activeRows, activeColumns, rowMap, columnMap, denseRows,
                      planReserved, executionReserved>>

Next ==
    /\ traceIndex < Len(Trace)
    /\ traceIndex' = traceIndex + 1
    /\ LET record == Trace[traceIndex + 1]
       IN /\ Apply(record)
          /\ MatchesNext(record)

TraceSpec == Init /\ [][Next]_vars /\ WF_vars(Next)

TypeOK ==
    /\ stage \in {"init", "plan", "planned", "replay",
                  "dense", "substitute", "complete"}
    /\ planCursor \in 0..FinalI
    /\ replayCursor \in 0..FinalI
    /\ substitutionCursor \in 0..FinalI
    /\ i \in 0..FinalI
    /\ u \in 0..FinalU
    /\ consumed \in 0..VNonzeros
    /\ denseColumns \in 0..FinalU
    /\ residualRows \in 0..TotalRows
    /\ residualColumns \in 0..L
    /\ activeRows \subseteq 1..Rows
    /\ activeColumns \subseteq 1..W
    /\ Permutation(rowMap, Rows)
    /\ Permutation(columnMap, L)
    /\ \A row \in 1..Rows: denseRows[row] \subseteq 1..L
    /\ planReserved \in Nat
    /\ executionReserved \in Nat
    /\ traceIndex \in 1..Len(Trace)

IncidenceConservation ==
    /\ VNonzeros = RowIncidences(Rows)
    /\ UIncidences(Rows) <= UCapacity
    /\ consumed + ActiveIncidences(1, activeColumns) = VNonzeros

NoPlannerStall ==
    (stage = "plan" /\ i + u < L) =>
        \E row \in activeRows:
            Cardinality(VRows[row] \intersect activeColumns) > 0

CursorInvariant ==
    /\ (stage = "init") =>
          /\ planCursor = 0
          /\ replayCursor = 0
          /\ substitutionCursor = 0
    /\ (stage = "plan") =>
          /\ planCursor = i
          /\ replayCursor = 0
          /\ substitutionCursor = 0
          /\ u = P + FrozenCount(planCursor)
    /\ (stage = "planned") =>
          /\ planCursor = FinalI
          /\ replayCursor = 0
          /\ substitutionCursor = 0
          /\ i = FinalI
          /\ u = FinalU
    /\ (stage \in {"replay", "dense", "substitute", "complete"}) =>
          /\ planCursor = FinalI
          /\ replayCursor = i
          /\ u = P + FrozenCount(replayCursor)
          /\ denseColumns = u
    /\ (stage \in {"dense", "substitute", "complete"}) =>
          /\ replayCursor = FinalI
          /\ residualRows = ExpectedResidualRows
          /\ residualColumns = ExpectedResidualColumns
    /\ (stage \in {"init", "plan", "planned", "replay", "dense"}) =>
          substitutionCursor = 0
    /\ (stage = "complete") => substitutionCursor = FinalI

ResourceAccounting ==
    /\ VCapacity = 3 * B + S + EncodedRows * 30
    /\ UCapacity = 2 * S + EncodedRows * 3
    /\ PlanWorkspace = ExpectedPlanWorkspace
    /\ ExecutionWorkspace = ExpectedExecutionWorkspace
    /\ planReserved <= ExpectedPlanWorkspace
    /\ executionReserved <= ExpectedExecutionWorkspace
    /\ (stage = "init") => planReserved = 0
    /\ (stage \in {"plan", "planned", "replay",
                   "dense", "substitute", "complete"}) =>
          planReserved = ExpectedPlanWorkspace
    /\ (stage \in {"init", "plan", "planned"}) => executionReserved = 0
    /\ (stage \in {"replay", "dense", "substitute", "complete"}) =>
          executionReserved = ExpectedExecutionWorkspace

RHSMapping == RHSMap = ExpectedRHSMap

PlanBudgetValid(record) ==
    CASE record.op = "init" ->
            /\ record.planBudget = 0
            /\ record.planReserved = 0
      [] record.op = "plan_limit" ->
            /\ record.result = "limit"
            /\ record.planBudget = ExpectedPlanWorkspace - 1
            /\ record.planReserved = 0
      [] OTHER ->
            /\ record.planBudget = ExpectedPlanWorkspace
            /\ record.planReserved = ExpectedPlanWorkspace

ExecutionBudgetValid(record) ==
    CASE record.op \in {"init", "plan_limit", "plan_start",
                         "plan_step", "plan_done"} ->
            /\ record.executionBudget = 0
            /\ record.executionReserved = 0
      [] record.op = "execution_limit" ->
            /\ record.result = "limit"
            /\ record.executionBudget = ExpectedExecutionWorkspace - 1
            /\ record.executionReserved = 0
      [] OTHER ->
            /\ record.executionBudget = ExpectedExecutionWorkspace
            /\ record.executionReserved = ExpectedExecutionWorkspace

PlanTraceBudgetFields ==
    \A index \in DOMAIN Trace: PlanBudgetValid(Trace[index])

ExecutionTraceBudgetFields ==
    \A index \in DOMAIN Trace: ExecutionBudgetValid(Trace[index])

TraceBudgetFields ==
    PlanTraceBudgetFields /\ ExecutionTraceBudgetFields

TraceObservationFields ==
    \A index \in DOMAIN Trace:
        IF Trace[index].op \in {"execution_start", "replay_step"}
        THEN /\ Len(Trace[index].binary) = Rows * L
             /\ \A offset \in DOMAIN Trace[index].binary:
                    Trace[index].binary[offset] \in 0..1
             /\ Len(Trace[index].workingD) = TotalRows * SymbolSize
             /\ OctetSequence(Trace[index].workingD)
        ELSE /\ Trace[index].binary = <<>>
             /\ Trace[index].workingD = <<>>

TraceCoverage ==
    traceIndex # 1 \/
        /\ Trace[1].op = "init"
        /\ Trace[2].op = "plan_limit"
        /\ Trace[Len(Trace)].op = "complete"
        /\ Cardinality({index \in DOMAIN Trace:
                Trace[index].op = "plan_step"}) = FinalI
        /\ Cardinality({index \in DOMAIN Trace:
                Trace[index].op = "replay_step"}) = FinalI
        /\ Cardinality({index \in DOMAIN Trace:
                Trace[index].op = "substitute"}) = FinalI

TraceComplete == <>(stage = "complete" /\ traceIndex = Len(Trace))

=====================================================================
