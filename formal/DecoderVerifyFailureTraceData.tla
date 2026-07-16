-------------------- MODULE DecoderVerifyFailureTraceData --------------------
EXTENDS Integers, Sequences

ImplementationSymbolIds == {0, 1, 2}
ImplementationRequired == 2

ImplementationTrace == <<
    [op |-> "init", id |-> -1, accepted |-> {}, phase |-> "collecting"],
    [op |-> "accept", id |-> 0, accepted |-> {0}, phase |-> "collecting"],
    [op |-> "accept", id |-> 2, accepted |-> {0, 2}, phase |-> "collecting"],
    [op |-> "decode", id |-> -1, accepted |-> {0, 2}, phase |-> "decoded"],
    [op |-> "verify_fail", id |-> -1, accepted |-> {0, 2}, phase |-> "verification_failed"]
>>

=====================================================================
