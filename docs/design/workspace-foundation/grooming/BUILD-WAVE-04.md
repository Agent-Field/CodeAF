# Wave 4 — the live validation's failures, closed at their seams

Wave 4 answers the live validation's scoreboard (`/home/santosh/work/pai-wave03-refs/validation-live/SCOREBOARD.md`, 8 FAIL of 37) and the three live8 failures that BUILD-WAVE-03 left as item 1. The work was one lane, Spark only. It had no tui3 run and no broad UI suite, and it did not merge to dev. Source: branch `codex/personal-e2e-opus`, from `84b423d87` / `4701f270d`.

## Outcome

- **Every item landed** at the seam the brief named. Every fix opens with a regression proven failing on the old logic at `84b423d87`, run in a throwaway worktree.
- **All seven named validators pass live** on `deepseek/deepseek-v4-flash`, one at a time: S02, S09c, S11, S12b, S17b, S25a, S27b. Three extra probes (S09c without the seeded report, S25a with its policy present, and a confinement probe) cost $0.036 in total.
- **Live9 (the acceptance protocol): 9/10 `DEMO PASSED`** at `b87a4e756`, $0.305 over ten serial runs. Wave 3 got 7/10. The one failure, run09, is autopsied below; it is not the wave-3 defect. Wave spend is about $0.57 of the $0.60 wave budget: live validators $0.036, the superseded live8 $0.209 and live9 $0.305, plus two unledgered replay checker calls, about $0.02.
- **S25b is recorded, not fixed** (see *Recorded, not fixed*).

## The plan

| Item | Validator | Seam chosen | Why this seam |
| --- | --- | --- | --- |
| 1 — ungranted tools absent | live8 05/07/08, S09c | `approval.Policy.Grants` (`internal/approval/approval.go:239`) answers what can run with nobody to ask. `Config.grants` (`internal/session/beltfacts.go:107`) applies it when `InTask` with a policy and not the guardian. `belt()` drops the rest before shelving (`internal/session/tools.go:256`). | This is the owner's Q3 ruling ("absent, not refused") at the one place a belt is made. The refusal classifier still names no tool: a granted tool that meets its boundary (`bash` outside its pattern) still reads as a question for the person. |
| 2a — the run is told its placements | S11, S12b | `renderStandingWorld(items, places, …)` in `standing_world.go`. `governingItemsLocked` keeps the placements it already read (`Agent.governingPlaces`). | Which rules apply is decided once by the owner (`ApplicableScope`). The run is told the placements so it does not decide again from a rule's wording. |
| 2b — per-rule check | S11, S12b, U2 | `standingRulesPrompt` and `parseStandingRuleVerdict` (`standing_rules.go`). `checkAgainstRules` (`standing_run.go:973`). `standing.RuleCheck.Verdicts []RuleVerdict` (`occurrence.go`). | An omission has nothing to quote, so "does it break a rule" can never find one. Asking each rule by kind makes an obligation cite where it is kept. |
| 3a — reads confined | S25a | `readRootGuard` (`internal/session/readroot.go`), a control-plane pre-action registered when `Config.readRoot` is set (`hooks.go:248`). Only `standingRunConfig` sets it, to the item's workspace (`standing_run.go:1244`). | The same place `taskGroundGuard` sits. It reuses `insideProject`/`deepestExisting`, the symlink-safe containment the report publisher already uses. |
| 3b — folder reads allowed | S25a | `approval.readOnlyCalls` table (`internal/approval/readonly.go`): `collections` list/show/find/governing and `shared_context` list/read/history are looks. | They join the existing read-only lift, which is one table rather than a second rule. |
| 3c — overshoot recorded | S25a | `Occurrence.PerRunUSD` stamped at admission, `Occurrence.OverLimit()`, and the `show` cost line. | The record carries the limit it was held to, so the overshoot is derived rather than asserted. |
| 4 — recursive `**`, bounded | S02 | `watched()` in `internal/standing/filewatch.go`, and `Item.CheckWatch` called by `Store.Create`, by `Store.Revise` when the waking changes, and by the stand tool before it asks. | One reader for the tick, setup and the report-in-watch law (`watchMatches`). |
| 5 — identical rewrite quiet | S17b | `contentHash` and `fileEntry.differs`. `fingerprint(…, last)` digests contents where it has a hash. `keepReading(…, refresh)` rewrites a same-digest manifest with fresh times. | Size and time stay the free first tier. Only a file whose size or time moved is read. |
| 6 — placements from the folder | S27b | `workspace.Store.Placed` (the placements key prefix, plan pinned). `collections show`/`find` print `folderContents`/`recordFolders`. | Placements already lived in their own table, so show and find had only to read it. |

## Change → effect → evidence

**Item 1 — an ungranted tool is absent, not refused.**
- **Change.** A firing under the shipped floor carries only what the floor grants. A scripted probe of the built belt prints `find grep jobs ls manual read recall track`, plus `collections` and `shared_context` where folders exist. It does not carry `write`, `edit`, `bash`, `commit`, `ask` or `fork`. The prompt composes from the belt:
  - `ASK_FACTS` without `ask` says "NOBODY IS HERE TO ASK" and gives the ladder without its last rung.
  - `HANDS_FACTS` without `write`/`edit` says "NOTHING HERE CHANGES A FILE". Without `bash` it says "THERE IS NO SHELL HERE".
  - The bash WAITS sentence moved into a belt fact.
  - The prompt-belt law (`prompt_belt_test.go`) gained the firing shape.
- **Effect.** A call to an absent tool answers `Unknown tool: …`, and a finished report publishes. The report-self-write exemption is now checked first, whatever the failure's wording.
- **Evidence.** `TestAnUnattendedRunCarriesOnlyWhatItIsGrantedAndItsReportPublishes`. It FAILS at `84b423d87` with `needs-you … refused in a task: default — nobody to ask` and passes now.
- **Controls.** These pass at both commits:
  - `TestAGrantedToolMeetingItsBoundaryStillWaitsOnThePerson`: the person granted `cat *`, so `rm` still waits.
  - `TestAnAbsentWriteOfItsOwnReportWithNoReportPublishesNothing`.
- **Unit tests.** `TestGrantsIsWhatCanRunWithNobodyToAsk` and `TestTheFolderVerbsReadsAreLooksAndTheirWritesAsk`.
- **Live.**
  - S09c made 3 `read` calls and S09c3 made 1; neither called an absent tool, and none reached a person.
  - Live9: across all ten runs the journals hold 0 calls to an absent tool, 0 `nobody to ask` refusals and 0 confinement refusals. The tools called were `read` 89, `find` 5, `recall` 2, `ls` 2, `grep` 2, `track` 2 and `shared_context` 1. The resumed pass, the step that failed 3 of 10 in wave 3 on `commit`/`bash` refusals, landed in all ten.

**Item 2a — the run is told its placements.**
- **Change.** A condition reaching the work through a folder is marked `[Travel] …`. The section opens `This work is placed in the folders Travel, Launch. A condition marked with a folder reaches this work through that placement and applies to all of it, whatever the work is about: never decide from its wording whether it applies.` A folder above a placement reads `(inside Company)`.
- **Evidence.** `TestAFiringIsToldTheFoldersItsWorkIsPlacedIn` FAILS at `84b423d87` (all four sentences missing). `TestAFolderAboveThePlacementIsNamedAsTheOneItSitsInside` covers the inherited placement.
- **Live.** The system prompt is not journaled, so the scripted regression is the proof that the sentence reaches the run. In live S11 the first draft still omitted both markers; item 2b caught it (below).

**Item 2b — each rule answered by kind, with its citation.**
- **Change.** The checker answers `{"rules":[{"rule":N,"kind","verdict","quote","why"}]}` for every numbered rule.
  - An obligation is `kept` only with a quote the report contains.
  - A prohibition is `broken` only with a quote.
  - A rule no report could show is `not-checkable`.
  - An answer that skips or repeats a rule, uses an unknown kind or verdict, or quotes words the report lacks, is no answer.
  - The correction lists every broken rule, one line each, then `the report says: …` or `the report does not do what it asks`.
- **Record.** `ruleCheck.verdicts` holds `{id, kind, verdict, quote, why}` per rule. The codes are pinned: `obligation`, `prohibition`, `kept`, `broken`, `not-checkable`. `ruleCheck.verdict` is the sum, and any `not-checkable` makes the sum `not-checkable`, never `kept`.
- **Show.** `standing show` prints `checked against 2 rule(s): 1 kept, 1 not checkable`, then `rule <id> “<words>” (obligation): kept — the report says “…”`.
- **Evidence.**
  - `TestAnOmittedObligationIsFoundAndSentBackNotRecordedAsKept` and `TestAnObligationStillOmittedAfterItsCorrectionHoldsTheReport` both FAIL at `84b423d87`: the omission was published as `landed`. They use a truthful scripted checker that answers whichever question it is asked, the way qwen did in S11.
  - New-behaviour tests: `TestEachRuleIsRecordedByIDWithWhatWasChecked`, `TestAnObligationKeptOnWordsTheReportDoesNotHaveIsNoAnswer`, `TestTheRuleCheckAnswerIsReadStrictly`, `TestTheRuleVerdictCodesArePinned`, and `TestStandingShowPrintsEachRulesTruthAndAnOvershoot` (cmd).
- **Live S11.** The first draft had neither marker. The check found both obligations broken ("the report does not do what it asks"), sent the run back once, and the corrected report with both markers published. Both were recorded `kept`, each quoting its marker. **This is the first live evidence of the correction path, which wave 3 could not reach in five attempts (U2).**
- **Live S12b.** MARK-C and MARK-D were present, and MARK-P and MARK-O absent.
- **Live S09c.** "September" was found as a quoted `broken` prohibition and corrected once (next item).

**Item 3 — confinement, folder reads, overshoot.**
- **Change.** `read`, `ls`, `grep`, `find`, `read_document` and `view_image` in a firing resolve their path against the workspace, symlinks included. Anything outside is refused with `<path> is outside this work's project (<workspace>): work that runs while nobody is watching reads only inside the project it was set up in.`, and the run goes on.
- **Evidence.**
  - `TestAnUnattendedReadOutsideItsProjectIsRefusedAndTheRunGoesOn` (5 of 5 outside reads refused, by `..`, an absolute path, a planted symlink, `ls ..` and `find ../`) FAILS at `84b423d87`: the secret was read.
  - `TestAnUnattendedRunMayReadItsFolders` FAILS at `84b423d87`: `needs-you`.
  - `TestARunThatSpentPastItsLimitSaysSoInItsRecord` FAILS at `84b423d87`: no limit on the record.
- **Live S25a.** Tools called: `read deps/available.txt`, `shared_context list` and `collections governing`, all answered (wave 3: refused). It did not roam and spent $0.0008 against a $0.06 cap (wave 3: $0.072 against $0.06). `NO POLICY` is the truthful answer, because the `--once` chat could not create the record: that is the consent law, S25b.
- **Live S25a with the policy present** (`--yolo` chat, as S25c): the exposure was `shared (0c3c…, 1)`, and the report says `NOT ELIGIBLE` citing revision 1, for $0.0009.
- **Live confinement probe S25x.** The brief asked the run to read `../home/v3/config.json` and `ls ../home/v3/standing`. Both were refused with the line above. The run landed and its report lists what it could not read.

**Item 4 — a recursive, bounded watch.**
- **Change.** A whole `**` segment walks the folder above the pattern's first wildcard (`filepath.WalkDir`, still size and time). A pattern without one is read by `filepath.Glob` as before.
- **The limit.** A watch reaching more than `WatchLimit` = 10000 files and folders (L7's figure) is refused at setup and edit with `inbox/** reaches more than 10000 files and folders, and a watch reads every one of them on every pass; watch a narrower pattern`. One that grows past it later stops at the limit and says so on its check line; it never reads a partial world as the truth.
- **The report-in-watch law** reads through the same matcher, so a report deep inside `**/*.md` is refused.
- **Evidence.** Each of these FAILS at `84b423d87`:
  - `TestANestedEditWakesARecursiveWatch` (Fired 0).
  - `TestAWatchPastItsLimitIsRefusedAtSetup` (accepted).
  - `TestAReportDeepInsideARecursiveWatchIsRefused` (admitted).
- **Live S02.** The `inbox/**/*.md` item woke on the nested append to `inbox/clients/acme/thread.md`, listed `modified inbox/clients/acme/thread.md` and published. The `inbox/*` item stays quiet. That is now the documented meaning of `*` (one folder), not a silent miss; the manual says so.

**Item 5 — an identical rewrite is not a change.**
- **Change.** The first tier is still size and time, and a file whose tier did not move carries its last hash unread.
  - A new file, or one whose size or time moved, is hashed (sha256, files up to `HashLimit` = 4 MiB).
  - `differs` compares hashes when both readings have one, and times otherwise.
  - The digest names contents where there is a hash.
  - A manifest under an unchanged digest is rewritten with the fresh times, so the next pass does not hash again.
- **Readings from before hashes** give a new digest with no differing entry. That is read quietly as a new way of reading, not as a change.
- **Evidence.** `TestATouchedOrIdenticallyRewrittenFileIsNotAChange` FAILS at `84b423d87` (Fired 1 on the touch): touch → no run, same text rewritten → no run, same-size different text → run. `TestAReadingKeptBeforeContentsWereHashedIsQuiet` covers the upgrade.
- **Live S17b.** After the regression run, `touch` gave `1 checked`, no run, inbox notes 1 → 1, report bytes unchanged, and spend unchanged at $0.000633.

**Item 6 — a folder shows the work placed in it.**
- **Change.** `Store.Placed(ctx, collectionID)` reads the placements key's prefix. The plan is pinned as `SEARCH placements USING … (collection_id=?)` with no temp sort (L10).
- **Show.** `collections show` lists references, then `Placed here, so this folder's rules reach it:`.
- **Find.** `collections find` lists referencing folders, then `Placed in, so these folders' rules reach it:` with `(placed directly)` or `(placed N folder(s) below)`, the wording `standing show` uses, now from one `placementHow`.
- **JSON.** `--json` is `{"references": […], "placed": […]}` for both (recorded in the change entry).
- **Evidence.** `TestPlacedWorkIsShownInItsFolderUnderItsOwnLabel` FAILS at `84b423d87` (Alpha: "This collection has no references yet."; find: Beta only). `TestPlacedListsOnlyWhatIsPlacedDirectlyInTheFolder` covers the store.
- **Live S27b.** `show Alpha` prints the placed item under its label, and `find` names Beta, then Alpha `(placed directly)`.

## Regressions and the old logic

The proofs ran in `/tmp/opus-w4-old`, detached at `84b423d87`. The session proofs are the committed test files. The standing and cmd proofs are copies trimmed to what exists at `84b423d87`, kept beside the log as `.go.txt`. Receipt: `validation/wave04-old-logic-84b423d87.log`.

| Regression | Old logic at `84b423d87` |
| --- | --- |
| `TestAnUnattendedRunCarriesOnlyWhatItIsGrantedAndItsReportPublishes` | FAIL: `needs-you`, `refused in a task: default — nobody to ask` |
| `TestAGrantedToolMeetingItsBoundaryStillWaitsOnThePerson`, `TestAnAbsentWriteOfItsOwnReportWithNoReportPublishesNothing` | pass at both (controls) |
| `TestAFiringIsToldTheFoldersItsWorkIsPlacedIn` | FAIL: no placement sentence, no folder marks |
| `TestAnOmittedObligationIsFoundAndSentBackNotRecordedAsKept`, `TestAnObligationStillOmittedAfterItsCorrectionHoldsTheReport` | FAIL: the omission published as `landed` |
| `TestAnUnattendedReadOutsideItsProjectIsRefusedAndTheRunGoesOn` | FAIL: another conversation's secret was read |
| `TestAnUnattendedRunMayReadItsFolders` | FAIL: `needs-you` |
| `TestARunThatSpentPastItsLimitSaysSoInItsRecord` | FAIL: the record has no limit |
| `TestANestedEditWakesARecursiveWatch` | FAIL: Fired 0 |
| `TestAWatchPastItsLimitIsRefusedAtSetup` | FAIL: set up |
| `TestATouchedOrIdenticallyRewrittenFileIsNotAChange` | FAIL: Fired 1 on a touch |
| `TestAReportDeepInsideARecursiveWatchIsRefused` | FAIL: admitted |
| `TestPlacedWorkIsShownInItsFolderUnderItsOwnLabel` | FAIL: Alpha "no references yet"; find names only Beta |

New-behaviour tests with no old-logic analogue: the per-rule record and reader tests, `TestAReadingKeptBeforeContentsWereHashedIsQuiet`, `TestPlacedListsOnlyWhatIsPlacedDirectlyInTheFolder`, `TestGrantsIsWhatCanRunWithNobodyToAsk`, `TestTheFolderVerbsReadsAreLooksAndTheirWritesAsk`, `TestAFolderAboveThePlacementIsNamedAsTheOneItSitsInside`, and `TestStandingShowPrintsEachRulesTruthAndAnOvershoot`.

## Live validator reruns

These ran at the lane head's binary (`4701f270d (dirty)`, the uncommitted wave), on `deepseek/deepseek-v4-flash`, one scenario at a time, with the checker `qwen/qwen3.8-27b`. The scripts are the validator's own. Only `BIN`, `MODEL` and the scenario root differ (`validation/wave04-live-scripts.sh.txt`); the logs are `validation/wave04-live-<scenario>.log`.

| Validator | Wave 3 | Wave 4 | Calls | Spend |
| --- | --- | --- | --- | --- |
| S02 nested edit | FAIL: both quiet | **PASS**: `inbox/**/*.md` woke on the nested append and published; `inbox/*` stays one folder (documented) | 2 | $0.000978 |
| S09c rule hold path | FAIL: `bash` refused → unanswerable `needs-you` | **PASS** (the acceptance is "held with `held-report.md`, or an intelligible question"). Only `read` was called. "September" was found and quoted, the run was sent back once, and the correction was kept. Publishing was then held by wave 3's `report-changed` law because the script seeds `out/ops.md` before aforge's first publish; the draft is in `held-report.md` | 7 | $0.008369 |
| S09c3 (no seed) | — | **PASS**: "EDT" was found, the run sent back once, and the corrected lipogram published (0 letters e) | 5 | $0.008336 |
| S11 two placements | FAIL: Travel dropped, check said kept | **PASS**: both markers. The first draft omitted both, the per-rule check found both obligations, and one correction fixed them. Per-rule `kept` with quotes | 5 | $0.010039 |
| S12b descendants | FAIL: MARK-D dropped, check said kept | **PASS**: MARK-C and MARK-D present, MARK-P and MARK-O absent | 3 | $0.002573 |
| S17b touch | FAIL: paid run and a second note | **PASS**: no run, notes 1 → 1, report unchanged | 2 | $0.000633 |
| S25a unattended reach | FAIL: roamed the home, $0.072 of $0.06 | **PASS**: 3 calls (read, `shared_context list`, `collections governing`), no reach outside, `NO POLICY` (true: the record could not be created, S25b) | 6 | $0.003458 |
| S25a with policy | — | **PASS**: exposure `shared (…, 1)`, `NOT ELIGIBLE` citing revision 1 | 4 | $0.000898 |
| S25x confinement probe | — | **PASS**: 2 outside reads refused with the line; landed; the report names what it could not read | 2 | $0.000702 |
| S27b placed from its folder | FAIL: invisible | **PASS** | 0 | $0 |

The ten scenarios cost **$0.036** in total.

## Live9 — ten serial demos (the acceptance protocol)

`scripts/demo-local-work.sh` ran ten times, one after another, at **`b87a4e756`** (`aforge b87a4e756 (dirty)`; the tree differed only by untracked docs) on `deepseek/deepseek-v4-flash`, with the checker `qwen/qwen3.8-27b`. The driver, `validation/wave04-live9.sh.txt`, reads each run's spend from its own usage ledger, so a failed run is counted too. Receipts: `validation/wave04-live9-summary.txt`, ten ANSI-stripped logs `validation/wave04-live9-run01.log` … `run10.log`, and run09's held occurrence, draft and journal verbatim in `validation/wave04-live9-autopsy.jsonl.txt`.

**Result: 9/10, $0.305.**

| Run | Result | Calls | Spend |
| --- | --- | --- | --- |
| run01 | `DEMO PASSED` | 13 | $0.0346 |
| run02 | `DEMO PASSED` | 12 | $0.0220 |
| run03 | `DEMO PASSED` | 12 | $0.0305 |
| run04 | `DEMO PASSED` | 13 | $0.0225 |
| run05 | `DEMO PASSED` | 15 | $0.0271 |
| run06 | `DEMO PASSED` | 14 | $0.0250 |
| run07 | `DEMO PASSED` | 16 | $0.0261 |
| run08 | `DEMO PASSED` | 13 | $0.0424 |
| run09 | **`DEMO FAILED`**, the spec change: check exit 4. The Marketing review was held (`held-by-rules`, `no answer after one correction`) | 16 | $0.0478 |
| run10 | `DEMO PASSED` | 16 | $0.0270 |

**Autopsy of run09.** The model caused it. The harness behaved as designed, and the test's expectation was not met.
- **What happened.** The first report listed three claims, and the third ("in one click") said the spec has no line about it. The Marketing rule reads "cite the spec line behind every finding", so the check correctly found that obligation broken: "Claim 3 states there is no spec line to cite". The run was sent back once. The writer reasoned that it "cannot cite a line that doesn't exist" and kept Claim 3, now worded "no spec line addresses the click count". The recheck then answered the rule `kept` twice, each time with a quote that is not in the report.
- **What aforge did.** An obligation kept on words the report does not have is no answer. That is the guard this wave added against S11's uncited "kept". So the report was held, the draft went to `held-report.md`, the previous review stayed, and the line read `report held back, not published: its check against the rules placed on this work gave no answer: the check answered rule 1 without quoting words that are in the report`.
- **Model.** The writer produced a finding that the person's rule does not allow, and the checker then "kept" it without a verbatim quote.
- **Harness.** No defect in the decision. The report does not keep the rule as written, and nothing was published on an uncited "kept". The record says `no answer`, not `broken`, because the last check gave no readable answer. That is truthful, but it is less specific than the first finding the record also keeps (`first`).
- **Test.** The demo expects the review to land. A report that honestly lists an uncitable claim under a "cite every finding" rule cannot.
- **Not the wave-3 defect.** No tool was refused. The resumed pass landed.

**A superseded run of the same protocol**, on the uncommitted binary (`4701f270d (dirty)`, the same product code), finished 6 of 7 before the lane stopped it to rerun at the commit; run08 was aborted and is not counted ($0.2090, `validation/wave04-live8-superseded-summary.txt`). Its one failure, run05, failed at the **same step and code path**. There the report cited a spec line behind both of its findings, and the checker still answered `kept` twice with a non-verbatim quote. A replay of that check was accepted 2 of 2 (`validation/wave04-live8-run05-replay.log`). **So 2 of 17 finished demos failed on the per-rule reader refusing a checker quote on the Marketing rule, a universally quantified obligation.** Showing "every finding" with one contiguous quote invites the checker to stitch citations together. The live evidence does not say whether to loosen the reader or to let an obligation cite several quotes, because the reader is what stops an uncited "kept". That is a design decision left open (see *Known limits*).

## Validation

`validation/wave04-validate.sh.txt` ran at **`b87a4e756`**, with the tracked tree clean, on Spark (`GOMAXPROCS=4 GOFLAGS=-p=2`), 03:18–03:23 UTC: **11/11 PASS, STATUS 0** (`validation/wave04-validate-b87a4e756.log`).

| Step | Result |
| --- | --- |
| vet (standing, session, cmd, workspace, workspaceview, manual, approval) and vet `-tags e2e` | PASS |
| standing, workspace, workspaceview, manual, approval suites | PASS 9s |
| `internal/session`, the whole package | PASS 211s |
| `cmd/aforge`, the whole package | PASS 48s |
| `internal/e2e` untagged, `make test-laws`, `make changelog-check` | PASS |
| `make build`, `make test-packed-manual` | PASS |
| `TestLocalWorkJourney` (`-tags e2e`, scripted model, real `bin/aforge`) | PASS |

Before this run, one session test was red on the uncommitted wave. `TestBashDescriptionStatesTheArmedBackgroundClock` read the raw prompt template for the bash-waits sentence, which now lives in a belt fact, so it reads the composed prompt (`promptWithBeltFacts`) instead. The manual probes for the six person-visible changes are in `internal/manual/chat_test.go`, and they pass in the packed and unpacked corpus:
- belt absence: "why does a standing order run have no bash or write tool";
- per-rule check: "which rule did the report check say was kept, broken or not checkable";
- confinement: "can an unattended run read files outside its project";
- recursive watch: "does a folder watch see files in subfolders with **";
- no-change quiet: "my watch fired when I only touched a file without changing it";
- placements: "collections show does not list the work placed in the folder";
- the overshoot and placements-told probes.

## Recorded, not fixed

- **S25b — the chat path pins provider lanes OpenRouter rejects for `deepseek/deepseek-v4.1-flash`.** Wave 3's `s25b.log`/`s25c.log`: two `--yolo --once` turns on v4.1-flash died with OpenRouter 404 `0 endpoints out of 1 requested are available matching your guardrail restrictions and data policy … Paid model training violation (account settings): 1 endpoint excluded` on the Fireworks and DeepSeek lanes, and 429 on Io Net. The same turn on `deepseek/deepseek-v4-flash` succeeded at once, and it did again in this wave (S25a with policy: `Record id: 0c3c5e639650c6921d2f100f631fea4a, revision: 1`). This is the router/provider layer, and it is model- and account-specific. It is not touched here.

## Known limits and findings (stated, not hidden)

- **The grant is the approval policy, not the item's `Grant` prose.** `Item.Grant` is free text a person wrote, not a typed per-item authority. So what an unattended run carries is exactly what the person's banked rules allow without asking. A typed per-item grant would be a data-structure decision and was not improvised.
- **Found, not fixed: a firing's hands and parts would run allow-all.** Forked hands (`fork.go:971`) and orchestrated parts (`orchestrate.go:1097`) are built with `Policy{Default: allow}` plus the critical floor. Under the shipped floor this is unreachable, because `fork` is not granted and so is absent. A person whose rules allow `fork` without asking would hand a firing's parts more authority than the firing itself has. This predates the wave. It needs a decision on how a firing's grant descends to its hands.
- **Found, not fixed: tools a connected account appends** (`use_service`) are added after `belt()` and so bypass the grant filter. No connector is used by standing work today.
- **A probe that names an ungranted tool** gets `Unknown tool` from its probe agent, which is honest but not a setup-time refusal.
- **A shell the person granted by pattern is not path-confined.** Confinement covers the reading tools. A granted `bash` pattern is the person's explicit allowance and can reach whatever the pattern admits.
- **The chat `collections` tool's `show`** lists references only; `governing` answers the placement direction there. Only the terminal's `show` prints placements.
- **`**` is only a whole segment.** Inside a segment (`a**b`) it means `*`. Links to folders are not followed.
- **The first reading hashes** every matched regular file up to 4 MiB (at most 10000 entries). Later readings hash only files whose size or time moved. A file over 4 MiB, and a matched folder, are compared by size and time, so an atomic-save editor that renames within a matched folder still moves that folder's time.
- **The per-rule check costs more than the old one.** It is the same call to the same role (`auditor`, `qwen/qwen3.8-27b`); only the question changed. The checker now reasons much longer on the demo's mixed rules ("never quote … ; write [redacted]", "cite … and never edit …"). In wave 3's live8 a check wrote 55–440 output tokens and cost $0.0002–0.0016. In this wave it writes 1.8k–6.6k and costs $0.004–0.015. A demo run went from about $0.009 to $0.02–0.036, most of it the check. Bounding the checker's reasoning is a role/cost decision and was not improvised.
- **Open: a universally quantified obligation against a single-quote reader.** "Cite the spec line behind every finding" is kept only if one contiguous quote shows it, and the checker tends to stitch several places into one quote. In 2 of 17 finished live demos (live9 run09, superseded live8 run05) that ended in `no answer` twice and a held review. Loosening the reader would reopen the S11 hole. The alternatives are a list of quotes for an obligation, or `not-checkable` for "every …" rules. That is a data-structure/product decision, so it was paused, not improvised.
- **The strict quote reader can hold a correct report.** In superseded live8 run05 the checker answered the Marketing rule `kept` twice with a quote that was not verbatim in the report. By design that is no answer, so the report was held (`held-by-rules`, draft in `held-report.md`) and the demo failed. A replay of the same rule, report and prompt on the same checker model was accepted 2 of 2 times, with multi-line verbatim quotes (`validation/wave04-live8-run05-replay.log`). So the cause is the checker's quoting in that one call, most likely a citation stitched from two findings; the log does not keep reply text to prove which. It is not reproduced, and not fixed.
- **The rule check is still a model's reading, not a proof.** In S09c3 it accepted a lipogram written by substituting `3` for `e`, which does keep the rule's letter.
- **The placement sentence is proven by the scripted regression.** It is not visible in a live journal, because firings' system prompts are not journaled.
- The S09c script seeds `out/ops.md` before aforge's first publish, so under wave 3's compare-and-swap its correct report is held as `report-changed`. S09c3 is the same scenario without the seed.
- No tui3 or broad UI/E2E suite was run. There was no install, migration or merge to dev. All compilation and tests ran on Spark (arm64, go1.26.5).
