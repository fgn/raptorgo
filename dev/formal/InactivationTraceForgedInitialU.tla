--------------- MODULE InactivationTraceForgedInitialU ---------------
EXTENDS InactivationTraceForgedInitialUData

VARIABLES status, phase, step, i, u, r, discarded, reserved, traceIndex

INSTANCE InactivationTraceSpec
    WITH Trace <- ImplementationTrace,
         Rows <- ImplementationRows,
         Columns <- ImplementationColumns,
         InitialU <- ImplementationInitialU,
         MaxNonzeros <- ImplementationMaxNonzeros,
         SymbolSize <- ImplementationSymbolSize,
         Workspace <- ImplementationWorkspace,
         status <- status,
         phase <- phase,
         step <- step,
         i <- i,
         u <- u,
         r <- r,
         discarded <- discarded,
         reserved <- reserved,
         traceIndex <- traceIndex

=====================================================================
