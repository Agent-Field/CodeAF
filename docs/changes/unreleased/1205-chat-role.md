---
kind: changed
title: a belt landing no longer wakes the chat, the conversation gains a work tab, three hand-off rules
pr: 1205
surface: [chat, engine]
invalidates:
  - "Every landing of a belt run woke a full model turn over the whole conversation, the shape pull request 1183 measured at one 28-minute turn of 49 tool rounds. A landing writes one dim digest line to the conversation's record and starts no turn; the wake is kept behind `landingOwesAnswer` for the case where the chat launched the task to answer the person and owes a reply."
  - "There was no place to watch a run without leaving the conversation. A conversation with a live belt run gains a tab on the strip titled with the run's root, holding the run's rows as the tasks place draws them, the live lines and the note composer; it closes itself when the run lands."
  - "The chat's system page said nothing about when to hand work off. Under the belt it states three rules once: hand off (launch a task), add to (a second `/task` joins the live root), ask about (answer from the store's rows and the task's steps, never redo the work)."
---

Design: `docs/design/worker-harness/CHAT-ROLE.md`, candidate C with the
owner's amendment, "a landing speaks only when an answer is owed". A fact
recorded there for the next cell: under the belt the model's own
`propose_task` does not reach the run engine today; only a typed `/task` does.
