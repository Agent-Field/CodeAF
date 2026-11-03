---
kind: fixed
title: Factory retries start without waiting for a worker timeout
pr: 2230
surface: [engine]
invalidates:
  - "Was true: with multiple worker slots, Try again could leave a job running with its selected step pending until the replacement worker timed out. Is true now: the replacement waits for the previous worker's local cleanup before starting the selected round."
---

Try again no longer leaves a job showing running while its next round is
pending for about a minute. Completed earlier steps stay completed, and the
selected round runs without being skipped during timeout recovery. Other jobs
continue to use the computer's available worker slots.
