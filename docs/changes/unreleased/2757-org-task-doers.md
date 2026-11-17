---
pr: 2757
kind: changed
title: Who does a task can be changed, a named agent can do it, and Start wakes every agent doer
pr: 2757
surface: [factory, engine, docs]
invalidates:
  - "Was true: a task's doer was fixed when it was made (TaskPatch had no done_by), the doer kinds were afi, chat, factory and person, and an organization's Start and Start all ran only Afi's tasks, leaving a chat's task in Up next with 409 \"a chat's task starts from that chat\" (E2 F7). Is true now: done_by is a change, the task owner's or whoever looks after the workstream's, refused while the task runs, once it is done and on a Factory task; a person outside the workstream or an agent not on its list is 422; unfinished work goes back to up_next and the thread gains a handed line \"from <old doer> to <new doer>\". A new doer kind {\"kind\":\"agent\",\"agent_id\",\"name\",\"crew\"} names an agent or crew from the workstream's agent list (serve.Config.WorkstreamAgents, empty until the organization's agent list lands), and the server keeps the list's name. Start and Start all run Afi's, a named agent's and a chat's agent's tasks, each in the task's own seat from its request, never the chat's conversation. The hello word org_workstream_task_doers says so."
  - "Was true: the manual offered \"Hand to someone\", which could hand a task to Everyone, and Everyone as the owner of incoming work. Is true now: that verb is \"Change the owner\" and offers only people, \"Change who does it\" is its own verb, and Everyone is offered as an owner only where a workstream already keeps it."
  - "Was true: ws_task_create's done_by was this_chat, factory or person, and person always meant the owner, so Afi could not give a review to the reviewer (\"Marco reviews the store text\" was made with its asker as doer). Is true now: done_by is afi, person:<id>, agent:<id> or factory, spelled once as session.WorkstreamDoneBySchema and read by factory/serve/workstream_doer_ref.go; this_chat and a bare person still parse for old prompts. A person outside the workstream or an agent not on its list is refused with words naming the fix. Lane L7's brief schema interpolates the same constant."
---
