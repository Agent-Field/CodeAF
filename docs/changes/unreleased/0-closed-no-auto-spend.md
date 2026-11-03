---
kind: fixed
title: codeaf skips automatic spending on issues closed on GitHub
pr: 0
surface: [engine]
invalidates:
  - "Was true: an issue closed on GitHub after arrival could still receive a paid read, automatic look or start-rule action. Is true now: a successful GitHub check reporting closed or merged skips that work and leaves the row unchanged."
---

Start rules and issue reads check GitHub before doing work. Closed answers are
remembered until the process restarts; Backlog cleanup is unchanged. Checks wait
at most five seconds. Missing access, errors and timeouts let work continue.
