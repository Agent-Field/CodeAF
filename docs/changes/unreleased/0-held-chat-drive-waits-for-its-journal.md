---
kind: fixed
title: Opening a held chat leaves the other window's sync lease alone
pr: 0
surface: [chat, engine]
invalidates:
  - "Opening a chat used to start its sync drive before checking the journal lock, so a second process could renew the other window's fence and release it on quit. A process now starts that chat's sync drive only after opening its journal successfully."
  - "Quitting at the move question or stopping a host after a refused open could delete the original writer's hold receipt. A refused journal open now leaves the original writer's lease and receipt untouched."
---

A terminal opened beside a chat held by the desktop app or another process can
still offer to move the conversation here. Leaving that question, quitting the
terminal, or stopping its host keeps the original window's chat sendable.
