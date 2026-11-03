---
kind: fixed
title: App and pair approve settle a lost approval reply; chat rows say when a take is running
pr: 0
surface: [engine, app]
invalidates:
  - "An approval from the desktop app or codeaf pair approve whose reply was lost left the joining computer unpaired, never shared the approving computer's chats, and reopening the link said that request was already answered; now both settle the approval they already gave, as the terminal chat screen does, and reopening a link this computer already approved says the device is already paired. An approval that never arrived says to try again."
  - "Was true: a `chats.list` row or `chat.changed` event looked the same while `agents.take` was still copying a chat as when no take had happened, so an app that stopped waiting could not tell the two apart. Is true now: every row carries `taking`, true from when `agents.take` asks for that chat until it ends; a take that ends without moving the chat emits the row again with `taking:false`, and a finished take clears it in the same emission that says the chat is here."
---
