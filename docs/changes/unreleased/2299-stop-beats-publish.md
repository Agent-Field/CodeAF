---
kind: fixed
title: Stop prevents publication until the PR request is sent
pr: 2299
surface: [engine, factory]
invalidates:
  - "A worker could publish from a stale job snapshot after Stop, then lose the successful PR receipt when the factory revoked its lease."
  - "Stopped jobs retained a queued publishing status that implied publication would still run."
---

Read Stop afresh under the job's row lock before every branch push and the PR
request. Leased workers also check the factory's current row at these boundaries.
Proof completion cannot overwrite a stored Stop. Stop withdraws queued publication.
An already sent PR request retains its receipt channel: the job links the opened
PR and tells the person it was not closed. No PR is closed automatically.

The push check now runs after remote preparation, immediately before git push.
Stop frees the active lease immediately; an old publisher retains only a bounded
receipt channel. Its already opened PR stays visible after Run again without
replacing the new run's state or lease. Older factories without publish-check
(404) keep earlier publication behavior without that boundary guarantee.
Transient confirmation errors retry three times within two seconds, then fail
closed with “could not confirm Stop” and guidance to press Approve again.
