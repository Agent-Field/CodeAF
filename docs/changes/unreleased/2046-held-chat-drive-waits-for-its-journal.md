---
kind: fixed
title: Opening a held chat leaves the other window's sync lease alone
pr: 2046
surface: [chat, engine]
invalidates:
  - "Opening a chat used to start its sync drive before checking the journal lock. A process refused the journal no longer starts, renews, releases or forgets that chat's sync hold."
  - "The fresh chat offered beside a held chat used to inherit the held chat's executor and seals. It now opens on its own seat with its own sync drive."
---

A terminal opened beside a chat held by the desktop app or another process can
still offer to move the conversation here. Leaving that question, quitting the
terminal, or stopping its host keeps the original window's chat sendable.
The fresh chat offered beside it runs commands and saves turns in its own cell.
This does not change the overlap when a drive already started in this process
outlives its released journal and a later open is refused.
