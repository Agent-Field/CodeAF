---
kind: fixed
title: A failing pipe stage now fails watch checks and automation commands
pr: 2191
surface: [chat, engine, app]
invalidates:
  - "Watch ticks and automation commands ran bash without pipefail, so `false | cat` reported success there. Now they run like every other bash door: a failing command early in a pipe fails the tick or the command, and a reader that stops early (for example `yes | head -1`) still succeeds."
---

A failing command early in a pipe now fails watch checks and automation
commands, as it already did in chat commands. A reader that stops early still
succeeds, and a command without a pipe runs as before.
