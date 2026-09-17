# The worker harness — one loop under `/task` and `do` — DESIGN

*2026-09-17, written against `origin/santos/dev @ 3b97f50c3`. Status: design for
a replacement, landing in waves on the branch `harness/worker-loop` through one
draft pull request. It grows out of two experiments that ran behind a flag on
the branch `spark/bash-task-loop` — the bash-only task worker and the plan
store it coordinated through — whose report and store design live there.
This is not behind a flag: it replaces the task engine.*

## The one sentence

A task in codeaf becomes one worker with one bash tool, its plan in a shared
store, and a runtime that launches every ready task and owns the lifecycle —
and the chat, the tasks pane, the room and `codeaf do` become views and
writers of that store rather than engines of their own.

## Why

The bash-only worker measured cheaper and faster on every cell it could run
(the experiment's report: 39–44% cheaper, 47–52% faster where both arms passed), and the one
cell it lost turned out to be a harness fault, not the loop: the `plandb` shim
launched the bench driver instead of the CLI, and the model *had* tried to
split. The current engine pays for a planner call, a contract call, a growth
decision, a reshape-and-judge pair and an audit around every leaf. Through
`codeaf do` the same small fix took 110 seconds against 42 seconds through the
engine's own task door, and the gap is planning and judging, not work.

The owner's direction, 2026-09-17: refactor and replace `/task` and `do` with
this simple loop, keeping every property the chat surface has today
(steering, the room and rail, landing to a branch, `do`'s exit ladder and
JSON), and later add per-seat models on top. **One shipped binary** — the plan
store is `internal/plandb`, in Go, never a separate install.

## What the experiments taught, and what carries

- **The loop itself is right.** One action per response; a malformed
  response is rejected without entering history and four in a row fail the
  task; bash with a process-group kill; head + tail + path truncation; a
  per-step frame; a task as a context boundary. All of it exists in
  `internal/session`'s bash belt and stays.
- **The policy page was diluted.** It was a 7 KB section in a 30 KB system
  prompt that contradicted it in four places and hedged its one decisive rule
  ("your FIRST action is the plan"). On this engine the policy *is* the system
  prompt, followed by the bash idioms and the few codeaf constraints that must
  stay (write scope, no push, report shape).
- **The store is the Go port.** Under eight concurrent writers the port lost
  none of 200 adds; it already has every verb the policy teaches, and the gaps
  are output shapes and two edge rules, which close in wave 1.
- **Bash writes must land.** The old landing staged the `write`/`edit`
  ledger, which a shell worker never fills. Landing stages the working copy's
  own changes.
- **`do` never ran the loop.** Its leaves do their file work in
  `internal/exec`'s executor; no flag on the session side reaches them. `do`
  is re-hosted on the new engine, not patched.

## Decisions

### D1 — two layers: the chat and the run

The **chat** is where a person is. The **run** is the loop: a root worker and
its recursive children, each on the identical loop, launched by a supervisor,
none of them talked to directly. There is no third "worker chat" layer between
them: the chat is already the relay, and a relay model in the middle would only
add a turn.

Steering rides the store. Workers coordinate only through PlanDB (`context`,
`amend`, `cancel`, `note`), so a person steering is **one more writer to the
store**, using the same verbs the workers read:

| the person, in the chat or a task's thread | the store write |
| --- | --- |
| "also handle X" | `add` / `task amend`; the waiting parent wakes and re-plans |
| "stop the auth part" | `task cancel`, cascade to descendants and dependents |
| "sqlite, not postgres" | `context --kind decision`; every worker's next frame carries the diff |
| a worker's question | `task note --kind question`, shown on the row; the reply is a note that wakes it |
| "what is going on?" | `task overview`, `critical-path`; the pane and the room render the store |

Recursion is inside the store — a chat that starts runs whose workers split
further is the loop, not a run inside a run.

### D2 — one store per home; scope is a filter, never a file boundary

One PlanDB for the machine (`$CODEAF_HOME`), every task, dependency, note and
context row tagged with its **project** and the **chat** that created it.
People open a chat for every new thing, so dependencies between chats and
between projects are ordinary, and a graph split into files cannot express
them. In one graph they are `task add-dep t-mine --after t-theirs` and
nothing else; `what-if cancel`, `critical-path` and the dependents of a task
answer across projects because there is one graph to walk. The "knowledge
graph connecting projects" is this store — tasks, dependencies, notes and
decisions with search over all of it — not a second system.

Consequences taken now, because they are cheap now and expensive later:

- **Persistence is SQLite through `modernc.org/sqlite`** (pure Go, already in
  `go.mod`, one binary). A machine-wide store with a writer in every codeaf
  process is what SQLite is for; the JSON file with an `flock` sidecar was
  right for one run and wrong for a home. `internal/plandb/persist.go` is the
  seam; the store's laws (cycle detection over both graphs, descendant
  cancellation, promotion, ownership, composite auto-completion) do not move.
- **Ids are six base-36 characters**, unique machine-wide; `--as` names are
  scoped by project.
- **Done subtrees archive** after a retention window so the live graph stays
  the size of live work.
- **The cost governor is the store's spend summary** per project and per
  chat; every process writes its usage rows there, and a limit is checked
  before every model request.

### D3 — dispatch is per process, by the chat that owns the run

Each running codeaf launches only the runs its own chat created. Readiness is
computed from the whole graph, so when another chat's task lands, mine goes
ready and my process picks it up — cross-chat dependency with no leader
election and no cross-process worker spawning. A run whose chat is closed sits
with "owner away" on its row, and the pane offers **take over**, which
re-origins the run to this chat and reclaims its claimed tasks (`Release` then
`Claim`, both existing store verbs).

### D4 — one working copy per run, shared by its children

codeaf keeps the run's own copy (the ground ladder and worktrees exist and
stay), and **the children of a run work in that copy** rather than in copies
of their own: a parent's work before it split is visible to its children,
which the old per-child copy lost, and landing happens once, at the run's
root. Two runs never share a dirty tree: hand-off across runs — and so across
chats and projects — is by **landed commit**; a task's `done --result` carries
the sha and the paths.

### D5 — every task keeps its trajectory, and the trajectory is the task's page

A worker's loop is linear: assignment, then steps of frame → reasoning → one
command → observation, then finish. Every step is recorded under the task
(`trajectory.jsonl`: the command, the truncated observation and the path of
the full one, the store writes the step made, the children it created), with
the notes, the result and the landing sha. That record is what resume reads —
a fresh worker opens on it with the effects-may-exist sentence — and it is
what a person opens when they click a task:

```
t-3f2a1k  Add rate limiter to /api/upload           running · 14 steps · $0.11
─ assignment ─────────────────────────────────────────────────────────────
  goal, inputs, acceptance …      from chat "upload perf" · after t-9c1x2 (done)
─ steps ──────────────────────────────────────────────────────────────────
  12  $ git grep -n RateLimit internal/api          3 hits
  13  $ sed -n 40,120p internal/api/upload.go
  14  $ plandb split t-3f2a1k --into '[…]'          created t-8a1…, t-8a2…
      ├ t-8a1f0  limiter type + tests               running · 3 steps
      └ t-8a2q7  wire into handler                  pending · after t-8a1f0
─ notes ──────────────────────────────────────────────────────────────────
  ? t-8a1f0: burst size 10 or 20?                   unanswered
 › _
```

Typing in the thread is a **person-note**: it appears in the worker's next
frame as `person said: …`, and the policy says a person's note outranks the
assignment — re-plan before continuing. The worker answers with an action, and
a `plandb task note` reply is an action, so the thread is semi-conversational
without breaking one-action-per-response. Beside the soft steering are the
hard verbs the runtime enforces whatever the model does: `/cancel` (cascade),
`/pause` (launch nothing new in this subtree; running steps finish),
`/amend`, `/add`, `/priority`, and answering a question on its row. Raw
`plandb …` typed in the thread runs as written.

The room and the rail draw these rows with the components they have; the
step gutter keys off the command glyph as it already does for bash.

### D6 — the tasks pane shows what this chat spawned

Default filter: the runs this chat created and their subtrees. A dependency on
another chat's task is **one dim linked row** ("waits on t-9c1x2 · chat
*upload perf* · running"), clickable into that task's thread — never the other
chat's tree. The project view and the machine-wide dashboard are later views on
the same store; they are not this wave.

### D7 — verification is a plan task, not a second engine

Three levels, cheapest first. **Now:** the `finish` check — a task cannot
finish while a child or a dependency is open, and the finish takes effect only
when the action that requested it exits 0. **Next:** the policy's "request one
review round for finished artifacts" becomes a task with a checker seat
(`add --role check --dep t-x`), and the runtime refuses a root's finish while a
leaf that produced deliverables has no done review task — the per-seat models
arrive here, naturally. **Not now:** an advisory watcher that spends a model
call per beat; codeaf's limits and the attached chat cover what it would watch
for.

### D8 — `do` is a run with nobody attached

`codeaf do` creates the run in the store and dispatches it with no chat
attached, keeping its exit ladder (0 done · 1 could not run · 2 ran and did not
finish · 3 a limit stopped it · 4 needed an answer · 124 the wall fired), its
JSON envelope and its usage ledger. Same store, same supervisor, same worker.
`codeaf attach t-…` — the thread in a terminal — is a later door on the same
record.

### D9 — the legacy engine stays for one comparison, then goes

`CODEAF_TASK_ENGINE=legacy` keeps today's engine reachable for one A/B cycle on
both doors. Then it is deleted with what only it needed: `propose_task`,
`quick_task`, `divide_work`, the `tasks` window's engine half, the auditor,
the plan and contract calls, the growth and reshape-and-judge steps. The
manual pages that describe them are rewritten in the same change, as THE
MANUAL LAW demands.

### D10 — the belt: one bash on the wire, and every internal hand reached through it

The worker's belt carries `bash` and nothing that a shell command can be. The
legacy task verbs (`propose_task`, `quick_task`, `divide_work`, the `tasks`
window, `ask`) are not on it — the plan store is their replacement and the
chat is the person's door. What codeaf can do that no shell command can —
**generate an image or other media, parse a document with the billed parser,
fetch a page, search the manual, an exact-match edit that refuses zero or two
regions, read a background job** — the worker keeps, and it keeps it through
bash: each is a `codeaf` subcommand, resolved the way `plandb` is (a shim in
the run's `bin/` that execs the running binary), so the wire carries one tool
and the policy's one-action rule has nothing to fight. The old belt put ten
tools beside bash under that rule and paid 1–17 rejected responses a run for
it.

| hand | on the loop |
| --- | --- |
| image and media generation | `codeaf image …` / `codeaf media …` writing into the working copy; the billed call, the profile's key, the spend row — all the binary's own |
| `read_document` (the billed parse) | `codeaf doc PATH`, text to stdout |
| `web_fetch`, `web_search` | `codeaf web fetch URL` / `codeaf web search Q`, markup stripped, bounded |
| `manual` | `codeaf manual QUERY`, which exists |
| exact-match edit | `codeaf patch FILE --old … --new …`, refusing zero or several matches |
| background jobs | bash's `background` flag as today; `codeaf jobs` to read them |
| `revise_assignment` | gone as a tool: a person's revision is a store write (D1, D5) |

A hand whose subcommand does not exist yet stays a native tool beside bash
until it does — A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN, and a
page must never name a command that is not there. Wave 3 lands the
subcommands; the worker page names only the ones that exist on the build it
ships in.

### D11 — roles: the seat is a property of the task, and the crew picks the model

The loop has one worker shape, but its calls fall into classes the product
already names — the five crew tiers (`reflex`, `low`, `worker`, `high`,
`mastermind`), the two seats every door resolves (`work`, `plan`;
`internal/config/seats.go`), the Pareto crew the learning session tunes per
role. The harness does not invent a sixth thing. **Every store task carries a
role**, and the supervisor resolves the role to a model through the crew the
moment it launches the task, so the crew's own machinery — presets, the open
and all families, `/crew`, the tuned rows — decides which model sits where,
and the harness only says which seat a call belongs to.

| role | who gets it | crew seat |
| --- | --- | --- |
| `plan` | the run's root, and any task that has children — the coordinator's turns are FRAME, PLAN, WAIT and INTEGRATE | `mastermind` (the plan seat) |
| `work` | a leaf that does the work itself; the default for a task born from `add` or `split` | `worker` |
| `check` | the review round (D7): reads a finished leaf's result against its acceptance and answers with a note | `high` |
| `probe` | a task the plan names as an unknown to discriminate — small, disposable | `low` |
| `reflex` | the harness's own non-model chores stay non-model; nothing in the loop uses this tier | — |

Two rules make it clever rather than a knob:

- **The role follows the shape, not a guess.** A task's role is read off the
  store when its next turn is composed: a task with children is a coordinator
  and takes the plan seat; a task with none takes the work seat; `check` and
  `probe` are declared at `add` (`--role`). So a leaf that splits mid-work
  moves up to the plan seat for its coordinating turns and nothing is
  configured. The cost of switching models between turns is a cold prompt
  cache; a coordinator's turns are few and its context is the store, not a
  transcript, so the switch is cheap where it happens.
- **The crew is the one source, and the harness feeds it.** The supervisor
  asks `config` for the tier's model exactly as `/task` and `do` do today, so
  a Pareto-tuned crew row changes the harness's seat with no harness change.
  In return every task's spend row carries `role`, `model`, steps, cost, wall
  and its review verdict, so the learning session's per-role fronts get a row
  from every run rather than from bench cells alone — the dynamic crew closes
  its loop on real work.

Later, not now: a size hint at `add` (`--size S|M|L`) so a crew row can say
"this worker only on small leaves", which is a rule the tuning already
produces; and per-request lane choice (`internal/lane/choose.go`'s Pareto
prune) applied per seat, which exists and needs no design.

## The bar — what "ready" means

The goal is a drastic improvement in what `/task` and `do` cost and how long
they take, at an equal or better pass rate; putting both doors on one engine
is how, not why. So readiness is a table, never a sentence: **the pull request
leaves draft only when `bench/bashloop` — the same six cells, both doors, the
same pinned model, n ≥ 3, run on the Spark — shows the harness at or beyond
current default `dev` on every axis at once**: pass rate, median cost and
median wall, with no cell where legacy wins on any of the three. Cheaper but
slower, or faster with a lost cell, is not ready. Every wave's proof row goes
into the pull request beside the wave, against legacy on the same day, and the
report reads the numbers before it reads the code.

## The waves

Each lands on `harness/worker-loop`, green on the Spark, and the draft pull
request describes what has landed so far.

| # | lands | proof |
| --- | --- | --- |
| **1** | the worker page opens with the policy near-verbatim and drops the contradicting chat sections; the store's gaps (cross-branch hard edges, `insert --before` rewiring, JSON shapes, note ids) | focused `internal/session` bash-belt tests; `internal/plandb` suite |
| **2** | `internal/plandb`: SQLite persistence, project and chat tags, six-character ids, `pause`, person-notes, `finish` semantics, archive, spend summary | store suite, a concurrent-writers test |
| **3** | `internal/run`: the supervisor (launch loop, wait/finish, limits, cancel and pause cascade, per-process ownership, take-over), the worker (bash belt agent, trajectory record, resume clause), landing from the working copy; roles resolved through the crew (D11); the `codeaf` subcommands the belt reaches through bash (D10) | scripted-model tests; a third bench arm: legacy on the crew's seats vs harness on the same seats |
| **4** | `codeaf do` on the run engine | `bench/bashloop -door do` against legacy on the Spark |
| **5** | the chat: `/task` creates a run; the pane's filter and linked rows; the thread with soft and hard steering; the offer card | `bench/bashloop -door task` against legacy; the tmux e2e suite |
| **6** | legacy deleted; manual pages; change entries; the pull request leaves draft | `make check` on the Spark |
| later | attach from the terminal; project and machine dashboards; the review round with a checker seat; decisions promoted to memory | — |

The bench is `bench/bashloop` as it stands — six cells, two arms, graded by
code — with the arms redefined as engines rather than belts.

## Boundaries

Not touched by this design: `internal/exec/bare`'s hands (the bash belt uses
its bash), the conversation's own belt, the ground ladder, the seal, the
resident. The conversation keeps its tools; only what runs a *task* changes.

## Open questions

1. **`ask` on a worker.** The design routes a worker's question through a
   store note the chat surfaces. Is a headless `do` run with an unanswered
   question a `4 needed an answer` exit, as today, or does it wait out a
   timeout first?
2. **Retention.** How long does a done subtree stay live before archiving —
   a fixed window, or until its chat is deleted?
3. **Seats.** Per-task models (`add --model`) could land in wave 3 as a
   column with one reader, or wait for the review round. Which?
