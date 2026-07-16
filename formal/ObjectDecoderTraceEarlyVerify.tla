---------------- MODULE ObjectDecoderTraceEarlyVerify ----------------
EXTENDS ObjectDecoderTraceData

ForgedTrace == [ImplementationTrace EXCEPT ![5].completed = 1]

VARIABLES state, completed, verification, delivery, traceIndex

INSTANCE ObjectDecoderTraceSpec
    WITH Trace <- ForgedTrace,
         Blocks <- ImplementationBlocks,
         state <- state,
         completed <- completed,
         verification <- verification,
         delivery <- delivery,
         traceIndex <- traceIndex

=====================================================================
