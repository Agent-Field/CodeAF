---
kind: fixed
title: an exhausted leaf continues the plan it has instead of a second full planning pass
pr: 1143
surface: [resident, chat]
invalidates:
  - "Every remainder of an exhausted leaf was planned from scratch: the overrun splice called plan.Build again on each round, paying a fresh planning call and re-emitting the planner's phases over a job that already had a plan. It now continues the plan it drew, whenever the lineage has recorded turns and the job still holds a plan, and falls back to the full re-plan only when either is missing."
  - "The shape a job was planned with could be replaced by a one-step shape after its first overrun, because the re-plan overwrote what the job already carried. The plan is retained across an overrun round now; a five-step plan stays a five-step plan."
  - "Nothing told the planner why a round was being planned, so a planner could not tell an exhausted leaf from a reviewer's finding. The round's reason now rides into the planning call as resident.GrowthReasonFrom, the same overrun/gap/cooperative vocabulary the growth journal already uses."
---

Only an overrun round continues. The delivery gate's gap round buys work a
reviewer named and a worker's cooperative split is that worker's own division,
so both still buy the full planning pass; so does a cold start and a job that
never earned a plan.
