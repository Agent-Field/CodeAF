---
kind: fixed
title: a background job reads as ended as soon as its log closes
pr: 1627
surface: [chat, engine]
invalidates:
  - "Since the bounded job logs, a background job kept reading as running while codeaf swept the whole job-log folder after it ended, so a stop took tens of milliseconds longer to show and an existing stop test failed under load. Its status and exit code now become final as soon as its log closes; the sweep still finishes before shutdown stops waiting for the job and before its completion note is written."
---

Only the order inside a job's ending changed. The log file still closes before
the status is final, the folder sweep still runs on every job's end, and a sweep
that could not finish is still named in the completion note.
