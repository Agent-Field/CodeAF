---
kind: added
title: App clients can read a repository's issues through the Factory's GitHub connection
pr: 2419
surface: [engine, factory]
invalidates:
  - "Was true: an app could read a repository's issues only through GitHub's public API or the Factory's open-issue search. Is true now: factory.repo_issues lists issues in every state, updated since a time, through the Factory's GitHub connection, so private repositories work."
---

`factory.repo_issues {factory, repo, since?, page?}` lists one repository's
GitHub issues in every state, oldest update first, through the caller's Factory
connection. The hello feature is `factory.repo_issues`; the capability is
`repo_issues`. The read accepts an optional RFC 3339 lower bound and `page`
from 1–10, default 1, with at most 50 GitHub rows each. Bodies are cut at
8,192 bytes on a rune boundary, marked by `body_cut: true`. This replaces
the earlier 4,000-character limit, preserving matching words later in that prefix.

Pull requests are excluded from `issues` while still contributing to `rows`
and `newest`, so a client can advance its cursor through them. GitHub's rate
limit on the issues request answers `busy` with `detail.retry_at`; a rate
limit during repository visibility checking currently answers `not_found`,
as on every repository route. A missing Factory route answers
`conflict` with reason `factory_too_old`, distinct from a repository's
`not_found`. The route itself stores nothing and appends no activity; the Factory's
ordinary per-request housekeeping runs as it does for every authenticated request.
This app wire read adds no chat or resident surface, so neither shipped manual
corpus changes.
