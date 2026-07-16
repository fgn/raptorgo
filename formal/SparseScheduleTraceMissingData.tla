---------------- MODULE SparseScheduleTraceMissingData ----------------
EXTENDS Integers, Sequences

ImplementationRows == 4
ImplementationColumns == 5
ImplementationMaxNonzeros == 6

ImplementationTrace == <<
    [op |-> "init", result |-> "ok", left |-> -1, right |-> -1, budget |-> 0,
     status |-> "unallocated", rowMap |-> <<>>, rowInverse |-> <<>>,
     columnMap |-> <<>>, columnInverse |-> <<>>, reserved |-> 0, cursor |-> 0],
    [op |-> "create_limit", result |-> "limit", left |-> -1, right |-> -1, budget |-> 119,
     status |-> "unallocated", rowMap |-> <<>>, rowInverse |-> <<>>,
     columnMap |-> <<>>, columnInverse |-> <<>>, reserved |-> 0, cursor |-> 0],
    [op |-> "create", result |-> "ok", left |-> -1, right |-> -1, budget |-> 120,
     status |-> "ready", rowMap |-> <<1, 2, 3, 4>>, rowInverse |-> <<1, 2, 3, 4>>,
     columnMap |-> <<1, 2, 3, 4, 5>>, columnInverse |-> <<1, 2, 3, 4, 5>>, reserved |-> 120, cursor |-> 0],
    [op |-> "set", result |-> "ok", left |-> 0, right |-> 0, budget |-> 0,
     status |-> "ready", rowMap |-> <<1, 2, 3, 4>>, rowInverse |-> <<1, 2, 3, 4>>,
     columnMap |-> <<1, 2, 3, 4, 5>>, columnInverse |-> <<1, 2, 3, 4, 5>>, reserved |-> 120, cursor |-> 1],
    [op |-> "set", result |-> "ok", left |-> 0, right |-> 2, budget |-> 0,
     status |-> "ready", rowMap |-> <<1, 2, 3, 4>>, rowInverse |-> <<1, 2, 3, 4>>,
     columnMap |-> <<1, 2, 3, 4, 5>>, columnInverse |-> <<1, 2, 3, 4, 5>>, reserved |-> 120, cursor |-> 2],
    [op |-> "set", result |-> "ok", left |-> 1, right |-> 1, budget |-> 0,
     status |-> "ready", rowMap |-> <<1, 2, 3, 4>>, rowInverse |-> <<1, 2, 3, 4>>,
     columnMap |-> <<1, 2, 3, 4, 5>>, columnInverse |-> <<1, 2, 3, 4, 5>>, reserved |-> 120, cursor |-> 3],
    [op |-> "set", result |-> "ok", left |-> 1, right |-> 3, budget |-> 0,
     status |-> "ready", rowMap |-> <<1, 2, 3, 4>>, rowInverse |-> <<1, 2, 3, 4>>,
     columnMap |-> <<1, 2, 3, 4, 5>>, columnInverse |-> <<1, 2, 3, 4, 5>>, reserved |-> 120, cursor |-> 4],
    [op |-> "set", result |-> "ok", left |-> 2, right |-> 0, budget |-> 0,
     status |-> "ready", rowMap |-> <<1, 2, 3, 4>>, rowInverse |-> <<1, 2, 3, 4>>,
     columnMap |-> <<1, 2, 3, 4, 5>>, columnInverse |-> <<1, 2, 3, 4, 5>>, reserved |-> 120, cursor |-> 5],
    [op |-> "set", result |-> "ok", left |-> 2, right |-> 2, budget |-> 0,
     status |-> "ready", rowMap |-> <<1, 2, 3, 4>>, rowInverse |-> <<1, 2, 3, 4>>,
     columnMap |-> <<1, 2, 3, 4, 5>>, columnInverse |-> <<1, 2, 3, 4, 5>>, reserved |-> 120, cursor |-> 6],
    [op |-> "reject_capacity", result |-> "capacity", left |-> 3, right |-> 4, budget |-> 0,
     status |-> "ready", rowMap |-> <<1, 2, 3, 4>>, rowInverse |-> <<1, 2, 3, 4>>,
     columnMap |-> <<1, 2, 3, 4, 5>>, columnInverse |-> <<1, 2, 3, 4, 5>>, reserved |-> 120, cursor |-> 6],
    [op |-> "swap_row", result |-> "ok", left |-> 0, right |-> 3, budget |-> 0,
     status |-> "ready", rowMap |-> <<4, 2, 3, 1>>, rowInverse |-> <<4, 2, 3, 1>>,
     columnMap |-> <<1, 2, 3, 4, 5>>, columnInverse |-> <<1, 2, 3, 4, 5>>, reserved |-> 120, cursor |-> 7],
    [op |-> "swap_column", result |-> "ok", left |-> 1, right |-> 4, budget |-> 0,
     status |-> "ready", rowMap |-> <<4, 2, 3, 1>>, rowInverse |-> <<4, 2, 3, 1>>,
     columnMap |-> <<1, 5, 3, 4, 2>>, columnInverse |-> <<1, 5, 3, 4, 2>>, reserved |-> 120, cursor |-> 8],
    [op |-> "swap_row_same", result |-> "ok", left |-> 1, right |-> 1, budget |-> 0,
     status |-> "ready", rowMap |-> <<4, 2, 3, 1>>, rowInverse |-> <<4, 2, 3, 1>>,
     columnMap |-> <<1, 5, 3, 4, 2>>, columnInverse |-> <<1, 5, 3, 4, 2>>, reserved |-> 120, cursor |-> 10],
    [op |-> "swap_column_same", result |-> "ok", left |-> 2, right |-> 2, budget |-> 0,
     status |-> "ready", rowMap |-> <<4, 2, 3, 1>>, rowInverse |-> <<4, 2, 3, 1>>,
     columnMap |-> <<1, 5, 3, 4, 2>>, columnInverse |-> <<1, 5, 3, 4, 2>>, reserved |-> 120, cursor |-> 11],
    [op |-> "reject_index", result |-> "index", left |-> 0, right |-> 4, budget |-> 0,
     status |-> "ready", rowMap |-> <<4, 2, 3, 1>>, rowInverse |-> <<4, 2, 3, 1>>,
     columnMap |-> <<1, 5, 3, 4, 2>>, columnInverse |-> <<1, 5, 3, 4, 2>>, reserved |-> 120, cursor |-> 11],
    [op |-> "observe", result |-> "ok", left |-> -1, right |-> -1, budget |-> 0,
     status |-> "ready", rowMap |-> <<4, 2, 3, 1>>, rowInverse |-> <<4, 2, 3, 1>>,
     columnMap |-> <<1, 5, 3, 4, 2>>, columnInverse |-> <<1, 5, 3, 4, 2>>, reserved |-> 120, cursor |-> 11]
>>

=====================================================================
