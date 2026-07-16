------------------ MODULE SparseScheduleTraceSpec ------------------
EXTENDS FiniteSets, Integers, Naturals, Sequences

CONSTANTS Trace, Rows, Columns, MaxNonzeros
VARIABLES status, rowMap, columnMap, reserved, cursor, traceIndex

vars == <<status, rowMap, columnMap, reserved, cursor, traceIndex>>

MatrixBacking == (Rows + 1) * 8 + MaxNonzeros * 4 + Columns * 4
ExpectedBacking == MatrixBacking + Rows * 4 + Columns * 4

Identity(size) == [index \in 1..size |-> index]

Swap(mapping, left, right) ==
    [mapping EXCEPT
        ![left + 1] = mapping[right + 1],
        ![right + 1] = mapping[left + 1]]

Matches(record) ==
    /\ status = record.status
    /\ rowMap = record.rowMap
    /\ columnMap = record.columnMap
    /\ reserved = record.reserved
    /\ cursor = record.cursor

MatchesNext(record) ==
    /\ status' = record.status
    /\ rowMap' = record.rowMap
    /\ columnMap' = record.columnMap
    /\ reserved' = record.reserved
    /\ cursor' = record.cursor

ObservationValid(record) ==
    IF record.status = "ready"
    THEN /\ Len(record.rowMap) = Rows
         /\ Len(record.rowInverse) = Rows
         /\ Len(record.columnMap) = Columns
         /\ Len(record.columnInverse) = Columns
         /\ \A position \in 1..Rows :
                record.rowInverse[record.rowMap[position]] = position
         /\ \A position \in 1..Columns :
                record.columnInverse[record.columnMap[position]] = position
    ELSE /\ record.rowMap = <<>>
         /\ record.rowInverse = <<>>
         /\ record.columnMap = <<>>
         /\ record.columnInverse = <<>>

Init ==
    /\ traceIndex = 1
    /\ Trace[1].op = "init"
    /\ Trace[1].result = "ok"
    /\ status = "unallocated"
    /\ rowMap = <<>>
    /\ columnMap = <<>>
    /\ reserved = 0
    /\ cursor = 0
    /\ ObservationValid(Trace[1])
    /\ Matches(Trace[1])

Apply(record) ==
    \/ /\ record.op = "create_limit"
       /\ record.result = "limit"
       /\ status = "unallocated"
       /\ record.budget < ExpectedBacking
       /\ UNCHANGED <<status, rowMap, columnMap, reserved, cursor>>
    \/ /\ record.op = "create"
       /\ record.result = "ok"
       /\ status = "unallocated"
       /\ record.budget = ExpectedBacking
       /\ status' = "ready"
       /\ rowMap' = Identity(Rows)
       /\ columnMap' = Identity(Columns)
       /\ reserved' = ExpectedBacking
       /\ UNCHANGED cursor
    \/ /\ record.op = "set"
       /\ record.result = "ok"
       /\ status = "ready"
       /\ record.left \in 0..(Rows - 1)
       /\ record.right \in 0..(Columns - 1)
       /\ cursor' = cursor + 1
       /\ UNCHANGED <<status, rowMap, columnMap, reserved>>
    \/ /\ record.op = "add_row"
       /\ record.result = "ok"
       /\ status = "ready"
       /\ record.left \in 0..(Rows - 1)
       /\ record.right \in 0..(Rows - 1)
       /\ record.left # record.right
       /\ cursor' = cursor + 1
       /\ UNCHANGED <<status, rowMap, columnMap, reserved>>
    \/ /\ record.op = "swap_row"
       /\ record.result = "ok"
       /\ status = "ready"
       /\ record.left \in 0..(Rows - 1)
       /\ record.right \in 0..(Rows - 1)
       /\ record.left # record.right
       /\ rowMap' = Swap(rowMap, record.left, record.right)
       /\ cursor' = cursor + 1
       /\ UNCHANGED <<status, columnMap, reserved>>
    \/ /\ record.op = "swap_column"
       /\ record.result = "ok"
       /\ status = "ready"
       /\ record.left \in 0..(Columns - 1)
       /\ record.right \in 0..(Columns - 1)
       /\ record.left # record.right
       /\ columnMap' = Swap(columnMap, record.left, record.right)
       /\ cursor' = cursor + 1
       /\ UNCHANGED <<status, rowMap, reserved>>
    \/ /\ record.op = "swap_row_same"
       /\ record.result = "ok"
       /\ status = "ready"
       /\ record.left \in 0..(Rows - 1)
       /\ record.right = record.left
       /\ cursor' = cursor + 1
       /\ UNCHANGED <<status, rowMap, columnMap, reserved>>
    \/ /\ record.op = "swap_column_same"
       /\ record.result = "ok"
       /\ status = "ready"
       /\ record.left \in 0..(Columns - 1)
       /\ record.right = record.left
       /\ cursor' = cursor + 1
       /\ UNCHANGED <<status, rowMap, columnMap, reserved>>
    \/ /\ record.op \in {"reject_capacity", "reject_index"}
       /\ record.result \in {"capacity", "index"}
       /\ status = "ready"
       /\ UNCHANGED <<status, rowMap, columnMap, reserved, cursor>>
    \/ /\ record.op = "observe"
       /\ record.result = "ok"
       /\ UNCHANGED <<status, rowMap, columnMap, reserved, cursor>>

Next ==
    /\ traceIndex < Len(Trace)
    /\ traceIndex' = traceIndex + 1
    /\ LET record == Trace[traceIndex + 1]
       IN  /\ Apply(record)
           /\ ObservationValid(record)
           /\ MatchesNext(record)

TraceSpec == Init /\ [][Next]_vars /\ WF_vars(Next)

IsBijection(mapping, size) ==
    /\ Len(mapping) = size
    /\ {mapping[index] : index \in 1..Len(mapping)} = 1..size

TypeOK ==
    /\ status \in {"unallocated", "ready"}
    /\ reserved \in Nat
    /\ cursor \in Nat
    /\ traceIndex \in 1..Len(Trace)
    /\ IF status = "ready"
       THEN /\ IsBijection(rowMap, Rows)
            /\ IsBijection(columnMap, Columns)
       ELSE /\ rowMap = <<>>
            /\ columnMap = <<>>

ResourceAccounting ==
    /\ reserved <= ExpectedBacking
    /\ (status = "ready") => reserved = ExpectedBacking
    /\ (status = "unallocated") => reserved = 0

RequiredOps == {"init", "create_limit", "create", "set",
               "reject_capacity", "swap_row", "swap_column", "add_row",
               "swap_row_same", "swap_column_same", "reject_index", "observe"}

TraceCoverage ==
    traceIndex # 1 \/
        /\ {Trace[index].op : index \in 1..Len(Trace)} = RequiredOps
        /\ Trace[2].op = "create_limit"
        /\ Trace[2].budget = ExpectedBacking - 1
        /\ Trace[3].op = "create"
        /\ Trace[3].budget = ExpectedBacking

TraceComplete == <>(traceIndex = Len(Trace))

=====================================================================
