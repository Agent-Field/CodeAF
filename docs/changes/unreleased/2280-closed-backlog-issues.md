---
kind: fixed
title: codeaf removes closed GitHub issues from untouched Backlog
pr: 2280
surface: [engine]
invalidates:
  - "The GitHub poll only imported open issues, leaving closed issues in Backlog until a person tried to start them."
---

Normal polls now finish untouched issues closed on GitHub and record the finish
in Activity. Each time the factory opens, untouched rows the open list lacks get one check, at
most 40 direct reads per repository per poll; closed history is never imported. Running, queued, previously started and worked jobs are preserved.
An issue reopened on GitHub returns to Backlog when nothing was done on it; a job
with work stays Done. Finished jobs no longer qualify for the paid arrival read,
and a job that ended with no work gets no end brief. App wire fields are unchanged.
