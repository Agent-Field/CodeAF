---
kind: fixed
title: Recover completed plan work through explicit acceptance
pr: 1911
surface: [chat, engine]
invalidates:
  - "A plan completed before restart could remain interrupted with no working acceptance door. Its saved work now returns as your call, preserving the copy, crew, spend and completion time."
  - "An unverified recovered plan could otherwise take an unattended default. Recovery waits for explicit acceptance or refusal and never replays the completed worker."
---

Existing task-page and conversation decisions can land the saved work and report
its actual commit. Ownership, branch and Stop checks remain enforced; failed
checkpoint or page-receipt writes report the actual outcome without claiming
that a completed merge was rolled back or safely recorded.

When the last checkpoint predates the final spending receipt, recovery restores
the run's price from its existing ledger without charging the conversation again.
