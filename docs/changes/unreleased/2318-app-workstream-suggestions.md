---
kind: added
title: Workstream agents suggest edits on your pages for you to keep or drop
pr: 2318
surface: [app]
invalidates:
  - "The agent could not act on the person's pages at all; it now suggests edits there that the person keeps or drops."
  - "collections.db previously wrote schema 3; its first successful write now upgrades it to schema 4, which engines built before this change refuse."
---

The agent's `suggest_page_edit` tool proposes changes on a person's page without
editing it. Keep or drop individual edits or all proposed edits through the wire
contract. Each keep saves one page version attributed to the person and its
suggestion; dropping leaves the page unchanged. Stale edits refuse atomically,
while remaining edits in an unchanged block move with kept text.

Suggestions add two tables on the first successful database write. Reads leave
schema-3 files unchanged. Engines built before this change refuse schema-4
collections databases with the existing unsupported-database error.
