---
kind: fixed
title: the canary measures the door's change against the anchor base, so a committed fix is not zero files
pr: 519
surface: [build, chat, engine]
invalidates:
  - "A canary row with `files 0` and an empty `work.patch` meant the door changed nothing. Both doors commit their work on `main` inside the cell's work tree, and the judge diffed against HEAD, so a committed fix read as nothing while its tests were graded green on the tree (the packaging chat pair on #407, 2026-09-03 02:52Z, both cells). The diff, file count and patch are now taken against the base sha, the commits above it are counted (`commits` in rows.csv) and listed in `work.commits`, and `CANARY_REGRADE=1` backfills finished cells. Tests verdicts were never affected."
---
