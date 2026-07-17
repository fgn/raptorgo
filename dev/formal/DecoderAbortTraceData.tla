-------------------- MODULE DecoderAbortTraceData --------------------
EXTENDS Integers, Sequences

ImplementationSymbolIds == {0, 1, 2}
ImplementationRequired == 2

ImplementationTrace == <<
    [op |-> "init", id |-> -1, accepted |-> {}, phase |-> "collecting"],
    [op |-> "accept", id |-> 1, accepted |-> {1}, phase |-> "collecting"],
    [op |-> "abort", id |-> -1, accepted |-> {1}, phase |-> "aborted"]
>>

=====================================================================
