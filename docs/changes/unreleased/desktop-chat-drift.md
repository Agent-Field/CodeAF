---
kind: added
title: Places stream weighs settled chats by both anchors without moving their primary
surface: [engine, desktop, docs]
invalidates:
  - "The stream stage only filed new chats from START. It now exposes Drift for already-homed chats: both anchors earn uncapped extra homes, while a different primary is only proposed."
  - "A chat already considered for filing had no organiser continuation hook. The bridge now passes real END text through an optional Settled hook after its existing settle wait, including later turns. Desktop startup still needs the organiser caller wired; automatic live drift is not shipped by this lane."
---

The recorded primary is preserved even when neither anchor still earns it.
Repeated extra homes and another home in an occupied branch are suppressed.
An absent END, a probe, or an unfiled chat does not produce drift operations.
The hook adds no timer, and the stream stage adds no embedding door.
