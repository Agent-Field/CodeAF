---
kind: internal
title: v0.2.0 is rolled up into CHANGELOG.md, and the removed-worker law reads the changelog as the record
pr: 1045
surface: [build, docs]
invalidates:
  - "`docs/changes/unreleased/` held every entry since v0.1.0 — 613 of them, the codeaf rename included. They are the `## v0.2.0` section of `CHANGELOG.md` now, and the folder holds only what has landed since; a memory of a change lives in the changelog's own fold, not in a loose file."
  - "`internal/exec`'s removed-worker law skipped `docs/changes/` as the record but not `CHANGELOG.md`, so the first roll-up went red on the laws for naming `swepro` in the very entries that record its removal. `CHANGELOG.md` is exempt as the same record; a live file that names a removed worker still fails the build."
---
