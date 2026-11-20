---
kind: fixed
title: Keep desktop sync and chat handoffs reliable
pr: 2843
surface: [app, chat, engine, remote]
invalidates:
  - WorkAF can distinguish storage-off pairing with blind pass-through from a relay where chats stay local.
  - A chat handoff waits for background jobs and their completion messages, and preserves saved job output.
---

Supported desktop provider keys added after pairing sync through the encrypted
vault. Custom and Ollama connection keys stay on the computer that uses them. Clients
report relay outages, retain local turns, and suppress false sharing warnings.
Completing desktop setup or choosing a relay still leaves an unused home
eligible to join, even with a cached storage receipt.
Pairing records the relay and storage status promptly, and rotation refreshes
storage status for the new identity. Chat handoffs preserve background job
history, output and saved messages.
