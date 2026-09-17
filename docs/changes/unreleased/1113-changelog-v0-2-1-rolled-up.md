---
kind: internal
title: v0.2.1 is rolled up into CHANGELOG.md
pr: 1113
surface: [build, docs]
invalidates:
  - "`docs/changes/unreleased/` held every entry since the v0.2.0 roll-up — 33 of them, from the alt+k switcher to the update-download clock. They are the `## v0.2.1` section of `CHANGELOG.md` now, and the folder holds only what has landed since; stable v0.2.1's release notes are that section."
  - "Five of those entries (#1045, #1047, #1050, #1051, #1056) landed after the v0.2.0 roll-up commit and before the v0.2.0 tag, so they shipped in v0.2.0 and read under v0.2.1. The roll tool reads the folder, not the tags; cutting a tag from the roll-up commit itself is what keeps a section and a release the same set."
---
A stable release takes its notes from its own `## <tag>` section, so the roll-up
is the release notes and has to be on the commit that is tagged. It lands on
`dev` and is promoted like anything else, never written onto a pointer.
