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

Outstanding: validate tooling, open draft PR and add its numbered changelog;
wait for and review harness lane; integrate and run one frozen uncached relevant
suite; build, pass required checks, merge, verify ancestry, and write READY.
