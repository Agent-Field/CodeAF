---
kind: changed
title: Desktop terminal menus match running and finished jobs
surface: [desktop, chat]
invalidates:
  - "The terminal menu offered disabled Stop and a play glyph for Run again. Stop now lives only in the running header, and Run again uses rotate-cw."
  - "Finished jobs offered Copy output in their menu. They now offer Run again, Close tab, and Remove job; Open log precedes these only when a real retained-log opener is available."
---

Closing a finished job detaches its tab without removing its output. The current
client has no retained-log availability field, so live Open log awaits the jobs
client dependency rather than presenting an unavailable action.
