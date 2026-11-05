---
kind: fixed
title: Keep late saves from renewing expired chat holds
pr: 0
surface: [chat, engine, remote]
invalidates:
  - "Publishing a late save previously renewed an expired hold if nobody had acquired it; publication now refuses that hold and preserves the turn in a separate copy."
---

Publishing requires an expiry later than the directory time, matching the
new-work admission check. A finished call can still seal locally; the existing
stale-hold path preserves it in a branch without changing the original head,
expiry or fence. Native and hosted publish rules agree. Heartbeat renewal and
acquisition fence numbering are unchanged.

The hosted relay applies the same refusal under every lease policy, including
the frozen stage-one policy, and a released lease also refuses publication until
it is taken again. The engine and the hosted relay must ship together.
