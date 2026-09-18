# The worker harness — measured against the shipped engine

*2026-09-17. The record of what the run engine (this branch) cost, how long it
took, and what it passed, beside the shipped engine it replaces
(`santos/dev`). Every figure below is a median over the runs named in its
section; the per-run transcripts and the raw logs live under `runs/`.*

Two questions are answered here, and the second is the one that decided the
work: the six calibrated cells say whether the loop is cheaper and faster at an
equal pass rate, and the repository feature tasks say whether that survives an
objective that a person actually wrote. The first question has a table; the
second is still being read.

## Six-cell grid, run engine under /task, n=3, medians

One seat per arm, the same model on both, a $5 cap per run, on the shared build
box. `shipped` is the `santos/dev` engine; `harness` is this branch. n=3, and
every figure is the median of the three.

| cell | shipped $ | shipped wall | harness $ | harness wall | pass shipped / harness |
| --- | --- | --- | --- | --- | --- |
| c1 | 0.0075 | 14s | 0.0057 | 26s | 3/3 · 3/3 |
| c2 | 0.0299 | 273s | 0.0102 | 66s | 3/3 · 3/3 |
| c3 | 0.0140 | 114s | 0.0042 | 43s | 3/3 · 3/3 |
| c4 | 0.0105 | 98s | 0.0037 | 19s | 3/3 · 3/3 |
| c5 | 0.0477 | 359s | 0.0230 | 131s | 3/3 · 2/3 |
| c6 | 0.0259 | 231s | 0.0056 | 48s | 3/3 · 3/3 |

Median output tokens per call: **476 shipped, 217 harness**. Model
`z-ai/glm-5.3-flash`, one seat per arm, cost cap $5, on the shared build box.

The harness is cheaper on all six cells and faster on five; the one cell it
loses, c1, is the cheapest and the shortest and the difference is tens of
seconds. It also drops a cell — c5 passes 2/3 where the shipped engine passes
3/3 — which is why the grid alone is not the readiness bar.

## Repository feature tasks, 45-minute wall, pinned base commit, same model

Each task was handed a repository at a pinned base commit and a feature to
build, with a 45-minute wall and the same model on both arms.

- **bandit, incremental cache** (base `765f00d3`). **harness PASS**: 358
  existing + 89 new tests green, $0.44, 41m, 195 calls. **shipped FAIL**: hit
  the wall at 45m with 83/89 new tests and the task still running; its log shows
  the engine re-choosing the shape and resuming the same task from recorded
  turns from minute 32 onward (log `runs/ds1-dev-p1/run.log`, tests
  `test-new.log`).
- **awilix, async container initialization** (base `82ac179c`). **harness
  23/24** ($0.33, 37m): one case missed, `scope.initialize()` without a parent
  initialize. **shipped FAIL**: the worker left `src/awilix.ts` with an
  unterminated import block (a parse error at line 10) and the build never ran
  again before the wall
  (`runs/awilix-async-container-initialization-dev-p1/test-base.log`).
- **bandit, interprocedural taint** (base `b46fa3a2`). **harness**: ended after
  6m with 19/85, because its plan writes went to a store the run does not read —
  fixed by the PATH change in this branch, rerun pending. **shipped**: in
  progress.
- **cattrs, partial structuring recovery**. harness: in progress. shipped: in
  progress.

## What the corpus runs exposed and what changed

- The worker belt refused `git clone` in an empty workspace. The task's git
  guard now reads the workspace root: where the folder is not inside a git work
  tree there is no work of the person's to protect, so every git verb — clone,
  checkout, fetch, pull — passes, and inside a repository the refusals stand
  (`internal/session/taskgit.go`).
- A parent was not woken when its children landed. The plan pulse now fires at a
  bash-belt worker's turn end and when a fan slot goes back, so a task that
  became ready — a parent waiting on its children among them — is dispatched
  instead of waiting for some worker's next bash call
  (`internal/session/plandb_plan.go`).
- The run's `plandb` was not on the worker's PATH. Every belt command is now
  prefixed with the run's armed shim directory, so the worker's `plandb` is the
  run's own CLI and the process environment is never touched
  (`internal/session/plandb_plan.go` `planBashPrefix`, armed in
  `internal/session/bashbelt.go`).

## Reruns after wave 7

*2026-09-17. The same tasks as the corpus runs above, rerun after the fix named
beside each one — the same pinned model on both arms, the same pinned base
commit, and the same 45-minute wall.*

- **cattrs, partial structuring recovery** — **PASS** after the work-seat
  effort fix: 93 calls, reasoning tokens 43,896 → 18,018, completion tokens
  69,618 → 39,353, prompt tokens 4.70M → 3.33M, and wall 20m → 14m. THE
  DOLLARS ARE NOT COMPARABLE ACROSS THESE TWO RUNS — different serving
  providers and different cache rates — so only the calls, the tokens and the
  wall are read beside each other, and the cost is not.
- **awilix, async container initialization** — **23/24** after the parent wake:
  8m for $0.19, against 23/24 in 37m for $0.33 before it. The root integrates
  its children's landings now rather than being closed on them, which is where
  the 29 minutes went. The one case still missed is `scope.initialize()`
  without a parent initialize.
- **bandit, interprocedural taint** — after the PATH fix the plan is written to
  the run's own store (seven tasks), where before it went to a store the run
  never reads. The run still ended early: a reply with no action ended a task,
  and the fix for that is in flight.

## Reruns on 3585556b4 — explicit endings, one wake per wait, the named seats

Same corpus, same pinned model on both arms, 45-minute wall, one run each.
Every harness root finished cleanly (exit 0) and no call went to a seat the
flags did not name. `shipped` is trunk 7231347eb.

| task | harness | shipped |
| --- | --- | --- |
| bandit, incremental cache | 88/89, $0.15, 11m, 142 calls | 82/89, $0.24, 45m (wall) |
| bandit, interprocedural taint | **PASS** 85/85, $0.23, 13m, 231 calls | 83/85, $0.19, 41m |
| awilix, async container initialization | 23/24, $0.07, 7m, 43 calls | 23/24, $0.27, 45m (wall) |
| cattrs, partial structuring recovery | 66/69, $0.03, 3m, 32 calls | **PASS**, $0.21, 45m |

Cost and wall are three to ten times lower on every task. Pass rate is one in
four on both arms and not on the same task: the harness lost bandit cache
(passed before the prompt diet, one warm-cache CLI case now) and cattrs (the
root did the work alone in 31 steps and never read `refine()` against the
sentence that said to preserve fields). Every miss is a result nobody read
against the ask, which is the review round (`Limits.ReviewRound`, off on both
doors today).

## Reruns on a02845661 — the review round on both doors

Same corpus, same pinned model, one run each; every leaf's finish now gets a
check task seated on the plan model, and a `does not hold` finding opens one
`fix:` task. Walls are the `do` time; the bandit pair's scoring was redone at
normal load after a box-wide spike.

| task | harness, review round | harness, no review round | shipped |
| --- | --- | --- | --- |
| bandit, incremental cache | **PASS** 89/89, $0.19, 22m | 88/89, $0.15, 11m | 82/89, $0.24, 45m (wall) |
| bandit, interprocedural taint | 84/85, $0.18, 26m | **PASS** 85/85, $0.23, 13m | 83/85, $0.19, 41m |
| awilix, async container initialization | 23/24, $0.06, 6m | 23/24, $0.07, 7m | 23/24, $0.27, 45m (wall) |
| cattrs, partial structuring recovery | **PASS** 69/69, $0.08, 8m | 66/69, $0.03, 3m | **PASS**, $0.21, 45m |

The review round bought back cattrs (the root's own work is now checked before
completion) and bandit cache, at about twice the cost and wall of the bare loop
and still a third to a tenth of the shipped engine's. Bandit taint lost one
case (a non-spec sanitizer) that the bare loop had passed: the fix task the
check opened on the taint engine changed a behaviour the check did not cover.
On this corpus the branch is now at or above the shipped engine on every task
on pass count, and below it on cost and wall on every task.

The bandit cache miss on the bare loop was read step by step against the
earlier PASS (a read-only cell, $0.71): model variance, not the prompt diet.
Both workers read "results empty" for `--warm-cache`, one fell through to the
JSON formatter and one exited before it, and each wrote a test ratifying its
own reading; the root of the missing run even saw the JSON decode error and
finished anyway. That is the shape the review round reads against the ask.

