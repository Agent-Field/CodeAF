---
kind: fixed
title: "a belt check runs on the crew checking seat, not the thinking seat"
pr: 1207
surface: [chat]
invalidates:
  - "Under CODEAF_TASK_BELT=bash a review check resolved to the mastermind (thinking) tier, so the crew checker model was billed for but never asked to check. A check now takes the careful work tier where the checker is seated, and the work, fix, and plan seats are unchanged."
---

SeatFor maps a check to the careful work tier (config.ModelTierHigh) rather than
the mastermind tier, so a review round runs on the model the crew seats to check.
The work, fix, plan, and probe seats are unchanged, and the check seat stays
read-only.

The check seat is resolved at the door: `--check-model`, then
`CODEAF_CHECK_MODEL`, then a plan seat pinned by `--plan-model` or
`CODEAF_PLAN_MODEL`, then the crew's careful row. A pinned run sees no third
model from the profile. run.Seats carries the door's Check seat and CrewFactory
seats the careful tier on it. `CHECKER_CAP_USD=2` is live, so a cell can end
on the checker cap.
