---
kind: internal
title: the test files build for Windows, and make vet checks that they do
pr: 2192
invalidates:
  - >-
    Was true: the gates proved the Windows build because build-cross runs
    `GOOS=windows go build ./...`, but go build never compiles a test file,
    and four packages' tests (cmd/codeaf, internal/factory/worker,
    internal/clipboard, internal/ports) did not build for Windows. Is true
    now: FIFO cases go through a per-platform mkfifo test helper that skips on
    Windows, tests of Unix-only helpers sit in !windows or unix test files
    beside that code, and `make vet` also runs `GOOS=windows go vet ./...`.
---

The CI workflow's Vet step still runs plain `go vet ./...`; `make vet`,
`make test-quick` and `make pr-ready` carry the Windows pass.
