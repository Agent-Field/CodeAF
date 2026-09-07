---
kind: fixed
title: Keep hosted chat identity attached to the selected session
pr: 653
surface: [chat]
invalidates:
  - "The UI put a hosted connection's shared agent handle in its background-chat keeper. After a switch both entries described the newly selected chat, and selecting the previous entry opened the wrong body. Shared handles now select one conversation without creating aliases or duplicate background subscriptions."
  - "Resume and new-session paths could interrupt or close the newly selected hosted session through the old shared handle. The engine owns that swap, and the surface no longer closes the same handle again."
---

Engine-backed connections currently select one chat at a time; changing chats closes
the previous session and retains saved history. The UI states that limit and presents
saved chats directly. Independent local conversations under `--no-host` keep their
existing background behavior. This correction does not add server-side multiplexing.
