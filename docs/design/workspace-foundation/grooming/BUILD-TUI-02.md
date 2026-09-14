# TUI checkpoint 2 — continuing local-file work in the same fixture

**Status: bounded checkpoint-2 acceptance PASSED on open-weight models (2026-09-14), phase by
phase — not a single-revision run, and not J1 as a whole** (report renames and full
reliability stay open in [NEXT-STEPS.md](NEXT-STEPS.md) J1). Lane
`codex/personal-experience-0914`, base `0ac146768` (runtime identical to checkpoint 1's
`36922486c`). Claude Code Opus on `spark`. W5-B stays **not accepted** (21/37). Demo 1
(`/home/santosh/src/af-pai-demo-36922486c`, `/home/santosh/aforge-pai-demo-36922486c`) was
not touched. Compact tracked evidence:
[validation/tui-checkpoint-2.md](validation/tui-checkpoint-2.md).

| Phase | Fleet job | Revision | What it proves |
| --- | --- | --- | --- |
| Implementation + DeepSeek/Haiku exploration | `20260914-203238-000447` (interrupted for the model correction; `…000449` made no edits) | `0ac146768`-dirty | Exploration only; Haiku never qualifies |
| Source | `20260914-211634-000450` | `9687f15c5` | Focused tests; Opus review 1 |
| Acceptance 1 | 450 | `5417b4b71` (= `9687f15c5` + doc) | Setup through chat passed; stopped for fixes; retained |
| Review-1 fixes; acceptance 2 **setup** | 450 | `061ffdc2f` | Setup through chat on GLM-5.3-Flash |
| Acceptance fix; acceptance 2 **inspect/run/pause/resume/edit/stop** | 450 | `9d9beebc3` | Every remaining proof |
| Review-2 fixes (final runtime) | 450 | `92ba84022` | Deterministic regression tests and a no-call re-read of the accepted record — **not** a live rerun |
| Reconcile with the journey checklist `b99b784f7` | `20260914-220626-000455` | merge (see *Integration*) | Docs only; runtime diff to `92ba84022` empty |

Coordinator integration builds carried as receipts (docs/runtime-unchanged, not
acceptance): Fleet448 `20260914-203328-000448` at `0ac146768877f84a6d6767847c3175ca2279568c`,
`make build` passed, ended 20:33:32Z; Fleet454 `20260914-214321-000454` at `b99b784f7`,
`make build` passed, ended 2026-09-14T21:43:25Z.

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

**Decision (user, C28 — drafted here as C27 before the journey goal took that number):** product, validation and demo runs use open-weight models only.
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
**Guarantee scope.** What is guaranteed is narrow: this journey's call log and usage
ledger contain only allowlisted open-weight IDs, and the profile pins chat, fallback, five
tiers and vision. **Still unpinned (stated, not implied):** image/speech/music/video making
and listening/watching resolve from the catalog and curated names, some closed; no gate
enforces a profile allowlist; nothing in the journey invoked them. Enforcement is an open
J1 model-reliability step, not done. v1 `aforge models` still prints the
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
watch's own matcher. **Re-review** (second Opus pass over `9687f15c5..9d9beebc3`): nine
findings fixed, three partial, and one new regression — the input filter put every name
inside the watched folder, so under `product/**` a real rival report
(`reports/old-digest.md`) was silenced. Fixed in `92ba84022` with the smaller items (only a
bare name is put inside the watched folder; `e` replaces an older lead instead of stacking;
adopted wording; `checked` dot; manual on `e` and rules; fixture STAGE check before the
seeder build). Tests cover `product/**` and the older lead.

## Acceptance — real terminal, isolated profile, open-weight models

Driver: tmux on Spark, 200×52, `AFORGE_HOME` isolated, pinned binary copy in the profile,
serial live calls, no retries. Evidence `/tmp/af-pai-exp-logs/cp2-accept2/` (screens,
`timeline.txt`, `receipts-*.txt`), profile `/tmp/af-pai-cp2-accept2`.

| Proof | Result |
| --- | --- |
| Set up through the chat | `Product reports` chat on `glm-5.3-flash` (binary `061ffdc2f`): `ls .`, `ls product`, `stand` refused `rails.per_run_usd 0.05 came with no cost_words…`, `stand` again → card with `folder · Product, where this conversation is placed — its rules reach every run` and the rule; `aforge standing list` before the yes showed only the rule; yes → `60acae90a7652958`. 7m42s of interactive journey, 4 tool calls: ≈447 s chat-model streaming (one Sail Research stream cut at 2m30s and re-asked by the harness, 160 s; one 139 s Reka call; ≈43 s time-to-first-token), ≈20 s driver approval/card answers, tool execution seconds (breakdown in the validation file). |
| Inspect in Folders | restart on `9d9beebc3`: `state active`, `wakes when product/** changes`, `report · reports/product-digest.md · published by aforge`, three `checks` lines, `limits $0.05 a run · 10 runs a day`, `instructions v1`, no false named-file warning. |
| External change → explicit check | `aforge standing check` → `1 checked · 1 ran` (31 s); in place, selection unmoved: `last run now · landed · v1 · $0.002`, `why modified product/spec.md`, `published … 385 bytes`, `held to 1 kept · rule 659dac6d` (the inherited Product rule), `on disk 385 bytes · put there by aforge now · sha 2254fc05`. Report quotes no contact detail. |
| Pause ⇒ no run | `p` → store `paused`; change 2; check → `nothing was due`; runs `{000001}`. |
| Resume ⇒ pending change observed | `p start again` → `going again`; check → `1 checked · 1 ran`; run `000002` `modified product/spec.md`, 611 bytes, report now covers change 2; `before 1m ago landed`. |
| Edit through its chat | `e` → box `Change the ongoing work “product spec digest”: `; sentence sent; `stand op: edit id 60acae90…` (report unchanged) → edit card → yes → `specRevision 2`, inspector `instructions v2`. |
| Stop ⇒ no run, no restart | `s` → `stopped`, strip offers none of `p`/`s`/`e`; change 3; check → `nothing was due`; `aforge standing resume` → `a stopped item must be set up afresh`. |
| Open-weight receipts | calls.jsonl + usage.jsonl: `z-ai/glm-5.3-flash` (chat turns and both runs), `qwen/qwen3.8-27b` (rules check), `deepseek/deepseek-v4-flash-0731` (captions/titles). **NON-OPEN MODELS: none.** |

Setup ran on `061ffdc2f` and the rest on `9d9beebc3`; the only source change between them
is `session.StandingNamedFiles`, which only the inspector reads. The final `92ba84022`
changes the inspector's named-file filter, `e`'s lead handling and two wordings; the
stopped item was re-read on it through the real engine (`40-final-binary-inspect-stopped.txt`):
the same lines (`held to 1 kept · rule 659dac6d`, `instructions v2`, `on disk 611 bytes · put
there by aforge`, no named-file warning), with no model call (`calls.jsonl` 46 → 46). Acceptance 1
(`/tmp/af-pai-exp-logs/cp2-accept1/`, binary `5417b4b71`) is retained: setup passed
(3 tool calls; first `stand` refused `folder scope is only available for a rule that does
not wake`), then stopped for the review fixes.

**Spend (cumulative checkpoint 2):** exploration ≈ $0.14 (incl. Haiku $0.108), acceptance 1
$0.0116, acceptance 2 $0.0311 → **≈ $0.18 of the $1 budget**.

**Review 2** (Opus, over `9687f15c5..9d9beebc3`): 9 fixed, 3 partial, 1 new regression — the
input filter put every name inside the watched folder, so under `product/**` a real rival
report was hidden. `92ba84022` resolves all of it (bare names only; `e` replaces an older
lead instead of stacking; adopted wording; `checked` dot; manual on `e`; STAGE check order;
comments), proven by deterministic tests (`TestStandingNamedFilesListsOnlyOtherReportShapedPaths`
with `product/**`, `TestOngoingWorkIsPausedStoppedAndEditedOnlyThroughItsOwners` with an older
lead) and the re-read above. It was not re-accepted live: the setup path is unchanged and
the accepted screens re-read identically.

**Verification of `92ba84022`** (third independent Opus pass, read-only, job `…000455`): all
seven review-2 findings RESOLVED with file:line evidence; runtime unchanged by the docs
merge; nothing blocks. Non-blocking risks it named, kept open rather than changed now so the
handed-off runtime stays the reviewed one: `e`'s lead stripping removes everything up to the
first `”:` in a draft that starts with `Change the ` (a person's own such sentence, or a
title containing `”:`, loses its start — fix by matching the exact `ongoing work “`/`rule “`
opening on one line); a bare name such as `digest.md` under `product/**` is treated as an
input (documented ambiguity); `./spec.md` is read as written and warned; untested branches:
absolute watch, empty check line, `e` on a rule over an older lead, adopted word order.

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

## Demonstration 2 (pinned, separate from development)

Worktree `/home/santosh/src/af-pai-demo2-92ba84022` (detached at `92ba84022`, `make build`
exit 0 on Spark), profile `/home/santosh/aforge-pai-demo2-92ba84022` (`STAGE=2`). **It starts
with no ongoing work** — only the folders, chats, the finished task and Product's rule; the
person sets the work up. Verified 2026-09-14 ~22:15Z through the laptop form below: no
first-run setup, `glm-5.3-flash` on the new-conversation line, `OPENROUTER_API_KEY` present
with an `sk-or-` shape in both the TUI and its engine (value never printed or stored), Folders
reaches Startup → Product → Product reports (`chat · idle · placed`); the check command
answered `nothing was due`; no model call logged; engine stopped afterwards. The key is read
from the Spark account's `~/.aforge/config.json` at launch because the demo profile holds none
and a non-interactive ssh shell does not export it.

From the laptop ([validation/tui02-launch-command.txt](validation/tui02-launch-command.txt)):

```sh
ssh -tt spark 'cd /home/santosh/aforge-pai-demo2-92ba84022/fixture/startup && env -u AFORGE_MODEL -u AFORGE_VISION_MODEL AFORGE_HOME=/home/santosh/aforge-pai-demo2-92ba84022 OPENROUTER_API_KEY="$(jq -r .api_key ~/.aforge/config.json)" /home/santosh/src/af-pai-demo2-92ba84022/bin/aforge'
```

Keys, as the surface spells them (source and acceptance screens):

1. `alt+8` opens `folders`; `enter` goes into `Startup`, then `Product`; `↓` to
   `Product reports` (`chat · idle · placed`); `enter` opens it.
2. Type, then `enter`: `Keep reports/product-digest.md current: whenever a file in product/
   changes, rewrite it as a short digest of the product spec. At most $0.05 a run.`
3. `stand` first shows `allow? [1] allow once …` under the default approval policy: `1`.
   If the model's first call is refused (it may retry once), allow again. On the card
   (`folder · Product …`, `rule · …`) press `1` for `yes, set it up`.
4. `alt+8`, select `product spec digest` or whatever title the card used
   (`ongoing work · active · placed`) to read the inspector.
5. Second terminal — change a watched file and run one check:

```sh
ssh -tt spark 'cd /home/santosh/aforge-pai-demo2-92ba84022/fixture/startup && echo "- Pricing: annual plan billed yearly (demo change)." >> product/spec.md && AFORGE_HOME=/home/santosh/aforge-pai-demo2-92ba84022 OPENROUTER_API_KEY="$(jq -r .api_key ~/.aforge/config.json)" /home/santosh/src/af-pai-demo2-92ba84022/bin/aforge standing check'
```

6. With the work selected, `→` shows the verbs: `p pause` (then `p start again`), `s stop`,
   `e edit in its chat` (opens the chat with `Change the ongoing work “…”: ` ahead of
   anything already typed; the change applies only on its card's `1`). A stopped item offers
   none; `aforge standing resume` answers `a stopped item must be set up afresh`.

Setup took 7m42s in acceptance, almost all provider streaming; each chat message and each run
is an ordinary paid call (acceptance total $0.031). Unsupported in the demo: a TUI "check now"
(use the command above or the open window's 5-minute pass), a background timer (off in this
profile), report-path renames, the Product → Marketing watch, and any guarantee about making
verbs' models.

## Integration with the journey checklist (job `20260914-220626-000455`)

`origin/codex/personal-ai-backend` at `b99b784f7` (journey-driven NEXT-STEPS, NEXT-STEPS-HISTORY,
PRODUCT-JOURNEY-MAP, C27–C28) merged into this lane as `218dbb086` (parents `b777ed109`,
`b99b784f7`). Conflicts were docs only (NEXT-STEPS, DECISIONS, PRODUCT-EXPERIENCE-PATH):
the journey checklist is the base, the lane's open-weight draft folded into C28, and this
checkpoint's status was written into J1 from the evidence. `git diff 92ba84022 218dbb086`
outside `docs/` is empty (manual included); `git ls-tree -r` without `docs/` hashes to
`111496efc9005ba3` at both. `make build` on Spark at `218dbb0863cf1a36df2d2986b7cd250b3dc897b9`:
exit 0, 2026-09-14T22:13:02Z–22:13:03Z (build cache warm). No paid call and no broad suite
was run for the merge: runtime did not change. Later commits on the lane are docs only.

## Not in this checkpoint

Roles, compound triggers, semantic discovery, dynamic collaboration, a TUI instruction
editor, a TUI "check now", timer installation, report-path rename reliability, and the
Product → Marketing explicit watch (checkpoint 3).
