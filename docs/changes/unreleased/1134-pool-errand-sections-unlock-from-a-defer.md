---
kind: internal
title: the pool errand tracker's critical sections unlock from a defer
pr: 1134
surface: [engine]
invalidates:
  - "`stopPoolErrands` and `poolErrandGo` in `cmd/codeaf/poolindex.go` took `poolErrandsMu` without a deferred unlock, which the guarded tree's lock law refuses because an absorbed panic inside the section would deadlock every later pool start-up. Each section is its own function now and unlocks from a defer; the wait and the goroutine start still happen with the lock released."
---
