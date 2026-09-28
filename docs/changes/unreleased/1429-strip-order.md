---
kind: changed
title: the tab strip reads Home, the team chip, the manager, then the tabs
pr: 1429
surface: [chat, docs]
invalidates:
  - "The team chip led the tab strip, before Home (`● harbor ▾   Home   ◆ Manager   tabs…`). Home is first and the chip sits right before the tabs it filters: `Home   ● harbor ▾   ◆ Manager   tabs…   +   ▦ All`. As the row narrows Home goes first, then the chip, never the tab in front."
---
