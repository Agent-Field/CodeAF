---
kind: fixed
title: recover transient background memory failures before reporting a failed save
pr: 1901
surface: [chat, engine]
invalidates:
  - >-
    A stalled memory request could exhaust its shared deadline without leaving
    time for recovery. The primary now reserves half the remaining patience for
    one small-work attempt, and a transiently failed pass retries the original
    exchange once after two seconds in the background.
  - >-
    The chat prompt told models that /remember was required without a remember
    tool call. It now describes automatic background capture and waits for the
    save notice before claiming success.
---

Recovery respects chat cancellation and does not persist pending exchanges across
restarts. Permanent failures are not retried. Exhausted recovery retains the
technical journal entry and names /remember as a direct save path.
