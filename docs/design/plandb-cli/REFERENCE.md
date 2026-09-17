# plandb port — working reference

Shared material for the plandb-cli wave. READ THIS FIRST, then
[DESIGN.md](DESIGN.md). This file is reference for the wave's workers: it
carries the observed behaviour of the earlier plandb CLI (captured 2026-09-16 from
`~/.local/bin/plandb` 0.2.1, run against a scratch db), the adapted store's
API as it stands on this branch, and the wiring semantics. Nothing here is
owned by anybody: it is read and never written.

## The earlier plandb CLI's observed shapes (what to match)

Human text unless `--json`. Errors go to stderr as `error: <sentence>` with
exit 1; successes print their line and exit 0.

```
$ plandb init demo --description "the shape run"
created p-s90m (demo)

next: plandb add "title" --description "detailed spec" [--dep t-upstream] [--as custom-id]
tip:  create tasks in dependency order. use --dep to chain them.
      plandb add "A" --as a && plandb add "B" --dep t-a --as b
      plandb go → work → plandb done --next → repeat

$ plandb add "A" --description "part a" --as a
created task t-a (A)

$ plandb add "B" --description "part b" --dep t-a --as b
created task t-b (B)

$ plandb go
→ t-a "A" [1/9 · 4 ready · 2 blocked]

downstream: t-yj0y "validate A" (receives YOUR result)

actions: context "..." --kind discovery | search "query" | split --into "A, B" | done --next

$ plandb task done t-n8r7 --result '{"tables": "ok"}'
✓ t-n8r7 done [1/1 · 0 ready · 0 blocked]

all tasks complete!

$ plandb done --next        (with a running task: same as task done --next)
$ plandb context "chose sqlite for simplicity" --kind decision
c-1bgx [decision]

$ plandb contexts
  c-1bgx [decision] chose sqlite for simplicity

$ plandb search "schema"
  [task] t-n8r7 generic Design schema: design the tables

$ plandb critical-path
Critical path (2 tasks):
  ○ t-a A [ready]
  · t-b B [pending]

$ plandb bottlenecks
Bottlenecks (tasks blocking the most downstream work):
  t-a A — blocks 1 tasks [ready]

$ plandb task insert --after t-a --before t-b --title "validate A" --description "check a's output"
inserted t-yj0y

$ plandb task amend t-b --prepend 'NOTE: use jwt'
amended t-b

$ plandb what-if cancel t-b
what-if cancel t-b          (ours previews the cascade: see below)

$ plandb task cancel t-b
cancelled rows=1

$ plandb split t-a --into 'X, Y'
split t-a

$ plandb status
p-s90m demo: 0/1 done (0%) | ready: - | running: t-n8r7@default | blocked: 0

$ plandb list --status ready
(no rows)          (or rows like:   t-a A [ready])

$ plandb task overview
Project overview: 1 tasks
  ◉ t-n8r7 Design schema [running] default

$ plandb task get t-a
id: t-a
project: p-s90m
title: A
status: ◉ running
kind: generic
priority: 0
agent: default
description: part a

$ plandb --json add "Design schema" --description "design the tables"
{
  "id": "t-n8r7",
  "project_id": "p-s90m",
  "parent_task_id": null,
  "is_composite": false,
  "title": "Design schema",
  "description": "design the tables",
  "status": "pending",
  "kind": "generic",
  ... (only fields our store carries; unknown fields omitted)

$ plandb --json split t-a --into '[{"title":"P","description":"p"},{"title":"Q","description":"q","deps_on":["P"]}]'
{
  "created": ["t-7ekw", "t-gy3y"],
  "done": [],
  "effect": {
    "accelerated": ["t-7ekw", "t-gy3y"],
    "blocked_now": [],
    "critical_path": ["t-a", "t-yj0y", "t-b"],
    "delayed": [],
    "depth": 3,
    "ready_now": ["t-7ekw"]
  },
  "parent_task_id": "t-a",
  "project_state": {"done": 1, "pending": 2, "ready": 4, "running": 0, "total": 8},
  "title_to_id": {"P": "t-7ekw", "Q": "t-gy3y"}
}

error shapes (stderr, exit 1):
error: dependency task 't-1' not found. Create it first, then add the dependency.
error: no running task found for agent 'default'. Specify task ID explicitly.
error: not found: task t-impl
```

Status icons: `✓` done, `◉` running, `○` ready, `·` pending, `⊘` cancelled,
`✗` failed. The `status --full` containment tree uses `├─` / `└─` before the
child rows. The `@default` in status is `@<agent>` of the running task.

Ids: ours are `t-` + the bare id the store keeps (`t-a`, `t-7ekw`). The store
trims the `t-` prefix on input; the CLI prints it back on. `--as api` yields
`t-api`. Unnamed adds mint four base-36 characters.

The earlier plandb CLI's `what-if cancel` prints only the echo line; ours previews the
real cascade (the task, its descendants, its hard dependents) because "preview
effects" is what its help promises — the echo alone previews nothing.

## The adapted store, as it stands (internal/plandb, on this branch)

Adapted from an earlier port of `internal/plandb`. The
graph laws are unchanged from that port: cycle detection over both graphs,
descendant cancellation, promotion, claim ownership, composite
auto-completion. What changed: the governance gates are GONE (no role /
deliverables / acceptance / effect requirements — `validateSpec` checks only
shape), `parallel` defaults to `safe`, notes and fuzzy `Resolve` and search
and `critical-path`/`bottlenecks` were added, and every transaction takes an
advisory `flock` on `<db>.lock` around load-change-rename so separate
processes (the CLI) and the runtime's own writes cannot lose each other.

Key methods: `Open(path, project, rootID, rootTitle, rootDesc)` — an existing
file belonging to another run is a refusal; `AddMany(specs)` — the batch is
validated whole before anything is written; `Claim(id, agent)` — only a ready
non-composite leaf, agent recorded for ownership; `Done(id, agent, result,
artifacts, evidence)` / `Fail` — `requireOwner` enforces the claiming agent;
`Cancel(id, reason)` — cascades descendants and hard dependents; `Amend(id,
text)` — prepends to the description; `AddDep(downstream, upstream, kind)`;
`AddNote` / `Notes`; `AddContext` / `Contexts` / `Prune`; `ReadySet()` /
`ReadyLeaves()`; `Tasks()` in admission order; `Resolve(word)` — exact id,
then `t-`+id, then unique prefix; `Search(query, limit)`; `CriticalPath()`;
`Bottlenecks(limit)`; `NextID()`; `Summary()`; `CanFinalize()` — (true, "")
only when every non-root task is terminal; `CompleteRoot(result)` — runtime
only; `ClaimNext(agent)` — one atomic claim of the highest-priority ready
leaf, the `go` verb's whole body. `Store.Task(id)` is a nil-answering Show.
NEVER nest store method calls inside another store method call's change
function — every method takes the same two locks, and nesting them is a
self-deadlock. Compound CLI verbs decompose into single calls; `done --next`
is a Done followed by a ClaimNext, and a crash between the two leaves the
next task unclaimed, which the next pass simply claims.

The store's ROOT is the run: one root task, claimed running by "runtime",
and no worker verb may complete it. `depsDone` walks the parent chain, so a
child cannot run out from under an unfinished parent's coordination. A hard
(non-`suggests`) dependency across a containment lineage is REFUSED — a
written divergence from the earlier CLI, recorded in DESIGN.md.

## The runtime's side of the contract (what the CLI's callers rely on)

- The runtime claims every store task it dispatches as a node with the
  TASK'S OWN ID as the agent name — the planner's trick, and what makes the
  worker's `done --agent <task-id>` the ownership check.
- A worker that finished its own task first (the taught finish,
  `plandb done --agent <id> --result …`) is never written over: the
  write-back skips already-terminal tasks.
- `go` and `done --next` claim for the CALLER (parity with the earlier CLI). A task claimed
  that way and not yet a node is dispatched by the pulse as a node and
  re-claimed under its own id, so the node's worker can finish it.
- Dispatch respects the fan cap (`TaskGraph.claimChild`, refusal leaves the
  task in the store for the next pass) and the depth limit (a ready child of
  a floor node stays in the store; it is cancelled with a plain reason when
  its parent lands).

## Rules for every worker on this wave

- `make build` and `bin/codeaf` are the owner's law (AGENTS.md). Do not
  create stray binaries outside the tree's own `bin/`.
- Do NOT run `make check`, `make pr-ready`, or the full `go test ./...` /
  `./internal/tui3` / `./internal/session` suites. Prove with `go build
  ./...`, `go vet` on the packages you touched, and focused tests
  (`make test-focus` where it fits).
- Do NOT push, commit, or open a pull request. Files only.
- `internal/exec/bare` is untouchable. The shipped belt and the conversation are
  unchanged: everything new is behind `CODEAF_TASK_BELT=bash`.
- The name is `codeaf`, lowercase, everywhere a person or a model reads it
  (AGENTS.md's namelaw gates the session prompts too). No `CodeAF`.
- Comments state the why in prose, the way the surrounding files do. Read
  AGENTS.md at the repo root before writing.
