---
kind: changed
title: Team chat sidebars default to Tasks
pr: 1797
surface: [chat, docs]
invalidates:
  - "Manager conversations in Chats opened the sidebar on Traffic. Team overlays now default to Tasks for managers and members, while retaining explicit selections and offering both Tasks and Traffic."
---

The sidebar keeps its width and header position when switching views. The session
continues to remember manager and member selections independently, including when
a team overlay is cleared and reopened.
