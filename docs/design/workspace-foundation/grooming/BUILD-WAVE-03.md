# Ongoing work over local files, end to end

2026-09-10. Lane `codex/personal-e2e-opus` (Claude Code Opus on Spark), base
`72660bf57`, destination draft #662 through `codex/personal-ai-backend`. The
owner's steering replaced the daily Slack journey with **local files only**: no
Slack, real or simulated, and no connector or account setup.

## Outcome

A person can set up ongoing work from a terminal. The work watches a local
folder and keeps a report current in the project, and the rules placed on its
folder reach it. Before a report is published it is checked against those rules.
Each run records what woke it and what it made. An edit changes the next run.
Pause and stop work as described below, and a killed process neither loses nor
doubles a run. A second item runs the Product → Marketing review on the same
parts.

All of it reuses existing owners:
- the standing item, store, pass, run folders, ledger and log;
- the session runner;
- the collections placements;
- the per-run journal.

This wave adds no new scheduler, no audit store, no connector and no widened
tool authority.

## Try it

```sh
make build
make test-local-work                 # deterministic: scripted loopback model, no key
OPENROUTER_API_KEY=... make demo-local-work     # live: real model, disposable AFORGE_HOME
make test-rules-check-live           # the report check on the real violation, 3 calls
```

`scripts/demo-local-work.sh` has these options:
- `DEMO_DIR=/tmp/x` keeps the home and project in a named place.
- `DEMO_MODEL=<slug>` and `PER_RUN_USD=0.25` choose the model and the per-run limit.
- `SETUP_ONLY=1` seeds the folders, rules and work and takes the baseline without calling a model, so you can drive it yourself:

```sh
export AFORGE_HOME=/tmp/x/home
echo 'Decision: ship Friday' > /tmp/x/project/inbox/today.md
bin/aforge standing check      # 0 all finished · 2 a run did not finish · 4 one waits on you
bin/aforge standing show <id>  # rules reaching it, every run, cause, check, receipt
bin/aforge standing edit <id> --instructions "..."   # version 2 reaches the next run
bin/aforge standing pause|resume|stop <id>
```

The demo is strict. It asserts every step:
- the check's exit status and the run count;
- the newest run's outcome, instructions version and change list;
- that the report on disk matches its sha256 receipt;
- that no raw contact detail from the fixture is in a published report;
- that the copy is unchanged and stop is final;
- that an idle pass rewrites nothing.

The first step that fails prints `DEMO FAILED`, then the item's own account of the run (`standing show`), then exits with status 1. The last good reports are left as they were.

## Change → effect → evidence

| Change | Effect a person sees | Evidence |
| --- | --- | --- |
| `aforge standing add/edit/show/pause/resume/stop/check`; `collections place/unplace` | Ongoing work is set up whole at the terminal (`set up by: person, through the terminal`), without a chat card, and never installs the timer. | `TestLocalWorkJourney`; manual *Set up ongoing work without the chat* |
| `Store.Revise`, instructions version | `revised <id> to version 2: instructions`; the next run uses it; a run under way keeps its version; `--version` fences stale edits; a report-only edit is an edit. | `TestReviseIsFencedOnTheInstructionsVersion`, `TestTheReportPathIsPartOfTheInstructions…`; journey step "edit v2" |
| `occurrence.json` before each task run; journal `parent_cause: standing_occurrence` | `show` says what woke each run, which files changed, attempt, cost, publication and the journal path. | `TestAFiringRecordsItsCauseBeforeItRuns…`; journey `expectCause` |
| Per-file change lists (size and mtime readings; contents are never hashed) | Runs are told `added / modified / removed` per file (size or mtime). A failed or held run's changes are listed again for the next run. | `TestTheRunAfterAFailedOneIsToldWhatTheFailedOneMissed`; journey f.md + g.md |
| Occurrence key carries the run count | A folder that returns to an earlier reading no longer recovers an old record forever (review blocker 1). | `TestAFolderThatReturnsToAnEarlierState…` (fails on the old key) |
| Recovery | A run killed midway is retried as `attempt 2`, naming the run it replaces. A run that finished, or already published, before its process stopped is recorded, not rerun (review issue 4). | `TestAnInterruptedAttempt…`, `TestAFinishedButUnrecorded…`, `TestAnOccurrenceThatPublishedBeforeItsProcessDied…` (fails on old logic); journey SIGKILL step |
| `--report` published by aforge, not by the run | The run gets no wider write authority. The report is written atomically with a sha256 receipt. Containment is checked before folders are made. Only the text between `<report>` lines is published. A report inside its watch, or inside a watched folder, is refused at setup. | `TestAStandingReportIsPublishedInsideItsProjectOnly` (symlinked parent), `TestAReportIsWhatIsBetweenItsLines…`, `TestAReportMustBeTheWorksOwn…` |
| A cut-off or wordless run does not land | `the run was cut off before it finished`; the last good report stays. | `TestARunCutOffMidAnswerPublishesNothing` |
| **Report checked against the rules that reached the run** | The auditor role runs in a fresh context with no tools. A finding must quote the report. The run is sent back once with the quote. If the report still breaks the rule, it is held back: the previous report stays, the draft goes to `held-report.md` and the item waits on the person. Recorded as `ruleCheck`. | `TestAReportThatBreaksARuleIsCorrectedOnce…`, `…StillBreaksARuleIsHeldBack…`, `TestAFindingThatDoesNotQuoteTheReport…`, `TestAReportWhoseCheckGaveNoAnswerIsHeldBack`; journey first report and f.md hold; live `TestRealRulesCheckOnTheLiveViolation` |
| `standing check` exit status | 0 only when every run finished; 2 for a run that did not finish; 4 for one waiting on you, naming `aforge standing show <id>`. | Journey held step asserts exit 4 |
| Stop while a run works | The run is not undone, but its report and note are withheld (`stopped while it ran`). A pause lets it finish and publish. | `TestAStopWhileARunIsWorkingWithholdsItsReportButAPauseDoesNot` |
| Product → Marketing | The review item is placed in Marketing and gets exactly the Marketing rule. A reference in Marketing does not make the inbox work governed. The copy is untouched. | Journey review steps; live demo review step |

## Receipts

Source commits: `63609b5a4`, `3afdba568`, `b427dde07`, `62bd2f9fc`, `178db0eed`,
`f651acd9a`, `d73ce259a`, `9cc7b638c`, `c0de4d8f9` (final review 425), `40cd31e8c` (the second review), and `84b423d87` (the third round); docs and receipts follow in their own commits.

Deterministic, scripted model (not live-model acceptance):
- Final focused validation: `validation/wave03-validate.log` (script `validation/wave03-validate.sh.txt`). **All 11 steps passed, STATUS 0, with 0 skips**, at `7c3312539`: tracked tree clean, source identical to `9cc7b638c`, 2026-09-10 20:49:33–20:51:17 UTC, host `spark`, aarch64, go1.26.5. The journey logged `firings=9 rule checks=11`. It records the revision, host, per-step PASS/FAIL and time for each step:
  - vet;
  - the standing, workspace, workspaceview and manual suites;
  - selected session and cmd tests;
  - the untagged e2e;
  - `make test-laws`, `make changelog-check`, `make build`, `make test-packed-manual`;
  - `TestLocalWorkJourney`.

Real model (`deepseek/deepseek-v4-flash` on OpenRouter), one call at a time:

| Run | Revision | Result | Spend |
| --- | --- | --- | --- |
| live1 | `3afdba568` | Found two defects: pre-tool narration was published, and a 120s pass cut a final answer that was then published as landed. Both fixed in `b427dde07`. | $0.0066 |
| live2 | `b427dde07` | Every step landed, **but report 1 quoted `priya@example.com, +1 555 0100` against the Launch rule.** The journal shows the rule reached the run, so the model did not comply; scope was correct. **Not acceptance.** | $0.0073 |
| live3 | `f651acd9a` | `DEMO PASSED` with strict assertions; the rule check ran on every report (`kept`). Both reports opened with the model's narration in the same turn, which was fixed by the `<report>` delimiter in `d73ce259a`. | $0.0083 |
| rules check | `d73ce259a` | The real model marked the live2 report broken, quoting `priya@example.com, +1 555 0100`. It kept the redacted version, and did not hold back a report on a rule about work that a report cannot show. | <$0.0001 |
| live4 | `d73ce259a` | **The correction worked live.** The first draft quoted `priya@example.com`; the check found it, the run was sent back once, and the published report had no contact details. **`DEMO FAILED` at the Marketing review**, with check exit 4: the model wrote its report, then called `write` on the report path, was refused (unattended), and apologised, so the run waited on the person. Fixed in `9cc7b638c`. | $0.0135 (usage ledger) |
| live5 | `9cc7b638c` | **`DEMO PASSED`**, every strict assertion held: rule check `kept` on all four reports, no contact details, a table after the edit, pause held back and resume caught up, the review cited spec lines, the copy was unchanged, stop was final, and an idle pass rewrote nothing. Both reports were clean, with no narration. Superseded by live6: final review 425 then found two publish-path blockers at this revision. | $0.0079 (13 calls) |
| live6 | `1c7012818` (source `c0de4d8f9`) | **`DEMO PASSED`**, every strict assertion held (`validation/wave03-live6.log`). It also **re-observed the self-write live**: inbox run 0003 called `write` on `reports/inbox-report.md`, was refused, then replied with a closed `<report>` block, and aforge published that block, not the apology in front of it. Both reports were clean. | $0.0114 (13 calls) |
| live7 | `40cd31e8c` | **`DEMO PASSED`**, every strict assertion held (`validation/wave03-live7.log`), after the second review's fixes. All four runs landed with the rule check `kept`, and no contact details were published. No published run carried a `withheld` code. | $0.0134 (12 calls) |
| live8 ×10 | `84b423d87` | **7/10 `DEMO PASSED`**, ten serial runs after the third round. run05, run07 and run08 failed the same way, a defect that predates this round: the resumed pass called a tool the unattended posture does not grant, and its complete report was held as waiting on the person. Table and autopsy under the third round below. | $0.0941 (133 calls) |

Logs, ANSI-stripped, are in `validation/`: `wave03-live2.log` (the violation), `wave03-live3.log`, `wave03-live4.log` (the live correction and the failure), `wave03-live5.log`, `wave03-live6.log`, `wave03-live7.log` (the second review's pass), `wave03-live8-run01.log` … `wave03-live8-run10.log` with `wave03-live8-summary.txt` and `wave03-live8-autopsy.jsonl.txt` (the third round), and `wave03-rules-check-live.log`. Paid usage across the wave, from the usage ledgers and logs, is about $0.163 of the $5 limit: $0.069 for live1–live7 plus the rules check, and $0.0941 for live8. Every run made one call at a time. The model was `deepseek/deepseek-v4-flash` throughout.

What live acceptance does and does not show: live8 ran the whole journey ten times at the final source (`84b423d87`), and it passed 7/10. The three failures are one pre-existing defect, recorded under the third round and in Boundaries, and not fixed here. live7 passed at `40cd31e8c`, and live6 at `c0de4d8f9`. It includes a live refused self-write that still published a closed report. The live rules correction was observed in live4. The publish-decision failures (a limit, an unclosed report, a self-write with no report) are shown by the deterministic regressions below, not live: a real model does not produce them on demand. No run was repeated to get a pass: each rerun followed a fix made for the evidence of the run before.

## Final review 425 — who may publish, and on what evidence

Two blockers were in the publish path at `9cc7b638c`:
1. A refused write of the run's own report, followed by an apology with no report lines, published the apology.
2. A run stopped at its step or spending limit published whatever it had written. The limit's interrupt ends the turn normally, not with an error, so the cut-off guard never saw it. The same held for a report opened and never closed, and for a limit reached during the rules correction, because the check received `capped` by value.

**The shape chosen.** The run's evidence is now one value, `firingEnd` (`internal/session/standing_publish.go`). The run's one event reader fills it: last words, final words, the last report block and whether it was closed, the question for a person, the turn error, saved, limit, and a refused own-report write. One method, `firingEnd.withheld`, answers "may this run publish, and if not, why not" with a named reason: `withheldForAPerson`, `withheldCutOff`, `withheldAtALimit`, `withheldUnclosed`, `withheldSelfWrite` or `withheldNoReport`. The outcome and its person-facing line (`firingEnd.outcome` / `why`) read that answer, the publication requires it to be `notWithheld`, and it is read again after the correction turn.

- A limit withholds even a closed report.
- A closed report stands even beside a refused self-write.
- An unclosed report is no report.
- A run that said nothing and saved nothing comes to nothing, as before.

No new persisted field, outcome kind or user-visible state was added: a withheld run is `failed`, with its reason's line in `outcomeText`. (The second review's pass below adds one field, `withheld`, which the owner approved.)

| Change | Effect | Evidence |
| --- | --- | --- |
| Refused own-report write, no closed report | `failed`: `the run tried to write its report instead of replying with it; the previous report is unchanged`. The write stays refused, and its content is never used. | `TestARefusedWriteOfItsOwnReportWithNoReportLinesPublishesNothing` |
| Step or spending limit | `failed`: `the run reached its step or spending limit before it finished; the previous report is unchanged` | `TestARunStoppedAtItsStepLimitPublishesNothing`, `TestACorrectionStoppedAtTheStepLimitPublishesNothing` |
| Unclosed `<report>` | `failed`: `the run's report was never finished — it has no closing line; …` | `TestAReportOpenedAndNeverClosedIsNotPublished`; `delimitedReport` table (the old "cut before the close" case now expects no report) |
| Closed report beside a refused self-write | Still published (live4's case) | `TestARefusedWriteOfTheRunsOwnReportIsNotAQuestionForThePerson`; live6 run 0003 |
| Recovered-record line | `whether its note was delivered, and what it cost, is not known` | `TestAnOccurrenceThatPublishedBeforeItsProcessDied…` |

**Old logic.** The four new tests were run against `9cc7b638c` in a temporary detached worktree, with only the new test file copied in. All four FAIL there: the apology, the text written after the limit, and half a page were each published (`validation/wave03-publish-old-logic.log`). They pass at `c0de4d8f9`.

**Commands and results at `1c7012818`** (tracked tree clean; source `c0de4d8f9`), Spark, `GOMAXPROCS=4 GOFLAGS=-p=2`:

```sh
bash validation/wave03-validate.sh.txt   # vet, vet -tags e2e, standing/workspace/workspaceview/manual,
                                         # selected session + cmd tests, untagged e2e, make test-laws,
                                         # make changelog-check, make build, make test-packed-manual,
                                         # TestLocalWorkJourney
DEMO_DIR=/tmp/opus-localwork/live6 scripts/demo-local-work.sh
```

11/11 PASS, STATUS 0, 0 skips, 22:20:35–22:21:54 UTC (`validation/wave03-validate-final.log`). live6: `DEMO PASSED`, $0.0114.

## Second review — an empty report and an answer cut at the output limit

The independent review of `b48387207` confirmed the fixes above and returned **NOT DONE** on two new blockers, both in the same publish path:

- **A. An empty closed report overwrote the last good one.** A clean run replying exactly `<report>\n</report>` published an empty file.
- **B. An answer whose output-limit continuations ran out was published.** The loop asks for the rest twice (`truncationContinuations`), then ends the turn like any other. Only a flag on the agent (`lastTurnTruncated`) knew, and the standing reader never read it. The same held for the rules correction turn.

**What this pass changed** (`40cd31e8c`):

- **A is a reason inside the one decision.** `firingEnd.withheld` returns `withheldEmpty` for a closed report whose trimmed body is empty. There is no second gate at the call site.
- **B is read off the event stream.** `EventTurnDone` carries `Truncated`, built by one constructor, `Agent.turnDone`, from the same bit `markTurnTruncated` sets. Every model-loop turn end sends it. The run's reader records the last turn's ending, and `beginCorrection` clears it, so it binds during the correction too. The decision maps it to `withheldOutputLimit`, ranked above a limit and above every report-lines case: an answer cut at the output limit is not an answer, whatever lines it holds. **Why this seam:** it is the smallest honest one. A field on the existing event needs no new `EventKind` (the tui3 and remote consumers cannot be tested in this lane and ignore the field). And because the event is built from the flag, the stream and the flag cannot disagree. `lastTurnTruncated` is now the source of the event and of the orchestrate digest. The standing path reads only the event, so it has no flag of its own that it could miss.
- **The fallback shape is refused, not documented.** The review noted that a `</report>` with no `<report>` line took the whole-answer fallback. That published the narration and the stray tag, so I judged it dishonest: `delimitedReport` now answers `unopenedReport`, and the run fails with `the run's report has a closing line but no opening line`. An answer with neither tag is still published whole, as before.
- **The withheld code is persisted (owner-approved, additive).** `occurrence.json` gains `withheld`, present only on a withheld run of an order that keeps a report. No existing field changes meaning, and older records stay valid. `aforge standing show` prints it on the `came to:` line as `· withheld: <code>`. The twelve codes are one table (`withheldCodes`), pinned exactly by `TestTheWithheldCodesArePinned`: `waiting-on-person`, `cut-off`, `output-limit`, `at-a-limit`, `unclosed-report`, `unopened-report`, `empty-report`, `self-write`, `no-report`, `held-by-rules`, `stopped`, `not-written`.
- **Manual.** It adds a section, *Why wasn't my report published*, with the code table and each line. The self-write exception is qualified: a refused self-write with a finished report still publishes that report. It also adds the probe "standing show says withheld: output-limit, what does that mean".

| Change | Effect | Evidence |
| --- | --- | --- |
| Empty closed report | `failed`: `the run's report was empty; the previous report is unchanged` · `empty-report` | `TestAnEmptyReportBetweenItsLinesIsNotPublished`; parser table |
| Continuations exhausted | `failed`: `the run's answer was cut off at the model's output limit; …` · `output-limit` | `TestAnAnswerCutAtTheOutputLimitIsNotPublished` (three `length` answers, no tags) |
| The same during the correction | as above | `TestACorrectionCutAtTheOutputLimitIsNotPublished` |
| `</report>` with no `<report>` | `failed`: `the run's report has a closing line but no opening line; …` · `unopened-report` | `TestAReportClosedButNeverOpenedIsNotPublished`; parser table |
| `withheld` code | recorded on withheld runs only; shown by `standing show` | `TestAWithheldRunIsRecordedWithItsCode`; the earlier regressions now assert their codes; the journey asserts `held-by-rules` in `--json` and on the `came to:` line, and no code on published runs |

**Old logic.** Each of the four regressions was run at `b48387207` in a throwaway detached worktree, with only a portable test file copied in. **All four FAIL there**: an empty file, two partial answers and the whole answer with its stray tag were each published (`validation/wave03-publish-old-logic-b48387207.log`). They pass at `40cd31e8c`.

**Commands and results at `40cd31e8c`** (tracked tree clean), Spark, `GOMAXPROCS=4 GOFLAGS=-p=2`. The session filter now also names `Publish|Correction|Withheld|Truncat|Digest`, so every regression above runs in it:

```sh
bash validation/wave03-validate.sh.txt
DEMO_DIR=/tmp/opus-localwork/live7 scripts/demo-local-work.sh
```

**11/11 PASS, STATUS 0, 0 skips**, tracked tree clean, 23:00:02–23:01:19 UTC (`validation/wave03-validate-second.log`).

The first run at the same head is kept as a failure (`validation/wave03-validate-second-run1.log`, STATUS 1), and it failed for two reasons:
- **The checkout guard fired because of me.** I edited this file and the old-logic log while the session suite ran, and the guard reported the tracked tree changing underneath it. That was my edit, not a test touching the checkout.
- **Two concurrency tests failed once:** `TestEachHandsReportArrivesWhenThatHandFinishes` (2 hand reports of 3) and `TestAReportThatRacesTheWithdrawalIsNotSwallowed` (the report that lost the race was not queued). Neither reads turn endings or standing code. Neither failure reproduced: I reran 48 times with the tree untouched and all passed. That was 3 runs of the same session filter, 5 in isolation, and 40 more while that filter ran beside them. **Not reproduced, so not diagnosed.** Recorded rather than dismissed, per the rule that a session test failing under load is a bug report.

**live7 at `40cd31e8c`: `DEMO PASSED`, every strict assertion held, 12 calls, $0.0134** on `deepseek/deepseek-v4-flash` (`validation/wave03-live7.log`). All four runs landed. The rule check was `kept` on every report, and no raw contact detail was published. As designed, no published run carries a `withheld` code. A real model does not produce an empty, unopened or output-cut report on demand, so those withheld reasons are shown by the deterministic regressions, not live. The binary reads `(dirty)` only because of untracked files: `.opus-progress.md` and the ignored receipt copies.

## Third round — a report kept with its turn, fenced acts, consume after commit

The round-2 review of `c7ec2566f` judged the four second-review fixes credible and correctly wired, but **not a clean sign-off** (`review2`). Three blockers remained in the same seam. The round also applied the scale audit's laws L1 (fenced writes), L2 (recheck controls where the effect happens) and L3 (consume after commit) there, fixed three audit findings in other files, and repaired two timing-fragile tests. Source: **`84b423d87`**.

**Blocker 1 — a report and the ending of its turn came apart.** A divided run wrote a closed report in a turn cut at the output limit; its parts came home and the resumed turn said only "Acknowledged." The report survived, because the new words had no report lines, and the cut was forgotten, because the new turn ended clean.
- **The shape chosen:** a report and the ending of the turn that wrote it are one value, `turnReport{body, lines, cutAtLimit}`. Likewise the run's last words are `turnWords{reply, final, cutAtLimit}`.
- The run's reader closes each turn with one call, `firingEnd.closeTurn(said, sinceLastTool, cutAtLimit)`, and there is no free-standing truncation flag left in `firingEnd`.
- A report is replaced only by a turn that writes new report lines. So only a new report can stand in for a cut one.
- The decision withholds `output-limit` when the words' own turn was cut, or, for a report order, when the report's own turn was cut.

**Blocker 2 — no-report orders announced incomplete work as landed.** The no-report exemption came before the output-limit and cap checks. Now the exemption sits after them, so owing no report is not itself a failure and that is all it says. A cut, output-limited or capped run of a no-report order comes to `failed` with its code. `withheld` is recorded for any run that did not come back clean. The lines drop "the previous report is unchanged" when there is no report.

**Blocker 3 — the stop/cancellation window (L2 + F1).**
- **The fence.** `effectFence` rechecks the pass's context and the item's stop immediately before each of aforge's own acts: the report's rename and the note's delivery.
  - The stop is read under the item's own flock, through `standing.Store.UnlessStopped`, which is the lock `SetStatus` writes under. A stop therefore lands wholly before or wholly after the act.
  - A cancellation counts only if the decision was taken on a live pass. A run already cut off still delivers the note that says so.
  - Outcomes: `stopped` or `cut-off`. A stop after the write leaves the report and sends no note (`stopped while it ran: its report was published before the stop`).
- **Compare-and-swap.** Before the rename, the target is compared with the sha256 of the item's last publication to that path (`Store.LastPublication`, newest first).
  - It is replaced only if unchanged, or absent.
  - A changed file, or one there before aforge's first publication, is not written over (round 3b corrected this claim: a first publication is now a create that refuses a taken name, and a replacement keeps a microseconds window between its compare and its rename; see BUILD-ROUND-3B.md). The draft goes to `held-report.md` in the run folder, and the run comes to `needs-you` with the new code `report-changed`: `report held back, not published: … Move your copy aside to let the next run publish`.
  - The remaining window is the instant between the compare and the rename. A person's editor takes no lock aforge can share.

**Scale audit.**
- **F2 / L3, consume after commit.**
  - `standing.StageInbox` and `StageProjectInbox` stage the live inbox and re-read every stale `*.draining` file. Notes carry an `id`; one without an id is named by a hash of its line.
  - The session's fold carries one durable delivery per note. That is the existing `durableDelivery` mechanism: the id is journaled with the line and settles only after a successful write.
  - The staged files are removed only when the fold's record settles. Notes the journal already holds (`hasRecorded`) or that this agent has already folded (`foldPending`) are not folded twice.
  - `DrainAnswers(dir, apply)` removes the staged files only after the answers are applied, and re-reads stale files. That is idempotent by question id, because a resolver drops an id nobody waits on.
- **F4 / L4, the journal tail.**
  - `openSessionFile` moves a torn tail to `transcript.jsonl.torn` and truncates to the last newline before anything is appended.
  - The journal syncs once per turn, in `sealTurn`, and before any delivery settles, since settling may consume the inbox the line came from.
  - Standing's `writeAtomic` now syncs the file and the folder. That covers item documents, `occurrence.json`, receipts and the run counter.
- **F7 / L8, run numbering.**
  - Run numbers come from a per-item counter (`last-run`, beside `runs/`), advanced under the item lock and written before the folder is made.
  - The counter is seeded once from the highest existing folder.
  - New folders are six digits (`standing.RunName`), and runs are ordered by number rather than as strings. Old four-digit folders read unchanged.
- **F3 was not done here, on purpose.** The memory tidy pass and the post-turn decider (`internal/session/memory_consolidate.go`, `internal/store/memory.go`) are present only because this branch was cut from dev. This branch never merges to dev, so a fence here would never reach the code the audit is about. It is noted for the dev-side wave: capture `updated_seq` at read and update `WHERE … AND updated_seq = ?`.

**Test repairs (review2 §6).**
- `TestEachHandsReportArrivesWhenThatHandFinishes` now reads the caller's own record, waiting a bounded time for all three reports, instead of the last request it happened to capture.
- `TestAReportThatRacesTheWithdrawalIsNotSwallowed` asserts the conversation's record holds the report. The queue-occupancy assertion is gone.
- A mutation check shows each still fails when the product really loses the report. Removing the conversation fallback, or dropping the slow hand's report, fails the test (`validation/wave03-round3-mutation.log`).
- The unmutated tests passed 30 times in a row, and 25 times more under focused-suite load.

**Non-blocking items.**
- Report delimiters are now whole lines outside fenced code blocks (`markdownFence`), and a leading byte-order mark is ignored. In the live runs, every one of the seventeen closing tags a model wrote was on a line of its own, so a tag glued to other words is read as no closing line.
- The same-turn stickiness after `checkpointReopen` (review2 §1, PLAUSIBLE) is recorded under Boundaries. It only over-withholds, and a correct clear would also have to decide the continuation counter, so it is not an obvious one-liner.
- `TestSessionFileRoundTrip` was already red at `c7ec2566f`. It predates this round: the governing-context wave's `context_exposure` journal lines were never counted, and it is not on the known-red ledger. It surfaced when the session filter widened, and it now counts those lines.

| Regression | Old logic at `c7ec2566f` |
| --- | --- |
| `TestAReportCutAtTheLimitIsNotRehabilitatedByALaterTurn` | FAIL: the cut report was published |
| `TestALaterTurnsNewReportStandsInForOneCutAtTheLimit` | passes at both (positive control) |
| `TestANoReportRunStoppedAtItsStepLimitDidNotLand`, `…CutAtTheOutputLimitDidNotLand` | FAIL: `landed` |
| `TestACleanNoReportRunStillLands` | passes at both (positive control) |
| `TestAStopBetweenTheDecisionAndTheActPublishesNothing`, `TestACancellationBetween…` | FAIL: published after the stop / cancellation |
| `TestAReportThePersonEditedMidRunIsNeverWrittenOver`, `TestAReportFileAforgeNeverWroteIsNotWrittenOver` | FAIL: the person's bytes were replaced |
| `TestInboxNewsOutlivesAWindowThatNeverRecordedIt`, `TestAnInboxStagedBeforeACrashIsReadAtTheNextOpen` | FAIL: consumed before recorded; staged news lost |
| `TestATornJournalTailDoesNotSwallowTheNextLine` | FAIL: the line after the tear was lost |
| `TestRunsPastTenThousandAreStillReadNewestFirst`, `TestARunNumberIsNeverHandedOutTwice` | FAIL: `9999` read as newest; `0003` handed out twice |
| delimiter shapes (BOM, inline mention, fenced tags) | FAIL: four parser cases |

The proof copies use only what exists at `c7ec2566f` (`validation/wave03-publish-old-logic-c7ec2566f.log`, with the proof sources beside it as `.go.txt`).

**Commands and results at `84b423d87`** (tracked tree clean), Spark, `GOMAXPROCS=4 GOFLAGS=-p=2`. The session filter now also names `Hand|Journal|Torn|Session|Question|Mailbox|Fork|Deliver|Settle`:

```sh
bash validation/wave03-validate.sh.txt
bash validation/wave03-live8.sh.txt        # ten serial live demos
```

**11/11 PASS, STATUS 0, 0 skips**, 00:01:50–00:03:50 UTC (`validation/wave03-validate-round3.log`).

**Live acceptance: 7/10.** The local-file demo ran ten times, one after another, at the final source `84b423d87` on `deepseek/deepseek-v4-flash`, one call at a time. The binary reads `84b423d87 (dirty)` because the working tree differed only under `docs/`. Seven runs passed; three failed, all with **one systematic, pre-existing defect, at a 30% rate**. Receipts: the driver `validation/wave03-live8.sh.txt`, its summary `validation/wave03-live8-summary.txt`, ten ANSI-stripped logs `validation/wave03-live8-run01.log` … `run10.log`, and the three failing runs' `occurrence.json` and journal verbatim in `validation/wave03-live8-autopsy.jsonl.txt`.

| Run | Result | Calls | Spend |
| --- | --- | --- | --- |
| run01 | `DEMO PASSED` | 12 | $0.0095 |
| run02 | `DEMO PASSED` | 13 | $0.0116 |
| run03 | `DEMO PASSED` | 13 | $0.0061 |
| run04 | `DEMO PASSED` | 13 | $0.0086 |
| run05 | `DEMO FAILED`, the resumed pass: check exit 4. Inbox run `000003` called `commit` on its own belief `b1` | 12 | $0.0071 |
| run06 | `DEMO PASSED` | 14 | $0.0203 |
| run07 | `DEMO FAILED`, the resumed pass: check exit 4. Inbox run `000003` called `commit` on its own subgoal `p1` | 11 | $0.0063 |
| run08 | `DEMO FAILED`, the resumed pass: check exit 4. Inbox run `000003` called `bash` four times to find the inbox folder | 18 | $0.0087 |
| run09 | `DEMO PASSED` | 14 | $0.0063 |
| run10 | `DEMO PASSED` | 13 | $0.0096 |

Spend is $0.0941 over the ten runs. The summary's `total=$0.072` counts only the passing runs, because a failed demo prints no spend line. The three failed runs' figures come from their homes' usage ledgers.

**Autopsy, the same for all three: harness, pre-existing, set off by the model's choice of tool.**
- **What happened.** In each run the failing firing was the resumed pass (instructions version 2, run `000003`). It read the inbox files and the previous report, then called a tool the unattended posture does not grant:
  - `commit` in run05 and run07. The shipped allowances in `cmd/aforge/chatv3.go` (`v3BuiltinApprovals`) deliberately leave it out, because it is the one working-state tool that declares work finished.
  - `bash` in run08, after the brief said not to run shell commands.
- **What aforge did.** Each call was refused with `refused in a task: default — nobody to ask`. The model then wrote a **complete, closed `<report>`** anyway. aforge's reader (`standingRefusal` → `firingEnd.needs`) reads any "nobody to ask" refusal as a question for the person. So the run came to `needs-you` with `waiting-on-person`, the good report was withheld, and the demo's resume step failed on exit 4.
- **Not the model.** The refused calls were optional, and the model recovered from each one.
- **Not the test.** The demo is right to refuse a resume that did not land.
- **Not this round.** `standingRefusal`, its `end.needs` path, and `commit`'s absence from the allowances are unchanged since `c7ec2566f`.
- **Disposition: wave-04 item 1, not fixed in this round.** It follows the owner's Q3 ruling: a tool the unattended posture does not grant is absent from an unattended run's belt, not present and refused. Validator finding S09c is the same class. The earlier exemption for a refused write of the run's own report (`ownReportWrite`) is one case of this class.

## Boundaries (stated, not hidden)

- **The rule check is a model's reading, not a proof.** It can miss a breach or see one that is not there. It covers the report aforge publishes, and only when rules reached the run. It does not cover what the run did with its tools, the text of a note, or work that has no report. General semantic enforcement of rules is not claimed.
- **One correction, then the person.** There is no resampling until green. A held report is `needs-you`, and it is not retried by itself.
- **A failed run is not retried automatically.** The next change starts a new run, and that run is told about the failed run's changes.
- **Stop mid-run withholds only aforge's own last acts**, the report and the note. A tool effect that has already started is not cancelled or rolled back. A pause does not affect a run that was already admitted.
- **An unattended run can read but cannot write or run shell commands**, because they need approval. That is why aforge publishes the report.
- **Why a run did not come back clean is a code as well as a line** (`withheld` in `occurrence.json`), for report and no-report orders alike. A clean run carries none, and a run that said nothing and saved nothing came to nothing and carries none.
- **The stop/cancellation window is closed (third round).** Each of aforge's own acts rechecks both at the act, the stop under the item's lock. What remains is the instant between the report-file compare and its rename, which a person's editor can still hit, because an editor takes no lock aforge can share.
- **Known defect, systematic, 3 of 10 live runs: a refused optional tool holds a finished report.** When an unattended run calls a tool its posture does not grant (`commit` on its own working state, or `bash`), the refusal reads `nobody to ask`. aforge counts that refusal as a question for the person, so a run that went on to write a complete report comes to `needs-you` and publishes nothing. This defect predates the round. It is wave-04 item 1: under the owner's Q3 ruling, an ungranted tool is absent from the unattended belt rather than refused, and validator finding S09c is the same class.
- **Known: a report file aforge did not write blocks publication until it is moved aside.** That covers a file that existed before the first publication, and one a person changed. The run waits on the person with its draft held. There is no command to adopt the person's copy; moving or deleting it is the way.
- **Known: same-turn truncation stickiness.** When `checkpointReopen` continues a turn whose answer ran out of continuations, a later complete answer in that same turn does not clear the mark (`loop.go` around the truncation count). That only withholds more often, never less.
- **Known: a project inbox fold can repeat across two conversations.** If one conversation recorded the fold and was killed before removing the files, a different conversation of the same project folds it again. The dedupe is per journal. A duplicate, never a loss.
- Change detection uses size and modification time, not contents.
- Terminal-made items do not install the background timer. They are checked by an open window, by a timer that is already on, or by `aforge standing check`.
- T12d is only partial. Standing task runs now carry their causal parent. Tasks, forks and other executions still record `not_recorded`, and there is no global event graph.
- The chat-card setup path was not re-exercised in this wave. No tui3 suite or broad UI/E2E suite was run (per the constraints). There was no install, migration or merge into dev.

All compilation and tests ran on Spark (arm64, go1.26.5, `GOMAXPROCS=4 GOFLAGS=-p=2`).
