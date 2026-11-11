---
kind: fixed
title: a call stopped before its first streamed word keeps its charge instead of losing it
pr: 2566
surface: [chat, engine, docs]
invalidates:
  - >-
    A paid streamed call cancelled before its first SSE frame arrived (Esc, a
    quit, a job withdrawn) lost its charge: the socket was cut at once, the
    provider's generation id never arrived, and the ledger got neither a priced
    `reconciled` row nor the `unbilled` marker. Now a call that reconciles
    receipts keeps its stream open for at most 100 ms after the cancel and cuts
    it the moment the first frame names the generation, so that call's receipt
    is asked for and booked. A call with nothing to reconcile, and a direct
    service, are still cut at once.
  - >-
    An abandoned hedged race released its caller's pending-spend hold as soon as
    it returned, before its cut arms had queued their receipts, so work waiting
    on pending spend before closing its books could see nothing owed and stop
    waiting. The race now holds one pending token until its arms have reported,
    or for at most the one-second drain grace when an arm never does.
---

The person's stop does not wait for the hold on a shipped build: every call is
raced, the race answers its caller at once, and only the arm keeps its socket
open. A build with no waiting controller returns only when the stream is cut, so
there a stop can wait up to the same 100 ms.

NOT FIXED HERE: receipts that are still pending only in memory when the process
exits (#2591's other half). A quit before the receipt arrives still loses the
charge, because the queued receipt dies with the process and nothing durable
records it. A cancel on a provider that sent neither an id nor any text within
the 100 ms is still reported as nothing, as before.
