---
kind: fixed
title: Desktop scroll restoration browser tests wait for durable positions
surface: [desktop, build]
invalidates:
  - "The scroll-restore browser spec assumed a 600ms sleep proved persistence and that the Latest entrance survived a browser round trip. It now waits for the saved position and captures entrance keyframes when the pill mounts."
---

The two-reload case has a 60-second overall budget while its restoration polls
keep their existing limits. The real wheel still proves bottom anchoring, and
both light and dark themes retain the fade, rise and narrow-layout assertions.
