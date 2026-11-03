---
kind: fixed
title: codeaf removes closed GitHub issues from untouched Backlog
pr: 0
surface: [engine]
invalidates:
  - "The GitHub poll only imported open issues, leaving closed issues in Backlog until a person tried to start them."
---

Normal polls now finish untouched issues closed on GitHub and record the finish
in Activity. Existing older rows are reconciled with at most 40 direct checks
per repository per poll, with progress saved across restarts; closed history is
never imported. Running, queued, previously started and worked jobs are preserved.
Reopened issues stay Done until a person explicitly starts them again. Finished
jobs no longer qualify for the paid arrival read. App wire fields are unchanged.
