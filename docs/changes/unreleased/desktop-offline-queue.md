---
kind: changed
title: Offline sends wait in the pane and go out when the engine answers
surface: [desktop, chat]
invalidates:
  - "A send while the engine was unreachable stayed in the composer as an unsent draft. Plain text now waits in that pane, drawn at 60% opacity, and goes out in order when the engine answers."
---

A send with files stays in the composer. A refusal after reconnect puts that message back.
