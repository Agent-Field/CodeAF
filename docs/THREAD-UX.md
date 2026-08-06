# The thread and the cards — one conversation, many living jobs

The observed failure: with several jobs in flight, the thread interleaves
their updates as anonymous bubbles — "finished the comparison, writing it
up" lands between two unrelated messages and the reader cannot tell which
job spoke. The store already knows (every message carries an optional
`NodeID` anchor, `store/thread.go:63-65`; the narrator already debounces
per-job progress, `resident/narrator.go`); only the *presentation* flattens
it. This document settles the fix. The design vocabulary is Apple's — Live
Activities, progressive disclosure, one attention stream — applied to a
terminal-first agent.

## The one-sentence design

The thread is a conversation between two parties — the user and the agent —
and every piece of *work* renders not as stream bubbles but as a **living
card**: one card per job, updating in place, docked while active, settling
into the thread where it was born when it completes.

## Decision 1 — The stream is attention; the card is state

Two kinds of content, two behaviors:

- **Conversation** (user words, head replies, askbacks, receipts of intent)
  is the stream — append-only bubbles, exactly today.
- **Work** (narration, progress, landed parts, cost ticks) is card state —
  it *mutates* its job's card and never appends to the stream.

The stream's interruption budget is three things, the three a person would
speak up for: a **question** the work cannot proceed without, a
**delivery**, and a **failure**. Everything else is ambient — visible on
the card, never breaking the conversation's flow. (The narrator's existing
doctrine — milestone-driven, debounced, failures interrupt — is exactly
this; the card is where its lines now land.)

**Rejected:** per-message job tags in the stream (colored glyph prefixes).
Attribution by decoration still leaves N interleaved monologues; the reader
does the grouping in their head. Cards do the grouping in the interface.

## Decision 2 — One card per job, four states

A card is the job's public face, derived entirely from the store (anchored
messages + node status + usage — no new tables):

| state | rendering |
|---|---|
| **compiling** | title forming: the verbatim ask + "reading this as…" receipt |
| **working** | breathing: latest narrator line, parts done/total, elapsed, cost so far |
| **question** | raised hand: the question inline, answer box focused — the card *is* the prompt |
| **settled** | receipt: ✓/✗, one-line outcome, cost; the deliverable unrolls beneath it |

The card carries its whole history: expanding it shows the rolled-up
running summary (every narrator line spoken so far, assumptions declared,
parts landed with one-line results) — "a summary of all previous, per
task," kept current by the machinery that already produces each line.

## Decision 3 — Docked while active, settled in place when done

Where a card lives answers "what is it doing *now*" and "what did it do
*then*" differently:

- **Active cards dock** in a compact strip above the input line — the
  Dynamic Island move. One line each: `◐ pricing survey · 3/7 · 2m · 12¢`.
  However far the user scrolls into history, running work stays one glance
  away. Three or fewer active jobs: one line each; more: the dock
  summarizes ("5 running · 1 question ⚑") and expands on focus.
- **When a job settles, its card leaves the dock** and its full form lives
  permanently at its birth position in the thread — where the user asked.
  The thread reads as a true history: ask, card, ask, card. The
  deliverable's landed answer (markdown, reveal, clickable paths — all
  kept) unrolls inside the settled card, not as a disconnected bubble.

## Decision 4 — Disclosure is a ladder, and every rung is a click

card line → expanded card → job graph → node flight recorder

- **Click a docked or settled card**: expands in place — running summary,
  assumptions, parts with one-line results, cost meter.
- **Click deeper** (or `enter`): the job view — the existing graph rail
  scoped to this job's subtree.
- **Click a part**: the existing node flight recorder.
- `esc` / ‹ back climbs down; the ladder never dead-ends.

Nothing new is invented here — the graph rail and node view exist; the
ladder gives them an entry point that starts at the conversation instead of
beside it. The rail stops being a second place to watch everything and
becomes where you go when a card invites you deeper.

## Decision 5 — The store already speaks this shape

- Narrator lines: `PostMessage` with `NodeID` = the job's root (anchored
  today or a one-line change). A card is `Messages` filtered by subtree —
  a view, not a table.
- Card status/parts/cost: `ActiveNodes()` + `Usage()` scoped to the
  subtree, both existing queries.
- The head's snapshot doctrine is unchanged — cards are the *user's* read
  of the workforce; the head keeps its own.
- Attach/replay: because cards are derived, `aforge attach` and any future
  web lens render the same thread-with-cards from the same journal. The
  TUI is one renderer of a store-shaped truth, which is what keeps this
  Apple-like instead of Apple-themed: the polish is in the model, so every
  surface inherits it.

## What this costs and when

Pure presentation layer over existing events: no schema change beyond
anchoring narrator messages to job roots, no new LLM calls (the rolling
summary is the narrator's own accumulated lines). Lands after the current
TUI work-in-progress merges — it touches `internal/tui` rendering and the
thread pane only.
