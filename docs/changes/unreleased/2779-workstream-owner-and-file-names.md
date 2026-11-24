---
kind: fixed
title: Afi requires one task owner and Files shows people by display name
pr: 2779
surface: [chat, factory, app]
invalidates:
  - Afi previously accepted everyone as a new task owner; it now requires one person.
  - Imported files previously showed their maker handle; reads now resolve the current display name.
---

Afi names the person who may own a task when refusing a shared owner. Existing
shared-owner tasks remain readable. Files resolves makers on every read, including
files imported before this change.
