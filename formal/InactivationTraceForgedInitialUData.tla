---------------- MODULE InactivationTraceForgedInitialUData ----------------
EXTENDS Integers, Sequences

ImplementationRows == 29
ImplementationColumns == 27
ImplementationInitialU == 10
ImplementationMaxNonzeros == 597
ImplementationSymbolSize == 1
ImplementationWorkspace == 3993

ImplementationTrace == <<
    [op |-> "init", result |-> "ok", status |-> "unallocated", budget |-> 0, reserved |-> 0,
     phase |-> 0, step |-> 0, i |-> 0, u |-> 0, r |-> 0, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "solve_limit", result |-> "limit", status |-> "unallocated", budget |-> 3992, reserved |-> 0,
     phase |-> 0, step |-> 0, i |-> 0, u |-> 0, r |-> 0, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 0, i |-> 0, u |-> 11, r |-> 0, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 1, i |-> 1, u |-> 11, r |-> 2, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 2, i |-> 2, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 3, i |-> 3, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 4, i |-> 4, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 5, i |-> 5, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 6, i |-> 6, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 7, i |-> 7, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 8, i |-> 8, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 9, i |-> 9, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 10, i |-> 10, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 11, i |-> 11, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 12, i |-> 12, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 13, i |-> 13, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 14, i |-> 14, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 15, i |-> 15, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase1", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 1, step |-> 16, i |-> 16, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 0],
    [op |-> "phase2", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 2, step |-> 16, i |-> 16, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 2],
    [op |-> "phase3", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 3, step |-> 16, i |-> 16, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 2],
    [op |-> "phase4", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 4, step |-> 16, i |-> 16, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 2],
    [op |-> "phase5", result |-> "ok", status |-> "running", budget |-> 3993, reserved |-> 3993,
     phase |-> 5, step |-> 16, i |-> 16, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 2],
    [op |-> "complete", result |-> "ok", status |-> "complete", budget |-> 3993, reserved |-> 3993,
     phase |-> 5, step |-> 16, i |-> 16, u |-> 11, r |-> 1, l |-> 27, m |-> 29, discarded |-> 2]
>>

=====================================================================
