---
kind: fixed
title: A keyed Factory request runs again after a server error; a rate-limited poll says so
pr: 2619
surface: [engine, factory]
invalidates:
  - "Was true: a keyed Factory request replayed its first answer for 24 hours even when that answer was a server error. Is true now: 5xx, 408 and 429 answers are not replayed, so a retry with the same key runs again."
  - "Was true: a source poll that GitHub rate limited read not reachable. Is true now: a 429, or a 403 that says when to retry, reads rate limited."
---

Under an `Idempotency-Key`, 5xx, 408 and 429 answers are no longer kept or
replayed, including rows stored by an older build; successes and other
refusals still replay for 24 hours. A source poll's trouble words say
"rate limited" for any GitHub 429 and for a 403 that carries a retry time; a
403 without one still reads "not reachable".

Carried from #2439 and #2460, which merged only into the closed integration
branch of #2306.
