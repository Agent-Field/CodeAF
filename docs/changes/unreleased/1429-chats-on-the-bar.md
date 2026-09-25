---
kind: added
title: the places bar has a chats word that goes back to the conversation
pr: 1429
surface: [chat, docs]
invalidates:
  - "The places bar read `home  tasks  spend  settings` and the digits were home 1, tasks 2, spend 3, settings 4, standing 5, memory 6, search 7. It reads `home  chats  tasks  spend  settings` and the digits follow it: chats 2, tasks 3, spend 4, settings 5, standing 6, memory 7, search 8. A click on `chats`, `alt+2` or `enter` on it returns to the conversation in front, or opens a new chat when none is open; `tab` steps over it."
---
