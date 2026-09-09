---
kind: fixed
title: Keep hosted chat identity attached to the selected session
pr: 653
surface: [chat]
invalidates:
  - "The UI put a hosted connection's shared agent handle in its background-chat keeper. After a switch both entries described the newly selected chat, and selecting the previous entry opened the wrong body. Shared handles now select one conversation without creating aliases or duplicate background subscriptions."
  - "Resume and new-session paths could interrupt or close the newly selected hosted session through the old shared handle. The engine owns that swap, and the surface no longer closes the same handle again."
---

The ownership correction keeps a connection attached to the conversation it selected
and prevents an old handle from closing a newer selection. Every shipped engine-backed
door now dials one connection per conversation, so changing chats closes neither the
previous session nor its background subscriptions. `--no-host` retains the same visible
multi-conversation behavior inside the process; engine-backed doors achieve it with
separate connections rather than protocol multiplexing.
