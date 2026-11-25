---
kind: added
title: A task's Working conversation is readable by its workstream, and its owner or starter steers it
pr: 2744
surface: [engine, factory, app]
invalidates:
  - "Was true: a task Afi ran on an organization's server published no agent event; nobody could read what it said or did, only the task's state, results and thread. Is true now: every line of the run is kept and published as task.work to everyone who can see the workstream, read with GET /v1/workstreams/{id}/tasks/{tid}/work and relayed to the desk as factory.org_workstream_task_work (hello org_task_work)."
  - "Was true: anything a person typed to an agent in an organization was private. Is true now: Afi's pane and a person's own chats stay private, but a steer typed on a task (POST .../work/messages, owner or starter only, while the run works) is a line everyone in the workstream reads."
  - "Was true: the task-run prompt told Afi nobody reads what it says during a task. Is true now: it is told everyone reads it and that the owner or starter may steer it."
  - "Was true: the only way to change a task Afi had finished was Send back, which put it in Up next and held everything that waits on it again. Is true now: the owner's (or last starter's) words to a done Afi task start a revision run and the task stays done; can_revise on the conversation says so, and usd is what its runs spent."
  - "Was true: a comment asking Afi's task for a change sat there until someone sent the task back. Is true now: the server hangs Afi's offer \"Revise with this\" under a person's comment that asks for a change on an Afi task that is running or done, and taking it steers the run or starts a revision with the comment's words."
  - "Was true: on an organization's server Afi was told a batch is named for the chat that runs it, and promised people a chat to coordinate big work. Is true now: there are no chats on the server; Afi makes the batch as a plan of its own tasks that waits for Start."
---
