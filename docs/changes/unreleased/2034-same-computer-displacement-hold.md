---
kind: fixed
title: A window displaced by a newer hold on the same computer lets go at once
pr: 2034
surface: [chat, engine]
invalidates:
  - "A window displaced by a newer hold on the same computer kept reporting its hold, and kept its send receipt, until a heartbeat. It now retires its own receipt and wakes readers as soon as it learns of the takeover."
---

Ownership watches and work admission close the displaced window's work gate
first, before receipt I/O can block. They then retire its receipt before notifying
readers. Repeated identical readings do not repeat the notification, and the
displaced window continues to refuse work until ownership is confirmed again.

Only the receipt held when the relay reading was made is retired; a newer local
writer's receipt or this window's concurrently reacquired receipt survives.
A reading that still names this window's own device and fence keeps its receipt,
even when lapsed or released, because a heartbeat can still renew that fence.
A different device or fence retires the displaced receipt regardless of expiry;
a batcher branch supersession without a fresh reading also retires it.
