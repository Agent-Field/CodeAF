---
kind: changed
title: The desktop's order list and process output read less on every refresh
pr: 2006
surface: [engine]
invalidates:
  - "A warm orders list ran one identical Asking query per automation (2 + A queries). It now reads the asking runs once and shares that snapshot across list, ask and watch projections: 3 queries for any number of orders."
  - "A followed process held its spool open between 500 ms checks, blocking replacement or retention removal on Windows, and kept checking at that rate after it ended. It now closes after each read, retains byte progress, and backs off to one check every 5 seconds only when known ended and drained."
---

Approval ids, latest runs and the public output line cursor are unchanged. A
replaced, truncated or size-preserving rewritten spool starts a new read from
its beginning. Running output and unfinished lines keep 500 ms checks. Ended,
drained output backs off through 1, 2 and 5 seconds; a late flush or replacement
restores 500 ms checks when observed, with up to 5 seconds to detect it.
