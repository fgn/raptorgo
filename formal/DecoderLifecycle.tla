--------------------- MODULE DecoderLifecycle -----------------------
EXTENDS FiniteSets, Integers, Naturals

CONSTANTS MaxSymbol, Required, InjectFault

ASSUME /\ MaxSymbol \in Nat
       /\ Required \in 1..(MaxSymbol + 1)
       /\ InjectFault \in BOOLEAN

SymbolIds == 0..MaxSymbol
Phases == {"collecting", "decoded", "verified", "delivered",
           "aborted", "verification_failed"}
TerminalPhases == {"delivered", "aborted", "verification_failed"}

VARIABLES accepted, phase, everDecoded, everVerified

vars == <<accepted, phase, everDecoded, everVerified>>

Init ==
    /\ accepted = {}
    /\ phase = "collecting"
    /\ everDecoded = FALSE
    /\ everVerified = FALSE

Accept(id) ==
    /\ phase = "collecting"
    /\ id \in SymbolIds
    /\ accepted' = accepted \cup {id}
    /\ phase' = phase
    /\ everDecoded' = everDecoded
    /\ everVerified' = everVerified

Decode ==
    /\ phase = "collecting"
    /\ Cardinality(accepted) >= Required
    /\ phase' = "decoded"
    /\ everDecoded' = TRUE
    /\ accepted' = accepted
    /\ everVerified' = everVerified

VerifySuccess ==
    /\ phase = "decoded"
    /\ phase' = "verified"
    /\ everVerified' = TRUE
    /\ accepted' = accepted
    /\ everDecoded' = everDecoded

VerifyFailure ==
    /\ phase = "decoded"
    /\ phase' = "verification_failed"
    /\ accepted' = accepted
    /\ everDecoded' = everDecoded
    /\ everVerified' = everVerified

Deliver ==
    /\ phase = "verified"
    /\ phase' = "delivered"
    /\ accepted' = accepted
    /\ everDecoded' = everDecoded
    /\ everVerified' = everVerified

Abort ==
    /\ phase = "collecting"
    /\ phase' = "aborted"
    /\ accepted' = accepted
    /\ everDecoded' = everDecoded
    /\ everVerified' = everVerified

\* Fault-injection sentinel: enabled only by DecoderLifecycleMutation.cfg.
\* It forges verification while delivering directly from decoded.
AdversarialDeliver ==
    /\ InjectFault
    /\ phase = "decoded"
    /\ phase' = "delivered"
    /\ accepted' = accepted
    /\ everDecoded' = everDecoded
    /\ everVerified' = TRUE

Next ==
    \/ \E id \in SymbolIds : Accept(id)
    \/ Decode
    \/ VerifySuccess
    \/ VerifyFailure
    \/ Deliver
    \/ Abort
    \/ AdversarialDeliver

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ accepted \subseteq SymbolIds
    /\ phase \in Phases
    /\ everDecoded \in BOOLEAN
    /\ everVerified \in BOOLEAN

AcceptedInDomain == accepted \subseteq SymbolIds
NoPrematureDecode ==
    phase \in {"decoded", "verified", "delivered", "verification_failed"}
        => Cardinality(accepted) >= Required
VerifiedAfterDecode == phase \in {"verified", "delivered"} => everDecoded
DeliveredAfterVerification == phase = "delivered" => everVerified
VerificationFailureAfterDecode == phase = "verification_failed" => everDecoded
AbortedBeforeDecode == phase = "aborted" => ~everDecoded

AcceptOutcome(id) ==
    /\ accepted' = accepted \cup {id}
    /\ phase' = phase
    /\ everDecoded' = everDecoded
    /\ everVerified' = everVerified
DecodeOutcome ==
    /\ accepted' = accepted
    /\ phase' = "decoded"
    /\ everDecoded' = TRUE
    /\ everVerified' = everVerified
VerifySuccessOutcome ==
    /\ accepted' = accepted
    /\ phase' = "verified"
    /\ everDecoded' = everDecoded
    /\ everVerified' = TRUE
VerifyFailureOutcome ==
    /\ accepted' = accepted
    /\ phase' = "verification_failed"
    /\ everDecoded' = everDecoded
    /\ everVerified' = everVerified
DeliverOutcome ==
    /\ accepted' = accepted
    /\ phase' = "delivered"
    /\ everDecoded' = everDecoded
    /\ everVerified' = everVerified
AbortOutcome ==
    /\ accepted' = accepted
    /\ phase' = "aborted"
    /\ everDecoded' = everDecoded
    /\ everVerified' = everVerified

ExpectedNext ==
    \/ \E id \in SymbolIds : /\ phase = "collecting"
                                /\ AcceptOutcome(id)
    \/ /\ phase = "collecting"
       /\ Cardinality(accepted) >= Required
       /\ DecodeOutcome
    \/ /\ phase = "decoded"
       /\ VerifySuccessOutcome
    \/ /\ phase = "decoded"
       /\ VerifyFailureOutcome
    \/ /\ phase = "verified"
       /\ DeliverOutcome
    \/ /\ phase = "collecting"
       /\ AbortOutcome

NonTerminalCanProgress ==
    /\ phase = "collecting" =>
        /\ ENABLED (AbortOutcome /\ Next)
        /\ \A id \in SymbolIds : ENABLED (AcceptOutcome(id) /\ Next)
        /\ Cardinality(accepted) >= Required => ENABLED (DecodeOutcome /\ Next)
    /\ phase = "decoded" =>
        /\ ENABLED (VerifySuccessOutcome /\ Next)
        /\ ENABLED (VerifyFailureOutcome /\ Next)
    /\ phase = "verified" => ENABLED (DeliverOutcome /\ Next)
    /\ phase \in TerminalPhases => ~ENABLED Next

AcceptedMonotonic == [][accepted \subseteq accepted']_vars
DecodedNeverForgotten == [][everDecoded => everDecoded']_vars
VerifiedNeverForgotten == [][everVerified => everVerified']_vars
AcceptTransitionExact ==
    [][\A id \in SymbolIds : Accept(id) => accepted' = accepted \cup {id}]_vars
DuplicateAcceptIsIdempotent ==
    [][\A id \in accepted : Accept(id) => accepted' = accepted]_vars
TerminalStable ==
    [][(phase \in TerminalPhases) => UNCHANGED vars]_vars
EveryNextTransitionAllowed == [][Next => ExpectedNext]_vars

=====================================================================
