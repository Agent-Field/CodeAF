---
kind: changed
title: background jobs are their own section, named, paged, and stoppable
pr: 439
surface: [chat]
invalidates:
  - "A background job was a row among the task families on the column. It is now a third section on the same column, under `tasks` and `standing`, labelled `jobs`, collapsed by default to one line of counts."
  - "A job was named after its command, cut to three words. The name is three or four words from a cheap model; the row wears the command until that name arrives, and `job 3` is still the handle."
  - "A job's row carried its log path as `job 3 · log /…/3.log`, folded under a finished row. The absolute path is off the row; it is on the job's page, copied with `c`."
  - "Opening a job gave you a page that refused to be a chat (`this log grows as the job works — say it to main`, `a background job keeps a log, not a transcript`). It is a full-frame card with no composer, so there is nothing to refuse."
  - "A job could not be stopped from the surface; the only way was to ask aforge to run `jobs kill`. Open the job's page and press `x`. `Agent.Cancel` takes `job:3`."
---

Jobs were filed among the families because the roster was the only door. They
have a section of their own now, a page that is a card rather than a room, and
a stop that a person can reach without asking the model.
