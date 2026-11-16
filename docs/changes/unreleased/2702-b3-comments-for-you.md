---
kind: added
title: a comment reaches the task's owner and whoever it @names; a handed task says who handed it
pr: 2702
surface: [engine, factory]
invalidates:
  - "Was true: a comment on an organization task reached nobody in particular; people had to open the task to see it, and an @name was plain text. Is true now: a comment answers with `mentions` (the workstream's people it names with @), and a task read for one person carries `for_you` {comments, mentioned?, last}: the unread comments on a task they own or that name them, from the last fourteen days. Reading the thread with `seen=1` (`factory.org_workstream_task_events {seen: true}`), which only a screen sends, marks it read."
  - "Was true: a handed-over task said only \"Handed to Marco\" in its thread, so the new owner could not tell from their Inbox who gave it to them. Is true now: the task carries `handed_by`, the person who last handed it to its owner, cleared when the owner takes it by their own hand."
---
