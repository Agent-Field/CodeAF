---
kind: changed
title: the relay writes a submit batch once per distinct cell and day
pr: 0000
surface: [engine]
invalidates:
  - "A folded row's identity lived under its own `seen/<install>/<nonce>` key, so a submit batch spent one KV `get` and one KV `put` per row: an install re-sending a 200-row outbox spent 400 writes and reads to fold a handful of cells. The nonces of a day now live together under one `seen/<install>/<day>` key, and a batch costs one read and one write per distinct (day, cell) it folds into plus one read and one write per day for that day's seen set and its quota counter — 12 writes for 200 rows over ten cells on one day."
---

The submit path in `relay/src/worker.js` groups a batch's rows by day and by
their sheet key before touching the store, and reads a day's seen set once
instead of once per row. `fold` now runs in memory over the rows that share a
sheet key before the key is written, so two rows of one batch in the same
(day, cell) fold against each other rather than against whatever KV last
answered. The counts per batch are writes = distinct (day, cell) keys +
distinct fresh days (the seen set) + distinct fresh days (the quota counter),
and the same order of reads; neither scales with the rows.

The idempotence contract is unchanged: a nonce already in its day's set is
neither folded nor charged again, and a wholly remembered batch still answers
202 with `accepted: 0`. One consequence of the new key shape: a nonce the old
per-row key still holds is not consulted, so a retry that straddles the deploy
may fold once more — the same eventual-consistency window the per-row key had,
now stated on one key, where two batches from one install on one day race and
the last put wins. The day's set holds at most `ROWS_PER_INSTALL_PER_DAY`
nonces (16,500 bytes at the 500 default) against KV's 25 MiB value limit, and a
quota raised past what one value holds is refused rather than allowed to drop
nonces. `docs/design/model-pool/RUNBOOK.md` now states the per-batch cost and
the plan's daily KV write limit.
