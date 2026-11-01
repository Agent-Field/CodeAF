---
kind: changed
title: Giving back a taken-over factory job runs its test and review again on the person's commits
pr: 2112
surface: [engine, docs]
invalidates:
  - "Resume pipeline after Take over, before a pull request was open, restarted only the step the job was paused at, so a test and review that had already passed never saw the person's commits. It now goes back to the first check or review that had passed and runs it and every step after it."
---

The hello says `factory.job_give_back_checks`, and the job page offers Take
over only where it does.
