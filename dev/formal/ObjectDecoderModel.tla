------------------- MODULE ObjectDecoderModel --------------------
EXTENDS FiniteSets, Integers, Naturals

CONSTANTS Blocks, K, AllowDecodeFail, InjectFault

ASSUME /\ Blocks \in 1..2
       /\ K \in 1..2
       /\ AllowDecodeFail \in BOOLEAN
       /\ InjectFault \in BOOLEAN

BlockIds == 0..(Blocks - 1)
SymbolIds == 0..(K - 1)
States == {
    "receiving", "decode-failed", "decoded", "verified", "delivered",
    "verification-failed", "delivery-failed"
}
StableTerminalStates == {
    "decode-failed", "delivered", "verification-failed", "delivery-failed"
}

VARIABLES state, accepted0, accepted1, completed, verification, delivery

vars == <<state, accepted0, accepted1, completed, verification, delivery>>

Accepted(block) == IF block = 0 THEN accepted0 ELSE accepted1

AcceptedUpdate(block, esi) ==
    /\ accepted0' = IF block = 0 THEN accepted0 \cup {esi} ELSE accepted0
    /\ accepted1' = IF block = 1 THEN accepted1 \cup {esi} ELSE accepted1

AcceptedUnchanged == UNCHANGED <<accepted0, accepted1>>
CountersUnchanged == UNCHANGED <<completed, verification, delivery>>

Init ==
    /\ state = "receiving"
    /\ accepted0 = {}
    /\ accepted1 = {}
    /\ completed = 0
    /\ verification = 0
    /\ delivery = 0

\* One source packet that leaves its target block incomplete, including an
\* idempotent retransmission. Realization: AddPacket(source(block, esi)).
AddWait(block, esi) ==
    /\ state = "receiving"
    /\ block \in BlockIds
    /\ esi \in SymbolIds
    /\ Cardinality(Accepted(block) \cup {esi}) < K
    /\ AcceptedUpdate(block, esi)
    /\ state' = state
    /\ CountersUnchanged

\* One source packet completes a non-final block.
BlockComplete(block, esi) ==
    /\ state = "receiving"
    /\ block \in BlockIds
    /\ esi \in SymbolIds
    /\ esi \notin Accepted(block)
    /\ Cardinality(Accepted(block) \cup {esi}) = K
    /\ completed + 1 < Blocks
    /\ AcceptedUpdate(block, esi)
    /\ completed' = completed + 1
    /\ UNCHANGED <<state, verification, delivery>>

\* The completing AddPacket enters its verifier callback synchronously.
FinalizeVerifyEnter(block, esi) ==
    /\ state = "receiving"
    /\ block \in BlockIds
    /\ esi \in SymbolIds
    /\ esi \notin Accepted(block)
    /\ Cardinality(Accepted(block) \cup {esi}) = K
    /\ completed + 1 = Blocks
    /\ AcceptedUpdate(block, esi)
    /\ state' = "decoded"
    /\ completed' = Blocks
    /\ verification' = 1
    /\ UNCHANGED delivery

\* The verifier returns nil; production publishes ObjectVerified.
Verified ==
    /\ state = "decoded"
    /\ state' = "verified"
    /\ AcceptedUnchanged
    /\ UNCHANGED <<completed, verification, delivery>>

\* The verifier returns an error; the completing AddPacket returns it.
VerifyFailed ==
    /\ state = "decoded"
    /\ state' = "verification-failed"
    /\ AcceptedUnchanged
    /\ UNCHANGED <<completed, verification, delivery>>

\* The deliverer callback is entered exactly once.
DeliverEnter ==
    /\ state = "verified"
    /\ delivery = 0
    /\ delivery' = 1
    /\ AcceptedUnchanged
    /\ UNCHANGED <<state, completed, verification>>

\* The deliverer returns nil; the completing AddPacket returns success.
Delivered ==
    /\ state = "verified"
    /\ delivery = 1
    /\ state' = "delivered"
    /\ AcceptedUnchanged
    /\ UNCHANGED <<completed, verification, delivery>>

\* The deliverer returns an error; the completing AddPacket returns it.
DeliveryFailed ==
    /\ state = "verified"
    /\ delivery = 1
    /\ state' = "delivery-failed"
    /\ AcceptedUnchanged
    /\ UNCHANGED <<completed, verification, delivery>>

\* Invalid packet parsing is a receiving-state no-op.
RejectInvalid ==
    /\ state = "receiving"
    /\ UNCHANGED vars

\* A packet exceeding MaxPacketBytes is a receiving-state no-op.
RejectLimit ==
    /\ state = "receiving"
    /\ UNCHANGED vars

\* A conflicting duplicate is observable only while its block is incomplete.
RejectConflict(block, esi) ==
    /\ state = "receiving"
    /\ block \in BlockIds
    /\ esi \in Accepted(block)
    /\ Cardinality(Accepted(block)) < K
    /\ UNCHANGED vars

\* AddPacket rejects stable states and the two synchronous callback windows.
TerminalAdd ==
    /\ \/ state \in StableTerminalStates
       \/ /\ state = "decoded"
          /\ verification = 1
       \/ /\ state = "verified"
          /\ delivery = 1
    /\ UNCHANGED vars

\* The rank-deficient fixture realizes this branch after a hidden prologue.
DecodeFail ==
    /\ AllowDecodeFail
    /\ Blocks = 1
    /\ state = "receiving"
    /\ state' = "decode-failed"
    /\ AcceptedUnchanged
    /\ UNCHANGED <<completed, verification, delivery>>

\* Mutation sentinel: no production realization may skip verification.
AdversarialSkipVerify ==
    /\ InjectFault
    /\ state = "decoded"
    /\ state' = "delivered"
    /\ delivery' = 1
    /\ AcceptedUnchanged
    /\ UNCHANGED <<completed, verification>>

ExpectedNext ==
    \/ \E block \in BlockIds, esi \in SymbolIds : AddWait(block, esi)
    \/ \E block \in BlockIds, esi \in SymbolIds : BlockComplete(block, esi)
    \/ \E block \in BlockIds, esi \in SymbolIds : FinalizeVerifyEnter(block, esi)
    \/ Verified
    \/ VerifyFailed
    \/ DeliverEnter
    \/ Delivered
    \/ DeliveryFailed
    \/ RejectInvalid
    \/ RejectLimit
    \/ \E block \in BlockIds, esi \in SymbolIds : RejectConflict(block, esi)
    \/ TerminalAdd
    \/ DecodeFail

Next ==
    \/ ExpectedNext
    \/ AdversarialSkipVerify

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ state \in States
    /\ accepted0 \subseteq SymbolIds
    /\ accepted1 \subseteq SymbolIds
    /\ Blocks = 1 => accepted1 = {}
    /\ completed \in 0..Blocks
    /\ verification \in 0..1
    /\ delivery \in 0..1

CompletedFromAccepted ==
    Cardinality({block \in BlockIds : Cardinality(Accepted(block)) = K})

CompletionMatchesAccepted == completed = CompletedFromAccepted
ExactlyOnce == delivery <= verification
NoDeliveryWithoutVerification == delivery > 0 => verification > 0
VerifiedAfterVerification == state = "verified" => verification = 1
NoSuccessBeforeCompletion ==
    state \in {"decoded", "verified", "delivered",
               "verification-failed", "delivery-failed"} => completed = Blocks
NonTerminalCanProgress == state \notin StableTerminalStates => ENABLED Next

Accepted0Monotonic == [][accepted0 \subseteq accepted0']_vars
Accepted1Monotonic == [][accepted1 \subseteq accepted1']_vars
TerminalStable == [][(state \in StableTerminalStates) => UNCHANGED vars]_vars
EveryNextTransitionAllowed == [][Next => ExpectedNext]_vars

=====================================================================
