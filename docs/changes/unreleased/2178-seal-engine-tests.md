---
kind: internal
title: seal tests run codeaf's own engine, and every make test target stages it first
pr: 2178
invalidates:
  - >-
    Was true: three internal/session seal tests accepted any furrow on PATH,
    then failed because the product seals only with codeaf's own engine, and
    no make test target staged that engine. Is true now: real-engine tests ask
    celltest.Engine, which resolves the configured or embedded engine and skips
    by name when there is none, and `make test` and `make test-focus` (so also
    test-touched, pr-ready and the touched legs) run `make furrow` first.
---

The shared helper replaces the per-test resolve-and-skip copies in
internal/session, internal/cellstore, internal/cellsync, internal/cellindex,
internal/syncsetup and cmd/codeaf. internal/session lifts its suite-wide furrow
pin through one sealEngine helper that honours CODEAF_TEST_FURROW.
