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
`f651acd9a`, `d73ce259a`, `9cc7b638c`, `c0de4d8f9` (final review 425), and `40cd31e8c` (the second review); docs and receipts follow in their own commits.

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

Logs, ANSI-stripped, are in `validation/`: `wave03-live2.log` (the violation), `wave03-live3.log`, `wave03-live4.log` (the live correction and the failure), `wave03-live5.log`, `wave03-live6.log`, `wave03-live7.log` (the final pass) and `wave03-rules-check-live.log`. Paid usage across the wave, from the usage ledgers and logs, is about $0.069 (live1–live7 plus the rules check) of the $5 limit, with one call at a time. The model was `deepseek/deepseek-v4-flash` throughout.

What live acceptance does and does not show: live7 shows the whole journey on a real model at the final source (`40cd31e8c`); live6 did the same at `c0de4d8f9`. It includes a live refused self-write that still published a closed report. The live rules correction was observed in live4. The publish-decision failures (a limit, an unclosed report, a self-write with no report) are shown by the deterministic regressions below, not live: a real model does not produce them on demand. No run was repeated to get a pass: each rerun followed a fix made for the evidence of the run before.

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

## Boundaries (stated, not hidden)

- **The rule check is a model's reading, not a proof.** It can miss a breach or see one that is not there. It covers the report aforge publishes, and only when rules reached the run. It does not cover what the run did with its tools, the text of a note, or work that has no report. General semantic enforcement of rules is not claimed.
- **One correction, then the person.** There is no resampling until green. A held report is `needs-you`, and it is not retried by itself.
- **A failed run is not retried automatically.** The next change starts a new run, and that run is told about the failed run's changes.
- **Stop mid-run withholds only aforge's own last acts**, the report and the note. A tool effect that has already started is not cancelled or rolled back. A pause does not affect a run that was already admitted.
- **An unattended run can read but cannot write or run shell commands**, because they need approval. That is why aforge publishes the report.
- **Why a run did not publish is a code as well as a line** (`withheld` in `occurrence.json`). Only an order that keeps a report carries one, and a run that said nothing and saved nothing came to nothing and carries none.
- **Known limitation: a stop or deadline landing after the last check can still publish.** The run's context is read for the last time at `internal/session/standing_run.go:772` (`:804` after a correction), and the item's status at `:831`. The report is then written at `:844` by `publishStandingReport` (`:1051`), which takes no context. A cancellation arriving in that window does not stop the write. The window is a single local file write, and it predates this wave's fixes. Recorded by the second review as PLAUSIBLE; not fixed here.
- Change detection uses size and modification time, not contents.
- Terminal-made items do not install the background timer. They are checked by an open window, by a timer that is already on, or by `aforge standing check`.
- T12d is only partial. Standing task runs now carry their causal parent. Tasks, forks and other executions still record `not_recorded`, and there is no global event graph.
- The chat-card setup path was not re-exercised in this wave. No tui3 suite or broad UI/E2E suite was run (per the constraints). There was no install, migration or merge into dev.

All compilation and tests ran on Spark (arm64, go1.26.5, `GOMAXPROCS=4 GOFLAGS=-p=2`).
