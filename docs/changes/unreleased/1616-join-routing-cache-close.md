---
kind: fixed
title: Session close joins its routing-cache refresh
pr: 1616
surface: [engine]
invalidates:
  - "Session close only cancelled the routing-cache beat and could return while it still wrote local state. Close now joins the cancelled beat before returning."
---

Network cancellation still stops the fetch; the join finishes local cache work
before the caller can remove or switch the state directory.
