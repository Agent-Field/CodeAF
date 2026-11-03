---
kind: added
title: codeaf app chat rows say when a take of that chat is running
pr: 0
surface: [engine, app]
invalidates:
  - "Was true: a `chats.list` row or `chat.changed` event looked the same while `agents.take` was still copying a chat as when no take had happened, so an app that stopped waiting could not tell the two apart. Is true now: every row carries `taking`, true from when `agents.take` asks for that chat until it ends; a take that ends without moving the chat emits the row again with `taking:false`, and a finished take clears it in the same emission that says the chat is here."
---
