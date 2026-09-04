# 10 · First-class task continuation
Pareto: COST + WALL TIME down, UX up. Evidence: F23/F25 — "continue task 4" re-derived
a fresh brief + new worktree instead of resuming.

Fix: add a `continue <id>` verb that re-arms the SAME node with its brief + auditor
evidence, reusing the persisted worktree/checkpoint/journal. The seams exist; this is an
addition, not a rearchitecture.

Test: continuing a failed task resumes its context and worktree, and does not call
propose_task for a new node.
