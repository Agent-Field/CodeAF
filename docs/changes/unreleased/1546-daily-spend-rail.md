---
kind: fixed
title: interactive turns enforce the daily spend rail, with task workers and errands exempt
pr: 1546
surface: [chat, engine]
invalidates:
  - "An interactive chat turn only checked the conversation's own limit, so a day of many conversations could spend past the daily budget without a word. A turn now refuses to start once the day's spend — the ledger's total, or the crew's held day if that is larger — reaches the daily budget, and the refusal names the figure and the door in one line: `/budget day changes it`."
---

Task workers and errands are exempt from the daily rail: they run inside a turn
that already passed it, and a worker stopped at the rail would strand the work it
was given. The budget is read fresh at the door where turns begin, so a changed
`/budget` takes effect on the next turn without a restart.
