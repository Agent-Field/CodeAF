---
kind: fixed
title: Saved-key sync returns vault read failures instead of panicking
pr: 0
surface: [engine, app]
invalidates:
  - "Checking whether a saved key was removed previously could panic on a local vault read failure; it now returns the read error."
---

A failed local vault read during a removal check now returns its error, allowing
background sync to retry rather than ending its worker. This does not repair a
damaged vault or change which saved keys travel between computers.
