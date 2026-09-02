---
kind: fixed
title: the chat journal and the demo seeder post through the one thread door, and its sweep is green
pr: 406
surface: [chat, build]
invalidates:
  - "`internal/session/chatlog.go` wrote messages with `PostMessage` directly, around the `thread.Post` door, and the sweep that forbids it (`internal/thread TestEveryMessageWriteUsesThreadPost`) was skipped by CI as known red. Both remaining direct writers — the v3 chat journal and `cmd/aforge-demo-home`'s search-index seeder — go through `thread.Post` now, and the test is out of `.github/known-red.txt` and the CLAUDE.md known-red list."
---

`thread.Post` is a pure pass-through today, so nothing a person sees moves. What
moves is that the door is the only door again: a later check that lands in
`internal/thread` reaches every durable conversation write, including the v3
chat journal, which was born after the law and had never been wired to it.
