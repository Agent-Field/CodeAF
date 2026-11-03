---
kind: fixed
title: Factory jobs count completed calls discarded by a provider race
pr: 0
surface: [engine, factory, chat]
invalidates:
  - "Was true: when both provider racers completed, the discarded answer could be missing from Spending and the job budget. Is true now: both completed answers are counted once against the same job."
---

When a slow request races another provider, the job includes the cost of both
completed calls, even though only one answer is used. Spending and the job header
retain the same charges. No app wire methods or limits change.
