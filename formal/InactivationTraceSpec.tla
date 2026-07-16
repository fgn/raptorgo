-------------------- MODULE InactivationTraceSpec --------------------
EXTENDS FiniteSets, Integers, Naturals, Sequences

CONSTANTS Trace, Rows, Columns, InitialU, MaxNonzeros, SymbolSize, Workspace
VARIABLES status, phase, step, i, u, r, discarded, reserved, traceIndex

vars == <<status, phase, step, i, u, r, discarded, reserved, traceIndex>>

MatrixBacking == (Rows + 1) * 8 + MaxNonzeros * 5 + Columns * 5
ExpectedWorkspace == MatrixBacking + Rows * SymbolSize + Columns * SymbolSize
                     + Rows * 5 + Columns * 16

Matches(record) ==
    /\ status = record.status
    /\ phase = record.phase
    /\ step = record.step
    /\ i = record.i
    /\ u = record.u
    /\ r = record.r
    /\ discarded = record.discarded
    /\ reserved = record.reserved

MatchesNext(record) ==
    /\ status' = record.status
    /\ phase' = record.phase
    /\ step' = record.step
    /\ i' = record.i
    /\ u' = record.u
    /\ r' = record.r
    /\ discarded' = record.discarded
    /\ reserved' = record.reserved

RecordShape(record) == /\ record.l = Columns /\ record.m = Rows

Init ==
    /\ traceIndex = 1
    /\ Trace[1].op = "init"
    /\ Trace[1].result = "ok"
    /\ status = "unallocated"
    /\ phase = 0
    /\ step = 0
    /\ i = 0
    /\ u = 0
    /\ r = 0
    /\ discarded = 0
    /\ reserved = 0
    /\ RecordShape(Trace[1])
    /\ Matches(Trace[1])

Apply(record) ==
    \/ /\ record.op = "solve_limit"
       /\ record.result = "limit"
       /\ status = "unallocated"
       /\ record.budget = ExpectedWorkspace - 1
       /\ UNCHANGED <<status, phase, step, i, u, r, discarded, reserved>>
    \/ /\ record.op = "phase1"
       /\ record.result = "ok"
       /\ IF status = "unallocated"
          THEN /\ record.step = 0
               /\ record.i = 0
               /\ record.u = InitialU
               /\ record.r = 0
               /\ record.discarded = 0
               /\ record.budget = ExpectedWorkspace
               /\ record.reserved = ExpectedWorkspace
          ELSE /\ status = "running"
               /\ phase = 1
               /\ record.step = step + 1
               /\ record.i = i + 1
               /\ record.r \in 1..Columns
               /\ record.u = u + record.r - 1
               /\ record.discarded = 0
               /\ record.budget = ExpectedWorkspace
               /\ record.reserved = reserved
       /\ record.i + record.u <= Columns
       /\ status' = "running"
       /\ phase' = 1
       /\ step' = record.step
       /\ i' = record.i
       /\ u' = record.u
       /\ r' = record.r
       /\ discarded' = 0
       /\ reserved' = record.reserved
    \/ /\ record.op = "phase2"
       /\ record.result = "ok"
       /\ status = "running"
       /\ phase = 1
       /\ i + u = Columns
       /\ record.phase = 2
       /\ record.step = step
       /\ record.i = i
       /\ record.u = u
       /\ record.r = r
       /\ record.discarded = Rows - Columns
       /\ record.budget = ExpectedWorkspace
       /\ record.reserved = reserved
       /\ status' = "running"
       /\ phase' = 2
       /\ discarded' = Rows - Columns
       /\ UNCHANGED <<step, i, u, r, reserved>>
    \/ /\ \/ /\ record.op = "phase3" /\ phase = 2 /\ record.phase = 3
             \/ /\ record.op = "phase4" /\ phase = 3 /\ record.phase = 4
             \/ /\ record.op = "phase5" /\ phase = 4 /\ record.phase = 5
       /\ record.result = "ok"
       /\ status = "running"
       /\ record.phase = phase + 1
       /\ record.step = step
       /\ record.i = i
       /\ record.u = u
       /\ record.r = r
       /\ record.discarded = discarded
       /\ record.budget = ExpectedWorkspace
       /\ record.reserved = reserved
       /\ status' = "running"
       /\ phase' = record.phase
       /\ UNCHANGED <<step, i, u, r, discarded, reserved>>
    \/ /\ record.op = "complete"
       /\ record.result = "ok"
       /\ status = "running"
       /\ phase = 5
       /\ record.phase = phase
       /\ record.step = step
       /\ record.i = i
       /\ record.u = u
       /\ record.r = r
       /\ record.discarded = discarded
       /\ record.budget = ExpectedWorkspace
       /\ record.reserved = reserved
       /\ status' = "complete"
       /\ UNCHANGED <<phase, step, i, u, r, discarded, reserved>>

Next ==
    /\ traceIndex < Len(Trace)
    /\ traceIndex' = traceIndex + 1
    /\ LET record == Trace[traceIndex + 1]
       IN /\ RecordShape(record)
          /\ Apply(record)
          /\ MatchesNext(record)

TraceSpec == Init /\ [][Next]_vars /\ WF_vars(Next)

TypeOK ==
    /\ status \in {"unallocated", "running", "complete"}
    /\ phase \in 0..5
    /\ step \in Nat
    /\ i \in 0..Columns
    /\ u \in 0..Columns
    /\ r \in 0..Columns
    /\ InitialU \in 0..Columns
    /\ i + u <= Columns
    /\ discarded \in 0..(Rows - Columns)
    /\ reserved \in Nat
    /\ traceIndex \in 1..Len(Trace)

ResourceAccounting ==
    /\ Workspace = ExpectedWorkspace
    /\ reserved <= ExpectedWorkspace
    /\ (status = "unallocated") => reserved = 0
    /\ (status \in {"running", "complete"}) => reserved = ExpectedWorkspace

TraceBudgetFields ==
    /\ Trace[1].budget = 0
    /\ \A index \in 3..Len(Trace): Trace[index].budget = ExpectedWorkspace

PhaseInvariant ==
    /\ (phase = 1) => /\ discarded = 0 /\ i + u <= Columns
    /\ (phase \in 2..5) => /\ i + u = Columns /\ discarded = Rows - Columns
    /\ (status = "complete") => phase = 5

RequiredOps == {"init", "solve_limit", "phase1", "phase2", "phase3", "phase4", "phase5", "complete"}

TraceCoverage ==
    traceIndex # 1 \/
        /\ {Trace[index].op : index \in 1..Len(Trace)} = RequiredOps
        /\ Trace[2].op = "solve_limit"
        /\ Trace[2].budget = ExpectedWorkspace - 1

TraceComplete == <>(traceIndex = Len(Trace))

=====================================================================
