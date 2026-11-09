---
kind: added
title: A factory follows go-aheads itself — group policies and waits-on
pr: 2524
surface: [engine]
invalidates:
  - "Was true: no codeaf served factory.policy_get, factory.policy_set, factory.waits_set or factory.features, hello never carried factory.group_policy or factory.waits_set, and a job could not wait on another, so the desk said \"This codeaf can't apply go-aheads yet.\" and nothing started a job without a person. Is true now: the factory keeps each group's policy (GET/PUT /v1/policy) and a job's waits_on (POST /v1/jobs/{id}/waits_on), and after every poll starts a covered job that never ran once every job it waits on has merged, inside its allowance's cap and before its end, with no app attached and whether or not Start on its own is on."
  - "Was true: a factory on your own machine started nothing a person did not start unless Start on its own was on. Is true now: it also starts what a go-ahead covers, recorded as auto_started with detail \"allowance <id>\" and policy_run on the job."
---

`factory.features {factory}` answers `{features}` (`group_policy`, `waits_on`;
`[]` from an older factory). `factory.policy_set {factory, group, stopped,
allowances}` replaces a group's whole policy and `factory.policy_get {factory,
group}` reads it, both answering the desk's `FactoryPolicy` shape with each
allowance's `spent_usd` and `state`. `factory.waits_set {factory, job, waits_on,
expect?}` sets what one job waits on. A covered job that waits carries `blocked`;
a started one carries `policy_run`. Ending, stopping or withdrawing a go-ahead
starts nothing further and stops nothing already running.
