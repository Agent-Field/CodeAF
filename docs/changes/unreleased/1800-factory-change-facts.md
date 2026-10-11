---
kind: added
title: Factory jobs report changed files, branch and explicit check counts
pr: 1800
surface: [chat, engine, docs]
invalidates:
  - "Only PR jobs exposed changed files, through a limited GitHub detail list. Any job now carries its holding machine's complete worktree change, diff stat and branch after completed steps."
  - "The job wire did not expose test counts or a PR's draft flag. Check steps now report explicit outcome counts when their output supplies them, and source PRs and published receipts retain the draft flag."
---

V7 adds optional change facts without bumping Contract 1. Committing work during
a step does not remove it from the comparison. Unknown or zero facts are omitted;
an exit code or package summary never invents test counts. Fixture, codec,
real-worktree runner and worker HTTP tests cover the new fields.
