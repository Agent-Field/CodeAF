---
pr: 2008
kind: fixed
title: A start whose model has no key is refused before it runs
surface: [engine, docs]
invalidates:
  - "Was true: a job whose model was on a service with no key started, then failed at its first step's first call. Is true: on a solo factory, a person's Start, Run, Start now or Run again is refused before any write when no model the job could answer on has a key: its model, plus the profile's first two fallbacks on automatic models (the default), saying which service needs one."
---

A solo factory, which runs on its person's own profile, checks the job's
model (or the default model, when the job names none) before a start writes
anything. On automatic models, which is also a job that never chose, a refused
call may move to the profile's fallbacks, so the first two of them (the
provider's limit) count as well; with automatic models off, only the job's
model counts. When none of those can authenticate, the start is refused with
the model, its service and what to do. A team server never checks: it holds
none of its people's keys. Nor does a profile set to one model for every task,
whose own check refuses a model it cannot reach.
