---
kind: changed
title: the node belt is the default again, on a measured comparison
pr: 1340
surface: [engine, chat]
invalidates:
  - "#1335 made the worker harness the belt a `/task` and a `codeaf do` run on, with `CODEAF_TASK_BELT` as the way out. It is the way in again: unset is the node belt, and the exact word `bash` is the only thing that reaches the harness."
  - "`node`, `legacy` and `off` were words that turned the harness off. They are not words any more, because there is nothing to turn off; the predicate is an equality against `bash` and every other value is the node belt."
---
#1335 moved the default on an instruction rather than a measurement, and its own
change entry said so. The measurement has now been made and it does not support
the move, so the default goes back.

**What was measured.** Two engines, one binary, five DeepSWE tasks that scored,
eleven runs per arm across two replicates, `z-ai/glm-5.3-flash` on every seat,
graded by each fixture's own suite with the official test patch applied on top.
The node belt took three tasks to the bash belt's two, missed seven tests to its
fourteen, and cost `$4.04` against `$4.14` — `$1.35` per passing task against
`$2.23`. Wall time was not compared: the cells shared a box, so their times
measure the launch schedule.

That is a one-task difference in outcome, and it is honest to say the quality
gap is suggestive rather than settled. The cost gap is the firmer half. Neither
points at the harness, and the earlier eighteen-task comparison in
`docs/design/bash-task-loop/REPORT.md` pointed the same way, so the default
returns to the belt that has never lost a comparison.

**The harness is not withdrawn.** `CODEAF_TASK_BELT=bash` reaches it exactly as
it did before #1335, every byte of it is still in the binary, and the run
engine, the plan store and the crew seats are untouched. What moves is which
belt a person who has said nothing gets.

**Two things #1335 got right are kept rather than reverted with it.**

The bench names a belt on both arms. `bench/bashloop` ran arm A with the
variable unset, which was correct only while unset meant the node belt; the
moment a default moves, an arm that relies on absence becomes a copy of the arm
it is compared against and the driver reports a difference of zero as a
measurement. Both arms name a word now, and the branch that handled an absent
one is gone. This is true whichever belt is default, which is the point.

The default itself is asserted. It had never been written down anywhere: it was
carried only by the absence of a value in other tests, so it could move without
one test in the tree saying a word. `bashbelt_default_test.go` now spells the
whole answer out, including that the match is exact and that a blank value is an
unset one.
