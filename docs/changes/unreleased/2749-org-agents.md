---
kind: added
title: An organization's agents, made in My space, shared into workstreams, approved for everyone
pr: 2749
surface: [factory, engine]
invalidates:
  - "An organization had no agents of its own: agents were Personal only and absent from an organization window. Each person now makes agents in My space (factory.org_agent_new), known to nobody else, the operator included, until shared."
  - "A workstream had no list of the agents it may use. People and rules › Agents (factory.org_workstream_agent_list, _add, _drop) lists them as links, shared from a person's space or added from the organization's list; it is the list a task's doer is checked against."
  - "An organization had no shared list of agents. Its person asks (factory.org_agent_roster) and the operator approves or declines; approved, everyone may chat with it and add it to a workstream."
  - "A chat on an organization's server always spoke as Afi. A chat may now be made with a named agent (org_workstream_chat_new agent) and answers as that agent."
  - "L1's Config.WorkstreamAgents seam (#2757) answered no agents on every server, so a named agent could never be a task's doer. It now defaults to the agents the store keeps for each workstream, and Afi may give a task to one of them."
  - "Setting the My space limit wrote no record: it was filed under budget_changed, which the organization record refuses. It is now spending_changed, set space limit."
---
