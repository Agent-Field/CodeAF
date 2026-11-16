---
kind: fixed
title: an organization workstream made with no pages starts with an Overview page
pr: 2657
surface: [engine, factory]
invalidates:
  - "Was true: POST /v1/workstreams kept exactly the pages the request carried, so a client other than the desk (which sends its own Overview) made a workstream with no pages that nobody could write in. Is true now: when the create carries no pages the server adds one, titled \"Overview\", kind overview, empty — the same page the desk sends. A create that carries pages keeps exactly those, and an import is unchanged."
---
