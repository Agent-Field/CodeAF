---
kind: added
title: App clients can comment on a job's GitHub issue and see the manager's read of each new comment
pr: 2109
surface: [engine]
invalidates:
  - "Was true: the factory only read issue comments (the last three, author, body and time) and nothing handled them beyond the step brief. Is true now: comments also carry id, url, tier and by_factory, and every comment that arrives after a job first read its issue gets one comment_reads entry (context, asked or nothing; adopted is reserved and never produced yet)."
  - "Was true: the app could not post to GitHub issues. Is true now: factory.issue_comment posts the person's words as that person, with their own factory sign-in token (the host's on a solo factory), only for jobs from a GitHub issue."
---

A comment from someone with write access on a job with a run under way is asked
about in the manager conversation instead of changing the plan, because the engine
cannot yet tell a comment that narrows scope from one that widens it.
