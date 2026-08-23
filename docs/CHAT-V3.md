# Chat v3 — a session-based surface: v1's look, v2's wiring, omp's session model

This document settles the design for the third chat surface before code. It is
written as decisions with the alternatives rejected, in the manner of
ARCHITECTURE.md. One constraint governs everything: **the tasker does not
change** — store, admission, compiler/plan, reconciler, runner, executors,
journal. v3 adds a surface and one conversational loop; it touches nothing
downstream of the journal.

**Branch law (chat-v3):** v3 is developed on branch `chat-v3` with NO v1/v2
compatibility obligation. v1/v2 surfaces, their parity gates, and their
preservation shims may be deleted freely.

**The tasker-edit exception, enumerated in full** (the only edits outside
cmd/aforge + new packages that the design requires; each is justified at its
decision): the three organizational-parent gate widenings of Decision 3
(~15 lines: fold, distill, jobRootID), the two model-pin inheritances of
Decision 8 (~4 lines: overrun, jit), and the optional plan-model rebind
variant of Decision 8 (~40 lines, only if mid-run planner rebind must outrank
a stamped pin). Nothing else in `internal/store`, `internal/resident`,
`internal/plan`, `internal/exec`, `internal/thread`, `internal/cas` changes.

## The one-sentence design

A v3 session is a pi/omp-style working session — one agent you iterate and
discuss with, holding real tools — whose finalized work is commissioned into
the unchanged tasker through the same journal seam the head has always used,
rendered by a surface with v1's visual language (conversation left, DAG rail
right) rebuilt on v2's clean wiring (narrow Backend/Commander, shell/app
layering, Bubble Tea v2).

---

## Decision 0 — What v3 takes from each ancestor

| ancestor | what v3 takes | what v3 leaves |
|---|---|---|
| **v1** (`internal/tui`, `cmd/aforge/chat.go`) | the UX concept and visual language: thread with living cards, DAG rail on the right, node drill-in with flight recorder, palettes, top bar | the 330-field god-model, Bubble Tea v1, render-mutates-model |
| **v2** (`internal/tui2`, `cmd/aforge/chatv2.go`) | the wiring: narrow `Backend` interface (engine.go:36), `Commander` seam, shell/app/blocks/tokens layering, watermark polling (300ms), session switcher, stream-event bridge, parity-gate entry pattern | the card-grammar philosophy (1 thread : N detached jobs) |
| **omp/pi** (local install; prompts at `packages/coding-agent/src/prompts/system/`) | the session model (open a project, work, resume), the exact normal-chat system prompt adapted, settings/model patterns (layered config, roles, `/settings`, model picker) | JSONL session files — our journal is the store's thread |
| **head v1** (`internal/head`) | the seam positions: mail-loop shape (`Head.Serve`), `RequestCommand` commissioning (task.go), turn-cancel handle, receipt wakes | the orchestrator philosophy (no tools, commission everything) |

The furniture v3 does not expose: triggers, `origin: self`, services, standing
watches, wake. The tasker keeps them — they are tasker machinery and the
tasker does not change — but no v3 screen, prompt line, or slash command
mentions them. Removal from the experience, not from the code.

## Decision 1 — The session agent is a worker, not a dispatcher

**Decision.** v3's conversational loop is a pi-exact agent loop with tools —
read, bash, edit, write, grep, glob, todo — plus the commissioning verbs. It
does small, immediate, reversible work itself in the workspace and hands
anything with a deliverable, real time, or a consequence to the tasker. This
is the omp normal-chat shape, and it is a deliberate break from the v1 head's
"I do none of that work myself" orchestrator doctrine.

**Basis in existing code.** `internal/exec/bare` is already a pi-0.82.1-exact
loop: same wire behavior, turn/stop/retry/compaction semantics, four tools,
message assembly and usage accounting in `loop.go`. The session agent reuses
that loop machinery with three changes: the prompt (Decision 2), an extended
belt (the four tools + grep/glob/todo + commissioning + graph reads), and an
interactive input source — between turns it drains the session's user-message
mailbox instead of exiting. Esc interrupts mid-turn through the same
`turnCancel` handle the head keeps (head.go:155).

**Where it runs.** Exactly where the head runs: in the elected resident, as a
mail loop beside `Head.Serve`, not inside the runner. Claims, turn budgets
and leases are laws for leaves; a session is a conversation, and the head
already established that conversational loops live outside the runner. Visitor
v3 windows post and poll like every other surface.

**Routing between head and session agent.** A room is marked v3 by a journaled
typed message part at open — the same mechanism v2 uses for room-switch
journaled parts (threads.go:108-132), no schema change. The head gains one
skip clause in `answerable()` (head.go:106) for v3-marked rooms. That clause
is the only line of v1-era code v3 edits; it is chat-side, not tasker-side.

**Why not keep the orchestrator.** The user's model is "a session as if we are
working and iterating" — discussing, looking at files together, trying small
things, and only then handing a finalized task to the workforce. An
orchestrator that cannot read a file cannot hold that session. The judgment of
*what* to hand over is two lines in the prompt (time/consequence test,
adapted from `orchestratorJudgment`), enforced by nothing new.

## Decision 2 — The prompt is omp's, adapted, not rewritten

**Decision.** Vendor `system-prompt.md` (250 lines) and `project-prompt.md`
(60 lines) from the omp source and adapt by substitution, keeping section
structure, RFC-2119 voice, and the delivery/verification contracts byte-near.

The substitution table:

| omp section | v3 rendering |
|---|---|
| Tool inventory (read/edit/write/bash/grep/glob/lsp/task/todo/…) | the v3 belt: read, bash, edit, write, grep, glob, todo + `task`, `change`, `stop` + graph reads (`board`, `open`, `recall`) + `ask`, `say` |
| Delegation gates (subagents) | commissioning doctrine: substantial/durable/consequential work goes to the tasker verbatim in the user's words; one ask one task; iterate on live work through `change`, never a duplicate commission |
| Internal URLs (skill://, agent://, omp://…) | dropped; graph reads are tools, not URL schemes |
| Skills & rules, memory (retain/recall) | routed durable memories, reflex extraction, and `/memory` inspection over the store |
| Workflow 1–6, Delivery contract, Critical | kept near-verbatim — this is the pi quality bar the user wants in the session |
| project-prompt footer (workstation, context files, cwd, date) | kept; context files = AGENTS.md discovery as in omp |

The prompt names the workforce and its verbs precisely, so the model can
explain the product (the `manual.Pitch` trick — the product's account of
itself concatenated, not copied).

**Rejected: reusing `orchestratorPrompt`.** It is a good prompt for a
dispatcher and the wrong one for a colleague; keeping it would make v3 v1
with new paint.

## Decision 3 — The session IS a parent node: a territory-group anchor

**Decision.** A v3 session is a real node in the graph — the parent task the
user described — and every task commissioned from the session splices beneath
it. The anchor is created once at session open with the existing store API
and the one group the tasker already treats as durable, never-executable
furniture:

```go
store.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
    ID: sessionAnchorID, Brief: "Session: " + title, Title: title,
    Group: store.TerritoryGroup,        // the whole mechanism
}}}, store.Provenance{
    Origin: store.OriginSelf,           // NOT user: a user-origin pending
    Intent: "Session: " + title,        // node pins UserIdle forever and
})                                      // starves background work (practice.go:360)
// Stays Pending forever. Never held, never folded, never claimed:
// Ready excludes grp='territory' in SQL (query.go:512); every user command
// calls it "not executable work" (thread.go:1264); Splice accepts it as a
// parent because it is pending and unfolded (splice.go:46-48).
```

This buys the exact mental model asked for: the rail is the anchor's subtree;
`@session` and `@task` are the same NodeID-message mechanism at two depths;
children splice under a non-terminal parent at any moment (`Splice` already
supports arbitrary parents — jit.go:308, craftrun.go:489, overrun.go:353,
revise.go:250 all do it today).

**The price, enumerated — the only tasker edits v3 ever needs.** Three gates
in `internal/resident` assume job roots attach directly to the spine
(`Parent == RootID`). Under an anchor, jobs attach to the anchor, and each
gate misfires. The fix is the rule the rest of the codebase already uses
(`isSurgeryJobRoot` surgery.go:515, `briefJobRoot` brief.go:503): *a job root
is a node whose parent is the spine OR whose parent's group is
organizational.* Total: three sites, ~15 lines, aligning stragglers with an
existing rule — no schema change, no command change, no behavior change for
any existing graph.

| site | breaks as | fix |
|---|---|---|
| `foldableSettledJob` (resident.go:3176) | jobs under an anchor never fold; the active view grows forever | widen gate to organizational-parent rule |
| success distill (resident.go:2131) | jobs under an anchor never distill; the learning loop dies silently | same widening |
| `jobRootID` (overrun.go:587) | JIT/overrun/repair/thread-labels attribute to the anchor, not the job | stop the parent walk at organizational nodes |

**Rejected: the held-node anchor.** `held` is user-reversible by design
(thread.go:1276 admits resume on any held node); one resume at a childless
moment and the runner claims the anchor (`Ready` has no leaf predicate,
query.go:507 — leaf-ness is a runner-side `openChildren` check). Territory
group is excluded in SQL instead: no reversal path exists.

**Rejected: virtual grouping by `provenance.session_id` (the first draft of
this decision).** Zero tasker edits, but the rail becomes a rendering lie —
tasks are roots grouped by a string, not children of the session — and
`@session` has no node to address. The user asked for the parent to be real;
the three gates above are the honest price, and they are small.

**Note:** existing territories are `Done`+folded (territory.go:259) and
`Splice` rejects settled parents — the session anchor is a *new shape* of an
*existing kind*: pending forever. No code path creates that shape today; the
session agent does, once, at open.

## Decision 4 — The surface: v1's face, v2's skeleton, nothing entangled

**Decision.** New package `internal/tui3` on Bubble Tea v2, structured like
tui2 (shell/app/blocks/tokens; app owns conversation; 300ms watermark poll on
`LatestEventSeq`; stream bridge for live tokens), rendering v1's layout
language:

```
┌────────────────────────────────────────────────────────────────┐
│ aforge · parser-lab · sonnet-4.5 · $0.42 today · ● resident     │ top bar
├──────────────────────────────────────────┬──────────────────────┤
│ you: let's rebuild the tokenizer         │ ▶ this session       │
│                                          │  ├─ ◐ rebuild parser │
│ agent: [streams; reads 2 files, edits 1] │  │   ├─ ✓ scan repo   │
│                                          │  │   ├─ ◐ write LR   │
│ ┌ ◐ task: rebuild parser · 3/7 · 2m ·12¢┐│  │   └─ · tests      │
│ │ landed: scanner.go — 41 tests pass    ││  ├─ ⏸ bench (held)   │
│ └────────────────────────────────────────┘│  └─ ✗ old parser     │
│                                          │                      │
│ you: @write-LR use the table from old/   │ (click/enter a node  │
│ agent: passed along — it's mid-turn,     │  → flight recorder + │
│ will hit it between steps                │  steer box)          │
├──────────────────────────────────────────┴──────────────────────┤
│ > message · @task to steer · / for commands                     │ input
└──────────────────────────────────────────────────────────────────┘
```

Ported from v1, de-entangled: `renderTree` (view.go:2499) fed by
`Snapshot`/`ActiveSnapshot` (query.go:396-409), the card dock
(cards.go:1168), the node flight recorder with steer input (node.go:148-484),
the design-system theme (view.go:30-130), palettes (commands.go:228), and the
vendored textinput/viewport. The god-model does not come along; state lives
in the app struct, v2-style.

## Decision 7 — One agent, two tool families, one chokepoint, and no new middleman

**Decision.** The session agent's belt has two families. **Hands**: read, bash,
edit, write, grep, glob, todo — the same wire behavior as `exec/bare`, so the
session works exactly the way a general subharness leaf works. **Workforce**:
task, change, stop, pause, resume, restart, reprioritize, expedite, set_model
— each mapping 1:1 onto an existing `store.Command` kind — plus graph reads
(board, open, recall, status) and voice (ask, say, note, forget).

**No composer is resurrected.** New work passes through the tasker's own
compile/plan — the right and only middleman for decomposition. Control of
existing work has no middleman by construction: command kinds dispatch
straight through `Reconciler.applyCommand`. Steering words travel verbatim to
a task (`change`/redirect or a NodeID-anchored message), and the task's own
planner works out what they mean — whoever holds the plan owns the meaning.
The session understands the person; the planner understands decomposition;
nothing third sits between them.

**The commit gate is code, not prompt.** Every workforce call routes through
`gate.Commander`, a thin seam wrapping `store.RequestCommand`: it stages the
command, the surface renders a countdown card (`tasks.commit_gate_seconds`,
default 5), a cancel kills the staging, expiry fires the command. Staging,
cancellation and firing journal as typed message parts. Deterministic timing,
a setting for the duration, and a complete audit trail — none of it entrusted
to model behavior.

**Module shape.** `internal/session` (the agent: Serve loop, pi-exact turn
machinery from `exec/bare`, embedded prompt templates as data, one file per
tool family), `internal/gate` (the one seam), `internal/tui3` (the surface:
app/shell/panes plus independent rail, room, gate, palette widgets over the
v2-style `Backend`/`Commander`/`Streams` interfaces). The agent never imports
the surface; the surface never imports the agent; both meet at the store.
Every seam is an interface narrow enough to fake in a test.

## Decision 9 — Compaction follows omp, minus the rasterizer

**Decision.** The session agent's context management copies omp's compaction
architecture, with the image-frame strategy left out:

- **Trigger**: `estTokens > window − max(15% of window, 16384)`, estimated
  from provider usage when present, else `len(content)/4`. Checked post-turn
  and forced on a context-overflow provider error (the alternative to
  compacting there is failing).
- **Keep-recent**: last ~20,000 tokens verbatim — capped at `window/4` so a
  small window still leaves something to summarize — cut at a message
  boundary, walking forward off any orphaned tool result whose assistant
  tool_calls fell into the summarized prefix.
- **Summary** (the one LLM call): omp's section contract — Goal, Constraints
  & Preferences, Progress, Key Decisions, Next Steps, Critical Context — an
  unanswered question awaiting the user preserved verbatim; exact paths,
  symbols, errors preserved. The transcript becomes system + summary (a
  user-role note, clearly marked) + the kept tail.
- **Persistence**: a `{"type":"compaction"}` line in the session JSONL, so
  resume rebuilds from after the latest marker with the summary as prefix.
- **Rendering**: omp's slim divider, not a message box —
  `── ⚭ compacted from ~Nk tokens ──`.
- **Settings**: `compaction.enabled` (auto passes only; overflow-forced still
  compacts), `.thresholdPercent`, `.keepRecentTokens`, surfaced by
  `Agent.Compact` for the surface's `/compact`.
- **Rejected for now**: snapcompact's PNG bitmap frames (model-tuned raster
  pipeline) — real savings, wrong first dependency; the settings shape and
  divider leave the slot open.

## Decision 8 — Per-task models: worker exists end-to-end; planner gets its missing read side

**Worker model per task: already built.** Pinned at splice
(`Provenance.WorkModel`, inherited subtree-wide, splice.go:352), changeable
anytime including mid-run via `CommandSetModel` (subtree sweep over
pending+claimed+running; running leaves finish on the model they started on,
the change lands at next claim — setmodel.go, node_model.go:65). Readable per
node (`node.Provenance.WorkModel`, or the ladder-applied
`ResolveRoleForNode(RoleWork, node)`). One gap, two two-line fixes: nodes
grown *after* the splice (overrun replan overrun.go:343, JIT jit.go:272)
build fresh provenance and drop the pin — copy `WorkModel`/`RunModel` from
the parent, exactly as restart already does (surgery.go:196).

**Planner model per task: the write side exists, nothing reads it.**
`store.SetRoleBinding` + `ResolveRole(RolePlan, nodeID)` implement
pin → node → task-scope → global → default (role_bindings.go:560), and the v2
model chip already *writes* task-scope bindings — but every planning call
site snapshots one process-global `planClient`. v3 wires the read side: one
~20-line resolver (`planClientFor(graph, jobRoot)` → role ladder →
per-model client pool, falling back to the process slot) threaded through the
six planning call sites, all of which live in cmd/aforge (boot wiring v3
rewrites anyway): planSubtree (chat.go:4090), replanRemainder (:4383),
taskContract (:4265), the jit planner closure (jit.go:34), reviseOn (:3472),
reviseForUser (:3518). No store schema change, no new command kind; a mid-run
planner change is one `SetRoleBinding(RolePlan, TaskScope(root))` journaled
by the existing set_model path.

**The task room header shows both, live.** Worker model from node provenance;
planner model from `ResolveRole(RolePlan, jobRoot)`. Changing either from the
room is a palette writing the same mechanisms — the user asked to see and set
these per task at any time, and that is exactly what the ladder was built for.

## Decision 5 — Every control interaction is an existing journal verb

The user-facing control inventory, mapped to mechanisms that exist today.
**No new command kinds, no tasker changes.**

| interaction | mechanism | status |
|---|---|---|
| talk / iterate / discuss | session agent turn loop over the thread | new loop, same substrate |
| finalize a task or plan → tasker runs it | `RequestCommand{Kind: Splice}` from the agent's `task` tool (head/task.go:22 pattern) | exists |
| pause a node | `CommandPause` (surgery.go:29; running leaf finishes current turn) | exists |
| resume | `CommandResume` (surgery.go:65) | exists |
| cancel | `CommandCancel` (resident.go:1907) | exists |
| redirect ("no — do this instead") | `CommandRedirect` → re-plans the remainder against the new words (redirect.go:103) | exists |
| replan | redirect *is* the replan verb; a from-scratch redo is `task(amends=…)` or cancel + re-task | composed, no new verb |
| amend delivered work | `task{amends: id}` (head/task.go) | exists |
| restart failed/cancelled | `CommandRestart` (surgery.go:115) | exists |
| reprioritize / expedite | `CommandReprioritize` / expedite (surgery.go:94, redirect.go:156) | exists |
| change a node's model | `CommandSetModel` (setmodel.go:28) | exists |
| **@task a message** | `PostMessage{NodeID}` → that leaf's steer mailbox, drained between its turns (chat.go:774 → `exec.Task.Steer`) | exists |
| go inside an atomic task | node pane: flight recorder + steer box | port from v1 |
| worker/consent questions | `AskQuestion`/`SurfaceQuestion`/`ResolveQuestion(+WithCommand)` (agent_questions.go) | exists |
| interrupt the agent mid-turn | Esc → turn-cancel handle (head.go:155 pattern) | port |
| queue messages while agent works | journal rows; the agent drains between turns | free |
| attach from a second window | visitor surface: posts + polls; promotion via the lease (residency.go) | exists |
| switch / resume sessions | session table + switcher | port from v2 |

**Honest gaps, accepted:** no push subscribe — 300ms watermark polling is what
v2 already proves sufficient; steering lands between executor turns, not
mid-tool-call; the agent's un-journaled scratch (in-flight tool context) is
rebuilt by refold after a crash, same as the head today.

## Decision 21 — The control plane: four named hooks, and recovery as a citizen

**The turn has four seams, and they have Harness-R1's names**
(https://arxiv.org/abs/2608.02276, `internal/session/hooks.go`):
`episode-init` (starting state), `pre-decision` (shape the context before the
model decides), `pre-action` (canonicalize or **veto** a proposed call before it
hits the environment), `post-feedback` (read the observation, trigger recovery
when the trajectory stalls). Nothing about the harness's behavior changed when
they landed: the stub pass (D16) became the first `pre-decision` citizen, the
approval gate with the guardian inside it became the first `pre-action` citizen,
the loop detector became the first `post-feedback` citizen, and the loop window
became `episode-init` state. What was bought is the FIFTH mechanism — every
future guardrail, retrieval or recovery move plugs into one named place, with one
ordering law and a test that reads it, instead of another line in the turn. The
registry is built PER TURN (`Agent.newEpisode`), because two of its citizens hold
state that must not outlive a turn, and the episode is a required argument of
`executeTool`, so a new call site cannot reach a tool without passing the gate.

**Revert-then-refix is the third rung of the stuck ladder** (PMCoder,
https://arxiv.org/abs/2608.06811, `internal/session/recovery.go`). Words are the
right first move and a poor third one: by the third repetition what stands
between the model and a working approach is usually the half-finished edit it is
reading back. So the escalation the person already gets (D16) now carries the
move — *"stuck: write ×3 — revert the 2 files this turn touched and retry from
clean?"* — with three answers: **revert+retry / keep going / stop**. Only the
person can trigger it, never a timer, never a task node, never a headless run.
A file the turn CREATED is deleted; a MODIFIED tracked file is restored with
`git checkout --`; anything else — no repository, untracked, outside the
workspace — is left alone and NAMED ("not under git — restore by hand"), because
a model re-attempting on a base it wrongly believes is clean is worse than no
recovery at all. The set of files comes from a ledger written by two hooks that
straddle the execution: `pre-action` stats the target (the only moment anybody
can know whether a write creates or modifies), `post-feedback` records the calls
that actually succeeded. The person's choice, what moved and what did not are one
steering note in the journal. The stuck question never writes a consent memo — it
borrows the consent lane but is not a question about a tool.

**Hysteresis is the law of the ladder** (PMCoder's phase hysteresis): forward
progress is accepted immediately, backward movement needs repeated evidence. One
successful call the turn has not been nudged about resets the streak outright; an
already-named signature must repeat TWICE MORE before the ladder climbs, and the
streak resets on each escalation. A batch that both repeated and got something
done reads as progress. The asymmetry is deliberate — escalating costs the
person's attention, and being wrong about progress costs nothing.

## Decision 24 — The generalization gate

Every harness edit — human, sliced, or one day automated — passes one
admission test before it lands (HarnessCompass): reject anything that names a
specific task instance, test function, or private symbol; what is admitted is
a reusable decision criterion plus an applicability condition that holds on
unseen work. And the two tracks never mix: capability edits are CODE (tools,
middleware, task nodes), guidance edits are PROMPT (system.md, memory) —
guidance encoded as executable logic has no reliable trigger, and capability
hidden in prose has no enforcement. This is the user's standing no-hardcoding
law made checkable, and it is the gate any future self-modification answers
to.

## Decision 16 — The guardian, the nudge, the stub, and the frames ladder

**Auto-approval is a model's judgment, opted into.** A `Prompt` decision with
`approval.guardian: on` first asks the guardian role (low tier) with the
tool, args, and matched rule under a binary ALLOW/ASK contract; ALLOW
executes with a dim note, ASK or any error falls through to the person
unchanged. Off by default — a gate that answers for the person must be
chosen, and the person's own rules always outrank it.

**Stuck is detected, nudged, then escalated.** A sliding window of tool-call
and error signatures: the same call ×3 or the same error ×3 injects one
rethink note into the next request and raises `stuck? nudged` in the
surface. The third repetition in prompt-mode escalates to the person
through the consent lane — the person is the better nudge by then.

**Tool output is stubbed, never deleted (omp's shake).** Results older than
four turns and over 1500 bytes are replaced in the *live* transcript with a
bounded stub naming the artifact path the full bytes were written to. The
journal is never stubbed; the record stays whole.

**Compaction is a ladder, SOTA-ordered** (researched, D9 amended): rung 1
stub (deletion — best fidelity per cost, runs every turn); rung 2 *frames*
(omp's snapcompact — the discarded prefix rasterized to PNG pages attached
as image parts; full fidelity, no LLM call; now reachable via `x/image`
since vision support exists), used when the model sees images; rung 3 the
LLM summary (D9) for blind models and focus-text passes. LLMLingua-style
token pruning is rejected: a local scoring model is a heavy dependency, and
pruning inside code is where compression lies.

**Images generate and see.** `generate_image` saves to a path, never bytes
in context (registers only when an image model resolves; default
`google/gemini-3.1-flash-image`). A blind chat model never refuses a
picture: the `vision` role answers it one-shot, journaled with a
`[vision: model]` note; refusal only when nothing resolves.

## Decision 15 — Honest numbers, cache affinity, and the thinking window

**The meter tells the truth.** The context meter reads the session's own
estimate (provider-reported usage when known, full content estimate
otherwise — tool outputs and the system prompt included), displayed as
`12.4k/128k · 10%`, accent past 80% of the compaction threshold. A percent
of an undercount is a number that means nothing.

**The session is a cache lineage.** Every request stamps the session id as
the prompt-cache-affinity key (bare sends none by design — a leaf is not a
lineage; a session is). Cache reads/writes accumulate per turn and session;
a turn with cache reads shows a dim `⟲ 9.8k cached · saved $0.0041`, the
status line carries the session's cached share, and "saved" is computed from
the model's real prompt/cache prices (withheld when pricing is unknown — a
router's "-1" never reads as free).

**Thinking is a window, not a wall.** Streaming reasoning shows only the
last three lines on a true-color opacity gradient (oldest fading to
background, newest at dim ink) with a live `⠿ thinking · N tok` counter;
completion collapses to `⠿ thought for Ns · N tok · ctrl+e`.

**Identity is hue; markdown is weight.** The user's text renders in the
pastel accent for its whole body — an assistant answer full of bold can no
longer read as the user. Bold-as-identity is retired.

## Decision 13 — Search is a plug registry; the default costs nothing

**Decision.** `internal/search` is an open registry of providers, not an Exa
client. `Provider` (search) and `Fetcher` (page → clean text) are separate
registries resolved independently. Auto resolution: a settings pin wins;
else the first keyed plug that is available (Exa when `EXA_API_KEY` exists);
else the zero-key default — DuckDuckGo's HTML endpoint for search (no key,
scraping is the price of zero-config and the registry is the upgrade path)
and Jina's `r.jina.ai` reader for fetches (free, 20 RPM, no key). Tavily or
Firecrawl later is one file each: the registry, not the belt, is the
extension point. The belt tools (`web_search`, `web_fetch`) register only
when a provider pair resolves — a model told about a tool it cannot reach is
worse than no tool.

## Decision 14 — Images are context by reference, never by inline journal bytes

**Decision.** The surface attaches images to a turn (`Agent.SubmitImage`):
text + `image_url` parts on the user message, gated by the model's advertised
input modalities (a model never receives parts it cannot read). The journal
stores a *reference* — path, sha256, mime — never the bytes; replay re-reads
the file when the hash matches and writes an honest placeholder when it does
not. Snapcompact-style rasterized-context frames (omp's bitmap trick) remain
a documented extension: the savings are real, the rasterizer dependency is
not yet.

## Decision 12 — Streaming intelligence and background jobs

**Reasoning is visible, then collapses.** The provider surfaces reasoning
deltas (additive `StreamReasoning`, parsing both `reasoning` and
`reasoning_content` wire fields); the session forwards them as
`EventReasoning`. The surface renders thinking as a muted, lower-tier
streaming block while it runs, and collapses it on completion to a one-line
`⠿ thought for Ns` row the person clicks to expand. Reasoning never enters
the conversation transcript — it is display material, like tool glosses.

**Read-only tools start as their calls stream in; mutating tools never do.**
The SSE decoder marks a tool call complete the moment its arguments close
(`StreamToolCallReady`), and the loop starts read-only calls (read, grep,
find, ls) immediately, concurrently with the still-streaming response.
Mutating calls (write, edit, bash) wait for response completion exactly as
before, because a retryable mid-stream failure would otherwise run a write
twice; a read is idempotent, so a retry re-running it is harmless. Journaling
is unchanged: results record after the response completes, in call order.

**Background jobs ride three mechanisms that already exist.** `bash` gains an
optional `background: true` (a session-side wrapper — bare is untouched): the
call returns immediately with a job id and a log-file path. Output lives in a
bounded in-memory ring plus the full log spooled to disk, so the model pages
the file with `read` instead of hauling bytes through context. A `jobs` tool
lists, tails (default 50 lines, capped), and kills. Completion is
event-based: an exit note rides the *existing steering lane* into the next
batch boundary — the model learns without polling, and no new push machinery
exists. A cleverer per-job summarizer is a documented extension slot, not
built: bounded tails + exit notes + disk logs answer the context-pollution
question without an LLM in the middle.

## Decision 11 — The rendering law: four tiers, one accent, tools as a living cluster

**Decision.** The surface's typography is four tiers and no fifth colour —
ink (body), accent (headings, the user's gutter glyph), dim (meta, tool
glosses, receipts), status hues (muted green/red). Violet stays reserved for
the question state. User messages are `›` + bold; assistant messages plain
ink; role labels and bubbles are noise and do not exist. One blank row before
a user message is the only spacing the layout pass ever inserts — tool
clusters and assistant blocks carry none.

**Tool calls render as a cluster, not a stream.** One line per call with a
box-drawing rail marker — `├─▶` middle, `╰─▶` last (ASCII tier `+-> `);
expanded detail rows continue the rail `│ `. Tool name in muted accent; the
*target* (file, command, pattern) in primary ink — it is the substance.
**No success glyph, ever** — a quiet line is a success; only failure speaks,
as a soft red `✗`. While running, a spinner animates in place at the line's
right end. At most the three most recent calls are visible; older collapse
to a single `↳ N earlier tool calls` line. `ctrl+o` or a click expands the
turn's calls; a click on one line expands that call's detail; click again
collapses.

**Every tool has an inline stat and a tool-specific expansion** — derived in
the surface from the event's `Args`/`Output` payload (pi schemas are stable;
presentation derivation is surface business):

| tool | inline stat | expansion |
|---|---|---|
| edit | `+N −M` (pastel green/red) | unified diff computed from old/new strings, 2 context lines, `+`/`−` painted, capped 40 with `… N more` |
| write | `+N lines` | content preview |
| read | `· N lines` | the returned chunk |
| bash | `exit N` on failure only | output + exit line |
| grep | `· N matches` | the matches |
| find/ls | `· N entries` | the listing |

**The palette is pastel, dark-terminal first, designer-curated** — soft, low
saturation, never loud: body ink soft white (#D8DEE9), accent pastel blue
(#9DC3E6) for the one live or chosen thing, dim #6B7280 for meta, diff and
match accents in nord pastels, failure soft orange-red (#D08770), and the code
fence theme a pastel chroma style (catppuccin-mocha where available, else the
tokens ramp with pastel overrides). One rule above every choice: if a colour
could be described as "bright", it is wrong.

The exact values, the four-step ground ladder and the laws that govern which
role paints what all live in **docs/DESIGN-LANGUAGE.md** and in
`internal/tui3/styles.go`, and they have moved since this decision was written —
read them there rather than from this paragraph.

**Spacing law.** One layout pass owns all spacing: one blank row before a
cluster that follows text, zero between cluster lines, one after a cluster
before text, one before each user message. Nothing else, from anywhere.

**Markdown is real.** Model text renders through `internal/tui2/prose`
(goldmark AST + chroma with the token layer's colours — glamour is rejected
by the same law as v2: no second colour authority). Headings promote by tier
(h1 accent+bold), code fences highlight, tables truncate rather than soup.
While a turn streams the live block renders as plain text; markdown replaces
it on settle (and on a 1.5s throttle for long turns). Repaints coalesce to
~30fps; entries hold pre-rendered rows; a frame joins visible rows only.

**Todo is dropped, from first principles** (user question, settled here).
Todo's four responsibilities each have a better-placed owner in v3:
decomposition-forcing → the plan note (a visible, numbered message in the
transcript) for session-scale work, the tasker's compiler for commissioned
work; progress state across a long sitting → the commissioning doctrine
(long sittings ARE tasks; session turns stay short), with the DAG rail for
visibility once commissioned; user visibility into intent → the plan note,
which the person can read (a hidden todo list never offered that);
compaction survival → the summary's Progress/Next Steps sections (D9). Todo
is a *list* — weak self-written ordering, no dependency semantics; with a
real DAG one layer down, list items that are real work are commissions, and
the chain is the degenerate case the DAG already generalizes. Reversible by
measurement: if solo-session drift shows up, re-adding is one line in
tools.go.

## Decision 10 — Steering follows omp's two-queue model

**Decision.** Typing while the session agent works produces two kinds of
messages, exactly omp's split:

- **Steer** (plain Enter mid-turn): injected into the running turn at the
  next tool-batch boundary — the agent sees it between steps. v3's
  `Agent.Submit` during a turn already implements this; every Submit returns
  a live fan-out channel over the turn's event hub.
- **Follow-up** (`ctrl+q`): queued to start a fresh turn the moment the
  current one yields.

Dequeue is one message per poll by default (`steering.mode:
one-at-a-time`); `all` flushes the queue at one boundary. The queue renders
above the input as a dim enumerated list ("Steering · 2", "After yield ·
1"); `alt+up` pops the last queued message back into the input. Interrupt
(esc) clears both queues — a drain must never auto-resume a turn the person
just stopped. The display counts only user-authored messages.

## Decision 6 — Settings and models follow omp's pattern on aforge's registry

**The panel is schema-driven, like omp's.** omp's `/settings` is a fullscreen
overlay where every setting is declared once with a `ui: {tab, group, label,
description, options?, condition?}` block and type mapped to a widget
(boolean → inline toggle, enum → cycle or select submenu, string → text
submenu masked when credential). v3 adopts the mechanism over aforge's config
registry: each registry key gains a UI metadata block; the panel renders
tabs; type-to-search filters across tabs; changed-from-default rows are
marked. Tabs for v3 (aforge-shaped, not omp's eleven): **Session** (model
roles, thinking, commit-gate seconds), **Context** (compaction.*, steering
mode), **Workspace** (tool approvals, bash timeout), **Display** (theme,
rail open/closed, nerd-font tier), **Providers** (base URL, timeouts,
per-role models). Adding a setting = one registry row with a ui block.

**Model roles and pickers.** aforge's slots + role ladder map onto omp's
roles: `default` (session talk), `work` (tasker workers), `plan` (tasker
planner), `cheap`/`strong` rungs. Two pickers, omp-shaped: a compact
bottom-anchored `alt+p` for session-only switches (role assignments
untouched), and a full `/model` hub with a roles sidebar (badges, cycle
order, thinking suffix `model:high` in role values). Persistence through the
existing registry + Prefs.

**Layering**: built-in defaults ← `~/.aforge/config.json` (existing registry)
← `<workspace>/.openaf/config.json` (new project-local layer, surface-side)
← flags/env.

**Welcome/resume, omp-shaped**: on open, a two-column welcome — left logo +
model + workspace, right the four most recent sessions for this cwd
(fixed-height slots); `--continue` resumes the latest, `--session <path>` a
specific one, a `/sessions` picker lists them (title/first-message preview,
date, size). The session file is the JSONL transcript (internal/session),
so resume is exact.


| milestone | lands | acceptance |
|---|---|---|
| **V3-0** skeleton | chatv3 gate; `internal/session` agent (bare-loop machinery, omp-adapted prompt, four tools + grep/glob/todo + task/change/stop + board/open/recall + ask/say); minimal tui3 (top bar, conversation, streaming, input); resident wiring + head skip clause | talk; agent reads/edits/runs in the workspace; finalize → task lands and runs; Esc interrupts; restart resumes the thread |
| **V3-1** the workforce on screen | DAG rail scoped to session; cards + dock; node drill-in + steer; `@tag` steering; control palettes & slashes (pause/resume/cancel/redirect/restart/model); question UX | every Decision-5 row drivable by keyboard and mouse |
| **V3-2** omp comfort | settings panel; model picker per role; project-local config; session switcher/welcome; compaction polish | settings/model flows match omp muscle memory; crash mid-session loses nothing journaled |
| **V3-3** cutover | delete `internal/tui`, `internal/tui2`, `internal/head`, the chatv2 gate; `aforge chat` = v3 | the repo has exactly one chat; tasker packages diff-free |

## Decision 17 — One picker, a question per slot, and the two things a row says

**Every model row is a model choice, and the picker asks that row's own
question.** The two tier rows and the vision row were text boxes — a person
typing an id from memory in front of a catalog that knows every one of them —
so all three now open the same filterable picker `/model` opens, and which
models it offers comes from ONE predicate (`modelFilter`) chosen from the row
key (`filterFor`) rather than from a list assembled at each call site: the chat
law is text out **and** text in (the input side is what keeps the transcription
family out — whisper answers in text and takes sound), the looking row asks for
models that publish an image input, and a row that publishes nothing on a side
is read by its id against a name vocabulary that a published modality list
always overrides.

**The selected row is one band and the running row carries a clock.** Selection
used to bold the label and leave the tail — the window, the price, the arena
score — dim grey on the one row a person was comparing them on; it now paints
the whole line, lead to note, at the terminal's full width, in a background one
step louder than hover's so the pointer never reads as the cursor. And a
running tool line trails its own age beside the spinner (`⠿ running · 1m 5s`,
compact: `12s`, `1m 4s`, `2h 5m`), which stops at completion where the
finished-call figure takes over. Nothing new ticks for it — the frame clock
that already turns the spinner redraws it, so the count-up costs no wakeup.

## Decision 18 — The event-driven watch: the harness polls so the context does not

**Watching is a primitive, not a habit.** Re-running `tail -50 app.log` every
turn spends a round trip and fifty lines to learn two — Claude Code and omp
offer background tasks and polling, and polling is what fills a transcript
with near-identical chunks. So the `watch` tool hands the timer to the
harness: it runs the command every N seconds (default 10, floored at 2,
capped at an hour, each tick bounded by `min(every, 60s)` so a hung run
cannot stall the loop) and speaks *only when there is news*. `on: change`
diffs against the previous tick and delivers the new lines — the
suffix/prefix overlap, so a scrolling `tail` yields the two lines that
arrived, not the fifty in the window; `on: match` delivers only the new lines
a regex selects; `on: always` delivers the last ten every tick, for a number
that is meant to be watched climbing; `until` ends the watch on the first
line matching it and delivers that line as the final note. **The first tick
is a silent baseline** (except `always`): there is no "new" without a
"before", and a baseline delivered as news is the chunk this tool exists to
stop sending. Notes cap at 40 lines + `… N more`.

**A watch is a job, and its note rides the steering lane.** It comes from the
same registry as `bash background: true` — one id space, one log file on
disk, one `jobs` list (kind `watch`, with its name, terms and tick count),
one `jobs kill`, one death at `Close` — because everything around a watch is
what a job already is; only the middle differs, which is a `jobKind` and a
stop function, not a second machine. Delivery reuses Decision 12's mechanism
verbatim: `enqueueSteering`, drained at the next step boundary as plain user
text. No push, no new event, no surface change. Two governors keep a timer
from burning a session down: **three watches at once** (the fourth is a tool
error naming the limit), and **three consecutive identical failures** end the
watch with one note — a watch spinning on a broken command is the loop
detector's cousin, and the honest answer to repetition carrying no
information is to say so once and stop.

## Decision 19 — Native tasks: work leaves the conversation as a graph node

**The destination is a DAG, and what ships is one node of it.** A complex task
decomposes into nodes with dependency edges; an executor runs the READY
FRONTIER — every node whose dependencies are done — in parallel; a finished
node's report becomes part of its dependents' briefs. `propose_task`
(`internal/session/task.go`) is how the first node is born, and every mechanic
under it is the graph's own: the proposal is a node, the countdown is how a
node is admitted, the worktree is how a node is isolated, the report is what a
node hands the work after it. There is no "depth", no "nesting" and no level
counter anywhere in the slice — one level today is simply a belt without
`propose_task` on it, and decomposition, when it lands, is **edges added to
this same executor**, not a second machine. `depends_on` is on the wire and
honoured by the scheduler from day one; the model just has no sibling to name
yet.

**The brief is the node's whole world.** A node never reads the conversation:
it gets a title, a summary, a self-contained brief, and an observable
acceptance condition, and it is submitted as `brief + "\n\nAcceptance: …"` to
a child agent in this same package — same loop, same hands, same prompt. The
brief is assembled **when the node starts, not when it was proposed** (JIT):
its own text, then `What the work before you learned` and its prerequisites'
reports, which did not exist at proposal time. That is the whole reason
assembly lives in the executor rather than in the tool.

**The proposal IS the consent, and it has a clock.** The call blocks and emits
`EventTaskProposal` with a deadline; four things end it — approve, redirect
(approval plus the person's words appended to the brief as *"The person
redirecting this task says: …"*), decline (a plain tool RESULT, never an
error, which the model reads as grooming feedback), or **the clock, which
approves**. Silence is a yes because the countdown is a window to redirect
work the model has already groomed, not a gate the work waits behind: a
surface that draws no answer box still works, and every task starts after five
seconds. The countdown is `task.autoapprove_seconds` (default 5, 0 = wait for
an answer). **A headless run never waits**: with nobody subscribed, the
deadline approves whatever the setting says, 0 included — consent.go's law for
a question with no reader.

**The conversation may choose the hands.** A node inherits the model the
conversation is on, because a node is the same worker doing the same job
somewhere quieter — that is the default and it is almost always right. What the
person can do is override it in words ("let opus handle this one", "something
cheap for the sweep"), and `propose_task` carries an optional `model` for
exactly that: the model that grooms the work is the one holding both halves of
that sentence. The word is resolved against the models this install actually has
(`internal/session/taskmodel.go`) — the whole id, the vendorless tail, then every
token anywhere in it — and there are three answers and only one is an error. One
match is the decision. **More than one is a question, not a guess**: the
shortlist rides on the proposal the person is already being shown, the closest
match is preselected, the digits pick between them, and the countdown keeps
running, because an ambiguity the harness raised is not a reason for work to
stop. Nothing at all is a refusal the model can act on — an ordinary tool result
naming the nearest ids — and so is a word that fits half the catalog, because a
shortlist of eleven is a list rather than a choice. A surface holding no catalog
(`Config.TaskModels` nil) validates nothing and takes the word as written: that
is "nobody can say", not "there are none". The default when nothing is named is
`task.model`, and the conversation's own model when that row is blank; the
proposal, the rail row where the column can afford it, the room header and the
landed card all name what the work is running on, and the node's model is on its
checkpoint so a resumed graph starts where it was sent.

**A worktree is a branch of the tree, and merging is how work bubbles up.**
Each node runs in `git worktree add` on `task/<slug>-<shortid>` off the
person's current HEAD, under `<repo>/.aforge-v3/tasks/<id>/`, so its
half-finished sweep is never what the person's build compiles. On success the
node's work is committed on its branch and merged into the person's — clean
means the worktree and branch are removed (`merged`), a conflict means
`merge --abort` and the **branch and worktree are kept** and named in the
report (`conflicted`). The merge is attempted whatever the person's tree looks
like: a dirty checkout is the normal state of somebody working, and nothing a
node wrote is ever thrown away. A workspace that is not a repository (or has
no commit to branch from) runs **in place** and says so — pretending to
isolate is worse than not isolating.

**A node is a job, and it never asks anybody anything.** It comes from the same
registry as `bash background: true` and `watch` (kind `task`): one id space,
one log, one `jobs list` row, one `jobs kill` — which cancels it, keeps its
branch, and marks the merge `aborted` — and one death at `Close`. Its approval
posture is composed from `internal/approval`'s own pieces: allow everything,
with the critical table still a floor, and a decision that would have asked a
person is refused in the node's words (*refused in a task: … — nobody to ask*).
Its belt is the conversation's minus two: no `propose_task` (nobody to show a
proposal to) and no `watch` (no conversation for the news to arrive in).
**Two nodes run at once**; a third runnable node queues on the frontier rather
than erroring the tool, because queuing is what a graph does. Every node is
bounded at 30 minutes, its spend folds into the session's auxiliary usage, its
journal is a real session file under `~/.aforge/v3/tasks/<session>/`, and its
completion reaches the model on the **steering lane** (Decision 12) while
`EventTaskUpdate` reaches the surface — during a turn on the turn's stream,
and always on `Agent.TaskUpdates()`, because a node's most important event
lands minutes after the turn that proposed it ended.

**THE FRONTIER IS VERIFIED: a node never marks itself done.** Before the
auditor, a node's done-state was its own last words — the child stopped calling
tools, wrote a confident sentence, and the graph wrote that down as `done`.
Everything downstream (the merge onto the person's branch, the dependents'
briefs, the note in the conversation) was built on an executor's self-assessment
of forty steps it had spent reasoning about its own intentions. LongHorizon
Harness names the fix in one line — task state is updated **only from
independent audit evidence; executor self-reports never flip a record to
completed** (`harness-research-notes.md` §1, arXiv:2608.01964) — and
`internal/session/task_audit.go` is that line, made structural.

When a run finishes, a **fresh auditor** is pointed at the node's worktree: a
different agent, no shared context, no sight of the trajectory, on the HIGH tier
(`roles.RoleAuditor` — the one role where the tier is not an economy question,
because a wrong verdict either lands broken work or throws good work away). Its
belt is **composed, not filtered**: `read`, `grep`, `find`, `ls`, and a `bash`
that runs a configurable allowlist of verification — `go test`, `go build`, `go
vet`, `git diff/log/status/show` — and refuses everything else, shell
composition first (a prefix check alone would admit `go test ./... && rm -rf .`,
so operators are rejected before the allowlist is consulted at all). There is no
hand here that writes, because an auditor that could fix what it found would be
an executor with a second name, and the first thing it would do is repair the
thing it was sent to judge and then report success. The node's work is **staged**
before the audit (`git add -A`, minus the harness's own droppings) so `git diff
--cached` shows new files too, and the commit that follows a pass comes from the
same index.

The contract is two words, for the guardian's reason (Decision 16): a verdict
with a middle answer has a middle answer nobody defined. `VERIFIED — <what I
ran, what I saw>` or `REFUTED — …`, at most three lines of evidence, and the
verdict **leads the node's Report** so the first thing a person reads off a
finished card is the reason to believe it. Only VERIFIED reaches `comeHome`, so
only verified work is ever merged. **REFUTED is `TaskFailed`** with the
auditor's evidence as the report — the node's own claim does not survive its
refutation — and the existing cascade fails its dependents with it, which is
exactly right: work built on work that does not hold is work built on nothing.
The branch is **kept** either way (`aborted`), as a killed node's is. Everything
that is not the word VERIFIED refutes — an essay, an empty reply, an auditor
that would not start, a five-minute timeout (`audit timed out`, spelled
differently because "the auditor looked and says no" and "nobody ever answered"
are the same state and very different news). **The frontier fails closed**, so
the worst a broken auditor can do is leave good work on a branch with an
explanation attached. The audit is a real session file of its own beside the
node's (`<when>_<id>-audit.jsonl`) and its spend folds into the same auxiliary
pocket the node's does.

**A node stops by a NAMED THRESHOLD, never by wandering.** Argus terminates on
named thresholds rather than on a reviewer's judgement (§1, arXiv:2608.05144),
and a node now has three: its 30-minute deadline, a **step budget**
(`max_steps`, default 40) and a **no-progress count** (`no_progress`, default 6
consecutive steps that changed no file). A step is one finished tool call —
the only unit visible from outside the child's loop — and *progress* is
narrower still: a **successful** `edit` or `write`, because an edit whose
`oldText` did not match changed nothing and repeating it is the exact spin the
counter exists to catch. Tripping either cancels the child and the report names
which (`stopped: 6 steps without a change`), instead of leaving half an hour of
silence for the deadline to collect. Both are per-node on the wire, because the
right budget for a one-file rename and for a sweep across forty files is not the
same number; a negative one is a stated error rather than a silently substituted
default. The no-progress default is deliberately tight — it catches a spin in a
minute — and a node with real reading to do before its first edit is expected to
say so with `no_progress`.

**The goal contract has two tiers, and the line is admission.** Argus again:
semantic clarifications move freely, the precise objective moves only with
recorded authority. Here the semantic tier is everything *before* `admit` — the
model grooms the brief, the person redirects and their words are appended in
their own voice — and **after admission `spec.brief` and `spec.acceptance` are
frozen for the node's life**. Nothing in `runFrontier` writes them; a late
answer to the same proposal moves nothing; a correction is a NEW admission by
the person, which is them exercising the same authority a second time. The
reason is the auditor: a frontier that verifies work against an acceptance which
can move while the work runs verifies nothing, because whoever holds the pen can
always make the work pass. One acceptance, two readers — the instruction the
child is finished against and the contract the auditor judges — so there is no
version of this where the work was finished against one text and graded on
another.

**THE FRONTIER IS DURABLE: the graph is a checkpoint, and recovery is a pure
function of it.** Until `internal/session/task_store.go` the graph lived in
exactly one place — memory — so killing the app mid-run destroyed not the work
but the *knowledge of it*: which nodes were admitted, what they were briefed
with, which had already landed and what their reports said, and above all that a
branch called `task/fix-the-reconciler-9c1a2f` is sitting in the repository with
somebody's half-finished work on it and nothing left alive that knows why. The
worktree survived; the record did not. The resume contract is
`harness-research-notes.md` §7 (arXiv:2608.03836) in three rules, machine-checked
against real frameworks and violated by several of them:

- **Checkpoint after EVERY transition, not at exit.** A killed process never runs
  its shutdown path, so a checkpoint that is only correct at exit is only correct
  when nothing went wrong. Admitted, running, the working copy prepared, the
  verdict in, done/failed — each writes `<journal>.tasks.json` (the per-journal
  convention `state.go` established, because a graph belongs to ONE conversation
  and the journal is what names one) through tmp + `rename`, so the file is
  always a whole graph somebody wrote and never half of two. The *running* write
  lands **before** the run it authorizes starts, and the worktree and branch are
  written the moment the node has them — that record is the only thing that can
  later tell a person where interrupted work went.
- **Loading is schema-validated, and a violation drops the file WHOLE.** Version,
  type tag, one record per node (id, title, brief, acceptance, `depends_on`,
  state, report, changed, branch, worktree, merge, thresholds, elapsed), and the
  edge rules that make it a graph: an edge to a node the file does not contain is
  a brief that can never be assembled, and an edge pointing *forwards* is a cycle
  the frontier would wait on forever, since ids are minted in admission order and
  an edge can only point backwards. One log line, never fatal — `state.go`'s law,
  for `state.go`'s reason: a bad byte in a bookkeeping file must not cost the
  person their conversation.
- **An interrupt is consumed by exactly one recovery.** A node that was RUNNING
  when the process died is work nobody will finish and nobody will audit. It
  comes back **failed** with a report that says so and *names what is on disk* —
  `session ended mid-run; branch task/… kept, its worktree is at …` — after a
  real check that the branch is still there, because promising work on a branch
  the person has since deleted is worse than saying nothing. The checkpoint then
  **records that the interrupt was consumed**, so the next resume reads plain
  history rather than interrupting the same node twice.

**Recovery is load, reconcile, continue — and there is no second scheduler.** At
`newAgent`, a journal with a checkpoint beside it (same journal = resumed
session) rehydrates: done and failed nodes return as history with their reports,
leavings and frozen specs intact; the running node is interrupted as above;
queued nodes return queued; the id counter carries on so a resumed session never
mints an id some sentence in the transcript already means something else by. Then
the ordinary `runFrontier` turns — a queued node whose prerequisites are done
starts *now*, with those reports assembled into its brief (JIT, exactly as if
nothing had died), and a queued node whose prerequisite was interrupted fails
through the cascade that already existed.

**A completion is announced exactly once, across lives.** A resumed done node
must not re-notify: its note is already in the transcript the journal replays,
and repeating it would tell the model that work it has read about has just
happened. So the checkpoint records whether each node's completion note was ever
handed to the steering lane, and recovery delivers a note only for the nodes that
never got one — the interrupted node, and the rare node that landed in the
instant before the process died — inside a **single journal note per recovered
graph**: `recovered task graph: 2 done · 1 interrupted (branch task/… kept) · 1
waiting`, with those owed notes under it in the shape `taskNote` always produces.
One note, one grammar, and the person can see what survived.

**A NODE IS A PLACE, AND IT HAS THREE DOORS.** Everything above treats a node as
work you hand off and stop thinking about, which is the right shape for delegated
work and the wrong shape for the moment a person changes their mind: the node is
running, they can see it going the wrong way, and the only thing they could do
about it was kill it and propose the corrected task again.
`internal/session/task_room.go` makes the node *enterable* — one running node,
one page, three doors, no more:

- **`WatchTask(id)` — the live stream.** The child agent's own events, as they
  happen: its deltas, its tool calls beginning and ending, its errors.
  `runTaskChild` already consumed that stream to count steps and read off changed
  files; it now **publishes each event to the node's room first** and consumes it
  after, so a watcher sees the child's narrative in the order it happened. The
  room is opened by whoever arrives first — the runner attaching its child, or a
  person entering a node that has not started yet — and it closes on the node's
  **own `done` channel**, so every path to a final state empties it: the runner's
  return, the frontier's cascade over a dependent whose prerequisite failed, a
  recovery consuming an interrupt, and `Close` (which kills the node's job, and a
  killed node lands). Subscribers are the same unbounded `eventStream` every
  other fan-out here uses, for the same reason: the child's loop must never wait
  on a surface that is redrawing, and must never drop a text delta.
- **`SteerTask(id, text)` — the person's words into the child's loop**, on the
  **steering lane** (Decision 12) — the same queue a background job's exit note
  rides, drained at the child's next step boundary as plain user-role text. The
  line goes in **undecorated**: a job's exit is framed because the model has to be
  told what kind of news it is, and a person's line needs no frame, because from
  the child's side it *is* what it looks like. Unknown id, a node that is not
  running, a running node whose worker is not up yet, and an empty line are four
  errors that each name which.
- **`TaskJournal(id)` — the history**, the node's real session file under
  `~/.aforge/v3/tasks/<session>/`, recorded on the node the moment its child is
  built (the name carries a timestamp, so that is the only moment anybody can
  learn it).

**The live lane and the journal are two doors for the same reason the two update
lanes are.** A watcher gets what happens **from now** — nothing is replayed,
exactly as `eventHub.subscribe` replays nothing — because a stream that
re-narrated half an hour of somebody else's greps before reaching the live edge
would make "watch this node" mean "read this node's history slowly". A finished
node therefore answers with a **channel that closes immediately**, not an error:
the id is real, the work is over, and the history is a file. Steering is likewise
**not a redirect**: nothing in this file writes `spec.brief` or
`spec.acceptance`, which stay frozen at admission for the auditor's reason above.
A line of talk to the worker ("the config lives under `etc/`, not `conf/`") is not
the objective moving; if the objective itself was wrong, the answer is still a
new proposal.

**What v1 defers, deliberately:** the decomposition tool that writes edges (the
graph and its frontier are already here to receive them), and the question lane
— the room's talk is one-way, so a node that needs to ask something today
finishes with what it has and says so in its report, rather than blocking on a
person who is having a different conversation.

## Decision 20 — The document ladder's local rung: `read` grows a sense

**A PDF is a file, so `read` reads it.** The local rung is in-binary text
extraction (`internal/pdfx`, over `github.com/AOShei/go-fast-pdf` — pure Go,
zero dependencies, MIT, compiled into the static binary, so the rung works on a
machine where nobody ran `apt-get`), and it is wired in by WRAPPING pi's `read`
(`internal/session/tools_pdf.go`, exactly as `backgroundBash` wraps pi's `bash`)
rather than by adding an `extract_pdf` tool — a belt with two hands for one
intention makes the model choose, and what it chooses is a Python script for a
library that is not installed. A scanned PDF is reported as itself — "no text
layer (N pages, images only)", not an empty read — so it falls through to the
ladder's OCR and vision rungs instead of looking like a broken file. The engine
is one swappable file by ladder design: `pdfx.Extract(path) (string, error)` is
the entire contract, and a better pure-Go extractor lands as an edit to it with
nothing above it moving.

**And the rung above it is `read_document`, because a ladder is not a tool.**
The local rung shipped saying the true thing about a scan — "no text layer; the
`document_engine` ladder's OCR rungs can read it" — and a real person's scanned
PDF then produced, in the field, `bash: pip install easyocr`. The sentence was
honest and unactionable: `internal/provider` has carried `ParseDocument` (native
file handling, `mistral-ocr`, `cloudflare-ai`) and `internal/exec` has driven it
for the workforce all along, and NEITHER WAS REACHABLE FROM A CONVERSATION. So
the rung becomes a hand (`internal/session/tools_doc.go`), and read's refusal now
names it: *"use `read_document` (the OCR rung) or paste a page as an image."*

**A second tool, not a second sense — the opposite of the local rung's choice,
and the difference is who pays.** Growing `read` was right when the answer was
local, instant and free: one hand, one habit, no decision. This answer costs
money per page, and a model that cannot tell "read a file" from "spend two cents
OCRing forty pages" will spend it on every source file it opens. **The split is
where the bill is.** It is also ALWAYS ON THE BELT, inverting Decision 13's
conditional law: `web_search` and `generate_image` are optional back ends a
surface may never have wired, while the document rungs ride the session's own key
and base URL — there is nothing to be conditional about, and a way out named in
`read`'s own refusal must never resolve to nothing.

**The rungs, and who is allowed to climb them.** `document_engine` (auto ·
local · free · ocr, `config.DocumentEngineAt`) decides, in the same vocabulary
`internal/exec` uses: `auto` walks NATIVE (the chat model's own file handling,
paid as ordinary tokens) then `cloudflare-ai` then `mistral-ocr`; `local` is
refused by name, because the local rung IS `read` and that is what already
failed; an image has exactly one rung, since the paid parsers are file parsers
and there is nothing cheaper than a model that can already see. Two guards keep
the tool from being charged for what is free: a plain-text file is sent back
("the plain read handles this"), and a PDF that turns out to HAVE a text layer is
extracted in-binary before any rung is called. A thin answer — a page number
where a page should be — escalates to the next rung, but a thin answer from the
LAST rung is the answer, because a photographed receipt really does extract to
four words and a refusal invented by a threshold is worse than a short truth.
Every failure is a RESULT NAMING THE RUNG (`mistral-ocr: 402 insufficient
credits`), never a Go error: the model can act on it, and the person reading the
transcript learns which row of settings to change. The answer obeys pi's
truncation law through the same `piReadLaw` the local rung uses — same 2000
lines, same 50KB, same offset to continue — and the extraction is MEMOIZED per
session by content digest, because paying a per-page OCR bill twice to show line
three would be the one place this ladder robs somebody.

## Decision 22 — BPE working state: what is true and what is open, outside the trajectory

**The session had experience memory and no working state.** The routed memory
store carries standing facts across sessions; everything a
turn learned about the work in front of it — which file matters, which subgoal
is half-done, which command proves the build green — lived in exactly one
place, the transcript. That is the one structure compaction destroys, so every
pass had to **rediscover the plot from the summary it had just written**.

The fix is the research's BPE abstraction (harness-research-notes.md §4,
EvoHarness-RL): harness state is **Belief** (true in the workspace right now),
**Progress** (a subgoal: open, blocked, done) and **Experience** — and
Experience is already durable memory, so it is not duplicated. Beliefs and
progress become **records held outside the transcript** (`internal/session/state.go`),
written by three tools beside the existing two: `track(text, kind, evidence)`,
`commit(id)` (progress → done, belief → stale), `recall()`.

Two laws make it worth its cost. **Grounded in execution, not narration**
(PMCoder, §4): `evidence` is required at every entrance and names what RAN —
`bash: go test ./internal/session`, `read: go.mod` — so a record can be
re-verified and a summarizer can never launder narration into fact through this
store; a `track` with no evidence is a tool error, not a recorded guess.
**Bookkeeping shares the turn's budget** (the BPE paper's own finding): each
tool description says so in one line, because bookkeeping that is free is
bookkeeping that is done compulsively.

State is per-conversation and durable: `<journal>.state.json` beside the JSONL —
per journal, not one file per session directory, because that directory holds
every session this workspace ever had. A file that does not parse, carries
another version, or holds a record without evidence is **dropped whole with one
log line** — never fatal: a corrupt bookkeeping file must not cost anyone their
conversation, and a half-loaded state is a state nobody wrote.

The seam is the state card: the records as one bracketed block —
`[state] …` then `beliefs:`, `open:`, `done:`, newest first, capped at 40 lines
with finished work squeezed first — which the compaction pass injects into the
**rebuilt** transcript right after the summary note. The pass then hands the
model two different things about the same conversation: a lossy narration of
what happened, and a verbatim record of what is true and what is open, the
second never having passed through the summarizer. **Trajectory compresses;
state does not.** (The store, the tools and the block land here; the injection
line in `compact()` is the wiring wave.)

This is not the todo list Decision 11 refused, and the distinction stands: a
todo list is a plan the person reads, and work big enough to decompose belongs
to the workforce (Decision 19). These records are the model's own working state,
sized for surviving a compaction, and no surface draws them.

## Decision 23 — Routed memory: reflex extraction, provenance and direct control

Memory is an event-sourced block in the store, not an ever-growing `memory.md`
prompt. Before a turn, the reflex model routes a title-only index and injects
only relevant full rows. After a turn, reflex extraction settles a durable
candidate against nearby memories as add, update, supersede or skip. The
`/memory` panel exposes the result directly: search, scope, full text, use count,
provenance, edit, forget, and one-deep undo.

The state card separately preserves working beliefs and progress. Compaction is
a zero-LLM transcript rearrangement: it retains the state card and bounded
conversation material without asking a summarizer to invent a new account of
either.

## Decision 25 — The session chases speed and checks that it got it

**A model id is an address, not a machine.** One id is fanned over many
endpoints that answer at very different speeds for the same price, so a session
that only names its model is describing a decision it did not make. v3 both asks
for the fastest endpoint and measures what it actually got.

**The ask is one object on every request.** `provider: {sort, allow_fallbacks:
true, require_parameters: true}` — the router picks the currently-fastest
endpoint, fallbacks keep every preference advisory (a slow answer beats no
answer), and `require_parameters` stops a request carrying the reasoning knob
from landing on an endpoint that would silently drop it. The `routing` settings
row is the only dial: `latency` (default) · `price` · `off`, and `off` is total
— no preference object, and no measurement either, because a session that asked
for no routing asked for no ledger.

**The check is a ledger, and it names no vendor.** Every completion is timed —
TTFT from the send to the first token (reasoning counts; it is the endpoint
writing), the rate over the generation window alone — and the response says who
served it. Per model, per served endpoint, in memory, no call, no spend:

| what happened | what the next request carries |
|---|---|
| TTFT > 2s, or a sustained rate < 30 tok/s over ≥ 32 output tokens | a strike |
| 2 strikes | the endpoint goes last in `provider.order`, behind every healthy one |
| 3 strikes | `provider.ignore` for 5 minutes |
| the cooldown expires | it comes back **on probation** — demoted and one strike short, so a still-slow endpoint is dropped by its very next answer |
| a fast answer | one strike back; the ledger is a measurement, not a ratchet |

Two rules keep it honest: an answer whose server did not identify itself is
measured but earns no strike (a strike is a claim about an endpoint), and
`order` is omitted entirely when no healthy lane is left — `order` names what to
try *first*, so a list of nothing but demoted endpoints would pin the worst one
known to the front.

**And the HUD says who answered.** The model segment carries the served endpoint
and its rate when they are not already the model's own name —
`deepseek-v4-flash · via quicksilver · 92 tok/s` — and goes quiet when there is
no fact to state: nothing measured, nobody named, or a sighting old enough that
its rate describes a conversation that has since gone to sleep.

## Decision 26 — A session is a folder; the person's repo is borrowed, never littered

**The unit of storage is the session folder.** Everything one conversation
puts on disk lives in one directory:

```
~/.aforge/v3/projects/<encoded-workspace>/<session-id>/
    transcript.jsonl      the journal (flock lives here)
    state.json            BPE working state (Decision 22)
    tasks.json            live graph checkpoint (Decision 19)
    meta.json             identity: title, the REAL workspace path, owned?,
                          created, last-active, model
    tasks/                node journals and their audits
    logs/                 job logs, stubs, frames — droppings
    trees/                git worktrees, one per running node
    work/                 the workspace itself — OWNED sessions only
```

Deleting a session is `rm -rf` of one folder (after the worktree law below).
Exporting one is zipping one folder. Inspecting one needs no filename
arithmetic. The sidecar-derivation scheme (`<stem>.tasks.json` beside a flat
transcript) dies with the flat layout, and with it the parallel orphan tree
`~/.aforge/v3/tasks/<session>/` — a node's journal now lives beside the
conversation that commissioned it. The old objection to per-directory state
files ("that directory holds every session this workspace ever had") dissolves:
the directory now holds exactly one.

**Rejected: going back to the store for conversations.** v1/v2 keep rooms as
rows in `graph.db`, and the machinery is good — FTS, reuse, reaping. But a
transcript a person can `cat`, `grep`, and `rsync` is worth more than a JOIN,
and Decision 0 already chose the journal-as-file school. If listing hundreds of
sessions ever needs ranked search, the answer is a DERIVED index rebuilt from
the folders — an index, never the store of record.

**The workspace is the git root, and the encoded dirname is a bucket, not an
identity.** `aforge` launched from `repo/cmd/` and from `repo/` is the same
project; the workspace resolves to the repository root (a folder outside any
repo resolves to itself), the launch subdirectory is recorded in `meta.json`,
and the true path lives there too — so a moved repository is re-linkable and
the encoding can stay dumb.

**Borrowed or owned — every session has exactly one workspace.** A session
opened inside a project BORROWS it: tools root at the repo, exactly today's
behavior. A session opened nowhere — `$HOME`, a temp dir, a launcher — OWNS
its workspace: tools root at `work/`, inside the session folder. Research
notes, bash output, scraped data all land in one place, reaped with the
session. There is no third mode; "ephemeral" is not a mode a person must pick
correctly, it is what an owned session already is (cheap to delete), plus one
sweep rule: a session whose recorded workspace was under a temp directory is
litter, and the idle sweep reaps it.

**Every owned workspace is silently `git init`-ed.** The person never has to
know. What it buys, from machinery that already exists: task nodes get
worktrees, isolation, the auditor and the merge (Decision 19) for research
sessions that today run "in place" with none of that — and every document the
agent touches gets undo history. The in-place fallback survives only for its
one honest case: a borrowed folder that is not a repository.

**Nothing of ours lives in the person's folder.** `<repo>/.aforge-v3/` dies
entirely. Worktrees move to `trees/<node-id>/` in the session folder — git
registers every worktree in `.git/worktrees/` whatever its path, so repo-local
placement was never a constraint — and the git-surgery lock moves to
`~/.aforge/v3/locks/<repo-hash>.lock`. Two laws pay for the move: session
deletion and the sweep run `git worktree remove`/`prune` against the recorded
repo path BEFORE removing the folder, and the task proposal card names the
branch point ("from HEAD — unsaved edits not included"), spending the
two-trees surprise before the work runs instead of after the merge.

**Deliverables are indexed; droppings expire.** A deliverable — a generated
image, an export, a finished document — is recorded as one row in a global
append-only `~/.aforge/v3/artifacts.jsonl` (`path, session, title, kind,
created`), the same citation-not-archive pattern as `tasks.jsonl`. A `/files`
picker reads it newest-first with three verbs: open, reveal, copy to. "The
report from Tuesday" is found by title, from any directory, and getting a
keeper OUT of an owned session is a deliberate promotion ("copy to
~/Documents"), never a surprising write the harness made on its own. Rejected:
a visible `~/aforge/<title>/` folder per session (litter for every throwaway,
and reaping becomes a user-facing event) and an `aforge://` URI scheme (plain
paths plus an index do everything a resolver would, without the resolver).
Droppings — `logs/` — carry a 7-day TTL swept in the dreaming slot (Decision
23's idle window). Transcripts are forever, and `work/` is a person's content:
never TTL-ed silently, gone only when its session is deliberately gone.

**Resume follows v2's law, not mtime.** The default resume is the session with
the newest USER message (`session_rooms.go:38-50` holds the rationale: a
background write touching a file is not a person returning to a conversation),
read from `meta.json` last-active stamped on user turns. Launch-time grooming
is imported with it: an empty untitled session is reused rather than
duplicated, and empties are reaped — the flat layout left 19 dead `/tmp`
workspace dirs on the author's own machine, which is this law's whole case.

**One home, one seam, one late rename.** Every v3 path goes through
`internal/home` — the three direct `os.UserHomeDir()` calls (`chatv3.go`,
`task_run.go`) were the reason `AFORGE_HOME` half-worked. The product's final
name is openaf, and the rename happens ONCE, at the end, as its own refactor:
until then every name stays on the `aforge` scheme, which is why the
project-config layer reads `<workspace>/.aforge-v3/config.json` and the
`.openaf` path in Decision 6 is deferred to that rename. A test greps for
hardcoded `.aforge` literals outside the seam so the rename stays a
constants-change plus a boot migration, not an excavation.

**Migration is one boot pass, one-way, never fatal.** A flat-layout transcript
found under `v3/sessions/<ws>/` is folded into a session folder named by its
header id; its sidecars and its `v3/tasks/<session>/` journals move with it. A
file that will not parse stays where it is with one log line — a corrupt old
session must not cost anyone their new one. The legacy
`~/.aforge/v3/memory.md` is imported once into the routed store and renamed out
of the way; later inspection and control is through `/memory`.

## Milestones

| milestone | lands | acceptance |
|---|---|---|
| **V3-0** skeleton | chatv3 gate; `internal/session` agent (bare-loop machinery, omp-adapted prompt, working tools + todo); minimal tui3 (status line, conversation, streaming, input) | DONE (lite, tasker-free): talk, read/edit/run in the workspace, Esc interrupt, resume, steering, compaction |
| **V3-1** the workforce on screen | session anchor splice; workforce tools → gate → RequestCommand; DAG rail scoped to session; cards + dock; node rooms + steer; `@tag`; control palettes (pause/resume/cancel/redirect/restart/model); question UX | every Decision-5 row drivable by keyboard and mouse |
| **V3-2** omp comfort | settings panel; model picker per role; project-local config; session switcher/welcome; compaction polish | settings/model flows match omp muscle memory; crash mid-session loses nothing journaled |
| **V3-3** cutover | delete `internal/tui`, `internal/tui2`, `internal/head`, the chatv2 gate; `aforge chat` = v3 | the repo has exactly one chat; tasker packages diff-free |

## What this is not

- **Not a tasker change.** Store, admission, compiler, reconciler, runner,
  executors: untouched. The one edited v1-era line is the head's room-routing
  skip clause, chat-side by definition.
- **Not a second brain.** The session agent is a surface-flavored loop over
  the same journal; `aforge do`, `plan`, `run` observe the same graph.
- **Not a cleanup of v1/v2.** They ship until V3-3 proves parity.
