---------------------- MODULE MappingTraceSpec ----------------------
EXTENDS Integers, Naturals, Sequences, FiniteSets

CONSTANTS Trace, KPrimes, MaxESI
VARIABLE traceIndex

ASSUME /\ Len(Trace) > 0
       /\ Len(KPrimes) > 0
       /\ \A index \in 1..(Len(KPrimes) - 1) :
              KPrimes[index] < KPrimes[index + 1]
       /\ MaxESI >= KPrimes[Len(KPrimes)]

KPrimeValues == {KPrimes[index] : index \in 1..Len(KPrimes)}
MaxK == KPrimes[Len(KPrimes)]

ExpectedKPrime(sourceSymbols) ==
    CHOOSE candidate \in KPrimeValues :
        /\ candidate >= sourceSymbols
        /\ \A other \in KPrimeValues :
               other >= sourceSymbols => candidate <= other

ExpectedISI(sourceSymbols, extendedSymbols, esiValue) ==
    IF esiValue < sourceSymbols
    THEN esiValue
    ELSE esiValue + extendedSymbols - sourceSymbols

ValidRecord(record) ==
    /\ record.k \in 1..MaxK
    /\ record.kPrime = ExpectedKPrime(record.k)
    /\ record.esi \in 0..MaxESI
    /\ record.isi = ExpectedISI(record.k, record.kPrime, record.esi)
    /\ record.isi \notin record.k..(record.kPrime - 1)

RangeStart(index) == IF index = 1 THEN 1 ELSE KPrimes[index - 1] + 1

BoundaryRecordExists(index, sourceSymbols, esiValue) ==
    \E recordIndex \in 1..Len(Trace) :
        LET record == Trace[recordIndex] IN
            /\ record.k = sourceSymbols
            /\ record.kPrime = KPrimes[index]
            /\ record.esi = esiValue
            /\ record.isi = ExpectedISI(sourceSymbols, KPrimes[index], esiValue)

CoverageComplete ==
    /\ Len(KPrimes) = 477
    /\ KPrimes[1] = 10
    /\ KPrimes[Len(KPrimes)] = 56403
    /\ \A index \in 1..Len(KPrimes) :
           \A sourceSymbols \in {RangeStart(index), KPrimes[index]} :
               \A esiValue \in {0, sourceSymbols - 1, sourceSymbols, MaxESI} :
                   BoundaryRecordExists(index, sourceSymbols, esiValue)

\* Evaluate the data-only coverage audit in the initial state. Subsequent
\* trace states cannot change Trace or KPrimes.
TraceCoverage == traceIndex # 0 \/ CoverageComplete

Init == traceIndex = 0

Advance ==
    /\ traceIndex < Len(Trace)
    /\ ValidRecord(Trace[traceIndex + 1])
    /\ traceIndex' = traceIndex + 1

Next == Advance

TraceSpec == Init /\ [][Next]_traceIndex /\ WF_traceIndex(Advance)
TypeOK == traceIndex \in 0..Len(Trace)
TraceComplete == <> (traceIndex = Len(Trace))

=====================================================================
