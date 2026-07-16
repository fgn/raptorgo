---------------- MODULE ObjectDecoderVerifyFailureTrace ----------------
EXTENDS ObjectDecoderVerifyFailureTraceData

VARIABLES state, completed, verification, delivery, traceIndex

INSTANCE ObjectDecoderTraceSpec
    WITH Trace <- ImplementationTrace,
         Blocks <- ImplementationBlocks,
         state <- state,
         completed <- completed,
         verification <- verification,
         delivery <- delivery,
         traceIndex <- traceIndex

=====================================================================
