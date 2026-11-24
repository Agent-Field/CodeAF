---
kind: added
title: an organization can move a chat, share it read only, and hears when a share runs out
pr: 2890
surface: [factory, app, docs]
invalidates:
  - "Was true: a chat never moved, and the manual said so (My space: \"A chat never moves: it stays private\"). Is true now: the owner of a chat on an organization's server can move it between My space and a workstream they may edit (factory.org_move with kind chat). Moving into My space makes it private and drops its grants; moving into a workstream keeps its privacy and grants; a chat that is answering is refused until it has stopped."
  - "Was true: Afi's pane and a task's working conversation were not resources anything could be granted on. Is true now: they are registered as session resources (afi:<workstream>:<person> and task:<workstream>:<task>), and a grant can name them. Who may read or talk to either through its existing routes is unchanged."
  - "Was true: only the person whose chat it was could read what was said in it. Is true now: a person holding view on a chat or session can read its transcript, read only (GET /v1/resources/{kind}/{id}/transcript, factory.org_transcript); the message, stop and card doors stay with its owner, and anyone else, the operator included, is told it is not there."
  - "Was true: a grant with an end date stopped granting at that moment but nobody was told, so a list kept showing the thing until it was refreshed. Is true now: the server announces a lapse once to the people who lost access, at the next end date and at least once a minute, and a lapse during downtime is announced at the next start."
  - "Was true: the factory node named itself the owner of a run with X-AgentField-Run-Owner alone, and every 401 or 403 from AgentField was worded as a bad API key. Is true now: the node also sends X-AgentField-Run-Owner-Proof, minted over its own link, on the calls that start a run, read it, acknowledge it, steer it or answer its question; a control plane that does not know the proof gets the calls as before. A refused proof is told in plain words and is not reported as a bad key."
  - "Was true: santosh/work added a chats.read method and a chunk-paged remote reader beside dev's chats.read hello word. Is true now: dev's hello word and its agents.entries behaviour are the only chats.read; the method and the reader are removed, and a client that called the method reads a remote chat through agents.entries."
---

Defaults are unchanged: nothing moves, shares or expires until someone does it. The organization's policy, the roles and the verbs are the ones the generic access model already carried; this finishes it for chats and sessions. The desktop has no sharing buttons for chats yet, and the manual says so (`internal/manual/chat/organization-sharing.md`).
