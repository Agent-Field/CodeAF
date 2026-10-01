---
kind: fixed
title: Startup and home conversation openings keep the interface responsive
pr: 1662
surface: [chat, engine]
invalidates:
  - "Launch assembly previously permitted eleven blocking catalog reads. It now permits zero; cold discovery also cannot hold agent creation."
  - "Home new/open and /new previously waited for engine preparation on the UI loop. Current connections now show opening feedback and accept cancellation while preparation runs."
---

Model capability checks use a nonblocking snapshot of current or cached rows,
with curated fallback rows only for the default service. Harness tool bridges
are constructed on first use. Home search ranks each matching conversation once.

A cancelled or refused conversation opening keeps the draft and current
conversation. Repeated Enter does not create duplicate openings. Legacy
connections with a shared agent retain their existing transition behavior.
