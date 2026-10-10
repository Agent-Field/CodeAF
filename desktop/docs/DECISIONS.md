# Decisions

Dated decisions taken without waiting for the designer. Each section is titled with its plandb task id.

## t-d5-be-key-set-decision: entering or replacing the provider key in Settings (2026-10-10)

**Decision: v1 does not set or replace the provider key from the app.** The engine builds no write route and the UI draws no key field. Covers BE-SET-07 (Shell IX S-IX-12).

Why:

- The design's Settings "engine connection" row only states where the key comes from. It draws no input, and the emptiness law forbids inventing one.
- `config.writeProfileValues` (`internal/config/budget.go`) can write `api_key`, but the key ladder in `apiKeyResolution` (`internal/config/apikey.go`) puts the `OPENROUTER_API_KEY` variable first. A key typed into the app would be silently ignored on any machine where the variable is set, so the field could say "set" while the engine uses another key.
- A write path puts a secret through the renderer, the bridge and the mock engine. The brief forbids secrets in renderer storage, logs and URLs, and the first-run setup in the terminal already owns key entry.

What ships instead (BE-SET-04, `t-d5-be-key-status`): a read-only row from `APIKeySourceAt`, giving `{source, present}` and never the value. It reads "OpenRouter variable", "Profile", "OpenAI variable", or draws nothing when no key is found.

Recommendation if the designer later wants entry:

1. Write only to the profile `api_key` row through `writeProfileValues`, never to the environment.
2. Add a write-only field. After saving it clears and shows a "Key set" mask with a "Replace" action. The value is never read back, shown, logged, or kept in renderer state beyond the submit.
3. If `source` is not "Profile", the field says that a variable overrides the profile key, so a saved key cannot look active when it is not.
4. Offer "Remove key" through `removeProfileKey`.

## t-d5-be-pg-suggest-decision: cluster, filing-offer and "Move them" suggestions (2026-10-10)

**Decision: v1 builds no engine path and draws no UI for any of the three suggestions until the designer answers.** Covers BE-PL-46 (Places §6e P-6e-16, P-6e-17, §8c P-8c-10, P-8c-11, §6d P-6d-2c). The design fixes the numbers (5 or more chats cluster; at most one line after the first reply; only existing places are offered) but names no model role, no similarity signal, and no surface for the filing offer.

What exists already: `config.desktoproles.go` registers the roles `placefiling` (`roles.RolePlaceFile`: picks an existing place, never creates one) and `placesuggest` (`roles.RolePlaceSuggest`: names a group and offers it as a new place, for approval). Those are Settings rows only; nothing calls them.

Recommendation, when the designer answers:

1. **Role.** Cluster suggestion and "Move them" use `RolePlaceSuggest`; the filing offer uses `RolePlaceFile`. Neither role runs on the chat's conversation model, and neither runs without a configured cheap role model.
2. **Cluster threshold.** The signal is shared workspace first: 5 or more unplaced chats whose recorded working folder is the same repository root. Shared words are a fallback only for chats with no workspace, and a model-named group of fewer than 5 is dropped. The model names the group; it never decides membership by itself. A dismissed set is not offered again until it grows by 3 chats.
3. **Filing offer.** A transcript line after the first reply, not a header affordance. A header affordance would be a persistent control for a one-time offer and would add colour or chrome the design forbids. The line is dim, one sentence, with "Move" and "Not now", names an existing place only, and stays in the transcript as a record. Never offered in a chat already in a place.
4. **"Move them" on the unplaced root.** One quiet line at the root ("Not in any place"), showing the cluster's count and place name, with "Move them" as a single action. It moves members through the existing membership write and is undoable; it never creates a place without the person approving the name.
5. **Silence.** With no role model configured, fewer than 5 chats, or an unavailable engine, nothing is drawn (emptiness law). No placeholder, no count of zero.

Until answered the app ships none of this; the Settings rows stay informational.
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

## t-d5-be-terminal-persist-decision: terminals and job logs across a bridge restart (2026-10-10)

**Decision: v1 adds no disk persistence or restoration for bridge-owned terminals or job logs.** Existing in-memory retention stays; the engine builds no recovery path and the UI draws no recovered output, status or log action. Covers BE-TERM-08. The BE-TERM-08 row in DESIGN-QUESTIONS.md is ready for the integrator to paste into **Open: for the designer**.

Evidence and scope:

- Shell §3c draws a terminal/job output field, a live header and “Ask codeaf about this output”. Components says “A finished job keeps its log until you Remove it”; Q4 confirms Close keeps the log and list entry. Neither specifies restart recovery. Q6 fixes 16 live terminals per conversation and 512KB scrollback each, not disk retention. Interactions and I2.1–I2.14 add no restart retention rule.
- `internal/desktopbridge/terminal.go` owns a `terminalSet` on each bridge conversation and stores output in `terminal.buf`, capped by `scrollbackBytes`. There is no disk writer or loader for these records; `closeAll` ends the processes when the bridge goes away. `terminal_routes.go` keeps a closed job but removes a closed interactive terminal; Remove drops the record and its buffer. A renderer reload can replay a retained terminal by ID while the same bridge lives. A bridge restart cannot.
- `desktop/src/features/terminal/useTerminalFeed.ts` reattaches by stored session/terminal identity and handles a missing record as “This terminal is gone.” `TerminalPane.tsx` supplies no Open log handler without a real log file. That truthful missing-record behavior remains; “draws nothing” applies to invented recovered data, not to hiding the existing missing-terminal explanation.
- Canonical background jobs are a separate data path: `internal/session/jobs.go` already spools bounded output via `droppingsDir` in `landing.go`, normally under the session's `logs/jobs/`. `sweep.go` defines the seven-day TTL and delegates job-log cleanup to ownership-aware retention. Existing canonical logs are not removed or made memory-only by this decision, and a file on disk alone does not establish a restored desktop job record.

**Recommendation for designer review: finished desktop job logs should survive restart with bounded retention under the session's `logs/`.** Closing a view should not make a recorded failure disappear; the session already owns temporary logs and their cleanup. However, silently expiring a log would contradict “until you Remove it”, so the seven-day limit needs an explicit designer answer before implementation. Memory-only is the conservative shipping assumption, not a claim that seven-day persistence already works.

If approved, the follow-through is:

1. Persist only real finished-job output and enough recorded metadata to identify and display it (stable job/session identity, title, command, working directory, recorded exit status and start/end times). Bound output; do not equate the 512KB scrollback limit with an approved full-log disk budget.
2. Store logs beside their canonical session, never in the borrowed project or renderer storage. Reuse the session's existing retention rules and seven-day sweep based on file modification time; opening a log must not refresh that age. Use owned paths and the existing protections against symlinks and active-writer cleanup.
3. Close detaches and retains; Remove explicitly deletes the retained job and log. Offer Open log only when the engine reports an available file. Expired or missing files provide no recovered output or invented exit status; the designer must specify how an already-saved tab explains expiry.
4. Restore a finished job as read-only recorded output. Do not restore interactive PTYs, automatically rerun commands, or turn a job interrupted by shutdown into a fabricated successful completion. Run again remains an explicit new job.

No code or user-visible feature changes are needed for this research task. No manual page changes are needed: this decision records the current boundary and a proposed future capability, not a capability the app offers.
Calls made from the design and the code when a lane was asked to decide
rather than wait. The assumption the app ships is also a row in
`DESIGN-QUESTIONS.md`.

## t-d5-be-pg-merge-decision (2026-10-10)

**Question.** Places §6d (the card coverage calls P-6d-10) says “Delete or
merge a place. From the Home menu.” The sentences under that card describe
delete: chats keep their other places or become unplaced, nothing is lost,
history keeps everything, and children move up to the deleted place’s
parents. The 8f place menu drawing (coverage P-X-13) lists Go to, Quick Look,
Open in new window, Rename, Tint, Add to another place…, Pin to rail,
Archive, Delete place…. It does not draw Merge. The 60-day card says a quiet
suggestion to merge or archive, on Home and in ⌘P, and does not say which
place a merge lands in. 9d says a place inside two families keeps the tint of
the parent it was created in, and never blends colours. Nothing in the design
says how instructions or policy combine.

**Call.** Merge ships. The place the menu was opened on is absorbed. The
person picks the survivor. Nothing is deleted, and colours are not blended.

### Target

“Merge into…” opens the existing Go to sheet titled `Merge “Name” into…`.
The absorbed place, every place under it, and archived places are not
offered, because a place cannot sit inside itself. There is no suggested
target and no second confirmation. Picking a row merges at once. The toast
reads `Merged “Name” into “Survivor”` and offers Undo. Undo restores both
places, because the receipt puts the graph back to the snapshot from before
the write. Merging a place into itself is refused. A missing target is
refused with “Say which place to merge into.” An archived target is refused.

The 60-day suggestion’s Merge button opens that same chooser. It does not
pick a place. Archive and Not now on that line stay as they are.

### What combines

- **Instructions.** The survivor’s text stays first. The absorbed place’s
  instructions are appended after a blank line when they are non-empty and
  different. The same text is not repeated.
- **Sources.** United by kind and ref. The survivor’s copy of a duplicate
  stays.
- **Policy.** The survivor wins each field. A blank model or permissions
  field is filled from the absorbed place. The reserved manager blob follows
  the same empty-fill rule. No question is asked, and the two values are not
  mixed.
- **Tint.** The survivor keeps its own tint. A child that moves keeps the
  colour it was showing, pinned when inheriting from the survivor would
  change it. 9d’s “never blend” rule is the reason.
- **Chats.** Memberships move onto the survivor. A chat already in both keeps
  the survivor’s existing row. Other places that chat belongs to stay. No
  chat is deleted and none becomes unplaced only because this place went
  away. The delete sentences on the same 6d card stay the delete path.
- **Children.** They become children of the survivor. A child that already
  sits above the survivor takes the absorbed place’s parents instead, so the
  graph does not loop. Parents of the absorbed place are added to the
  survivor unless that edge would loop; those are reported as skipped and
  not applied.
- **Knows lines.** They move to the survivor. Their text is not rewritten
  and duplicates are not folded by this write.
- **Rail pin.** If the absorbed place was pinned and the survivor was not,
  the pin moves to the survivor. If the survivor was already pinned, it
  stays pinned.
- **Tabs.** The survivor keeps the tab set it already had. The absorbed
  place’s saved tabs are not copied onto it. They remain stored under the
  absorbed place and return with Undo.

### Where the item sits

One menu builder draws it, so it appears everywhere that builder is used
and the merge handler is wired:

1. The place right-click menu (8f), after “Add to another place…” and before
   Pin to rail.
2. The Home page’s ⋯ menu, in that same place. Archive and Delete stay on
   this menu.
3. The Home tab’s place menu, in that same place, then Close place. That
   tab menu does not carry Archive or Delete (PS9).

Offline, the write is absent, so the item is absent. Now and All places
have no place menu.

### Window

The window that ran the merge goes to the survivor. Another window that
still has the absorbed place selected, once its graph read shows the place
is gone, shows Now and says “That place no longer exists, so this window
shows Now.”

### What this does not invent

No blended tint, no model call to reconcile instructions, no default
survivor, no preview counts, and no confirm the design does not draw.
Delete’s confirmation stays on delete only.
# Desktop decisions

Append one dated section per task. Leave every other section as it is.

## t-d5-be-files-write-decision (2026-10-10)

### Question

Should the desktop file tab be able to write, rename, or delete a file? (BE-FILE-07, code-bridge §6.5.)

### Call

No. The app ships no file-mutation route and no control for one.

### Why

Shell 3e is a viewer: changes first, a toggle to the full file, and a handoff to your editor. The drawn controls are Changes, File, and Open in editor. Iteration 2 (I2.1–I2.14) does not add an editor. Rename in Shell 3g is the tab and group menu, not a filesystem rename.

The bridge file doors are reads: file bytes, text, stat, list, find, locate, changes, and diff. Open in editor starts a program the engine just listed. It does not change the file. There is no write, rename, delete, or move route. The file header offers the view toggle and that handoff. The lines are text, not a field.

### What still changes files

A conversation can still create, overwrite, edit, rename, move, and delete files through its own tools when you ask. That is not a control on the file tab, and this decision does not remove it. Sending a picture or a file with a message attaches it to that message. It does not write a workspace path from the tab. Writing into a terminal is the terminal, not the file tab.

### What ships

Nothing new is built. A request to write, rename, delete, or move a file through the desktop bridge is an unknown action. The tab does not draw a save, a rename, or a delete, and it does not invent a dirty or saved state. The assumption is also row BE-FILE-07 in the Open table of DESIGN-QUESTIONS.md, so a later design that draws an editor can replace it.
