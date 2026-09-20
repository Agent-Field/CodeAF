---
kind: internal
title: a settled design for closing the window, where work keeps running or waits as interrupted
pr: 1224
surface: [chat, engine]
invalidates:
  - "Nothing was written down about what closing the window does to a run. `docs/design/worker-harness/CLOSE-WINDOW.md` records what is true today (a live run does not count as work, so its conversation is retired at 30 minutes; an unsettled run reads failed and stopped on reopen) and states the rule: work is running, interrupted or settled, continuing always asks first, and the five-minute tick never continues a run. The owner settled both open questions on 2026-09-20, so the page is a design to build rather than a proposal to answer. It still changes no behaviour; three cells follow it."
---

The owner asked what happens to work when codeaf is closed and whether it can
be continued from where it was. The page is the answer. It was opened as a
proposal with two questions that were his to settle, he answered both on
2026-09-20, and his words are quoted at its foot. Three cells follow: a live
run counts as work and keeps its engine, an unsettled run reads `interrupted`
from the store rather than failed, and the card that continues it. A fourth,
the tick continuing a run on its own, is refused by that answer and is not
built.
