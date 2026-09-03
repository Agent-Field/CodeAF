---
kind: fixed
title: Active tool context stays whole until the work uses it
pr: 575
surface: [chat, engine, docs]
invalidates:
  - "Task workers treated the working-set target as a hard observation limit and could spill unread results; they now retire those results only after a successful filesystem mutation, unless the real model context safety limit is reached."
  - "A repeated edit to an already-recorded path did not advance the context-consumption boundary; every successful filesystem mutation now advances a monotonic per-worker revision."
  - "Conversation turn folding could trigger from total provider context and repeatedly rewrite tiny read results; it now measures actual tool observations and requires meaningful reclaimable headroom."
  - "A replacement task inherited neither the failed task transcript nor a guarantee that its useful report was copied; its brief contract now explicitly requires those findings to be carried forward."
---

This keeps research and edit state verbatim while it is still feeding the next
action, preferring result quality over prompt savings while the selected model
has safe context room.
