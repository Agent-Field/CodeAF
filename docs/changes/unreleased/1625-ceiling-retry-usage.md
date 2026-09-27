---
kind: fixed
title: paid reasoning-ceiling retries remain in session and budget accounting
pr: 1625
surface: [chat]
invalidates:
  - "An empty answer at the reasoning ceiling could be retried with its first paid usage omitted from session spend and task, day and seat budgets. Each discarded paid response is now accounted before retry, including when that retry fails or is cancelled, without inflating the final answer's context size or double-charging a full billing owner."
---

Fixes #1624. Observed live: a 718-input/320-output reasoning-only response cost
$0.0005994, but only its successful retry reached the session ledger.
