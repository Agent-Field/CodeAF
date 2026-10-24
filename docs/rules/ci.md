# Continuous integration

## Pull request checks

`PR gate` (`ci.yml`) runs on pull requests and pushes to `dev`. The required
`check` status combines the light gate and successful touched-package checks.

The light gate builds and vets all Go packages, checks formatting, generates
and tests packed manuals, validates change entries, and runs manual coverage,
structural laws, carried-program help and belt-page checks. Structural laws are
selected through `go/ast` or `go/parser` imports by `scripts/laws.sh`.

Touched packages use `scripts/touched-packages.sh` to map the committed diff to
root-module packages. Module changes select the whole tree; changes without Go
or module edits select no package leg. Tests run fresh with `-count=1` in
separate chat, session, command and remaining-package jobs. Heavy packages are
sharded from one compiled test binary, with a 15-minute timeout per shard.

## Local verification

```sh
make test-focus PKGS=./internal/pkg RUN='^TestRegression$$'
make test-quick
make pr-ready
```

Use focused tests during editing. `test-quick` is the light gate. `pr-ready`
adds touched-package tests and, when applicable, tooling acceptance. Commit Go
and module edits first; the selector compares committed history against
`origin/dev`, or `BASE=<commit>`. Complete heavy suites take a shared per-machine
lock; use focused checks while another full suite is running.

Use `make check` for intentional full-tree validation. It runs the tests,
builds the shipped binary and enforces the size budget described in `PERF.md`.

## Diagnose failures

Read failing job logs before merging. The touched-package classifier uses plain
Go test output, including package results and shard summaries. It retries a
named failure once at the candidate; a repeat failure is probed once at the
exact base. A passing retry is reported as flaky. A failure also present at base
is reported as already failing. Both remain visible as warnings in logs and a
tracking issue; neither is evidence the candidate introduced the failure.

A failure at the candidate with a passing base blocks the check. Build failures,
timeouts, panics, missing results and inconclusive probes stay red. More than
five failed tests in a job stop diagnosis without retries. Fork runs do not
create tracking issues. Local classification makes no issue API calls.

Fix the underlying test or implementation. Do not hide failures with skips,
retry loops or wider timeouts. Use deterministic clocks, scripted providers and
explicit synchronization to reproduce concurrency failures. There is no list
of tests exempted from passing on a clean checkout.

## Full and release checks

`Full check` (`ci-full.yml`) runs nightly and validates promotion candidates.
It runs full tests, cross builds and container-based remote checks; binary size
is reported separately. `Promote to staging` requires a successful fresh Full
check on its exact candidate before moving the pointer. `Release` guards branch
ancestry, tests the applicable release surface and publishes platform builds.
Inspect workflow definitions and live branch rules for current required checks.

CI does not run the race detector. Run `go test -race` on the affected package
when investigating a concurrency issue. A green ordinary test run does not
establish absence of data races. Live-provider tests skip when credentials are
unavailable; container-based remote tests skip without Docker.
