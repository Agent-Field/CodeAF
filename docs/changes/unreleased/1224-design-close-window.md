---
kind: internal
title: a proposed design for closing the window, where work keeps running or waits as interrupted
pr: 1224
surface: [chat, engine]
invalidates:
  - "Nothing was written down about what closing the window does to a run. `docs/design/worker-harness/CLOSE-WINDOW.md` records what is true today (a live run does not count as work, so its conversation is retired at 30 minutes; an unsettled run reads failed and stopped on reopen) and proposes the rule: running, interrupted or settled, continue always asks, the tick never continues a run. It is PROPOSED and changes no behaviour."
---

The owner asked what happens to work when codeaf is closed and whether it can
be continued from where it was. The page is the answer as a proposal, with two
questions that are his to settle before any cell is built.
