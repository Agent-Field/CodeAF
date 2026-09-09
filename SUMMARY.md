# Test-speed follow-up status

## Pass 1

- Baseline retained: tui3 563.224s, session 210.453s, remote 21.111s,
  enginehost 13.249s, cmd/aforge 46.852s; uncached tests, warm/unspecified build
  cache, `GOMAXPROCS=4 GOFLAGS=-p=2`.
- Implemented focused, quick-feedback, and JSON-report targets on the existing
  `make test` `PKGS`/`TEST_FLAGS` contract.
- Added strict report parsing tests for cache labels, incomplete packages,
  malformed input, and empty input.
- No sharding or blanket test parallelism: current data points first to harness
  waiting, and the shared-state audit is not complete.

## Pass 1 checkpoint

- Draft PR #658 is open against the owner-overridden target
  `codex/conversation-execution`; commits `cac54a7d2` and `6a6d1beae` are pushed.
- The real-number change entry validates. Developer instructions now distinguish
  focused/cacheable iteration, fresh affected-package runs, the non-acceptance
  quick gate, and fresh-test/warm-build-cache timing reports.
- Exit probes preserved a producer's status 7 and rejected malformed JSON
  nonzero without retaining the prior report at its requested path.

Outstanding: the harness lane is still performing its one baseline run. Do not
contend with it. After it commits, review its event/lifecycle fidelity and
measurements, integrate it, then perform the frozen combined validation listed
above.

## Pass 2 checkpoint

- Re-fetched both refs under the integration hold. The target remains
  `6fa88e027`; this branch is exactly three commits ahead and has no target
  commits to reconcile.
- Re-ran the focused workflow and a fresh timing report with the constrained
  resource defaults. The report contains 16 events, three passing tests, a
  passing package terminal event, no cache label, and no incomplete package.
- Revalidated all 329 unreleased change entries and a clean diff. The report
  wrapper's strict producer/parser exit behavior remains covered by the pass 1
  probes; no CI trigger, permission, timeout, shard, or broad parallelism change
  is justified by the available measurements.

Outstanding is unchanged: the harness lane is actively measuring and must not
be contended with. Review and integrate only its finished committed history,
then run the single frozen uncached combined validation. The target-merge hold
remains active, and `reports/READY` remains absent.

## Pass 3 checkpoint

- Steering revision 02 supersedes the temporary merge hold. The room/timer
  integration is now on the target at
  `b252108d67a2f385a530367f9e22419ca4d37440`, preserving `6fa88e027`.
- The feature branch reconciled that exact target normally at merge commit
  `d6e970ac89b0738d93b7f4c1749d0a751a9128a5`. The focused report package and
  all 331 current unreleased change entries pass on the combined source.
- The harness lane still reports `running`; its worktree contains uncommitted
  profiling/driver work, so there is no finished history to review or integrate
  yet. No competing heavy suite was started.
- `reports/READY` and `reports/QUALITY_READY` remain absent. They are permitted
  only after measured harness improvement, correctness gates, target merge,
  and verified target ancestry.

Outstanding: wait for a committed harness result, review and integrate it, then
run the one frozen combined validation and protected merge.

## Pass 4 checkpoint

- Steering 03 supersedes the prior merge authorization while the release owner
  freezes the combined conversation/UI head. PR #658 remains draft against
  `codex/conversation-execution`; no target, `dev`, `main`, or `staging` write
  was made. After the root explicitly lifts the hold, the coordinated direction
  is to reconcile the separate follow-up onto `origin/dev`, retarget PR #658 to
  `dev`, and use the normal gates rather than merging it into the old target.
- Harness commit `b463a9b10a1f721eaf86891e788423c0f7bc929d` is pushed. Its same-host,
  warm-build-cache and uncached-test measurements report `internal/tui3`
  improving from 564.32s to 402.60s (161.72s / 28.7%), with 4,359 baseline
  tests plus two new harness-law tests, the same four skips, and no failures.
  The change keeps the 150ms/120ms budgets and overlaps eligible waits instead
  of shortening or deleting them.
- Coordinator review confirms the commit is confined to three `internal/tui3`
  test-harness files and has a clean `git diff --check`. It has not been merged
  into this branch because the harness report still marks its full race run and
  order/load stress unfinished; the shared runner is actively executing that
  full race run. No competing full suite was started.
- `reports/READY` and `reports/QUALITY_READY` remain absent. The feature branch
  head remains `ac8486c0ef1f7537a7e1b7526628d049ea873686`; no final tested or merged
  hash exists yet.

Outstanding: wait for the harness lane's completed validation and state, review
the final report/commit, integrate safely, then run one frozen uncached combined
validation. Preserve the draft PR and evidence during the Steering 03 hold.

## Pass 5 checkpoint

- Steering 03 and the coordinated destination decision remain active. PR #658
  is still draft against `codex/conversation-execution`; it must not be merged
  or retargeted until the root explicitly releases the hold and supplies the
  verified PR #653 merge SHA. No target branch was written in this pass.
- Harness commit `b463a9b10a1f721eaf86891e788423c0f7bc929d` remains the finished,
  pushed implementation checkpoint. The lane's full `-race` tui3 run is still
  active under `scripts/one-suite.sh`, and `state/harness.state` still reads
  `running`; its report still lists full-race and order/load acceptance as
  unfinished. The commit therefore remains unmerged here.
- The coordinator rechecked the workflow on the current feature head
  `b2fcbe632b34caf4128207b3b2840809b63eca63`: the report parser package passes
  fresh, `make test-focus` preserves the repository timeout/known-red contract,
  the harness commit has a clean `git show --check`, and PR #658 points at this
  exact pushed head. No competing full or quick suite was launched.
- `reports/READY` and `reports/QUALITY_READY` remain absent. The measured
  564.32s to 402.60s tui3 result is promising but is not final acceptance until
  the harness validation finishes, the commit is integrated on the eventual
  post-PR653 base, the frozen affected suite passes, and the authorized target
  merge is verified.

Outstanding: consume the harness lane only after its state and report are final;
then review and integrate its committed history and run the single frozen
combined validation. Under the current hold, preserve the draft branch and
evidence without producing either success marker.

## Pass 6 checkpoint

- The harness lane finished at
  `b463a9b10a1f721eaf86891e788423c0f7bc929d`. Its final evidence reports the
  same-host, uncached-test improvement from 564.32s to 402.60s (28.7%), an
  independently shuffled full run at 403.66s, waiter-heavy shuffle and
  `GOMAXPROCS=1` stress, and no data races in the full race run. The sole race
  run failure is a pre-existing allocation ceiling that reproduces on the base
  and is not part of the normal race-free claim.
- Coordinator review confirmed that command deadlines remain 150ms/120ms,
  eligible waits overlap without crossing `drive` calls, result collection is
  deterministic by start order, and two new laws guard waiter-symbol and tick
  callback assumptions. The lane was integrated with history at feature merge
  `937ad3478`.
- A frozen JSON-report run of `internal/tui3` plus `internal/ci/testreport` was
  attempted through the required suite lock. It was correctly refused because
  another owner's tui3 suite holds the box at PID 2260601; the parser rejected
  the empty stream and no stale JSON artifact remains. No competing run was
  launched.
- Steering 03 and the follow-up destination decision still govern. PR #658
  remains draft against `codex/conversation-execution`; it must not be retargeted
  or merged until the root supplies the verified PR #653 merge SHA and explicitly
  releases the hold. READY and QUALITY_READY remain absent.

Outstanding: retry the one frozen affected suite after the shared runner is
free, then preserve the exact tested feature head under the hold. Final dev
reconciliation, normal PR gates, target merge, ancestry verification, and both
success markers remain deferred until explicit release.
