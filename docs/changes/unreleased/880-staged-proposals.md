---
kind: changed
title: propose_task is a staged tool, so its card can go up before the reply that carries it is whole
pr: 880
surface: [chat, engine]
invalidates:
  - "Only the four readers (read, grep, find, ls) could start before a reply finished streaming. A tool built with bare.StagedTool can too; its reversible half runs early and its commit waits on a bare.Hold until the reply is whole."
  - "propose_task compiled its admission context before putting the card up. It compiles it at the commit, after the reply that carries the call is recorded, so the brief quotes that reply."
  - "askTask asked and waited in one call. openTask puts the card up and starts the wait on its own goroutine at once; taskWait.answer reads what it came to and taskWait.withdraw takes it all back."
  - "A proposal card could only settle as approved, declined, redirected or expired. It can also settle as `withdrawn · its reply did not go through`, carried on the new TaskNotice.Withdrawn field."
---

The turn loop's half of this (the seam in loop.go's warm batch) and the manual lines that
describe the early card are one commit on `speed/ts-B-seam`, because loop.go belonged to
another lane. Until that commit lands, a proposal still waits for its reply to finish.
`docs/design/task-start/DESIGN.md` has the measurement and names every move of the seam.
