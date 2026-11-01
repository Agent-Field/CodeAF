---
kind: added
title: codeaf app says who started each agent and crew
pr: 0
surface: [engine]
invalidates:
  - "Was true: an app client could not tell an agent or crew the person started from one an agent or an assistant acting for them started. Is true now: agents.create takes an optional `created_by` (one line, at most 128 bytes, such as `afi:<agent id>`); every agent the app makes carries `created_by` (`person` when none was given, the manager's agent id for a member its manager's start line made) on agents.create, agents.list, agents.open and agent.changed; every crew carries `created_by` (`person` for one the app made, the creating agent's conversation id for a sub-team made by team_start of kind team) on crew reads and crew.changed. Agents and crews made before this omit the field."
---
