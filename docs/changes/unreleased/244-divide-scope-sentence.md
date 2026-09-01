---
kind: changed
title: a worker that cannot name exclusive file ownership is told not to divide
pr: 244
surface: [engine]
invalidates:
  - "`prompts/divide.md` treated overlapping files as advice — \"Two parts that edit the same file are not independent.\" It now has a heading of its own: EVERY PART OWNS ITS OWN FILES, and if that boundary cannot be drawn the worker is told not to call `divide_work`."
---

#241 is the prompt-level half. #231 is the admission check. This lane is the
sentence the worker reads before it asks.
