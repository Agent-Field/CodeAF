---
kind: added
title: Apps read a repository's issues through the Factory; a GitHub rate limit answers retry
pr: 2618
surface: [engine, factory, app]
invalidates:
  - "Was true: an app could read a repository's issues only through GitHub's public API or the Factory's open-issue search. Is true now: factory.repo_issues lists issues in every state, updated since a time, through the Factory's GitHub connection, so private repositories work."
  - "Was true: a rate limit during the Factory's repository visibility check answered not_found on every repository route, and the denial was cached for ten minutes. Is true now: it answers busy with detail.retry_at on factory.repo_issues (busy on other calls), and the next check asks GitHub again."
  - "Was true: a GitHub 429 on the Factory's visibility check without X-RateLimit-Remaining or Retry-After read as repository not found and was cached as a denial. Is true now: every 429 answers busy with retry_at and the next read checks GitHub again."
---

`factory.repo_issues {factory, repo, since?, page?}` lists one repository's
GitHub issues in every state, oldest update first, through the caller's Factory
connection. The hello feature is `factory.repo_issues`; the capability is
`repo_issues`. The read accepts an optional RFC 3339 lower bound and `page`
from 1–10, default 1, with at most 50 GitHub rows each. Bodies are cut at
8,192 bytes on a rune boundary, marked by `body_cut: true`. Pull requests are
excluded from `issues` while still contributing to `rows` and `newest`, so a
client can advance its cursor through them. A missing Factory route answers
`conflict` with reason `factory_too_old`, distinct from a repository's
`not_found`. The route stores nothing and appends no activity.

A sign-in with no GitHub credential the route can use, such as a team Factory
attached with a pasted access token and no GitHub sign-in, answers
`github_expired` and keeps the sign-in: the Factory names the reason and sends
no bearer challenge, and the engine reads any unchallenged 401 from this route
that way. Only the Factory refusing the sign-in itself is `signed_out`, which
forgets a restored sign-in, so a background issue poll can no longer sign a
person out of a Factory that still accepts them.

A spent GitHub rate limit, on the issues request or on the repository
visibility check behind any repository route, is any 429, or a 403 with
`X-RateLimit-Remaining: 0` or a `Retry-After`. It answers HTTP 429 with
`retry_at` (GitHub's reset or `Retry-After` when given, otherwise one minute
after the Factory's clock) and is never cached as a denial. Job lookups behind
visibility still answer a hidden job the same as a missing one.

This app wire read adds no chat or resident surface, so neither shipped manual
corpus changes. Carried from #2419, with the visibility parts of #2439 and
#2460, which merged only into the closed integration branch of #2306.
