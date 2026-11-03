---
kind: added
title: Workstream agents suggest edits on your pages for you to keep or drop
pr: 2318
surface: [app]
invalidates:
  - "The agent could not act on the person's pages at all; it now suggests edits there that the person keeps or drops."
  - "collections.db previously wrote schema 5 for Sources; its first successful write now upgrades it to schema 6, which engines built before this change refuse."
---

The agent's `suggest_page_edit` tool proposes changes on a person's page without
editing it. Keep or drop individual edits or all proposed edits through the wire
contract. Each keep saves one page version attributed to the person and its
suggestion; dropping leaves the page unchanged. Stale edits refuse atomically,
while remaining edits in an unchanged block move with kept text.

The Suggestions addition (v0.5) adds the suggestion methods to the wire contract.
The first successful database write upgrades schema-5 files from the Sources
build to schema 6, adding two suggestion tables and preserving wake and source
rows. Schemas 1–4 apply each missing migration in order. Reads leave older files
unchanged; a failed write rolls back migration too.
Engines built before this change refuse schema-6 collections databases with
the existing unsupported-database error.
