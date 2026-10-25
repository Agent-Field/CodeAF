---
kind: fixed
title: Chats can name models for new chats and tasks that join an existing run
pr: 1935
surface: [chat, engine, docs]
invalidates:
  - "A proposal joining a running task discarded its requested model and used the run's worker instead; the joined task now retains and uses its own work model."
  - "The worker-harness manual omitted the proposal's model override; naming a model for one task overrides the task default and crew worker without changing profile pins."
  - "A manager could not select a model for a new teammate conversation; team_start now accepts a model for a member or sub-team manager."
  - "A chat could not request an independent chat through a dedicated tool; start_chat now asks the attached terminal to open one with its chosen model and first message."
---

The new chat opens behind the requesting chat and inherits its approval posture.
Unknown or ambiguous model names are refused before a chat opens. The start receipt
confirms the request; the terminal reports whether the new chat actually opened.

Named task workers survive plan-store persistence, including tasks sharing one run.
Work descendants inherit the nearest named model when a task splits.
Planning and checking keep their crew seats. The one-model launch option continues
to force every task seat onto the conversation model.

Quick tasks retain their per-call model argument on the node task engine. The default
worker harness continues to expose the full task proposal door instead.
