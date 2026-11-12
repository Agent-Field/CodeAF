---
kind: added
title: App clients can read Factory job outcomes from a cursor and have them pushed as factory.event
pr: 2416
surface: [engine, factory]
invalidates:
  - "Was true: an app learned that a Factory job merged, failed or needed a person only by reading the whole queue again, and nothing kept those changes for an app that was not connected. Is true now: the Factory keeps the newest 2,000 job outcomes and factory.events_since answers them from the app's cursor, with factory.event pushing later ones."
---

The Factory records a job outcome at the store write: `merged`, `finished`
(with how), `failed` (code, step) or `needs_you` (need, reason). Each carries
the job's own time for that fact, the same time `factory.job` answers. They
are kept in `job-outcomes.json` beside the live record, newest 2,000, and
served by `GET /v1/job-events?after=&limit=&wait=` for the jobs the caller may
see. The live record and `/v1/events` are unchanged.

`factory.events_since {factory, after?, limit?, follow?}` (hello feature
`factory.job_events`, capability `job_events`) answers
`{factory, supported, events, cursor, reset, more}`. `reset` means the cursor
was absent, older than what is kept, or crossed a gap written by an older
binary: read the Factory's current state once, then continue from `cursor`.
With `follow`, the engine pushes each later outcome as `factory.event`. A
Factory older than the route answers `supported: false`. A job created already
finished or waiting records nothing, and `group` stays empty until jobs carry
one.
