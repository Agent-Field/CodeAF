---
kind: fixed
title: Factory repository reads reuse persisted discovery and unchanged PR files
surface: [factory, engine, app]
invalidates:
  - "Every repository read walked GitHub and counted issues again. Reads now reuse the last complete identity-scoped list across restarts and refresh it behind requests at five-minute intervals."
  - "Every pipeline pull request read fetched each candidate's files. The existing bounded read cache now reuses files for an unchanged head SHA."
---

The additive `available_read_at` field reports the discovery time through the
factory HTTP response and app wire. Watched repositories remain live Store
state. A transient refresh failure serves the stored list with its unchanged
`available_read_at`; a lost, expired, revoked or refused GitHub sign-in cannot
receive the stored list. With nothing stored, the read still reports the failure.
Failed walks may retry on a read after 30 seconds, rather than waiting five
minutes; successful walks restore the five-minute interval. Conditional repository
walks skip GraphQL when every page answers 304 and previous counts are available.
