----------------------- MODULE HybridTraceForgedResidual -----------------------
EXTENDS HybridTraceData

VARIABLES stage, planCursor, replayCursor, substitutionCursor,
          i, u, consumed, denseColumns, residualRows, residualColumns,
          activeRows, activeColumns, rowMap, columnMap, denseRows,
          planReserved, executionReserved, traceIndex

INSTANCE HybridTraceSpec
    WITH Trace <- ImplementationTrace,
         PlanEvents <- ImplementationPlanEvents,
         VRows <- ImplementationVRows,
         URows <- ImplementationURows,
         RHSMap <- ImplementationRHSMap,
         ExecutionCorpus <- ForgedExecutionCorpus,
         S <- ImplementationS,
         H <- ImplementationH,
         W <- ImplementationW,
         P <- ImplementationP,
         L <- ImplementationL,
         EncodedRows <- ImplementationEncodedRows,
         VCapacity <- ImplementationVCapacity,
         UCapacity <- ImplementationUCapacity,
         VNonzeros <- ImplementationVNonzeros,
         SymbolSize <- ImplementationSymbolSize,
         PlanWorkspace <- ImplementationPlanWorkspace,
         ExecutionWorkspace <- ImplementationExecutionWorkspace,
         stage <- stage,
         planCursor <- planCursor,
         replayCursor <- replayCursor,
         substitutionCursor <- substitutionCursor,
         i <- i,
         u <- u,
         consumed <- consumed,
         denseColumns <- denseColumns,
         residualRows <- residualRows,
         residualColumns <- residualColumns,
         activeRows <- activeRows,
         activeColumns <- activeColumns,
         rowMap <- rowMap,
         columnMap <- columnMap,
         denseRows <- denseRows,
         planReserved <- planReserved,
         executionReserved <- executionReserved,
         traceIndex <- traceIndex

=====================================================================
