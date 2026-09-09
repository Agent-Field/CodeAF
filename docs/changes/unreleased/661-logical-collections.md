---
kind: added
title: Organize existing records through independent logical collections
pr: 661
surface: [engine, chat, docs]
invalidates:
  - "Organization was derived from filesystem and conversation locations. The collections command now stores independent logical membership without moving those records or changing execution ownership."
  - "A folder reference in chat still means filesystem context. Logical collections are a separate local command and do not yet alter the dashboard, inject instructions or coordinate conversations."
---

The new store owns names and memberships only. A chat, task, ongoing item or
artifact can be referenced from several collections. Task references retain their
session ID, nested collections reject cycles atomically, and removal detaches a
reference without stopping or deleting its target. Optional learned memory and
model availability do not control this store.

The agreed broader personal-AI model and the first slice's boundaries are recorded
in `docs/design/workspace-foundation/`.
