---
pr: 1899
kind: changed
title: Make factory concurrency visible and default to fifteen jobs
surface: [engine, chat]
invalidates:
  - "Was true: a new factory runner defaulted to two jobs with an eight-job ceiling. Is true: new runners default to fifteen jobs, within a configurable one-to-one-hundred range; existing saved limits are preserved."
  - "Was true: a queued manager could say a bench must free without a configuration path. Is true: queued jobs name execution capacity, report current owned-machine usage when known and point to Jobs at once in Settings → Factory concurrency."
---

The desktop exposes concurrency on its main Settings page and gives queued-job
notices a direct navigation action. Capacity changes remain owner-only and take
effect without interrupting admitted work. Idle PR monitoring remains independent
of execution slots: review feedback queues work once and a finished maintenance
pass releases its lease before background monitoring continues.
