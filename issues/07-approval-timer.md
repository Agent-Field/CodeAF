# 07 · Stop deny-by-default approval timer
Pareto: TRUST + UX. Evidence: F41 — a hidden ~10s timeout records "denied" and killed a
user's proposed task.

Fix: the timeout default must be pause/keep-waiting, never an implicit "no". Surface
the approval mode explicitly.

Test: an unanswered approval does not record a denial and does not cancel work.
