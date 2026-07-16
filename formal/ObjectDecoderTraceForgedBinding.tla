---------------- MODULE ObjectDecoderTraceForgedBinding ----------------
EXTENDS ObjectDecoderTraceData

ForgedTrace == [ImplementationTrace EXCEPT ![5].bindingOK = FALSE]

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
