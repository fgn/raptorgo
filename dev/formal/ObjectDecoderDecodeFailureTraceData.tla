---------------- MODULE ObjectDecoderDecodeFailureTraceData ----------------
EXTENDS Naturals, Sequences

ImplementationBlocks == 1

ImplementationTrace == <<
    [op |-> "init", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "add_wait", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "add_wait", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "add_wait", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "add_wait", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "add_wait", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "add_wait", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "add_wait", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "add_wait", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "add_wait", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "add_wait", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "decode_failure", result |-> "decode_error", state |-> "decode-failed", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "terminal_add", result |-> "terminal", state |-> "decode-failed", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE]
>>

=====================================================================
