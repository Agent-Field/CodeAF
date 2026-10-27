---
kind: changed
title: The desktop's order list and process output read less on every refresh
pr: 2006
surface: [engine]
invalidates:
  - "A warm orders list ran one identical Asking query per automation (2 + A queries). It now reads the asking runs once and shares that snapshot across list, ask and watch projections: 3 queries for any number of orders."
  - "A followed process re-opened and re-scanned its whole spool every 500 ms, even after the output had ended. It now keeps a byte offset: a drained spool costs one stat per tick, and only new bytes are read."
---

Approval ids, latest runs and the public output line cursor are unchanged. A
replaced, truncated or size-preserving rewritten spool starts a new read from
its beginning, so no line is lost or repeated.
