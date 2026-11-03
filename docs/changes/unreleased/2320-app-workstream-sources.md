---
kind: added
title: Keep sources and agent source proposals in app workstreams
pr: 2320
surface: [app]
invalidates:
  - Collections database files now become schema 5 on their first successful write; an engine built before this change refuses them, leaving collections unavailable there and all data intact.
  - Workstream inputs can now be kept as separate sources instead of adding Repos or Files lines to why.
---

Workstreams keep file, repo, issue, link and kept-work pointers through the wire
contract's v0.4 source methods, including atomic initial sources on creation. The
agent reads sources and proposes additions for the person to keep or drop.
Removing a source leaves its target intact. Sources are separate from collection
members, with at most 2,000 per workstream; done workstreams refuse source writes.

Agent source reads refuse final-component links, including links planted after
a file was added, while allowing intermediate directory links. Create retries
return the original workstream even after an initial source disappears. Invalid
UTF-8 source request bytes are refused before decoding, and links obey the
2,048-byte bound both before and after URL normalization.

Schemas 3 and 4 remain readable without migration. The first successful write adds
the sources table and advances to schema 5 atomically, preserving existing rows.
Schema 3 first gains the wake tables; schema 4 already has them and keeps every wake.
Older engines refuse that newer database with their existing unsupported-database
error; nothing is lost. Source indexing and jobs consuming Read by jobs selections
are separate follow-ups.
