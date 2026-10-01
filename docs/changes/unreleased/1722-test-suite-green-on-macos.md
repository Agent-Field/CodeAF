---
kind: fixed
title: the test suite is green on macOS, and the allocation law prices its marshals itself
pr: 1722
surface: [engine]
invalidates:
  - "the ten tests that failed `make pr-ready` on a clean dev on a Mac were a Linux assumption in each test, never a product fault or a flake — CI on ubuntu was green for the same commit."
  - "`internal/provider/alloclaws_test.go` no longer names 8 for a warm breakpoints encode; the figure belongs to encoding/json (8 on Go 1.26, 11 on Go 1.27) and the test now measures the two marked marshals in-process."
---

Four `internal/session` tests that hold a task under a 1 TiB memory floor
state their machine through `littleMemoryHost`, a `readHost` seam the
governors are built over, because the real reading is `/proc/meminfo` and a
host without it admits everything; they now run and pass on every host instead
of holding only on Linux. Four `cmd/codeaf` engine tests, the standing
isolation test and the held-run reopen test compare against the canonical temp
folder, since the engine records the resolved root and macOS spells `/var`
through `/private/var`. The warm breakpoints encode law demands the result
slice plus whatever the two marked marshals cost on this toolchain, and nothing
more.
