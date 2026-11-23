---
kind: fixed
title: A displaced window keeps its unsent turns, and a taken chat can be taken again
pr: 2091
surface: [remote, chat, docs]
invalidates:
  - "When an ownership check found another device holding the chat, Drive.CheckLease made the window a viewer without telling the batcher, so unsent turns were not kept as a branch then; now the hold is retired at once and Batcher.Displaced resolves pending work before the window names the new holder, once."
  - "A publish refused with a stale fence announced 'another device continued this chat' and then '<device> continued this chat'; OnLost now only retires the hold, and the window speaks once, after the branch."
  - "Materialize left the sender's earlier .cell/published.json in place, naming a head this store never fetched, so the next take refused with 'snapshot … is not held in this store'; Materialize now records the received head as published."
  - "The conversation tip read 'space space takes you back to home'; it now reads 'space space brings you back to home'."
---
