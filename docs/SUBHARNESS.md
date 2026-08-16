# SUBHARNESS — one node, many harnesses

Design for implementation. Every file and symbol below was read on branch
`chat-v3-task` as of 2026-08-16. The implementing agent should re-read the
named functions before editing them; this document is the law, not a
transcript.

---

## 0. What this is

The v3 task graph (`internal/session/task_run.go`) runs every node as an
in-process `*session.Agent` built by `newTaskAgent`. This change makes the
node's worker **polymorphic**: a node may run as our own agent (today's path),
or as a foreign CLI harness — **claude** (Claude Code) or **codex** (OpenAI
Codex) — launched as a subprocess in the node's worktree, using the person's
**native, already-logged-in CLI auth** (`~/.claude`, `~/.codex`). No API keys
are collected, stored, minted, or passed. Ever.

The point of the design is that **nothing downstream learns the difference**:
the room, the journal, the index, the auditor, the TUI, and the checkpoint see
exactly the shapes they see today. All harness specificity lives in one
registry entry and one small translator per harness.

### The name's two lives

`internal/exec` already has `RegisterSubharness` (`subharness.go`) — a registry
of *leaf workers* for the old planner/executor world (`swe`, `bare`). That is
the pattern this doc copies, not the layer it touches: *the seam had to be
load-bearing before the second worker existed, so the second worker arrives as
a registration, not a rewrite.* What ships here is the same move one layer
down, for the v3 session graph's **nodes**. Two registries, one philosophy.
Do not merge them; do not rename either.

### Binding constraints (the user's, non-negotiable)

- **C1 — Native auth, never keys.** Foreign harnesses authenticate with the
  person's own logged-in CLI. The subprocess inherits `HOME` and nothing
  credential-shaped is ever written into a brief, a journal, an env we
  construct, or a log.
- **C2 — Error propagation is designed, not hoped for.** The two-envelope
  model in §5 is part of the contract, with the probe and the pattern table
  living **in the harness registration**, beside the constructor.
- **C3 — Agot is not built, but the seam must not preclude it.** Nested
  dynamic decomposition (a node adding edges to its own graph, per
  `audit-notes/agot-implementation-blueprint.md` and the graph's own header
  law: *edges into this executor, not a new shape*) arrives later. This change
  must leave the hooks it will need: the worker is constructed with the node
  id and journal path, and nothing anywhere assumes a one-node graph.
- **C4 — No second convention.** Every downstream consumer (room, index,
  audit, TUI, checkpoint, `tasks` tool, `@` mentions) keeps reading today's
  types. A harness-specific shape leaking past the translator is a defect.

---

## 1. The seam: `taskWorker`

Today `workTaskNode` (`task_run.go`) knows the child is an `*Agent`: it calls
`newTaskAgent`, `runTaskChild` (which consumes the child's `Events()` and
counts steps), `child.Close()`, `child.Usage()` (via `foldTaskUsage`), and the
room's `speaking(child)` (which is how `SteerTask` reaches
`child.enqueueSteering`).

Replace that knowledge with one narrow interface, defined in
`internal/session/task_worker.go` (new file):

```go
// taskWorker is one node's worker, whatever harness runs it. The executor
// knows this and nothing else.
type taskWorker interface {
    // Run works the brief to a claim. ctx carries the node's deadline and
    // the person's kill. changed is repo-relative; report is the worker's
    // own account, two or three lines (task_run.go's caps still apply).
    Run(ctx context.Context, brief string) (report string, changed []string, err error)
    // Events is the room's feed: the node's narrative as it happens, in the
    // session Event type. Closed when the worker settles.
    Events() <-chan Event
    // Steer puts the person's words to the worker. A harness that cannot
    // hear mid-run answers an error that says so, in its own words.
    Steer(text string) error
    // Close releases the worker: process group, temp files, the lot.
    Close() error
}
```

The in-process agent becomes `agentWorker` — a thin adapter over today's exact
construction (`newTaskAgent` + `runTaskChild` + `foldTaskUsage` move inside
it). Zero behavior change for the `aforge` harness; the move is purely
structural.

`workTaskNode` keeps everything else: worktree prep, thresholds are **rebased
onto Events** (see §4), the audit gate, `comeHome`, `node.finish`, spend
folding (workers that can't report usage report zero; the journal is the
record).

The room (`task_room.go`) changes one line of type: `speaking(child *Agent)` →
`speaking(w taskWorker)`, and `SteerTask` calls `w.Steer(text)`. A worker not
yet started answers the same "no worker to talk to yet" it answers today.

---

## 2. The registry

New file `internal/session/task_harness.go`:

```go
// HarnessInfo is one harness's whole registration: how it is probed, how its
// failures read, whether it can hear, and how a worker for it is built.
type HarnessInfo struct {
    Name string // "aforge", "claude", "codex"

    // Probe answers nil when this harness can run on this machine RIGHT NOW:
    // binary present, native login alive. A non-nil answer is a sentence the
    // person can act on ("claude is not logged in — run `claude auth login`").
    Probe func() error

    // Patterns maps this harness's known failure signatures (stderr/stdout
    // substrings or exit codes) to plain sentences: auth death, rate limit,
    // context overflow. First match wins; no match is a plain crash report.
    Patterns []FailurePattern

    // Steering is how this harness hears the person mid-run.
    Steering SteeringKind // SteeringInProcess | SteeringStdinJSONL | SteeringNone

    // New builds the node's worker.
    New func(node *TaskNode, dir string, parent *Agent) (taskWorker, error)
}
```

Registration mirrors `installSubharnesses` (`cmd/aforge/subharness.go`): one
`installTaskHarnesses()` in `cmd/aforge`, called before any surface runs,
registering `aforge`, `claude`, `codex`. `proposeTask` resolves the name
through the registry; an unknown name is an ordinary tool-result refusal
naming the registered set.

**The `harness` field on the wire.** `taskSchemaJSON` gains one optional
property (model after `model`, in the same voice):

```
"harness":{"type":"string","description":"Optional. Which harness runs this
work: \"aforge\" (our own agent, the default), \"claude\" (Claude Code CLI) or
\"codex\" (Codex CLI). Set it ONLY when the person asked for a particular
harness; leave it out and the task runs as our own agent"}
```

It flows: `taskArguments.Harness` → `taskSpec.harness` (frozen at admission
with the rest of the spec) → `TaskNotice.Harness` (the card shows it, the way
it shows Model) → `taskRecord` in `task_store.go` (checkpoint survives
restart) → the index row in `task_index.go` (so `@` and the `tasks` tool can
say "the claude one"). Empty means `aforge`, everywhere, forever.

---

## 3. `cliWorker` and the journal law

`internal/session/task_harness_cli.go` (new). One worker for every
subprocess harness; per-harness difference is argv construction and the
translator.

**Launch.** `exec.CommandContext` in the node's worktree, own process group
(kill = the group's, like `jobs.go`'s). Env: `os.Environ()` minus nothing —
the harness's native auth lives in `HOME` and travels by inheritance (C1).
No `AFORGE_*` or credential-shaped additions. (Agot note: a future
`AFORGE_GRAPH=` socket env slots in here without touching anything else.)

**Claude argv (pin exact flags against the installed binary at
implementation time):** `claude -p <brief> --output-format stream-json
--verbose --dangerously-skip-permissions`. Steering uses
`--input-format stream-json` with the brief as the first stdin message and
steering lines as subsequent user messages. **Codex:** `codex exec`
non-interactive mode; steering kind `SteeringNone`. Posture law for both:
bypass-in-worktree — the node's approval posture is *allow everything except
the floor*, and for a foreign harness **the floor is the worktree, the audit,
and the person's kill switch**, because our in-process critical table cannot
reach inside a subprocess. The report must say which harness ran (it lands in
the index row and the card).

**THE JOURNAL LAW — one format, whoever wrote it.** The node's journal is a
real session-file JSONL (`taskJournalPath`), and that does not change for a
foreign harness. The cliWorker **translates** the harness's stdout stream into
the same journal records an `*Agent` would have written — append-only, as the
stream arrives, never buffered to the end. The translator is **total**: a
line it cannot parse is appended as a raw record, never dropped and never
fatal. Four consumers — the room (`WatchTask`), the project index
(`task_index.go`), the auditor (`task_audit.go` reads the journal), and the
person opening the file — all keep working with zero changes, and a search
over `~/.aforge/v3/tasks/**` stays harness-blind. This translation point is
the whole parity story; it is why the TUI's room viewer renders a claude run
with no new code.

**Events.** The same translation drives `Events()`: stream records become
`EventToolBegin`/`EventToolEnd`/`EventToolFailed`/text-delta `Event`s, so
`runTaskChild`'s successor loop counts **steps and progress with today's exact
semantics** (one step = one finished tool call; progress = a successful
edit/write). `max_steps` and `no_progress` therefore bind a claude node
exactly as they bind ours — named thresholds, never wandering.

**Report and changed.** The claim is the harness's final assistant text,
capped as today. `changed` is collected from the stream's edit/write
tool-call records, repo-relative — the same accounting `runTaskChild` does
now. Usage: folded when the harness's stream reports it (claude's result
record carries usage), zero otherwise; `foldTaskUsage` already tolerates the
shape.

---

## 4. Error propagation — the two envelopes

The existing law stands: *every failure is a state and a report, never a bare
error; a node stops by a named threshold, never by wandering.* Foreign
harnesses split failure into two envelopes.

**Envelope 1 — harness failure (we detect; mechanism is generic, knowledge is
registered).**

| Class | Detection | Consequence |
|---|---|---|
| Binary missing / not logged in | **Probe at admit.** `proposeTask` runs `Info.Probe()` before the proposal event is emitted; failure REFUSES the tool call with the probe's sentence (it names the fix) | no node, no card, no countdown — the model grooms again or tells the person |
| Auth death / rate limit / context overflow mid-run | exit code + stderr tail matched against `Info.Patterns`, first match wins | `TaskFailed`, report = matched sentence + elapsed + journal pointer; branch kept |
| Crash / killed / deadline | exit status and ctx, today's paths | `TaskFailed` with the named reason, branch kept, journal intact |
| Stream garbage | total translator | invisible unless the auditor cares |

A probe result is not cached across proposals: a login fixed between two
proposals must not read as still-broken, and a login that dies between them is
Envelope 1 row 2, not a surprise.

**Envelope 2 — work failure (the auditor detects, unchanged).** A foreign
node that finished but didn't deliver is REFUTED exactly like ours
(`auditNode` reads journal + worktree evidence). The auditor cannot tell which
harness ran. That is the design working, not a gap.

The invariant a person can rely on: **any** failure leaves the journal (what
it did) and the branch (what it changed), plus a report sentence that names
the class. Doctor (`cmd/aforge/doctor.go`) gains one row per registered
harness running its probe, so "will my claude tasks run" is answerable before
any proposal.

---

## 5. Steering parity

`SteerTask` is harness-blind; the worker answers:

- `SteeringInProcess` (aforge): `enqueueSteering`, today, unchanged.
- `SteeringStdinJSONL` (claude): one framed user message on the process's
  stdin. Same law as today's steering: the person's words arrive undecorated.
- `SteeringNone` (codex one-shot): `Steer` returns an error in its own words —
  *"codex hears nothing once started; kill it and repropose with the
  correction in the brief."* An honest refusal is the consent law applied to
  steering: never a fake queue, never a silent drop.

---

## 6. Dogfood order: `aforge node` first

The first cliWorker is **ourselves**. New headless subcommand
`cmd/aforge/node.go`: `aforge node --brief-file <f> --journal <path>` runs one
brief in the current directory as a `InTask` agent and emits the journal —
a thin main over what `newTaskAgent` + `runTaskChild` already do. The
`aforge-proc` harness registration drives it through cliWorker.

Only after that seam is proven (translator total, thresholds bind, audit
reads, room renders) do `claude` and `codex` land — as **pure registrations**:
probe, patterns, argv, translator. If a foreign harness needs a change below
the registry line, the seam was drawn wrong; fix the seam, not the harness.

The in-process `aforge` harness stays the default. `aforge-proc` exists for
isolation and dogfooding; whether it ever becomes the default is a separate
conversation, not this one.

---

## 7. Agot forward-compat (do not build; do not close)

- `taskWorker` constructors receive the `*TaskNode` (id, journal path) — a
  future graph-socket env is one line in cliWorker's env construction.
- `depends_on` is already on the wire and honored by `runFrontier`; nothing in
  this change touches admission, the frontier, or the countdown.
- `TaskNotice.Harness` and the index row mean a future nested graph can say
  "node 7 is a claude node" at every level for free.

---

## 8. Non-goals

- No edge-adding CLI, no socket server, no node-spawned nodes (agot).
- No API-key management, no credential storage, no OAuth flows.
- No sandboxing beyond the worktree (bwrap/landlock is a separate hardening
  conversation).
- No changes to the old `internal/exec` subharness world; this is the v3
  session graph's layer.
- No merging/verifying changes: audit stays exactly as it is.

---

## 9. Acceptance

1. `propose_task` with `"harness":"claude"` on a logged-in machine admits,
   runs in a worktree, and lands through the same audit/merge gate; the room
   renders the run live; the journal greps like any session file; the index
   row and the card name the harness.
2. `"harness":"claude"` with the binary missing or logged out → the tool call
   is refused before any card, naming the fix. `aforge doctor` shows the same
   fact.
3. A claude node killed mid-run leaves branch + journal and a `TaskFailed`
   report naming the kill; an auth-death simulation (pattern-table unit test)
   produces the registered sentence, not a stack trace.
4. `max_steps`/`no_progress` stop a spinning claude node with the same named
   thresholds as ours.
5. Steering: claude node receives the person's line mid-run (observable in its
   journal as a user message); codex node refuses steering with the honest
   sentence; aforge node unchanged.
6. `aforge node` runs a brief headlessly and produces a valid journal;
   `aforge-proc` drives it through cliWorker.
7. Checkpoint/resume: a graph with a `harness` field restores after restart;
   pre-change checkpoints load with harness = aforge (empty default).
8. Every pre-existing task test (`task_*_test.go`, `wiring_test.go`) passes
   unchanged.

## 10. Slices (suggested)

- **S1 — seam:** `taskWorker`, `agentWorker`, registry skeleton, wire field
  end-to-end (schema → spec → notice → record → index), room re-type.
- **S2 — dogfood:** `aforge node` + `aforge-proc` through cliWorker, journal
  translation, thresholds on events.
- **S3 — claude:** probe, patterns, argv, stream-json translator, stdin
  steering.
- **S4 — codex:** probe, patterns, argv, translator, steering refusal.
- **S5 — doctor + polish:** doctor rows, TUI card/index harness display,
  docs touch-up.

Each slice ships green on its own; S1 must not change any observable
behavior.
