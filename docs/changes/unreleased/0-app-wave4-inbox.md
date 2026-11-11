---
kind: added
title: codeaf app keeps every open question in one Inbox with a held answer and Undo
pr: 0
surface: [engine]
---

New methods: `inbox.list`, `inbox.answer` (held five seconds, answers
`undo_until`), `inbox.undo`, `inbox.later`, `inbox.back`, `inbox.open`. Events:
`inbox.added`, `inbox.resolved` with the receipt, and `inbox.counts` for the
badges. Items carry where they come from (agent, crew, task, path) and the two
turns before the question; an answer anywhere resolves the item at once.
