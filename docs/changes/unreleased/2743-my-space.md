---
kind: added
title: A private My space per person in an organization, chats on its server, totals only for the operator
pr: 2743
surface: [factory, engine]
invalidates:
  - "An organization had no private place for one person beyond a private workstream. Now each person has one My space per organization (GET /v1/myspace, factory.org_myspace): a workstream marked space, private, holding documents, canvases and chats and no tasks, never listed, shared, nested, archived or deleted."
  - "An organization's chats ran on the laptop and the server never held them. A person's own chats are now kept and run on the server (factory.org_workstream_chats, _chat_new, chat on org_workstream_agent*), private to them, whatever agent_runner says."
  - "The operator was told every private workstream and canvas existed, with its name, owner and last change (name_only). The operator is now told nothing of a private item; of each person's space only spend this month and an item count (factory.org_myspace_totals)."
  - "Nothing limited what one person spent on the organization's key outside a workstream budget. A per-person monthly limit for My space (default $20, factory.org_myspace_limit_set) now pauses that space's chats."
  - "Nobody could learn that someone else had a chat in a workstream. By v2 ruling C10, factory.org_workstream_chats now also answers others: every other person's chat there as {id, workstream, title, owner, owner_name, state, last_activity}. It is metadata only and never what is said, and a space lists nobody's."
---
