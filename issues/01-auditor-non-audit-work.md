# 01 · Stop the auditor doing non-audit work
Pareto: COST down, quality neutral. Evidence: F38/F40.

The TierHigh auditor (qwen3.8-27b) is ~67% of total spend. A slice is pure waste:
- 4+ calls were literally `task naming` (see calls.jsonl 15:26).
- Several were self-refusals: the auditor refuses its own "ONE verification command, no
  shell composition" contract when the command contains `>`, then retries paying again.

Fix:
- Route task-naming OFF the auditor tier (to low/reflex).
- Pre-validate the auditor's verification command so it never emits a command its own
  contract rejects; or relax the contract to allow a single pipeline.

Test: a task whose audit would previously have triggered a naming/self-refusal call
completes with zero auditor-tier calls for those purposes.
