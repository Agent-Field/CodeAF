# bashloop — the two-arm bench for the bash-only task loop

Wave 3 of the bash-task-loop experiment
([DESIGN.md](../../docs/design/bash-task-loop/DESIGN.md)): the same briefs run
on both belts — arm A with `CODEAF_TASK_BELT` unset (the belt as shipped),
arm B with `CODEAF_TASK_BELT=bash` — one pinned model, n replicates, cells
interleaved so both arms share the day. Every cell is graded by code: the
fixture's own suite, a mechanical diff, or a document's presence and
coverage. No LLM judges anything, per [bench/README.md](../README.md).

## Dry run first

```sh
go run ./bench/bashloop -dry-run          # every invocation: arm, cell, replicate, env, brief
go run ./bench/bashloop -mode pair -dry-run   # the same-question pair
```

The dry run composes every invocation — arm, cell, replicate, env, model,
wall, brief, and where its record lands — and executes nothing. It is the
check bench/README.md's honest-wiring rule asks for; the test
`driver_test.go` proves it (every invocation printed, the arms differing
only in the belt variable).

## Live grid

```sh
go run ./bench/bashloop                       # grid: 6 cells × 2 arms × 3 replicates
go run ./bench/bashloop -mode pair            # one brief, both arms, n=1 — the first smoke
go run ./bench/bashloop -cells c1,c3          # a subset
go run ./bench/bashloop -out bench-results/bashloop/manual-run
```

Each invocation seeds its own fixture repo from `fixtures/` (committed, so
the diff is measurable), works in a throwaway `CODEAF_HOME`, starts one task
through the engine's own task door, waits on the landing through the event
lane, and is read back off the disk the engine wrote: cost from the home's
usage ledger, steps from the family journals' `took` lines, the ending and
the wall from the task notice, the fan-out shape from the graph checkpoint.

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
the driver into the run's CSV (`bashloop.csv`). The verdict words stay in
the design document.

## Reading the rows

`cost_usd` is the home's own ledger sum — the provider's per-response
accounting, the only honest source on a shared key (bench/README.md's cost
rule). `unbilled` counts the rows whose receipt the provider never returned;
a cost that reads low says so in that column. `ending` is the engine's own
state word; a row with `ending=setup-failed` never started. Wall is the
node's own record when it kept one and the driver's clock otherwise, and
`wall_source` says which.
