---------------- MODULE ObjectDecoderDeliveryFailureTraceData ----------------
EXTENDS Naturals, Sequences

ImplementationBlocks == 2

ImplementationTrace == <<
    [op |-> "init", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "add_wait", result |-> "ok", state |-> "receiving", completed |-> 0, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "block_complete", result |-> "ok", state |-> "receiving", completed |-> 1, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "add_wait", result |-> "ok", state |-> "receiving", completed |-> 1, verification |-> 0, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "verify_enter", result |-> "ok", state |-> "decoded", completed |-> 2, verification |-> 1, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "verified", result |-> "ok", state |-> "verified", completed |-> 2, verification |-> 1, delivery |-> 0, bindingOK |-> TRUE],
    [op |-> "deliver_enter", result |-> "ok", state |-> "verified", completed |-> 2, verification |-> 1, delivery |-> 1, bindingOK |-> TRUE],
    [op |-> "add_return", result |-> "delivery_error", state |-> "delivery-failed", completed |-> 2, verification |-> 1, delivery |-> 1, bindingOK |-> TRUE],
    [op |-> "terminal_add", result |-> "terminal", state |-> "delivery-failed", completed |-> 2, verification |-> 1, delivery |-> 1, bindingOK |-> TRUE]
>>

=====================================================================
