---
kind: internal
title: the tui3 test harness no longer waits out a waiter its fake proves empty
pr: 1397
surface: [build]
invalidates:
  - "About 80% of the internal/tui3 suite was the harness waiting 150ms per drive step for waiters that could never answer; the serial run took 341s. A synchronous fake now registers its channels and an empty one costs nothing, so the serial run is about 82s on an eight-core box."
  - "A new test fake that hands the surface a channel is not free by default. It keeps the full 150ms budget until it implements harnessWaiterQueue, and it must not implement it for a channel any goroutine writes."
  - "pulseTick and taskPaneFollow called tea.Tick directly. Both go through surfaceTick now, so the test clock declines them instead of the harness waiting out a real timer."
---

The waiter budget itself did not move: #463's finding that a shorter budget is a
bet on the scheduler still stands. What changed is that an empty channel is a
fact the fake can state on the test goroutine.
