---------------- MODULE ObjectDecoderVerifyFailureTraceData ----------------
EXTENDS Naturals, Sequences

ImplementationBlocks == 2

ImplementationTrace == <<
    [op |-> "init", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "add_wait", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "block_complete", result |-> "ok", state |-> "receiving", completed |-> 1, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "add_wait", result |-> "ok", state |-> "receiving", completed |-> 1, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "verify_enter", result |-> "ok", state |-> "decoded", completed |-> 2, verification |-> 1, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "add_return", result |-> "verification_error", state |-> "verification-failed", completed |-> 2, verification |-> 1, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "terminal_add", result |-> "terminal", state |-> "verification-failed", completed |-> 2, verification |-> 1, delivery |-> 0, bindingOK |-> TRUE]
>>

=====================================================================
