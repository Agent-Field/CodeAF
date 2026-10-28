---
pr: 2008
kind: fixed
title: A start whose model has no key is refused before it runs
surface: [engine]
invalidates:
  - "Was true: a job whose model was on a service with no key started, then failed at its first step's first call. Is true: on a solo factory, a person's Start, Run or Start now is refused before any write when neither the job's model nor the profile's first fallback is on a service with a key, saying which service needs one."
---

A solo factory, which runs on its person's own profile, checks the job's model (or the
default model, when the job names none) and the first fallback before a
start writes anything. When neither can authenticate, the start is refused
with the model, its service and what to do; a key, a sign-in or a keyed
fallback lets it start as before. A team server never checks: it holds none
of its people's keys. Nor is a job on automatic models checked, since its
role routing may use other models that have keys.
