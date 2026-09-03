---
kind: changed
title: the canary marks column counts an unreachable or absent mark reader, and carry counts the steward alone
pr: 509
surface: [build, chat]
invalidates:
  - "The canary `marks` column (#505) was described as the post-task check rounds that failed, and the #407 bisect note read every `mark` row saying `failed` as the check refusing green work. A `mark` row is the mid-turn read of whether a long turn should become a task (decisions `split`, `continue`, `waiting`, `wrote`); `failed` meant the configured mark reader could not be reached under `--one-model` (#443, fixed by #449), and `no reader` (one row per session, #507) means the install has no mark model. The column now counts only those two. The check of the work is the steward row, whose `carry on` decisions are the `carry` column; the conclusion that the carry-on loop is baseline behaviour at 35c1a79e is unchanged."
  - "The canary read `mark` and `principal` payloads as Python-repr strings only. Binaries from #449 on write them as JSON objects; the rig reads both, and every chat row now records `steward_last` (`carry on`, `done` or `stop`) in rows.csv so a #507 cell that ends can be checked for a final `done` or `stop` with no carry-on after it."
---
