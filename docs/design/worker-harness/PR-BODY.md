# The worker harness — one loop under `/task` and `do`

This replaces the engine behind `/task` and `codeaf do` with one worker, one
shell hand and a plan store the binary carries. It is the replacement in
docs/design/worker-harness/DESIGN.md, landing in waves on this branch; this
body describes what is on the branch now, at product height.

## What a person gets

- **`/task` becomes a run.** `/task <brief>` starts a run on the plan store
  instead of a node of the conversation's own tree. It answers at once with the
  run's id, the run's root page is the store, and a second `/task` joins the
  live run as a child of its root. When the run ends the conversation is woken
  with the outcome, the result the root reported, and where the work went.
- **`codeaf do` becomes a run with nobody attached.** Same store, same
  supervisor, same worker and the same exit ladder (`0` done · `1` could not run
  · `2` ran and did not finish · `3` a limit stopped it · `4` needed an answer),
  with the same `--json` envelope and usage ledger.
- **The plan store is in the binary.** `internal/plandb` is one SQLite store,
  written by the Go driver already in `go.mod` — never a separate install. A
  worker reaches it as `plandb` in its shell, bound to the run's own store on
  every command.
- **Steering rides the store.** Notes, pause/resume, cancel, amend and priority
  are writes to the plan, and a run worker reads them in its next frame. Hard
  verbs (cancel cascades to descendants and dependents; pause holds a subtree
  out of the frontier) are enforced by the runtime whatever the model does.
- **A text reply no longer ends a task; the task ends when `plandb` says
  done.** A worker that answers in words with no command is told `no action
  executed: answer with one bash call; finish with plandb done <your id>
  --result '…' when the acceptance holds; wait with plandb wait when you are
  blocked on another task` and goes round again; four such replies in a row
  fail the task; `plandb wait` parks it with its claim released. Three tests
  that scripted the old text ending were rewritten to finish in the store:
  `TestBashWorkerPublishesTheLiveStepWhileItsCommandRuns`,
  `TestDoOnTheRunEngineCompletesABriefAndNamesTheRootResult` and
  `TestDoOnTheRunEngineLeavesTheUsageLedgerToTheSession`.
- **A parked task comes back once, when its wait is over.** A worker that
  parks with `plandb wait` is launched again only when everything it waited on
  has finished, or at once when a child failed or was cancelled; a child being
  claimed, started or noted while a sibling still runs is not a reason. A root
  whose children all landed done is never closed failed.
- **The seat a person named is the seat every launch takes.** `--model` seats
  every leaf and `--plan-model` every planning task, wakes included; only the
  check and probe rows still come from the profile. Before this, every wake of
  a root ran on the profile's mastermind row whatever the flags said.
- **The spend block.** Every model call a run makes lands one row in the store's
  ledger tagged with the task, the model and the seat it ran on. The spend page
  draws the run's task spend by seat, and reads it back with `plandb spend --by
  seat` (also `chat`, `project`, `model`, `task`), with `--since 7d` to bound the
  window.

## How to try it

**The harness is behind the switch `CODEAF_TASK_BELT=bash`.** With it unset the
build is unchanged and the shipped engine serves every road; with it set, a
`/task` is a run on the plan store, a task worker carries one shell, and
`codeaf do` takes the run road.

```
CODEAF_TASK_BELT=bash codeaf            # then type:  /task <brief>
CODEAF_TASK_BELT=bash codeaf do "<task>"        # one headless run, then exit
CODEAF_TASK_BELT=bash codeaf do --json "<task>" # the machine-readable envelope
```

A run worker's belt is one shell hand. It coordinates through `plandb`
(`plandb add`, `plandb split`, `plandb task note`, `plandb task overview`,
`plandb done --agent <name> --result '…'`), and it reaches codeaf's non-shell
hands through the binary — `codeaf patch`, `codeaf doc`, `codeaf web fetch`,
`codeaf web search`, `codeaf image` — each on the same code path its tool runs.

**What to try.** Set `CODEAF_TASK_BELT=bash` and open `codeaf`. First, type
`/task <a brief with two or three parts>`: the task rail lists the run's tasks
and, under the one that is running, its live step — the running glyph, `$` and
the command the worker is executing at that moment, with `N steps · $` beneath
it; open the task page and it follows the newest step as it lands until you
scroll up, and resumes following when you reach the bottom again. Second, steer
it: on the task page type a note and press enter; the page confirms it and the
note is read at the worker's next step, and the task's record shows the step
that picked it up. Notes, pause, resume and cancel all ride the plan store, so
a task steered from the page and a task steered from `plandb` on the command
line are the same task. Third, the door with nobody attached:
`CODEAF_TASK_BELT=bash codeaf do "<the same brief>" --json` runs the same loop
headless and prints the envelope — the root's result, the landed files and
branch, the node count and `spend_usd`; `plandb spend --by seat` reads the same
run's ledger back.

## Measured — the six calibrated cells

One seat per arm, the same model on both, a $5 cap per run, on the shared build
box. `shipped` is the shipped engine; `harness` is this branch. n=3, and every
figure is the median of the three.

| cell | shipped $ | shipped wall | harness $ | harness wall | pass shipped / harness |
| --- | --- | --- | --- | --- | --- |
| c1 | 0.0075 | 14s | 0.0057 | 26s | 3/3 · 3/3 |
| c2 | 0.0299 | 273s | 0.0102 | 66s | 3/3 · 3/3 |
| c3 | 0.0140 | 114s | 0.0042 | 43s | 3/3 · 3/3 |
| c4 | 0.0105 | 98s | 0.0037 | 19s | 3/3 · 3/3 |
| c5 | 0.0477 | 359s | 0.0230 | 131s | 3/3 · 2/3 |
| c6 | 0.0259 | 231s | 0.0056 | 48s | 3/3 · 3/3 |

The harness is cheaper on all six cells and faster on five; the one cell it
loses (c1) is the cheapest and shortest, by tens of seconds. It also drops a
cell — c5 passes 2/3 where the shipped engine passes 3/3 — which is why the grid
alone is not the readiness bar.

Median output tokens per call: **476 shipped, 217 harness**.

## Measured — repository feature tasks

Each task was handed a repository at a pinned base commit and a feature to build,
with a 45-minute wall and the same model on both arms.

| task | harness | shipped |
| --- | --- | --- |
| bandit, incremental cache | **PASS** — 358 existing + 89 new tests green, $0.44, 41m, 195 calls | **FAIL** — hit the wall at 45m with 83/89 new tests and the task still running |
| awilix, async container initialization | **23/24** — one case missed, $0.33, 37m | **FAIL** — the worker left an unterminated import block and the build never ran again before the wall |
| bandit, interprocedural taint | ended after 6m with 19/85 — its plan writes went to a store the run does not read | in progress |
| cattrs, partial structuring recovery | in progress | in progress |

The three gaps this table exposed are fixed on the branch, and the reruns after
them are in docs/design/worker-harness/BENCHMARKS.md, § *Reruns after wave 7*:
cattrs **PASSes** (93 calls, wall 20m → 14m, prompt tokens 4.70M → 3.33M);
awilix holds at **23/24** but in 8m for $0.19 against 37m for $0.33, with the
root integrating its children now; and bandit interprocedural taint writes its
plan to the run's own store (seven tasks) though its run still ended early on a
reply with no action.

## Known gaps

- **The readiness bar is not met yet.** The pull request leaves draft only when
  the six cells, both doors, on the same pinned model, at n ≥ 3, show the
  harness at or beyond the shipped engine on pass rate, median cost and median
  wall *at once*, with no cell where the shipped engine wins on any of the
  three. Today it loses c5 on pass rate.
- **A leaf that produced deliverables has no review task yet.** The checker seat
  (a `check` role reviewed against the leaf's acceptance) is designed and
  partly landed; the runtime does not yet refuse a root finish without it.
- **The harness is behind `CODEAF_TASK_BELT=bash`.** With it unset the shipped
  engine serves every road. That is the shape for the owner's hands-on test.
  Once the owner is happy the default flips: the run road serves `/task` and
  `codeaf do` with the variable unset, the variable stays for one release as
  the way back to the shipped engine, and then it goes with the engine it
  selected.
- The manual pages and the prompt corpus are updated with the branch; per-task
  models (`add --model`), the project and machine dashboards, and terminal
  `attach` are later views on the same store.

## Words on the screen

Every word the harness shows a person was read against the chat manual before
this body was written. On the task rail and the task page: *task*, *step*,
*note*, *queued*, *waits*, *running*, *done*, *incomplete*, *your call*,
*node*, *slot*, *branch*, *session* — all already the manual's. In the run's
own sentences (a task's record, a failure reason, the `do` envelope): *run*,
*plan*, *store*, *worker*, *root*, *landing*, *landed*, *result*, *spend*,
*cap*, *wall*, *seat*, *parked*, *wake*, *trajectory* — all already on the
manual's pages. Two sentences were new, and both are now quoted on the
`worker-harness` page rather than renamed, because they are what the belt
says to the model and the record has to show the words the model was told:
`no action executed: …` and `4 replies in a row carried no action`.
*Harness*, *belt*, *envelope* and *leaf* are design and manual words and
never drawn on a screen. The rail's live step line and the page's
following of it (`$ <command>`, `N steps · $`, `queued · waits: <what>`) are
spelled from the same list.

## Draft

**This pull request stays a draft until the owner's hands-on test.** The numbers
above are read before the code, and the bar is the table, not a sentence.
