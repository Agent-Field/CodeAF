---
kind: fixed
title: a watched repository with no jobs yet reads the checkout setup recorded
pr: 2171
surface: [engine]
invalidates:
  - "Was true: a repository setup cloned, or a folder the person chose in setup, read checked_out false with no checkout in the queue's repositories until the repository's first job existed, so the app said it would clone on the first job. Is true now: every watched repository in GET /v1/queue and factory.queue carries the folder the factory's checkout record holds for it, with checked_out true, whether it has jobs or not."
---
