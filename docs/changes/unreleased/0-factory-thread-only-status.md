---
pr: 0
kind: fixed
title: The manager's status tells a reply apart from a running step
surface: [engine]
invalidates:
  - "Was true: a job held only so its manager could answer a reply read \"running on its worker\" in the manager's status. Is true: a thread-only hold is said to run no step and take no execution slot."
---

A thread-only lease lends the manager to a reply without restarting the job's
steps. The manager's status now reads the hold's kind and says so, instead of
describing the step as running.
