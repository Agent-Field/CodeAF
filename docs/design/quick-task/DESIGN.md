# Quick tasks — DESIGN

2026-09-10. Owner's ruling after the earlier exploration (Parts 1–5): keep the big task
exactly as it is; add ONE small notion, the quick task, with no barrier to enter or to
leave; the checklist is the graph; a quick node shows on the rail and has a room like any
task; `fork` comes off the belt. Unify later only if it earns it.

## What a quick task is, in one paragraph

A task node of kind `quick`. It runs a worker agent **where the caller works** — the
caller's workspace, no seal, no worktree, no branch. It is started by one tool call that
returns the id at once: no sizing, no shaping, no countdown card, no consent. It has a
`line` (what to do) and `items` (an ordered list it works through, ticking each). It has
no audit and no landing: its **last message is its result**, delivered to the caller as
the ordinary landing note, and the row goes `done`. It may name `files` it will write;
two quick nodes claiming one path run one after the other. It may `depends_on` other
nodes. It may itself call `quick_task` and `propose_task` under the graph's fan and depth
caps. Steer, stop, page and the room are the ones every task has.

## The judge, written once in the tool description

> A task gets its own copy of the folder, is checked, and lands. A quick task works where
> you are and its last message is its answer. If you will read the result and carry on,
> it is quick. If it must be checked and merged on its own, or survive the window
> closing, it is a task. One edit, one read, one command is a step: do it yourself.
> Related steps that share what they learn are one quick task's items, not several quick
> tasks.

## Precedent, and the laws this reuses

- **`designHarnessNode` (`harness_task.go:484`) is the shape**: a real worker built by
  `newTaskAgent` in `a.config.Workspace`, a room opened before the first call, and
  `node.finish(report, nil, "", "")` at the end. `runQuickNode` is that with `runTaskChild`
  in the middle instead of `designRun.round`.
- **Kind is derived from the spec** (`taskSpec.kind()`, `harness_task.go:412`), never
  passed. `quick *quickTaskSpec` beside `design` and `run`.
- **One admission door** (`TaskGraph.admit`, `task_run.go:1203`); one run door
  (`runTaskNode` switch, `task_run.go:4447`); one landing road (`complete` → `announce`
  → `reportTaskNode` → `taskNote` → parent's worker or the conversation).
- **The row's state word is replaceable** by `Doing` (`node.doingNow`,
  `harness_task.go:936`). That is how the row reads `quick · 2/4 · reading foo.go` with
  no surface change.
- **Nesting is `TaskNotice.Parent`.** A quick node started from a task hangs under it on
  the rail for free.
- **The write guard is one config field**: `Config.writeScope` (`session.go:1513`),
  enforced by `writeGuard` (`orchestrate.go:1293`, registered `hooks.go:228`), normalised
  by `normalizeWriteScope` (`fork.go:622`), overlap by `forkScopesCollide` (`fork.go:682`).
- **Fan and depth**: `taskFanLimit = 5` per parent via `claimChild`; `taskDepthLimit = 2`
  (a node at the floor has no `propose_task`; the same rule withholds `quick_task`).
- **The parallel cap** `task.parallel` defaults to no limit; quick nodes take a slot like
  any worker. Tunable later, not now.
- **The kind word law** (`task_kind_test.go:66`) needs a row: the word is `quick`.
- **Emptiness law**: no branch, no merge, no changed list on a quick card unless it wrote.
- **Manual law**: `quick_task` and `items` must appear in `internal/manual/chat/` by exact
  name; probes in `internal/manual/chat_test.go` must reach the section.
- **Icon law**: no new glyph. State glyphs are per state, and a quick row uses them.

## The interface (every lane codes against this)

```go
// internal/session/task_quick.go

// TaskKindQuick is a task that runs where its caller works: no worktree, no
// audit, no landing. Its last message is its result.
const TaskKindQuick TaskKind = "quick"          // TaskKindWord → "quick"

type quickTaskSpec struct {
    line  string   // what to do, one line; also the node's title when no title is given
    items []string // ordered; the worker does them in order and ticks them
    files []string // normalised write claim; empty = unrestricted (fork.go:1120 law)
    done  []bool   // parallel to items; set by the `items` tool; read under graph.mu
}

// admitQuick is the ONE door every quick node comes through, on every road
// (2026-09-11, see "One door, one record, one hold" below). newQuickSpec is
// its only caller-side constructor. Refusals are results the reader gets back,
// never errors, and the door says which refusal it was: a model reads one
// sentence either way, but the ceiling road writes the fan cap down under its
// own word.
func (a *Agent) admitQuick(ask quickAsk) (id uint64, spec taskSpec, refusal quickRefusal)
func (a *Agent) newQuickSpec(ask quickAsk) (taskSpec, string)

type quickRefusal struct {
    said    string // the one sentence whoever asked reads
    fanFull bool   // the parent task's fan cap declined it, not the ask itself
}

// quickTools is the belt door: `quick_task`, withheld at taskDepthLimit exactly
// as propose_task is (task.go:463).
func (a *Agent) quickTools() []bare.Tool
// itemsTool is on a QUICK WORKER's belt only: `items` with {done: n} | {add: [..]}.
func (a *Agent) itemsTool() bare.Tool

// runQuickNode is the body runTaskNode dispatches to when node.spec.quick != nil.
func (a *Agent) runQuickNode(ctx context.Context, node *TaskNode, listed bool) TaskState
// landQuickNode mirrors landSubharnessNode (subharness_run.go:313).
func (a *Agent) landQuickNode(node *TaskNode, report string, state TaskState) TaskState
```

Tool `quick_task` schema (`additionalProperties:false`):

| field | type | required | meaning |
| --- | --- | --- | --- |
| `line` | string | yes | what to do, one sentence |
| `items` | []string | no | ordered steps; empty means the line is the whole job |
| `files` | []string | no | paths it will write, relative to the workspace or absolute inside it; `[]` or absent = unrestricted |
| `depends_on` | []integer | no | ids whose result it needs; same semantics as `propose_task` |
| `title` | string | no | row title; default is the line clipped to `titleLimit` |
| `model` | string | no | same resolution as `propose_task` |

Reply on success (a result the model reads, mirrors `propose_task`'s):
`quick task 7 started: <title> · <n> items` and, when `files` were claimed and another
running quick node claims one of them, ` · waits for task 5 (both claim <path>)`.

Tool `items` (quick worker only): `{done: 2}` ticks item 2 (1-based); `{add: ["…"]}`
appends. Reply: `items 2/4 done` or `items: 5 now`. Every change calls
`node.doingNow(quickDoing(spec))` where `quickDoing` renders `quick · 2/4 · <next item>`
(`quick` alone when there are no items).

Write claim serialisation, at admission (`newQuickSpec`): for every RUNNING or QUEUED
quick node in the same workspace whose `files` collide (`forkScopesCollide`), append its
id to `dependsOn`. Readiness (`readinessLocked`, `task_run.go:1668`) then does the waiting.
Nothing new in the frontier.

Where it runs: `a.config.Workspace` of the caller, `Ground = Workspace`,
`Mode = TaskModeInPlace`, so the card's "where" line is honest. The worker's `Config`
carries `writeScope: spec.quick.files`, `InTask: true`, the caller's places, and
`roomThread: false`.

The worker's opening message (`quickBrief`): the line; the items as a numbered list; the
files claim if any; `admissionQuotesHeading`/`admissionEvidenceHeading` blocks from
`a.admissionContext()` (already compiled, bounded); and one paragraph from
`prompts/quick.md` saying: you work where the caller works, do the items in order and tick
each with `items`, do not build or run tests unless an item says so, say plainly what you
could not do, and your last message is the answer the caller reads. No shaping, no
acceptance clause, no "DONE WHEN".

Landing: `said := lastSaid(child)`; `node.keepWorkerConclusion(said, log)`;
`landQuickNode(node, composeTaskReport(said), TaskDone)`; on a stop `TaskFailed`; on the
round/step cap `TaskFailed` with the report leading `out of rounds — ` and the last
sentence quoted, the hand's law (`fork.go`). The existing `taskNote` carries `resultBlock`.
`settlePolicy` is never consulted: a quick node is never `TaskUnverified`.

Seams touched in existing files (kernel lane, and nowhere else):

| file | change |
| --- | --- |
| `task.go` | `quick *quickTaskSpec` on `taskSpec` |
| `harness_task.go:412` | `case s.quick != nil: return TaskKindQuick` |
| `task_contract.go` | `TaskKindQuick` doc + `TaskKindWord` row `quick` |
| `task_run.go:4447` | `case node.spec.quick != nil: work = a.runQuickNode` |
| `task_run.go:6688` (`newTaskAgentOn`) | `writeScope`, belt `+items`, no `divide`, `fanout` page stays |
| `task_divide.go:499` | refuse division on `TaskKindQuick` like harness |
| `tools.go:189` | `a.quickTools()...` registered beside `taskTools` |
| `beltfacts.go` | bullet: `  - Work you will read the result of and carry on: `quick_task` — it starts now, where you are, and lands as its last message.` |
| `task_status.go:477,840` | arms so a quick node has no `needs look` / `approve` tier |
| `task_kind_test.go` | the row |

Everything else is new files: `task_quick.go`, `task_quick_test.go`,
`prompts/quick.md`, manual sections.

## Lanes

| Lane | Worktree | Owns |
| --- | --- | --- |
| K kernel | `~/af-quick-k`, branch `quick/kernel` | everything in the seams table, `task_quick.go`, unit tests, `prompts/quick.md` |
| S surface | `~/af-quick-s`, branch `quick/surface` | tui3: rail row/room/done card/stop card for the kind, `taskSetupAvailable` excludes quick, tests |
| M manual | `~/af-quick-m`, branch `quick/manual` | `internal/manual/chat/tasks.md` sections + probes, `propose_task` description sentence (`task.go:108`), `docs/changes/unreleased/` entry |
| C ceiling | `~/af-quick-c`, branch `quick/ceiling` | `launchRouteTask`: a write-free turn's sketch becomes a quick node's items; test |
| F fork | `~/af-quick-f`, branch `quick/fork-off` | `fork` off the belt, belt bullet, manual sections, probes, actioncategory; keep the scope functions |

Merge order K → S → M → C → F, one PR against `dev`, suites on the Spark.

## Acceptance, end to end first

1. `internal/session`: a scripted chat proposes two quick tasks in one batch with no edge;
   both run at once (both children's first calls happen before either lands); each lands
   `done`; the note carries each last message; the parent reads two notes.
2. `internal/session`: two quick tasks claiming `a.md` — the second waits for the first.
3. `internal/session`: items tick — the row's `Doing` reads `quick · 1/3 · <item 2>` after
   the first `items{done:1}`.
4. `internal/session`: a quick node under a task hangs with `Parent` set and its note
   reaches the task's worker, not the conversation.
5. `internal/tui3`: a `TaskNotice{Kind: quick, Doing: "quick · 1/3 · …"}` draws a rail row
   with that doing line and no branch or merge word; a done card shows the result body.
6. Real model, `deepseek/deepseek-v4.1-flash`, person-style in tmux on the Spark: ask the
   chat to compare four files in parallel and summarise; expect four `quick` rows at once,
   each done within a minute, the summary as a reply, no branch anywhere.

## Testing law for every lane

NO FULL SUITE ON THE LAPTOP. `go build ./...`, `go vet`, gofmt, `make test-quick`, and
single named tests (`make test-focus PKGS=./internal/session RUN='^TestX$$'`) run here.
Every package suite runs on the Spark:

```sh
ssh spark 'export PATH=$HOME/.local/bin:$PATH; set -a; . ~/.config/fleet/secrets.env; set +a; \
  git -C ~/src/codeaf fetch -q origin <branch> && \
  git -C ~/src/codeaf worktree add -f --detach /tmp/af-<lane> origin/<branch> && \
  go -C /tmp/af-<lane> build ./... && go -C /tmp/af-<lane> test ./internal/session/ -count=1 -timeout 20m 2>&1 | tail -60; \
  git -C ~/src/codeaf worktree remove --force /tmp/af-<lane>'
```

Push the branch first. tui3 gets `-timeout 20m`. Four tui3 tests are red on dev since #798
and are being fixed by another session on `fix/798-harness-clock-reds`; do not touch them.

## One door, one record, one hold — 2026-09-11 (task-start wave, lane Q)

Measured on dev `6a3b478cf` and on the owner's own `tasks.json`. Six things were true
of the quick task that this section replaces.

**The graph would not load once a quick task had run.** `newQuickSpec` leaves the
done-condition empty by design, the record wrote it without `omitempty`, and
`decodeTasks` refused the whole document on an empty one — so every conversation that
had run a quick task resumed with no tasks at all, its ordinary tasks included. #869
fixed that first, and this lane builds on it: whether a node owes a done-condition is a
property of its kind (`kindsWithoutAcceptance`, read through `acceptanceHolds` by the
builder's law test and by the decoder), and `quick` is the one kind that owes none.

**The record labelled a quick node and could not rebuild it.** `taskRecord` had the kind
and not the body, so `restoreNode` came back with `spec.quick == nil` — nothing told the
runner it was quick — and ticks lived only in memory and in the row's rendered word. Now
the record carries `quick: {line, items, done, files}` under `omitempty` (ordinary
records are byte-for-byte unchanged), `restoreNode` rebuilds the body through the one
body constructor `newQuickTaskSpec`, and a tick checkpoints like every other transition
(`TaskNode.quickItemChange`). Before and after, one quick node:

```json
{"id":1,"title":"compare the schema with the migration","summary":"compare the schema with the migration",
 "brief":"compare the schema with the migration","where":"in place","acceptance":"",
 "ground":"/home/me/proj","groundMode":"in place","depth":1,"state":"running",
 "wrote":["notes.md"],"model":"…","startedAt":"…","kind":"quick"}

{"id":1,"title":"compare the schema with the migration","summary":"compare the schema with the migration",
 "brief":"compare the schema with the migration","where":"in place","acceptance":"",
 "ground":"/home/me/proj","groundMode":"in place","depth":1,"state":"running",
 "wrote":["notes.md"],"model":"…","startedAt":"…","kind":"quick",
 "quick":{"line":"compare the schema with the migration",
          "items":["read the schema","read the migration","say which disagrees"],
          "done":[true,false,false],"files":["notes.md"]}}
```

**So the restart rule changed its reason, and its reach.** A quick node is settled on the
close not because it cannot be rebuilt but because it must not be restarted: its
worker's context and its caller's turn died with the process, and a worker started
again would open on the line alone, in whatever folder the new runner stands in.
#869 had already extended the settle to a quick node that was still *waiting*, on the
ground that its list was not on the record; the list is on the record now, and the
waiting one still settles — one left queued would start on its own in a session that
has forgotten why it was asked for (`nothingIsComingBackForIt`). The report is the
list (`ticked 2 of 4: …`, `not ticked: …`), the changed list is what it wrote, and the
recovery line counts it under its own clause, `N quick tasks did not finish`, rather
than as `interrupted (no branch kept)` — a resume and a branch it never had. The
continue verb refuses kind `quick` in the same switch and the same sentence as a design
and a saved shape's run.

**Two doors built two objects.** The ceiling's handover (`checkpoint_quick.go`)
assembled its quick node by hand and admitted it through the route judge's launcher:
an invented done-condition, the ground ladder's mode (a working copy) instead of in
place, a naming call the verb promises not to make, no `where`, depth 0 and no owner,
and no look at the write claims. Now there is one door, `Agent.admitQuick(quickAsk)`:
it holds `quickGate` over the look and the admit, builds the spec through
`newQuickSpec` — the one place mode, ground, `where`, `named`, the empty done-condition,
the family and the model are decided — takes the fan slot and admits. The tool parses a
`quickAsk` out of a call and the ceiling reads one out of a drawing; `launchRouteTask`
no longer knows quick exists. The line is kept whole (the ceiling's line is the
person's own message, which may be a paragraph); only its first line titles the row.

**And the ceiling road no longer pays for a name it cannot use.** `nameAhead` used to be
started above the branch that chooses this road, so every write-free carry-on sent a real
request to a real model and cancelled it microseconds later — for a node `newQuickSpec`
admits `named`, which `TaskGraph.nameNode` therefore leaves alone. The ask now stands
below the branch, where the road is already known; the full road still has the brief
writer's call to land in, so nothing is lost by asking a moment later
(`TestAQuickCarryOnAsksForNoNameAndTheFullRoadStillDoes` counts the requests on both
roads). The other refusal the road can meet is the fan cap: a ceiling reached *inside* a
task carries on to a child of that task, so `claimChild` can decline it, and that ending
is journalled as `dropped:parent-fan-full` rather than as the `dropped:no-brief` it
borrowed — a session file should not send a reader looking for a brief that was never
the problem.

**A running quick task held nothing it wrote.** `claimOver` skips a node with no working
copy and `fileOwner` skipped any node without a branch, so the one kind of node writing
in the person's own folder was the one whose half-written file the chat or a sibling
could overwrite — while the manual said the first writer owned it. Now both claims read
one derivation, `TaskNode.holdsTreeLocked`: a node holding its tree is answered by
`claimOver` for every path in it, and every other running node owns the paths it has
written, by name (`fileOwner`). A quick task still never holds the folder, and a file it
only *named* is still only a sequencing promise between quick tasks, never a hold on
the person.

**Another window's quick parts read as unrelated jobs.** `PresenceTask` now carries
`Kind`, `Parent` (the id in its own spelling), and `Done`/`Total` filled from a quick
node's list. `renderElsewhereBlock` folds a window's rows onto the top of each family
(`foldElsewhere`) and names a non-ordinary kind with `TaskKindWord`, so the model reads
`- Survey the config loaders · window "docs pass" · 3 quick parts running · …` as one
piece of work. The tick count stays off the block: it would churn a block that
promises not to.

**And `TaskNode.Expects` is now on the record.** It was left off on the grounds that a
restored node had already answered its preflight, which was false for a node that had
never started, and the brief section it feeds (`instructionOn`) came back empty for
every restored node. The spec's copy is always restored; the preflight manifest only
onto a record that never ran (`expectsOwed`).
