---
kind: changed
title: tui3 test waits overlap without crossing a harness call
pr: 658
surface: [build]
invalidates:
  - "The tui3 harness waited for every command serially, so one test paid each command's wait in turn. Safe waiters and pure ticks now overlap within one drive or runCmd call, and every command is answered or dropped before that call returns."
---

The driver belongs to one `drive` or `runCmd` call. A test debugging a flaky boundary may
see safe waits run together inside that call, but an assertion between calls sees the same
settled surface as before; no command is carried across the boundary.
