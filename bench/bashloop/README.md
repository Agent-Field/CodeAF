# bashloop — the arm bench for the bash-only task loop

Wave 3 of the bash-task-loop experiment
([DESIGN.md](../../docs/design/bash-task-loop/DESIGN.md)): the same briefs run
on both belts — arm A with `CODEAF_TASK_BELT=node` (the older belt),
arm B with `CODEAF_TASK_BELT=bash` — n replicates, cells interleaved so the
arms share the day. Every cell is graded by code: the fixture's own suite, a
mechanical diff, or a document's presence and coverage. No LLM judges
anything, per [bench/README.md](../README.md).

An arm is named by belt AND seats. `-seats one` (the default) runs the one
`-model` on every seat — arms `A-one` and `B-one`, the grid as it was before a
crew arm existed. `-seats crew` runs the five seat rows the machine's own
profile holds instead, arms `A-crew` and `B-crew` — so the grid can answer
whether a person on the tuned crew is better off beside the one-model
comparison. `-arms` selects any subset by name. A crew arm's rows quote every
model the run billed — the crew's worker, planner and auxiliary seats — which
is what its `models` column and `seats` column carry.

## Dry run first

```sh
go run ./bench/bashloop -dry-run          # every invocation: arm, cell, replicate, env, brief
go run ./bench/bashloop -mode pair -dry-run   # the same-question pair
go run ./bench/bashloop -dry-run -arms A-crew,B-crew   # a crew arm prints its five seat rows
```

The dry run composes every invocation — arm, cell, replicate, env, model,
wall, brief, and where its record lands — and executes nothing. A crew arm
adds its five seat rows, one per tier, named with the model and the rung the
machine's own profile filled it with. The dry run is the check
bench/README.md's honest-wiring rule asks for; the test `driver_test.go`
proves it (every invocation printed, the arms differing only in the belt
variable, the default grid unchanged, and a crew arm differing from its
twin only in the seat rows).

## Live grid

```sh
go run ./bench/bashloop                       # grid: 6 cells × 2 arms × 3 replicates
go run ./bench/bashloop -mode pair            # one brief, both arms, n=1 — the first smoke
go run ./bench/bashloop -cells c1,c3          # a subset
go run ./bench/bashloop -out bench-results/bashloop/manual-run
go run ./bench/bashloop -door do              # the same grid through the product's do door
go run ./bench/bashloop -seats crew            # both belts on the profile's crew seats
```

Each invocation seeds its own fixture repo from `fixtures/` (committed, so
the diff is measurable), works in a throwaway `CODEAF_HOME`, starts one task
through the engine's own task door, waits on the landing through the event
lane, and is read back off the disk the engine wrote: cost from the home's
usage ledger, steps from the family journals' `took` lines, the ending and
the wall from the task notice, the fan-out shape from the graph checkpoint.

## The second door

`-door do` runs the same cells, arms, fixtures, homes, readings and graders
through the product's adaptive-run door instead of the engine's task door:
the driver launches the built binary as a subprocess, the way
[bench/deepswe/run.sh](../deepswe/run.sh) drives it —

```sh
codeaf do -w <fixture> -model <pinned> -plan-model <pinned> \
  -db <home>/graph.db -keep -timeout <seconds> -yes-spend -json < <brief>
```

with `CODEAF_HOME` and `HOME` pointed at the throwaway home and the arm's
belt switch in the environment. The door's own exit ladder reads
0 done · 1 could not run · 2 ran and did not finish · 3 a limit stopped it ·
4 needed an answer · 124 the driver's wall fired first, and the ending word
in the row is that word. The dry run prints the exact invocation for this
door too. The door edits the fixture in place (`-w`), so the grader reads
the seeded fixture directory itself, and the numbers are read from the
door's own run store — the home's `graph.db`: cost and models from the
`usage` table (the door's per-call ledger), steps from the transcript's
finished tool calls, the diagnostics from the tool results, the family
shape from the `nodes` table. The belt must be seen armed in the do door's
own trace (a belted worker's shell hand is named `bash`, where the linear
subharness's own executor names it `sh`; the envelope rejections and the
plan-CLI's name are the other fingerprints) before its rows are worth
reading — the do-door report names that check.

## The cells

| cell | shape | graded by |
| --- | --- | --- |
| c1 | small fix — one failing suite | the suite green, tests untouched |
| c2 | feature — a new package + tests | suite green, package present, existing files untouched |
| c3 | multi-file refactor — reads-heavy | suite green, one copy of the math, public surface unchanged |
| c4 | report from a document corpus | REPORT.md present, four sections, grounded |
| c5 | wide job — four independent packages | suite green, children landed in the graph, INTEGRATION.md |
| c6 | kept-tool image | the image exists and is referenced |

The medians the design doc quotes — steps, cost, wall over the graded
passes, per arm and cell, plus the branch-only diagnostics — are printed by
the driver into the run's CSV (`bashloop.csv`). The summary table groups by
cell and arm name and adds a `models` column; the CSV carries the arm's
seats (`one` or `crew`) beside its belt letter. The verdict words stay in
the design document.

## Reading the rows

`cost_usd` is the home's own ledger sum — the provider's per-response
accounting, the only honest source on a shared key (bench/README.md's cost
rule). `unbilled` counts the rows whose receipt the provider never returned;
a cost that reads low says so in that column. `ending` is the engine's own
state word; a row with `ending=setup-failed` never started. Wall is the
node's own record when it kept one and the driver's clock otherwise, and
`wall_source` says which.
