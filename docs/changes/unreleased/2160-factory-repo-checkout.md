---
kind: added
title: Each repository on the factory's queue says how a run of it gets its checkout
pr: 2160
surface: [engine, app, chat, docs]
invalidates:
  - "Was true: only a job's own read (`factory.job`, `GET /v1/jobs/{id}`) said `checkout` (`here`, `will_clone` or `missing`), so a repository with no jobs yet could not be checked before its first Start; the queue's repositories said only `checked_out` and a path. Is true now: each of `factory.queue`'s `repos[]` (`GET /v1/queue`) also carries `checkout_state` with the same three words from the same check a start makes. A repository no job has run on is `will_clone`, because the runner clones it on the first job. The word is absent on a factory that does not know its machine's checkouts, never a guess."
---

The hello says `factory.repo_checkout`. The field is `checkout_state`, not
`checkout`, because a queue repository's `checkout` already carries the
checkout's path on the factory's own machine.
