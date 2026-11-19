---
kind: changed
title: A factory stage round may run for eight hours before it parks for you, up from two
pr: 2842
surface: [factory, docs]
invalidates:
  - One round of a factory chat stage ran for at most two hours before the job parked and waited for a person; the wall is now eight hours.
---

The wall is a backstop for a round that has stopped making progress. The job's
dollar cap (default $5, changeable per job or per repository with
`default_budget`) still bounds what a round can spend, so the longer wall lets
long, honest rounds finish without raising what a job may cost.
