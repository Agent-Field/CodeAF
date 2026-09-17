# The do door — the six cells through `codeaf do`, and what stopped them

*2026-09-17. The companion report to the task-door grid — the same six cells,
the same two arms, the same pinned model (`deepseek/deepseek-v4-flash`), the
same fixtures, graders and reading discipline, driven through the product's
adaptive-run door instead of the engine's task door.*

**The headline first: the grid was not spent, because the arming gate failed.
On the do door the two arms are the same arm by every observable the door
itself keeps — no belt fingerprint appears in arm B's workers — and the brief's
own law says two identical arms are not a comparison. What ran is the arming
probe: a c1 pair and a c5 pair, both arms, n=1 each. Their rows are below. The
other four cells are recorded as not run, at the stage that stopped them.**

## The door, the flag, the dry run

The driver carries `-door task|do` (default `task`). Door `do` launches the
built binary as a subprocess, the rig's own shape
([bench/deepswe/run.sh](../deepswe/run.sh)):

```sh
codeaf do -w <fixture> -model deepseek/deepseek-v4-flash -plan-model deepseek/deepseek-v4-flash \
  -db <home>/graph.db -keep -timeout 1800s -yes-spend -json < <brief>
```

with `CODEAF_HOME` and `HOME` on the throwaway home and `CODEAF_TASK_BELT`
unset for arm A, `=bash` for arm B — the only byte that differs between the
arms. The dry run prints every invocation for both doors; the driver's tests
prove the two doors' blocks and that the arms differ only in the belt variable
(`driver_test.go`).

The door's exit ladder, in its own words: **0** done · **1** could-not-run ·
**2** ran-not-finished · **3** limit-stopped · **4** needed-answer · **124**
the driver's wall fired first. `stdout` is one JSON envelope; `stderr` is the
stream, kept as `run.log` beside each row.

## The rows that ran (the arming probe)

Read off the door's own store, the home's `graph.db` — cost and models from
`usage` (the door's per-call ledger, the same per-response accounting the task
door's `v3/usage.jsonl` keeps), steps from the transcript's finished tool calls
(the task door's `took` unit), the diagnostics from the tool results, the
family shape from `nodes`:

| cell | arm | n | pass | rate | ending (exit) | med steps | med $ | med wall | invalid | trunc | idiom | parts | changed |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| c1 | A | 1 | 1 | 1.00 | done (0) | 5 | $0.0053 | 110s | 0 | 0 | 0 | 0/0 | 29 |
| c1 | B | 1 | 1 | 1.00 | done (0) | 21 | $0.0188 | 265s | 0 | 0 | 0 | 0/0 | 31 |
| c5 | A | 1 | 0 | 0.00 | ran-not-finished (2) | 87 | $0.0400 | 487s | 0 | 0 | 0 | 0/0 | 43 |
| c5 | B | 1 | 1 | 1.00 | ran-not-finished (2) | 93 | $0.0696 | 922s | 0 | 0 | 0 | 2/3 | 45 |

Over the graded passes: arm A (n=1) — 5 steps · $0.0053 · 110s. Arm B (n=2) —
medians 57 steps · $0.0442 · 594s. Both arms graded pass on c1 (the c5 grader
passed arm B's work: suite green, 2 of 3 parts landed as their own work, one
node failed — `task-2-x2`, read straight off the `nodes` table). Arm A's c5 ran
the four packages green and still graded no: the family never left one pair of
hands, and the run ended exit 2. The wall column is the envelope's own seconds
(`wall_source=node`); the driver's backstop never fired.

The cells not run, with the stage that stopped them:

| cell | arm | reading |
| --- | --- | --- |
| c2, c3, c4, c6 | A and B | not run — the grid was withheld at the belt-arming gate (below); spending four cells on one arm would have been the wrong answer the brief names |

## The arming gate, and how it was checked

The fix this branch carries (`36a689926`) arms the belt on the do door's run
leaves: `orchestrate.go`'s `newChild` now builds its `Config` with
`bashBelt: bashBeltAsked()` (`InTask` was already true, and
`Config.mayBashBelt` is `InTask && bashBelt`). The task door's workers take the
same flag at `task_run.go`'s worker construction. That wiring is real, and it
is where the task door's arming is enough — its workers do the work themselves.

The do door is built differently: a run leaf's file work is done by the
subharness's own executor, not by the leaf agent's own hands. So the probe
looked for the belt in the three places the brief names, using only the door's
own records:

1. **The worker's own trace** — the store's `transcript` table, per node. Both
   arms' workers called the same executor toolset: c5 arm A `sh`×73, `edit`×5,
   `read`×4, `share`×2, `write`×3; c5 arm B `sh`×80, `edit`×6, `read`×2,
   `share`×1, `write`×4 (c1 the same shape, smaller). Arm B carries **zero**
   calls of a bash-belt hand (`bash` — the belt's own shell, `branchBash` — is
   a different name than the linear executor's `sh`), and the belt's tool
   composition (`bashBelt()`: branch bash, document, jobs, manual)
   appears nowhere.
2. **The composed page** — per-node first-turn `prompt_tokens` off
   `usage_turns`: arm A 4640/6468/6667, arm B 4663/6714/6552/6771 (c1's three
   and four nodes). Within a percent either way. The belt swaps whole pages
   (`bashworker.md` in place of the fan-out page, belt facts into the frame);
   a page swap that moves nothing measurable is not there.
3. **The belt's own diagnostics** — the one-action envelope's rejections
   ("no action executed") and the plan CLI's name (`plandb`, which the
   bashworker page teaches): **zero** in every transcript body and event
   payload on both arms. On the task door arm B's rows carried 1–17 invalid
   actions each; here the envelope never armed, which is itself the fingerprint
   of absence.

One behavioral divergence between the arms did show — c5 arm B spliced a
family (2 of 3 root children done) where arm A stayed single-handed and graded
no — but nothing in that run's trace names the belt, and at n=1 a family-shape
difference is run noise as far as the switch is concerned. The belt cannot be
credited with it and the grid cannot be built on it.

**Verdict: the belt does not reach the do door's workers.** The flag arms the
leaf's `Config` and stops there: the leaf's file work is executed by the
subharness executor, which is built without the belt, and the leaf's own prompt
is composed without the belt's pages. This is the DeepSWE finding one seam
deeper — the switch now reaches the process *and* the leaf `Config`, and still
arms nothing the door's records can see. `planSeed`'s store never seeds on this
door either (it runs from `TaskGraph.admit`; the do door registers and does not
admit), so the plan-CLI path has no purchase here either.

## The two sentences the brief asks for

The do door does not change the task-door verdict, because it cannot re-measure
it: with the belt invisible on this door, both arms are one arm, and the honest
do-door reading is "unmeasured", not "agree" or "disagree". The doors diverge
structurally — the task door builds workers at `newTaskAgentOn` whose toolbelt
and pages the belt composes, while the do door builds run leaves at
`orchestrate.go`'s `newChild` and hands the actual work to the subharness's own
executor, so the belt stops at the leaf's `Config` and nothing downstream of
that seam ever reads it.

## Where the readings live

Raw, beside this report, in `bench-results/bashloop-do-door/` — per results
root (`bashloop-do-pair2` = the c1 pair, `bashloop-do-c5probe` = the c5 probe,
`bashloop-do-pair` = the first pair, run before the store reader existed, whose
rows read hollow: `steps=0 $0.0000`, the defect that forced the reader): the
driver's `bashloop.csv`, each run's JSON envelope (`do.json`), its stderr
stream (`run.log`), and the home's own `graph.db` and `logs/calls.jsonl`.
On the Spark the full run directories (fixtures and all) are
`/home/santosh/work/bashloop-do/bench-results/`.

The first pair also earned the one driver change this report rides with: the
do door keeps its numbers in the store, not in the `v3` ledger-and-journals
layout the task door writes, so `door.go` reads the store read-only (same
helpers, same units, one new reader) and the row now carries the family shape
the c5 grader needs.
