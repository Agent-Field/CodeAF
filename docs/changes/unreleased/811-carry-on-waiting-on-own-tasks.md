---
kind: fixed
title: A reply waiting on its own running task is left alone rather than pushed on
pr: 811
surface: [chat, engine, docs]
invalidates:
  - "Only a task started for the message you just sent stopped the end-of-turn reader; a task from an earlier message excused nothing. A reply you did NOT type — one woken by a landing — is now left alone while any task or quick task this conversation started is queued or running. Your own words still outrank it: type or steer anything and the reply is read exactly as before."
  - "The carry-on road ended a turn silently on every gate it has. The one it takes for work of its own writes a journal row now — seam `carry-on`, decision `dropped:awaiting-own-work`, and the id of the piece it stood down for in the reason."
---

Measured on a real drive on 2026-09-10: a chat started two quick tasks in one
batch, the first landed, and the reply that answered that landing was read and
pushed on three times over the second — 19:51, 20:05 and 20:11, each one a
reader call and a `tasks` poll of the node that was about to report, each one
answered "still running, no gap to fix". That is the whole carry-on allowance
spent on a reply that was correctly waiting.

A reply that ends with work of its own still running is waiting, not stopped
short: the child's landing is the wake that continues it. The gate is the one
`propose_task`'s handoff already used, with the turn's epoch taken out and the
person put in its place — the same rule the write seam states as *the person
vetoes and nothing else does*.
