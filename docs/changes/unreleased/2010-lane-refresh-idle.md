---
kind: fixed
title: Rest lane refresh between routing requests
pr: 2010
surface: [engine, chat]
invalidates:
  - "Previously an open engine refreshed remembered models every five minutes; now startup warms once and periodic refresh pauses after one interval without routing."
  - "Previously callers declared work lifetimes to wake the lane beat; now actual sends and probe selections record demand."
---

Lane sheets still warm at startup. Sends and probe selections keep the shared
five-minute beat active, including independent pool judging, factory triage and
typing probes. After a full interval without routing, the beat stops its timer
and performs no periodic sheet refresh or lane-ledger reload. The first stale
selection starts one coalesced background catch-up and routes on existing
beliefs without waiting, even if the call ends before the beat runs.

Model joins still fetch immediately while idle. Remembered models, rows, routing
gates, support backoff and terminal/app behavior otherwise stay the same.
The pre-existing `exec` and `plan revise` doors have no beat and remain outside
this change.
