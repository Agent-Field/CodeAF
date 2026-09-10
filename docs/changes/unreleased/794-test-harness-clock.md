---
kind: changed
title: test clocks stop paying for discarded waits
pr: 794
surface: [chat, engine]
invalidates:
  - "The chat test harness started real Bubble Tea timers and waited up to 120ms even when their frame or polling messages would be discarded. Its injected clock now delivers short message ticks synchronously and skips polls outside the same budget without starting timers."
  - "Steer, watch, wall-clock, and folder-preview tests waited through product-duration sleeps or hundreds of rendered key turns. Their assertions now advance injected clocks, wait for completed loops, or begin next to the boundary they exercise."
---

Product timing constants and the 150ms channel-waiter budget are unchanged.
