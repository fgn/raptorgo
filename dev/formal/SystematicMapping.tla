---------------------- MODULE SystematicMapping ----------------------
EXTENDS Integers, Naturals, Sequences, FiniteSets

CONSTANT MaxESI

KPrimes == <<10, 12, 18, 20>>

ASSUME /\ Len(KPrimes) > 0
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

VARIABLES k, esi, kPrime, isi

vars == <<k, esi, kPrime, isi>>

Init ==
    /\ k \in 1..MaxK
    /\ esi \in 0..MaxESI
    /\ kPrime = ExpectedKPrime(k)
    /\ isi = ExpectedISI(k, kPrime, esi)

Next == UNCHANGED vars
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ k \in 1..MaxK
    /\ esi \in 0..MaxESI
    /\ kPrime \in KPrimeValues
    /\ isi \in Nat

LeastEligible ==
    /\ kPrime >= k
    /\ \A other \in KPrimeValues : other >= k => kPrime <= other

SourceIdentity == esi < k => isi = esi
RepairSkipsPadding == esi >= k => isi = esi + kPrime - k
NoESIMapsToPadding == isi \notin k..(kPrime - 1)

=====================================================================
