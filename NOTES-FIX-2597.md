FIX-2597: real 16 (fixed 16, PR dropped: none); already red 1; flaky 3; packages cmd/codeaf pending session pending; laws pending; hand pending

Working record for SPEC-FIX-2597.md. Local commits only on
`land/app-v3-open-prs`; no push, GitHub actions, or Git configuration changes.

## Starting evidence

- Starting head: `980918d1d`; frozen comparison `origin/dev`: `86a2069f6`.
- Read `/home/abir/af-integrate/NOTES-INTEGRATE.md`, the contributor guide,
  architecture map, documentation ownership, and repository instructions.
- `/tmp/integrate/final/ci-failing.txt` contains 18 top-level `cmd/codeaf`
  tests and no `internal/session` failures. Each listed test will run alone
  here and on the frozen base before classification. No unlisted session
  failures will be invented.
- The specified base worktree did not exist; created a detached worktree at
  `/home/abir/af-base-dev-now` at `86a2069f6`. Its source will remain unchanged.
- Go invocations use `nice -n 10`, `GOFLAGS=-p=3`, `CODEAF_TELEMETRY=off`,
  `TMPDIR=/tmp/integrate/fix`, and the default shared caches. Test logs and
  diagnostic scripts live outside worktrees under `/tmp/integrate/fix/`.
- Existing unrelated jobs and tmux servers are left alone.
- Scratch bisects use a detached worktree under `/tmp/integrate/fix/bisect`;
  they do not move the requested contribution branch or create other commits.

## Reproduction and classification

In progress. The CI input and initial branch/base outcomes are retained with
the scratch evidence. Existing assertions will remain unchanged.

Initial classification: 14 wave regressions (including two wave-added tests),
one already-red startup/upload test, and three flaky tests. Every listed name
was invoked alone on each tree. Prepared-base reruns supersede engine skips.
The two missing base tests remain explicitly identified below.

| CI-listed test | Initial branch | Prepared base | Classification |
| --- | --- | --- | --- |
| TestARefusedRunThatWroteNothingNamesTheDirectory | cleanup failure, isolated rerun passes | pass | flaky |
| TestFactoryStageCallsPRBaseSyncThroughTheRealToolBelt | folder refusal | absent | real, wave-added fixture |
| TestFinalPublishDisplacementReachesTheRestoredWindowOnce | writer never started | pass | real |
| TestLiveDriveRefusedStartupRetriesAndUploads | initial notice absent | recovery/upload failures | already red, separate failure signatures |
| TestNoticeReplayIsCurrent | stale storage-full notice | pass | real |
| TestProcessCloseJoinsTheUsageWriter | pass | pass | flaky |
| TestSharedDriveNoticesEachWindow | no notices | pass | real |
| TestSharedLocalToolsExpireOwnershipWhilePongsKeepArriving | no live feed | pass | real |
| TestSharedLocalToolsReuseASlowFailedReading | zero reads | pass | real |
| TestSharedLocalToolsShareOneConcurrentDirectoryReading | zero reads/no feed | pass | real |
| TestStorageNoticeReachesEngineWindow | absent notices/writer | pass | real |
| TestStorageOffChatKeepsSendingAfterItsOwnRecordedHeadHoldExpires | quiet profile | pass | real |
| TestStorageOffChatWithMissingSavedTurnsDoesNotReturnQuietly | quiet profile | pass | real |
| TestStorageOffContinuedChatSendsAfterItsOwnEmptyHeadHoldExpires | quiet profile | pass | real |
| TestStorageOffSeatedTurnStopsWhenAnotherComputerContinuesIt | misses displacement | pass | real |
| TestStorageOnChatWithDifferentHeadsStillAsksToContinue | quiet profile | pass | real |
| TestTheNextLookSeesEveryChange | stale full-read refusal | absent; old-watcher comparison passes | real, wave-added compatibility test |
| TestTheSameSentenceIsInTheJSONAnswer | pass | pass | flaky |

Raw results: `initial-results.json`, corrected engine results in
`base-prepared-results.json`, and individual `initial-*.log` files under the
scratch root. One driver pause while preparing base is included in the recorded
wall time of the next branch invocation; the test log retains its actual time.

Preparation correction: the first fresh-base engine cases skipped because its
ignored furrow artifact was absent. Ran the prescribed `make furrow` preparation
on that worktree; rerunning those cases into `initial-base-prepared-*.log`.
Skipped executions are not counted as passing behavior. The listed
`TestFactoryStageCallsPRBaseSyncThroughTheRealToolBelt` does not exist on base;
its initial base command explicitly reported no tests, not a behavioral pass.

## Cause isolation and repairs

Pending paired reproductions and `git bisect run` evidence.

### Shared notices and ownership: #2090

Authoritative `git bisect run` on the paired passing/failing shared-gate probe
identifies `c25dbd3b3` (#2090), encrypted custody, as the first bad commit.
`61d259d1b` has an intermediate stamp-type compilation failure and was skipped;
the parent of #2090 passes and #2090 itself gives the exact zero-read failure.
Evidence: `bisect-sharing-gate.txt`, `bisect-sharing-gate-git.log`, and per-cut
logs. The earlier startup/upload bisect remains rejected.

Repair: use the existing `spokenDoorChatWithPairing(t, true)` in fixtures whose
premise is an already-paired profile, before their first metadata save. Preserve
the two deliberate solo fixtures, every existing assertion, and production
`EnsureSolo`/custody behavior. No refusal expectation or ownership check changes.
Fix commit: `09dbdb2fb` — pair shared fixtures before #2090 custody saves a
solo identity.

Focused green: shared slow-failure reuse, both pairing/solo transition tests,
custody restart/move, existing-profile sync posture (paired and solo), and
fresh-profile quiet behavior all pass in `shared-custody-green.log`.

### Stop-watch/full-admission handover: #2358

The carried `TestTheNextLookSeesEveryChange` fails twice on the unchanged wave
(`early-stop-branch.log` and its initial individual run), at the handover's full
read. `cmd/codeaf/factory_person_stop.go`, `floorJobFiles`, reuses a negative
memo for 100 ms even after an atomic job rename changes the folder. The new
`WatchedRefusal` from #2358 uses the current `TalkStop` index, so it sees the
handover while the admission path can still allow it. The old suite advances
`floorNow` between edits; the carried test does not.

Added `TestAutomaticRefusalSeesHandoverBeforeNextFloorInterval`, which fixes the
clock through a real store handover and asserts both watcher and admission
answers. It fails before the repair in 0.28 seconds (`handover-red.log`). This
removes machine-speed dependence from reproduction without changing an existing
assertion. Bisect of the carried test and repair validation are in progress.

Repair under test: retain the floor folder's identity with a shared look and
check it before reusing that look within 100 ms. An atomic rename invalidates
the negative memo immediately; unchanged job documents still are not decoded.
The pre-fix unmanaged-admission benchmark (100 iterations) is 144 B/op and one
allocation/op; elapsed timings are diagnostic, not acceptance gates. Benchmark
source and overlay are external scratch artifacts, not repository tests.

Green proof: the deterministic handover, both existing next-look cases, unchanged
manager checks, unrelated floor churn, idle watcher zero-document work, exactly
one document after one job edit, and allocation growth limits pass together in
`handover-factory-green.log`. Unmanaged admission now costs 432 B/op and three
allocations/op (the additional directory stat), still within existing unchanged
allocation caps. No cap or existing assertion was changed. Fix commit:
`e8fc2e3a0` — invalidate #2358 handover look before stale admission.

### Factory PR-base belt fixture: #2570

`git bisect run` narrows the new test failure to `97bdc7207` (#2570) or its
integration build repair `dc6bbde8d`. The former does not compile until the
latter supplies the retained `updateAt` argument, so Git reports that exact
two-commit ambiguity. The test and its nested profile are introduced by
`97bdc7207`; the second commit changes only the lease recovery call signature.
This is attribution to #2570, not to the unrelated guard it trips.

Cause: `cmd/codeaf/factory_stage_test.go`,
`TestFactoryStageCallsPRBaseSyncThroughTheRealToolBelt`, makes its state home
`root/state` while requesting `root` as a Factory workspace. Existing
`factoryFolderRefusal` correctly refuses an ancestor of the profile.
Repair under test: give the profile a separate TempDir. All tool-call, repair
reason, and error assertions remain byte-for-byte unchanged.

Green proof: the exact real-tool-belt test passes in
`handover-factory-green.log` (2.00 seconds). Fix commit: `0d20225dd` — keep
#2570 belt fixture outside its profile ancestor.

The unpinned stop-test bisect is also rejected: changing I/O speed let older
cuts spend more than the 100 ms cache interval and pass, falsely blaming the
unrelated pinned-Go verification fix `306a14c84`. Restarted with the same
carried assertions and a scratch-only overlay that freezes its existing
`floorNow` seam. This forces the handover to remain inside one shared cache
interval on every cut. No repository test assertion is altered. The new
deterministic repository regression uses that same controlled clock.

The controlled `git bisect run` identifies `21ebed37c` (#2358) as the first bad
cut (`bisect-stop-clock.txt`, `bisect-stop-clock-git.log`). This locates the
introduced watcher/full-admission interaction, not an unrelated later speed
change. Pre-introduction cuts have no such test and are explicitly marked as
absent rather than described as behavioral passes.

Evidence so far (before any edits):

- `TestProcessCloseJoinsTheUsageWriter` passes on both trees in isolation;
  classify flaky and leave its assertions and implementation alone.
- `TestARefusedRunThatWroteNothingNamesTheDirectory` initially failed only
  TempDir teardown, then passed its isolated rerun without an edit. Base passes.
  Treat that observed cleanup race as flaky; do not change its assertions or
  invent a deterministic production regression from the first cleanup result.
- Prepared base passes final-publish notices, notice replay, shared-drive
  notices, expiry while pongs arrive, and sharing one slow failed reading.
  This branch fails those same contracts.
- Prepared base's startup-retry test fails later, during recovery/upload and
  request deadlines, while this branch misses the initial refusal notice.
  Preserve both failure signatures: the initial startup test selection is
  unsuitable as an unqualified bisect predicate because it also catches the
  base-red upload path. Its preliminary bisect is diagnostic only; a successful
  paired shared-gate test will supply the authoritative family predicate.
  The rejected startup bisect blamed `19c0ecd66`, a two-line rollup-only change,
  on upload failures rather than missing notices; it is explicitly not causal
  evidence. Retained as `bisect-startup-rejected*`. Restarted with
  `TestSharedLocalToolsReuseASlowFailedReading`, accepting only the observed
  zero-read failure as bad and skipping other failures.
- Custody protection in `internal/session/agent_identity.go`,
  `protectAgentIdentity` (#2090), calls `identity.EnsureSolo` on first save.
  Existing sharing fixtures save a turn before calling `identity.Ensure`,
  which deliberately preserves an existing solo marker. Their intended paired
  premise is therefore never established. Existing solo/quiet assertions must
  remain intact; shared fixtures can explicitly establish pairing before save.
- The exact stop-watch test is wave-added (#2358), absent on base. An external
  overlay runs its unchanged assertions on base through the old watcher road
  (`AutomaticRefusal` in both positions); that comparison passes. It took
  102.45 seconds under host I/O pressure, so this is compatibility evidence,
  not proof that a missing base test executed verbatim.
- On the wave, the stop-watch failure is the full-read negative memo reusing
  `floorJobFiles` for 100 ms after a job gains the conversation; the new cheap
  watcher sees the edit immediately. Investigating the interaction and bisect.
- The Factory PR-base test is wave-added (#2570), absent on base. Its workspace
  contains its profile (`root/state`), which violates dev's existing factory
  folder guard before the tool belt can run. The assertion itself is sound;
  the profile must be outside the workspace.

## Final validation

Pending fixes: all CI-listed tests individually, each complete cmd/session
package once (20-minute ceiling), laws, manual gates, build, and the specified
throwaway-profile live hand checks.

Final validation is running on `0d20225dd`, with the final binary built first
so binary-driving package tests use the fixed source. Build passes. A private
driver invokes each listed test separately and then the two complete package
targets with four shards and `TEST_TIMEOUT=20m`, followed by laws and both manual
paths. Logs and structured results are `final-*.log`, `final-results.json`, and
the package report JSON files. No full package has been used as an editing loop.

All 18 final individual CI selections are complete: 17 pass, including all 14
regressions and all three flaky selections. The sole red is the already-red
`TestLiveDriveRefusedStartupRetriesAndUploads`: initial notices now pass, but
`relay_storage_full0` reaches the same later pending-upload failure observed on
base (calls=9, frames=1). No upload assertion or underlying base-red behavior was
changed. The complete `cmd/codeaf` run has started under the per-machine lock.

Full-package raw JSON streams are also archived as `full-cmd-shard-*.jsonl`
(and the corresponding session files) because the standard timing report keeps
outcomes but not assertion text. A read-only follower copies each existing shard
stream until its temporary directory is removed; it changes no test command.

The cmd package's four shards reached their 20-minute ceiling (1,210.93 seconds
including setup). Exactly 3,129/3,507 top-level tests started and reported an
outcome; 378 never started. The initial package run is red, not a complete green
result. Session has started once under the same lock and ceiling.

To fulfill whole-package coverage without repeating completed tests, the
remaining never-started names will run as explicit, disjoint continuation
selections after the initial package runs finish. They use the same repository
shard runner, per-machine lock, four shards, 20-minute ceiling, and final source.
The expected lists, starts, and continuation selections are saved externally;
already-started tests are excluded. Actual reds still get only the required
isolated rerun and frozen-base comparison.

### New real family from the full session run: journal bubble ownership

The initial complete session selection aborted two shards in Memory tests with
`fatal error: send on synctest channel from outside bubble`. Both
`TestMemoryRecoveryKeepsOriginalExchangeOnce` and
`TestClosedSessionHasNoMemoryPassStillWritingTheStore` reproduce alone here
(0.268/0.753 seconds) and pass alone on the frozen base (0.288/0.825 seconds).
Their original assertions are unchanged. These add two confirmed real cases
beyond the initial CI list. Raw paired outcomes are already saved in
`triage-results.json`; they will not be redundantly rerun by later triage.

Cause under investigation: #2387's `usagePending.append` creates its batch
synchronizer channels in the recording caller. Dev deliberately starts the
process-wide `usageWriter` outside fake-clock bubbles. The journal's new
`usageWriter.run` cleanup then sends to a bubble-owned wake channel from that
outside writer. A fix must keep the journal's independent batching (including
when projection is blocked), crash recovery, and bounded joined shutdown.

The automatic continuation/triage follower was stopped while waiting, by its
exact owned PID, so it cannot start tests while this newly confirmed family is
being repaired. No unrelated process was signalled. The two initial package
runs and initial laws/manual gates have finished; both gates pass.

`git bisect run` identifies `4fadc9452` (the #2387 projection-durability follow-up)
as the first bad cut for the existing Memory recovery test. Removing projection
fsyncs exposed the caller-owned batch synchronizer before its temporary scope
had drained. Evidence: `bisect-usage-bubble.txt` and its Git log.

Added `TestJournalSyncStaysWithTheProcessWriterAcrossCallerScopes`. It starts
the process writer outside a caller bubble, records and joins inside that scope,
and checks one unchanged charge afterward. The exact test passes on the base
through an external overlay, and fails with the fatal cross-bubble send on the
original wave production files (`journal-scope-base.log`, `journal-scope-red.log`).
Existing Memory and journal assertions remain unchanged.

Repair under verification: the shared writer owns a joined, idle journal loop.
Recording schedules it without blocking; batch synchronizers are created only
from that owner. The loop remains separate from ledger projection, so a stalled
projection cannot stop journal syncing. Shutdown joins the journal before replay.
The 100 ms/64-row batching constants and all existing shutdown ceilings remain.

The first focused journal run passed its assertions, including subprocess
compaction, but exited red administratively because I added the regression file
while that command was still active. The checkout-change guard correctly caught
that. Its result is not claimed as a green command; verification will be repeated
with the checkout held still. This was an agent workflow mistake, not a test or
production assertion failure.

The stable repeat passes the journal/Memory assertions and the public 20,000-row
benchmark (exact $200, no drops). Its cmd process-close assertion catches the
new helper being counted via its `created by usageWriter.run` stack line.
Launching both owned loops from `usageWriterForLocked` keeps the process's one
writer-per-ledger observation intact; the existing assertion is not changed.
The scoped lifetime and process join checks are being rerun after that fix.

The constructor-launch repeat passes both packages' focused checks, including
the unchanged process writer-count and close assertions
(`journal-owner-green-joined.log`). The final coalesced-wake variant also passes
all journal, Memory, scope, and process-close assertions; one unrelated flush
fixture exceeded its two-second bound during I/O contention, then passed
unchanged in isolation in 0.15 seconds (`journal-owner-final.log`,
`journal-durable-isolated.log`). No deadline or expectation was relaxed.

Fresh-process before/after `BenchmarkLedgerWriter20K` invocations both verify
20,000 public rows, exact $200 total, and no drops. Allocation measurements per
20,000-charge iteration: before 91,502,848 bytes / 724,209 allocations; after
95,270,832 bytes / 733,473 allocations. Elapsed times are diagnostic. The
additional owned loop is idle without a timer and wakes only when a new sync
burst is needed; existing batching, reader, and shutdown caps are unchanged.
Evidence: `journal-perf-before.log`, `journal-perf-after.log`.

The remaining original full-run failure comparisons will use the detached
scratch tree frozen at the full-run source `0d20225dd`; continuation failures
will use their newer repaired source. This avoids relabeling an old failure as
flaky merely because the new journal repair makes it pass. No comparison source
will be edited, and the two already-completed Memory pairs will not be repeated.
