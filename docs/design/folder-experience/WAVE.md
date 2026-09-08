# Folder experience wave — 2026-09-08

## Owner's request and branch authority

The owner asked for a proper, spacious terminal folder modal with Finder-like successive columns, clickable navigation and strong search, and for an added folder to actually reach the chat's context immediately. This is the live v3 surface in internal/tui3 and internal/session.

The owner's latest instruction explicitly overrides the repository's default dev workflow FOR THIS WAVE: base all work on their CURRENT codex/conversation-execution, captured at d2d86f02a08ecb58ab5714b93a2af5373d5be715. Integrate worker branches into codex/folder-experience. The PR targets codex/conversation-execution on origin, never dev. Do not merge that final PR or update the target branch: the owner will merge the integrated branch into their Codex branch later. No promotion or release. No force pushes.

This seed includes two commits that were ahead of origin/codex/conversation-execution when captured. Preserve them. The owner explicitly chose the exact local branch. Do not rewrite or drop its history, or silently change the base to dev. Explain baseline differences in the PR while the target catches up.

The user explicitly requested parallel Claude Opus sessions on Spark. Codex integrates them. All coordination must run on Spark and survive laptop disconnect. No work depends on the Mac after launch.

## Product contract

Adding a folder means attaching a directory reference to this conversation, not reading its entire tree into the prompt. The next model request must identify the chosen absolute directory and the fact the user attached it; it must not search the home directory to rediscover it. Attachments survive restart, are visible in a compact removable folder indicator, and do not silently move the original working directory. Multiple folders have explicit scopes, no silent instruction blending. Read applicable attached-project instructions with clear path scope and a bounded overview / retrieval strategy. Do not load an entire repo or invent capabilities.

Preserve existing write-isolation and /land semantics; make the distinction between attached folders and the original working directory clear without implementation jargon. Preserve accurate paths, including spaces, Unicode and explicitly chosen subdirectories; if repository-root snapping is necessary for task isolation, do not silently misrepresent the selected context scope.

The UI is an intentional large terminal modal/sheet with successive parent/child folder columns (Miller-style), a breadcrumb path, mouse selection/navigation, scroll support, complete keyboard operation, and a separate Add folder action. Search results must open for browsing without retyping their paths. Esc cancels and preserves the draft. Responsive small terminals, hidden-folder access, empty/unreadable directories and stale async results must work honestly. The owner's requested modal and attached-folder indicator supersede older design proposals that ban these elements.

Search should handle basename/path-segment matches, abbreviations, common transposition typos such as afroge, useful recency/frequency, existing project/conversation folders and non-Git directories. Open fast, avoid broad blocking recursive scans per keystroke, do directory IO asynchronously with cancellation/stale-response handling. Show useful errors; never equate permission denied with empty. Bound background discovery; do not index file contents for a folder chooser.

## Diagnosed gaps to recheck on this exact branch

- internal/tui3/folderplace.go referPlace type-asserts placeReferrer, but still displays folder success when absent.
- The local-host client goes through the wire yet a.hosted() is only true when the far hostname is nonempty. The local engine can lack ReferPlace/Places and still show the picker.
- The exact observed session cbb4f0be56591db8 had no Places in its saved metadata after the UI said folder added.
- session.places feed taskstands and standingtree, but not the next model context. A displayed feed note is not a model message.
- Current folderpick.go caps at twelve rows and draws parent / here / facts, not successive children. Mouse folder integration is missing.
- ideation/folder-picker-v2.md was an untracked proposal, not completed work; it includes stale claims and retractions. Treat the copied reference as design background, never higher authority than this brief.

## Parallel ownership

- context (Opus): internal/session folder registration/removal/context/persistence/scoped instruction behavior; internal/remote wire/client/server capability and methods; regression tests. Own its focused manual update about folder meaning/removal if needed. Do not redesign folderpick.go or UI event maps.
- browser (Opus): internal/tui3 folderpick.go, folderplace.go, frame/input/hover/mouse wiring and folder indicator. Own folder picker UI manuals. Reuse the existing registration API, use RemovePlace(path string) error and Places() for removable references; coordinate any necessary interface adjustments through status notes.
- search (Opus): isolate ranking/discovery into NEW foldersearch*.go / folderindex*.go helpers and meaningful tests. Do not edit folderpick.go/folderplace.go shared browser files. Publish a tiny integration note with exact function signatures and call sites. Browser or coordinator wires these helpers. Do not change shared @ file completion scoring.
- coordinator (Codex): integrate commits as lanes finish, wire search helpers, resolve shared interfaces and conflicts, verify whole product, write final changelog and review-ready PR against codex/conversation-execution. Only the coordinator mutates codex/folder-experience.

Each lane has its OWN worktree. Never stage all files, touch another worktree, rewrite shared refs, or perform unrelated cleanup. Read repository instructions and relevant change entries, then work autonomously within this explicit owner request. Write progress in the shared lane report and push checkpoints. Do not launch extra agents.

## Verification and deliverables

Workers run focused regression tests; coordinator runs make build, relevant engine/wire/TUI suites (TUI timeout 15m), make test-laws, packed manual checks and required checks. Capture outputs to files. Respect the known-red ledger; do not add entries. Limit test concurrency on shared Spark (GOMAXPROCS=4, GOFLAGS=-p=2 unless measured need); no unrelated GPU jobs.

Exercise the real binary in an isolated profile / fixture through a local engine connection: select folder -> ask about folder -> captured next request knows exact path; remove -> future context no longer presents it as attached; reconnect/reopen -> selection remains; multiple folders retain correct scope. Drive terminal keyboard AND mouse behavior, small/wide sizes, folder names with spaces/Unicode, search-to-browse, empty/unreadable dirs and draft-preserving cancel. Use model stubs for deterministic assertions and live model checks only with existing configured credentials; never print secrets. Never report skipped e2e as passed.

Build ONLY via make build into each worktree's bin/aforge. Never install Linux artifacts onto the Mac or replace a shared running binary. Keep the integrated remote worktree and local review worktree for the owner. Clean only disposable worker worktrees after their commits are pushed and integrated, if it does not destroy useful evidence.

Final shared SUMMARY.md must name base/head hashes, branch/PR link, merged lane commits, exact tests and limitations, remaining blockers and Mac fetch/merge/build steps. A clean build alone is not completion. Never claim completion if context attachment, clicks, search or persistence is missing. Keep the PR draft while unfinished or mismatched, and leave final merge into codex/conversation-execution to the owner.


## Owner steering 02 — files, previews and visual quality

The 2026-09-08 update expands this wave beyond folders. Build one Add context browser for folders and files: `/folder` opens with folder intent and bare `/attach` reuses the browser while explicit paths, paste/drop and existing image attachments retain their behavior. Opening or previewing never attaches; confirmation distinguishes persistent folder references from file/image attachments. Support deliberate mixed selection with a compact tray.

Use the supplied Yazi image as layout guidance: readable current path, narrow ancestry, generous active contents, stable useful preview and compact explicit controls. Preserve successive folder navigation, aforge theme and plain-font usability. Preview can hide or expand; narrow terminals keep filenames and controls legible. The supplied Nearform interview summary emphasizes keeping essential information present and revealing secondary panes on demand.

Code previews use existing Chroma syntax highlighting with language detection, subdued line numbers, safe tabs/Unicode, ANSI-safe horizontal clipping, bounded reads and visible truncation. Never execute files or render embedded control sequences. Reuse `internal/tui3/imagepreview.go` for aspect-preserving half-cell TrueColor/ANSI256 pictures; this is portable text-cell rendering, not Kitty/iTerm graphics. Unsupported renderers and PDF/video/archive files need honest useful fallbacks. Keep preview reads asynchronous, bounded, cached and protected against stale results.

Capture and visually inspect real wide/narrow terminal screens for dense folders, code, text, images and fallback, mixed selection, no matches and permission errors. Verify mouse, wheel, keyboard and pane controls agree. End-to-end local-engine evidence must show preview without attachment, deliberate confirmation, correct chips and the actual next request carrying exact folder scope or existing file/media attachments; verify removal/reopen and draft preservation. Spark evidence cannot prove Mac-specific graphics.

PR #657 stays draft until this expanded contract is supported. Shared `reports/QUALITY_READY` is required alongside `reports/READY`; neither is earned by unit tests or a clean build alone. Full owner steering and screenshot remain in the shared wave directory as `STEERING-02.md` and `reference-yazi.png`.

## Owner steering 04 — concurrent conversations through the local engine

The same wave and PR #657 also fix ordinary local-engine conversations closing
with "a connection holds one conversation at a time" when another tab opens.
Multiple conversations must run concurrently, with independent event, context and
cancellation routing. Opening or switching tabs must preserve earlier active work.
Removing the warning alone does not satisfy this contract.

The async worker owns engine/remote/conversation lifecycle changes, tests and
manual corrections in sibling async, branch codex/folder-async-20260908, based on
integration df53007eb81b398834792ef7061176a5a44d0001. It is the fifth host lane;
the preview lane remains responsible for new contextpreview helpers. Coordinator
owns existing browser UI integration and must inspect runner exit, scoped commits
and realistic concurrent-turn evidence before merging async with history.

READY, QUALITY_READY and PR readiness require the async lane to be integrated and
combined real local-engine acceptance to pass, including simultaneous work,
isolated events/context/cancellation and tab switching without closing earlier
work. Keep the target codex/conversation-execution and leave the final merge to
the owner. Preserve all active worker worktrees.

## Owner steering 05 — closing a tab preserves work

Switching tabs and going Home preserve active work. Closing an active chat offers
"Keep running", "Stop work" and "Cancel" using existing dialog conventions.
Keep running hides the tab and keeps the conversation discoverable in Chats with
understandable running, completed or needs-attention state. Reopening recovers
live progress and output without duplicate execution. Cancel and Escape leave the
tab and work untouched; Stop work explicitly stops only that conversation. Idle
and completed tabs close without unnecessary friction. Closing never means deleting
history, and this change must not silently alter whole-app shutdown semantics or
promise work survives quitting without evidence.

Async owns the minimal dialog, lifecycle, status and discovery UI for this contract.
Before implementing, publish the Opus UX decision in reports/async.md: wording,
focus/default, keyboard navigation, hidden-chat discovery and safe transitions.
Coordinate overlapping UI files with the coordinator's context browser work.
Acceptance must cover real engine/TUI Keep running and Stop work paths while a
second chat runs, Home and tab switching, reopening without duplicate execution,
completion racing with the dialog, pending permission/input, mouse and keyboard,
narrow terminals and accessibility tiers. Neither readiness marker nor PR readiness
is allowed until this behavior is integrated and evidenced.

## Completion ownership — steering 06

All five initial lanes are integrated. The finishui worker, based on b7e75f60d,
owns the complete browser/preview/mixed attachment UI and its real terminal and
next-request evidence. Coordinator owns async lifecycle, permission/input,
completion timing, reconnect and held-task stopping. Preserve the active lane;
merge only after its runner exits and scoped committed work is reviewed. Full
combined suites and product acceptance follow integration. Existing valid
evidence is retained; unchanged baselines are not repeated each supervisor pass.
