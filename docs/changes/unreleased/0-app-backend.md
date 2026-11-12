---
kind: added
title: codeaf app is the desktop app's backend, speaking JSON lines on its own pipes
pr: 0
surface: [engine]
invalidates:
  - "The desktop app's backend was planned as one `codeaf engine` child per agent (the app/v2 branch). `codeaf app` holds every agent in its own process, built by the same assembly a host uses and reached over an in-memory pipe; nothing is spawned per agent."
  - "bootEngine was the only way to assemble an engine conversation and always moved the process into its workspace. assembleEngine now does the assembly for a resolved workspace without the chdir; bootEngine is the chdir and then assembleEngine."
---

`codeaf app [--home dir]` implements docs/design/app-v3/PROTOCOL.md contract 1:
hello first, then ping, shutdown, agents.list/create/open/send/stop/close,
tasks.list and crews.list, with agent.changed, turn.started/delta/tool/done,
task.changed, crew.changed and log events. Listing reads the disk and opens
nothing; a conversation is built on create, open or send, let go after ten idle
minutes, and rebuilt by the next send. Text and thinking chunks are folded per
agent and kind for 16 ms or 4 KB. internal/appwire is the transport.
