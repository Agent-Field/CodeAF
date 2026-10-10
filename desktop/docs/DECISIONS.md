# Desktop research decisions

## 2026-10-10 — t-d5-be-pg-memory-decision

Coverage: BE-PL-47. Sources: Places §6e (P-6e-14), P-BE-11,
Iteration 2 I2.13–I2.14, and Decisions 12a/12e.

**Decision for this lane:** keep automatic chat-decision promotion into places
disabled until its destination and removal semantics are designed. Add no
engine writer, route, renderer data, memory section, or placeholder for this
unresolved feature. Existing real knowledge lines and their approved editing
and context behavior remain available. This is a research decision, not a
claim that place knowledge is absent.

### Evidence and current behavior

Places §6e says decisions from a place's chats are promoted there, can be
removed, and memory from several places adds up. It does not choose a write
destination for a chat with multiple memberships. Context depends on membership,
never on the visible tab strip; ancestors contribute up to two levels.

Iteration 2 supersedes a separate memory list: I2.13 and Decisions 12e merge
instructions, notes, memory and learned rules into “What [place] knows.” Lines
carry provenance; “remember…” adds a line with Undo. Contradictions keep the
newer line and strike the older for seven days, with a question for two personal
statements on the same day. These rules do not define multi-place promotion or
whether removing membership retracts earlier saved lines.

The current code already contains relevant primitives:

1. `internal/session/memory_to_place.go` exports `MemoryToPlace` for one caller-
   supplied place. Repository Go call-site search finds its declaration only;
   it is not a wired post-turn fan-out. `internal/placegraph/knows_promote.go`
   settles against that place's real lines and returns provenance and Undo.
2. `internal/placegraph/knows_store.go` supports line deletion with a graph receipt.
   The existing knowledge tests cover deletion and Undo. A saved line belongs
   to a place and retains its source chat; membership is a separate record.
3. `internal/placegraph/resolve.go:resolveInstructions` already includes live,
   unreplaced knowledge alongside instructions. It charges their text to
   `ContextInstructionBudget` (currently 12 KiB across the chat's places),
   marks trimmed entries, and uses a separate `ContextSourceBudget` (12 source
   references). The proposal's older byte limits for attached files are not
   this resolver's budget. No extra unmetered memory block is needed.

The existing Open rows P-14 and KP-1 are assumptions, not designer approval.
P-14's “first parent-most place” concerns an explicit “Remember…” request but
does not define ties or justify automatic writes to every membership. KP-1
explicitly leaves destination selection to the caller. This task is therefore
not ALREADY-DONE despite the landed knowledge primitives.

### Recommendation for the designer

Prefer **one explicitly named direct member place** per promotion. Do not copy
to every member or inherited ancestor, and do not infer the destination from
the active strip or an undefined “first” order. A sole direct membership can
be proposed as the target; several memberships need the target named before
any write. Unplaced chats keep their existing chat memory behavior and create
no place line. This limits accidental spread of chat-specific decisions.

Save into the unified knowledge list with the source chat, date and the existing
receipt-backed Undo. Removal deletes the named place's line through its normal
knowledge controls; it does not erase the source conversation, other places'
independent lines or session memory. Removing a chat from a place stops that
place's context reaching it from the next turn, subject to any remaining
inherited membership; it does not retract knowledge already saved for other
chats. Preventing later re-extraction of a deliberately removed fact needs a
designer-approved suppression rule; do not silently re-promote it.

Saved knowledge should join the existing membership-based context union, with
provenance and trimming disclosed by Using, and consume the existing instruction
byte budget. Future saves must not bypass this budget or add a second memory
quota. These destination, removal and re-promotion choices are recommendations;
they do not authorize building automatic promotion in this lane.

### Verification

- `npx tsc --noEmit -p .` and `npm run design:check` passed in `desktop/`.
- Focused `internal/placegraph` tests passed for promotion, knowledge deletion
  and Undo, context union, instruction trimming, strip independence and
  unplaced chats (`make test-focus`, fresh run).
- Focused `internal/desktopbridge` tests passed:
  `TestKnowsCRUDAndDeleteUndo` and
  `TestKnowsUsingReadsLiveLinesWithPlaceSourcesAndPolicy`.
- A document assertion passed for one identical BE-PL-47 row in the Open table
  and the integrator handoff, the dated task heading and all decision topics.
  `git diff --check` passed.
- No product, Go, manual, token or UI files changed. No new behavior requires
  manual probes or browser geometry/interaction tests; Playwright, Go build/vet
  and Go law tests were not run for this documentation-only decision.

### Ready-to-paste integrator row

The same row is recorded under **Open: for the designer** in
`DESIGN-QUESTIONS.md` as required by the research brief.

| # | Question | Assumption the app ships now |
|---|---|---|
| BE-PL-47 | Places §6e P-6e-14 / P-BE-11, with I2.13: does a chat decision save into every direct member place or just one (how is “first” chosen)? Does removing the saved line or chat membership retract it elsewhere or prevent re-promotion, and does saved knowledge consume the context budget? | Automatic chat-decision promotion is not wired; no UI data or placeholder is invented. Existing real knowledge stays usable. Recommend one explicitly named direct member, never fan-out or an active-strip default; save into What [place] knows with source chat/date and Undo. Delete only that place's line; membership removal does not retract saved knowledge, and re-promotion suppression still needs design. Live knowledge already joins membership/ancestor context and consumes ContextInstructionBudget, with trimming reported by Using; no additional memory store or budget. Clarifies Open P-14/KP-1 without treating them as designer approval. |
