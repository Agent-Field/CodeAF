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
| Skills & rules, memory (retain/recall) | the notebook (`note`/`forget`) and `recall` over the store — aforge's existing equivalents |
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
(#9DC3E6) for headings and the user's glyph, dim #6B7280 for meta, diff and
match accents in nord pastels (+#A3BE8C, −#BF616A), failure soft orange-red
(#D08770), and the code fence theme a pastel chroma style (catppuccin-mocha
where available, else the tokens ramp with pastel overrides). One rule
above every choice: if a colour could be described as "bright", it is wrong.

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
