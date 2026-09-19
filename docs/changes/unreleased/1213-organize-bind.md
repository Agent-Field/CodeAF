---
kind: changed
title: standing tick organizes from ingested journals
pr: 1213
surface: [chat, engine]
invalidates:
  - "v3Organizer() returned nil, so tick and the standing pass left observe_and_organize pending. Production now ingests world session journals into discovery.db, calls RoleOrganize through Agent.Organize, and validates a typed ActionPlan before apply."
  - "wsdiscover.Store.Ingest was reached only from tests. The door organizer now calls it; a down embedder still stores passages, finishes deferred with discovery delayed, and never installs dummy vectors or FakeEmbedder."
---

Keyword-only and degraded plans record no-action rather than inventing
membership. A constructor that cannot bind still leaves the job pending.
