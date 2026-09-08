---
kind: internal
title: the stalled-check regression leaves room for real checker setup
pr: 655
surface: [build]
---

The real-door retry fixture used a 200-millisecond checking window, which could
expire during repository and checker setup when package suites shared a machine.
It now allows ten seconds while still requiring the first call to time out,
exactly two calls, and a successful second answer. Production limits are unchanged.
