---
kind: fixed
title: a question's own answer box answers to the whole caret vocabulary, not a hand-rolled copy of it
pr: 1032
surface: [chat, docs]
invalidates:
  - "The `something else…` row on a question — the one box on the block a person writes an answer in — bound its own short key map, so ⌘←/⌘→ reached no part of the caret, ⌘⌫ killed no line and ⌥⌫/ctrl+⌫ killed no word, all while the message box directly beneath it answered to every one of them. It now reads the same shared vocabulary every other box on the surface reads (`editorMotion`/`editorWordKill`)."
  - "`internal/manual/chat/keys.md` listed the boxes that answer the one caret vocabulary and left the question's own answer box off it. It is on the list, and `questions.md` says so at the `something else…` row."
---

The row hand-rolled `left`/`right`/`home`/`end` and `alt+←`/`alt+b` and nothing
else — a copy of the surface's word-and-line vocabulary that had drifted from
the one every other box reads. It is one more caller of `internal/tui3/editkeys.go`
now, so the next chord added there reaches it too.
