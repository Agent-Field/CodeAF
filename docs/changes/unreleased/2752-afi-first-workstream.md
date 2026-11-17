---
kind: added
title: A new workstream starts as a chat with Afi and a card; nothing exists until Create workstream
pr: 2752
surface: [app, engine]
invalidates:
  - "New workstream opened a sheet that asked for a name first, and the new workstream then offered \"Draft the brief and tasks\", which sent Afi searching an empty workstream. Now New workstream opens a page that is only Afi's conversation; Afi proposes the whole setup as one card (name, what it is for, people, inside, ticked first tasks, Library pages, rules), and nothing exists and nobody is told until Create workstream."
  - "There is no empty \"New workstream\" left behind: the page is the window's memory until Create, and Set up by hand opens the same card empty."
---

Owner decisions 2026-11-17 §9 and §11 (v2 designs, screen 5). The card is the desktop app's: before
Create there is no workstream for an agent to work in, so each of Afi's turns is an agents.errand and
no tool or store keeps the card. This change is the chat manual's page for it,
internal/manual/chat/workstream-new-setup.md.
