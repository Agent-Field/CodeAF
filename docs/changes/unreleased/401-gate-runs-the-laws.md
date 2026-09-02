---
kind: changed
title: the pull-request gate runs every law and the packages you touched, and one proof means one thing
pr: 401
surface: [build, docs]
invalidates:
  - "The pull-request gate's only engine test step was `go test -run 'Manual' ./internal/tui3/ ./internal/session/`; no structural test ran on a pull request, which is how #279 crossed the endings ratchet and merged green (#371, #372). The gate now runs `make test-laws` — every test in a file that imports `go/ast` or `go/parser`, selected at run time by `scripts/laws.sh` — and a second job, `touched packages`, runs the full suite of every package the change touched. Write a structural test with that import and it is on the gate the day it lands."
  - "`make test` was a bare `go test ./...` that skipped nothing, so `make check` could not pass on a clean tree while CI skipped `.github/known-red.txt`. The Makefile now reads the ledger once (`KNOWN_RED`, `TEST_SKIP`) and every run — `make test`, `make check`, `touched packages`, the nightly — goes through it with a measured `TEST_TIMEOUT := 15m`. `make test PKGS=./internal/x` runs named packages; `make test TEST_FLAGS='-p 1'` is what the nightly's shards call."
  - "The nightly's per-package timeout was `8m`, and `internal/tui3` takes about 485 seconds on the runner and on a loaded box; every nightly before 2026-09-02 had been red and the 2026-09-02 one reported `TestTheStripCannotOutliveTheRowItWasOpenedOn` as a hang when it had been running for two seconds. Give tui3 `-timeout 15m`; a timeout under load is unattributed, never a red."
  - "A scheduled full check reported to nobody. It now opens or updates one issue titled `ci: the nightly full check is red on dev` on a red night; whoever makes the nightly green closes it."
  - "`.github/known-red.txt` could gain a line in a pull request. It cannot now: `internal/ci` holds `knownRedEntries` (18 at 35c1a79e), fails if the file has more, fails if it has fewer without the constant lowered in the same change, and fails if a listed name is not a test the tree declares. Ruled 2026-09-02: the ledger burns to zero in its own wave and never gains an entry. The commit that deletes the file deletes that test with it; an absent ledger skips nothing."
  - "CLAUDE.md copied the list of tests that fail on a clean tree. It no longer does; `.github/known-red.txt` is the one place it is written, and CLAUDE.md's Tests section points there."
  - "Two full suites could run on one box at once, and on 2026-09-02 nine did, manufacturing reds by load. `make test` on `./...` now takes `scripts/one-suite.sh`'s lock — a pid with its command line checked, so a dead holder never blocks — and a second whole-tree run refuses to start, naming the holder. Runs of named packages do not take it."
  - "gofmt was not in the gate. `make fmt-check` is, and `internal/cachedir/cachedir_test.go` and `internal/tui3/ground_test.go` are formatted."
---

docs/rules/ci.md is rewritten to say all of this. The one thing the gate cannot prove
from a laptop is itself: the `touched packages` job's first real run is on this pull
request, and the `page` job's is the next scheduled nightly.
