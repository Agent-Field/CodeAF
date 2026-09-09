---
kind: fixed
title: Footer throughput identifies the whole-turn average
pr: 653
surface: [chat]
invalidates:
  - "The footer token rate could be mistaken for current provider generation speed. It now says tok/s avg because its denominator includes tool execution and model waiting time across the current turn."
---

The footer labels its existing rate `tok/s avg`. The calculation, rounding,
quiet-period suppression and accounting remain unchanged. The provider-specific
rate beside `via` remains a separate generation measurement. This clarification
does not claim to improve provider speed or shorten a running task.
