# Round 3b — the third review's blockers, closed at their seams

The third independent review (`c7ec2566f..4701f270d`, `/home/santosh/work/pai-wave03-refs/agents/review3-out.txt`) asked for changes. It confirmed the narrower fixes: report and turn pairing, conflict detection, the torn tail, run numbering and the fence parser. It did not confirm the stronger claims (L2, F1, L3, L6), and it named five blockers and four missing receipts. This round closes them on branch `codex/personal-e2e-opus`, from the wave-4 lane head `3cd9fe683` (source `b87a4e756`). The source commit is **`1ad80ad3f`**. It is one lane on Spark, with no tui3 run, no broad UI suite and no merge to dev.

## Outcome

- **All five blockers are closed at the seam the review named.** Each fix opens with a regression that fails on the old logic at `3cd9fe683`, run in a throwaway worktree (`/tmp/opus-r3b-old`). Receipt: `validation/round3b-old-logic-3cd9fe683.log`.
- **The four missing receipts are in the tree:** the wordless continuation, the inbox restart after the record, two-process minting, and a failed sync. Each receipt that already held on the old logic is mutation-checked: it fails when the mechanism it proves is removed.
- **The chat door's refusal class is confirmed closed for absent tools.** Wave 4 already fixed it; this round proves it and closes one more path, a connected account's tools armed after the grant filter.
- **One claim is corrected rather than strengthened.** A report replacement is not an atomic compare-and-swap. The manual and this record now state the window that remains.
- **Focused validation: 11/11, STATUS 0** at `1ad80ad3f`. **Live10: 10/10 `DEMO PASSED`, $0.2967** (wave 4: 9/10).

## Blocker → seam → change → proof

**B1 — cancellation bypassed the final fence.**
- **Seam.** `effectFence` in `internal/session/standing_publish.go`, and `standingRunner.fenceFor` in `standing_run.go`.
- **Change.**
  - The fence carries no sampled `live` bool. `effectFence.act(check, effect)` runs under the item's lock and reads, in this order: the stop, the act's own check (the file compare), then `ctx.Err()`, then the effect. The context is read at the moment of the act, after every other check.
  - `effectFence.tell` is the one exemption. It sends the note of a run whose decision was already cut off, because that note is the account of the cut.
  - A store that could not be opened (`fence.unreadable`), or an item lock or document that could not be read, withholds with the new code **`stop-unknown`**. It never falls through to an unfenced act.
  - `heldAtTheNote` is the one place kind and code are made to agree when a note is held:
    - a run already withheld keeps its own reason;
    - a run whose report was placed stays `landed` with no code, and says `the pass was cut off before its note was delivered`, because the receipt is the end of the work (the pass's recovery keeps the same rule);
    - any other clean run becomes `failed · cut-off`.
- **Proof.** Each of these fails at `3cd9fe683`:
  - `TestAPassCancelledBeforeTheFenceWasMadePublishesNothing` (published).
  - `TestACancellationWhileTheFileIsComparedHoldsTheRename`. The report path is a named pipe, so the compare blocks until the test has cancelled the pass. The old logic renamed over it.
  - `TestAnItemLockThatCannotBeTakenWithholdsThePublication` and `TestAStoreThatCannotBeOpenedWithholdsThePublication` (published unfenced).
  - `TestANoteHeldByACancellationIsNotAlsoLanded` (`landed · cut-off`).
  - `TestACutAfterThePublicationKeepsTheLandingAndSaysTheNoteDidNotGo` (`landed · cut-off` beside a receipt).
  - The two Run-level tests use pass contexts that become cancelled at exact moments: inside the item's lock, or once the report exists. That makes the window deterministic.

**B2 — publication was not an atomic compare-and-swap.**
- **Seam.** `publishStandingReport`, `reportFileAt` and `placeReport` in `standing_run.go`.
- **Change.**
  - **(a)** A first publication, where the path was absent when looked at, is placed by `os.Link` from the finished temporary file. Link refuses a taken name, so a file that appeared after the look is kept and the run waits on the person (`report-changed`, draft held). A filesystem that cannot link refuses the first publication rather than falling back to a rename.
  - **(b)** A replacement is read and hashed under the item's lock immediately before the rename. Only `ctx.Err()` sits between them. **The window between the last byte read and the rename is a few microseconds, and an editor's save landing inside it is written over.** No portable call replaces a file only if its contents are unchanged. The manual (`standing-orders`, *I edited the report file — aforge does not write over your changes*) and the change entry now say exactly this, and the wave-3 record's "never written over" is corrected where it stood.
  - **(c)** A held draft that could not be written says so on both hold lines: `the draft could not be kept (…)`.
- **Proof.**
  - `TestAFirstPublicationAndARacingCreateNeverBothSucceed` races an exclusive create against the first publication. At `3cd9fe683`, 4000 iterations clobbered the racing writer's file at iteration 1173. At the new code, 4000 iterations never let both succeed. The committed default is 300 iterations.
  - `TestAFileThatAppearsBeforeTheFirstPlacementIsNotWrittenOver` is the same race made deterministic; it is new-behaviour only.
  - `TestAHeldDraftThatCouldNotBeKeptIsSaidOnTheLine` and `TestARuleHoldWhoseDraftCouldNotBeKeptSaysSo` both fail at `3cd9fe683`.

**B3 — answer drains deleted without a durable acknowledgement or restart dedupe.**
- **Seam.** `drainAnswersInto` and `Agent.drainAnswers` in `answers.go`, and the journal's new `answer` line in `sessionfile.go`.
- **Change.** This is the inbox's L3, applied exactly:
  - An answer carries a `delivery` id, minted by `deliverAnswer`. A line from an older writer is named by the sha256 of its bytes.
  - The drain applies each answer through `ResolveQuestion`, which now returns its error.
  - It writes an `answer` line to the journal (`{delivery, kind, question, picked, from, refused}`) and indexes `answer:<delivery>`, both at write and at replay.
  - It syncs the journal, and only then removes the staged doorstep.
  - A replay skips any delivery the journal holds.
  - A record that cannot be written or synced clears nothing.
  - Resolver refusals are recorded with their reason, and `DrainAnswers` returns them joined.
  - A session with no journal consumes on application, as before, and the comment says so.
- **Limit, stated.** This is not exactly-once. An answer applied and then lost with an unsynced record is applied again in the next life if something is waiting on it there. A lane's own effect is not made durable here.
- **Proof.** Each fails at `3cd9fe683`:
  - `TestAnAnswerWhoseRecordNeverReachedTheDiskIsAppliedAfterTheCrash`. The sync is injected to fail, and the journal is truncated to its pre-drain length to model the power cut. The old logic had already cleared the doorstep.
  - `TestAnAnswerAlreadyRecordedIsNotAppliedAgainAfterACrash` (the old logic applied it twice).
  - `TestARefusedAnswerIsRecordedWithItsReason` (no record).
  - `TestDrainAnswersReturnsTheRefusals` is new-behaviour only.

**B4 — settlement proceeded after a failed fsync.**
- **Seam.** `sessionFile.sync` and `Close` in `sessionfile.go`, `Agent.settleDeliveries` in `agent.go`, `drainStandingInbox` in `standing_run.go`, and `syncDir` in `internal/standing/store.go`.
- **Change.**
  - `sync` returns its error. The first failure is kept for the life of the file (`syncFailed`), because after a failed fsync a later success says nothing about the lines before it.
  - `Close` keeps what its own sync said.
  - `settleDeliveries` settles nothing after a failed sync. The same holds on the attach path, where every staged note is already in memory (`recordIsDurable`).
  - `syncDir` returns I/O errors, so the write fails. `EINVAL`/`ENOTSUP` (a filesystem that cannot sync a folder) are still accepted.
- **Proof.** Each fails at `3cd9fe683`, with only an injection seam added there (diff in the receipt): `TestAFailedJournalSyncStopsTheSettlement`, `TestAnAttachAfterAFailedSyncDoesNotConsumeTheInbox` and `TestAFolderSyncThatFailsIsAFailedWrite`.

**B5 — L6: an ever-growing fold set, scanned on attach.**
- **Seam.** `markFoldPending`, `foldSettled` and `drainStandingInbox` in `standing_run.go`, and `Store.LastPublication` / `KeepPublication` in `internal/standing/occurrence.go`.
- **Change.**
  - A fold's ids leave `foldPending` when the fold settles, or when nobody took it. The set holds only folds whose record has not settled, so `foldIsPendingAny` on attach walks only those.
  - The runner keeps the last receipt per path in `<item>/published.json` the moment a report is placed. `LastPublication` reads that one file. An item whose receipt predates the file is looked for in its newest 64 runs, by number from the run counter, never by listing and reading every record. A receipt older than that is not found, and the publisher then holds rather than writes over.
- **Proof.**
  - `TestASettledFoldLeavesNothingPending` fails at `3cd9fe683` (3 ids left after settling).
  - `TestAnOlderItemsLastPublicationIsLookedForInItsNewestRunsOnly` fails at `3cd9fe683`: the old logic found a receipt 200 runs back.
  - `TestTheLastPublicationIsReadFromItsReceiptNotTheHistory` is new-behaviour only. It uses 200 garbage run records, so only the kept receipt can answer.

## The missing receipts

| Receipt | Test | On the old logic |
| --- | --- | --- |
| Wordless continuation | `TestAReportCutAtTheLimitIsNotRehabilitatedByAWordlessTurn` (Run level) and `TestSilenceAfterAReportKeepsWhateverThatReportsOwnTurnSaid` (unit) | Holds at `3cd9fe683`. Mutation "the last turn's ending decides" publishes the cut report, so both fail. |
| Inbox restart after the record, before the delete | `TestAFoldRecordedBeforeACrashIsNotFoldedAgainAndItsInboxIsConsumed` | Holds at `3cd9fe683`. With the journal dedupe removed, it folds twice. |
| Two-process minting | `TestTwoProcessesMintingRunsNeverShareANumber`: two real processes (the test binary re-executed), 50 runs each, every folder reaped at once | Holds at `3cd9fe683`. With the item lock removed, every number 1..50 is minted twice. |
| fsync failure | the three B4 tests above | Fail at `3cd9fe683`. |

The review's third sequence (a cut report, then a new closed report in a clean turn, then a wordless clean turn) **publishes the new report**. That is the design: the report that stands is one whose own turn ended clean, and silence is not a newer account. The unit receipt pins it.

## The chat door's refusal class, against wave 4

The chat door's ten-run measurement at `2495b6526`, which predates wave 4, lost 3 of 10 runs (01, 02, 08) the same way. The run reached for `bash` (`stat -c '%s %Y'`, `ls -la`, `find -exec stat`, `pwd; ls -la`), was refused `nobody to ask`, and a finished report was parked as `waiting-on-person`.

- `TestAFinishedReportIsNotParkedByTheShellCallsAfterIt` scripts run 01's exact calls **after** a closed report. Under the shipped floor `bash` is not on the belt, each call answers `Unknown tool`, no tool result says `nobody to ask`, and the report publishes. It **fails at `84b423d87`** (before wave 4: a shell on the belt, parked) and **passes at `3cd9fe683`**.
- `TestNoUngrantedCallParksAFinishedReport` calls `bash`, `write`, `edit`, `commit`, `ask` and `fork` after a finished report. It fails at `84b423d87` (`needs-you`) and passes at wave 4.
- **One absent-tool path was still open.** A connected account's family, armed through `use_service`, was added after the grant filter (wave 4's known limit). In a firing, an ungranted family tool would be present and refused, which parks the report again. Now:
  - `Agent.grantedOnly` filters arriving tools at both `use_service` doors, and an all-ungranted family says so;
  - `armFamily` asks the grant again as the backstop.
  - Proof: `TestAToolArmedIntoAnUnattendedRunIsAskedForItsGrantToo` fails at `3cd9fe683` (`[slack_post]` armed).

**The parking path that remains is by design, and it is the owner's call.** A person whose rules allow any shell pattern — one "always" on `git status` is enough — puts `bash` on every unattended belt, since `Grants(bash)` is true when any allow pattern exists. Then `stat` is a granted tool meeting its boundary, refused `nobody to ask`, and it still parks a finished report as `waiting-on-person`. Wave 4 pinned that as a true question (`TestAGrantedToolMeetingItsBoundaryStillWaitsOnThePerson`), and the chat door's A3 (publish a finished report and record the refusal) is a change to a stated law. So it is recorded here, not changed. The chat door's fresh homes bank no shell rule, so its measurement will not show this path. A person's everyday home would.

## Also in this round

- `gofmt` drift from wave 4 in `standing_rules.go` and `prompt_belt_test.go` was reformatted.
- Manual: the `stop-unknown` row; the replacement window; the draft that could not be kept; the answer that survives a crash (`home`); the fold cleared only once saved (`keeping-an-eye`). Probes were added for four questions a person would ask: the replacement window, `stop-unknown`, the draft that could not be kept, and an answer from home after a crash. One held-out question ("what happened while I was away") dropped out of the top four when the first draft of the fold sentence lengthened its section; the sentence was shortened until the question reached the page again (fix the page, never the test).
- A transient cleanup failure (`TestAnAcceptedFolderFamilyRefusesAFolderThatMovedUnderIt`: `TempDir RemoveAll cleanup … directory not empty`) appeared once in a filtered session run. It did not reproduce in 3 filtered reruns or in isolation at either commit. It is unrelated code (task mirror), and it is noted here rather than chased.

## Validation

`validation/round3b-validate.sh.txt` is wave 4's script with only the keep folder renamed. It ran at **`1ad80ad3f`** with the tracked tree clean, on Spark (`GOMAXPROCS=4 GOFLAGS=-p=2`), 05:42–05:48 UTC: **11/11 PASS, STATUS 0** (`validation/round3b-validate-1ad80ad3f.log`).

| Step | Result |
| --- | --- |
| vet (standing, session, cmd, workspace, workspaceview, manual, approval) and vet `-tags e2e` | PASS |
| standing, workspace, workspaceview, manual, approval suites | PASS 10s |
| `internal/session`, the whole package | PASS 229s |
| `cmd/aforge`, the whole package | PASS 48s |
| `internal/e2e` untagged, `make test-laws`, `make changelog-check` | PASS |
| `make build`, `make test-packed-manual` | PASS |
| `TestLocalWorkJourney` (`-tags e2e`, scripted model, real `bin/aforge`: 9 firings, 11 rule checks) | PASS |

The first run of the same script, five minutes earlier, came back STATUS 1 on one step. The session package's own tests all passed, and its checkout guard then failed the package, because this lane wrote untracked record files into the checkout while the suite ran. That is a mistake in how the run was driven, not a product failure. It was rerun with the tree left alone; the first log is kept at `/tmp/opus-r3b/validate-1ad80ad3f-guard.log`.

## Live10 — ten serial demos (the acceptance protocol)

`scripts/demo-local-work.sh` ran ten times, one after another, on **`1ad80ad3f`** (`aforge 1ad80ad3f (dirty)`: the only difference was untracked record files). The model was `deepseek/deepseek-v4-flash` and the checker `qwen/qwen3.8-27b`, from 05:48 to 06:40 UTC. The driver is `validation/round3b-live10.sh.txt`, which is wave 4's with only the names and a $0.45 guard changed. It reads each run's spend from that run's own usage ledger. Receipts: `validation/round3b-live10-summary.txt` and ten ANSI-stripped logs, `validation/round3b-live10-run01.log` … `run10.log`.

**Result: 10/10 `DEMO PASSED`, $0.2967.** Wave 4 got 9/10 ($0.305), and wave 3 got 7/10.

| Run | Result | Calls | Spend |
| --- | --- | --- | --- |
| run01 | `DEMO PASSED` | 15 | $0.0322 |
| run02 | `DEMO PASSED` | 18 | $0.0254 |
| run03 | `DEMO PASSED` | 14 | $0.0245 |
| run04 | `DEMO PASSED` | 12 | $0.0375 |
| run05 | `DEMO PASSED` | 14 | $0.0343 |
| run06 | `DEMO PASSED` | 14 | $0.0304 |
| run07 | `DEMO PASSED` | 14 | $0.0280 |
| run08 | `DEMO PASSED` | 14 | $0.0292 |
| run09 | `DEMO PASSED` | 13 | $0.0252 |
| run10 | `DEMO PASSED` | 12 | $0.0300 |

**Across the 40 firing journals:**
- The runs called `read` 94 times, `ls` 8, `find` 1 and `track` 1.
- There were 0 calls to an absent tool and 0 `nobody to ask` refusals.
- No occurrence carries a `withheld` code.
- The rules check sent a report back in 2 of 40 occurrences, and both corrections published.
- Every item that published keeps its receipt in `published.json` (20 files): the new `LastPublication` path, exercised live.

Wave 4's open finding, the "every finding" obligation against the single-quote reader (live9 run09), did not recur in these ten runs. It is not fixed. Ten clean runs do not show it cannot happen, and it stays an open decision.

## Known limits (stated, not hidden)

- **A replacement can lose an edit saved inside a few-microsecond window** between its compare and its rename, and an editor holding the file open writes its own copy over the report after the rename. The first publication has no such window.
- **A filesystem without hard links refuses a first publication** rather than risking a rename over an unlooked-at file.
- **Answers are not exactly-once** (see B3). An answer's own lane effect is not made durable here.
- **A journal whose sync failed settles nothing for the rest of its life.** Its inbox news and task landings wait for the next open, which folds only what the disk's record does not hold.
- **A legacy item's receipt older than 64 runs is not found.** The next publication then holds (`report-changed`) rather than writes over.
- **The granted-boundary parking path** (above) remains, pending the owner's A3 decision.
- There was no tui3 or broad UI/E2E suite, no install, no migration and no merge to dev. All compilation and tests ran on Spark (arm64, go1.26.5).
