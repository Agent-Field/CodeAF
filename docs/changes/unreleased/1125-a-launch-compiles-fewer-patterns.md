---
kind: changed
title: a launch compiles 78 fewer patterns, and a surface holds 8 Ps instead of every core
pr: 1125
surface: [chat, engine, build]
invalidates:
  - "`internal/verify` compiled all 78 of its regexps in package-level vars, so every one was built before `main` on every invocation, `--version` included. They are lazy now: package init falls from 1.5 ms and 1539664 bytes to 0.033 ms and 15216 bytes, and best-of-20 `--version` from 14236 us to 12455 us. `MustCompile`'s panic on a malformed pattern is kept, at first use rather than at init."
  - "`tuneForTheSurface` capped nothing but the GC percent, so a surface and its engine host each held one runtime GC-worker goroutine per P — twenty of them on a twenty-core box. The surfaces now cap GOMAXPROCS at 8 and the engine host inherits it: the idle daemon drops from 33 to 21 goroutines and 13 to 11 OS threads. An explicit GOMAXPROCS still decides, as an explicit GOGC already did, and a machine no bigger than the cap is untouched. RSS does not change, and idle wakeups were too noisy across runs of one build (0 to 84/s) to claim anything from."
---
