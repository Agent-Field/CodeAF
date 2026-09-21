---
kind: fixed
title: a usage flush says whether it drained or ran out of time
pr: 1285
surface: [engine]
invalidates:
  - "FlushUsage waited under a ceiling and returned the same way whether every row had reached the disk or the deadline had fired, so a caller could not tell a join from a timeout and went on as though the writing were done."
  - "It now answers true only when everything in front of it was written. A caller that needs a join rather than a wait can stop one path's writer and wait for it with StopUsageWriter, the single-path form of the close the process already performs."
---

The wait itself is unchanged and deliberate: a stalled ledger must not hold a
terminal open. What was missing is the one thing the caller cannot work out for
itself.
