---------------- MODULE SparseScheduleTraceMissing ----------------
EXTENDS SparseScheduleTraceMissingData

VARIABLES status, rowMap, columnMap, reserved, cursor, traceIndex

INSTANCE SparseScheduleTraceSpec
    WITH Trace <- ImplementationTrace,
         Rows <- ImplementationRows,
         Columns <- ImplementationColumns,
         MaxNonzeros <- ImplementationMaxNonzeros,
         status <- status,
         rowMap <- rowMap,
         columnMap <- columnMap,
         reserved <- reserved,
         cursor <- cursor,
         traceIndex <- traceIndex

=====================================================================
