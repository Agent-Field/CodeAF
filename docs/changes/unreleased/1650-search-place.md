---
kind: removed
title: Search lives on Home instead of a separate page and slash command
pr: 1650
surface: [chat, docs]
invalidates:
  - "The Search page was reachable through /search, the map and alt+9. Those doors and the page are removed; Home searches conversation names, projects, folders, task titles and task outcomes."
  - "The navigation and first-run hints advertised nine destinations. They now stop at eight, ending with Memory; existing destinations keep their shortcuts."
---

Home search does not search the full text of every message. Conversation storage
and engine search capabilities remain available to their existing callers.
