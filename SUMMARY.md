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
