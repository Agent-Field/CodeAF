---
kind: changed
title: pull-request parity has one local target, and heavy test packages share one box lock
pr: 794
surface: [build, docs]
invalidates:
  - "The laptop pull-request ritual was `make check`, which paid for the full tree and the shipped binary. It is now `make pr-ready`: the light gate plus fresh tests for committed packages changed from `origin/dev` or `BASE`; uncommitted Go changes fail closed, and `make check` remains the full-tree Spark/staging ritual."
  - "`scripts/one-suite.sh` locked only a `./...` run, so concurrent agents could still stack full runs of the 73 MB `internal/tui3` and 48 MB `internal/session` test binaries. A full `make test` or `test-report` containing either heavy package now takes the same per-box lock; `test-focus`, manual probes and unrelated packages remain unlocked."
---

The target follows the package-selection logic in the pull-request workflow:
module changes reach the whole tree, while changed Go files reach only their
surviving package directories.
