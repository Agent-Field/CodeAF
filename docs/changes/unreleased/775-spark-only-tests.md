---
kind: internal
title: Full acceptance tests run on Spark rather than the laptop
pr: 775
surface: [build, docs]
invalidates:
  - "The test commands in the session instructions did not name their execution host. Full suites, full affected-package runs, and final end-to-end acceptance must now run on Spark, never on the laptop."
---

Keep the remote job ID, exact tested revision or snapshot, and results. If Spark
is unavailable, report the blocker instead of falling back to a full local run.
The owner’s global Claude and Codex instructions carry the same standing policy.
