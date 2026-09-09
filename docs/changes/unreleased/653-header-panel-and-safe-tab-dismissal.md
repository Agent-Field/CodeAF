---
kind: changed
title: Separate chat navigation from task metadata and make tab dismissal explicit
pr: 653
surface: [chat]
invalidates:
  - "The chat header was one row of names with a floating dropdown glyph. It now groups padded tabs with separators and a labeled Chats control above a transcript separator. Task ancestry and task metadata occupy separate rows."
  - "Ctrl+w in the conversation switcher used to close an agent and require a second press for running work. It now dismisses another row's view while preserving its work; the current row follows its tab's × and offers keep running, stop work and cancel when working or asking. Ctrl+w in the composer closes its tab; word deletion remains alt+backspace or ctrl+backspace."
  - "The whole task header, including empty space and breadcrumb separators, used to return to the main chat. Only the rendered ancestor and Back targets navigate now; the explicit Stop action occupies the metadata row."
---

Every tab, including the selected one, responds to hover. Close targets reserve
their own cells so pointer movement never shifts the labels. An idle tab dismisses
without erasing its draft or removing it from Chats. A working or asking tab first
offers `keep running`, `stop work` and `cancel`; only `stop work` ends that
conversation's reply, tasks, adaptive runs and jobs. Surviving tabs keep their
order and the last-used one becomes current; closing the last visible tab goes Home.

The current breadcrumb has primary emphasis, its ancestors and punctuation are
quieter, and status, time, model, cost and call counts sit below the path. Header
rows share their geometry with the body, sidebar, selection and hit maps. The
conversation behind every shipped tab has its own connection, so navigation and
dismissal do not exercise another conversation's lifetime.
