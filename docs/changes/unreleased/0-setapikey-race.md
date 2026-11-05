---
kind: fixed
title: Keep provider keys consistent when opening conversations
pr: 0
surface: [app, chat, engine]
invalidates: []
---

Opening a conversation while the app hands over a provider key now reads
settings under the same lock as the handoff. Launches receive a complete
key and its matching model sources instead of racing with the update.
