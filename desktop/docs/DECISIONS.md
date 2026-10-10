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


## 2026-10-10 — t-d5-pl-decisions: places questions and assumptions

**Decision:** use the 13 PLD rows in the Open table of DESIGN-QUESTIONS.md as
this lane's integration contract. This task records decisions and review
findings; it changes no runtime behavior and makes no new manual capability
claim. Other places lanes retain ownership of implementation files.

Sources: design v3 Places 6c–6e, 8a–8g, 9a/9c/9e and 10a; Interactions Places,
Multiple windows and Shortcuts; Iteration 2 I2.13–I2.14. The requested
`cov-places.md` was not present in the available worktree/design snapshot.
Its open-question list is reproduced in DESIGN-COVERAGE.md, Places “Open
questions (each with the conservative assumption the tasks use)”; that is
the coverage source used here. Covers PL-007, PL-070, PL-086, PL-124, PL-133,
PL-174, PL-221, PL-230 and PL-235, plus the named shortcut, peek, delete,
counts and window-scope questions.

### Chosen behavior

| Area | Decision and ledger rows |
|---|---|
| Navigation | Go to is ⌘P, All places is ⌘⇧P; ⌘N creates a place only inside Go to and opens a window on Now elsewhere (PLD-01/02). |
| Numbering and peek | Keep Pinned-then-Open numbering from the drawn 9c switcher, Now at 0, Linux Alt+digits; this remains an assumption where 10a/Interactions say pinned only. Left-edge dwell reveals the rail; top edge reveals the Focus-mode strip (PLD-03/04). |
| Structural actions | Prefer the non-destructive 8g drop rule over the conflicting Interactions place row: plain adds, Option moves the source edge only. Merge follows BE-PL-44, superseding the earlier “omitted in d5” coverage assumption. Place delete never deletes conversations (PLD-05/06/07). |
| Engine content | Recap, cluster, filing offer and knows lines render real engine data only. No renderer AI call, guessed destination, fabricated recap or automatic transcript extraction. Iteration 2 unifies knowledge; a new-place cluster requires at least five chats (PLD-08/09/10/11). |
| Counts and persistence | Child-place numbers are direct; chat totals include descendants and deduplicate. Pinned is global; Open visits/closes are window-local. Shared tab content and local focus remain distinct (PLD-12/13). |

### Current-code review and follow-through

`desktop/src/design/keyboard.ts`, `places/shell/GoToChooser.tsx`,
`places/shell/useShellPlaces.ts` and `shell/useShellFrame.ts` already implement
the selected shortcut, numbering and peek split. `places/dnd/placeDnd.ts`
already implements additive plain drops, source-edge-only Option moves and
cycle refusal. The merge menu and chooser already follow the newer merge
research decision; retain them rather than reverting to the stale coverage.

The older BE-PL-46 and memory research sections are historical findings,
not a current inventory: `internal/placegraph/recommend*.go` and
`internal/desktopbridge/places_advice.go` now provide recommendation paths;
`internal/placegraph/digest.go` builds recaps from saved evidence;
`internal/session/remember_place.go` implements an explicit save into a
place. None authorizes the desktop to invent missing data or establishes that
automatic chat-decision promotion is wired. Do not interpret PLD-08–11 as
instructions to remove existing engine functionality.

Two integration gaps remain, recorded instead of silently changing another
lane's files. `places/shell/selectors.ts` reads `counts.descendants` for both
`placeMeta` and child tile metadata, while `counts.children` already exists;
the display owner should use it for “inside” and “places”, preserving
`chatsInclusive` for chat totals. `internal/placegraph/rail.go` stores Open
in the global graph; the shell filters local closes but still starts from
that global visit list. The persistence owner must isolate each window's
Open visits/close/idle state without forking shared place tab contents.

### Verification boundaries

The React-wired v-Places design was opened through local Playwright in
Chromium and WebKit. Both measured its first section at 2624 × 2658.84375px
with the specified system font stack using getBoundingClientRect and
getComputedStyle. These measurements establish the design was read from a
rendered page; they do not claim runtime pixel parity for this documentation
change. Acceptance is all named questions having explicit assumptions,
including the superseded merge omission and the two implementation gaps.

Validation passed: TypeScript (`npx tsc --noEmit -p .`), design policy
(`npm run design:check`), 62 focused Node tests (keyboard, shell selectors,
drop/filing model and palette), and eight Playwright cases across Chromium
and WebKit (Go to, rail slots/close/pin, merge chooser and place-delete Undo).
The browser run used port 1765, reuseExistingServer false and the lane's
external output directory; its temporary config was removed. Vite's runner
config loader avoided the read-only shared node_modules bundle cache.
A document assertion checked 13 unique Open rows with non-empty questions
and assumptions, the dated task section, all nine acceptance IDs and both
integration gaps. `git diff --check` passed. Go build/vet/laws and manual
retrieval probes were not run because no Go or user-visible feature changed.

## t-d5-qa-nat-web: native web tab verification on macOS and Linux (2026-10-10)

Decision: the checklist below is the record for the PR body. Lines marked **auto** are proven by `cargo test` (109 pass on Linux aarch64) or the existing Playwright/unit suites. Lines marked **manual** need a person at `npm run desktop:dev` on each OS with a real webview. This lane ran headless on Linux with no display and no Mac, so those lines are NOT recorded as passing. Nobody has run them on either OS yet.

Finding fixed: `584cf68e3` removed the scoped `http:default` grant (loopback `/api/engine/*`) from `capabilities/default.json` while `engineFetch.ts` still calls `plugin:http`. In a packaged window every engine request would have been refused. `web::tests::native_http_accepts_only_engine_routes_on_loopback` was red on `d3-int` because of it. The grant is restored for `main` and `w-*` only; `web-*` still has no capability, and the test still proves a page cannot reach it.

| # | Line | Linux | macOS | Evidence |
| --- | --- | --- | --- | --- |
| 1 | Page renders at the pane rect through split 1/2/4 | manual | manual | `policy::rectangles_stay_inside_the_window` (auto, clamping only) |
| 2 | Rail collapse and window resize move the page | manual | manual | none automated |
| 3 | Back, forward, reload | manual | manual | none automated |
| 4 | Title, URL, loading, favicon events | manual | manual | none automated |
| 5 | `target=_blank` becomes a web tab | manual | manual | `on_new_window` in `web.rs`; `about:blank` handled |
| 6 | Menus, palette, Quick Look, overview, hover previews never covered | manual | manual | `weboverlay::tests` (hide-all/show-all idempotent, closed pane not re-shown) |
| 7 | `window.__TAURI_INTERNALS__` undefined in the page | manual | manual | `web::tests::a_web_page_reaches_no_command_and_no_plugin` (IPC refused for `web-*`) |
| 8 | fetch to the bridge refused from the page | manual | manual | `web::tests::native_http_accepts_only_engine_routes_on_loopback` (auto, now green) |
| 9 | ⌘T / ⌘W with the page focused | manual | manual | `policy::app_chords_are_new_close_and_address_on_the_primary_modifier_only`; menu accelerators `menu.rs`, GTK accel group `platform.rs` |

Gap, stated plainly: acceptance ("every line passes on both OSes") is not met until lines 1-6 and 9 are walked by hand on both machines and 7-8 are confirmed from the page's devtools. Lines 2-4 have no automated check at all.

## t-d5-settings-audit-role: model role controls and overlapping saves (2026-10-10)

Decision: audited `SettingsPage` role rows, `ModelSelect`, `useModelSettings` and `saveQueue` from source. Read-only against the engine; no live profile was written. Browser fixture screenshots were not taken (headless lane); the claims below rest on source and unit tests.

Finding fixed (real): an effort click sent `{ model: role.model, effort }`, where `role.model` is the last SAVED model. Choosing model B then clicking an effort before B's save returned queued a second save carrying the old model A, and the engine ended on A with B's choice silently lost (B's own answer is correctly not applied because it is superseded). Now an effort-only change resolves its model from the latest queued choice (`roleChoice.ts`, `resolveRoleChoice`), and a model change still clears the effort because the new model may not accept it. Covered by `roleChoice.test.ts`.

Checked and sound: same-key saves run in intent order (`saveQueue`); a duplicate Enter/blur shares one save; a failed save neither blocks a retry nor drops a newer intent; only the newest version applies its answer or its failure text; pinned slots re-read the saved list when they run, so overlapping pin edits compose; the receipt shows the latest failure, else "Saving…", else the saved line.

Remaining gap, not fixed: the receipt is one shared line, so a failure on one control has no marker on its own row. Left for the coordinator (see DESIGN-QUESTIONS, SETTINGS-ROLE-282).

## t-d5-sh-focus-native-lights: traffic lights in Focus mode and peek (2026-10-10)

Decision: option (b). Tauri 2.12.1 and tao 0.37.1 have no runtime API to hide the macOS traffic lights (`titlebar_buttons_hidden` is creation-time only, `set_closable` only disables). A small native command `window_set_traffic_lights(window, visible: bool)` calls `standardWindowButton(...).setHidden` on the main thread; the renderer hides the lights with the strip in Focus mode and shows them with the top-8px peek. This reverses the earlier SH-013 assumption (lights always visible), because the design puts the lights inside the strip. Full reasoning, signature and risks: `docs/research/d5-focus-traffic-lights.md`. No code changed in this lane; the native writer owns a new d5-nat task. Not verified on a Mac (headless Linux lane, no network, so no newer plugin was ruled out).
## t-d5-settings-audit-using (2026-10-10)

Decision: **review complete; product acceptance has gaps**. This lane is a
read-only source and browser-fixture audit; the coordinator owns fixes. No
live model, profile, place or conversation was changed. The committed browser
spec records measurements without asserting that known design gaps must remain.
The external `settings-using-audit.md` report and screenshots carry the evidence.

Design checked: Places 6e/6f, Components Places, Interactions “Using chip”
(I-ICV-31), and Iteration 2 I2.13. Both Chromium and WebKit rendered the local
`v-Places.html` reference; dimensions and colors below came from
`getBoundingClientRect` and `getComputedStyle`, not visual estimates.

| Finding | Evidence and coordinator follow-through |
| --- | --- |
| USING-283-1: successful working folder is invisible | `ConversationView.tsx:198-199,265` reads `workingFolder` but renders only its note or skipped-folder explanation. A fixture returning `/work/fixture-project`, `from: place`, and no skips displays neither path nor label in either browser/theme. Fallback notes do display. Expose the engine's actual workspace as read-only context, subject to SETTINGS-USING-283-1. Reopening must continue to use the saved workspace, not the place's current folder. |
| USING-283-2: source refusal and failure text are colored | `using.css:65,75,81` colors refusal reasons red, selected choices accent, and alert text red with a danger fill. The design reserves state color for 6px glyphs. Keep reasons/state text in ink, selection as a soft fill, and any state color on the permitted mark. The failed chip also colors its 12px icon (`using.css:7`), exceeding the state-glyph rule. The sheet's keyboard focus also lacks the required 4px halo (`using.css:34`). |
| USING-283-3: sheet is narrower than the rendered reference | At a wide viewport, the reference measures **392px** outer width (`width:380px` plus two 6px paddings, content-box); the app measures **380px** outer width (border-box). Both have 12px radius and 6px padding. The chip matches the reference's 24px height, 7px radius and 12px type. Resolve the 12px sheet discrepancy against the rendered design, through tokens. |
| USING-283-4: chat-only drop is absent | Interactions explicitly permits dropping files on the Using popover for this chat only. `UsingSheet` and `UsingLine` have no drop handler or source-add seam. Do not route a drop to place-wide sources; coordinator must connect the chat-local engine door. |
| USING-283-5: existing Engine Retry test is red | `settings-engine.spec.ts:61` expects ink-3; both browsers receive ink-2 while Retry is hovered after reload. The shared ghost button changes foreground on hover. Review the hover-only-fill law and fix the control/test contract; this audit does not change shared controls. |

Sound source behavior: `internal/desktopbridge/using.go` uses
`session.PlaceGraphUsing` with the engine's source policy and
`session.DecidePlaceSettings` for settings. Credential paths and symlinks into
them are refused by `internal/placegraph/sources.go`; the source policy stats
paths without reading their contents. Using shows provenance (including AI
additions), missing/refused reasons and budget omissions; refused and missing
sources offer no Open action. Pending permissions do not claim to be in use.
Escape returns focus to the chip. Unknown/empty context draws no chip.
Settings currently offers no source-policy editor or working-folder selector;
its Engine section names connection location, not a conversation's workspace.

Validation: TypeScript and design policy passed; 22 focused Node tests passed;
focused Go tests passed in `internal/placegraph` and `internal/desktopbridge`.
The audit spec passes eight cases in Chromium/WebKit, Light/Dark, including
Using at 320, 800 and 1200px. The additional existing Engine test fails once in
each browser at the same Retry color assertion (USING-283-5). Go sources and
product behavior were unchanged, so no build/vet/laws or manual update was
needed. Ledger rows added: SETTINGS-USING-283-1 and SETTINGS-USING-283-2.

## t-d5-sh-overview-pinch (2026-10-10)

Decision: **no additional pinch wiring**. Shell 2h and Interactions require pinch-out, but renderer Ctrl+wheel cannot distinguish a physical pinch from ordinary zoom, and native delivery/cancellation has not been observed on either supported platform. The existing tab-strip-only `useOverviewGesture` is already wired; this research does not remove it or broaden it. Corrected stale OV2, which said no wiring existed.

[Research and minimal native probe](research/d5-overview-pinch.md) distinguish macOS GestureEvent/Ctrl-wheel source evidence from GTK native magnification and from synthetic browser tests. Keep the overview button and platform keys as dependable doors. SH-190 native acceptance stays open until physical traces are recorded on macOS WKWebView and Linux WebKitGTK. No renderer, engine, tokens or manual behavior changed.

## t-d5-tab-web-find-research: find in a native web page (2026-10-10)

**Decision: find is feasible on macOS and Linux without giving the `web-*` view IPC. Build it as `t-d5-nat-web-find`.** The command is `web_find { pane, query, forward } -> { found, matches? }`. `pane` is the id the other web commands already take (the task's `label`); the view label stays `web-<pane>` inside Rust. `found` is always a boolean. `matches` is present only when Linux reports a total under its cap. There is no `current` index, so the field never shows "n of m".

Find stays absent in the app until that follow-up lands. This lane adds no command, menu item, or field. Covers TW-14. The design files do not draw find in a web tab; Open row TW-14 records what ships.

### Why the synchronous `{matches, current}` shape does not fit

Both platform calls are asynchronous, and neither public API reports the ordinal of the selected match.

**macOS.** Public `WKWebView` method, WebKit `WKWebView.h` (main, 2026-10-10):

```objc
- (void)findString:(NSString *)string
 withConfiguration:(nullable WKFindConfiguration *)configuration
 completionHandler:(void (^)(WKFindResult *result))completionHandler
    WK_API_AVAILABLE(macos(11.0), ios(14.0));
```

The selector is `findString:withConfiguration:completionHandler:`. The header says a match is selected and the page scrolls to it. `WKFindConfiguration` (class available macOS 10.15.4) has `backwards` (default NO), `caseSensitive` (default NO), and `wraps` (default YES). `WKFindResult` has one property, `matchFound`. The same headers are what `objc2-web-kit` 0.3.2 translated: `findString_withConfiguration_completionHandler` is compiled only with features `WKFindConfiguration`, `WKFindResult`, and `block2`, and `WKFindResult::matchFound` is its only method. This crate's `Cargo.toml` enables `WKWebView` and `block2` and does not yet enable the two find features. Tauri 2.12.1 does not wrap find; `Webview::with_webview` already hands the process the `WKWebView` (`PlatformWebview::inner`), which `web/platform.rs` uses for history and snapshots.

**Linux.** Installed WebKitGTK 2.52.6 (`/usr/include/webkitgtk-4.1/webkit/WebKitFindController.h`) and the already-linked `webkit2gtk` 2.0.2 crate. `WebViewExt::find_controller` is `webkit_web_view_get_find_controller`. `FindControllerExt` exposes `search`, `search_next`, `search_previous`, `search_finish`, and `count_matches`. The GIR (`WebKit2-4.1.gir`) says the operations are asynchronous: `found-text` and `failed-to-find-text` after a search, `counted-matches` after `count_matches`. `found-text`'s `match_count` is the number of matches, and a total above `max_match_count` is reported as `G_MAXUINT`. `search_finish` unhighlights. `PlatformWebview::inner` on Linux is a `webkit2gtk::WebView`, already used from `with_webview` in `web/platform.rs`.

**Private macOS SPI stays unused.** `WKWebViewPrivate.h` declares `_findString:options:maxCount:`, `_countStringMatches:options:maxCount:`, `_hideFindUI`, and `_findDelegate`. Those can count and clear. They are absent from the public header and from `objc2-web-kit` 0.3.2. `macOSPrivateApi` in `tauri.conf.json` is the title-bar switch, not a grant to call WebKit SPI. The follow-up calls the public method only.

### Command the follow-up implements

`web_find` is a Tauri command on the app webview, the same trust path as `web_history` and `web_snapshot` (`owned_view`, then `with_webview`). `capabilities/default.json` keeps `web-*` out of the capability set. The page gets no IPC permission and no script evaluation.

The command awaits one platform result on a channel, as `web_snapshot` already does, and returns:

| Field | When it is present |
|---|---|
| `found` | Always. macOS: `WKFindResult.matchFound`. Linux: true on `found-text`, false on `failed-to-find-text`. |
| `matches` | Linux `found-text` `match_count` when that value is not `G_MAXUINT`. Omitted on macOS, on a failed search, on dismiss, and when the count hit the cap. |

`current` is not a field. Counting Next presses in the app would lie once the search wraps.

Call rules, so the two platforms agree:

- A new or changed `query` starts a search. Linux: `search` with `WEBKIT_FIND_OPTIONS_CASE_INSENSITIVE | WEBKIT_FIND_OPTIONS_WRAP_AROUND`, plus `WEBKIT_FIND_OPTIONS_BACKWARDS` when `forward` is false. macOS: `findString` with `caseSensitive = false`, `wraps = true`, `backwards = !forward`. Linux `search` starts at the beginning of the document; macOS starts at the current selection. On a fresh page those are the same place.
- The same `query` again steps. Linux: `search_next` or `search_previous`. macOS: `findString` again; `backwards` selects the direction from the current selection.
- An empty `query` dismisses and returns `{ found: false }` with `matches` omitted. Linux calls `search_finish`. macOS calls `findString` with an empty string, because the public header has no hide method. The follow-up confirms on a Mac that the selection clears. If it does not, Esc still closes the field and the page selection stays until the next find or navigation. That gap does not justify `_hideFindUI`.
- One named match cap lives beside the command. `G_MAXUINT` means the total is unknown, so `matches` is omitted. The renderer never draws 0 for a missing count.

The find highlight is WebKit's own selection inside the page. It is page content. The app's find field, when the renderer lane draws it, uses the shared search field: no coloured count, no "n of m".

### Keys while the page is focused

The page holds keystrokes. Today ⌘L / Ctrl+L reaches the app (macOS menu item `web-address`; Linux accelerator in `web/platform.rs`, which forwards `t`, `w`, and `l` only). ⌘F / Ctrl+F does not. `t-d5-nat-web-find` adds Find the same way: a View-menu item `web-find` with Cmd+F on macOS, and the letter `f` on the Linux accelerator list. `t-d5-tab-web-keys` (TW-15) keeps the other chords. The renderer task `t-d5-tab-web-find` consumes `{ found, matches? }` and draws the field. Until both land, find stays absent (WEB-QUESTIONS W9).

### Verification

Evidence is the public headers, the GIR, the crates already in this tree (`objc2-web-kit` 0.3.2, `webkit2gtk` 2.0.2, Tauri 2.12.1), and the existing `with_webview` call sites. No Mac was available in this lane, so `findString` was not executed. No product, token, engine, or manual file changed. `desktop/src/features/web/find-decision.test.ts` locks this section and Open row TW-14.
