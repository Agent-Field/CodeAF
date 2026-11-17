---
kind: added
title: Told "you coordinate it", Afi hands the work to a chat it names, which splits it into a plan
pr: 2716
surface: [app, engine]
invalidates:
  - "Afi made a batch's five or more tasks itself and the desk gave them to a chat of the group's name. Now Afi starts the chat with ws_chat_start (a name and a brief), and the chat splits the work into tasks under a group of its own name, with owners and waits. A plan's Change the plan goes to that chat, not to Afi."
  - "\"You coordinate it\" used to mean nothing special to Afi. It now hands the work to a named chat whatever the number of tasks."
---

Final spec, "A batch of work". ws_chat_start is served by the desktop app beside the person's
chats and is on no server belt.
