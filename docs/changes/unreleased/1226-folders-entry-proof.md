---
kind: changed
title: Folders-entry manual and J36–J43 harness name the dedicated Folders place
pr: 1226
surface: [chat, docs]
invalidates:
  - "The chat manual said `/folders` focuses home's folders panel and that there are seven places with a four-word bar. `/folders` now enters the dedicated Folders place; the bar is `home` `tasks` `spend` `settings` `folders`; `alt+5` is Folders and standing, memory and search are `alt+6` `alt+7` `alt+8`."
  - "J36–J43 had no committed e2e harness. internal/e2e now names the frozen needles and asserts collections.db job and graph state; live tmux remains t-fe-validate and must not report passed:true from this lane."
---

Visible **New folder**, **New chat**, and **Organize existing chats** are documented.
Home's `folders` panel stays as enter-from. Chords for those three actions are not
recorded in TRY until ui lands them.
