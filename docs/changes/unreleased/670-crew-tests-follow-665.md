---
kind: fixed
title: dev is green again after #665 — the crew tests stop expecting a rung on a shipped preset's brain
pr: 670
surface: [chat]
invalidates:
  - "The max crew's confirmation line and its /status line read `brain kimi-k3:high`. Since #665 they read `brain kimi-k3`: a shipped preset buys a bigger planning model and leaves its generation behaviour to the provider, and the two chat tests that still asked for the rung were what kept dev red."
---

Test-only. Plain `origin/dev` at `5376c1e23` failed both tests with no other
change on it; this restates them as the law #665 made, and asserts the rung is
absent so the old spelling cannot be reintroduced by accident.
