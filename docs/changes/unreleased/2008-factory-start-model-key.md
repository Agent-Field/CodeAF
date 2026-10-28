---
pr: 2008
kind: fixed
title: A start whose model has no key is refused before it runs
surface: [engine]
invalidates:
  - "Was true: a job whose model was on a service with no key started, then failed at its first step's first call. Is true: a person's Start, Run or Start now is refused before any write when neither the job's model nor the profile's first fallback is on a service with a key, saying which service needs one."
---

A factory that knows its person's profile checks the job's model (or the
default model, when the job names none) and the first fallback before a
start writes anything. When neither can authenticate, the start is refused
with the model, its service and what to do; a key, a sign-in or a keyed
fallback lets it start as before. A team server, which does not hold its
people's keys, makes no such claim.
