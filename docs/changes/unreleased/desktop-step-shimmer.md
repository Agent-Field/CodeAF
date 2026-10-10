---
kind: fixed
title: Desktop step shimmer follows the last running step
surface: [desktop, chat]
invalidates:
  - "Preparing steps could take the live shimmer, and a settled step could retain it when its caller passed a stale shimmer flag. Only the last running step now receives cf-shimmer; settling removes it, and reduced motion keeps secondary ink."
---

The existing shimmer tokens keep the measured 250% gradient and 2.4-second linear
sweep. Browser tests cover light, dark, reduced motion and recorded clock durations.
