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
