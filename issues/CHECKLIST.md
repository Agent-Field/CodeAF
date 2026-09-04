# Aforge Pareto fix checklist (local branch `santosh/performance-update`)

Work locally; merge each fix into THIS branch; tick the box when it lands with a test.
Priority is by Pareto impact — the biggest quality-neutral cut to cost/wall-time first,
then trust/correctness, then deeper architecture. Do not reorder without a reason.

## Wave 1 — quality-neutral cost & wall-time cuts (do first, cheapest, no behaviour risk)
- [x] 01 [stop auditor doing non-audit work](01-auditor-non-audit-work.md) — cost
- [x] 02 [fire the hedge on a stall](02-hedge-on-stall.md) — wall time
- [x] 03 [dedup the interrupt fan-out](03-interrupt-fanout.md) — wall time + cost
- [x] 04 [compact old tool results mid-turn](04-compact-tool-results.md) — cost + wall time

## Wave 2 — trust & correctness (the Pareto floor: quality must be TRUE)
- [x] 05 [comeHome must never claim success when the branch didn't fasten](05-comehome-trust.md)
- [x] 06 [reject a mojibake hedge winner before persisting](06-hedge-mojibake.md)
- [ ] 07 [stop deny-by-default approval timer](07-approval-timer.md)
- [ ] 08 [single writer per file set (chat vs tasks)](08-single-writer.md)

## Wave 3 — architecture (real leverage, higher risk, do after 1–2 are green)
- [ ] 09 [gate task-spawn so trivial asks never convert](09-task-spawn-floor.md)
- [ ] 10 [first-class task continuation (`continue <id>`)](10-task-continue.md)
- [ ] 11 [route task-naming off the auditor tier](11-task-naming-tier.md)  *(folds into 01)*

## Wave 4 — hygiene / DX
- [ ] 12 [raise debug call-log cap when bodies are on](12-calllog-cap.md)
- [ ] 13 [default first-run model must answer](13-first-run-model.md)

See audit-notes/perf-debug-2026-09-03.md for the evidence behind each (F-numbers).
