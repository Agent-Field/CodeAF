---
kind: fixed
title: the @ list reads recent conversations on every opening and @chat shows them all
pr: 1720
surface: [chat, docs]
invalidates:
  - "The `@` list's recent conversations were read once per process, so a conversation started in another window after the first `@` was never on it. They are read again every time the list opens."
  - "`@chat:` and `@team:` kept the first eight rows of their section and showed no sign of more. A prefixed list keeps up to thirty-two and scrolls; the bare `@` still keeps eight per section."
  - "The manual did not say which conversations the `@` list holds. It does: every open tab except the one you are in, then the twenty most recent in this project; older ones and other projects' are reached with `/resume`."
---

Santosh's report (2026-09-30): a tab reading `cloudfl…` was on the strip and
`@chat:cloudfl` did not list it. The match itself was fine; what the list held was
not. The recent list was a snapshot taken on the window's first `@` and kept for
the life of the process, and a section cut at eight rows said nothing about the
rest. The conversation in front is still left off on purpose: pointing at the
chat you are typing in is not a reference.
