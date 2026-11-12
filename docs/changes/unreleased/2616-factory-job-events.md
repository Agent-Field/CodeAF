---
kind: added
title: App clients can read Factory job outcomes from a cursor and have them pushed as factory.event
pr: 2616
surface: [engine, factory]
invalidates:
  - "Was true: an app learned that a Factory job merged, failed or needed a person only by reading the whole queue again, and nothing kept those changes for an app that was not connected. Is true now: the Factory keeps the newest 2,000 job outcomes and factory.events_since answers them from the app's cursor, with factory.event pushing later ones."
  - "Was true: factory.job answered needs_you.since for a failed publish with the time its run landed, and on a Factory service with the current step's time until the runner's done arrived. Is true now: it answers the time the publish failed, so a publish retried and refused again waits from its own refusal."
  - "Was true: on a Factory service, a job parked at its budget answered needs_you.since with the arrival of its runner's latest report, because the runner sends the budget question again with each later report while it waits. Is true now: the same question sent again keeps the time it was first asked, and adds no second ask line to the job's log."
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
finished or waiting records nothing, and one wait on a person is one
`needs_you` however many writes the job takes while it waits, a runner's
later reports while the job is parked at its budget among them. A job a person
stopped records nothing (it waits in Backlog and asks nothing of anyone),
whether the stop came at a gate, in a step or after one; a Close or Reject
records only its `finished`. `group` stays empty until jobs carry one. The
cursor checks which jobs the caller may see from each job's saved copy, so
reading it looks up no author access on GitHub.

Carried from #2416, which merged only into the closed integration branch of
#2306.
