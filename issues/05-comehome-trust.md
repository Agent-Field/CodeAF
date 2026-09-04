# 05 · comeHome must never claim success when the branch didn't fasten
Pareto: TRUST (quality floor). Evidence: F31/F32 — orphaned commit edd9138 on no branch,
chat claimed "arrived as a merge result". Quality auditor: "bookkeeping integrity".

Fix: make the merge outcome a hard precondition of the user-facing "landed" notice.
If mergeIntoGround() did not land, the sentence MUST say so and name the kept branch.

Test: a stranded/conflicted merge yields a notice naming the branch and NOT claiming
success; a clean merge is unchanged.
