---------------- MODULE ObjectDecoderTraceSpec ----------------
EXTENDS Naturals, Sequences

CONSTANTS Trace, Blocks

\* Blocks counts only nonempty source blocks, matching CompletedBlocks.

VARIABLES state, completed, verification, delivery, traceIndex

vars == <<state, completed, verification, delivery, traceIndex>>

States == {
    "receiving", "decode-failed", "decoded", "verified", "delivered",
    "verification-failed", "delivery-failed"
}

StableTerminalStates == {
    "decode-failed", "delivered", "verification-failed", "delivery-failed"
}

AddRejectingStates == StableTerminalStates \cup {"decoded", "verified"}

RejectingStateReady ==
    /\ state \in AddRejectingStates
    /\ (state = "decoded" => /\ verification = 1
                               /\ delivery = 0)
    /\ (state = "verified" => /\ verification = 1
                                /\ delivery = 1)

Matches(record) ==
    /\ state = record.state
    /\ completed = record.completed
    /\ verification = record.verification
    /\ delivery = record.delivery

MatchesNext(record) ==
    /\ state' = record.state
    /\ completed' = record.completed
    /\ verification' = record.verification
    /\ delivery' = record.delivery

ReceivingInit ==
    /\ Blocks > 0
    /\ Trace[1].op = "init"
    /\ Trace[1].result = "ok"
    /\ Trace[1].bindingOK
    /\ state = "receiving"
    /\ completed = 0
    /\ verification = 0
    /\ delivery = 0

ConstructDeliveredInit ==
    /\ Blocks = 0
    /\ Trace[1].op = "construct_final"
    /\ Trace[1].result = "ok"
    /\ Trace[1].bindingOK
    /\ state = "delivered"
    /\ completed = 0
    /\ verification = 1
    /\ delivery = 1

ConstructVerificationFailedInit ==
    /\ Blocks = 0
    /\ Trace[1].op = "construct_final"
    /\ Trace[1].result = "verification_error"
    /\ Trace[1].bindingOK
    /\ state = "verification-failed"
    /\ completed = 0
    /\ verification = 1
    /\ delivery = 0

ConstructDeliveryFailedInit ==
    /\ Blocks = 0
    /\ Trace[1].op = "construct_final"
    /\ Trace[1].result = "delivery_error"
    /\ Trace[1].bindingOK
    /\ state = "delivery-failed"
    /\ completed = 0
    /\ verification = 1
    /\ delivery = 1

Init ==
    /\ Len(Trace) > 0
    /\ traceIndex = 1
    /\ \/ ReceivingInit
       \/ ConstructDeliveredInit
       \/ ConstructVerificationFailedInit
       \/ ConstructDeliveryFailedInit
    /\ Matches(Trace[1])

Apply(record) ==
    \/ /\ record.op = "add_wait"
       /\ record.result = "ok"
       /\ state = "receiving"
       /\ UNCHANGED <<state, completed, verification, delivery>>
    \/ /\ record.op = "block_complete"
       /\ record.result = "ok"
       /\ state = "receiving"
       /\ completed < Blocks - 1
       /\ completed' = completed + 1
       /\ UNCHANGED <<state, verification, delivery>>
    \/ /\ record.op = "verify_enter"
       /\ record.result = "ok"
       /\ record.bindingOK
       /\ state = "receiving"
       /\ completed = Blocks - 1
       /\ state' = "decoded"
       /\ completed' = Blocks
       /\ verification' = verification + 1
       /\ UNCHANGED delivery
    \/ /\ record.op = "verified"
       /\ record.result = "ok"
       /\ record.bindingOK
       /\ state = "decoded"
       /\ verification = 1
       /\ delivery = 0
       /\ state' = "verified"
       /\ UNCHANGED <<completed, verification, delivery>>
    \/ /\ record.op = "deliver_enter"
       /\ record.result = "ok"
       /\ record.bindingOK
       /\ state = "verified"
       /\ verification = 1
       /\ delivery = 0
       /\ delivery' = delivery + 1
       /\ UNCHANGED <<state, completed, verification>>
    \/ /\ record.op = "decode_failure"
       /\ record.result = "decode_error"
       /\ record.bindingOK
       /\ state = "receiving"
       /\ completed < Blocks
       /\ verification = 0
       /\ delivery = 0
       /\ state' = "decode-failed"
       /\ UNCHANGED <<completed, verification, delivery>>
    \/ /\ record.op = "add_reject_invalid"
       /\ record.result = "invalid"
       /\ state = "receiving"
       /\ UNCHANGED <<state, completed, verification, delivery>>
    \/ /\ record.op = "add_reject_limit"
       /\ record.result = "limit"
       /\ state = "receiving"
       /\ UNCHANGED <<state, completed, verification, delivery>>
    \/ /\ record.op = "add_reject_conflict"
       /\ record.result = "conflict"
       /\ state = "receiving"
       /\ UNCHANGED <<state, completed, verification, delivery>>
    \/ /\ record.op = "add_return"
       /\ IF record.result = "ok"
          THEN /\ state = "verified"
               /\ state' = "delivered"
               /\ verification = 1
               /\ delivery = 1
          ELSE IF record.result = "verification_error"
          THEN /\ state = "decoded"
               /\ state' = "verification-failed"
               /\ verification = 1
               /\ delivery = 0
          ELSE /\ record.result = "delivery_error"
               /\ state = "verified"
               /\ state' = "delivery-failed"
               /\ verification = 1
               /\ delivery = 1
       /\ UNCHANGED <<completed, verification, delivery>>
    \/ /\ record.op = "terminal_add"
       /\ record.result = "terminal"
       /\ RejectingStateReady
       /\ UNCHANGED <<state, completed, verification, delivery>>

Next ==
    /\ traceIndex < Len(Trace)
    /\ traceIndex' = traceIndex + 1
    /\ LET record == Trace[traceIndex + 1]
       IN /\ Apply(record)
          /\ MatchesNext(record)

TraceSpec == Init /\ [][Next]_vars /\ WF_vars(Next)

TypeOK ==
    /\ state \in States
    /\ Blocks \in Nat
    /\ completed \in 0..Blocks
    /\ verification \in 0..1
    /\ delivery \in 0..1
    /\ traceIndex \in 1..Len(Trace)
    /\ (Blocks = 0 => state \in StableTerminalStates)

ExactlyOnce == delivery <= verification
NoDeliveryWithoutVerification == delivery > 0 => verification > 0
VerifiedAfterVerification == state = "verified" => verification = 1
NoSuccessBeforeCompletion ==
    state \in {"decoded", "verified", "delivered", "verification-failed", "delivery-failed"} =>
        completed = Blocks
TraceComplete == <>(traceIndex = Len(Trace))

=====================================================================
