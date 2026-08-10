# Task-page journey + node-view audit — August 9, 2026

Second campaign. Scope: the node (task) inspection view's rendering and scrolling defects, the
cancel/restart journey, cancellation informing the plan level above, and the head's ability to
answer questions about live work. Four parallel deep-reads verified against current code
(post-waves 6a87b6b..c8ac59e and perf wave 8306500). This document is the spec for four fix
waves. Line numbers are approximate — verify before editing.

---

## Law constraints (from docs/JOURNEY.md, docs/THREAD-UX.md, manual, source doctrine)

1. **One mouth**: a node-view key/click must journal the *identical* typed command a sentence
   would (`store.CommandCancel` / `CommandRestart` via the commander) — never a second control
   path language can't reach.
2. **Exactly one visible reply**: rejected commands speak in the thread even if the head already
   spoke; a transient status line alone is not a reply.
3. **Confirm gates are absolute**: >3 open nodes, >$0.25 spent, or >5 min running ⇒ one blocking
   confirm naming count and loss (`store/surgery.go:17-27`). Cheap/young work is just done.
4. **Cancel stays cooperative**: promise "will release at the next boundary", say "$X spent
   stays spent".
5. **Verbs match state**: resume is for held work; a cancelled node's forward door is
   **restart** (`CommandRestart`, legal only on Failed|Cancelled, `store/thread.go:1037`), which
   re-splices a fresh subtree wired to the dead attempt's digest (`resident/surgery.go:149-163`).
6. **Cancel-informs-parent must not become a fourth replan authority**: it may only reach
   unstarted work, must ride the existing revision sentinel seam, and must obey the citation
   invariant + `MaxOverrunRounds=3` + `maxJobNodes=90`. A user cancel is not a failure — don't
   double-count charter probation (STANDING.md:100).
7. **No new nouns, no new modes, no permanent chrome**: node-view sticky chrome stays one line;
   affordances live in the existing footer-hint grammar; control ink, never accent; survives
   60 columns. Esc precedence (voice → node view) unchanged; single-key actions belong to the
   feed, never the steer field.
8. **Derived, not new state**: everything renderable must be readable from journaled events.

---

## Wave A — node view: ghost frames + scrolling

### A1. The tripled sticky header (user screenshot: three `‹ back … elapsed` rows)

There is exactly one header emitter (`view.go:817`). The ghosts are terminal-scroll artifacts:
any frame row whose *real* cell width exceeds its *measured* width soft-wraps, pushes the last
row past the alt-screen bottom, scrolls the screen one row, and bubbletea's line-diff renderer
then re-stamps only the changed line (the header, whose elapsed ticks at 120ms) at its nominal
row while stale copies survive above. Commit 454aa6e already named this failure mode. Three
causes, fix all:

1. **Deterministic frame-height desync**: `model.go:1211` guards relayout with
   `m.nodeViewID == ""` — a steer draft that wraps to a second input row makes the frame
   `m.height+1` lines with no `setSize`. Fix the guard so the node view relayouts too. (Same
   guard also suppresses `syncPalette` — slash-typed steers get no completion palette — and the
   `inputRecall` reset; see A2.)
2. **Unsanitized worker bytes**: the trace renderer passes raw subprocess output through
   (`exec/trace.go:135` snip only replaces `\n`; `node.go:694-856` renders it). ANSI SGR, `\r`,
   erase/cursor-motion, OSC survive into frame rows. Strip control sequences (ansi.Strip +
   filter C0/C1 except spaces/tabs-normalized) at render time in the node feed path — worker
   colors are not ours to replay.
3. **Zero-slack width math**: header and clampLines pad rows to exactly `m.width`; ambiguous-
   width glyphs (`·` U+00B7, `‹`, braille spinner) can render 2 cells where lipgloss counts 1.
   Keep one column of slack on rows that are padded to full width and mix ambiguous glyphs with
   fill (reduce the header gap fill by one, and have clampLines pad to width-1 … but keep visual
   right-alignment consistent — choose the least-visible implementation, and add a regression
   test asserting no rendered node-frame row measures > width-1 when it contains ambiguous-width
   runes).

### A2. Scrolling defects (fix all)

1. **64KB trace window re-anchoring** (`cmd/aforge/chat.go:2198` NodeTraceSince returns last
   64KB; `node.go:601` prefix check fails once the head moves → full re-parse, leading lines
   change, `SetYOffset(offset)` restores an absolute index → reader yanked every poll; expansion
   state keyed by occurrence counters also resets). Fix: anchor restore semantically (e.g. keep
   the window head stable at a line boundary between polls, or restore scroll by matching a
   content anchor line rather than absolute offset), and preserve `feedExpanded` across window
   slides.
2. **Forced bottom-jump on message land**: `landOptimisticNodeMessage` → `refreshNodeView(true)`
   (`node.go:338,343`) teleports a scrolled-up reader. Only jump when already at bottom.
3. **Resize never re-renders the feed**: `setSize` resizes `nodeTrace` viewport but never
   re-renders content (`model.go:2678-2748`); settled nodes stay wrapped at the old width
   forever. Call the node re-render on size change, and re-clamp YOffset.
4. **Mouse-move releases the reading pin**: `tea.WithMouseCellMotion` delivers motion events to
   `updateNodeViewport` → `releaseNodeTopPin()` when blurred (`model.go:1227`, `node.go:943`).
   Only wheel/click may release the pin; motion must be inert.
5. **Wheel dead zones**: `nodeBounds` excludes input frame/footer; wheel there falls through and
   releases the pin (`model.go:2924`, `view.go:230`). Route wheel anywhere in the node view to
   the feed scroll.
6. **Inconsistent scroll step**: 3 lines (empty draft) vs 1 line (blurred viewport fallthrough)
   vs 0 (focused, non-empty). Make up/down scroll the feed by 3 consistently when the key isn't
   editing text; keep pgup/pgdn; add `home` (top) alongside `end` (bottom) when not editing.
7. **Recall-state leak**: `openNodeByID`/`closeNodeView` never reset `m.inputRecall`, and the
   1211 guard suppresses the typing reset — thread up/down captured by stale recall after
   visiting a task. Reset recall state on node open/close.
8. **Attachment leak**: `openNodeByID` copies `m.attachments` but never clears; chips linger and
   attachments are dropped on close (`node.go:185,226`). Stash/restore like the draft.
9. **Steer not recallable**: `submitSteer` never calls `rememberSubmission`. Add it.
10. **Unrelated polls re-render the doc** (`model.go:2215-2227` unconditional
    `refreshNodeView(false)`): cheap now, but with A2.1's stable anchoring make re-render a
    no-op when trace and messages are unchanged.

Regression tests per defect, in the existing test style (feed_incremental_test.go,
clamp_lines_test.go, node-related tests).

---

## Wave B — cancel / restart journey + upward rethink

### B1. Two store/runner defects (fix first, they gate the journey)

1. **Invisible permanent park**: `runner.go:555` `_ = r.graph.CancelPending(...)` — on failure
   after Release, node stays Pending+cancel_requested, excluded by Ready and Claim, and
   `ReleaseOrphans` only repairs Claimed|Running rows. Handle the error (retry once, then log +
   leave a journaled trace), and teach `ReleaseOrphans` (or startup sweep) to finish
   Pending+cancel_requested rows.
2. **Partial silently dropped**: cancel path computes `outcome.Text` → `ExecResult.Summary`
   (`chat.go:847`) but `applyNodeCancelledView` writes only `error`, never `summary`
   (`amend.go:172`). Persist the partial into the cancelled node's summary (so the restart
   digest — "<id> (not run): reason" today — can instead carry what was actually written; update
   `dependencyDigest` for cancelled-with-summary accordingly: e.g. "<id> (cancelled midway):
   <first line of partial>").

### B2. The node-view affordance (state-aware, in existing grammar)

- Footer hints (hints.go:130-133) become state-aware for the blurred feed:
  running/claimed/pending → `↑/↓ read · c stop · tab steers · esc back`;
  cancelled/failed → `↑/↓ read · r restart · tab steers · esc back`;
  done → no verb (read-only).
- `r` joins `actionRunes` (keys.go) — feed-only, impossible from the steer field, exactly like `c`.
- `r` journals `store.CommandRestart` through the commander (new `Restarter` capability
  interface beside `Interrupter`, implemented by chatCommander → `RequestCommand`), the
  identical command "restart that" would journal. `c` keeps journaling CommandCancel.
- **Confirm gates**: the conversational path gates via head surgery; the key path must not
  bypass them. Route the TUI command through the same gate logic: if the impact exceeds the
  gates (>3 nodes / >$0.25 / >5min for cancel; subtree size for restart), the key press does NOT
  journal — instead it produces the same durable confirm question the head would ask (reuse
  `surgeryNeedsConfirmation` + the encoded surgery option machinery; the head is in-process).
  Small work: journal immediately.
- **The receipt speaks properly**: today `c` shows only a 3s status line; the durable receipt
  lands anchored to the node card. Keep that, but ensure inside the node view the receipt is
  visible where the user is looking (the node's thread section shows it — verify it renders) and
  the state glyph flips promptly.
- Cancelled state renders distinctly from failed: keep the muted ink family, but the header
  status word/glyph says cancelled (not the rose ✗ of failure), and a cancelled node's document
  head shows the preserved partial (B1.2) under a quiet label (e.g. `left off` instead of
  summary) plus the restart affordance in the footer.

### B3. Cancel informs the level above (the "rethink")

Today failure has 5 upward channels; cancel has zero (leaf wrapper early-returns at
`chat.go:846` before `plans.reviseAfter`; settle watcher's `EventNodeCancelled` arm only records
a craft negative, `resident.go:1813`). Design:

- **Running-leaf cancel**: in the leaf wrapper's cancel branch, before returning, invoke
  `plans.reviseAfter` with a cancellation event (phrasing like `Node X CANCELLED by user:
  <reason>` — extend `RevisionEvent`/`revise.go:152` with a cancelled flavor so the prompt says
  "the user stopped this; reconsider only the unstarted remainder — do not re-add or verify the
  cancelled work"). The sentinel already: runs on the plan model, edits only unstarted nodes,
  refuses when no pending work remains, and is bounded. A cancel-triggered revision must never
  add a node whose purpose is to redo or check the cancelled node (extend the revise prompt's
  refusal list accordingly).
- **Pending-subtree cancel** (no leaf running): the settle watcher's `EventNodeCancelled` arm
  routes the same event through the reconciler's existing planner handle (like
  `WithOverrunPlanner`) — one new wire, same sentinel, same constraints. Guard: fire once per
  command (a cascade cancelling N nodes = one revision event naming the subtree root, not N).
- **What the user sees**: nothing extra when the plan didn't change; when the sentinel edits the
  remainder, its existing one-line revision receipt is the reply. No new ceremony.
- Charter probation already handles cancelled subtrees — do not add a second demotion signal.

---

## Wave C — model attribution (the "planned by glm-5.2" confusion)

The two-rung display (short badge on the sticky line, full vendor path under BRIEF) is law and
stays. The defect: in the ordinary split-slot configuration `WorkModel` provenance is empty
(only user-pinned models are recorded) and `PlanModel` is recorded whenever it differs — so both
receipts degenerate to a bare `planned by glm-5.2` and the surface never names the model that
actually ran the leaf. Fix within the law ("three facts at most, silence on ordinary jobs"):

- At splice, when the plan slot is split, record the *effective work model* alongside PlanModel
  (a new provenance value read at the same place `splitPlanModel` decides, or read actuals from
  `store.NodeModels(id)` at render). Prefer the durable-row source (receipt law: proof is read
  from the row work runs from).
- Render: when both facts exist, `ran by <work> · planned by <plan>` (short spelling in header,
  full path under BRIEF). When the plan slot follows work (no split), silence — unchanged.
- Ordinary unpinned unsplit jobs keep no line at all.

---

## Wave D — the head can answer "what's the plan?"

The head has never read a graph edge; its entire structural vocabulary is "N running, M queued".
The shallow answer was a faithful reading of everything it was given. Fixes (all internal/head +
read-only store accessors):

1. **Board slice** (`head.go renderGraph ~1501-1612`): from `snapshot.Edges` (already fetched)
   add `| waits on <first-line labels>` to dependent rows (the TUI already derives this,
   `view.go:2496`); for running nodes add elapsed from `StartedAt`/`Impact.RunningFor` (Impact
   is already called and 4/5 fields discarded — use them). Stay within/near the 4KB budget
   (raise modestly if needed; live work first ordering already protects).
2. **Belt `plan` tool**: expose `store.PlanGraphFor(prefix)` (journaled planner DAG: Needs,
   Contract, Turns, per-node state/cost) as a read tool beside `result`; render as indented
   dependency-ordered lines with plain words. Widen `resultChildren` (`toolbelt.go:883`) to the
   subtree via `store.SubtreeNodes` with a depth marker.
3. **Belt memory**: the belt prompt today carries no conversation ("which part is slowest?" is
   answered blind). Include the same folded recent-thread slice the router gets (or a smaller
   window) in the belt prompt.
4. **Trigger holes**: add `hows`/`how's` to `selfQuestionLeads` (`control.go:406`); stop
   stripping `plan` in `redirectReference` stopwords (`revision.go:586`); consider `dag`,
   `progress`, `status` as belt-opening status cues. A status question about live multi-part
   work should reliably open the belt.
5. **License structure reads for status**: `control.go:44` ("a status question is answered from
   a board read and nothing else") must permit `plan`/`result` reads when the question asks
   about structure/progress of multi-part work. The reply contract (plain words, no graph nouns)
   stays — the model translates structure it now actually has.
6. Keep costs bounded: belt cap stays 4 tool calls; plan tool output capped (~4KB) like result.

Tests: board slice carries edges+elapsed; a "hows it going what is the plan" message opens the
belt and the plan tool returns the DAG; belt prompt carries thread; follow-up question resolves
against prior reply.

---

## Execution order

A (tui-only) → D (head-only) → B (store/resident/head/cmd/tui) → C (small, store/resident/tui).
`make check` green after each wave; commit per wave.
