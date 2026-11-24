---
kind: added
title: The server's Afi and an organization's task runs draw the workstream's canvases with ws_canvas
pr: 2774
surface: [engine, factory, chat, docs]
invalidates:
  - "An organization server's Afi and its task runs had no canvas tool: an org canvas could only be drawn by a person or written through the canvas API. On a server that keeps canvases they now carry ws_canvas, which runs the same `canvas` commands a laptop's agent types, guided by the desktop app's own canvas skill, against this workstream's canvases only."
  - "A canvas only reached a workstream's Files when a person made it. One Afi makes is now in Files as made by Afi for the person who asked, and in a task's run it is the task's Result."
---
