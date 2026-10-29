---
kind: fixed
title: A window displaced by a newer hold on the same computer lets go at once
pr: 0
surface: [chat, engine]
invalidates:
  - "A window displaced by a newer hold on the same computer kept reporting its hold, and kept its send receipt, until a heartbeat. It now retires its own receipt and wakes readers as soon as it learns of the takeover."
---

Ownership watches and work admission both stop reporting the displaced window's
hold before notifying readers. Repeated identical readings do not repeat the
notification, and the displaced window continues to refuse work until admission
acquires a new fence.

A temporary lease lapse keeps the receipt needed to resume the same fence.
Retiring a displaced window preserves a newer local writer's receipt.
A delayed refusal also preserves this window's reacquired receipt when the relay
still names that hold. A replacement fence that has expired or been released
still retires the displaced window's receipt, since the older fence cannot resume.
