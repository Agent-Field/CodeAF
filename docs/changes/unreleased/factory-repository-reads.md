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
state. A failed refresh retains the stored list and reports the error; a refused
GitHub identity cannot receive the stored list. Conditional repository walks
skip GraphQL when every page answers 304 and previous counts are available.
