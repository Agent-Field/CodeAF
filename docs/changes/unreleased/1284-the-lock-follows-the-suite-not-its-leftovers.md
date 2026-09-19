---
kind: fixed
title: the heavy-suite lock is held for the suite, not for what the suite leaves behind
pr: 1284
surface: [build]
invalidates:
  - "The locked descriptor was handed to the heavy suite itself. An advisory lock lives on the open file description, and an inherited descriptor is not close-on-exec, so every process the suite left behind kept the whole box locked: one orphaned test shell held it for eight minutes after a green run."
  - "The descriptor now goes to a holder started beside the suite, in its own session, which runs nothing else and exits when the suite's pid is gone. The suite never receives it, so nothing can inherit it, and a wrapper killed mid-run still does not unlock a box that is running a suite."
---

The lock is released when the holder notices the suite has ended, within its poll,
rather than at the instant the suite exits. After a suite ends, the wrapper checks
the lock and, if it is still held, says so and names the file-level way to find the
holder. It ends nothing.
