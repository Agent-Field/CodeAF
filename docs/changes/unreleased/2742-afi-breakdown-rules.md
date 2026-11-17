---
kind: changed
title: Afi breaks big work into 5 to 8 deliverables, and a task's steps live inside it
pr: 2742
surface: [engine, factory]
invalidates:
  - "Was true: Afi's workstream rules said only that five or more tasks from one request are a batch, so a launch plan could arrive as many small rows, each its own task, with no steps anywhere. Is true now: a task is a deliverable of hours to days, nothing under 30 minutes is made a task, big work is 5 to 8 tasks and never more, every task names its owner and doer, a task's steps end its request under a line \"Steps:\", and one Start releases the whole plan."
  - "Was true: asked to make a task for a one-word typo, Afi made one. Is true now: it makes no task and does the fix itself in the same turn (see the next line)."
  - "Was true: asked for a task for a one-word typo, Afi made none and answered only why (\"I can't do that\"), and once said it had suggested the fix without making any change. Is true now: small work is done in the same turn with a tool (a suggestion where Afi may not rewrite), and a reply says it changed something only after the tool answered; when the thing is out of reach, the first sentence offers what Afi can do now (\"Add the pricing page to Files and I'll fix it at once\")."
---

The rule and its numbers live once, in internal/session (WorkstreamTaskSmallestMinutes,
WorkstreamPlanFewest, WorkstreamPlanMost, WorkstreamStepsLead). Once tasks carry a
checklist (CodeAF #2745), the steps move from the request into it.
