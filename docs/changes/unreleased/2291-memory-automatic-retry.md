---
kind: fixed
title: Retry ordinary memory builds automatically after failure
pr: 2291
surface: [chat, docs]
invalidates:
  - "Ordinary memory builds previously waited for new sources or an explicit Build after an identical failed input. They now retry automatically while retaining completed progress."
---

Ordinary memory builds again report `Completed progress is retained; retrying
automatically.` and retry unchanged input. Memory off still prevents compilation.
Factory sources remain excluded from independent background compilation, with
failures, ownership and usage accounting handled by the owning job.

Documentation follows dev wording while preserving guidance for capabilities
retained in this tree, including desktop reply suggestions, question Hold,
conversation commands, service cleanup and local saving recovery.
