---
kind: changed
title: Factory conversations and keys use factory wording
pr: 1800
surface: [chat, engine, docs]
invalidates:
  - "The factory_add card asked put this on the factory floor? It now asks hand this to the factory?, and factory messages and navigation use factory wording."
  - "The factory manual called the page and its jobs the floor. It now calls them the factory and keeps the exact factory_floor tool name for compatibility."
---

Change person-facing factory strings and the matching TUI e2e needle together.
Go comments, internal names and registered tool names keep their existing names.
