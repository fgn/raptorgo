---------------- MODULE PlannerProgress ----------------
EXTENDS FiniteSets

CONSTANTS Rows, Columns, ForgedRemoval

VARIABLES incidence, activeRows, activeColumns

vars == <<incidence, activeRows, activeColumns>>

Init ==
    /\ incidence \in [Rows -> SUBSET Columns]
    /\ \A column \in Columns:
          \E row \in Rows: column \in incidence[row]
    /\ activeRows = Rows
    /\ activeColumns = Columns

Remove(row) ==
    /\ row \in activeRows
    /\ incidence[row] \intersect activeColumns # {}
    /\ activeRows' = activeRows \ {row}
    /\ activeColumns' =
          IF ForgedRemoval
          THEN activeColumns
          ELSE activeColumns \ incidence[row]
    /\ UNCHANGED incidence

Next == \E row \in activeRows: Remove(row)

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ incidence \in [Rows -> SUBSET Columns]
    /\ activeRows \subseteq Rows
    /\ activeColumns \subseteq Columns

NoPlannerStall ==
    \A column \in activeColumns:
        \E row \in activeRows: column \in incidence[row]

=====================================================================
