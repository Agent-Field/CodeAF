---
kind: fixed
title: A command piped into head no longer reads as failed on macOS
pr: 2870
surface: [engine, build]
invalidates:
  - "Was true: on a Mac, a bash command whose reader stopped early, such as git log | head, reported exit 141 and read as a failed command. Is true now: it succeeds as it does on Linux, on every bash from 3.2 on."
  - "Was true: the fixed-prefix budget test measured find's description and the load_capability schema as the machine running it had them. Is true now: it weighs the widest machine everywhere, and the widest prefix is 52,458 bytes."
---

macOS ships bash 3.2, which does not restore PIPESTATUS after a trap, so the
check that forgives a writer killed by SIGPIPE never saw the pipeline. The
`find` and `ls` descriptions are shorter and state the same facts. Two tests
no longer depend on the machine's git config or on goroutine scheduling.
