---
kind: added
title: A factory keeps a default job budget, a cap on automatic starts, and a default model
pr: 2166
surface: [engine]
invalidates:
  - "Was true: a job with no budget and a repository with no default_budget always got the built-in $5, a job started on its own was held only above its repository's start.max_cost or the built-in $5, and a step with no model of its own ran on the runner's launch default. Is true now: a factory's operator can set each of those for every repository (factory.spending_set, PATCH /v1/spending); a repository's own default_budget still stands, its stricter start.max_cost still holds, and a runner on one model for every task keeps that model."
---

`factory.spending {factory}` and `factory.spending_set {factory,
default_budget_usd?, auto_start_max_usd?, default_model?, expect?}` (hello
features `factory.default_budget`, `factory.auto_start_cap`,
`factory.default_model`) read and change the values over the factory's new
`GET` and `PATCH /v1/spending`, kept in the store beside the factory's rules.
Only the operator may write; a stale `expect` writes nothing. The default model
is stamped on each job when it is queued (`factory_model` on the job) so a
runner on another machine opens a step on it.
