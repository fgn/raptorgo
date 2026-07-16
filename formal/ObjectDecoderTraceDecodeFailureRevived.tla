---------------- MODULE ObjectDecoderTraceDecodeFailureRevived ----------------
EXTENDS ObjectDecoderDecodeFailureTraceData

RevivedTrace == [ImplementationTrace EXCEPT ![Len(ImplementationTrace)].state = "receiving"]

VARIABLES state, completed, verification, delivery, traceIndex

INSTANCE ObjectDecoderTraceSpec
    WITH Trace <- RevivedTrace,
         Blocks <- ImplementationBlocks,
         state <- state,
         completed <- completed,
         verification <- verification,
         delivery <- delivery,
         traceIndex <- traceIndex

=====================================================================
