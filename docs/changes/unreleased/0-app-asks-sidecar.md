---
kind: added
title: codeaf app answers asks, detects a dead conversation, and builds a garbled sidecar
pr: 0
surface: [engine, build]
invalidates:
  - "codeaf app had no approval or question door, so a needs_you agent could only be stopped. It now serves asks.list and asks.answer and the ask.opened/ask.closed events, over the engine's own question lane and answer door — an answer from a joined TUI window closes the ask with by:\"elsewhere\"."
  - "A codeaf app conversation whose engine died stayed drawn as working. It now reads agent state \"stopped\", closes its open asks with by:\"stopped\", and reopens on the next send."
  - "A team that closes or disappears left its crew on the app's list. It now sends crew.removed {id}."
  - "assembleEngine recorded the app's own working directory as a new conversation's launch folder. It now records the workspace the app asked for."
---

The engine's `interrupted` task state has no PROTOCOL word, so an interrupted
task is left out of `tasks.list` rather than reported as `failed` — the chat
draws it as `interrupted`, apart from `failed`, and guessing would mislabel it.

`make app-sidecar APP_GOOS=… APP_GOARCH=…` (and `app-sidecars` for all four)
builds the desktop app's bundled codeaf with garble `-literals -tiny`, pinned
at v0.17.0, output `dist/app/codeaf-<rust-triple>`.
