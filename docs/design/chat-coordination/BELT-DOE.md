# The belt DOE: node vs bash, measured (2026-09-21)

This is the record of the measurement that decides which belt a task runs on
by default. It exists because the default moved twice in one day on the wrong
grounds, and a third move should rest on something a stranger can re-run.

## The question

`CODEAF_TASK_BELT` picks one of two engines for a `/task` and a `codeaf do`:

- **node** — the older engine. A task is a node of the session's own tree, its
  worker carries the conversation's tools, an auditor checks it, and nodes run
  one at a time.
- **bash** — the worker harness. A task is a run in the plan store, its worker
  carries one shell and the plan CLI, a check task is seated after each leaf,
  and up to `Slots` workers run at once.

Which should a person who has said nothing get?

## What was measured, and how

**Fixtures.** DeepSWE tasks from `~/src/deepswe-arms/corpus` on Spark: a real
repository pinned to a base commit, an objective, and an official test patch.
The arm's binary is handed the objective, works in a clone, and is graded by
applying the official patch on top of its work and running the fixture's own
suite twice: the base suite (nothing already working may break) and the new
tests (the feature must work). `pass` requires both to exit 0. This is the
strict form; a fix that breaks an existing test cannot pass.

**One binary.** Every arm ran `codeaf 619860cc6` (linux/arm64, built from
`santos/dev2`). Only the environment variable differed. `z-ai/glm-5.3-flash`
on every seat, so no arm could win by having a stronger model.

**Three batches.**

| batch | arms | fixtures | cells | launch |
| --- | --- | --- | --- | --- |
| b1 | node, bash, dsflat, crew, crewplan | 4 | 20 | 4 first, 16 twenty minutes later |
| b2 | node, bash | 7 | 14 | one batch |
| **d1** | node, bash | **8** | **32** | **one shuffled batch, both arms interleaved** |

`dsflat` seated `deepseek/deepseek-v4.1-flash` on every seat (the control that
stops crewing being credited for a stronger model). `crew` seated deepseek on
the plan seat and glm on the work seat. `crewplan` was `crew` plus the root
task seeded `RolePlan` so its first pass took the plan seat.

**What counts.** Cost is the sum of `cost` over `home/logs/calls.jsonl`,
skipping `phase: start` rows. It is NOT read from `result.json`, whose `usd`
and `calls` fields came back zero on a cell that had spent $1.21 over 703
calls. Quality is the scorer's `pass`, plus the count of new tests failed as a
finer measure. Wall is `wall.txt`, launch to scored.

**A suite that ran nothing is not a result.** `0 passed, 0 failed` is a suite
that never started (a collection error, a missing dependency, a container path
that does not exist here). Those cells are reported as `NO-RUN` and excluded
from every mean. Counting their zero failures as quality would credit work
that did not happen. Two fixtures (`adaptix`, `arcane`) produced this for both
arms in b2; `arcane`'s `test.sh` does `cd /app/backend` and cannot score
outside its container.

## Wall time is valid only in d1

b1's wall figures record the launch schedule: four cells ran at load 2.5 and
sixteen ran at load 38, and the four were node and bash. That biased timing
toward the arms it flattered and still showed node slower. b2 ran on an empty
box and is not comparable to b1. Neither is used for wall.

d1 launched all 32 cells in one shuffled batch, 16 per arm interleaved. Every
cell met the same contention, so no single wall figure is a clean measure of
how long the work takes, but the DIFFERENCE between arms is. Santosh's ruling,
2026-09-21: the difference due to shared CPU is acceptable; what is not
acceptable is one arm meeting a quiet box and the other a loaded one.

## Result

**d1, the shuffled DOE (wall valid):**

| arm | scored | passes | rate | mean $ | mean wall | tests missed |
| --- | --- | --- | --- | --- | --- | --- |
| **bash** | 14 | 3 | **21%** | **$0.239** | **1010s** | 15 |
| node | 13 | 2 | 15% | $0.384 | 2924s | 52 |

**Every batch pooled (cost and quality only):**

| arm | scored | passes | rate | mean $ |
| --- | --- | --- | --- | --- |
| bash | 23 | 5 | 22% | $0.310 |
| node | 22 | 5 | 23% | $0.376 |
| dsflat | 4 | 1 | 25% | $0.534 |
| crewplan | 4 | 1 | 25% | $0.835 |
| crew | 3 | 0 | 0% | $0.420 |

**Reading.** Quality is a tie: five passes each, one point apart on rate. Bash
is about a third cheaper per run and about three times faster. In the shuffled
DOE node is dominated on all three dimensions. Crewing in both forms and the
deepseek control are dominated on cost and quality by the flat arms.

**Why node is slower, from the code.** `defaultRunSlots = 4`
(`cmd/codeaf/do.go`) bounds how many run-engine workers run at once. The node
road has no slot concept: one `sync.WaitGroup`, no pool, no parallel dispatch.
37 samples from one node cell's log all read `N tasks pending, 1 running`. The
node belt also runs an auditor the bash belt does not
(`auditOn() = TaskAudit && !bashBeltAsked()`), which is one extra pass per node
against a 4x difference in how many nodes run at once.

**What crewing showed.** On three of four fixtures the `crew` arm never called
its plan-seat model at all: the root task was `RoleWork` until its first split,
so the pass that decides the split ran on the worker seat and the plan model
was configured, paid for, and never asked. `crewplan` fixed that seating and
the plan model was then called 15-27 times per run, producing 13-29 tasks
against a grid median of 9, at double the cost, for one pass more. The
inversion is real and the fix for it does not pay for itself at this scale.

## How the default moved, and why this record exists

- #1335 (2026-09-21 morning) made bash the default on instruction. Its own
  entry said the only prior comparison had gone the other way.
- #1340 (afternoon) reverted to node after b1+b2, on 3 passes to 2 across five
  fixtures. That was one task of difference, called too early, and this author
  said so in the PR body while merging it anyway.
- d1 (evening) reversed it. The restore PR cites this table.

The lesson is the one the repository already teaches about defaults: a default
carried by the absence of a value can move without any test objecting, and a
comparison whose control arm relies on that absence silently becomes a copy of
the arm it is compared against. Both `bench/bashloop` and the DeepSWE rig had
that defect and both now name a belt word on every arm.

## What this does not establish

- n is 13-14 scored cells per arm across 8 fixtures. A one-pass difference is
  noise. The cost and wall gaps are large enough to trust; the quality tie is
  the honest reading of the quality numbers.
- One model. A stronger work model may change the ratio.
- DeepSWE fixtures graded by test suites. This says nothing about codeaf-repo
  cells under a checker, which is a different workload; a peer nearly
  generalised it there and withdrew the inference.
- Wall under `Slots = 4`. The restore raises that to 16; the effect is
  unmeasured and should be measured on this same rig before anything else is
  built on it.

## Re-running it

```sh
# on Spark
cd ~/src/deepswe-arms
TAG=d2 WALL=3600 bash launch-doe.sh        # 32 cells, shuffled, both arms
python3 pareto.py                           # both tables and the frontier
python3 summarize-b1.py                     # per-cell detail from the files
```

`run-arm.sh` names a belt word on every arm and resolves each fixture's patch
by an explicit map; the glob it replaced handed one fixture another fixture's
tests.
