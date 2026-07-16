------------------ MODULE DecoderTraceInvalidData -------------------
EXTENDS Integers, Sequences

ImplementationSymbolIds == {0, 1, 2}
ImplementationRequired == 2

\* Negative control: decoding after only one unique symbol is illegal.
ImplementationTrace == <<
    [op |-> "init",   id |-> -1, accepted |-> {},  phase |-> "collecting"],
    [op |-> "accept", id |->  0, accepted |-> {0}, phase |-> "collecting"],
    [op |-> "decode", id |-> -1, accepted |-> {0}, phase |-> "decoded"]
>>

=====================================================================
