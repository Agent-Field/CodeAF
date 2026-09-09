# Test-speed follow-up

## Measurement and boundary

This wave starts after PR #657, at `6fa88e027`. On the same Spark host with
`GOMAXPROCS=4` and `GOFLAGS=-p=2`, the last uncached relevant-package run measured:

| package | wall time |
| --- | ---: |
| `internal/tui3` | 563.224s |
| `internal/session` | 210.453s |
| `cmd/aforge` | 46.852s |
| `internal/remote` | 21.111s |
| `internal/enginehost` | 13.249s |

The harness lane owns `internal/tui3`'s fake command lifecycle and waiting. This
lane owns developer workflow, timing reports, documentation, and only CI changes
that measurements prove necessary. The lanes do not run competing full suites.

## Feedback ladder

Use the smallest proof that answers the current question:

1. `make test-focus PKGS=./internal/tui3 RUN='^TestExactRegression$$'` runs one
   named regression. Add `TEST_FLAGS='-count=1'` when the test result itself must
   be fresh; without it, Go may reuse a cached test result.
2. `make test PKGS='./internal/tui3 ./internal/session' TEST_FLAGS='-count=1'`
   runs affected packages through the repository's timeout and known-red ledger.
3. `make test-quick` mirrors the deterministic light PR checks: build, vet,
   format, packed manual, well-formed change entries, manual gates, and laws.
   Whether the branch adds a change entry needs the pull request's base commit,
   so only CI checks that half. It is quick feedback, not acceptance.
4. `make test-report PKGS='./internal/tui3 ./internal/session' REPORT=/tmp/tests.json`
   forces fresh test execution, keeps the separate Go build cache, prints a
   heartbeat and completed slow tests, and writes a machine-readable duration
   report sorted slowest-first.
5. Freeze the candidate, then run one final uncached relevant suite with the
   same resource caps. A whole-tree run still goes through `scripts/one-suite.sh`.

`-count=1` disables reuse of prior test results; it does not discard Go's build
cache. Cold compilation and fresh test execution are therefore separate facts
and should be labeled separately in measurements.

The JSON report is diagnostic evidence, not a replacement gate. A failing test
command remains failing; malformed or empty JSON also fails. A stream cut mid-line
still writes a report, lists packages without a terminal event as incomplete, and
exits non-zero, which makes cancellation or abrupt termination visible.

## Parallelism decision

Package concurrency is already bounded by this wave's `GOFLAGS=-p=2`. Raising
`-parallel` cannot speed tests that do not call `t.Parallel`, and blanket
parallelization is unsafe where tests share process globals, environment,
working directories, ports, or files. No process sharding or broad `t.Parallel`
change is justified before the harness waiting is removed and a fresh duration
report identifies the remaining stragglers. Any later shard must prove exact
once-only inventory and aggregate every failure; speed alone is insufficient.
