---
kind: added
title: Mark a job pull request ready for review
pr: 2238
surface: [engine]
invalidates:
  - "Was true: a draft PR blocked Merge and required GitHub or the manager to mark it ready. Is true now: the job offers a direct ready-for-review action to the person who may merge it."
---

Mark an open draft ready with your own GitHub sign-in. The job immediately
shows the confirmed ready state, without waiting for a poll. Merge keeps its
other checks. Changed jobs and unavailable actions are refused; an uncertain
outcome is never retried automatically.
