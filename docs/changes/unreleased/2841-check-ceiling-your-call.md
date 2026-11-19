---
kind: fixed
title: finished work whose check met its spend ceiling is your call, and that ceiling grows with the work
pr: 2841
surface: [engine, chat, app]
invalidates:
  - "Was true: when a task's check reached the checker's own spend ceiling, internal/run failed the check task, `CompleteRoot` failed the root, and the row read `failed` over work that had finished and landed (run19: `Vector database research brief · failed`, $0.58). Is true now: the check ends as a non-answer (`session.ErrSpendStopped.SeatCeiling`, `run.Summary.Unchecked`), the run answers done, and the row is `your call · the check stopped at its spend ceiling` (`TaskNotice.Unchecked`). `accept` settles it done, `not right` marks it incomplete, nothing lands a second time, and `check again` is refused."
  - "Was true: the checker's ceiling was always three times a fixed ordinary-task estimate, whatever size the work turned out to be. Is true now: it stretches by how far the worker's spend overran the worker's own estimate, by at most three times more (`config.CrewCheckCeilingFor`, `CrewCheckCeilingStretch`). It still sits under the per-task and daily limits."
  - "Was true: the stop read `the check stopped at its spend ceiling of $X, three times its estimate, before it finished`. Is true now: `the check stopped at its spend ceiling of $X before it finished`. A ceiling that has stretched is not three times the estimate, and a restored crew uses the new sentence."
  - "Still true: the ceiling is the checker's alone, each check in a run gets its own, and a call that costs nothing is never stopped by it."
---
