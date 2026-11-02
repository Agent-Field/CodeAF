---
kind: fixed
title: codeaf preserves stage budget questions and bounded knowledge batches
pr: 0
surface: [engine]
invalidates:
  - "A completed stage was exempt from every budget stop instead of only a crossing caused by optional memory."
  - "An ordinary forced knowledge Flush drained additional batches instead of retaining its single-batch limit."
---

A step's own model and tool spending still asks at the cap and rejects an early
completion report when that spending stops it. Memory charges remain included
in the job total and cannot revoke completed work when only memory crosses the
cap. The next step asks before starting; the last step finishes at the true
spend without asking for unused budget.

Ordinary knowledge compilation processes one bounded batch per Flush. Owned job
turns continue draining their finite captured snapshot.
