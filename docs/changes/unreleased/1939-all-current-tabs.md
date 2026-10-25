---
kind: fixed
title: All shows the conversation tabs in the current view
pr: 1939
surface: [chat, docs]
invalidates:
  - "The tab strip's All button opened the complete saved library; it now shows only the conversation tabs in the current view."
  - "All discarded the selected team; its cards now follow that team's strip and opening a card keeps the team selected."
---

Normal All cards disappear when their tabs close. The dock's All button shares
the same scope. The full saved library remains available through /wall and alt+v.
