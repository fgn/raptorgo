-------------------- MODULE DecoderTraceInvalid ---------------------
EXTENDS DecoderTraceInvalidData

VARIABLES accepted, phase, traceIndex

INSTANCE DecoderTraceSpec
    WITH Trace <- ImplementationTrace,
         SymbolIds <- ImplementationSymbolIds,
         Required <- ImplementationRequired,
         accepted <- accepted,
         phase <- phase,
         traceIndex <- traceIndex

=====================================================================
