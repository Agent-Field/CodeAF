---
kind: fixed
title: A question's own answer box edits like the message box under it
pr: 1013
surface: [chat, docs]
invalidates:
  - "In the `something else…` row — the box a question becomes when you type your own answer — only `alt+left`/`alt+right`, `home`, `end`, `ctrl+a` and `ctrl+e` reached the caret. ⌘←/⌘→ (as `super+left`/`meta+left`), ⌃←/⌃→ (`ctrl+left`/`ctrl+right`), the word kills `alt+backspace`/`ctrl+backspace` and the line kill `ctrl+u`/`super+backspace` were all silently dead there. They now move and kill exactly as they do in the message box, through the shared vocabulary in `internal/tui3/editkeys.go`."
  - "The answer box read its own hand-rolled key map. It reads the shared `editorMotion`/`editorWordKill` vocabulary now, so `ctrl+a` still goes to the line start here (the map's spelling of it agrees with the row's own `home` case) and `ctrl+e` still goes to the line end rather than being claimed by the map — the same split `editkeys.go` draws for every other box."
---

`#1013`: the `something else…` row is a box, and it bound a short list of editing
keys by hand while every other box on the surface read the one vocabulary in
`editkeys.go`. `cmd+←/→` never reached the ends of the line, `cmd+delete` (or
`ctrl+u`) never killed to the line's start, and `opt+delete`/`ctrl+delete` never
deleted a word. It was the same defect that file was written for, said about a
box that had grown its own map.

`ctrl+w` is deliberately still not the word kill here: it shuts the tab in front
everywhere on this surface, and the message box makes the same bargain.
