# Chat UX audit — August 9, 2026

Scope: the chat surface end to end — TUI rendering (`internal/tui`), input handling, cancellation,
and the head dispatch pipeline (`internal/head`, `cmd/aforge/chat.go`). Four parallel deep-reads,
findings verified against file:line. The architecture (store-as-transport, one mouth, single head
loop) is sound; every defect below lives at a seam and can be fixed without a second engine.

---

## 1. The three reported symptoms, root-caused

### Symptom A — "it waits, then reprints older chat"

Four stacked causes, all real:

1. **The streaming reply sorts ABOVE your just-sent message, then jumps below it.**
   `renderMessages` places the live stream at synthetic order `m.lastSeq + 1`
   (`internal/tui/view.go:1476-1486`), but `landPostedMessage` deliberately does not advance
   `m.lastSeq` (`internal/tui/model.go:2250-2256`), and `Seq` is the *global* journal sequence
   (`internal/store/thread.go:383-396`) — node transitions, receipts, and commands all consume
   seqs. So your posted message lands at `Seq = S` with `S > lastSeq+1` essentially always during
   active work: the streaming block renders above your message until the next poll advances the
   watermark, then leaps below it in one frame. `postResultMsg` does not kick an immediate poll
   (`model.go:934-948`), and idle cadence is 2s (`model.go:42`) — so the misordering persists up
   to **2 seconds**. This is the "reprints older chat" effect.

2. **No optimistic echo.** `submit()` resets the input (`model.go:2203`) and the message only
   appears after the async SQLite round-trip returns `postResultMsg`. Between Enter and the
   commit, the text exists nowhere on screen. The node view *does* echo optimistically
   (`internal/tui/node.go:272-273`); the main thread doesn't. (JOURNEY.md journey 6 promises
   "appears as the user's message instantly" — violated.)

3. **The cold render path is the normal path, and it does I/O on the UI thread.**
   - Both render caches are wiped by every non-quiet poll during active work: `pollResultMsg`
     invalidates the splice cache (`model.go:829-833`), `applyPoll` bumps `threadGen`
     (`model.go:1900`) which `clear()`s the settled-block cache (`view.go:1427-1430`). Result:
     full cold transcript re-render ~2.5×/sec while anything runs.
   - Job cards are **not cached at all** (`view.go:1508`), and rendering one calls
     `linkWorkspaceReferences` (`internal/tui/markdown.go:195-227`) which does
     `resolveWorkspacePath` per whitespace token — each one a SQLite `Node()` query + parent-chain
     walk (`cmd/aforge/chat.go:5312-5337`) + `os.Stat` (`chat.go:2204-2223`), synchronously inside
     `Update`. A 300-word deliverable ≈ 300×(1+depth) SQL queries + 300 stats per render. This is
     the literal "it waits": a multi-hundred-ms stall, after which the whole viewport is replaced
     at once.
   - A once-a-minute clock repaint forces a full cold re-render even on an idle screen
     (`model.go:1871-1883`), and `threadBlockKey` embeds `relativeTime` (`view.go:1757-1759`) so
     cache keys churn at minute boundaries.

4. **Settled cards teleport content up-thread.** When a job settles, its deliverable stops
   rendering as a tail message and reappears inside a card at `BirthSeq` — mid-history
   (`view.go:1471-1475`, `cards.go:1017-1034`). With auto-scroll pinned to bottom, the whole
   visible window shifts by the card's height in one frame.

Also latent: any `Seq == 0` message gets a *negative* sort order and renders at the very top of
the transcript (`view.go:1464-1467`) — a trap for whoever adds the optimistic echo.

### Symptom B — two rapid messages should be answered as one

**There is no queue, no coalescing, no lock — and the first turn structurally cannot see your
correction.**

- Input is never disabled during a turn; a mid-stream submit posts immediately as an ordinary
  journal row (`model.go:2150-2206`).
- The head answers rows strictly one at a time in journal order — one `answer()` per message
  (`internal/head/head.go:272-282`), one guaranteed reply each (`postAgentFloor`, `head.go:412`).
- The prompt window for turn 1 hard-stops at the message being answered:
  `recentThread(..., beforeSeq)` excludes everything at or after it (`head.go:939-942`). Your
  correction is invisible to the turn it corrects **by construction**. Observed behavior: reply
  to the wrong thing, then a second reply reacting to the correction. Exactly what you described.
- `internal/head/correction.go` and `urgency.go` operate on **settled/running graph jobs**, not
  the in-flight chat turn. No turn-level supersede exists.

**The wanted semantics already exist in the repo, unwired to chat**: the worker steering mailbox
drains *all* queued messages into one turn (`internal/exec/linear.go:762-772`, mirrored in
`swe.go:1043-1060`). And `spliceContinuity` (`internal/head/revision.go:218-234`) already encodes
the reasoning: "new work typed while a job was mid-sentence is work about that job often enough
that running the two side by side is never the safer guess."

Narrowest fix point: the head's consume loop at `head.go:272-282` — before answering a user row,
look ahead in the already-fetched page for following user rows within a freshness window
(`AdjacencyMessageWindow` at `revision.go:156-196` is the existing primitive) and fold their
bodies into one turn. Cursor advancement already marks the folded rows handled.

### Symptom C — Escape to cancel, and getting typed text back

**No cancellation of an in-flight assistant turn exists at any layer**, and the key you'd reach
for is booby-trapped:

- Escape's back-out ladder (`model.go:1219-1290`) ends in: draft non-empty → **wipe draft +
  attachments unrecoverably** (`model.go:1279-1283`); draft empty on the thread → **quit the
  app** (`model.go:1287`). While a reply streams in the ordinary state, Escape quits.
- `ctrl+c` is unconditional instant `tea.Quit`, first line of `updateKey` (`model.go:1076-1078`).
  No double-press, no draft save.
- The head runs on a **process-lifetime context** (`cmd/aforge/chat.go:1291-1320,1385-1387`);
  there is no per-turn context, no handle to the in-flight provider call, no `Interrupt` on the
  `Commander` interface (`model.go:130-140`). `/cancel` targets graph nodes only, and even that
  is cooperative-at-boundary (`internal/resident/runner.go:543-557`).
- **No input history.** Up-arrow scrolls the viewport (`model.go:1483-1491`);
  `internal/tui/history.go` is job recall, not input recall. Submitted text is destroyed at send
  (`model.go:2203-2204`) and not restored even if the post fails.
- **Draft preservation is inconsistent**: help and voice preserve the draft; settings
  (`settings.go:71`), the model palette (`commands.go:116-119`), and `/history`
  (`history.go:48`) silently destroy it. `alt+,` fires even mid-draft.

---

## 2. Bonus findings — dead air and a structural provider bug

These came out of the dispatch trace and explain the rest of the "fundamentally off" feeling:

1. **The control belt burns a full LLM round-trip that can never succeed.** Streaming is attached
   to the whole head context (`chat.go:1291`), and the streaming accumulator drops tool calls —
   `MessageDelta` has only `Role` and `Content` (`ai/response.go:99-102`;
   `internal/provider/client.go:257-277`). So `manageControl` (`head.go:347`,
   `control.go:157-183`) always sees zero tool calls, spends one full board+notebook prompt, and
   falls through to the router. Every control-ish or self-question message pays this before the
   real answer starts. This also breaks tool-calling for anything else on the streamed path.
2. **Dead air with the pulse suppressed.** `awaitingReply()` yields to any live stream
   (`awaiting.go:65-67`), and `streamingMessage()` shows nothing while `streamTarget` is empty
   (`stream.go:272-278`). Both the wasted belt call and reasoning-token phases (reasoning deltas
   arrive on a field the SDK type lacks — dropped) leave the TUI in `streamReal` with an empty
   target: no pulse, no text, blank thread for the longest part of the wait.
3. **Permanent-blank-reply trap.** After a control-belt stream ends with `streamSeq == 0`, the
   model is stuck in `streamReal/providerDone/seq=0`; the real reply then lands, gets parked in
   `streamQueue` (`stream.go:145-151`), `streamedBody` returns empty for queued replies
   (`stream.go:284-289`), and `finishStream` never runs — **the reply renders as an empty line
   forever** (violates JOURNEY.md's cardinal sin clause directly).
4. **The typewriter is a ceiling, not a floor**: 2 whitespace tokens per 120ms tick ≈ 16.7
   words/s (`stream.go:53`, `model.go:33`). A 400-word reply takes ~24s to draw after the
   provider finished. THREAD-UX.md:113-130 explicitly says "stop buffering… existing unroll
   pacing as the floor."
5. **Second-turn feedback is eaten**: `awaitingSeq` is single-slot and overwritten
   (`awaiting.go:36-42`); turn 1's reply clears turn 2's pulse (`awaiting.go:48-55`). A queued
   second message shows no pending indicator at all.
6. Latency ledger: head detection 0–400ms (`head.go:227,30`) + reply pickup 0–400ms hot / 0–2s
   idle (`model.go:32,42`, tick re-arms only after poll completes) + 4–6 full-table BM25 scans
   per message before the router (`store/surgery.go:356-401`) + silent full retry on empty
   response (`head.go:688-702`) + `_txlock=immediate` on all transactions (`store.go:511`).

---

## 3. Standard chat principles — the checklist this surface is missing

1. **Optimistic echo**: the user's message appears in the transcript the instant Enter is hit;
   reconciled (not re-inserted) when the durable row lands.
2. **Append-only visual order**: once a block is on screen, nothing ever renders above it or
   reorders it. Sort by a stable key assigned at display time, not by a racing watermark.
3. **Always-visible pending state**: any in-flight turn has an indicator; N queued turns show N
   (or "thinking about your 2 messages"), never a blank thread. Thinking/reasoning phases show a
   "thinking" affordance.
4. **Escape interrupts**: first Escape cancels the in-flight turn (keeping the partial, marked
   interrupted); it never quits the app while a turn is live and never destroys a draft without
   recovery. Ctrl+C requires a second press to quit.
5. **Input is never lost**: submitted text goes into an up-arrow history ring; Escape-cancel
   returns the just-sent text to the composer; overlays never wipe the draft.
6. **Rapid messages coalesce**: messages sent while a turn is pending fold into one combined
   turn (or supersede: cancel + re-ask with both). Never answer a corrected message as if the
   correction didn't happen.
7. **The view function does no I/O**: rendering reads memory only; workspace-path resolution and
   any store reads happen async, cached per message content-hash.
8. **Caches survive the hot path**: streaming and polling invalidate only the tail block, never
   the settled history.
9. **Stream at provider speed**: pacing is a smoothing floor, not a throughput ceiling.
10. **Every message gets a visible reply**: no state-machine path may leave a landed reply
    rendering empty (JOURNEY.md's own law).

---

## 4. Fix plan (respecting the architecture)

Nothing here needs a second engine or breaks store-as-transport. Suggested waves:

**Wave 1 — transcript truth (Symptom A + eaten replies)**
- Give the live stream a stable display anchor (e.g. order = max(lastSeq, last user Seq)+ε or an
  explicit "after message X" anchor) instead of `lastSeq+1`; make `landPostedMessage` insertion
  and stream ordering share one comparator. Kill the negative-order `Seq==0` branch.
- Optimistic echo in the main thread (node view already shows the pattern), reconciled by Seq on
  land.
- Fix the `streamQueue` empty-render trap: `StreamFinished` with `streamSeq==0` must fully reset
  stream state; `streamedBody` must never return empty for a landed reply that isn't animating.
- Stop suppressing the awaiting pulse mid-stream when the stream has no visible target; add a
  thinking indicator for empty-target phases.

**Wave 2 — render cost (the "waits")**
- Cache job-card renders keyed by (card content hash, width); stop clearing `blockCache` on every
  `threadGen` bump — invalidate per-message by Seq/content instead.
- Move `resolveWorkspacePath` off the render path: resolve async once per message, cache results;
  render plain text until resolution lands.
- Drop the once-a-minute full repaint; recompute relative-time labels without cold-rendering.

**Wave 3 — interruption (Symptom C)**
- Per-turn cancellable context in the head loop (`answer` gets its own child context); new
  `Commander.Interrupt()` reaching it; partial reply posted as interrupted (durable, marked).
- Escape ladder gets a new top rung: live turn → interrupt (never quit). Quit moves behind
  ctrl+c×2 or an explicit rung when idle.
- Input history ring + draft restore on cancel/failed post; overlays stash and restore the draft
  (settings/palette//history bugs).

**Wave 4 — coalescing (Symptom B)**
- Fold look-ahead in the head consume loop (`head.go:272-282`) using the adjacency-window
  primitive; optionally interrupt-and-refold when a message arrives mid-turn (compose with Wave
  3's per-turn context). Steering-mailbox semantics (`exec/linear.go:762`) are the template.
- Per-turn awaiting (multi-slot), so queued turns are visibly pending.

**Wave 5 — pipeline hygiene**
- Fix streamed tool-call accumulation in the provider (or run the control belt unstreamed);
  until then the belt is a pure latency tax and tool-calling is dead on the streamed path.
- Typewriter becomes a floor (catch-up when behind provider); poke the TUI poll immediately on
  `postResultMsg`; cache-key without relative time.
