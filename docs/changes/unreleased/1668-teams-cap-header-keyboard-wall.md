---
kind: fixed
title: the teams cap, header, keyboard and wall show the right thing
pr: 1668
surface: [chat]
invalidates:
  - "The daily cap per team default was one shared pool when it reached All teams, and the header read `All teams's cap`. Each top-level team has its own default cap now, All teams has none unless one is set on it, and the possessive reads `All teams' cap`."
  - "After `Raise to $4` the team header kept showing the old recurring cap. It shows today's raised ceiling for the rest of the day."
  - "After `M` started a manager on the teams page the keyboard stayed on the page, so typing ran page shortcuts. The new manager's message box has the keyboard, the hints say `M`, and the key sheet lists `M m p r d u`."
  - "The wall counted conversations another window held as open here, and a tile opened from Teams landed back on the teams page. The wall shows only this window's conversations, and enter on a tile brings that conversation to the front."
  - "The team naming card waited for the next key press before painting the name. It paints as soon as the name is ready."
---

Re-cut from #1604. The manager's empty Tasks column on the teams page (item 3 of #1552) is not fixed here.
