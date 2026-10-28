---
kind: fixed
title: Open chats no longer stall while an older chat in the home folder is copied
pr: 2002
surface: [chat, engine, docs]
invalidates:
  - "After pairing, codeaf copied a recent older chat that worked in the home folder by saving the whole home folder; it now leaves that chat where it is."
  - "While that copy ran, every open chat's tools failed with an engine capture-plan answer interrupted error; they no longer wait behind it."
---
A copy's first save walks the chat's whole folder while the engine's one
writer is held. Over the home folder that took minutes. The home folder is not
a project, so a chat that worked there keeps opening in place on this computer
and does not move to other devices.
