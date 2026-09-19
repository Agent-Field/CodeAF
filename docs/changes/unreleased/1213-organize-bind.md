---
kind: changed
title: standing tick organizes from ingested journals
pr: 1213
surface: [chat, engine]
invalidates:
  - "v3Organizer() returned nil, so tick and the standing pass left observe_and_organize pending. Production now ingests world session journals into discovery.db, calls RoleOrganize through Agent.Organize, and validates a typed ActionPlan before apply."
  - "wsdiscover.Store.Ingest was reached only from tests. The door organizer now calls it; a down embedder still stores passages, finishes deferred with discovery delayed, and never installs dummy vectors or FakeEmbedder."
  - "v3OrganizePass constructed the organizer while assembling the conversation, and bindSessionEmbedder called v3Embedder against the live catalog on the way to the first frame (13 blocking reads vs the pinned 11). The organizer is bound when the standing pass fires; RoleEmbed is resolved when a vector is needed, the way generate_image already is."
  - "RoleOrganize received evidence but no folder catalog, so live J11 completed no-action even when Security passages were cited. The door now passes folder id/name lines and the add action shape (collection_id + conversation ref)."
  - "Live J11 on the catalog SHA still completed no-action because hierarchy omitted member conversation ids, so the model could not map cited chat: ids onto Security. Hierarchy now lists each folder's members; already being in another folder is not a reason for no-action."
---

Keyword-only and degraded plans record no-action rather than inventing
membership. A constructor that cannot bind still leaves the job pending.
