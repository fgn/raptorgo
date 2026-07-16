-------------------- MODULE DecoderRejectedCallsTraceData --------------------
EXTENDS Integers, Sequences

ImplementationSymbolIds == {0, 1, 2}
ImplementationRequired == 2

ImplementationTrace == <<
    [op |-> "init", id |-> -1, accepted |-> {}, phase |-> "collecting"],
    [op |-> "decode_reject", id |-> -1, accepted |-> {}, phase |-> "collecting"],
    [op |-> "observe", id |-> -1, accepted |-> {}, phase |-> "collecting"],
    [op |-> "accept", id |-> 0, accepted |-> {0}, phase |-> "collecting"],
    [op |-> "accept", id |-> 2, accepted |-> {0, 2}, phase |-> "collecting"],
    [op |-> "decode", id |-> -1, accepted |-> {0, 2}, phase |-> "decoded"],
    [op |-> "deliver_reject", id |-> -1, accepted |-> {0, 2}, phase |-> "decoded"],
    [op |-> "observe", id |-> -1, accepted |-> {0, 2}, phase |-> "decoded"],
    [op |-> "verify", id |-> -1, accepted |-> {0, 2}, phase |-> "verified"],
    [op |-> "deliver", id |-> -1, accepted |-> {0, 2}, phase |-> "delivered"],
    [op |-> "accept_phase_reject", id |-> 99, accepted |-> {0, 2}, phase |-> "delivered"],
    [op |-> "observe", id |-> -1, accepted |-> {0, 2}, phase |-> "delivered"],
    [op |-> "decode_reject", id |-> -1, accepted |-> {0, 2}, phase |-> "delivered"],
    [op |-> "observe", id |-> -1, accepted |-> {0, 2}, phase |-> "delivered"],
    [op |-> "verify_reject", id |-> -1, accepted |-> {0, 2}, phase |-> "delivered"],
    [op |-> "observe", id |-> -1, accepted |-> {0, 2}, phase |-> "delivered"],
    [op |-> "deliver_reject", id |-> -1, accepted |-> {0, 2}, phase |-> "delivered"],
    [op |-> "observe", id |-> -1, accepted |-> {0, 2}, phase |-> "delivered"],
    [op |-> "abort_reject", id |-> -1, accepted |-> {0, 2}, phase |-> "delivered"],
    [op |-> "observe", id |-> -1, accepted |-> {0, 2}, phase |-> "delivered"]
>>

=====================================================================
