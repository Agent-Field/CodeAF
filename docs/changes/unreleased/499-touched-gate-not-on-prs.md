---
kind: changed
title: the touched-packages job no longer runs on pull requests
pr: 499
surface: [build]
invalidates:
  - "Every PR into `dev` carried a `touched packages` check that ran the full suites of the packages the change touched. It no longer runs on PRs; the PR gate is `check` alone, and `touched packages` still runs on every push to `dev` and on manual dispatch."
---

The job never blocked a merge — it was `continue-on-error` and only `check` is
required — but it still burned up to ~15 minutes per PR, because most changes
touch `internal/tui3` (~500s on a free runner). The nightly ledger shows its
per-PR red signal was not being acted on, so the cost bought nothing. It still
runs on the merge to `dev`, which is where its result is actually read; a red
there still means what it always meant.
