---
kind: fixed
title: one delivery seam — a report reaches a reader or the person, and the model's say is not yours
pr: 623
surface: [chat, engine]
invalidates:
  - "`Agent.deliverTaskNote` chose the parent's worker at one instant and enqueued to it at another: it read the room's speaker, asked `takesNotes`, and then called `enqueueNote` WITHOUT READING ITS ANSWER. A worker that closed in that window answered false and the report was dropped on the floor; a worker whose runner withdrew it in that window took the report onto a queue nothing would ever drain. Either way the node was then marked announced and checkpointed as such, so the report was never said again in that session or any later one. The reader is now resolved and the note appended under ONE hold of the room's lock (internal/session/mailbox.go's taskRoom.handIn — the protocol a person's steered line already used), and a delivery nobody took falls back to the conversation."
  - "The mark that says a landing has been announced was made whether or not anybody took the note. It is made only on acceptance now, and it carries the ENDING it was made for (`noted_state` on the task checkpoint, absent on older files and read as \"announced, whatever it said\"), so the same landing announced twice buys no second model turn while a node that later ends somewhere else — a person deciding about work nobody could check — is news again."
  - "A task worker's runner asked only about the person's steering mark after withdrawing the room seat. A sub-task's report is not marked that way, so a part landing in that instant was left queued and unread; the runner now re-reads the news count as well (task_child_run.go)."
  - "`tasks id N say \"…\"` — the MODEL's door into a running task — called `Agent.SteerTask`, which is the person's door. The worker journaled the model's sentence as the person's own correction, the folder recorded that the person had spoken, and nothing in the worker's transcript could tell coordination from authority: a parent saying \"you may change the schema\" read exactly like the person saying it. It takes `Agent.relayToTask` now, arrives framed as `the main conversation says: …` (or `task 4 says: …`) with a clause stating it is not the person, and is written in the session's own lane rather than as a correction. The person's `SteerTask` is unchanged in every respect."
  - "The tool used to answer the model with `It arrives in its loop as the person's own words.` That sentence is gone — it now reads `named as this conversation speaking, not as the person` — because a model told its line lands as the person's will use the tool to give itself permissions."
  - "`Agent.enqueueSteeredLine` no longer exists; a line said into a node goes through the room's seat (`taskRoom.steerIn`). `userMessage.steered` is documented as a fact about DELIVERY — is somebody waiting on an answer to a line no request has carried — and never about authorship, which is `authored` plus the correction mark."
  - "how-tasks-run.md gained two sections: a piece landing while its parent is being checked now says its report comes to the conversation, and `When the conversation says something to a running task` states which road spoke and what each is recorded as."
---

This is the delivery half of the conversation-runtime wave, extracted from the
callers that already shared it rather than replaced by a message bus. There is
no router, registry, event store or cross-session anything: `conversationID`
carries a session with its task number because a task number repeats across
sessions, and a later router would resolve one to the same local contract.

What it deliberately does NOT do yet, said plainly because a receipt is easy to
over-read: a delivery carries no assignment revision. A correction and a
worker's finalization remain two independent races on one node — the correction
is accepted, and a worker that lands before reading it finishes against the
older request. Accepted means a live reader has it, never that the work has been
re-aimed. Revision-aware steering is a separate, integrated step.
