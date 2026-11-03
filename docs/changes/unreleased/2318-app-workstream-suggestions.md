---
kind: added
title: Workstream agents suggest edits on your pages for you to keep or drop
pr: 2318
surface: [app]
invalidates:
  - "The agent could not act on the person's pages at all; it now suggests edits there that the person keeps or drops."
  - "collections.db previously wrote schema 4 for the watching agent; its first successful write now upgrades it to schema 5, which engines built before this change refuse."
---

The agent's `suggest_page_edit` tool proposes changes on a person's page without
editing it. Keep or drop individual edits or all proposed edits through the wire
contract. Each keep saves one page version attributed to the person and its
suggestion; dropping leaves the page unchanged. Stale edits refuse atomically,
while remaining edits in an unchanged block move with kept text.

Wire v0.4 adds suggestions. The first successful database write upgrades
schema-4 files from the watching agent's build to schema 5, adding two suggestion
tables and preserving wake rows. Schemas 1–3 apply each missing migration in
order. Reads leave older files unchanged; a failed write rolls back migration too.
Engines built before this change refuse schema-5 collections databases with
the existing unsupported-database error.
