---
pr: 1963
kind: added
title: Start a queued factory job above Jobs at once, or hold it
surface: [engine, chat]
invalidates:
  - "Was true: a queued job always waited for a free slot; Start now only queued a Backlog job at the top. Is true: Start now on a queued job lets it take one slot above its computer's Jobs at once, this one time."
  - "Was true: a queued job could only be stopped back to Backlog. Is true: its person can Hold it in the queue and Resume it later, and both are recorded with who made them."
  - "Was true: queued-job notices pointed to Settings → Factory concurrency. Is true: they point to Jobs at once in Settings → Factory → Your computers."
---

Jobs at once stays the one limit on how many jobs a computer runs; there are no
CPU or memory limits per job. A queued job now carries can.start_now, can.hold
and can.resume for its person, and reads Starting now or Held while it waits.
The app reaches Hold and Resume through factory.job_hold and factory.job_resume.
A runner's own capacity read also says how many started-now jobs may run above
its number, and the runner keeps that many extra slots ready until they are
taken; the factory still refuses any other job past the limit.
