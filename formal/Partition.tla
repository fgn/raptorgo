-------------------------- MODULE Partition --------------------------
EXTENDS Integers, Naturals

CONSTANTS IMax, JMax

ASSUME /\ IMax \in Nat
       /\ JMax \in Nat \ {0}

VARIABLES i, j, il, is, jl, js

vars == <<i, j, il, is, jl, js>>

Init ==
    /\ i \in 0..IMax
    /\ j \in 1..JMax
    /\ is = i \div j
    /\ jl = i % j
    /\ il = IF jl = 0 THEN is ELSE is + 1
    /\ js = j - jl

Next == UNCHANGED vars

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ i \in Nat
    /\ j \in Nat \ {0}
    /\ il \in Nat
    /\ is \in Nat
    /\ jl \in Nat
    /\ js \in Nat

ConservesTotal == il * jl + is * js = i
ConservesParts == jl + js = j
Balanced == il = is \/ il = is + 1
LargeBlocksFirstCountIsRemainder == jl = i % j
PartSize(n) == IF n < jl THEN il ELSE is
LargeBlocksPrecedeSmall ==
    \A n \in 0..(j - 1) :
        /\ n < jl => PartSize(n) = il
        /\ n >= jl => PartSize(n) = is

=====================================================================
