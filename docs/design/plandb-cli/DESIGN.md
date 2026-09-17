# The plandb CLI on the bash belt — DESIGN

*2026-09-16, written against `spark/bash-task-loop`. This is the next wave of
the bash-only task loop experiment
([docs/design/bash-task-loop/DESIGN.md](../bash-task-loop/DESIGN.md)): the
planner's loop gets its planner. Everything here lives behind one switch,
`CODEAF_TASK_BELT=bash`; unset, not one byte of any worker or the conversation
is where it was.*

## The one sentence

The bash belt's coordination stops being codeaf's own graph verbs and becomes
what the loop actually drives — a `plandb` CLI over a ported store,
run by the model through its one bash tool, with the runtime owning the task
lifecycle exactly as the supervisor does.

## What the planner does

The planner drives the CLI this way:

- **The model's verbs are coordination only.** The policy page teaches: `add`,
  `split` (JSON parts with `deps_on` naming sibling titles), `task add-dep`,
  `task amend --prepend`, `task insert --after --before`, `task pivot
  --subtasks`, `task cancel` / `what-if cancel`, `task note`, and the reading
  set `task overview`, `task notes`, `search`, `critical-path`, `bottlenecks`,
  `list --status ready`, plus `context TEXT --kind decision`.
- **The lifecycle is the runtime's.** The page says "The supervisor launches
  ready tasks continuously. Never claim/start/spawn/done tasks yourself", and
  `runtime.py` enforces it in the argv stream: a worker's `claim`, `start`,
  `go`, `done`, `fail`, `pause`, `init`, `use` and friends are refused with
  "Task lifecycle and scope are managed by the supervisor". The supervisor
  claims each task at launch with the task's own id as the agent name
  (`claim task --agent task`), and the worker's finish is a `finish --result …`
  runtime verb that checks lifecycle consistency and then writes the
  completion into the plan store.
- **The plan is one database for the whole run.** `registry.py` resolves the
  db path from the run's `run.json`, passes it explicitly on every call, sets
  `PLANDB_AGENT` for the caller, and reads `--json` when it wants structured
  answers.
- **Dispatch is automatic.** "DISPATCH is automatic: every ready task you
  create is executed by a fresh worker." The supervisor polls the ready set and
  launches one agent per ready task; each worker's assignment is one task; the
  worker reads its work order from the store and recursively coordinates the
  same way.

## The two things that already exist, and are adapted

**The earlier plandb CLI is installed here**: `~/.local/bin/plandb` (0.2.1).
`plandb --help` prints the full surface and is the behaviour to match; where
its help and the Go port disagree, the earlier plandb CLI wins. It is NOT the binary this
work installs over — it uses SQLite and a different store, and the runtime must
read the same store the worker writes.

**The Go port of the store exists**: an earlier port of `internal/plandb`
(`store.go` 948 lines, `model.go`, `persist.go`, `store_test.go`) with a tool
surface beside it. Its shape is kept: `Status` (pending →
ready → claimed → running → done/failed/cancelled), `DepKind`
(feeds_into/blocks/suggests), `TaskSpec`/`TaskPatch`/`Task`, `ContextEntry`,
`Summary`, `ReadySet`; and `Store` with `Open/AddMany/ReadyLeaves/ReadySet/
Show/Claim/Done/Fail/Release/Retry/Cancel/Revise/AddContext/Contexts/Summary/
CanFinalize/CompleteRoot`. Its graph laws are kept: cycle detection over both
graphs, descendant cancellation, dependency promotion, claim ownership.

What the adaptation changes, and why:

- **The earlier governance gates come off.** `validateSpec` demands role,
  deliverables, acceptance, effect and resource claims, and `Claim` refuses a
  task whose effect is unresolved or that carries no resource claim. The
  earlier plandb CLI has none of that — `add TITLE --description SPEC` creates a task. Every
  CLI `add` would fail under the old gates, so the gates go: the store keeps
  the graph laws and drops the eligibility ladder (`eligibilityReasons` and the
  resource-conflict refusal in `Claim` go with it — the CLI has no
  `--resource` flag, so a claim would conflict with nothing and block every
  parallel dispatch).
- **`parallel` defaults to `safe`, not `serial`.** Under the port's conflict
  rule two serial tasks never run at once, which is the opposite of the
  loop's whole point. The earlier plandb CLI is parallel-unless-declared;
  the port follows it.
- **Descriptions become optional at the store, taught as mandatory.** The
  earlier plandb CLI's `add` accepts a descriptionless task; the doctrine still says "ALWAYS use
  --description. It's the work order". The store accepts; the page teaches.
- **Notes are added.** The earlier plandb CLI has task-scoped notes (`task note`,
  `task notes`) separate from project-wide context; the port has only
  `ContextEntry`. State gains `notes` — task id, agent, content, timestamp —
  and the store grows `AddNote`/`Notes`.
- **IDs keep the port's bare spelling behind the CLI's `t-` spelling.** The
  store trims a `t-` prefix on the way in (as the port does); the CLI accepts
  both spellings on input and prints `t-…` on output, generates short ids for
  tasks added without `--as` (`t-` + four base-36 characters), and honours
  `--as api → t-api`. Fuzzy ids: exact match first, then unique prefix.
- **A cross-process file lock.** The port's mutex guards one process; workers
  drive the CLI as separate processes against the same file. Every
  read-modify-write transaction (store method or CLI verb) takes an `flock` on
  a `<db>.lock` sidecar around load, change and atomic rename. The runtime's
  own writes go through the same lock, so the two roads cannot lose each
  other's updates.
- **Hard edges cross containment branches, and are a written divergence.**
  Dependencies cross containment boundaries freely: a hard (non-`suggests`)
  edge may join two tasks in different branches of the containment tree, which
  is what the doctrine's "Use existing task IDs for cross-branch edges" line
  teaches. The one hard edge still refused is between a task and its own
  ancestor or descendant, because that edge would have a task wait on the
  lineage that schedules it, and cycle detection runs over both graphs. The
  readiness rule walks the parent chain: a task is ready when its own hard
  dependencies are done **and** every ancestor's hard dependencies are done,
  and promotion demotes a ready task back to pending whenever that stops
  holding, so the two never disagree. `suggests` edges cross freely.

## The store's home

One store per run, beside the session's own state: `plandb.json` in the
session folder ([Place.Dir]), or — for the legacy flat layout, whose Place is
zero — `<workspace>/.codeaf/plandb.json`. The runtime derives the path once
per graph and remembers it on the graph; a resumed graph derives it again the
same way.

The CLI resolves the same store without being told: it walks up from its
current directory looking for the first ancestor holding `plandb.json` or
`.codeaf/plandb.json`. Every bash-belt worker's shell runs inside the session's
tree directory, so the walk lands on the run's own store. `--db` overrides;
`PLANDB_DB` is honoured for parity with the earlier plandb CLI. Nothing mutates the
process environment to point workers at the store — an environment variable is
one process wide, and two sessions in one process would fight over it.

## The CLI

`cmd/plandb` builds `bin/plandb` (`make build` builds it beside `bin/codeaf`);
the same runner is also reachable as `codeaf plandb`, which is the fallback
road when the sibling binary is not where the running binary is. One runner,
`internal/plandb/cli`, both entries call. The worker's page names the resolved
command (see the doctrine section); nothing else hardcodes either spelling.

Output matches the earlier plandb CLI's shape: human text by default, `--json` for
structured answers, `-c/--compact` accepted (compact is the default text
shape). Every mutation prints the created or changed task's id; `add` and
`split` print created ids and a `title_to_id` map for a split, which is what
the doctrine tells the model to read.

**Ported verbs** — the doctrine's set plus the loop's own:

| verb | ported behaviour |
| --- | --- |
| `add TITLE --description SPEC --parent TASK_ID [--dep PREDECESSOR[:KIND]] [--as NAME] [--kind K] [--priority N]` | creates a task under the parent (absent parent: the caller agent's running task, else refused with the doctrine's own advice); repeatable `--dep` |
| `split TASK_ID --into SPEC` | SPEC is a JSON array of `{title, description, deps_on[]}` (deps_on names sibling titles), or comma titles, or `>` chain; answers created ids and `title_to_id` |
| `task add-dep DOWNSTREAM --after UPSTREAM [--kind feeds_into\|blocks\|suggests]` | additive edge, graph laws enforced |
| `task amend TASK_ID --prepend TEXT` | prepends to the description |
| `task insert --after A [--before B] --title T [--description D]` | new task depends on A; with `--before`, the after→before edge is rewired through the new task |
| `task pivot TASK_ID --subtasks JSON [--keep-done]` | cancels the pending subtree and creates the named children |
| `task cancel TASK_ID` / `what-if cancel TASK_ID` | cancel with descendant and dependent cascade; what-if previews without applying |
| `task note TASK_ID TEXT` / `task notes TASK_ID` | task-scoped notes |
| `task overview` / `show TASK_ID` / `list [--status …] [--kind …]` / `status [--full]` | the reading set, rendered from the store |
| `search QUERY [--limit N]` | BM25 over titles, descriptions, notes and context |
| `context TEXT [--kind K]` / `contexts [--kind K]` / `prune ID` | project-wide shared context |
| `critical-path` / `bottlenecks` | longest hard-dependency chain; most-blocked tasks |
| `done [TASK_ID] --result TEXT [--agent A] [--next]` | completes a task the caller owns (ownership enforced); `--next` reports what became ready and says dispatch is automatic |
| `go` | reports the ready set and says dispatch is automatic — it does not claim, because claiming is the runtime's (see below) |
| `init NAME` | creates the project and its root task |

**Refused to the model, in the planner's own words**: `claim`, `start`,
`fail`, `pause`, `next`, `heartbeat`, `progress`, `approve` — the lifecycle is
the runtime's. They are named in the refusal so a model that reaches for one
learns the rule in one step.

**Not ported** (no store fields behind them, and no doctrine sentence teaching
them): `project` beyond `init`, `use`, `mcp`, `serve`, `watch`, `events`,
`ahead`, `artifact`, `export`/`import`, `decompose`/`replan`/`create-batch`
(YAML surfaces), `task update`, `task remove-dep`, `what-if insert`, and
`--pre`/`--post`/`--tag`/`--max-retries`/`--timeout`/
`--requires-approval`/hooks on `add`. Each is named here so the omission is a
decision, not a gap.

## The wiring point

Everything below is behind `bashBeltAsked()` and answers through the graph —
the same `TaskGraph` codeaf's own verbs admit into — so the guarantee is the
one the brief names: a bash-belt node's work order arrives from the store, and
the children a worker adds or splits become real nodes the runtime dispatches.

**The seed — `TaskGraph.admit` (task_run.go:1282), the one door every task
passes.** Under the flag, the first ordinary task node admitted (not a quick
node, not a design, not a run) seeds the store: project = the node's title,
root task = the node's own contract. The node's brief is then composed FROM the
store read — description plus the plan's own lines (the task's id, its agent
name, the finish command) — so the work order genuinely arrives through the
store. The node's spec carries the store id in a new `planID` field
(checkpointed, so a resumed node still knows its task).

**The pulse — `TaskGraph.planPulse`, called from two places:**

1. after each bash tool call on a bash-belt worker (`bashbelt.go`'s
   `truncatingBash` wrapper — the same wrapper that applies the cut), and
2. on every road out of `workTaskNode`, after the node has landed.

One pass, no polling, no timers. It:

- **writes back** every plan-born node that has settled since the last pass:
  done with the node's report as the result, failed with its ending, cancelled
  with its reason — ownership taken from the store's own `claimed_by`, so a
  worker that completed its own task first is never written over; and
- **dispatches** every ready, non-composite store task that has no node yet:
  a fan slot is taken on the parent (`claimChild`, the same cap `propose_task`
  answers to), the spec is built from the store (title, brief from the
  description, the person's request and ground inherited from the parent node,
  depth one below the parent), the node is admitted through `admit` — the
  graph's frontier starts it exactly as it starts a `propose_task` child — and
  the store task is claimed with the task's own id as the agent name, which is
  the planner's own trick and what makes the worker's finish command enforceable.

Depth is the same three levels (`taskDepthLimit`): a ready child of a node at
the floor is left in the store, and cancelled with a plain reason when its
parent lands. Fan-cap refusals leave the task in the store for the next pass;
a landing frees slots, and every landing is a pass.

**Root completion.** When the graph's root node has settled and no plan-born
node is open, the pulse completes the store's root (`CompleteRoot`) and cancels
whatever was left undelivered, each with a plain reason. The store's own
composite auto-completion (a parent done when all its children are terminal)
does the rest of the bookkeeping the planner's "Composite tasks
auto-complete when children finish" promises — and when an auto-completed
parent's own node lands later, its report fills the empty placeholder the
auto-completion left: the one case the store's ownership guard yields to, and
the only write that may follow a worker's own `done`.

The pulse's landing point lives in the runner's landing road, after
`TaskGraph.complete` settles the node's state — not at the worker's return,
where a pulse would read a task still running and dispatch into a subtree
`stopChildren` has already cut. A run whose root has completed is over; a new
task in the same conversation seeds a fresh plan as the new root, and the
finished plan is archived beside the session (`plandb.json.1`, `.2`, …) — one
store per run, as the planner keeps it, and the one live name is the one
both the walk-up and the runtime find.

**What comes off the belt.** For a bash-belt worker, `propose_task`,
`quick_task`, `divide_work` and the `tasks` window come off
(`bashbelt.go` drops the four appends) — the CLI is their replacement, and a
belt that carried both would teach the model two ways to say one thing.
`revise_assignment` stays: it is the person's redirection lane, not
coordination, and when the node has a `planID` it writes the revised brief
through to the store task's description so the store stays the record.
`internal/exec/bare` is untouched, exactly as the parent design says.

**The worker's finish.** The page teaches the planner's rule: finish
with the CLI — `<plandb> done --agent <your agent> --result '…'` — after
acceptance holds, and end the turn when nothing independent remains. The
node's landing is the safety net, not the finish: a task still running when its
node lands is completed by the pulse with the worker's report, and the store
never says done twice. The generic fan-out page comes off this belt with the
verbs it teaches: the bashworker page carries the same loop (plan, automatic
dispatch, wait, integrate) in the plan's words, and a page naming tools the
belt does not carry is a prompt that lies.

## The doctrine

`internal/session/prompts/bashworker.md` gains the planning section, in
the planner's policy voice: the frame → plan → dispatch → wait → integrate loop;
the command set exactly as the table above spells it (with the resolved
command name rendered in, so the page never names a binary that is not there —
the same law `beltfacts.go` states for every tool-naming sentence); "dispatch
is automatic — every ready task you create is executed by a fresh worker";
"never claim/start/done tasks yourself — the runtime owns the lifecycle"; and
the finish shape. The page's own sentence that still points at `propose_task`
("HANDING OUT IS AUTOMATIC: what you propose runs in its own worker") is
rewritten to the store's words. The file-tool doctrine sections stay exactly as
they are — this wave adds planning verbs, it does not touch the shell idioms.

## Waves, each landing green

1. **The store and the CLI.** `internal/plandb` (adapted model, store,
   persist, lock, cli), `cmd/plandb`, the `codeaf plandb` subcommand, the
   Makefile line. Green: `go build ./...`, `go vet` on the new packages, the
   adapted store tests, and a CLI end-to-end test that drives the built binary
   over every ported verb.
2. **The wiring.** Seed at admit, pulse at the two points, dispatch, write-back,
   root completion, the four tools off the bash belt. Green: `TestPlandbCli*`
   tests — a bash-belt node whose worker coordinates through the built CLI
   (add, split, note, done) grows the same node tree codeaf's own verbs grow;
   and the flag-off belt is byte-identical to today's.
3. **The doctrine.** The bashworker page's planning section, the resolved
   command name, the propose_task sentences gone. Green: the page renders for
   every agent shape this package builds, and names the command that exists.

The verification bar for the whole piece: `go build ./...`, `go vet`, the
`TestPlandbCli*` tests green — no `make check`, no full suites (they run on
the Spark), no push, no pull request.
