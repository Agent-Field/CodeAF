---
kind: changed
title: the coverage round buys once, and the envelope says when the requested work was done
pr: 1095
surface: [resident, docs]
invalidates:
  - "An unexercised-only finding bought a repair round at every later delivery gate, because the finding stands until a measurement closes it and nothing bounded how many rounds `no check exercises this behaviour` could raise. A job buys at most one such round now (`maxUnexercisedRounds`), and a second finding is recorded unclosed rather than funded."
  - "Growth was bounded only by the round counter, the job-size ceiling, the wall and the daily dollar rail. A share-of-spend rail (`growthSpendShare`) refuses a round once growth has spent more than the requested work itself cost, counted from the gate that first found that work done, and journaled as `CauseSpendShare`."
  - "`codeaf do --json` carried no figure for when the requested work was finished. `core_done_seconds` now appears, in seconds from the start, when a delivery gate first passed or found only a coverage gap, and is absent when no gate said so."
---

An unexercised-only finding is a measurement, so it stands until a measurement
closes it — right, but unbounded, it raised the same test-writing round at every
gate. A run whose named fix was committed minutes in spent the rest of its wall
and most of its bill writing more tests. The round is bounded to one, and growth
past the point the work was done is bounded against the bill that got it done.
