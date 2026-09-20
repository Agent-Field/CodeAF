---
kind: changed
title: Decision dialogs support updated requests and concurrent clarification
pr: 1146
surface: [chat, engine, remote, docs]
invalidates:
  - "The conversation sidebar previously put its hide control after the task rows and reordered families by activity. The hide control now stays pinned at the top, task families and their children retain creation order, and + /task follows the scrolling list."
  - "Decision panels previously displayed c change and ? ask back, plus keyboard hints below the frame. They now display only esc later, o other and ? clarify on the lower boundary, with cyclic navigation in both directions."
  - "Other previously sent lane-dependent corrections, which could implicitly approve a task proposal. It now withdraws the pending decision without approval and starts the updated request in the same conversation."
  - "Clarification on a permission dialog previously queued behind the blocked tool call. It now runs in context beside that call, retains the original decision, and gives any question raised by the clarification priority until answered."
  - "The lowercase o shortcut previously opened a question's full page. Uppercase O opens that page now; c remains a note on the full page and a change action on completed receipts."
---

Clarification tools and replies appear in the conversation without consuming the
original turn's transcript entries. The pending permission remains unanswered,
and an updated request never grants it. The behavior also crosses the local
engine connection and remote connections.
