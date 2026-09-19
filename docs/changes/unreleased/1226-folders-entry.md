---
kind: changed
title: Folders is a dedicated Home tab-bar place, not only a panel
pr: 1226
surface: [chat, docs]
invalidates:
  - "CONTRACTS.md, serial-plan.md, and issue-1.md forbade an eighth tab-bar place and kept logical folders as a home panel only. The owner superseded that: Folders is a dedicated registered place; bar words are `home` `tasks` `spend` `settings` `folders`."
  - "`/folders` focused the home folders panel. It now enters the logical Folders place and is not an alias of `/folder`; `/folder` `/place` `/dir` stay physical directories."
  - "`workspace.organize` unset meant automatic filing could invent folders after a journalled message. A fresh Folders tab now stays empty of generated folders until **New folder** or **Organize existing chats**."
  - "The Home tab bar was four places (`placeBarPlaces=4`). Folders-entry makes it five; `alt+5` is Folders (was standing)."
---

Visible keyboard actions are **New folder**, **New chat**, and **Organize existing chats**.
Organize reuses `observe_and_organize`; no second scheduler. Home `folders` panel stays as enter-from.
File owners: ui `t-fe-ui`, organize `t-fe-organize`, proof `t-fe-proof`, gaps `t-fe-gaps`.
