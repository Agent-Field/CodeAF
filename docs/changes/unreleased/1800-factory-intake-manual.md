---
kind: changed
title: The factory manual explains automatic starts, asked first and stopped jobs
pr: 1800
surface: [chat, docs]
invalidates:
  - "Factory work was described as waiting for a person's Run action. Labeled forge jobs now start through intake when the repository's start rules allow their size, cost and paths."
  - "The manual did not explain asked first. A candidate outside the start rules waits for yes or no, keeps asked_first_reason after the answer, and records asked_first instead of repeatedly asking."
  - "A stopped labeled job could be assumed eligible for intake again. Its stopped event prevents automatic restarts even after a server restart; only a person's Run or go starts it again."
---

Document labels, max_size, max_cost, never, ask_first, personal tightening and
size measurement. Retrieval probes cover automatic starts, asked first, stopped
jobs and the YAML pipeline file. The record explains starts with auto_started.
