---
kind: fixed
title: a reply waiting on a job it started is waiting, and carrying on stops after three
pr: 95
surface: [chat, engine, docs]
invalidates:
  - "A reply that ended while a job it had started was still running was read for what remains and carried on. It is not: a live background job, watch, render or forked hand now ends the reply the way a question to the person does."
  - "The only bound on carrying on was the meter, and the manual said so in as many words: \"There is no separate limit and no number to raise.\" One ask is now carried on at most three times, and the fourth reading ends the reply instead."
  - "A reply that ends waiting on a watch used to be pushed on until the running-long point converted the wait into a task. It now just ends — a watch's news still waits for the next thing you say, which is a gap in the wake lane and not something carrying on ever fixed."
---

Measured 2026-08-31, 15:36:25–15:41:45Z: a conversation waiting on GitHub's
checks for two pull requests said so at the end of every reply, and the
end-of-turn reader — which can answer done, stop or carry on, and for which
"waiting on the world" is none of the three — carried it on twenty times in five
minutes. Each one a reader call and another `gh pr checks`, about a third of a
dollar plus two bloated replies for no progress, until the running-long point
moved the wait into a task nobody could ever finish.

Carry-ons can no longer reach that point by themselves: the rungs are ten,
twenty and forty rounds, and three is not ten. A reply that still gets handed to
a task got there on rounds of its own work.
