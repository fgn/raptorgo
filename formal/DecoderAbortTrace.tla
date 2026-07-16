--------------------- MODULE DecoderAbortTrace ----------------------
EXTENDS DecoderAbortTraceData

VARIABLES accepted, phase, traceIndex

INSTANCE DecoderTraceSpec
    WITH Trace <- ImplementationTrace,
         SymbolIds <- ImplementationSymbolIds,
         Required <- ImplementationRequired,
         accepted <- accepted,
         phase <- phase,
         traceIndex <- traceIndex

=====================================================================
