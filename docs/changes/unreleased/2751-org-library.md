---
kind: added
title: An organization's Library, added to by a person and read by everyone and every server chat
pr: 2751
surface: [factory, engine]
invalidates:
  - "An organization had no Library: the Library was Personal only and absent from an organization window, and the manual said so. An organization now keeps one on its server (factory.org_library, hello word org_library) that everyone in it reads."
  - "Nothing could put a page into a shared Library. A person adds one with Add to Library (factory.org_library_add); nothing else does, and the organization's Library learns nothing from chats."
  - "A chat on an organization's server could not look anything up beyond its workstream. Every one now carries the read-only org_library tool."
---

**Also invalidates (v2 ruling C10):** "nobody else in the workstream sees a chat". A chat inside a workstream is still private in what is said, but its name, its state and how many tasks it has are seen by everyone in the workstream. `workstream-chats-and-tools` now has a who-sees-what table. Chats in My space stay wholly private, their names included. The Library home's **Add files** (v2 J5) puts text and Markdown files straight in as pages.
