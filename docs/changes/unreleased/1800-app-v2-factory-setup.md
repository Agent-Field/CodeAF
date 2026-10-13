---
kind: added
title: Factory first-run progress, access-link opening and Running day facts
pr: 1800
surface: [chat, engine, remote, docs]
invalidates:
  - "Factory first run polled simulated setup state. Native setup routes now retain GitHub discovery, repository selection, issue ingestion, checkout and retry progress; imported issues wait in Backlog for a person's start."
  - "Opening an access link had no validity or joined read. Native access routes now expose those states through caller-bound GitHub device sign-in, and the app host retains the session and attachment without returning the credential."
  - "Running retention and today's counts were app fixtures. The binary now owns display.running_visible and the 24-hour merged-job boundary, and serves visibility-filtered calendar-day summaries."
---

Publishes the additive factory adapters and their shapes in generated wire.ts.
Setup progress excludes credentials, uses the existing issue merge identity,
and persists page-wise progress. Failed reads wait for an explicit retry;
rate-limit deadlines resume even without an open setup page. Checkout origin is checked before
keeping it, and Git cloning runs on the serving machine. Native setup and repo
mutations require operator authority; team discovery uses the read grant.
