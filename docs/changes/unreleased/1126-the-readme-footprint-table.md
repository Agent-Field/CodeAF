---
kind: changed
title: the README's one-binary claim is a measurement, with the method and full table in the repository
pr: 1126
surface: [docs]
invalidates:
  - "The README asserted \"one small binary\" and offered nothing to check it against. It now carries a five-row footprint table — on disk, first interactive frame, memory during a turn, cold start, idle memory — including the three rows where another single-binary CLI beats this one, and links `docs/benchmarks/` for the method, the full named table and the script that reproduces all of it."
  - "`docs/benchmarks/` is new: `measure-cli.sh` measures any agent CLI given a name, a HOME and a command; `README.md` there states the four rules the numbers depend on (a real repository, an authenticated profile, PSS rather than RSS, and a quiet box the script does not try to make quiet) and the known limits, including that the idle-wakeup figure ranged 0 to 84/s across runs of one unchanged build and so belongs in no comparison."
  - "The existing `## Benchmarks` section, which promises task quality on held-out issues, is unchanged and unrelated: the footprint numbers say what the binary costs to install and run, and nothing about whether the work is any good."
---
