---
kind: fixed
title: A finished change is checked against the ask-first and never rules before it is published
pr: 0
surface: [engine, docs]
invalidates:
  - "Was true: a job's ask-first and never path rules were checked against what its plan declared, at the approve step right after a step named `plan`, and only while that step was on. A write that touched a guarded file the plan never named, a pipeline whose plan was looked over by another step before its approval, or a plan approval turned off, all let the job publish (in Auto, or as a habit with no approve step) without asking. Is true now: when a job lands, the files its change really touches are checked against the same rules, and a hit holds it for the person, with the rule and the file (`A rule asks first: ask first · touch auth/ · auth/login.go`, or `never · infra/ · infra/main.tf`); Auto stops there and a job with no approve step does not ship itself. The plan's approval is the first approve step after the plan step, whatever the steps between are called. Under a path rule that approval runs even when it is turned off, and passes on its own unless the plan touches what a rule guards."
---
