---
kind: fixed
title: Keep billing facts and missing receipts when taking a chat
pr: 2386
surface: [chat, engine, remote, app, factory, docs]
invalidates:
  - "Taking a chat used to lose receipt billing facts and Factory job attribution; newly saved calls now retain them."
  - "Missing-receipt markers used to count as measured calls in some readers; their distinct kind now keeps call counts unchanged."
---

Draft implementation. Landing waits for the saved-format owner's approval.

Receipts retain optional billing, billing_basis, unbilled, and factory_job facts.
Token-free missing receipts carry kind: unbilled-marker and the ledger row's ID.
Ledger and Factory views label them unbilled with zero measured calls; call-only
exports omit them. Late receipt notes settle before the hosted handoff publishes.
Per-event transcript usage retains stated facts; aggregate usage stays unknown.

Dollars are not repriced. Taking a chat keeps global spend and saved job lifetime
spending unchanged. Old readers retain dollars and ignore the added keys, but may
count markers as calls and lose the new facts. Older receipts and already imported
rows are not enriched. The WAL V3 work on the other branch can serialize the same
cellstore.ModelCall without another saved shape.
