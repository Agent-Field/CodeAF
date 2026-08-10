# Chat rebuild — diagnosis and redesign notes

Date: 2026-08-10. Source: three deep read-throughs (thread/store architecture, TUI chat
surface, head authority) at HEAD `d9fc533`. This file is the working notes for the
threads/chat teardown: what exists, why it is weird, and the shape of the replacement.
The headless task path (`aforge do`) is healthy and stays; the chat surface and the
head's split brain are what get rebuilt.
Revised 2026-08-10 after adversarial review (see Part 9).

---

## Part 1 — What actually exists (short map)

- **No `Thread` type exists.** A "thread" is an emergent view over the `messages` +
  `commands` SQLite projections keyed by a `session_id` string. Journal-is-truth
  (`internal/store/thread.go:14-18`); projections rebuild from event replay.
- **Session identity is a global singleton**: `resolveChatSession`
  (`cmd/aforge/chat.go:2849`) resumes whatever `LastSeen()` names, across all surfaces.
  Headless runs never write seen edges, so the watermark goes stale; deliverables get
  re-homed into whatever window is attached (`deliverySessionID`,
  `resident/resident.go:1956-1967`).
- **The head is a router with an orchestrator bolted underneath.** `Head.answer`
  (`internal/head/head.go:429-549`) runs a 10-recognizer deterministic cue ladder
  (questions → services → charters → surgery → standing → redirect → correction →
  control belt) before the LLM router ever sees the message. The tool belt
  (`internal/head/toolbelt.go`) has 15 tools including real mutation authority
  (`control` = cancel/pause/resume/restart/reprioritize; `steer`; `revise`;
  `expedite`) — but it only fires when a lexical cue trips (`controlLoopApplies`,
  `control.go:281`).
- **One funnel for graph mutation already exists and is good**: everything converges
  on `store.RequestCommand` → `validateNodeCommand` → `Reconciler.applyCommand`. TUI
  keybindings and belt tools journal the identical typed command. Keep this.
- **Chat vs headless converge below the head** (`brainOptions`,
  `cmd/aforge/chat.go:100-152`): same reconciler, runner, compile→plan→splice, consent
  desk. `aforge do` writes the identical splice command the head would. The seam law
  (docs/ARCHITECTURE.md Decision 7: conversation is a lens) is architecturally true
  today. Keep this too.
- **No event bus** — append-only events table + 400ms/2s polling on a watermark,
  except provider token deltas which push over an in-process Go channel. Five separate
  progress channels: thread messages, replaceable progress rows, trace files (outside
  the journal), node-anchored mailboxes, and the in-process stream.

## Part 2 — Why it is weird (the indictment)

### The head's split brain
1. **Two prompts, two boards, two thread renderers, three verb vocabularies.** Router
   prompt (`head.go:78-136`) vs belt prompt (`control.go:22-77`) restate overlapping
   law; `renderGraph` (`head.go:1508`) vs `boardRows` (`toolbelt.go:1390`); command
   verbs exist as store constants, router JSON kinds, and belt tool names.
2. **The cue ladder is load-bearing and unauditable.** Ten sequential recognizers,
   each with a private keyword vocabulary; ordering documented only in comments;
   adding one means auditing ten interactions. A message that needs orchestration but
   trips no cue never reaches the tools.
3. **The head cannot spawn work as a tool call.** Spawning is only the terminal
   decision of a router turn (`issue.go:24`, `fanout.go:38`); the belt is explicitly
   forbidden from queuing (`control.go:56`). The head cannot read the board, decide,
   spawn, and report in one turn.
4. **The head cannot cancel its own turn programmatically.** `Head.Interrupt`
   (`head.go:370`) is an in-process handle bound to a TUI keypress. Not a command
   kind, not journalled, not reachable from another surface or from the head itself.
5. **The head cannot answer worker questions** — no tool, and `OpenQuestions` is a TUI
   backend capability never in the head's prompt.
6. **No feedback loop on async commands** — `control`/`revise` return "queueing";
   outcomes arrive later as reconciler receipts. The head cannot await or re-plan on
   a result inside a turn.
7. **The system prompt contradicts the machinery**: "It never plans or executes work"
   (`head.go:1-3`, `:91`) sits above a belt that cancels, steers, and revises plans.

### The thread model
8. **"One mouth" is a convention, not a mechanism** — 37 `store.PostMessage` call
   sites across 19 non-test files (head, resident, runner, cmd, TUI); the
   hand-maintained `headSpeaksFor` audit list (`resident.go:1020-1043`) is the
   compensating hack.
9. **One nullable column, four semantics.** `messages.node_id` decides: chat vs
   steering mailbox (`coalesce.go:106`), person vs ambient prompt window
   (`head.go:1023`), stream vs card mutation (`stream.go:261`).
10. **Three overlapping conversational stores** — `messages`, `agent_questions`,
    `messages_fts`; a question exists in five places (question row, message row, JSON
    blob in the body, `options` column, FTS copy).
11. **Structured data smuggled through prose.** Typed columns exist and are bypassed;
    the TUI recovers question components with a hand-rolled brace/quote scanner over
    the body (`cards.go:738-767`) plus a legacy numbered-list fallback.
    `Message.Options` is effectively write-only.
12. **Head reads all sessions on one cursor** (`Messages("", cursor, 200)`,
    `head.go:294`) while the lens reads one session — cross-session fold-stops and
    re-reads exist purely to defend against that query shape.

### The TUI
13. **330-field god struct** (`tui/model.go:300-781`), one flat namespace for every
    page, ~30 hand-maintained hit-test rectangles, 6 render caches. `newSession()`
    hand-resets 25 fields.
14. **Render functions mutate the model** (click-target row maps written during
    `View()`), forcing a fixed invalidate/track call order and a side channel for
    filesystem questions discovered mid-render.
15. **Four caching layers** for the transcript (block cache, card blocks, brief
    blocks, whole-thread splice) with `reflect.DeepEqual` validity checks and
    heuristic eviction.
16. **Two streaming lanes plus a queue, with a documented stall state** — belt
    answers never stream (land whole, get retyped by the simulator), router answers
    stream. Same product, two visibly different reply behaviors.
17. **28 capability interfaces + 44 runtime type assertions** to discover one
    concrete `chatCommander` struct.
18. **Cancel affordances are scattered and asymmetric**: chat transcript/dock cards
    have no cancel; `/cancel <id>` requires knowing an id; task page has `c`/`r`
    keybindings; head-turn interrupt is esc-only and in-process. Todos/plan progress
    have no chat surface at all.

### The wiring
19. **`cmd/aforge/chat.go` is a 6063-line god file owning engine logic** — delivery
    judging, gap extension, plan revision, consent desk, provider pool, the whole
    `chatCommander` — none reachable from `internal/`.

## Part 3 — Laws that survive the rebuild

- **Journal-is-truth**; projections rebuildable. One SQLite journal, one command
  funnel (`RequestCommand` → reconciler). No second engine.
- **Decision 7 (lens law)**: chat and headless differ only in where the task comes
  from. A feature that behaves differently in one surface is a bug in the seam.
- **One mouth** (JOURNEY.md) — but promote it from convention to mechanism.
- **THREAD-UX presentation law**: stream = conversation, card = work; interruption
  budget = question / delivery / failure.
- **Emergent-capability principle**: the head gets general tools, not hard-coded
  playbooks.
- Standing/charter probation law (STANDING.md Decision 5) untouched.

## Part 4 — Redesign: chat as the main orchestrator

### 4.1 One agentic head loop (kill the split brain)
Replace the cue ladder + router + belt trio with **one tool-loop agent**: system
prompt says "you are the orchestrator; you converse, and you act through tools."
Every capability becomes a tool in one belt:

- `spawn` (splice/reflex work orders — currently router-terminal only)
- `control` (cancel/pause/resume/restart/reprioritize — exists)
- `steer`, `revise`, `expedite` (exist)
- `plan` read (exists) — plus a bounded `await`/re-read so a turn can observe the
  receipt of a command it just issued
- `answer_question` (new: settle a worker's open question, gated same as today's
  human path where consent-class)
- `interrupt_self` / turn cancel as a journaled command kind (new — so esc, another
  surface, or a headless caller can stop the head's own turn)
- board/result/history/search/spending/standing/note/manual/read/competence/plan
  (exist)

Deterministic recognizers don't disappear — they become *cheap pre-answers* that the
loop may consult, not pre-empts that bypass it. One prompt, one thread renderer, one
board renderer. Reversibility/consent gates and confirm thresholds stay exactly where
they are (shared funnel), so authority expands without weakening the gates.

### 4.2 Threads become first-class
- A real `sessions` table (id, title, created, last-active, surface) replacing the
  `LastSeen()` singleton; explicit thread list + switcher in the UI.
- Per-session head cursor (not one global cursor over all sessions).
- Structured message parts instead of prose smuggling: a message carries typed
  blocks (text, question ref, card ref, progress ref) — the body scanner dies.
- Steering mail moves out of the chat table semantics: same journal, distinct kind,
  so `node_id` stops carrying four meanings.
- One mouth as mechanism: a single `thread.Post` door in one package; the store
  refuses `messages` writes from anywhere else (or at minimum a lint/test gate).
- **What "one mouth" means** (clarified after review — it never meant one writer):
  one **door** — a single `thread.Post` API, store-enforced — and one **answering
  voice** per room, that room's orchestrator. Multiple attributed speakers
  (reconciler receipts, worker questions, user-via-main dispatches) flow through
  the same door with explicit attribution. The law is about the funnel and the
  voice, not about a single author.

### 4.3 The chat surface (traditional harness shape)
Inspiration: Claude Code's TUI grammar — transcript + bottom composer + status line,
with aforge's right rail kept as the live-work surface.

- **Transcript**: user turns, streamed head replies (single streaming lane; every
  reply streams, including tool-loop turns), and *inline collapsed tool-call rows*
  (`▸ control: cancelled wisp-nav2`, `▸ spawn: 3 work orders`) — the head's actions
  visible in-thread, expandable, exactly like a coding-agent harness renders tool
  use. Settled deliverable cards stay inline at birth position.
- **Right rail (persistent, not toggle-hidden)**: live task cards with per-card
  affordances — cancel, steer, open — plus plan/todo progress (the plan-sight data
  the head already gets: steps, waits-on edges, elapsed). This gives the "todos"
  surface that today doesn't exist anywhere in the TUI.
- **Composer**: keep draft ring, attachments, esc-interrupt; add a persistent status
  line (model, spend vs rail, N running / M waiting) replacing the legacy activity
  bar + shimmer + dock fallback stack.
- **Esc ladder shrinks**: with one place grammar (thread + rail + task page +
  overlays) most of the 20-rung ladder disappears.
- **Interaction parity**: anything the head can do with a tool, the user can do with
  a click/key on the same object (cancel on a rail card journals the same command).

### 4.4 UI implementation reset
The current `internal/tui` chat page is not salvageable incrementally (god struct,
render-mutates-model, four caches, splice hack). Rebuild as per-page sub-models with
a message-part renderer keyed to the structured thread model; keep the reimplemented
viewport (our `charmbubbles/viewport` is a ~100-line reimplementation, not a vendored
fork; `textinput` is a genuine fork), the poll watermark, and the anchor-preserving
scroll idea (the one genuinely good invention in the current renderer — it survives
the 8.1.1 resolution, because we stay in alt screen).

### 4.5 Plumbing
- Keep polling (single process, SQLite, works) but collapse the five progress
  channels: progress and narration become structured message parts; trace tail stays
  the task-page-only exception.
- `chat.go` god file dissolves: commander → `internal/command` (or into head),
  plan-revision/judging → `internal/plan` or resident, consent desk → its own
  package, provider pool → `internal/provider`. The TUI's 28 interfaces collapse to
  ~3 (Backend, Commander, Streams).

### 4.6 Siting and phasing the orchestrators

Part 5 describes rooms as if every task already had its own live orchestrator agent.
It doesn't, and v1 does not build one. Where the orchestrator *lives* is the
structural decision; this subsection settles it and phases the work.

**v1 — exactly ONE head.** A task room is a **view**, not a second agent:

- its transcript is the task's node-anchored message trail + reconciler receipts +
  the orchestrator status line;
- its composer posts steer/redirect through the **existing journal verbs**;
- those messages are answered by the one head, which already has plan-sight.

This delivers the rooms product (the journeys in 5.3/5.4/5.5 of this doc) with **zero new
agents**. Everything in Part 5 about anatomy, scope, receipts, and predictability
holds; only the "who answers" is collapsed to one voice.

**Why v1 does not attempt live per-task agents**: the head is *window-sited* today.
It is built in `cmd/aforge/chat.go`, has a single turn slot, a single-session fold
cache, and a global cursor. Multiplying that object per task would multiply the
window brain, not the resident.

**v2 — orchestrators become RESIDENT-SIDE loops keyed by task session**, never
window-side. That siting is what makes headless `aforge do` tasks get orchestrators
for free, and it is what preserves the lens law: a headless-commissioned task's room
is identical to a chat-commissioned one, because neither depends on a window being
attached.

**Prerequisites for v2** (each detailed in Part 9): per-session cursors,
`Command.Issuer` with a subtree-authority check, per-task budget ceilings, and
resident-side scheduling of orchestrator turns.

## Part 5 — Product design: rooms, orchestrators, and the chat-of-chats

(Added 2026-08-10 after the philosophy discussion. This section settles the product
questions: where orchestrator chats exist, how they relate, and the user journeys.)

### 5.1 The governing principle: a chat exists where direction can change

The question "why not a chat for every subtask?" has a principled answer, not a
taste answer. A conversation is warranted only for a unit of work that:

1. **owns a plan that can be revised** (there is something to redirect),
2. **lives long enough to need redirection** (minutes-to-days, not one tool run),
3. **answers for an intent the user expressed** (there is someone to report to).

Leaves execute; they don't negotiate. You don't chat with your hand. So:
**chat follows plan ownership, not node existence.** Every node that owns a plan
subtree gets the orchestrator primitive; nodes that are pure execution get a trace
page and a steer line, nothing more.

In today's aforge this means: top-level jobs (splice roots) get orchestrators.
The architecture keeps the primitive *recursive in principle* — if a subtask ever
grows its own plan (recursive splice, or the user promotes it: "make this its own
task"), it earns a thread by the same rule, and the emergent-capability doctrine is
satisfied because nothing hard-codes "two tiers." The product simply doesn't
instantiate chats where there is no plan to steer.

Corollary for the rail: hierarchy is communicated by **depth of room, not badge
taxonomy**. Top-level cards are doors to rooms; subtask rows live inside the room
of their owner.

### 5.2 Topology: chats never call chats — they share a journal

The critical decision. Cross-chat interaction is NOT LLM-to-LLM conversation.
All heads (main + per-task) read and write one journal; commands are direct;
speech is addressed and absorbed at natural turn boundaries.

(Siting and phasing: **see 4.6**. In v1 there is one head and the task rooms are
views over this same journal topology; the diagram below is the v2 end state, and
nothing in it changes when the second orchestrator arrives — only who answers.)

```mermaid
flowchart TB
    U((user))
    subgraph rooms [conversational surfaces]
        MH["main head\n(home thread)"]
        T1["task orchestrator A\n(task thread A)"]
        T2["task orchestrator B\n(task thread B)"]
    end
    J[("journal\nmessages · commands · events")]
    R["reconciler\n(one funnel)"]
    subgraph dagA [task A subtree]
        W1[worker] & W2[worker]
    end
    subgraph dagB [task B subtree]
        W3[worker]
    end

    U -->|speaks in any room| MH & T1 & T2
    MH -->|journaled commands + addressed messages| J
    T1 -->|commands scoped to A| J
    T2 -->|commands scoped to B| J
    J --> R --> dagA & dagB
    W1 & W2 & W3 -->|progress, results, questions| J
    J -->|status projection up| MH
    J -->|task-thread rows| T1 & T2
```

Rules of the topology:

- **Authority is direct and concurrent.** Main head and task head both issue the
  same typed journaled commands; the task head's authority is bounded to its
  subtree and budget; main head's is global. Conflicts are resolved by the
  journal's serial order in the one funnel — no negotiation protocol needed.
- **Direction flows down as commands carrying the user's verbatim words**, which
  also land as a message *in the task's thread* (attributed "user, via main").
  The task orchestrator absorbs it on its next turn — a turn it would take anyway.
  No relay conversation, no double reasoning. This is today's redirect/steer
  machinery, promoted from hidden mailbox to visible thread row.
- **Awareness flows up as structured projection, never transcripts.** Main head
  sees board/plan-sight (status, steps, elapsed, cost) plus each task
  orchestrator's self-maintained one-line status. It does not read task
  transcripts. Summaries up, direction down, both as durable rows.
- **True cross-chat conversation is rare and explicit**: an `ask_task` tool for
  when the answer requires task-local depth. The product prefers moving the user
  to the right room over relaying words between heads.

### 5.3 Journey: changing direction from the main chat

```mermaid
sequenceDiagram
    actor U as user (home)
    participant M as main head
    participant J as journal
    participant T as task orchestrator (wisp)
    participant R as reconciler

    U->>M: "have wisp skip the H2 work, ship without it"
    M->>J: CommandRedirect(task=wisp, words verbatim)
    M->>J: message → wisp thread ("user, via main: skip H2…")
    M-->>U: "told wisp — it will rework the remainder"
    Note over M,U: main's turn ends here, ~one cheap turn
    T->>J: (next turn) reads redirect row in own thread
    T->>J: revises plan, journals the change
    R->>J: applies, posts receipt in wisp thread
    T->>J: status line updated ("dropping H2, 3 steps remain")
    J-->>M: projection reflects new plan
    Note over U: home rail card shows the plan shrink;<br/>no interruption in home thread
```

Identical outcome if the user steps into the wisp room and says it there — same
command row, no main-head involvement at all. Lens law holds: **same action from
any room writes the same journal row.**

Token economics: the relay model (main head *converses* with task head) is
rejected. Cost of a direction change = one main-head turn + the task-head turn
that was going to happen anyway. Asking "how's wisp doing?" in home costs only a
main-head turn reading projection — the task head is not woken.

### 5.4 Journey: a task needs the user (escalation upward)

```mermaid
sequenceDiagram
    actor U as user (home)
    participant T as task orchestrator (wisp)
    participant J as journal
    participant M as main head

    T->>J: blocking question (consent-class), addressed to owner thread
    J-->>U: home rail card gains ? badge; interruption budget admits it
    alt user answers from home
        U->>M: "yes, let it buy the API key"
        M->>J: resolves question (journaled answer)
    else user steps in
        U->>T: opens wisp room, answers in place
        T->>J: same journaled answer row
    end
    J-->>T: answer lands in wisp thread; work resumes
```

Only three things cross rooms upward — question, delivery, failure (the existing
THREAD-UX interruption budget). Routine progress never leaves its room.

### 5.5 Task lifecycle with an orchestrator

```mermaid
stateDiagram-v2
    [*] --> Commissioned: main head spawn tool\n(mints session + orchestrator)
    Commissioned --> Planning: orchestrator compiles plan
    Planning --> Working: subtree spliced
    Working --> Waiting: blocking question
    Waiting --> Working: answer lands
    Working --> Revising: redirect / overrun / cancel-rethink
    Revising --> Working: remainder re-spliced
    Working --> Settling: leaves done, gate runs
    Settling --> Delivered: deliverable posted to owner thread
    Working --> Failed: terminal failure escalates
    Failed --> Revising: user or main head restarts
    Delivered --> [*]: thread archived, searchable
```

The narrator, steering mailbox, and cancel-rethink sentinel all dissolve into
this one agent: progress narration = orchestrator speaking in its own thread;
steering = messages addressed to it; post-cancel rethink = its own revision turn.

(Phasing: **see 4.6**. In v1 the lifecycle above is driven by the single head plus
the reconciler, and the room renders it; "the orchestrator" in this diagram is a
role, and only in v2 is it a resident-side loop of its own.)

### 5.6 Navigation: three depths, one gesture

```mermaid
flowchart LR
    H["HOME\nmain thread + rail of task cards"]
    TR["TASK ROOM\ntask thread + sub-rail:\nplan steps, workers, cost"]
    W["WORKER PAGE\ntrace + steer line\n(no chat — it's a hand)"]
    H -->|enter task card| TR -->|enter worker row| W
    W -->|esc| TR -->|esc| H
```

- Breadcrumb is the spatial truth: `aforge › wisp-parity › NavCtx2`. Esc always
  zooms out one level. No mode flags, no toggle soup — the 20-rung esc ladder
  collapses into "zoom out."
- Every room has the same anatomy: transcript, composer, rail. The composer
  always talks to the room's orchestrator. Muscle memory transfers completely —
  learning one room is learning all of them.
- The worker page deliberately has no chat: its composer is the steer line
  (one-way mail), visually distinct, so the "you can converse here" affordance
  is never a lie.
- Apple-calm rules: home transcript is conversation only (work lives in rail and
  cards at birth/settle); attention states are the only color; a task card's
  status line is written by its orchestrator in first person, one line, plain
  words.

### 5.7 What each head knows (the knowledge contract)

| | main head | task orchestrator | worker |
|---|---|---|---|
| sees | board projection, all task status lines, home thread | own thread, own plan, own subtree, own budget | its contract + files |
| never sees | task transcripts, worker traces | other tasks, home thread | anything outside contract |
| speaks in | home thread | its task thread | trace + structured results |
| commands | global (spawn, control any task, ask_task) | own subtree only (revise, steer, control, answer own workers' informational questions) | none |
| escalates | to user | question/delivery/failure → owner thread | question → its orchestrator |

The row worth underlining: **a task orchestrator can answer its own workers'
informational questions** (it holds the task context — this is where the head's
current inability hurts most), while consent-class questions always escalate to
a human. That splits the `answer_question` autonomy decision (Part 6 open decision 1) cleanly:
autonomy is scoped by room and by question class.

### 5.8 Design risks noted

- **Two-room staleness**: the user redirects from home while stepping into the
  task room mid-revision — the task thread must render the pending redirect
  before the orchestrator's revision lands, or the room looks ignorant of an
  order it already received. (Render the addressed message immediately; mark the
  orchestrator's acknowledgment distinctly.)
- **Status-line drift**: the rolled-up one-liners are the main head's whole
  picture; a stale or vague line poisons home answers. Status refresh must be a
  duty of every orchestrator turn, not a courtesy.
- **Cost ceiling per task head**: each orchestrator is a standing model presence;
  its turn cadence must be event-driven (absorb → act → sleep), never polling
  chatter. Orchestrator turns bill to the task. (Correction after review: per-task
  budget rails do **not** exist today — the rail is daily+global. They must be
  built; see Part 9.)
- **Promotion boundary**: "give this subtask its own chat" must move the subtree
  AND its budget/ownership cleanly, or the knowledge contract table above leaks.

### 5.9 The rail card: information hierarchy

(Telemetry vocabulary inspired by oh-my-pi's status bar / task blocks; exact
formatting conventions to be confirmed against the oh-my-pi study.)

A card answers three questions in priority order: **does it need me? → what is it
doing? → what is it costing?** Anatomy of a home-rail task card:

```
◐ wisp-parity                                   ← line 1: attention glyph + name
  reworking NavCtx after the worker died         ← line 2: orchestrator status, first person
  K3 · $8.65 · 17% ctx · 41m · 4 workers         ← line 3: telemetry, dim, tabular-nums
```

- **Line 1** — the only saturated color on the card. Attention states: working ◐,
  waiting-on-user ?, failed ✕, settled ✓, queued ○. Question badge outranks all.
  The right edge of line 1 carries the dim composer-mode mark (`›` chat room,
  `↦` steer-only) — 5.11.
- **Line 2** — written by the task orchestrator, one line, plain words, refreshed
  every orchestrator turn (a duty, not a courtesy — see 5.8).
- **Line 3** — model word, cost, orchestrator context %, elapsed, worker count.
  Dimmest tier. Money is always visible (it's the one number the user never
  forgives us for hiding); the rest can truncate on narrow rails.

**Progressive disclosure**: collapsed = 3 lines. Focused/entered expands to plan
progress (`3/7 steps`) and per-worker rows (`NavCtx2  read → edit  ·  K3 · $0.37
· 4.2% ctx · 5m`). Full depth = enter the room. Never on any card: node IDs,
seq numbers, journal internals, raw JSON.

**Context % semantics**: context is per conversational surface — the card's ctx%
is the *orchestrator's* window; each worker row shows its own. It is a health
signal, not trivia: amber past the compaction threshold, and it is the honest
answer to "why did it get dumber." Data source: usage rows already journal
per-request tokens; executors must also journal window-size high-water marks
(new, small).

### 5.10 Mid-run model control

- **Default model** lives in settings (and shows as a chip in the home status
  line). Plan-model vs work-model split already exists
  (`AFORGE_PLAN_MODEL`); the settings surface exposes both as "planning" and
  "working" defaults, in model-words not provider IDs.
- **Per-task override, mid-run**: the model chip on a task card / room header is
  interactive → model palette → journals a new surgery verb `CommandSetModel`
  scoped to the subtree. Takes effect at the **next provider call** — the running
  turn is never killed by a model switch (calm, non-destructive; wanting it
  immediate = cancel turn + switch, two explicit acts). Receipt lands in the task
  thread ("switching remaining work to X"). Restart inheritance of pinned models
  already exists (`resident/surgery.go:193-203`) — same plumbing, new verb.
- **Boost lane** (exists) folds in as a temporary escalation state on the same
  chip rather than a separate concept: chip shows `K3 ⚡` while boosted.
  **Single home, stated once and repeated in 8.2.16**: role *bindings* (the 5.23
  roles table) are the **storage**; the **chip is the only surface**. Boost is a
  transient escalation of the **work-role binding**, rendered as `⚡` on the chip.
  There is no separate boost UI, no boost page, no second toggle.
- Reasoning effort rides the same chip (`K3 · high`), same palette, same
  journaled path.

### 5.11 Atomic tasks (SWE subharness and friends)

The 5.1 rule already decides this: **an atomic task owns no plan, so it gets no
orchestrator chat.** But a top-level atomic task (e.g. a single SWE-harness run
commissioned from home) is still a door in the rail — so its room exists, with
the same anatomy and one honest difference:

- transcript = the worker's activity feed (tool calls, progress, thoughts)
- composer = the **steer line**, visually distinct from a chat composer
  (different prompt glyph, placeholder "steer — one-way"), because steering mail
  is absorbed between turns, not conversed with.

Cards for atomic and planned tasks share the same anatomy (same three lines;
worker count simply reads `atomic` or is absent) but they are **not
indistinguishable** — the rail must tell you before you enter, or the affordance
lies in the one place the user can't yet see it. Each card carries a small
**composer-mode mark at the right edge of line 1**: `›` for rooms with a chat,
`↦` for steer-only (atomic) rooms. Same glyphs as the composer prompts (5.17), so
the mark *is* a preview of the composer you'll get. The *room* differs, and the
composer styling confirms in place what the mark promised. If the user
asks questions *about* an atomic task, home's main head answers from projection
— the harness itself is never impersonated. Promotion still applies: if an
atomic task fails repeatedly and the user says "figure it out," that mints a
plan around it — and with the plan comes a chat, by the standing rule.

### 5.12 Thinking display

- Effort is configuration → chip (`· high`). Thinking is activity → transcript.
- While a head reasons: the awaiting line gains "thinking" with elapsed
  (`aforge · thinking 12s`) — one quiet line, no raw stream in home, ever.
- Worker thinking renders only in the worker room feed as collapsed `▸ thought`
  rows (summaries when the provider gives them), expandable. Home and task
  threads never show worker CoT — that is trace-depth information.
- A turn that escalated effort mid-flight notes it in its receipt, not in a
  live banner.

### 5.13 Motion and typographic doctrine (TUI)

To be reconciled with the oh-my-pi motion catalog when the study lands; the
budget below is the Apple-calm position:

- **One live pulse per room.** A room has at most one ambient animation at a
  time: the awaiting/thinking line OR the stream caret OR a card shimmer —
  attention is a budget, and the current shimmer+dock+spinner stack overspends
  it.
- Streaming reveal is the hero animation and the only continuous one. Card
  expand/collapse animates height in 2–3 frames max (terminal reality);
  attention-state changes cross-fade color once, no blinking, no marquees.
- Progress is shown by numbers changing, not bars sweeping (`3/7 → 4/7` with a
  single-frame brighten on change).
- **Type hierarchy = weight + color + indent, three tiers**: primary (default
  weight, full contrast) for speech and titles; secondary (dim) for status
  lines and receipts; tertiary (dimmest, tabular-nums) for telemetry.
  ~~One accent color for attention, one for money — nothing else is colored.~~
  **Superseded by 5.16**: this line predates the five-hue vocabulary, which is the
  live color law. What survives from 5.13 is the **three-tier grey ramp** and the
  **accent = live** axis (8.1.6); the count of hues is 5.16's to set.
- Spacing rhythm: two-space indent per depth, one blank line between turns,
  cards separated by whitespace not boxes; hairline rules only at room
  boundaries (the current TUI's heavy box-drawing recedes).
- `prefers-reduced-motion` equivalent: a `calm` setting that freezes shimmer and
  typewriter, keeps only the caret.

### 5.14 Critical-vs-incidental information taxonomy

| tier | information | where |
|---|---|---|
| always visible | attention state, question badges, status line, cost | rail cards, status line |
| one focus away | model+effort, ctx %, elapsed, worker count, plan progress | focused card |
| one room away | per-worker telemetry, plan steps with waits-on, receipts | task room |
| trace depth | tool calls/results, thoughts, steering echo, raw output | worker page |
| never shown | node IDs, seqs, journal/provider internals, raw JSON | — |

Litmus: every piece of chrome must answer "what would the user *do* with this
right now?" — if the answer is nothing, it moves one tier down.

### 5.15 The rail is a map, not a menu — scoped master-detail

(Supersedes the "full-screen task room" reading of 5.6. The three depths remain,
but entering a task does NOT take over the screen — the rail persists and
re-scopes.)

**Rule: the rail always shows exactly one scope, and the main pane shows the
selected member of that scope.** Classic master-detail (Mail.app: list + reading
pane), applied recursively:

- **Home scope**: rail lists row 0 = `aforge` (the main head) + one row per
  top-level task. Selecting `aforge` shows the home thread; selecting a task
  card shows *a preview of that task* without changing scope.
- **Entering a task** (enter on its card): the rail **re-scopes to that task's
  DAG** — row 0 = the task orchestrator (its chat), then the plan steps and
  workers as an indented tree with waits-on structure visible. The row you came
  for is highlighted. The main pane shows the selected row's surface.
- **Moving between siblings never leaves the scope**: j/k (or click) moves the
  rail selection, and the main pane follows instantly (as a preview — see the
  select/open split below) — task chat → worker trace → sibling worker — while
  the rail stays put. The rail is the stable
  element; the main pane is the lens. This is the "stay inside the parent task
  while I'm focused on it" behavior.
- **Esc pops scope** (task scope → home scope), never just selection. Breadcrumb
  stays the spatial truth; the rail's scope header is the breadcrumb tail and is
  clickable to go up.
- **Selection = navigation.** There is no separate "highlighted vs open"
  state — one cursor, and what it rests on is what you see. Kills the current
  TUI's focus-zone carousel (tab through input/questions/chat/cards/graph/self/
  header) dead.
- **Selection previews; enter opens.** Refinement of the rule above, for cost, not
  for modes: j/k moves the selection and updates a **lightweight preview** of the
  selected row; **full main-pane commitment happens on enter**. One cursor still,
  one thing on screen still — but a keystroke never pays for a full-frame repaint
  of a whole transcript.
- **The scope model is width-independent.** Scope is not a rail feature; the rail
  is one *rendering* of it. Wide terminals render scope as the right rail; narrow
  terminals render **the same scope rows, with the same keys and the same
  selection semantics**, as a full-pane list. The 8.2.8 sticky HUD is a different
  object with a different job — it carries only the bounded live summary, never
  the scope map. (`railAtWidth` is 100 today, so at 80 columns the narrow path is
  the *primary* experience, not a fallback — Part 9.)

**You talk to what you're looking at.** The composer is bound to the selected
row's surface: orchestrator row → chat composer; worker row → steer line
(distinct glyph + placeholder, per 5.11); settled row → composer disabled with
"this work is settled — ask aforge about it." One rule, no exceptions, and the
affordance never lies.

Wireframes:

```
HOME                                          TASK SCOPE (entered wisp-parity)
┌────────────────────────┬───────────────┐    ┌────────────────────────┬───────────────┐
│ home thread            │ ● aforge      │    │ wisp-parity thread     │ ‹ wisp-parity │
│                        │ ─────────────  │    │ (orchestrator chat)    │ ● orchestrator│
│ you: ship the wisp…    │ ◐ wisp-parity │    │                        │ ─────────────  │
│ aforge: commissioned…  │   reworking…  │    │ you: skip the H2 part  │ ✓ XhrSyn      │
│                        │   K3 $8.65 ▄  │    │ wisp: dropping it —    │ ◐ H2      28m │
│                        │ ? data-clean  │    │   3 steps remain       │ ◐ T3Infra 21m │
│                        │   needs a key │    │                        │ ⚑ KeyCutter   │
│                        │ ✓ perf-audit  │    │                        │   waits on H2 │
│                        │               │    │                        │ ◐ NavCtx2  5m │
├────────────────────────┤               │    ├────────────────────────┤               │
│ › message aforge       │               │    │ › message wisp-parity  │               │
└────────────────────────┴───────────────┘    └────────────────────────┴───────────────┘
```

(Selecting `NavCtx2` in the right rail swaps the left pane to its trace feed and
the composer to a steer line — rail unchanged, scope unchanged.)

### 5.16 Color doctrine: pastel semantics on a dark ground

Color is a language with a five-word vocabulary; everything else is a grey ramp.
Pastels (desaturated, high-lightness) tuned for WCAG-AA-equivalent contrast on
the dark ground, and every hue owns exactly one meaning:

| hue | meaning | used for |
|---|---|---|
| soft amber | *needs a human* | question badges, waiting states, ctx meter near limit |
| soft cyan | *alive* | working glyphs, stream caret, thinking pulse |
| soft green | *money + success* | cost figures, settled ✓ |
| soft coral | *broken* | failures, cancels |
| per-task pastel accent | *identity* | task glyph + breadcrumb dot + rail highlight tint |

- **Identity hue**: each top-level task gets a stable pastel accent (hashed from
  task id, from a curated 8-hue pastel wheel — no two adjacent rail cards share
  one). Used only in the card glyph, the scope header, and the selection band
  tint inside that task's scope — so "which room am I in" is answered
  peripherally, never by a loud theme change.
- **Selection is a background band, not a foreground color** — a slightly
  raised background pill on the selected rail row (the Apple selection idiom).
  Text keeps its tier color.
- The grey ramp carries the three type tiers (5.13); accent hues never colorize
  running text — a whole colored sentence means something is wrong.
- Every pastel must pass contrast on BOTH the default dark ground and a dimmed/
  unfocused variant; the palette ships as named tokens with tested pairs, not
  ad-hoc ANSI numbers scattered through render code (today's state).

### 5.17 Glyph vocabulary — and why not emoji

**No emoji in chrome.** Three hard reasons: (1) emoji are double-width and
render inconsistently across terminals — the current codebase already fights
ambiguous-width ghosting (`clampNodeLines` sacrifices a column when it detects
one); (2) emoji carry their own loud colors that cannot be tinted, which breaks
the five-word color language above; (3) they read as notification confetti, not
instrument. Emoji remain welcome in *user content*. Chrome uses single-width,
tintable, metric-safe unicode:

| glyph | meaning |
|---|---|
| ○ ◐ ✓ ✕ | queued · working · settled · failed |
| ? | waiting on a human (always amber) |
| ⚑ | waiting on a sibling (waits-on edge) |
| ▸ ▾ | collapsed · expanded |
| › | composer prompt (chat) |
| ↦ | composer prompt (steer line) |
| ⚡ | boosted |
| ▁▂▄▆█ | one-cell context gauge (see below) |
| · | telemetry separator |
| ‹ | scope header / go up |

Banned for width-instability: ⏸ ⏵ ⏹, all emoji, most dingbats. Paused = `=`
glyph or dim `○`.

**The one-cell context gauge**: context % as a single eighth-block character
next to the model word — `K3 ▄ $8.65` reads as "half the window gone" at a
glance, turns amber past the compaction threshold, and costs one cell. The
precise `62%` appears at the focused-card tier (5.14); the gauge is the ambient
form. Same trick can serve plan progress on collapsed cards (`▆ 5/7`) — but
numbers stay the primary encoding (5.13: progress is numbers changing).

### 5.18 The `@` grammar: addressing without teleporting

Typing `@` in any composer opens an inline as-you-type filter (fzf-style, match
chars highlighted): **live** tasks first (attention-badged, identity-hue glyph) —
these are the direct-dispatch targets — then, under a **dim `history` group**,
recent settled tasks; fuzzy over task word + title. Enter/tab completes the
mention into the draft as a hue-tinted token. In a task room, `@` filters that
scope's workers instead (steer addressing).

**What sending does — the key decision**: `@` means *the user has already done
the routing*, so the message goes straight into the addressed task's thread as
user speech — **no main-head turn at all** (cheaper AND more predictable than
5.3's routed form, which remains for un-addressed prose like "have wisp skip
H2").

**Settled targets never receive direct injection.** Selecting a `history` row
routes the message to the **main head with that task as referenced context**
("about `perf-audit`: …") — because a dead thread has no one to absorb it, and a
composer that appears to speak into it is the affordance lying (5.15's disabled
settled composer says the same thing in the other direction).

**Every dispatch is journaled twice, and nothing is ever silent**: the message
lands in the task thread, AND a **durable dispatch echo row** lands in the
sending room, carrying the receipt vocabulary from 8.2.11 — `→ wisp-parity ·
woken` / `· queued` / `· settled`. The echo is a real row with an open
affordance, not a toast; the task's rail card pulses once. **If the task settles
before absorbing the message**, the echo row updates in place to
`→ wisp-parity · settled — returned to aforge` and the main head handles it from
there. A dispatch either reaches an orchestrator or comes home; it never
evaporates.

**You stay in home.** The user's instinct is right: a send must never teleport
the composer's context — spatial stability outranks convenience. The power
chord covers the other case: **enter = send and stay; ctrl+enter = send and
follow** into the task's room. Symmetric, learnable, optional.

### 5.19 The place line: where work lands on disk

The breadcrumb answers "where am I in aforge"; nothing answers "where is this
work happening on disk" — the always-missing pwd. Fix: a **place line** on the
composer's top edge, shell-prompt-familiar (the oh-my-zsh/p10k lineage:
informative, abbreviated, segment-shaped — but rendered in our dim tier with
`·` separators, no powerline triangles, which are font-fragile):

```
~/aforge-v2                              (home: resident root)
~/a/v2 · ⌂ /tmp/wisp-parity              (task room: task workspace)
~/a/v2 · ⌂ /tmp/wisp-parity · src/navigate.rs  (worker: its region)
```

- Fish-style path abbreviation (`~/a/v2`), middle-ellipsis for long tails,
  full path on focus; click/`y` copies.
- Room-scoped: it always describes the selected surface's material ground —
  which is also the root that tab path-completion (7.2) completes against, and
  the drop target for dragged files. One line, three duties.

### 5.20 Predictability doctrine: never a weird chat

The current head's deepest UX failure isn't rendering — it's that the user
can't predict what a message will *do* (reply? spawn a job? trip a recognizer?)
or what the system *can* do. Claude-Code-grade predictability reduces to six
rules:

1. **Visible dispatch.** When the head turns prose into work, the transcript
   shows the decision as a distinct commissioning row — task name, scale, and
   the moment it happened — never a silent side effect behind a chatty reply.
   The chat-or-work fork is THE ambiguity; it must always be inked.
2. **Preview before consequence.** Gated actions render an inline y/n naming
   the blast radius ("cancel 4 running workers, ~$2.10 in flight?"). No modals,
   no vague confirms.
3. **Capability honesty.** `?` in any room lists what *this* orchestrator can
   do, in words, from its actual tool belt — and disabled affordances say why
   ("settled — ask aforge"). The user never has to guess "can it do that?"
4. **No dead-air sends.** Every send gets an instant durable echo with a state
   the user can see move: queued → absorbed → acting. Type-ahead messages
   stack as visible queued chips under the composer until the orchestrator's
   next turn folds them in.
5. **Receipts for every mutation** — already law (journal receipts); the UI
   duty is rendering them in the room where the user is looking, compactly.
6. **Escape is always advertised.** While anything runs **in the room you are
   watching**, the awaiting line carries the interrupt hint (`esc to
   interrupt`) — interruptibility that exists but isn't visible doesn't exist.
   The hint appears only when esc would in fact interrupt; the reconciling rule
   (esc acts on what you are watching) is stated in 8.2.21.

### 5.21 Display-technique catalog (beyond single glyphs)

Techniques proven in the best TUIs (btop, lazygit, delta, fzf, p10k), adopted
into our language where they carry real information:

| technique | use here |
|---|---|
| **step dots** `●●●◐○⚑○` | plan progress on cards: one dot per step, filled=done, half=running, amber ⚑=waiting — discrete, honest, clickable (maps 1:1 to steps); beats a continuous bar |
| **rotating state glyph** ◐◓◑◒ | activity lives *inside* the state glyph — no separate spinner column ever |
| **braille sparkline** ⣀⣄⣤⣶ | cost/token burn trend on a focused card, 6–8 cells, one row (btop-style) |
| **left accent rail** ▎ | identity-hue tinted left edge groups a card's lines — structure without boxes (lazygit/delta idiom) |
| **diff micro-stat** `+124 −31` | worker settle rows, green/coral, tabular |
| **elapsed that ages** | `5m` dim → brightens when past estimate; unit granularity switches (5m → 1h02) at fixed width, no jitter |
| **middle-ellipsis paths** `src/…/navigate.rs` | never tail-truncate a path — the filename is the information |
| **count chips** `?2` | compact amber badge on cards with multiple open questions |
| **fzf-style inline filter** | `@` mention (5.18) and ctrl+k switcher: typed chars highlighted in matches |
| **OSC 8 hyperlinks** | real clickable paths/URLs where the terminal supports it; graceful plain text where not |
| **transient toast row** | one-line notices above the status line that decay after seconds (command receipts from other rooms) — replaces persistent banners |
| **width-stable everything** | every live cell (elapsed, cost, ctx gauge) renders at fixed width so nothing to the right of it ever dances |

Anti-catalog (seen elsewhere, refused): powerline triangle separators
(font-fragile), nested box-drawing frames, marquee/scrolling text, more than one
spinner per screen, percentage bars for discrete steps, emoji chrome (5.17).

### 5.22 Discoverability: no typed-only actions

The slash-command problem, named: a command language is a memory test, and every
action that exists *only* as typed text is invisible to anyone who hasn't read
the manual. The fix is structural, not cosmetic:

**Law: every action lives on a visible object; typing is an accelerator, never
the only door.**

Mechanically, one **command registry** is the single source of truth — each
entry: id, verb phrase, one-line description, scope predicate (where it
applies), key binding, slash alias, journal mapping. Six surfaces render FROM
the registry, so discoverability holds by construction and can never drift:

1. **Action strips on focused objects.** A focused rail card shows its verbs
   inline, dim and clickable: `enter open · c cancel · r restart · m model`.
   A focused message block shows `y copy · v expand`. The affordance appears at
   the point of attention — nothing to memorize, nothing hidden. (Confirm
   flows land inline in the same strip: `cancel 4 workers? y/n`.)
2. **One palette, ctrl+k.** Rooms, tasks, actions, and settings in a single
   fuzzy-searched catalog; every row = verb + description + its key/slash
   equivalent right-aligned. The palette *teaches* the accelerators through
   use (the macOS Help-menu-search idiom). Searching "cancel" finds the verb,
   the doc, and the cards it can apply to.
3. **Slash as filtered palette.** Typing `/` in the composer filters the same
   registry inline with descriptions — identical rows, same teaching format.
   A slash command is never a separate implementation; it's a text-shaped
   view of the registry.
4. **The contextual footer.** One dim line under the composer showing the 2–3
   most relevant verbs for the *current focus*, rotating occasionally to
   surface undiscovered ones. `?` expands to the full registry filtered to
   this scope (which is also the 5.20 capability-honesty surface — same data).
5. **Chips are buttons.** Model chip, ctx gauge, place line, breadcrumb
   segments, question badges, dispatch echoes — every informational chip is
   also the affordance for acting on what it shows (click model chip → model
   palette; click ctx gauge → context breakdown; click breadcrumb → go).
   Focus ring/underline marks interactivity; non-interactive text never gets
   one (the affordance never lies — 5.20).
6. **Empty states teach.** A new home (no tasks yet) shows three clickable
   example actions instead of a blank transcript; a new task room's first
   frame names what its orchestrator can do. First-run is onboarding; no
   tutorial mode.

Kill list this implies: bare action runes with no visible hint (today's `c`/
`r`/`v` on pages), the 17-command slash table as a parallel system, help as a
static wall. Everything routes through the registry or it doesn't ship.

**Registry-fix checklist** (the invisible affordances this doc itself still
proposed — each gets a visible object, the key stays as accelerator):

- [ ] **ctrl+k** — the breadcrumb root (`aforge ›`) doubles as click-to-search;
      the palette's door is a thing on screen, not a memorized chord.
- [ ] **@ dispatch keys** — once an `@` mention token exists in the draft, a
      **dispatch chip** appears showing `↵ stay · ⌃↵ follow`. The power chord
      (5.18) is only invisible before it is relevant.
- [ ] **alt+↑ dequeue** — queued type-ahead chips are **clickable to edit**;
      alt+↑ is the accelerator for the same act.
- [ ] **shift+j/k reprioritize** — the focused *pending* rail row shows a visible
      drag handle (`⋮`); shift+j/k is the accelerator.
- [ ] **Interactive chips brighten one tier on focus.** An interactive control
      may never live permanently in the dimmest tier — this amends 5.13's third
      type tier: telemetry that is also a button is dim at rest, secondary on
      focus, and carries the focus underline (5.22 rule 5).
- [ ] **Step dots are display, not targets** on narrow rails — too small to hit
      honestly. The **focused-card expansion** is the affordance (5.9); the dots
      only show. (Wide rails may hit-test them; nothing depends on it.)
- [ ] **Number-key precedence, stated once**: digits answer an open question in
      the current room when one is focused; otherwise they jump to rail rows in
      the current scope. The **contextual footer always shows which meaning is
      live** (`1–3 answer` vs `1–9 rooms`) — the ambiguity is resolved on screen,
      never in the user's head.
- [ ] **Footer/`?` rotation coverage** — `y` copy variants, ctrl+f search, `[` `]`
      resize, and esc-stash restore all appear in the contextual footer rotation
      and in the `?` overlay, registry-driven like everything else.
- [ ] **The `?` surface gets a permanent visible door**: a dim `?` segment at the
      right end of the status line. The capability-honesty surface (5.20 rule 3)
      cannot itself be a memory test.

### 5.23 Model economics: the role ladder and the cheap-model map

**Architectural fact first**: every turn — head, task orchestrator, worker — is
an independent provider call. Nothing about a running task binds it to a model;
aforge simply lacks the verb today (model is per-message for head turns and
inherited on restart, but a running subtree cannot be re-pointed). So mid-run
switching is not a feature to engineer; it is a command to add
(`CommandSetModel`, 5.10) and a chip to click. Design accordingly: model choice
is *always* revisable, everywhere, and takes effect at the next call.

**Five roles, no more.** Complexity cap: the whole economics surface is five
named role slots. Each is a binding from role → model+effort, inheritable by
scope (global → task → node; pinned overrides survive restarts, which the
surgery machinery already honors):

| role | word | does | default tier |
|---|---|---|---|
| orchestrate | the voice | head + task-orchestrator turns | mid |
| plan | the architect | compile/replan/graph revision | high |
| work | the hands | worker/executor turns | mid-high |
| verify | the skeptic | gates, judges, verify-then-escalate probes | cheap |
| scribe | the clerk | labels, titles, folds, briefs, sentinels | ultra-cheap |

(vision stays a capability flag resolved within a role, not a sixth role.)

**The scribe tier is the money move** — a whole class of calls that currently
either use the chat-tier model or live as hand-rolled heuristics in the view
layer, all of which are one-sentence-out jobs an ultra-cheap model does
indistinguishably well:

- task/card labels (replaces the `deriveNodeLabel` view-layer engine — 8.2.13)
- thread titles and session names
- arrival-brief compression; fold summaries when coalescing threads
- narrator progress lines (`speakProgress` today runs richer than it needs)
- charter sentinel checks (already "one small model call" — bind to scribe)
- toast/notification one-liners; settled-card recall snippets
- auto-effort classification (the oh-my-pi trick: tiny model classifies turn
  difficulty with a hard timeout; result discarded if superseded)

**The ordering law that keeps it honest: deterministic first, scribe second,
frontier last.** Receipts, counts, states, and anything a template can say
stays template (free beats ultra-cheap). Scribe handles prose-shaped
one-liners. Verify runs before any escalation (the Phase-A result:
verify-then-escalate beats probing). Frontier tiers are reserved for judgment.

**Surface, in our language** (adapted, per 7.1 — no role tables copied from
anywhere): the chip grammar from 5.10/5.17 carries it all. A chip reads
`⟨role word⟩ ⟨model word⟩ ⟨ctx gauge⟩` — e.g. the task room header shows the
task's work binding, each worker row its resolved model, the home status line
the orchestrate binding. Click any chip → one palette, scoped to what the chip
governs; changing at task scope rebinds inheriting descendants at their next
call, pinned nodes hold. The settings surface shows the five roles as five
rows — not a model catalog — with the catalog one level deeper. Spend rails
(existing) stay the guardrail; the roles ladder is how spend is *steered*,
the rail is how it is *stopped*.

**What we deliberately do not build**: per-message model syntax in the
composer (chips cover it), automatic silent downgrades (predictability law —
the system may *suggest* "this looks scribe-shaped" but never switches without
a receipt), and role proliferation (a sixth role needs this doc amended).

### 5.24 The rest of the product finds its rooms

The scope model has to house *everything aforge already is*, or the parts it
doesn't house grow their own surfaces again. One line each:

- **Home scope rail, below the task cards: a collapsed dim group** — `notebook`,
  `self`, `standing`, `services`. Each is a row that **re-scopes** on enter, per
  the master-detail rule (5.15); collapsed by default so live work keeps the top.
- **`self`** re-scopes to its 9 sub-routes as rail rows; the main pane is the
  selected route. No separate self page, no page-local key grammar.
- **`standing`** lists charters as cards; a focused charter's affordances **are
  the charter command verbs** (pause, probation, cadence, retire) rendered from
  the registry (5.22).
- **`services`** rooms are a log tail + `stop` / `restart` / `auto-restart`, with
  **the composer disabled** — a service is not a conversation, and the composer
  must not pretend otherwise (5.15's one rule).
- **Row 0 `aforge`** holds the home-thread list with a `+ new` row at its foot —
  this replaces `/new` as the visible door; `/new` remains as accelerator.
- **Arrival brief** = the **first transcript block on attach**, not a banner or a
  dock; its items link to rail rows (clicking "3 tasks settled" selects them).
- **Consent-desk questions** = an amber `?` on the **owning task's card**,
  answered inline from whichever room the user is in. The question dock dies.
- **Spend**: the money segment in the status line is clickable → **inline numeric
  editor**, the same pattern as charter cadence editing. Rails are edited where
  they are shown.
- **Voice**: the mic lives on the **place-line row of every composer** and targets
  **the selected surface**. Dictation into a steer line renders visibly one-way
  (`↦`), so voice never smuggles a chat into a room that has none.
- **Attachments**: the place line (5.19) is the labelled **drop target** — the
  same line that names the ground the files land on.
- **Multi-window / tmux**: each window **binds to a room via its seen edge**;
  windows without the resident lease are **visitor windows** — they render all
  rooms **read-only**. Only the resident process runs orchestrator loops (4.6),
  so N windows never mean N orchestrators.
- **Web surface (future)**: the **product model** — rooms, scopes, cards, the five
  semantic meanings, the predictability rules (5.20) — is **surface-independent**.
  The **glyph table, motion budget, and cell-craft** (5.13 / 5.16 / 5.17 / 5.21)
  are **TUI-only renderings** of it. Stated here explicitly so a web port ports
  the model and re-skins the craft, instead of re-litigating either.

## Part 6 — Open decisions (to settle before building)

1. **How much autonomy for `answer_question`** — head answering worker questions
   collides with STANDING.md's consent doctrine; probably: head may answer
   informational questions, must escalate consent-class ones.
2. **Spawn-as-tool vs router-terminal** — spawn-as-tool is the orchestrator model,
   but the fan-out cap (6) and consequence gates must move into the tool.
3. **Thread table migration** — new projection from the same journal (cheap, replay)
   vs schema migration of `messages`.
4. **Rail always-on vs toggle** below `railAtWidth`; narrow-terminal story.
5. **Whether the belt/recognizer removal happens in one wave or the recognizers are
   absorbed as tools incrementally** (risk: two brains coexisting again mid-flight).

## Part 7 — Inspiration doctrine + micro-interaction backlog

### 7.1 Inspiration, not imitation

oh-my-pi (and any other harness we study) is a source of *problems solved*, never
of visual language. We take: what information they chose to surface, which
interactions they proved matter, formatting problems they hit. We do NOT take:
their colors, their box/border style, their glyph choices, their status-bar
layout verbatim. Everything renders in our own language: the five-word pastel
vocabulary (5.16), the three-tier grey ramp (5.13), our glyph set (5.17),
whitespace over boxes, one pulse per room. Familiar in grammar (it feels like a
chat harness you already know), ours in taste. If a ported idea arrives wearing
oh-my-pi's clothes, the port is not done.

### 7.2 The minute things — standing backlog

The small mechanics that make a harness feel effortless. This list is meant to
be appended to continuously and burned down across waves; nothing here is
decoration — each is muscle-memory the user already has from other tools.

**Composer**
- [ ] ↑/↓ on empty draft = input history recall, in EVERY composer (home, task
      rooms, steer lines) — the draft ring exists in home chat today
      (`tui/draft.go`); parity everywhere is the requirement
- [ ] esc stashes the draft; the stash is restorable; never destroys typed text
- [ ] type-ahead while the orchestrator is mid-turn: messages queue visibly and
      fold into its next turn (head coalescing already supports the fold)
- [ ] ctrl+j newline; bracketed paste; multi-line paste never auto-sends
- [ ] `@task` mention from home = addressed dispatch without changing rooms;
      inline fzf-style filter on `@` (5.18); enter sends & stays, ctrl+enter
      sends & follows; dispatch echo row in home + one-time rail pulse
- [ ] queued type-ahead renders as visible chips under the composer until
      absorbed (5.20 rule 4); esc-to-interrupt hint on the awaiting line
- [ ] place line above composer: room-scoped workspace path, fish-abbreviated,
      click/`y` copies, doubles as tab-completion root (5.19)
- [ ] `/` palette with fuzzy match; tab completes; numbers answer questions
- [ ] tab path-completion against the selected task's workspace
- [ ] backspace on empty draft removes last attachment (exists — keep)

**Navigation**
- [ ] ↑/↓ or j/k moves rail selection; main pane follows instantly (5.15)
- [ ] enter descends scope; esc pops scope; ‹ header clickable
- [ ] ctrl+k global switcher: fuzzy across all rooms/tasks/settled history
- [ ] 1–9 jump to nth rail row in current scope
- [ ] end / "back to now" pill when scrolled up; unread count per room on rail
      cards; reading position preserved per room across switches
- [ ] `[` `]` split resize, persisted (exists — keep)
- [ ] shift+j/k on a *pending* rail row = reprioritize (journals the existing
      command; illegal states simply don't move)

**Transcript**
- [ ] anchor-preserving scroll across re-renders (exists — port the invention)
- [ ] per-block expand/collapse (▸/▾) with state that survives re-render;
      `v` toggles receipts (exists)
- [ ] y copy last answer / Y copy deliverable path (exists); y on a focused
      block copies that block
- [ ] in-thread search (ctrl+f) backed by the existing FTS table
- [ ] file paths clickable everywhere, including inside collapsed tool rows
- [ ] timestamps appear on focus, not ambiently (tier-4 information)

**Rail cards**
- [ ] focused card expands telemetry in place (5.9); enter commits to scope
- [ ] model chip interactive on any live card → palette → `CommandSetModel` (5.10)
- [ ] c cancel / r restart on the selected card with inline y/n confirm — no
      modal, confirm text names the blast radius ("cancel 4 running workers?")
- [ ] ⚑ rows reveal what they wait on, on focus (edges exist in plan-sight)
- [ ] stable ordering — cards never re-sort themselves while visible; badges
      pull the eye instead (5.16); new tasks slide in at top, once

**Global**
- [ ] ? contextual help overlay per scope (keybindings for *this* room)
- [ ] full-screen settings with fuzzy search, arrow-key navigation, live-apply
      (grammar informed by the oh-my-pi study; skin ours)
- [ ] calm mode: freezes shimmer/typewriter, keeps caret (5.13)
- [ ] every keyboard action has click parity and vice versa — one command layer
      beneath both (the journal funnel makes this nearly free)
- [ ] terminal-title + bell integration: title shows attention count; optional
      bell only on interruption-budget events (question/delivery/failure)
- [ ] alt+↑ dequeues the last queued type-ahead message back into the composer
      for editing (oh-my-pi `app.message.dequeue`)
- [ ] OSC 133 prompt-zone markers on user messages — terminal-native "jump to
      previous prompt" navigates the chat for free
- [ ] OSC 8 + `usage —` honesty rule: missing metrics render as `—`, never an
      estimate

## Part 8 — oh-my-pi reconciliation: adopted, adapted, refused

Source: deep study of can1357/oh-my-pi (TypeScript TUI coding agent, fork of
Mario Zechner's pi). Filed under the 7.1 doctrine: we take problems-solved,
never skin. Items below are the ledger the build waves work from.

### 8.1 Adopted — rendering architecture (the TUI reset foundation)

1. **Committed-prefix / live-region transcript — adopted as a DISCIPLINE INSIDE
   ALT SCREEN.** Their `transcript-container` splits the transcript at a live
   seam: rows above it are written to the terminal's NATIVE scrollback once and
   never repainted; only the live region rebuilds. We take the discipline and
   reject the destination:

   - Blocks declare `isFinalized()`, `settledRows()`, and a `version` for
     post-final mutation, so a streaming reply can finalize its byte-stable head
     mid-stream.
   - Finalized blocks live in **one immutable-block cache** that **replaces
     today's four caches** (block cache, card blocks, brief blocks, whole-thread
     splice). Finalized means never rebuilt, not never rendered.
   - **Time-derived cells freeze at finalization** (8.1.2), and only the **live
     region** rebuilds per frame. The unit of correctness becomes "is this block
     finalized," not "is this component dirty."
   - **Post-final mutation via `version` is coherent** precisely because blocks
     live in *our* cache rather than in terminal scrollback — a byte we own can
     be re-emitted; a byte the terminal owns cannot.

   **Native scrollback is explicitly REJECTED for v1.** It is incompatible with
   the persistent rail (5.15), the 9 overlays, and mouse hit-testing: bubbletea
   drops `tea.Println` in alt screen, and leaving alt screen forfeits mouse
   capture and the rail (verified against bubbletea v1.3.10
   `standard_renderer.go:664,190`). **Therefore the anchor-preserving scroll of
   4.4 survives** — we keep our own viewport, so we keep our own anchor.

   *Note*: a rail-less **"inline mode"** using native scrollback may be explored
   later as a separate mode — never as the default.
2. **Freeze-at-commit discipline.** Any time-derived cell (elapsed, countdown)
   captures the wall clock at rebuild and reuses it verbatim once its rows
   commit — committed bytes must never drift. Corollary: our "elapsed that
   ages" (5.21) lives only in live regions (rail, HUD), never in committed
   transcript rows.
3. **One shared animation clock.** All live glyphs derive their frame from
   `floor(now/interval) % frames` — parallel running rows animate in lockstep
   as one organism, and render ticks match glyph ticks (no wasted identical
   paints). This refines 5.13's "one pulse per room": where several live glyphs
   must coexist (rail with N running tasks), phase-lock them.
4. **Fixed-velocity shimmer, eased pulse.** If we shimmer at all, drive the
   band by velocity (cells/sec) so smoothness is length-independent; the
   thinking pulse eases its frame dwell (fast at cycle edges, slow mid-cycle)
   instead of ticking flat. Both at fixed glyph width so trailing text never
   shifts.
5. **One header grammar as law.** `<glyph> <Title>: <desc> [badge] · meta · meta`
   defined in exactly one place, newline-flattened, with shared badge/fold/
   expand-hint helpers. Every block renderer uses it; ad-hoc headers are a
   review reject.
6. **Long-lived rows change color, not shape.** Their agent rows use a static
   dot for running AND done — completion settles accent → plain text — because
   shape changes reflow and spinners lie for queued-but-detached states. Adopt
   as the state axis: **accent = live, plain = settled, dim = chrome**,
   orthogonal to our five semantic hues (5.16). Amends 5.21: the rotating
   glyph ◐◓◑◒ is for *transient* tool rows only; rail cards and agent rows are
   color-settled static glyphs.

   **Reconciliation with our glyph set (5.9 / 5.17), stated precisely** — "color,
   not shape" was over-read as "shape never changes":

   - **Shape encodes state CATEGORY** and **may change at a true state
     transition** — `○` queued → `◐` working → `✓`/`✕` settled. These are rare,
     meaningful, and *should* be visible; a state change that produced no visible
     change would be the worse bug.
   - **Shape NEVER animates on a long-lived row.** No spinner shapes on rail
     cards or agent rows, no hourglass pretending a detached-but-queued node is
     busy. Animation on a row that may sit for an hour is a lie about liveness.
   - **Color carries live / settled / chrome** per the accent = live law, on every
     row, continuously.
   - **Transient tool rows may use the shared-clock spinner** (8.1.3) — they live
     for seconds and their motion is honest.

   So `○ ◐ ✓ ✕` stands as the state vocabulary; what is banned is a *dancing*
   glyph on a durable object.
7. **Collapse policies protect the live edge and the failures.** Live lists
   fold from the top so running rows stay visible; finalized lists give slots
   to failed/aborted rows first; the fold line carries a breakdown
   (`… 21 more (18 pending · 3 done)`). This is the rail overflow policy.

### 8.2 Adopted — product mechanics

8. **Narrow-terminal answer (closes Part 6 open decision 4): bounded sticky HUD.**
   oh-my-pi has no side rail at all — running work lives in bounded sticky
   panels between transcript and composer. We keep the right rail on wide
   terminals (user preference, and our scope-map model needs it), but below
   `railAtWidth` the rail's content becomes a bounded HUD stack above the
   composer (≤8 rows + fold line) instead of today's "rail replaces chat."
9. **Statements are durable, questions can be ephemeral.** Their `/btw` runs a
   side-channel turn against a session's live context — history untouched, the
   in-flight turn undisturbed. Adopt as a law completing 5.18: `@task` speech
   lands durably in the task thread; *questions* about a running task ("why
   did you pick that?") may run as ephemeral turns. Directions are journal;
   curiosity is cheap.

   **"Ephemeral" means ephemeral to the model's context, not to the record** —
   journal-is-truth and 5.20 rule 4 (no dead-air sends) both hold:

   - The exchange journals a **collapsed one-line stub** in the room where it was
     asked — `▸ asked → answered` — expandable, referable later, durable. The echo
     the user needs exists; it just doesn't take a turn's worth of space.
   - The **full exchange stays out of the orchestrator's context and out of its
     prompt cache**: it must land **below the volatile prompt floor**, or under
     **its own cache key**. The head's prefix ordering is cache-critical and
     regression-tested — an ephemeral turn that reorders the prefix costs more
     than the turn it saved.
   - **Stream plumbing prerequisite**: stream events need **session keys** before
     ephemeral turns can ship — today's `StreamEvent` has no session field, so an
     ephemeral turn's deltas would render into whatever room is listening
     (Part 9).
10. **`read thread://<task>` — one chat reads another as a tool call.** Their
    `history://<id>` renders any agent's transcript as concise markdown on
    demand. Amends the 5.7 knowledge contract: the main head still never sees
    task transcripts *ambiently*, but gains an explicit on-demand read tool —
    cheaper and more honest than waking the task head for `ask_task` in the
    read-only case. `ask_task` stays for questions needing the task head's
    judgment.
11. **Dispatch receipts.** Their inter-agent sends return
    `injected | woken | revived | failed`. Our addressed dispatches (5.18) and
    head→task redirects (5.3) surface the same vocabulary on the echo row
    (`→ wisp-parity · woken`), mapping naturally onto resident lease states
    (task orchestrators sleep between turns; parked → revived).
12. **Fork-this-conversation-into-a-task** (`/tan`): spawn a top-level task
    that inherits the current conversation as context — "take what we just
    discussed and go do it." A spawn-tool variant, not a new mechanism.
13. **Tiny-model UI labels.** Their spawn schema has NO description field — a
    tiny model asynchronously compresses each assignment into a one-line label;
    the frontier model never writes UI copy. **Directly deletes our view-layer
    label mini-engine** (`deriveNodeLabel` and friends; Part 2's TUI indictment) and
    fits the model-routing philosophy.
14. **Brief format as prompt contract, not schema.** Goal/Constraints/Contract
    (shared context) and Target/Change/Acceptance (per work order) are
    markdown headings the spawn prompt mandates, rendered as markdown in the
    task block — self-documenting transcripts, zero schema weight. aforge's
    fan-out contracts adopt this shape.
15. **Plan steps lit by live execution.** Their pending todos highlight when a
    running subagent's description fuzzy-matches. Our DAG knows the join
    *exactly* — light the plan-step row accent while its worker runs (sub-rail
    + step dots).
16. **Model roles table.** Named, colored, cycleable slots (`@plan @work
    @judge @labeler …`) with per-role provenance and a mid-session cycle key —
    generalizes `AFORGE_PLAN_MODEL`/boost into one table; `CommandSetModel`
    (5.10) sets a role binding, scoped session/task. **Single home, same words as
    5.10**: role **bindings** (5.23's five roles) are the **storage**; the **chip
    is the only surface**. **Boost** is a transient escalation of the **work-role
    binding**, shown as `⚡` on the chip — there is no separate boost UI, and the
    roles table is a settings *view* of the bindings, not a second place to set
    them.
17. **Dual context thresholds.** Warn at `min(percent, absoluteTokens)` so a
    1M-window model warns at 150k tokens, not 500k. Our ctx gauge (5.17)
    adopts this for its amber point.
18. **Steering/follow-up/interrupt as explicit user choices.** Queue-vs-
    interrupt is a visible setting (immediate | wait), queued messages render
    as chips (we had this), and alt+↑ pulls a queued message back into the
    composer for editing.
19. **Settings grammar.** Full-screen, tabs → groups, **any printable char
    starts global fuzzy search across all tabs**, search results as navigation
    (tab bar becomes a filter breadcrumb), live previews, live-apply with
    debounced atomic writes, condition-gated rows appearing the moment their
    parent flips. Skin ours (calm, two themes, no 98-theme gallery).
20. **Formatting laws.** `1.5K/25K/1M` number ladder; `4.5s/3m12s/2h14m`
    duration ladder; `5.1%/1M` context form; active-time = union of turn
    windows (idle never counts); parent usage sums direct rows only (no
    double-counting children); **missing data renders `—`, never an estimate**.
21. **Esc non-negotiables** (their ladder's hard rules, ours by law): esc never
    destroys a non-empty draft; every cancellable maintenance state advertises
    "(esc to cancel)".

    **The reconciling rule — esc acts on what you are watching** (settles 5.20
    rule 6 against "esc never interrupts"):

    - If **the current room's transcript is actively streaming a turn**, esc
      **interrupts that turn** — and the awaiting line advertises `esc interrupt`
      **only then**.
    - Otherwise esc **navigates**: pops scope, stashes the draft.
    - Esc **never interrupts a turn you are not watching** (stepping into a room
      to leave it must not kill its work), and **never destroys a draft**.

    One key, one question answered on screen: whatever the awaiting line currently
    says is what esc will do.
22. **Prose-only thinking at trace depth.** Their thinking display elides code
    fences inside reasoning down to `…` — read the prose, skip the scratch.
    Adopt for worker-trace thought rows (5.12 placement unchanged: home never
    streams thinking).

### 8.3 Refused — with reasons

- **Their skin wholesale**: powerline separators (font-fragile; we use `·`),
  `⟦⟧` badge brackets, 98 themes, nerd-font preset, rainbow ultrathink
  gradient, emoji telemetry icons (`💾 ⚡ 👥` violate 5.17). Our five-hue pastel
  language + grey ramp stands.
- **HUD-only layout** (no rail) — refused for wide terminals; the rail is our
  scope map (5.15) and the user's stated preference. Adopted only as the
  narrow fallback (8.2.8).
- **A separate Agent Hub screen** — their control room exists because they
  lack a persistent rail; our scoped master-detail IS the hub. No fullscreen
  roster; ctrl+k covers cross-scope jumps.
- **Session trees / branching / checkpoint-rewind** — powerful, but aforge
  threads are execution-anchored conversations, not exploration trees; the
  journal is append-only truth and the DAG already carries the branching.
  Revisit only if head-side exploration ("try two plans, keep one") becomes a
  real journey.
- **Focus proxying as the primary navigation** — theirs repoints the whole TUI
  at a child session; ours re-scopes rail + pane (5.15), which keeps the map
  visible. We do adopt its one lesson: dim/tint global chrome while scoped so
  you always know which room you're in.
- **`ultrathink`-style magic keywords** — cue-word triggers are exactly the
  lexical-trip disease we're removing from the head (Part 2.2).

### 8.4 Amendments to earlier sections

- 5.7 knowledge-contract table: main head row gains "on-demand
  `read thread://` (never ambient)" under *sees*; `ask_task` reserved for
  judgment questions.
- 5.13: "one pulse per room" → "one shared clock per screen; phase-locked
  glyphs count as one pulse."
- 5.21 rotating state glyph: transient tool rows only; long-lived rows are
  color-settled (8.1.6). Shape may still change at a true state transition —
  see the reconciliation in 8.1.6.
- Part 6 open decision 4 (narrow terminals): resolved by 8.2.8 for the live
  summary — but the *scope map* itself is width-independent (5.15), and at 80
  columns the narrow path is the primary experience (Part 9).
- 4.4's committed-prefix requirement: adopted as an in-alt-screen discipline;
  native scrollback refused (8.1.1). The anchor-preserving scroll survives.

## Part 9 — Feasibility corrections and build prerequisites

(Added 2026-08-10 after an adversarial review verified this doc's claims against
the code at `d9fc533`. Everything here is a correction of fact or a precondition
the design assumed and the code does not provide. Where an item contradicts an
earlier section, this Part wins.)

1. **Per-session head cursors are a CORRECTNESS PRECONDITION, not cleanup.**
   `initialCursor` uses `LastNonUserMessageSeq` with **no session filter**
   (`store/thread.go:546-553`) and the poll reads **all sessions on one
   watermark** (`head/head.go:287,294`). With N rooms, any reply advances the
   single watermark past *other rooms'* unanswered rows and strands them. Rooms
   are not shippable before this; 4.2's bullet understated it.

2. **No `sessions` table exists.** `session_id` is an untyped column
   (`thread.go:264-280`) with no row of its own — no title, no created-at, no
   surface, no lifecycle. Cheap to add, and required for rooms (4.2).

3. **The command funnel has NO issuer/authority axis.** `requestCommandTx`
   (`thread.go:646`) and `validateNodeCommand` (`thread.go:1006-1043`) check only
   *what* and *state* — never *who asked*. Before any multi-agent authority
   (4.6 v2): add `Command.Issuer` (`user | main | task:<root> | reconciler`) plus
   **one** check — a task-issued command must target inside its own subtree. Note
   also that root-target commands are **refused today**
   (`thread.go:1010-1012`), so "cancel everything" is N commands, not one.

4. **Question CLASS does not exist.** Only `QuestionUrgency` exists — blocking /
   next-natural-moment / whenever (`agent_questions.go:19`). The consent-vs-
   informational axis that 5.7 and Part 6 open decision 1 both lean on must be
   built as a **compiler-side label on `AskQuestion`**, with a conservative
   default: **unlabeled ⇒ consent ⇒ escalate**. This lands **before** any
   `answer_question` autonomy is granted, not alongside it.

5. **Per-task budget ceilings do NOT exist.** The rail is daily + global
   (`usage.go:446`); only charters carry `PerFiringBudgetUSD`. 5.8's "budget
   rails per task already exist" is **corrected**: they must be built, and they
   are a prerequisite for orchestrator turns billing to their task.

6. **`CommandSetModel` needs a NEW reconciler arm.** The command-kind list is
   closed (`thread.go:112-176`), and `surgery.go:193-203` is *restart
   inheritance* — it mints new nodes. **No live-node model change exists.**
   5.10/5.23 are correct that the plumbing is familiar, and wrong that it is
   present.

7. **Delivery/question re-homing fights rooms.** `deliverySessionID`
   (`resident.go:1956-1967`) re-addresses deliverables to whatever session is
   currently attached; `surfaceQuestionInto`'s rehome + `LastSeen` read does the
   same for questions. Under rooms, **deliveries pin to the OWNER thread**, and
   re-homing dies together with the `LastSeen` singleton.

8. **`announceNode` speaks only for root-parented nodes**
   (`resident.go:1990-1993`). Task rooms need the **inverse** policy: a task
   orchestrator narrates **its own subtree** and **never leaks upward** — up-flow
   stays the three interruption-budget events (5.4).

9. **Stream events carry no session key.** `StreamEvent` has no session field
   (`tui/stream.go:29-33`) and the observer is installed once
   (`chat.go:1319-1337`). Multi-room streaming (4.3) and ephemeral turns (8.2.9)
   both require keyed events — without them, deltas render into whichever room is
   listening.

10. **The consent desk lives in the window brain only** (`chat.go:3006,1454`).
    Resident-side orchestrators (4.6 v2) cannot reach it. It must move to a
    package reachable from the resident — part of the 4.5 `chat.go` dissolution,
    and a hard dependency of v2 rather than a tidiness item.

11. **Corrected counts.** `PostMessage` has **37 call sites across 19 non-test
    files** (not ~21 — Part 2.8 corrected). The head's tool belt also includes
    **manual / read / competence / plan**; 4.1's "(exist)" enumeration was
    incomplete.

12. **`railAtWidth` is 100** (`tui/model.go:35`). At 80 columns — an ordinary
    terminal — the narrow HUD path is the **primary experience**, not a
    degradation. The scope model must therefore be width-independent (5.15), and
    any design that only works with a rail on screen is a design that mostly
    doesn't work.

## Part 10 — External survey fold-in (five sources, adopted under the 7.1 doctrine)

Sources: deep-dives on Warp 2.0/Oz, Cursor CLI, opencode (anomalyco, OpenTUI
rewrite); a nine-target field synthesis (Claude Code, Codex, Gemini CLI, Crush,
Amp, goose, aider, Factory, Antigravity CLI); and a TUI-craft survey of
terminal protocols, acclaimed TUIs, and framework releases. Ledger below; skin
never travels (7.1).

### 10.1 Build-stack decisions

1. **Bubble Tea v2 + Lip Gloss v2 are the build target** (shipped 2026). They
   deliver: declarative view management, keyboard-capability negotiation,
   synchronized-output atomic frames, bandwidth-proportional rendering (the
   right metric is bytes over SSH, not FPS), and a **cell-based layer
   compositor with per-layer mouse hit-testing** — which deletes the 31
   hand-maintained `paneBounds` rects and 45 `.contains()` sites outright.
   This is a decision, not an inspiration.
2. **tmux reality check**: tmux 3.7 (2026-06) honors synchronized output and
   forwards OSC 9;4 progress — atomic frames and taskbar progress now work for
   tmux users. tmux will never support the kitty keyboard protocol → KKP is an
   enhancement only; no important chord may require Shift+Enter/key-release;
   ship a legacy send-vs-newline binding. Never depend on focus events
   (tmux default-off). `COLORTERM` inside tmux is tmux's claim — verify like
   Charm's colorprofile does.
3. **Golden screenshot tests in CI**: render the UI as text at every width
   1–110 × height 1–20 in both themes (Crush's harness) — the regression net
   for exactly the ghost-frame/scroll defects the task-page campaign fixed by
   hand. Text-render tests are agent-verifiable.
4. **Anti-jank ledger**: debounce resize to 16–30ms and coalesce (multiplexer
   resize storms); suppress no-op style updates; never break bottom-anchor on
   trackpad scroll; animation interruption-correctness over curve choice;
   animate opacity/offset, never layout. Formalize `calm` as a real
   reduced-motion flag (no framework ships one).
5. **Linear accessible mode**: single column, no spinner, no cursor jumps,
   hideable cursor — the calm doctrine made literal, and the fix for screen
   readers (the 2026 accessibility critique used a chat TUI as its worked
   example). Same product, less motion.

### 10.2 Security and honesty laws

6. **Sanitize all model/tool output before it reaches the terminal.** Untrusted
   text carrying OSC 52 (clipboard write), OSC 8 (link spoofing), OSC 0/2
   (title rewrite) *executes* in most emulators. Every protocol we adopt is a
   channel someone else's text can drive. Only tcell hardened this in 2026; we
   do the same at the one output chokepoint.
7. **Remap raw ANSI-16 in tool output to our palette** (Crush) — tool output
   arrives in someone else's colors; recoloring at the same chokepoint keeps
   visual coherence without touching content. Composes with the sanitizer.
8. **Honesty marks**: `~` prefix on estimated numbers; missing data renders
   `—`, never an estimate; a `W`-style glyph past the context warning point
   (dual thresholds per 8.2.17).

### 10.3 Orchestration interactions

9. **Approval teleport** (Antigravity — the best single idea in the field
   survey): one key jumps to the next task awaiting a human, one key approves
   in place without leaving your room. This answers our hardest journey — a
   DAG with five blocked nodes and one human. Rail integration: amber `?`
   badges + `alt+j` next-blocker + inline approve. Backlogged in 7.2.
10. **Per-child telemetry footer + genealogy buttons** (opencode): inside a
    task room, the sub-rail header reads `(2 of 5) · 34K (17%) · $0.42` with
    Parent/Prev/Next as *clickable buttons with their shortcuts printed on
    them* — discoverability by construction; adopt for our scope header.
11. **Both surfaces for spawned work** (Crush + opencode): committed transcript
    gets a compact inline tree at birth/settle; the rail carries the live
    traversal. Matches our card-at-birth law; adopt the tree glyph form.
12. **Esc ladder refinement** (Crush): with queued messages, first esc clears
    the queue (less destructive) before any cancel arming; the armed
    "press again to cancel" state *mutates the footer text* — visible state,
    2s window. Merges cleanly into 8.2.21's "esc acts on what you're watching."
13. **Queue pills** (Crush): `▶▶▶▶▶ 5 queued` — one glyph per queued item,
    capped, pre-attentive count for our type-ahead chips.
14. **Timeline browser** (opencode/Codex): a jump-to-message dialog over the
    room's user turns, cursor-synced to transcript scroll. Adopt as history
    *navigation*; forking stays deferred (8.3), but if it ever ships, the
    rewind picker must show per-turn modified-file counts (Gemini) and offer
    conversation/files/both.
15. **Negative example** (Crush background jobs): background work with no
    panel and no kill affordance goes invisible. Our rail must always carry
    every live thing — no silent lanes.

### 10.4 Approvals and trust

16. **Typed rejection is steering, as the default path** (opencode/Cursor):
    rejecting any permission opens "tell it what to do differently"; the
    reason becomes the redirect. Now law for the consent desk.
17. **Dialog mechanics** (Crush): letter mnemonics (a allow / s session /
    d deny), `t` diff-style toggle, `f` fullscreen, forced fullscreen below a
    stated size threshold; a denied tool still renders the diff you rejected;
    chained shell commands re-prompt even when the head command is allowed.
18. **Scope shown, scope editable**: "allow always" lists the exact patterns
    it will whitelist before confirming (opencode); editing the pattern at
    grant time (Antigravity) lives behind the fullscreen escalation, off the
    fast path.
19. **Trust preview** (Gemini): the workspace-trust dialog is preceded by a
    scan listing what trusting would activate (commands, hooks, watchers) —
    preview-before-consequence applied to trust itself.
20. **Multi-client correctness** (Crush): a question answered anywhere closes
    everywhere — required for our visitor-window story (5.24).
21. **Governors become permissions** (opencode `doom_loop`): our overrun
    governors surface as legible, per-scope-overridable permission rows
    (default ask), last-match-wins ordering.

### 10.5 Information engineering

22. **Footer columns with priority-based dropping** (Gemini): the status line
    is a registry of columns that drop lowest-priority-first on narrow
    terminals — it shortens, never wraps. Our contextual footer adopts the
    mechanic.
23. **Health vs cost split** (opencode): system health (services, MCP-ish
    states, pending questions) lives in the footer with show-only-when-pending
    glyphs; this-turn cost/context lives on the composer's meta strip. Two
    homes, never mixed.
24. **Responsive breakpoints stated as numbers before building** (Crush): we
    write the aforge table (rail threshold, split-diff threshold, dialog
    fullscreen threshold, paste-to-attachment threshold) as part of the rail
    wave, not discovered later.
25. **Personality placement** (Crush): status labels stay factual; personality
    (sparingly, our voice) lives only in idle placeholders where it costs
    nothing.
26. **Contextual hint line** (Warp): the line under the composer changes with
    input state (empty / typed / last-action-failed / queued) — our 5.22
    contextual footer, validated; add the "attach failed output as context"
    hint pattern.
27. **Notification taxonomy** (Warp/opencode consensus): exactly three
    desktop-notifying events — needs-input, delivery, failure (our
    interruption budget, again) — probed OSC 99 → 777 fallback, focus-gated,
    never for progress; progress goes to OSC 9;4 taskbar + title only.
28. **Plan as annotatable artifact** (Gemini/Factory): the plan is a markdown
    artifact you open and comment in; approval starts implementation — fits
    the craft/plan-doc store directly. Candidate for the plan-consent flow.
29. **DAG render ladder** (Antigravity): plan/graph visuals degrade
    graphics → ASCII → source, chosen by probe. Our rail DAG adopts the ladder
    shape (we start at ASCII; the ladder names the ceiling and floor).
30. **Statusline contract** (Antigravity): if we ever expose a user statusline
    script, the JSON payload carries orchestration state (task counts,
    attention counts), not just model/cwd.

### 10.6 Refusals (with reasons)

- Model-generated ghost-text completion of the user's prompt (Gemini) —
  latency + predictability risk; our composer stays inert until asked.
- Scriptable-TUI plugin surface (opencode) — Amp's four-primitive ceiling
  preserves a design system; plugins may add data, never chrome.
- User theme galleries — one calm identity, two modes, `system` escape hatch
  only.
- Embedded pty focus-handoff (Gemini) — genuinely novel, deferred; the
  focus-handoff *pattern* is noted for future child surfaces.
- Web-relay architecture for a future web surface (textual-web's dead model) —
  when the web lens comes, it is a client of the journal, not a terminal
  mirror.

## Part 11 — Branch strategy and build waves

### 11.1 Branch and disconnection strategy

- Branch **`chat-v2`** cut from current trunk (`d9fc533` lineage). All waves
  land here as stacked merges; trunk stays shippable.
- **Disconnect, don't delete**: the existing chat TUI keeps working untouched
  while the new surface grows beside it behind `aforge chat --v2` (env
  `AFORGE_CHAT_V2=1`). The engine seams (Wave 0/1) are shared and land first;
  the old TUI runs on them unchanged. Default flips to v2 only when the parity
  checklist (journeys in 5.3–5.5, JOURNEY.md's 19, micro-backlog criticals)
  passes; the old chat is deleted one wave *after* the flip, never the same
  one.
- Every wave ends with `make check` + the golden screenshot suite once it
  exists (10.1.3). Builders: Opus for engine/architecture waves, Sonnet for
  well-specified surface work; every wave brief cites this doc by section.

### 11.2 Waves

- **Wave 0 — prerequisites (engine, invisible).** Part 9 items: sessions
  table + per-session head cursors; `Command.Issuer` + subtree authorization;
  question class axis (conservative default); per-task budget ceilings;
  `CommandSetModel` reconciler arm; keyed stream events; `thread.Post` single
  door; delivery/question re-homing → owner-thread pinning; announceNode
  room policy. Old TUI unaffected throughout.
- **Wave 1 — seams (engine).** chat.go dissolution (4.5): consent desk,
  commander, plan revision, provider pool into `internal/` packages; command
  registry (5.22) as a real package; model roles table (5.23) + scribe-tier
  rewiring (labels, titles, briefs, narrator, sentinels); output
  sanitizer + ANSI remap chokepoint (10.2).
- **Wave 2 — shell (new TUI skeleton).** Bubble Tea v2 migration; derived
  token layer FIRST (10.1, 5.16); committed-prefix discipline blocks; one
  transcript + composer + place line + contextual footer; home thread against
  the one head; breakpoints table (10.5.24); golden test harness.
- **Wave 3 — rooms.** Scoped master-detail rail (5.15, width-independent);
  task rooms as views (4.6 v1); dispatch grammar (`@`, receipts, echo rows);
  approval teleport (10.3.9); consent dialogs (10.4); registry-driven action
  strips, palette, `?` surfaces.
- **Wave 4 — depth.** Model chips + palette + mid-run rebinding; settings
  surface (8.2.19 grammar, our skin); 5.24 homes (notebook/self/standing/
  services/voice/spend); micro-backlog burn-down (7.2); linear accessible
  mode; notifications (10.5.27).
- **Wave 5 — orchestrators (v2 of 4.6).** Resident-side per-task loops;
  `answer_question` with class gates; ephemeral asks; `read thread://`;
  fork-conversation-into-task; headless parity closes the lens law.

Wave order is dependency order, but Waves 2–4 parallelize internally across
builders once Wave 1's seams exist.

## Part 12 — Build ledger (amendments and findings from the waves; later parts win)

### 12.1 Wave 0 batch 1 (landed: dec816e question class, d7a485d keyed streams,
8c724a9 sessions+cursors, 2345389 task budgets)

1. **9.1 refined by the build**: the head never "served one session at a time" —
   one head process tails every room and answers rows in journal order
   (foldAhead/step already stop at session boundaries). The fix is per-room
   *watermarks*, not a per-session head. The dominant stranding path is
   restart/resume (global `LastNonUserMessageSeq` skipping rooms whose unanswered
   rows sit below another room's newest reply); the secondary is a mid-turn fold
   jumping the shared cursor. "Any reply advances the watermark" was right in
   effect, wrong in immediacy.
2. **New invariant (not in Part 9)**: once "what has been read" and "what is
   owed" are different numbers, the head poll's read position must advance on
   EVERY row walked — including skipped ones — or a page consisting entirely of
   another room's settled history spins the poll forever. Regression-locked by
   `TestAPageOfAlreadyAnsweredRowsStillMovesThePoll`; later waves must not
   "simplify" that line.
3. **Empty rooms cannot exist yet**: `sessions` is a projection of the messages
   that name it, so a thread switcher that mints a room before its first message
   needs a `session_opened` journal event first. Wave 3 prerequisite; deliberately
   not invented in Wave 0.
4. **Question-class survey**: every existing `AskQuestion` producer is genuinely
   consent-bearing; none were labeled informational. The class axis is pure
   capability until a producer earns the label.
5. **Task-budget follow-ups**: (a) the media `BeforeSpend` gate in chat.go gains
   the task-rail check during the Wave 1 dissolution; (b) the overrun-replan gate
   must NOT defer on task rails until `PendingOverruns` is task-aware (today it
   would stall the whole graph); (c) whichever wave surfaces ceilings must land
   the raise-consent interception in the same wave (`head` handles
   `PendingDailyRailApproval` only) or a stopped task is unresumable from chat.
6. **Perf trap ledger**: the subtree-spend query must remain a `CROSS JOIN`
   (planner otherwise full-scans the whole `usage` table per admission check);
   `NodeModels` (usage.go) has the same pre-existing shape, off the hot path.
7. **swepro test failures are pre-existing on the base commit** (hardcoded
   homedir fixtures, float-parity fixtures) — outside `make test`'s curated set;
   not Wave 0 fallout.

### 12.2 Amendment to 9.7/9.8 for Wave 0 batch 2: pinning lands behind the seam

Owner-thread delivery pinning and the announceNode subtree policy are observable
behavior in the OLD surface (today a headless task's deliverable re-homes into
whatever chat window attaches — that journey is load-bearing until v2 rooms
exist). Part 11.1's disconnect-don't-delete therefore governs: batch 2 builds
both policies as a single policy seam selected once (legacy | owner-pinned,
`AFORGE_CHAT_V2=1` choosing owner-pinned), default legacy, both modes tested.
The legacy path dies with the old chat, one wave after the default flips.

### 12.3 Wave 0 batch 2 (command authority + SetModel; room-policy seam)

1. **Issuer default is `user`, resolved at read.** Empty stays empty on the wire
   (old events replay byte-identical); `Command.Authority()` resolves `""` →
   `user` once, at the reader. `user` because it is true of every existing
   producer and it is the MAXIMUM authority — a future narrowing of `main` can
   never retro-restrict the legacy journal. Unknown/malformed issuers (`task:`,
   blank root) are refused, never promoted to the trusted default.
2. **The subtree check walks UP from the target** (bounded by depth, not task
   width), one indexed recursive CTE, zero cost for non-`task:` issuers. An
   untargeted splice from a task issuer is refused (inside nobody's subtree);
   charter/service/global targets refuse under the same single rule.
3. **The reconciler's command dispatch has no extension point** — `applyCommand`
   and its call sites live in resident.go with no registration seam. Batch 2
   granted a two-line exception (the `CommandSetModel` case). Any wave adding a
   command kind must edit resident.go; Wave 1's dissolution should consider a
   dispatch seam if a third arm ever wants in.
4. **9.6's real shape**: live model rebinding is event-per-node in the
   `EventNodeWorkerChanged` mould (a root-scoped event would re-point whatever is
   live at REPLAY time, not command time). Rewriting `nodes.work_model` IS the
   next-provider-call semantics — dispatch re-reads the row; nothing running is
   touched. `run_model` moves; `plan_model` never does; settled work keeps the
   model it ran on; idempotent re-asks journal nothing.
5. **Reasoning effort has no axis to ride** — no Provenance/nodes field; effort
   is encoded inside the model slug (catalog normalization keeps `~` variants).
   5.10's "effort rides the chip" therefore needs a Provenance change + doc
   amendment before `CommandSetModel` can carry effort separately; deliberately
   not invented in Wave 0.
6. **9.7 corrected**: under owner-pinned, `LastSeen` survives as the FALLBACK for
   ownerless items (an ownerless deliverable goes to the attached room; with no
   room at all it stays silent-but-not-lost on the board). The singleton fully
   dies only when a system/home room exists to own the ownerless.
7. **9.8 in Wave 0 is re-addressing only** — which events announce is unchanged;
   `announceRoom` is the single resolution point where the room boundary becomes
   real when Wave 3 rooms land. `AFORGE_CHAT_V2` is registered as operator
   plumbing (a which-surface switch that dies one wave after the flip, not a
   settings row).

### 12.4 Wave 1 batch 1 (cache positions, registry, sanitizer)

1. **Position-by-volatility law applied to the head**: compiler system prompt is
   a bare const again (measured worker menu rides the end of the user message —
   position chosen over TTL so a specialist crossing its evidence gate lands at
   render time); router prompt puts the append-only thread ABOVE rewritten-in-
   place measured history; deep-slice cost renders in dimes via `dimeUSD`
   (shared with the board); revision audience count sits just above the message.
2. **Cache-audit claim (c2) was a misread and is REJECTED**: head.go's
   `| running` elapsed is the per-step duration from plan-sight (`00433f4`),
   regression-tested, not derivable from `now:` (board has no start
   timestamps). It stays. Honest residual: minute-tick churn inside the live
   snapshot block; mitigation (coarsening `boardElapsed`) is a user-visible
   plan-sight change, not this campaign's.
3. **`/model` is a liar today**: it repoints the session client locally via
   `chatCommander.SetModel` and never journals `CommandSetModel` — the Wave 0
   reconciler arm has no user door. Wiring it through the journal belongs to the
   chat.go dissolution. Registry seeds the entry with an empty Journal until
   then. Belt `steer`/`note` journal non-CommandKind things; splice/amend have
   no discrete door — all left unregistered rather than force-fit.
4. **Sanitizer coverage**: all message bodies, question text, trace tail, and
   stream deltas pass the chokepoint (three seams: poll, task-page poll, stream
   delta). NOT covered, for Wave 2's structured parts to close:
   `QuestionOption.Label/Hint` and receipt/reason text rendered outside bodies.
   ANSI-16 remap mechanism landed with identity table; palette plugs in Wave 2.
5. **Registry catalog is 39 entries**; the belt-tool gate reads toolbelt.go via
   AST (constants unexported); the store-kind gate hand-mirrors exported
   CommandKind constants. Multiple doors sharing one Kind (cancel ×3) is by
   design.

### 12.5 Three laws from a real failed session (2026-08-10, session bd3c78ed)

Evidence: user asked for an SVG architecture diagram; head authored it inline as
prose; the reply was truncated mid-stream at 1,611 chars (final call exactly 600
completion tokens — an output cap), journaled unmarked as if complete; asked to
save-and-open, the head offered, then discovered it held only the fragment, and
stopped — never considering redrawing. Net: 12 provider calls, ~78k prompt
tokens, zero deliverable, zero commands journaled. The pattern: the doc
regulated how the head SPEAKS but not what its speech may CARRY. Three laws:

1. **The artifact law** (binds Wave 2 structured-parts design + Wave 3 head
   prompt/tools): anything the user will USE outside the conversation — a
   diagram, a file, code, a document — is born on disk and referenced by a
   card/path, never carried inline as its only copy. Prose is for meaning; the
   journal is for record; the workspace is for artifacts. (Part 2.11's
   smuggling indictment, applied to outputs.) The head's belt must include a
   write-artifact door so "answer inline" is never the only route.
2. **The truncation law** (binds Wave 2 parts + the provider seam): a turn
   ended by anything other than its own completion (length cap, stream drop,
   interrupt) journals HOW it ended, and renders visibly cut. The provider
   already hands us finish_reason; it must ride the keyed StreamEvents and land
   as a message part, not be dropped. An unmarked half-artifact presented as an
   answer is a lie of omission.
3. **The repair doctrine** (binds the Wave 3 head system prompt): when a
   deliverable is lost or broken and the means of production still exist, the
   default is re-produce and say so — apology is the fallback, never the first
   move. Capability honesty (5.20) additionally requires checking deliverability
   BEFORE offering ("just say the word" then failing is the worst shape).

Scorecard for the rest of that session's failures: head unable to re-read its
own transcript → 8.2.10 `read thread://` (Wave 5); no open-affordance → 7.2
clickable paths + cards (Waves 2-3); 4-calls-per-turn machinery → the one
tool-loop head (Wave 3) + volatility cache (landed, 12.4.1).

### 12.6 Wave 1 dissolution (9f4cb3b, 98b8f5f, 37a6e22, 6c96172)

1. **chat.go: 6063 → 3895 lines.** Four packages out: `internal/provider/pool`
   (provider proper is closed by the existing config→provider import edge —
   4.5's "into internal/provider" reads "the provider tree"), `internal/consent`
   (9.10's resident-reachability boundary delivered), `internal/revision` (NOT
   internal/plan — co-working territory), `internal/command` (+ three companion
   files Go's method rules forced out of cmd).
2. **Interface collapse 28→3 is Wave 2's debt, not Wave 1's** — it requires
   editing internal/tui's test fakes, violating tests-pass-unchanged. Landed
   instead: compile-time `var _ tui.X = (*Commander)(nil)` assertions so a
   dropped method fails the build in the owning package. The 44 runtime
   assertions go when the fakes do.
3. **THE 600-TOKEN SMOKING GUN (12.5's cause found)**: the head's answer turn
   runs `ai.WithMaxTokens(600)` (head.go:880) — precisely the cap session
   bd3c78ed hit mid-SVG. Full cap table: router/answer 600, control loop 600,
   standing 800, revision voice 300, compiler 1000+2·len/3 (doubled on parse
   retry), delivery gate 400, remainder 400, retry-worker 200. Raising the cap
   is NOT the fix (the artifact law is); marking the truncation is (the
   truncation law). Wave 3's head rebuild sets caps consciously with the
   artifact door in place.
4. **finish_reason hooks for Wave 2 (truncation law)**: parsed at provider
   sse.go:161, assembled at client.go:292, then dropped by every consumer
   (`response.Text()`). Hook points: `pool.Client.CompleteWithMessages`
   (pool.go:132-143 — has the whole *ai.Response AND the attributed spend
   node) and the head's direct client call (head.go:880). Two seams, total.
5. **`/model` journaling is session-scoped by design** — one SetModel per live
   top-level job OF THIS SESSION (funnel refuses root targets; a headless
   errand beside a chat window does not move). Under rooms this becomes
   room-scoped by the same rule — 5.23 should state it before Wave 3.
   Issuer left empty = `user` via Authority(), correct for a typed command.
6. **Lock arrangements stay at the caller**: revision.Sentinel takes the
   already-wrapped plan.Completer, so unlocked-while-thinking remains
   jobPlans's property; a future resident-side caller supplies its own.
7. **Terrain markers placed** at chat.go:2947, chat.go:3134,
   revision/sentinel.go:39, revision/sentinel.go:93 for the world-grounded-
   planning handoff.

## Part 13 — Build state at the laptop→Spark handoff (2026-08-10)

Branch `chat-v2` (pushed to origin) is the build. `chat-v2-wip` (commit 5648060)
is a FROZEN SNAPSHOT of three lanes killed mid-build — DO NOT MERGE IT; mine it
for reusable code or ignore it.

### Landed on chat-v2 (all green under `make check` on macOS)

Wave 0 (all Part 9 prerequisites): question class axis; session-keyed stream
events; sessions table + per-room head cursors; per-task budget ceilings;
Command.Issuer + subtree authorization; CommandSetModel reconciler arm;
room-policy seam (AFORGE_CHAT_V2=1 → owner-pinned delivery/announce/question,
legacy default byte-identical); thread.Post single door + AST gate.

Wave 1: head prompt volatility fixes; command registry package + gates;
sanitizer + ANSI-remap chokepoint; chat.go dissolution 6063→3895 lines into
internal/{command,consent,revision} + internal/provider/pool; media task-rail
hook; /model journaling CommandSetModel; model roles table (five roles,
scope ladder, env seeds that initialize-never-override).

Wave 2 (partial): message parts + truncation marks (12.5's two engine laws);
golden screenshot harness (internal/tui2/golden); committed-prefix block
engine (internal/tui2/blocks) with anchor-preserving scroll, one latched
clock, freeze-at-commit, bounded cache.

### The three unfinished lanes (re-run these FIRST, fresh, in parallel)

1. **BT2 shell** — `internal/tui2` root + `cmd/aforge/chatv2.go`: Bubble Tea v2
   + Lip Gloss v2 shell, layer compositor with per-layer hit-testing, keyboard
   negotiation (tmux constraints, 10.1.2), resize debounce 16-30ms, placeholder
   panes, `aforge chat --v2` entry, linear-accessible stub. Was mid-way into the
   cmd entry point. go.mod/go.sum in 5648060 carry the v2 deps.
2. **Token layer** — `internal/tui2/tokens`: five-hue pastel vocabulary +
   identity wheel + grey ramp with contrast tests as the shipping gate, state
   axis (8.1.6), glyph table with width assertions, ANSI-16 remap table for
   internal/sanitize (currently Identity), breakpoints table (10.5.24),
   formatting laws (8.2.20) width-stable. Was mid test-writing; the 5648060
   copy does NOT compile (profileCount/nearest256/sgrString undefined).
   `blocks` does not block on it — it consumes a local `Styler` seam.
3. **Interface collapse 28→3** — internal/tui + internal/command: fold the
   capability shards into Backend/Commander/Streams, kill the 44 runtime type
   assertions, rewrite internal/tui test fakes onto shared bases (this lane MAY
   edit tui test scaffolding; assertions must stay unweakened). Was starting the
   fakes. Note 12.6.2: internal/command holds compile-time assertions to update.

### Then: the assembly wave (first user-testable chat)

Wire blocks+tokens+shell to the real engine: store poll → transcript blocks,
composer → head turn, keyed stream events → live region, parts → renderers
(including the visible truncation mark). Target: type, stream, scroll,
esc-interrupt in `aforge chat --v2`. THEN Wave 3 (rooms/rail), Wave 4 (chips/
settings), Wave 5 (resident orchestrators).

### Known non-blockers

- `internal/swepro/**` tests fail on the base commit (hardcoded homedir, float
  fixtures) — pre-existing, outside `make test`'s curated set (12.1.7).
- `internal/plan` `TestRenderTerrainStaysUnderTheCap` FAILS ON LINUX only: it
  mkdirs a 100-CJK-char name (300 bytes) over ext4's 255-byte filename limit.
  It is the world-grounded-planning campaign's file (co-working territory) —
  report it to them; do not silently fix.
- Co-working campaign owns internal/plan, internal/exec, internal/resident and
  merges onto chat-v2 rebasing over it. Terrain wiring markers are placed at
  cmd/aforge/chat.go:2947, :3134 and internal/revision/sentinel.go:39, :93.
