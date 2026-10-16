---
kind: fixed
title: codeaf app no longer lists or keeps blank conversations, and never sends a zero time for an agent
pr: 0
surface: [engine]
invalidates:
  - "Was true: an agent made with no words, which mints its folder at once, was announced with agent.changed, listed by agents.list and search.all while it was held, and its folder was kept forever. Is true now: a blank conversation, one nobody has said anything in, is answered to its caller and announced to nobody, never listed, and removed at quit unless a crew names it or another window holds it. The first words, or a manager's start, are what list an agent."
  - "Was true: a crew member a manager started, held again after a relaunch, was listed with an empty title and `last_active_at: 0001-01-01T00:00:00Z`, which a reader shows as an untitled row dated in year 1. Is true now: it is listed under the word its crew calls it (its brief's first words) in the list and on every agent.changed, and it carries no `last_active_at`, because the person never spoke in it."
  - "Was true: `Agent.updated_at` and `Agent.last_active_at` were required on the wire and went out as Go's zero time when unknown, as did a search.all hit's `at` and an archived agent's fallback `archived_at`. Is true now: both Agent fields are optional (`omitzero`) and the schema no longer requires them, an unknown time is left out everywhere it was sent, and agent.changed no longer fills `last_active_at` with `updated_at`: it is only ever when the person last spoke, and a reader sorts by `updated_at` when it is absent."
---

A blank conversation cannot simply wait for its first message to be made on
disk: agents.create answers with the agent's lasting id, which is its
folder's name, and a crew adds a member by its journal's path before the
member's brief reaches it. So the folder is still minted at once, and the
guarantee is that a blank one is neither listed nor kept. The quit-time
removal uses the rule the terminal reaps empty launches by
(`v3EmptyUnassignedSession`).
