---
kind: fixed
title: codeaf skips automatic spending on issues closed on GitHub
pr: 2282
surface: [engine]
invalidates:
  - "Was true: an issue closed on GitHub after arrival could still receive a paid read, automatic look or start-rule action. Is true now: a successful GitHub check reporting closed or merged skips that work and leaves the row unchanged."
---

Start rules and issue reads check GitHub before doing work. A closed answer is
remembered for ten minutes, so a reopened issue is worked again; Backlog cleanup is unchanged. Checks wait
at most five seconds. Missing access, errors and timeouts let work continue.
