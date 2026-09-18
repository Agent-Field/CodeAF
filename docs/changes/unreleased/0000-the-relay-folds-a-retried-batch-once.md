---
kind: fixed
title: The pool relay folds a retried batch once
pr: 0000
surface: [engine]
invalidates:
  - "A resubmitted batch was folded again. The relay keyed nothing by a row's identity, so a row whose 202 the client never saw — the client holds every row that did not get one and re-sends its outbox on the next judged run and at start-up, and a timeout after the relay had already stored the batch is the ordinary case — was summed into the install's running total a second time and charged against its daily quota a second time. A row's own nonce now names it under `seen/<install>/<nonce>`, and a nonce the relay has already stored is neither folded nor charged again; a batch that was entirely already stored still answers 202 with `accepted: 0`."
---

The submit path in `relay/src/worker.js` now drops the rows whose nonce it has
already folded before the quota check and the fold, and records the nonce of
every row it folds under a per-row `seen/<install>/<nonce>` key with a one-week
TTL. The key is per row and not per batch because a retry carries the rows that
did not get a 202, which may be fewer than the batch that first sent them, and
only the nonce each row carries survives that re-grouping. Idempotence is only
as strong as KV, which is eventually consistent — two concurrent copies of a
batch can still both fold a row; within what KV answers, a stored nonce is not
folded or charged again. A nonce repeated inside one batch folds once too.
`accepted` counts the rows folded now, so a wholly remembered batch answers 202
with `accepted: 0` — the client needs only the 202 to mark its rows sent.