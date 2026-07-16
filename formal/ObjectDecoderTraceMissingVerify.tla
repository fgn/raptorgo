---------------- MODULE ObjectDecoderTraceMissingVerify ----------------
EXTENDS ObjectDecoderTraceData

MissingTrace == SubSeq(ImplementationTrace, 1, 4) \o SubSeq(ImplementationTrace, 6, Len(ImplementationTrace))

VARIABLES state, completed, verification, delivery, traceIndex

INSTANCE ObjectDecoderTraceSpec
    WITH Trace <- MissingTrace,
         Blocks <- ImplementationBlocks,
         state <- state,
         completed <- completed,
         verification <- verification,
         delivery <- delivery,
         traceIndex <- traceIndex

=====================================================================
