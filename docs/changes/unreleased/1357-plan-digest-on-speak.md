---
kind: added
title: while a run is live the chat's turn opens on the run's rows, so a change of mind reaches the work
pr: 1357
surface: [chat, engine, docs]
invalidates:
  - "Nothing told the conversation what was in flight when a person spoke. A person who said 'skip the migration' while a task was doing the migration was answered, and the task carried on: the chat could have asked with `tasks`, and the turn where somebody changes direction is exactly the turn where nothing suggests asking. While a run is live, each of the person's messages now opens with a bounded digest of the run's rows — id, title, state, newest note — and the plan-road hand-off facts say to act on the row that the person's sentence just made wrong before answering them."
  - "The chat-coordination design's channel table names `revise_assignment` as the door that revises a task's brief. It is a WORKER'S verb and never the conversation's (Config.mayRevise is `InTask && tasker != nil && taskID != 0`), so the chat cannot rewrite what a running task was asked for. What it holds over a live run is `tasks` with `stop`, which stopBeltRow and stopJoinedRow resolve to the run's own row or to one hand-off that joined it, and `tasks` with `note`. Both of those reach an internal split part such as #1.2 too, by the label the listing and the digest print: `stop` ends it through the store the way the task page's own stop does, and `note` writes onto it. The prompt and the manual say what it can do and not what the design assumed."
---

The row bound is `planDigestRows` in `internal/session/plandigest.go`, with
`planDigestLineChars` and `planDigestNoteChars` on the two strings another
model wrote. What the bound leaves out is counted and named on its own line.

The digest carries no result, no step and no transcript — those are what
`tasks #N` is for — and it is absent entirely where a conversation's hand-offs
are not runs, which is the node road. The person never sees it: it rides in the
message the turn reasons from, and their own sentence is what the journal keeps,
the way a draft marked standing already works.
