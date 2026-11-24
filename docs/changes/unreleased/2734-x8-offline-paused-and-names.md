---
kind: fixed
title: An offline message is paused and survives a restart, and org threads name people, not handles
pr: 2734
surface: [app, factory, docs]
invalidates:
  - "A turn that could not reach the model ended as error_kind other with the transport's words (\"after 0 retries: after 8 attempts: … connection refused\"); it is now error_kind offline with \"this computer can't reach the model right now\"."
  - "Retry message lived only in the running app's memory and was gone after a restart; the failed words are now kept in app-footers.json beside the error entry (field retry) and a fresh process offers them again while that failure is under the newest message."
  - "The organization's server stamped task events, page comments, Resolved by, a file's maker and a task's created_by with the person's handle (\"Stopped by priya\"); they now carry the name the person joined with, falling back to the handle only when none is known."
  - "The server Afi pane showed ws_decisions as a step; it reads \"read the decisions\", and a tool with no words never shows its handle."
---

Desk half: Agent-Field/codeaf-desk (lane X8) draws the offline turn as "Paused", resends it when the connection is back, and stops counting it as failed.
