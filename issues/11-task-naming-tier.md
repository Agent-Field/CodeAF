# 11 · Route task-naming off the auditor tier
Pareto: COST down, quality neutral. Evidence: F38 — the TierHigh auditor (qwen) made
`task naming` calls.

Fix: naming a task does not need the dear auditor model; route it to low/reflex.

NOTE: this folds into issue 01 (same fix and test); kept as its own card only so the
naming-vs-audit split is visible. Close both together.
