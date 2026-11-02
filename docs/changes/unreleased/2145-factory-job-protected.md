---
kind: added
title: A Backlog job says which protected paths it names
pr: 0
surface: [engine, docs]
invalidates:
  - "Was true: nothing on a job said whether it touches a protected path before it started; only a job the start rules judged carried a mixed reason, and only a started job carried a plan's hold. Is true now: every Backlog job in the queue carries `protected {paths}`, the protected paths of the rules it would start under (ask-first path lines and never paths, as written) that its issue, comments, plan or files name, matched as a start matches them; an empty list names none, and absent is not judged. The app announces it with the hello word `factory.job_protected`."
---

A view such as "small and safe" can leave out Backlog jobs that touch a
protected path, by the same rules a start would hold them to.
