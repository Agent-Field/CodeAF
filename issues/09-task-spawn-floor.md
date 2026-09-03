# 09 · Gate task-spawn so trivial asks never convert
Pareto: COST + WALL TIME down, quality held. Evidence: F16/F26 — "commit everything"
became task 5 and the commit never happened. route_judge already proved the request
alone can't judge work; checkpoint already reads the work.

Fix: a hard structural floor — if the ask maps to a single command / known-trivial verb
set (commit, undo, a one-file edit), the checkpoint never converts to a task. Enforce in
code, not via more prompt.

Test: "commit", "undo", "fix this one line" never spawn a task; a genuine multi-part ask
still does.
