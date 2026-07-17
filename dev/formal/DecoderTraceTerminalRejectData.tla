-------------- MODULE DecoderTraceTerminalRejectData ----------------
EXTENDS Integers, Sequences

ImplementationSymbolIds == {0, 1}
ImplementationRequired == 1

\* Negative control: terminal decoder states reject every further operation.
ImplementationTrace == <<
    [op |-> "init",   id |-> -1, accepted |-> {},  phase |-> "collecting"],
    [op |-> "accept", id |->  0, accepted |-> {0}, phase |-> "collecting"],
    [op |-> "decode", id |-> -1, accepted |-> {0}, phase |-> "decoded"],
    [op |-> "verify", id |-> -1, accepted |-> {0}, phase |-> "verified"],
    [op |-> "deliver", id |-> -1, accepted |-> {0}, phase |-> "delivered"],
    [op |-> "reject", id |-> 99, accepted |-> {0}, phase |-> "delivered"]
>>

=====================================================================
