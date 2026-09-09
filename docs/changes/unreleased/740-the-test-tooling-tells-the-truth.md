---
kind: fixed
title: the timing report says only what go test said, and the quick target is the light gate it names
pr: 740
surface: [build, docs]
invalidates:
  - "`make test-report` wrote `test-report.json` into the repository root and nothing ignored it. Several sessions share this checkout and an untracked file at the root is what another lane's `git add` sweeps up; the default report is ignored now."
  - "`\"cached\": true` was written for any package whose output contained `(cached)` anywhere — a test that logged the word marked its own package, and a package run under `-count=1` was reported as cached. Go's package summary line, `ok  <package>  (cached)`, is the only witness now."
  - "A run cut mid-line lost everything: the reporter refused the stream, wrote nothing, and `scripts/test-report.sh` had already removed the previous report. A cut stream is now a truncation and not a parse failure — the report is written, `\"truncated\": true` and `incomplete_packages` say what did not finish, the packages that did not finish are named on stderr, and the command still exits 2. An empty stream and a whole line that is not JSON are still refused with no file."
  - "The report's `schema` was 1. It is 2: `truncated` is new and `cached` means something narrower."
  - "`scripts/test-report.sh`'s heartbeat was cleaned up by `trap … EXIT`, which SIGKILL does not run, so the loop reparented to init and reported forever on a run nobody was watching. Each tick asks whether the script is still there. `TEST_REPORT_HEARTBEAT` sets the interval; the repository's own test of this uses it."
  - "`make test-quick` was described in the Makefile, in CLAUDE.md and in docs/design/test-speed/PLAN.md as mirroring the deterministic light half of the pull-request gate, and it ran neither the changelog check nor two of the three manual gates. Adding a slash command with no manual page left it green while the pull request went red. It now runs `go run ./cmd/aforge-changes check`, `go test ./internal/manual/` and `go test -run 'Manual' ./internal/tui3/ ./internal/session/` as well — about twenty seconds more on a cold cache. The half of the changelog check that asks whether THIS branch adds an entry still needs the pull request's base commit and only CI can answer it."
---

#658 shipped the tooling with the sentence a timing artifact that lies is worse
than no artifact written in its own doc comment. These are the five places it was
lying, found by reading the artifact rather than the code.
