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

