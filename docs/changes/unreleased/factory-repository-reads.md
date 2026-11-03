---
kind: fixed
title: Factory repository reads reuse persisted discovery and unchanged PR files
surface: [factory, engine, app]
invalidates:
  - "Every repository read walked GitHub and counted issues again. Reads now reuse the last complete identity-scoped list across restarts and refresh it behind requests at five-minute intervals."
  - "Every pipeline pull request read fetched each candidate's files. The existing bounded read cache now reuses files for unchanged head and base SHAs."
---

The additive `available_read_at` field reports the discovery time through the
factory HTTP response and app wire. Watched repositories remain live Store
state. A transient refresh failure serves the stored list with its unchanged
`available_read_at`; a lost, expired, revoked or refused GitHub sign-in cannot
receive the stored list. With nothing stored, the read still reports the failure.
Failed walks may retry on a read after 30 seconds, rather than waiting five
minutes; successful walks restore the five-minute interval. Conditional repository
walks skip GraphQL only when every page answers 304 and the complete current
page contents match the counted snapshot. A partially failed walk cannot restore
a removed repository. Setup discovery preserves registered checkout folders
through the shared checkout attachment seam. Transient identity-lookup failures
can serve the saved list and its read time only with unchanged credentials;
refused sign-in still fails, including a refusal from an earlier discovery.
With no GitHub connection the read answers the watched repositories and no
available list, as it did before the list was stored.
