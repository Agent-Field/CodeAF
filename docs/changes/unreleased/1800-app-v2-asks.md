---
kind: added
title: v2 app asks, scoped permission rules and question pause clock
pr: 1800
surface: [engine, remote, chat, docs]
invalidates:
  - "The app asks, permissions and rule routes returned authenticated 501 stubs. They now read and answer engine questions, handle task batches and retain receipts, including decisions from other windows."
  - "Always remembered a tool-wide answer for one live agent. App rules now persist literal exact or shown command-class matches with agent or all-agent scope, stable ids, removal, restore and Why; irreversible actions and the always-ask list cannot use them."
  - "App blocking questions could wait indefinitely or take an autonomy default. Their binary-owned clock now stops work after 15 minutes by default without answering; Later holds it and a receipt explains the pause."
---

Ask ids include the owning agent, kind, engine token and creation time. A standing question
subscription records answers and withdrawals independently of a turn. Task
permissions use the parent conversation's consent channels and retain their task
source, so the waiting worker does not block the conversation.

A refused permission answer rolls back only a rule that answer created; an
existing matching rule stays active when another window won or the engine refuses.
