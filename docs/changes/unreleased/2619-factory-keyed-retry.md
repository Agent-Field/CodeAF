---
kind: fixed
title: A keyed Factory create runs again when it wrote nothing; a rate-limited poll says so
pr: 2619
surface: [engine, factory]
invalidates:
  - "Was true: a keyed Factory job create replayed its first answer for 24 hours, even a server error or refusal given before anything was written. Is true now: such an answer carries X-Codeaf-Phase pre-mutation and is not kept, so the same key runs the create again."
  - "Was true: GET /v1/features listed group_policy and waits_on, or nothing on a factory without a store. Is true now: it also lists keyed_create, on every factory with this rule."
  - "Was true: a source poll that GitHub rate limited read not reachable. Is true now: a 429, or a 403 that says when to retry, reads rate limited."
---

A job create (`POST /v1/jobs`, `POST /v1/issues`) sent with an
`Idempotency-Key` now keeps only the answers it gave after its first write.
An answer given before then, whether a refusal, a sign-in or read failure,
GitHub refusing the issue or GitHub not answering before the issue request was
sent, carries `X-Codeaf-Phase: pre-mutation` and is not kept. A resend with the
same key then runs the create again and succeeds once the cause has passed.
An answer given once the issue request may have reached GitHub, or once the job
exists, is still replayed for 24 hours, a 5xx or 408 included, so a resend
under the same sign-in does not make a second job or GitHub issue. Two gaps
remain: a resend under a new bearer or session cookie is a different caller,
and a Factory that stops after the create commits but before its answer is
saved runs the resend again. Unkeyed creates carry the same marker.
Worker and runner routes keep their rule: a 5xx, 408 or 429 is never kept.

`GET /v1/features` (`factory.features`) lists `keyed_create`, so an engine can
tell this Factory from an older one, which keeps every keyed answer for 24
hours, refusals and server errors included, and send a create key only here.

A source poll's trouble words say "rate limited" for any GitHub 429 and for a
403 that carries a retry time; a 403 without one still reads "not reachable".

The poll words are carried from #2460. The keyed rule replaces #2439's, which
stopped keeping every 5xx, 408 and 429, so a create that failed after making
its job or issue ran again and made a second one. Both merged only into the
closed integration branch of #2306.
