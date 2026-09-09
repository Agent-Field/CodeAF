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
- After the prior lock holder finished, the frozen JSON-report run of
  `internal/tui3` plus `internal/ci/testreport` passed at `5a5b245f4` through
  the required shared lock: 4,385 passed, 4 skipped, 0 failed, 0 package
  failures, no incomplete packages, and no cached package labels. Tui3 took
  407.901s. The machine-readable report is
  `../logs/coordinator-pass6-frozen.json`; stderr records 30-second heartbeats
  and the slowest tests, led by `TestALargePasteIsOneEditAndNoLayout` at 22.22s.
- `make test-quick` initially caught one missing `gofmt` alignment in the
  profiler. After that mechanical one-line rewrite, the complete quick gate
  passed (build check, vet, formatting, packed manual, and 56 law files across
  16 packages), and `make build` produced `bin/aforge`. The formatting change
  does not alter the already measured test binary's behavior.
- Steering 03 and the follow-up destination decision still govern. PR #658
  remains draft against `codex/conversation-execution`; it must not be retargeted
  or merged until the root supplies the verified PR #653 merge SHA and explicitly
  releases the hold. READY and QUALITY_READY remain absent.

Outstanding: preserve this tested feature branch under the hold. Final dev
reconciliation, an affected validation on that new base, normal PR gates,
target merge, ancestry verification, and both success markers remain deferred
until explicit release.

## Pass 7 checkpoint

- Steering revision 04 still records the PR #653 release-integration hold as
  active. GitHub confirms PR #653 remains open and draft against `dev`; this is
  observation only, not authority to infer completion or release the hold.
- Draft PR #658 remains open, cleanly mergeable, and still targets
  `codex/conversation-execution`. Its tested code head is
  `7ff64a810e61582336416d988356192e1d7f05e9`; the only later feature-branch
  change in this pass is this status record. It was neither retargeted nor
  merged, and no protected branch was written.
- The isolated candidate remains fully prepared at that head: controlled
  uncached tui3 tests improved from 564.32s to 402.60s (28.7%); the frozen
  affected report passed 4,385 tests with 4 skips and no failures, cached
  package labels, or incomplete packages; `make test-quick` and `make build`
  passed after the final formatting-only commit.
- Before this status-only checkpoint, remote refs were checked without
  mutation: `dev` was `65f060d338`, the old conversation target was
  `2a02ac0bb`, and the tested feature ref was `7ff64a810`.
  `reports/READY` and `reports/QUALITY_READY` remain absent as required.

Outstanding: wait for an explicit hold release carrying the verified PR #653
merge SHA. Then reconcile this separate follow-up onto that `origin/dev`,
retarget PR #658, rerun affected validation and normal gates on the reconciled
head, merge through the PR with expected-head protection, verify ancestry, and
only then write both success markers.

## Pass 8 checkpoint

- Steering revision 04 and the coordinated destination decision remain active.
  PR #653 is still open and draft against `dev`, with no merge commit recorded;
  that observation does not release the hold. No target or protected branch was
  written in this pass.
- The finished harness evidence was re-read from the shared wave: its controlled
  uncached-test result remains 564.32s to 402.60s (28.7%), its normal and
  shuffled full runs passed, and its full race run reported no data races. The
  one race-mode allocation-law failure reproduces at the base and is outside
  the normal gate; no known-red entry was added.
- Draft PR #658 remains open and mergeable against
  `codex/conversation-execution`, with pushed head
  `f795606e40c8814ba1bf1d8805dad39b7d2d4127`. Remote `dev` remains
  `65f060d338ec5d01eded666b420ec533319f35ac`, and PR #653's head remains
  `2a02ac0bb9c4d085a345805fd809ac9539f2d896`.
- No duplicate heavy validation was started: the frozen candidate already has
  4,385 passes, 4 skips, zero failures, no cached labels, and no incomplete
  packages, followed by green `make test-quick` and `make build`. The next
  meaningful full run is the required affected validation after reconciliation
  onto the explicitly authorized post-PR653 `origin/dev`.
- `reports/READY` and `reports/QUALITY_READY` remain absent.

Outstanding: await an explicit hold release carrying the verified PR #653 merge
SHA. Only then reconcile onto current `origin/dev`, retarget PR #658, validate
the combined affected source and normal gates, merge with expected-head
protection, verify target ancestry, rebuild in this owned worktree, and write
both success markers.

## Pass 9 checkpoint

- Steering revision 04 and the coordinated destination decision remain active.
  A fresh read-only check finds PR #653 still open and draft against `dev`, with
  no merge commit or merged timestamp. This observation does not authorize a
  retarget or merge, and no protected branch was written.
- Draft PR #658 remains open, cleanly mergeable, and based on
  `codex/conversation-execution`. Its exact pushed head is
  `5956e951622135bcedc37d3cc2060f922594150d`; remote `dev` remains
  `65f060d338ec5d01eded666b420ec533319f35ac`, and the conversation branch and
  PR #653 head remain `2a02ac0bb9c4d085a345805fd809ac9539f2d896`.
- No duplicate heavy run was launched. The frozen candidate evidence remains
  the controlled 564.32s to 402.60s uncached tui3 improvement, the 4,385-pass
  affected report with four skips and no failures/cache labels/incomplete
  packages, followed by green `make test-quick` and `make build`.
- `reports/READY` and `reports/QUALITY_READY` remain absent. The next useful
  expensive validation is on the explicitly authorized post-PR653 `origin/dev`
  source, not another run of the unchanged held candidate.

Outstanding: wait for the root's explicit hold release and verified PR #653
merge SHA. Then reconcile onto the current `origin/dev`, retarget PR #658, run
the affected suite and normal gates on that exact candidate, merge through the
PR with expected-head protection, verify ancestry, rebuild here, and only then
write both success markers.

## Pass 10 checkpoint

- Steering revision 04 and the coordinated destination decision remain active.
  After a fresh fetch, PR #653 is still open and draft against `dev`; its light,
  touched-package, and check jobs are green, but it has no merge commit or
  merged timestamp. Green checks alone do not release the explicit hold.
- Remote refs are unchanged: `origin/dev` is `65f060d338ec5d01eded666b420ec533319f35ac`,
  `origin/codex/conversation-execution` and PR #653's head are
  `2a02ac0bb9c4d085a345805fd809ac9539f2d896`, and the pushed PR #658 head is
  `183981045d7b649e31d049812765496abc0c4ff1`.
- PR #658 remains open, draft, and cleanly mergeable against the held
  conversation target. It was neither retargeted nor merged, and no protected
  branch was written.
- No duplicate heavy run was launched. The unchanged frozen candidate already
  has the controlled uncached tui3 improvement from 564.32s to 402.60s (28.7%),
  4,385 passing affected tests with four skips and no failures/cache labels/
  incomplete packages, followed by green `make test-quick` and `make build`.
- `reports/READY` and `reports/QUALITY_READY` remain absent as required.

Outstanding: the root must explicitly release steering03 with PR #653's
verified merge SHA. Only then reconcile this follow-up onto current
`origin/dev`, retarget PR #658, validate that exact candidate, merge through the
PR with expected-head protection, verify ancestry, rebuild in this owned
worktree, and write both success markers.

## Pass 11 checkpoint

- Steering revision 04 and the coordinated destination decision remain active.
  A fresh fetch and GitHub query show PR #653 still open and draft against
  `dev`, with no merge commit or merged timestamp. Its three current gates are
  green, but that does not supersede the explicit release requirement.
- Remote refs remain unchanged: `origin/dev` is
  `65f060d338ec5d01eded666b420ec533319f35ac`,
  `origin/codex/conversation-execution` and PR #653's head are
  `2a02ac0bb9c4d085a345805fd809ac9539f2d896`, and PR #658's pushed head before
  this checkpoint is `2bc8a94379d414cf942e21ad715c7c6986b76a97`.
- PR #658 remains open, draft, and cleanly mergeable against the held
  conversation target. It was not retargeted or merged, and no protected
  branch was written.
- The context-modal wave is currently running a full `internal/tui3` affected
  suite through `scripts/one-suite.sh`. No competing heavy run was launched.
  The next test-speed full validation remains reserved for the explicitly
  authorized post-PR653 `origin/dev` candidate.
- The tested implementation evidence is unchanged: controlled uncached tui3
  improved from 564.32s to 402.60s (28.7%); the frozen affected report passed
  4,385 tests with four skips, no failures, cached labels, or incomplete
  packages; `make test-quick` and `make build` passed. `reports/READY` and
  `reports/QUALITY_READY` remain absent.

Outstanding: await the root's explicit hold release carrying PR #653's verified
merge SHA. Then reconcile onto current `origin/dev`, retarget PR #658, validate
the exact combined head, pass the normal gates, merge through the PR with
expected-head protection, verify ancestry, rebuild here, and only then write
both success markers.
