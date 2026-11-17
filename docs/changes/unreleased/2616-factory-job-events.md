---
kind: added
title: App clients can read Factory job outcomes from a cursor and have them pushed as factory.event
pr: 2616
surface: [engine, factory]
invalidates:
  - "Was true: an app learned that a Factory job merged, failed or needed a person only by reading the whole queue again, and nothing kept those changes for an app that was not connected. Is true now: the Factory keeps the newest 2,000 job outcomes and factory.events_since answers them from the app's cursor, with factory.event pushing later ones."
  - "Was true: factory.job answered needs_you.since for a failed publish with the time its run landed, and on a Factory service with the current step's time until the runner's done arrived. Is true now: it answers the time the publish failed, so a publish retried and refused again waits from its own refusal."
  - "Was true: a cancel to a step out of attempts, or, while a runner held the job, a no to the budget or a send back with no earlier step to go back to, left a job a person had started in Needs you as a failed job reading Stopped by you; on a Factory service, where the runner's loop makes that stop, always. Is true now: the job waits in Backlog, as after Stop, and records no outcome."
  - "Was true: changing the assignee of a running or waiting job, or unassigning it, first wrote the run's stop on the job as a failure, so a reassigned job waited in Needs you as failed, Stopped by you, and the question its run had asked stayed on it (factory.job's waiting_since). Is true now: the job waits in Backlog for its new person with no failure and no question on it, and offers Run as new; the step the reassignment cut still shows the stop."
  - "Was true: unassigning a running or waiting job left it without Run as new. Is true now: a job whose run a change of assignee or an unassign cut offers Run as new, and the run_as_new door accepts it."
  - "Was true: factory.job answered needs_you.since for a wait with no time of its own (a start held for approval, a take-over, an AgentField run's approval, a Stop undone at a gate, a failed merge, a pull request's requested changes or conflict) with the job's last change, so it moved with every rename, priority, budget or note. Is true now: needs_you.since is when the wait began, and stays put while the job waits with the same need and question."
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
A gap resets once, because the read that finds it closes it, and a follower
that is reset to the cursor it asked waits before it reads again, 2 seconds
doubling to a minute, so a Factory in that state is not read back to back.
With `follow`, the engine pushes each later outcome as `factory.event`. A
Factory older than the route answers `supported: false`. A job created already
finished or waiting records nothing, and one wait on a person is one
`needs_you` however many writes the job takes while it waits, a runner's
later reports while the job is parked at its budget among them. A wait with
no time of its own (a start held for approval, a take-over, an AgentField
run's approval, a Stop undone, a failed merge, a pull request's review)
waits from the write that began it, which the store keeps on the job. A `failed`
outcome says what `factory.job` then says of the job. A job a person stopped
records nothing and waits in Backlog, asking nothing of anyone, whether the
stop came by Stop at a gate, in a step or after one, by a step's own Stop, or
by an answer that stops it (a cancel to a step out of attempts; while a
runner holds the job, a no to the budget or a send back with no earlier
step), on the service or in the runner's loop. A change of assignee or an unassign that stops a run hands the job on in
Backlog, takes down the question the run asked and records nothing. A Close or Reject records only its `finished`.
A job left failed and waiting on a person records `failed`, whatever its
failure's code. `group` stays empty until jobs carry one. The
cursor checks which jobs the caller may see from each job's saved copy, so
reading it looks up no author access on GitHub. An outcome whose repository
GitHub could not answer for just now (a 5xx, a failed connection, or a spent
rate limit, until its retry time) is not passed as hidden: the page ends
before it, and a read that would start at it answers try again (HTTP 503)
from the same cursor.

Carried from #2416, which merged only into the closed integration branch of
#2306.
