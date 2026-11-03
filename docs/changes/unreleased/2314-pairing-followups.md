---
kind: fixed
title: App and pair approve settle a lost approval reply; chat rows say when a take is running
pr: 2314
surface: [engine, app, docs]
invalidates:
  - "An approval from the desktop app or codeaf pair approve whose reply was lost still let the device join, but the approving computer reported a failure, stayed solo and never shared its chats, and reopening the link said that request was already answered; now both settle the approval they already gave, as the terminal chat screen does, and reopening a link that was already approved says the device is already paired. An approval that never arrived says to try again."
  - "Was true: a `chats.list` row or `chat.changed` event looked the same while `agents.take` was still copying a chat as when no take had happened, so an app that stopped waiting could not tell the two apart. Is true now: every row carries `taking`, true while `agents.take` runs for that chat; a take that ends without moving the chat emits the row again with `taking:false`, and once a take has acquired the chat, rows that still show it elsewhere keep `taking:true` until a row says the chat is here."
---
