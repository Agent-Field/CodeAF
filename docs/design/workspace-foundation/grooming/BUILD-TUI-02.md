# TUI checkpoint 2 — continuing local-file work in the same fixture

**Status: ACCEPTED on open-weight models (2026-09-14).** Lane `codex/personal-experience-0914`,
base `0ac146768` (runtime identical to checkpoint 1's `36922486c`). Source `9687f15c5`
(implementation), `061ffdc2f` (review fixes), `9d9beebc3` (acceptance fix); real-terminal
acceptance on `9d9beebc3` with setup on `061ffdc2f` (see *Acceptance*). Claude Code Opus,
Fleet jobs `20260914-203238-000447` (interrupted for the model correction) and
`20260914-211019-000449` onward, host `spark`. W5-B stays **not accepted** (21/37). The
user's checkpoint-1 demo (`/home/santosh/src/af-pai-demo-36922486c`,
`/home/santosh/aforge-pai-demo-36922486c`) was not touched.

Maintained integration build receipt carried from the coordinator: Fleet448
`20260914-203328-000448` passed `make build` at `0ac146768877f84a6d6767847c3175ca2279568c`
at 20:33:32Z (source identical to the checkpoint-1 runtime).

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

## Model decision and the DeepSeek failure (diagnosed before any new paid call)

**Decision (user, C27):** product, validation and demo runs use open-weight models only.
The checkpoint's earlier Haiku exploration (`anthropic/claude-haiku-4.5`, $0.108 of the
$0.14 exploration spend) is historical evidence, not acceptance.

**Exact IDs.** Exploration chat turns requested OpenRouter's floating alias
`~deepseek/deepseek-v4-flash-latest`, whose `alias_target` (retained catalog, fetched
20:53Z) and every response's `model` field were `deepseek/deepseek-v4-flash-0731` —
DeepSeek **V4 Flash 0731** (HF `deepseek-ai/DeepSeek-V4-Flash-0731`, MIT). It was **not**
V4.1 Flash: `deepseek/deepseek-v4.1-flash` (HF `deepseek-ai/DeepSeek-V4.1-Flash`, MIT,
created 2026-09-10) is reached by `~deepseek/deepseek-flash-latest` and was never called.
The user's "GLM 5.3 Flash" exists exactly: `z-ai/glm-5.3-flash` (canonical
`z-ai/glm-5.3-flash-20260826`, HF `zai-org/GLM-5.3-Flash`, license MIT, not gated, `tools`
supported on all 27 endpoints listed 21:17Z). Evidence:
`/tmp/af-pai-exp-logs/cp2-models/` (catalog, endpoints, HF metadata).

**Failure, from observable execution only** (allowlist projection of user messages, tool
names/arguments, tool results and call metadata; assistant text and every reasoning field
omitted — `/tmp/af-pai-exp-logs/cp2-diagnosis/project.py`, `explore{1,2}-projection.txt`):

| Question | Finding |
| --- | --- |
| Was `stand` on the belt? | Yes: every setup turn carried 27 tools, the same belt Haiku used when it called `stand` first. The 10-tool calls are the engine's standing runs. |
| Was the prompt misleading or missing? | No: the `# Things that keep working after this window` section (STANDING_FACTS) was rendered for the conversation shape. |
| Tool errors? | None blocking: a missing report read (expected, file not created yet), `find` answered `fd is not available and could not be downloaded`, bash asks answered `default` (denied) by the driver. |
| Refusals? | None from `stand`; it was never called in 30+ calls (explore1 and explore2). |
| What happened | Re-listed the same folders 5–6 times and re-read the same files; when told directly to propose, called `propose_task` for an older fixture line (`draft an onboarding FAQ`). Silent-batch nudges fired at 17/22/33 calls; the mark reader said continue. |
| Reasoning replay dropped by an alias/slug mismatch? | Ruled out in source: the loop tags and the wire compares the requested alias on both sides (`reasoning.begin(model)`, `attachMessageReasoning(…, c.modelFor(request))`). |

**Classification: model limitation** under a correct belt and prompt. No product fix was
justified for it, and no prompt was tuned for one model. Two product weaknesses seen and
**not fixed** here: `find` is on the belt on a machine where `fd` cannot be fetched (the
absence law), and the novelty ledger reported `82% new lines` during repeated reads.

**Closed-model routes found in the fixture and closed:** an empty `models.fallbacks` lets
the catalog's `NearestModels` pick the next model on a refusal, the talk model came from
the build default, and `vision_model` was automatic. `scripts/demo-personal.sh` now writes
`model.talk` = `z-ai/glm-5.3-flash`, `models.fallbacks` = `deepseek/deepseek-v4.1-flash`
(or GLM when the talk model is that), tiers reflex `mistralai/mistral-nemo`, low
`deepseek/deepseek-v4-flash-0731`, worker and mastermind `z-ai/glm-5.3-flash`, high and
vision `qwen/qwen3.8-27b`, and unsets the `AFORGE_*MODEL` variables that beat those rows.
GLM-5.3 (non-flash) was left out: its HF license tag is `other`.
**Still unpinned (stated, not implied):** image/speech/music/video making and
listening/watching resolve from the catalog and curated names, some closed; nothing in the
fixture calls them and the call log is the receipt. v1 `aforge models` still prints the
build default and ignores `model.talk` — a misleading receipt, not used here.

## What was built

- `workspaceview.WorkFacts` on a standing item's reading: `Occurrences(id, 3)`,
  `Receipt(ReportPath)`, `Running(id)`, `session.StandingNamedFiles` and `CheckWays`
  (window from `standingTicking()` in the serving process, timer from the watch's own
  status for this home). Each failed part is named in `Errors`; nothing is written.
- Folders inspector lines, controls `p`/`s` through `StandingSeam.Save` (engine
  `SetStatus` on the engine road, and over `--host`), `e` into the setup chat with the lead
  ahead of any draft there. A stopped item offers none. Row tail says `stopped`.
- `standing.RuleCheck.Summary` is the one reading `aforge standing show` and the inspector
  share. Manual: `standing-orders.md` *Ongoing work in the folders place …*, `places.md`.
- Fixture `STAGE=2` (no ongoing work; Product reports filed **and placed** in Product).

**Review.** Independent Opus review of `9687f15c5`: no data-corrupting defect (status
writes go through `SetStatus` under the item lock and cannot write a stale spec back); 11
findings, all addressed in `061ffdc2f` (draft kept on `e`; honest fixture pinning; manual
strings; rules show no `checks`; adopted receipts; failed receipt read shown; no raw codes
and no report quote in `held to`; empty `·` slots; glyph door; stale host comment; tests
for `s`, the draft and the word helpers). Acceptance then showed finding 6 live — `the
instructions also name spec.md` for the digest's own inputs — fixed in `9d9beebc3` by the
watch's own matcher. A second Opus pass over the fixes: see *Re-review*.

## Acceptance — real terminal, isolated profile, open-weight models

Driver: tmux on Spark, 200×52, `AFORGE_HOME` isolated, pinned binary copy in the profile,
serial live calls, no retries. Evidence `/tmp/af-pai-exp-logs/cp2-accept2/` (screens,
`timeline.txt`, `receipts-*.txt`), profile `/tmp/af-pai-cp2-accept2`.

| Proof | Result |
| --- | --- |
| Set up through the chat | `Product reports` chat on `glm-5.3-flash` (binary `061ffdc2f`): `ls .`, `ls product`, `stand` refused `rails.per_run_usd 0.05 came with no cost_words…`, `stand` again → card with `folder · Product, where this conversation is placed — its rules reach every run` and the rule; `aforge standing list` before the yes showed only the rule; yes → `60acae90a7652958`. 7m41s, 4 tool calls (one stalled stream re-asked by the harness). |
| Inspect in Folders | restart on `9d9beebc3`: `state active`, `wakes when product/** changes`, `report · reports/product-digest.md · published by aforge`, three `checks` lines, `limits $0.05 a run · 10 runs a day`, `instructions v1`, no false named-file warning. |
| External change → explicit check | `aforge standing check` → `1 checked · 1 ran` (31 s); in place, selection unmoved: `last run now · landed · v1 · $0.002`, `why modified product/spec.md`, `published … 385 bytes`, `held to 1 kept · rule 659dac6d` (the inherited Product rule), `on disk 385 bytes · put there by aforge now · sha 2254fc05`. Report quotes no contact detail. |
| Pause ⇒ no run | `p` → store `paused`; change 2; check → `nothing was due`; runs `{000001}`. |
| Resume ⇒ pending change observed | `p start again` → `going again`; check → `1 checked · 1 ran`; run `000002` `modified product/spec.md`, 611 bytes, report now covers change 2; `before 1m ago landed`. |
| Edit through its chat | `e` → box `Change the ongoing work “product spec digest”: `; sentence sent; `stand op: edit id 60acae90…` (report unchanged) → edit card → yes → `specRevision 2`, inspector `instructions v2`. |
| Stop ⇒ no run, no restart | `s` → `stopped`, strip offers none of `p`/`s`/`e`; change 3; check → `nothing was due`; `aforge standing resume` → `a stopped item must be set up afresh`. |
| Open-weight receipts | calls.jsonl + usage.jsonl: `z-ai/glm-5.3-flash` (chat turns and both runs), `qwen/qwen3.8-27b` (rules check), `deepseek/deepseek-v4-flash-0731` (captions/titles). **NON-OPEN MODELS: none.** |

Setup ran on `061ffdc2f` and the rest on `9d9beebc3`; the only source change between them
is `session.StandingNamedFiles`, which only the inspector reads. Acceptance 1
(`/tmp/af-pai-exp-logs/cp2-accept1/`, binary `5417b4b71`) is retained: setup passed
(3 tool calls; first `stand` refused `folder scope is only available for a rule that does
not wake`), then stopped for the review fixes.

**Spend (cumulative checkpoint 2):** exploration ≈ $0.14 (incl. Haiku $0.108), acceptance 1
$0.0116, acceptance 2 $0.0311 → **≈ $0.18 of the $1 budget**.

**Checks on committed source (Spark):** `go vet` on touched packages, `gofmt`, `make
test-laws`, `internal/iconlaw`, untagged `internal/e2e`, `internal/manual`,
`internal/standing`, `internal/workspaceview`, focused `internal/tui3` (`Folder|Ongoing|
Collection|Manual|Draft|Standing|Home`), `internal/session` (`Standing|Manual|Belt`),
`cmd/aforge` (`Standing|Organization|Collection|Host`), personal fixture test, `make build`.
`cmd/aforge-demo-home` has 12 failures (`deps-weekly … {{evidence}}`) that are identical at
base `0ac146768` and not in `.github/known-red.txt`. Broad suites and the tagged E2E
package were not run (deferred).

**Observed, not fixed (carried):** the edit card answers `yes, set it up` for a change;
`aforge standing list` prints a blank `watch-offer` row; the card for work with no named
per-day limit is silent while the store records `10 runs a day`; GLM paraphrased the
folder rule into the instructions on one setup (the rule still reached the run by
placement); the harness added `the ask is not finished · carrying on` after a successful
setup in acceptance 1.

## Not in this checkpoint

Roles, compound triggers, semantic discovery, dynamic collaboration, a TUI instruction
editor, a TUI "check now", timer installation, report-path rename reliability, and the
Product → Marketing explicit watch (checkpoint 3).
