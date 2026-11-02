---
kind: changed
title: A job's issue comment posts once, says whether it posted, and skips repositories that are off
pr: 2140
surface: [engine, chat]
invalidates:
  - "Was true: two factory.issue_comment calls on one job at once both reached GitHub, so a double click posted two comments. Is true now: while one is being posted, a second on the same job is refused with 409 in_flight and nothing more is posted."
  - "Was true: a failure of POST /v1/jobs/{id}/issue_comment carried only a sentence, so the app could not tell nothing posted from a lost answer except by status. Is true now: each failure of the route carries outcome refused or unknown, and factory.issue_comment's typed refusal detail carries it as an optional outcome."
  - "Was true: a job of a repository that is turned off still took a comment on its issue. Is true now: it is refused with repo_off before GitHub is asked."
---

No new hello word: the outcome comes from the factory, which may be older
than the engine, so the app reads `detail.outcome` as optional and falls back
to the code (uncertain may have posted) when it is absent.
