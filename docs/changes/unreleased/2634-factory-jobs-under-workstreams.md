---
kind: changed
title: A job in a workstream is changed only by its person, an editor of the workstream or the operator
pr: 2634
surface: [factory]
invalidates:
  - "On a team factory anyone who can read a job's repository can stop, steer, re-prioritise, reply on, nudge or recheck it; now only the job's person, someone with edit on its workstream, or the operator can (403 not_job_person)."
  - "A job with no person can be run, dropped or merged by anyone; inside a workstream it now needs edit on that workstream (outside every workstream it is unchanged)."
  - "Only the person who stopped a job can Undo the stop; the job's own person can now Undo it too."
  - "A job is visible to anyone who can read its GitHub repository; a job in a workstream is now also hidden, with its events, from anyone who cannot view that workstream."
  - "A sign-in whose token names no profile reads as the server machine's own GitHub login and sees everything; it now sees no workstream job and acts as no one's person."
---

A job belongs to the workstream whose go-ahead started it. The gate is one check in
front of every job door (`internal/factory/serve/job_access.go`); `follow`,
`brief_seen` and `scout` only need sight of the job, and `take`/`release` keep the
lease desk's own person rule.
