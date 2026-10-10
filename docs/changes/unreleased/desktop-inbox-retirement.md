---
kind: changed
surface: desktop
title: Retire Inbox tabs and migrate saved workspaces to the Next up design.
invalidates:
  - "Inbox was a registered pinned tab and background work automatically recreated it. Saved Inbox slots are now dropped while other tabs, drafts and closed records survive."
  - "Old Inbox addresses were refused. They now start the existing Next up walk without creating a conversation."
---

Workspace transfer plans ignore retired Inbox slots. A workspace with only those
slots opens a quiet New tab, and surviving panes of mixed splits retain their content.
