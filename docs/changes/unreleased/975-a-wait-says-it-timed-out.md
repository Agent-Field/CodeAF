---
kind: fixed
title: a test wait that runs out says it timed out, instead of naming a cause it never saw
pr: 975
surface: [engine]
invalidates:
  - "`internal/session`'s test helpers `ranNodes.await` and `heldProposal.await` failed with `no node ran` and `the held proposal never returned` — claims about a seam, from a helper that can only observe that nothing arrived on a channel. They say what they waited for and that it was a TIMEOUT now, spelled once in `awaitTimeoutWord` and pinned by a test. A red on either used to send a reader into the task graph; it now sends them to run the test alone first."
  - "Both carried a fixed five-second deadline, which is the stopwatch `PERF.md` bans in a gate and for the same reason: on a loaded box it reported the last arrival rather than the fault. The wait is `awaitPatience` now — a quarter of what the run's own `-timeout` has left, floored at five seconds and capped at a minute — so raising `-timeout`, which this repository already tells people to do on a loaded box, buys room in these helpers too. The number is never raised in the file again."
---

The two helpers are 63 call sites across 18 files, which is why one honest helper
is worth more than 63 edited messages. #968 carries the convention this is the
worked example of: a deadline branch reports what it observed, the diagnosis is
left to whoever has the graph in front of them, and a flake is never answered by
raising the deadline — thirty seconds only makes the same wrong sentence take six
times as long to arrive.
