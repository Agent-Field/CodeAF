---
kind: fixed
title: codeaf step costs include every round once
pr: 2294
surface: [engine]
invalidates:
  - "Step cost previously showed only the last round, and resumed live meters reported only new spending while fallback chats reported their cumulative chat cost. Step reports now mean cumulative spending for one logical round; a step sums its rounds, replacing repeated reports rather than charging them again."
---

Live stage receipts retain their round attribution before a final report, including
resumed work and delegated calls. Retries retain step spending, while a new run starts from zero. Coalesced worker
snapshots carry accumulated step cost in the existing `pipeline[].cost` field;
the latest result still carries its own round cost. Old pending jobs attribute
their saved result to its completed round so new spending reaches the job budget.
Worker transport keeps its existing wire fields.
The job total also includes its manager and other owned work outside steps, so it
can exceed the sum of step costs. Historical saved jobs remain readable without a
migration; spending from earlier rounds that was never saved cannot be recovered.
