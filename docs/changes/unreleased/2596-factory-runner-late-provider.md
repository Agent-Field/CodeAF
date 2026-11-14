---
kind: fixed
title: A running factory runner notices provider keys added or removed after startup
pr: 2596
surface: [app, factory, docs]
invalidates:
  - "Was true: a factory runner read needs_provider only when it started, so adding or removing a provider key required restarting the runner to update its state. Is true now: the runner rereads its needs before each lease poll and refreshes its attachment when they change, without interrupting a running step."
---

A key handed over through `keys.set` lets the running factory runner lease
queued work within one lease poll. Removing its only usable provider key makes
it report `needs_provider` within one poll. An injected parent still supplies
its own provider. Key replacement does not restart a runner or change an
existing lease, and shutdown joins the same worker loops as before.
