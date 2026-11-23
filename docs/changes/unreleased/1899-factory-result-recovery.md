---
pr: 1899
kind: fixed
title: Reconcile completed factory reports without repeating work
surface: [engine, chat]
invalidates:
  - "Was true: a completed Follow PR assessment could reset its local retry counter and have the service reject the no-change result as a different attempt. Is true: transport keeps the held attempt identity, and guarded completion acknowledges only the captured feedback revisions without rolling newer observations backward."
  - "Was true: a rejected worker report lost its refusal detail and stayed parked even after restart or a generic resume. Is true: the runner retains the refusal, performs bounded reconciliation before new work, and Retry saved results grants another bounded delivery attempt."
---

Retained result recovery checks the original machine and owner, held lease,
current phase round and start, PR head and feedback event. A durable receipt is
written with the accepted results and spending, so a lost acknowledgement cannot
charge for the original report again. Later recorded usage is reconciled by its
cumulative amount. Successful recovery preserves private evidence and releases
the old lease; further work uses the normal pull door. Newer decisions, manual
control, stopped work, replacement leases, stale heads and incomplete reports
remain fenced. App-owned local reports survive the loopback listener changing
ports across an upgrade; remote origins must still match exactly.

The desktop presents this internal reporting block separately from a question:
Blocked is red, step and conversation states agree, and retrying retained results
does not rerun a completed model turn or test suite. Isolated regression fixtures
cover the legacy round-37 no-change outcome, accepted-result deduplication,
cumulative late spending, bounded automatic recovery, explicit retries and stale
state refusals. The running app and live jobs are never used as test fixtures.
