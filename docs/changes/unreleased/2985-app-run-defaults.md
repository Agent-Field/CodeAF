---
kind: fixed
title: Check points to invalid multi-record run defaults
pr: 2985
surface: [app, engine]
invalidates:
  - An object-shaped multi-record run default passed definition validation and failed when Check tried the run on example records.
---

Check points to the affected default and explains that it takes a query string or a list of record IDs.
