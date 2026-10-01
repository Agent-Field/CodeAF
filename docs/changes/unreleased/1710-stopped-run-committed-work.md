---
kind: fixed
title: a stopped run names the work its worker already committed
pr: 1710
surface: [chat, engine, docs]
invalidates:
  - "Stopping a run whose worker had already committed its work said `stopped · it had changed nothing` and named no branch, although the branch held the commits. The stop receipt and row now count work committed on the task branch since its base as well as uncommitted edits, and name the branch; a run that truly changed nothing still says so."
---
