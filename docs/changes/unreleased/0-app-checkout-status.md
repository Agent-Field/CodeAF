---
kind: added
title: codeaf app reads the workspace checkout's status, and says it cannot land
pr: 0
surface: [engine]
invalidates:
  - "The app could list a conversation's unmerged task branches and diff one file, but had no account of the checkout itself: what is staged, edited, untracked, renamed or in conflict. checkout.status {agent, cursor?, limit?} now answers that, 100 entries a page (200 at most), from the same workspace files.diff and branches.list use."
  - "Landing work from the app has no producer, and now says so. Agent.Land merges the standing copy of a referred folder, not the task branches branches.list names, and binds no plan or revision; the app's consent questions approve a tool call, not a plan. Every checkout.status answers land {available: false, reason: no_producer} and no land method exists. A two-phase plan/confirm protocol is written in PROTOCOL.md as a proposal, not built."
---

Read-only git through the runner files.diff uses: no optional locks, no
fsmonitor, submodules by recorded commit only, one 3 s deadline for the call, at
most two reads at once (a third is `busy`). Every configured filter driver is
set empty before git compares a file, because `git status` otherwise runs a
repository's clean filter on a file whose stat data changed; a scratch-repo test
proves a marker-writing filter does not fire and fails when the override is
removed. A status longer than 4 MiB is `incomplete`, with no counts and no
cursor past the part read. A cursor is tied to the exact status it was issued
for and is refused, never re-aimed, once that changes.
