FIX-2597: real pending (fixed pending, PR dropped: none); already red pending; flaky pending; packages cmd/codeaf pending session pending; laws pending; hand pending

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
Fix commit: pending focused verification and local commit.

Focused green: shared slow-failure reuse, both pairing/solo transition tests,
custody restart/move, existing-profile sync posture (paired and solo), and
fresh-profile quiet behavior all pass in `shared-custody-green.log`.

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
