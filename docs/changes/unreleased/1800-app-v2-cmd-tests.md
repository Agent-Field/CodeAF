---
kind: fixed
title: cmd tests follow the shared task limit and model-seat command
pr: 1800
surface: [build, chat]
invalidates:
  - "The cmd tests still expected a $5 task limit and /crew guidance after A15. They now expect the $2 default and /seats; the headless daily-cap refusal also names /seats cap."
---

The /crew alias remains accepted for one release. The headless task-cap test
asserts that -yes-spend permits one $2 call under the fresh $2 task limit and
refuses the next call.
