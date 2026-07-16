---------------------- MODULE DecoderTraceSpec ----------------------
EXTENDS FiniteSets, Integers, Naturals, Sequences

CONSTANTS Trace, SymbolIds, Required

ASSUME /\ Trace \in Seq([op : STRING,
                         id : Int,
                         accepted : SUBSET SymbolIds,
                         phase : STRING])
       /\ Len(Trace) > 0
       /\ Required \in 1..Cardinality(SymbolIds)

Phases == {"collecting", "decoded", "verified", "delivered",
           "aborted", "verification_failed"}

VARIABLES accepted, phase, traceIndex

vars == <<accepted, phase, traceIndex>>

Matches(record) ==
    /\ accepted = record.accepted
    /\ phase = record.phase

MatchesNext(record) ==
    /\ accepted' = record.accepted
    /\ phase' = record.phase

Init ==
    /\ traceIndex = 1
    /\ Trace[1].op = "init"
    /\ Trace[1].id = -1
    /\ accepted = {}
    /\ phase = "collecting"
    /\ Matches(Trace[1])

Apply(record) ==
    \/ /\ record.op = "accept"
       /\ record.id \in SymbolIds
       /\ phase = "collecting"
       /\ accepted' = accepted \cup {record.id}
       /\ UNCHANGED phase
    \/ /\ record.op = "reject"
       /\ record.id \notin SymbolIds
       /\ phase = "collecting"
       /\ UNCHANGED <<accepted, phase>>
    \/ /\ record.op = "decode"
       /\ record.id = -1
       /\ phase = "collecting"
       /\ Cardinality(accepted) >= Required
       /\ phase' = "decoded"
       /\ UNCHANGED accepted
    \/ /\ record.op = "verify"
       /\ record.id = -1
       /\ phase = "decoded"
       /\ phase' = "verified"
       /\ UNCHANGED accepted
    \/ /\ record.op = "verify_fail"
       /\ record.id = -1
       /\ phase = "decoded"
       /\ phase' = "verification_failed"
       /\ UNCHANGED accepted
    \/ /\ record.op = "deliver"
       /\ record.id = -1
       /\ phase = "verified"
       /\ phase' = "delivered"
       /\ UNCHANGED accepted
    \/ /\ record.op = "abort"
       /\ record.id = -1
       /\ phase = "collecting"
       /\ phase' = "aborted"
       /\ UNCHANGED accepted
    \/ /\ record.op = "accept_phase_reject"
       /\ phase # "collecting"
       /\ UNCHANGED <<accepted, phase>>
    \/ /\ record.op = "decode_reject"
       /\ record.id = -1
       /\ \/ phase # "collecting"
          \/ Cardinality(accepted) < Required
       /\ UNCHANGED <<accepted, phase>>
    \/ /\ record.op = "verify_reject"
       /\ record.id = -1
       /\ phase # "decoded"
       /\ UNCHANGED <<accepted, phase>>
    \/ /\ record.op = "deliver_reject"
       /\ record.id = -1
       /\ phase # "verified"
       /\ UNCHANGED <<accepted, phase>>
    \/ /\ record.op = "abort_reject"
       /\ record.id = -1
       /\ phase # "collecting"
       /\ UNCHANGED <<accepted, phase>>
    \/ /\ record.op = "observe"
       /\ record.id = -1
       /\ UNCHANGED <<accepted, phase>>

Next ==
    /\ traceIndex < Len(Trace)
    /\ traceIndex' = traceIndex + 1
    /\ LET record == Trace[traceIndex + 1]
       IN  /\ Apply(record)
           /\ MatchesNext(record)

TraceSpec == Init /\ [][Next]_vars /\ WF_vars(Next)

TypeOK ==
    /\ accepted \subseteq SymbolIds
    /\ phase \in Phases
    /\ traceIndex \in 1..Len(Trace)

NoPrematureDecode ==
    phase \in {"decoded", "verified", "delivered", "verification_failed"}
        => Cardinality(accepted) >= Required
NoDeliveryWithoutVerification ==
    phase = "delivered" =>
        \E index \in 1..traceIndex :
            /\ Trace[index].op = "verify"
            /\ Trace[index].phase = "verified"
TraceComplete == <>(traceIndex = Len(Trace))

=====================================================================
