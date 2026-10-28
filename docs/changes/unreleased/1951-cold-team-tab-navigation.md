---
kind: fixed
title: Keep team navigation responsive while opening a worker for the first time
pr: 1951
surface: [chat, docs]
invalidates:
  - "Opening an unseen team chat previously blocked input; independent chat opens now run in the background."
---

The manager and worker tabs remain usable while a first connection waits on the
engine. Canceling or selecting another chat prevents a late opening from taking
over the selected conversation. Already-open chats keep their immediate switch.
Connections limited to a single shared conversation retain their existing swap
behavior.
