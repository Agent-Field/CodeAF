---
kind: fixed
title: the test suite is green on macOS, and the allocation law prices its marshals itself
pr: 1722
surface: [engine]
invalidates:
  - "the ten tests that failed `make pr-ready` on a clean dev on a Mac were a host or toolchain assumption in each test, never a product fault or a flake — CI on ubuntu was green for the same commit."
  - "`internal/provider/alloclaws_test.go` no longer names 8 for a warm breakpoints encode; encoding/json's own price is measured in-process, this package's own allocations are named, and nothing more is allowed (8 in all on Go 1.26, 11 on Go 1.27 — both measured)."
---

Four `internal/session` tests that hold a task under a memory floor no machine
meets (`1 << 40` MiB) state their machine through `littleMemoryHost`, a `readHost`
seam the governors are built over, because the real reading is `/proc/meminfo` and a
host without it admits everything; they now run and pass on every host instead
of holding only on Linux. Four `cmd/codeaf` engine tests, the standing
isolation test and the held-run reopen test compare against the canonical temp
folder, since the engine records the resolved root and macOS spells `/var`
through `/private/var`. The warm breakpoints encode law measures encoding/json's
own price in-process, names this package's own result and parts slice allocations,
and allows nothing more anywhere on the warm path, inside `marshalMarked` included.
