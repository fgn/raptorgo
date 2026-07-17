---------------- MODULE ObjectDecoderTraceEarlyDelivery ----------------
EXTENDS ObjectDecoderTraceData

ForgedTrace == [ImplementationTrace EXCEPT ![7].state = "decoded"]

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
