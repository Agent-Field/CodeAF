---
pr: 2008
kind: fixed
title: A start whose model has no key is refused before it runs
surface: [engine, docs]
invalidates:
  - "Was true: a job whose model was on a service with no key started, then failed at its first step's first call. Is true: on a solo factory, a person's Start, Run, Start now or Run again is refused before any write when the job's model has no key, saying which service needs one."
---

A solo factory, which runs on its person's own profile, checks the job's
model (or the default model, when the job names none) before a start writes
anything. Only that model counts: a call on a service with no key fails before
it is sent and is reported, never moved to a fallback model, so a fallback
with a key would not be asked. When the model cannot authenticate, the start
is refused with the model, its service and what to do. A team server never
checks: it holds none of its people's keys. Nor does a profile set to one
model for every task, whose own check refuses a model it cannot reach.
