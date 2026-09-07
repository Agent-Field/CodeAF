---
kind: changed
title: Separate chat navigation from task metadata and dismiss tabs without stopping work
pr: 653
surface: [chat]
invalidates:
  - "The chat header was one row of names with a floating dropdown glyph. It now groups padded tabs with separators and a labeled Chats control above a transcript separator. Task ancestry and task metadata occupy separate rows."
  - "Ctrl+w in the conversation switcher used to close an agent and require a second press for running work. It now dismisses the tab's view, as the tab's × does, preserving work and drafts. Ctrl+w in the composer still deletes a word."
  - "The whole task header, including empty space and breadcrumb separators, used to return to the main chat. Only the rendered ancestor and Back targets navigate now; the explicit Stop action occupies the metadata row."
---

Every tab, including the selected one, responds to hover. Close targets reserve
their own cells so pointer movement never shifts the labels. Closing a tab does
not stop its agent, erase its draft, or remove it from Chats. Surviving tabs keep
their order; closing the last visible tab goes Home instead of reopening a
previously dismissed tab. Shared engine-backed dismissal goes Home without
switching the connection to another conversation.

The current breadcrumb has primary emphasis, its ancestors and punctuation are
quieter, and status, time, model, cost and call counts sit below the path. Header
rows share their geometry with the body, sidebar, selection and hit maps. The
existing shared-engine lifetime limitation on actual conversation switching
remains; dismissing a view does not exercise that switch.
