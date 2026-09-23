# Open-issue sweep: 1091, 1197–1202, 1277, 1286, 1287, 1292, 1297, 1299, 1301–1303
Read-only audit against `origin/santos/dev2` at 80bdef36e (2026-09-23 13:31 EDT), GitHub Agent-Field/CodeAF.
Method: gh issue view/--comments + issues/N/timeline; `gh pr list --state all` (500 PRs) text search;
`git log origin/santos/dev2`/`--all --grep="#N"`; topic keyword logs; `git show --stat` on each candidate;
`git grep` at origin/santos/dev2 for each acceptance marker; `git merge-base --is-ancestor` for containment.

1091 STILL OPEN — internal/store/memory.go, internal/session/memory.go and memory_consolidate.go last touched by e2ae913b7 (rename, 2026-09-15); no commit/PR/change entry for the architecture's seams.
1197 SOLVED — commit abf80327f (PR #1360 "e2e: the tmux suite reads the surface again"), ancestor of origin/santos/dev2; `git show --stat abf80327f` = internal/e2e/tui_e2e_test.go (+226/-58), tuiwords_test.go, refusedargs_e2e_test.go, change entry 1360-*.md; its per-subtest table names exactly the issue's six (home foot carries ctrl+o, `here` clause, `needs you` without count, projects on the right rail, refused proposal settles on `not started`, task-room `→`). Caveat: the 2026-09-21 comment adds `a_nested_landing_asks_and_a_key_answers_it` and `TaskOnTheRunEngine`; #1360 declares the latter "another lane's and untouched" — follow-up, not the issue's six.
1198 STILL OPEN — internal/session/groundladder_test.go last changed by e2ae913b7; no commit/PR/entry for the TempDir `.git: directory not empty` cleanup race.
1199 STILL OPEN — no `## ` heading under internal/manual carries "model family" or "picked from learn" on dev2 (models-and-cost.md:504 is body text below the unchanged heading at :468); no manual probe or alias for the two questions.
1200 STILL OPEN — the run-engine door `runErrand` (cmd/codeaf/do.go:3413) sets `stopDeadline` at :3503 yet never sets Verdict/KeptBranch; those are composed only in settlementWatch (do.go:766, :2601-2604). No commit anywhere about a kept branch on a deadline; no change entry names 1200.
1201 STILL OPEN — `writeSweepLast`'s `Judged` count unchanged since #1142 (6eaeb25c9); #1395's only poolrecord.go edit is the claim file (#1267). Text form still says nothing when no nonce is set (cmd/codeaf/pool.go:941 appends " · identity set" only when set), so "says that none is set and why" is unmet.
1202 STILL OPEN — `cfg.Unattended = opts.Yolo` (cmd/codeaf/chatv3.go:1102) is still the only setter; do/exec/run never set it; the issue asks for a ruling and none is recorded.
1277 PARTIAL — on dev2 skills is end-to-end for six roots (#1365), withheld skills say so (#1382); still open: drafts #1409/#1410 (a draft is not a fix), #1396, #1327, and children #1403–#1408.
1286 STILL OPEN — no per-chat history or stash work; feature, needs grooming.
1287 STILL OPEN — no offer-to-run-as-a-task flow; no commit/PR.
1292 STILL OPEN — tokens.DetectGlyphSet still has no caller under cmd/; the command-line path has no detection or fallback.
1297 STILL OPEN — cmd/codeaf-suite-lock documents the accepted failure ("If the holder is killed, the lock frees while the suite may still run") and the lock file names both pids, but nothing outside the wrapper reads it back and there is no process-group teardown; the orphaned `*.test` binary is still unaccounted for.
1299 STILL OPEN — no keymap/keybind infrastructure and no `?` overlay under internal/tui3; page bindings remain per-surface switch statements.
1301 STILL OPEN — `ToolApprovalModes = []string{"prompt", "allow", "deny"}` (internal/config/settings.go:550) with the row labelled "ask before running" (:2107); the approved one-word set is not in the code.
1302 STILL OPEN — no "set by project" pin or effective-value row anywhere in internal/; the sheet still reads and writes the global profile only.
1303 STILL OPEN — no "Models & Providers" node on dev2; children 1299/1301/1302 open (1300 closed).

## Safe to close
1197 — link PR #1360 / commit abf80327f ("e2e: the tmux suite reads the surface again").
Nothing else is safe yet; 1277 must stay open until #1410 (and #1409, #1396, #1327) land.
