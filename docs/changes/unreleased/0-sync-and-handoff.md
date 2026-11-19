---
kind: fixed
title: Keep desktop sync and chat handoffs reliable
pr: 0
surface: [app, chat, engine, remote]
invalidates:
  - Storage-off pairing confirms the computers are linked and leaves chats local until storage is granted.
  - A chat handoff waits for background jobs and their completion messages, and preserves saved job output.
---

Desktop keys added after pairing sync through the encrypted vault. Clients
report relay outages, retain local turns, and suppress false sharing warnings.
Pairing records the relay and storage status promptly, and rotation refreshes
storage status for the new identity. Chat handoffs preserve background job
history, output and saved messages.
