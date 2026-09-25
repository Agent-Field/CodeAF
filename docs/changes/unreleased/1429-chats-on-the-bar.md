---
kind: added
title: the places bar has a chats word that goes back to the conversation
pr: 1429
surface: [chat, docs]
invalidates:
  - "The places bar read `home  tasks  spend  settings` and the digits were home 1, tasks 2, spend 3, settings 4, standing 5, memory 6, search 7. It reads `home  teams  chats  sessions  spend  settings` and the digits follow it: teams 2, chats 3, sessions 4, spend 5, settings 6, standing 7, memory 8, search 9. A click on `chats`, `alt+3` or `enter` on it returns to the conversation in front, or opens a new chat when none is open; `tab` steps over it."
---
