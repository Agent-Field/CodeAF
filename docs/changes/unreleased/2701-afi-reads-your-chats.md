---
kind: added
title: Afi can read your own chats in a Personal workstream, with ws_chats
pr: 2701
surface: [chat, app]
invalidates:
  - "No tool let Afi read a workstream's chats; Afi now has ws_chats (session.WorkstreamAfiTools), served by the desktop app on a Personal workstream and absent on every server belt."
---

The spec says Afi can read your chats in the workstream and other people cannot. The verb lives only where the chats do: on the person's own desk.
