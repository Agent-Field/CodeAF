---
kind: fixed
title: A job's brief updates when a remote step fails before it reports
pr: 2511
surface: [factory, engine]
invalidates:
  - "Was true: a step that ended failed with no round and no result gave the brief writer no summary for its latest try, so a brief citing that try was refused as naming nothing the job has, and the job's Brief read \"Couldn't update the brief\". Is true now: that try's summary is the job's failure detail when the failure names that step, and the brief check accepts it."
---
