---
kind: fixed
title: A Factory rate limit or server error no longer reads as a missing repository or replays for a day
pr: 2439
surface: [engine, factory]
invalidates:
  - "Was true: a rate limit during the Factory's repository visibility check answered not_found on every repository route, and the denial was cached for ten minutes. Is true now: it answers busy with detail.retry_at on factory.repo_issues (busy on other calls), and the next check after the reset asks GitHub again."
  - "Was true: a keyed Factory request replayed its first answer for 24 hours even when that answer was a server error. Is true now: 5xx, 408 and 429 answers are not replayed, so a retry with the same key runs again."
---

A spent GitHub rate limit on the repository visibility check (403 or 429
with `X-RateLimit-Remaining: 0` or a `Retry-After`) now answers HTTP 429 with
`retry_at` on the repository routes, in the issues read's existing shape, and is
never cached as a denial. Job lookups behind visibility still answer a hidden job
the same as a missing one. A source poll that fails on a spent limit says
"rate limited". Under an Idempotency-Key, 5xx, 408 and 429 answers are no longer
kept or replayed, including rows stored by an older build; successes and other
refusals still replay for 24 hours.
