---
kind: fixed
title: hosted chat stops the live run it can see
surface: [chat, engine, remote]
invalidates:
  - "The `tasks` tool could list a live bash-belt run but could not stop that same row until it had ended and entered the project index. A stop now resolves the live run first, returns its real receipt over the hosted wire, and publishes the stopped row."
---

A failed stop now says `Could not stop task …:` and gives the reason, so the
model cannot mistake a refusal or missing receipt for a successful stop.
