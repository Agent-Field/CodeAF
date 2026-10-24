---
kind: changed
title: Factory work items are issues only
pr: 0
surface: [engine, chat, docs]
---

The factory imports and runs issues only. Pull requests no longer appear as
independent jobs or pipeline choices. An issue still tracks its published PR,
checks, reviews, merge status and CI repair follow-ups.

On startup, old standalone PR jobs are permanently erased from the local floor
and worker mirrors, together with their factory conversations, local worktrees,
generated branches and event history. Issue jobs and their PR objects are kept.
The cleanup makes no GitHub changes. IDs stay monotonic after removal.

Invalidates: PRs were previously imported as review jobs with their own default
step sequence; standalone PR and CI pipelines are no longer offered or runnable.
