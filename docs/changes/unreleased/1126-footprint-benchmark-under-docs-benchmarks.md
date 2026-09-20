---
kind: added
title: a footprint benchmark under docs/benchmarks measures what the binary costs to install and run
pr: 1126
surface: [docs]
invalidates:
  - "`docs/benchmarks/performance/` is new: `measure-cli.sh` measures any agent CLI given a name, a HOME and a command; `README.md` there states the four rules the numbers depend on (a real repository, an authenticated profile, PSS rather than RSS, and a quiet box the script does not try to make quiet) and the known limits, including that the idle-wakeup figure ranged 0 to 84/s across runs of one unchanged build and so belongs in no comparison."
  - "`results-2026-09-17.md` is the full named table for that day — on disk, first interactive frame, memory during a turn, cold start, idle memory — including the rows where another single-binary CLI beats this one. The top-level README is unchanged; where and whether these figures appear there is a separate decision."
  - "The existing `## Benchmarks` section, which promises task quality on held-out issues, is unchanged and unrelated: the footprint numbers say what the binary costs to install and run, and nothing about whether the work is any good."
---
