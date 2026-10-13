---
kind: changed
title: Factory job screens have binary routes, step controls and state presentation
pr: 1800
surface: [chat, engine, remote, docs]
invalidates:
  - "The factory had no step stop or rerun routes. It now changes recorded steps under the lease lock, retaining earlier results and spending while revoking the old runner."
  - "Hand-off refused issue creation and non-default pipelines. It now forwards both options to the binary job creator, which validates named pipelines and keeps GitHub receipts."
  - "The published TypeScript contract contained only app routes. It now also exports FactoryRoutes, FactoryRequest and FactoryResponse from the factory service's Go wire."
---

Adds caller-owned assignment and sync routes, bounded step editing, pipeline
review branches, issue creation/search, hand-off reads and Not now, and pending
device-code cancellation. Every job read carries binary state presentation and
caller controls. The manual describes refusals and the local-only fallback when
pipeline review has no remote or gh.
