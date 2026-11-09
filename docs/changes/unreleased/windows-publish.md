---
kind: fixed
title: Reminders, spending history and state updates work on Windows
surface: [engine, chat, resident, remote, factory]
invalidates:
  - "Windows could not register an open window for the automation clock, so due reminders never fired. Presence now keeps its lock while publishing and removing its window file."
  - "Append-only Windows usage handles could not acquire the ledger lock, losing spending history and weakening the daily cap. Accounting now locks its matching read handle."
  - "A current spending window could omit completed calls sharing the clock's current reading. Its default end now includes that reading, while explicit end times remain exclusive."
  - "Permanent conversation deletion failed on its own open journal, and directory flushes refused sync and task state updates on Windows. These paths now use shared filesystem operations."
---

State replacements and removals use the existing bounded Windows retry policy.
Held presence, deletion and usage handles permit replacement and deletion. File
contents are still flushed before durable publication; directory entries are
flushed on platforms that support it. POSIX replacement and removal semantics
are unchanged. A persistent filesystem error still reaches its existing caller.
