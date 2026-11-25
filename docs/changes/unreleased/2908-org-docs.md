---
kind: changed
title: Organizations get an entry point, a corrected operator guide and manual sections people can find
pr: 2908
surface: [docs, chat, factory]
invalidates:
  - "Was true: the manual said a chat moved into a shared workstream appears in the workstream's list for its people, by name, owner and state. Is true now: a moved chat stays as private as it was and is listed only to the people it is shared with."
  - "Was true: the manual said a thing marked private is its owner's alone and that grants on it give nothing. Is true now: that holds for a workstream or a canvas; a grant on a private chat, Afi's pane or a hosted chat reaches the people it names and nobody else, and the workstream's people and the operator stay out unless the policy says otherwise."
  - "Was true: the manual said only the owner can stop or delete a hosted chat, and the refusal read `Only the owner of this chat can stop or delete it.` Is true now: stopping or deleting needs the manage permission, which the owner holds and a guest holds only through a role that includes it (the full role), and the refusal reads `Only this chat's owner, or someone it is shared with in full, can stop or delete it.`, which now names both people who can."
  - "Was true: the manual said a bot always starts hosted chats and reaches My space. Is true now: a bot starts them only where the organization's policy lets agents create, and only reads My space and the policy and people lists."
  - "Was true: deploy/org's README said the stack has five named volumes, that nothing long-lived runs as root, that only the owner drives a hosted chat, and that a Railway project is seven services. Is true now: six volumes (sessions-data holds the hosted chats), the sessionhost stays root on purpose, a guest drives only through the verbs the owner shared, and the Railway project is six services (seven with the keyless model stub)."
---

New: `docs/organizations.md` says what an organization is, what runs where, who can see what and how to get to a first hosted chat
in about ten minutes, and the top-level README links to it. The manual answers "can my admin read my chats", "who pays for a
shared chat", "how do I join my company's codeaf", the Personal and organization accounts, the server-down case and how to host
one, and probes keep those questions reaching those pages.
