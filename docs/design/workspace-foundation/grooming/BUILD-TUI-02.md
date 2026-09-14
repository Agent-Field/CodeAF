# TUI checkpoint 2 — continuing local-file work in the same fixture

**Status (current): IN PROGRESS — contract recorded, implementation not yet validated.**
Lane `codex/personal-experience-0914`, base `0ac146768` (runtime identical to checkpoint 1's
`36922486c`). Claude Code Opus, Fleet job `20260914-203238-000447` (checkpoint 1 was
`20260914-192937-000446`), host `spark`. W5-B stays **not accepted** (21/37).
The user's checkpoint-1 demo (`/home/santosh/src/af-pai-demo-36922486c`,
`/home/santosh/aforge-pai-demo-36922486c`) is not touched by anything below.

## Contract

A person in the SAME Startup → Product/Marketing fixture:

1. **Sets up** one local-file responsibility **through an actual chat**: in a conversation
   placed in Product, a sentence asks for `reports/product-digest.md` to be kept current
   when files in `product/` change. The existing `stand` card is answered in the TUI.
   Nothing is created before the yes; the work inherits Product's placement.
2. **Inspects it in Folders** (the checkpoint-1 inspector, extended, still read from the
   serving engine): trigger (`when product/* changes`), the stored report destination
   and who writes it, per-run and per-day limits, state (active / paused / stopped /
   checking now), instructions version, **how checks happen** on this machine (this
   engine's open window every 5 minutes, or `aforge standing check`; whether a background
   timer exists — never implied), the rules that reach it, and the last runs: when, why
   (the changed files), result, spend, the owner's publication receipt, the rules check,
   withheld code.
3. **Discrepancies are visible, not repaired silently:** the last publication's path
   differing from the stored `does.report`, and files the instructions name that could be
   a report but are not the stored destination (the existing named-report reading).
4. **Changes a fixture file externally**, runs one explicit `aforge standing check` (or the
   open window's pass), and sees state, run, cause and report update in the inspector
   without the selection moving.
5. **Controls through existing owners only:** `p` pause / start again and `s` stop, the
   same letters and words as home and the standing place, written through
   `StandingSeam.Save` → `standing.Store.SetStatus` (engine `SaveStanding` on the engine
   road). **Edit instructions through the chat**: `e` opens the conversation the work was
   set up in, where the existing edit card (`stand op: edit` → `Store.Revise`, fenced on
   `SpecRevision`) applies it after a yes. No raw document write from the UI, no new
   lifecycle owner, no report-path rename promised.
6. Preserved: ordinary unfiled chat; the shared chat's one history; filing ≠ placement;
   no folder agents; references never become rules; permissions not broadened.

Proofs required in acceptance (real binary, real stores, isolated profile):
pause ⇒ a check after a change admits no run; resume ⇒ the pending change is observed by
the next run; stop ⇒ no run and no restart; the inherited Product rule reaches the actual
run (its id in the run's rules check); the report is published by the owner with a
receipt and the stored path stays correct. Model spend ≤ $1 total, serial, no retries to
green; failures retained.

## Existing seams used (exact)

| Need | Seam | Where |
| --- | --- | --- |
| Set up from chat | `stand` tool card; placement inherited from the conversation's governing folders | `internal/session/standing_placement.go`; manual `standing-orders.md` *Keep a report current from the chat* |
| Edit instructions | `stand op: edit` card → `standing.Store.Revise(id, specRevision, …)` | `internal/standing/revise.go`; manual *Change ongoing work from the chat* |
| Pause / resume / stop | `tui3.StandingSeam.Save` → `Store.SetStatus` (local); `client.SaveStanding` → engine `SetStatus` (engine road) | `cmd/aforge/chatv3_standing.go:344`, `cmd/aforge/engine.go:743`, `internal/tui3/place_standing.go` `write` |
| Runs, cause, result | `Store.Occurrences(id, n)` (`Changes`, `Phase`, `Outcome`, `Published`, `RuleCheck`, `Withheld`, `Spec`, `USD`, `PerRunUSD`) | `internal/standing/occurrence.go` |
| Report receipt | `Store.Receipt(ReportPath(item))` | `internal/standing/receipt.go` |
| Checking now | `Store.Running(id)` | `internal/standing/running.go` |
| How checks happen | engine process ticking (`standingTicking`), `standing.background` profile row, `aforge standing check` | `cmd/aforge/chatv3_standing.go`, `internal/config/settings.go` |
| Named report files | `standingLooksLikeFile` + `standingCouldReport` | `internal/session/standing_placement.go` |
| Inspector transport | `workspaceview.Item` → `Collections.Item` (read-only) | checkpoint 1 |

## Not in this checkpoint

Roles, compound triggers, semantic discovery, dynamic collaboration, a TUI instruction
editor, a TUI "check now", timer installation, report-path rename reliability, and the
Product → Marketing explicit watch (checkpoint 3).
