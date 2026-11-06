---
kind: fixed
title: a GitHub 429 without rate-limit headers is a rate limit, not a missing repository
pr: 2460
surface: [factory]
invalidates:
  - "Was true: a GitHub 429 on the Factory's visibility check without X-RateLimit-Remaining or Retry-After read as repository not found and was cached as a denial. Is true now: every 429 answers busy with retry_at and the next read checks GitHub again."
---

The repository issues read answers a bare 429 the same way instead of a 502, and a poll's
trouble words say rate limited. Without a reset or Retry-After hint, retry_at is one minute
after the Factory's clock. A 403 is a rate limit only with the spent-limit headers.
