---
kind: internal
title: a replanning design, a baseline bench of the run engine, and an off-by-default root seat switch
pr: 1423
surface: [engine, docs]
invalidates:
  - "A run's root was said to ride the plan seat. A root with no children yet is a work leaf, so its first turn, where it decides whether and how to split, rides the worker seat; CODEAF_EXPERIMENT_ROOT_PLAN_SEAT seats it on the plan seat for an experiment, and it is off by default."
  - "bench/bashloop's bash-belt arm on the task door was read as a measurement of the run engine. That driver never links internal/run, so its task door took the legacy node road; bench/replan drives the run engine through codeaf do."
---

docs/design/replan/DESIGN.md carries the design, its critique and the baseline:
checks were most of the bill, most of that went to checks the store's gate
would not let answer, and no replanning trigger's signal fired once.
