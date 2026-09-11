---
kind: fixed
title: A dev build publishes its release instead of skipping publish after the build
pr: 834
surface: [build]
invalidates:
  - "The first live run of `release.yml` on `dev` (ef36c3770) built every target and published nothing, because `publish` had no `always()` condition under the skipped `test` job. It carries one now, and the law test demands it on both jobs below `test`."
---

GitHub skips a job whose dependency chain holds a skipped job unless the job's own
condition says otherwise, and `test` is skipped on every dev build by design.
