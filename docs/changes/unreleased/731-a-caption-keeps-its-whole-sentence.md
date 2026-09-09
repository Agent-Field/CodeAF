---
kind: fixed
title: A step caption keeps a filename, a version number and a path whole
pr: 731
surface: [chat, engine]
invalidates:
  - "A step caption ended at the first `.` in the line, whichever token that dot belonged to — no longer true. A `.`, `!` or `?` closes a sentence only when what follows it, past any closing quote or bracket, is a space, a tab, a newline, or the end of the line; every other dot belongs to the token it sits in. `Reading livesteps.go now.` was drawn `Reading livesteps` and is now drawn whole."
  - "A caption could begin in the middle of a word — no longer true. The fragment in front of a mid-token dot is usually a word or two, a sentence under three words is passed over, and the caption a person read then started after the dot: `searching ~/.aforge/v3/projects for the transcript` was drawn `aforge/v3/projects for the transcript`."
  - "The sentence rule lived in two packages, byte for byte — `internal/tui3/caption.go` and `internal/session/caption.go`. There is now one: `session.ShortCaption`, and the surface calls it. A change to how a caption is cut is made once."
  - "The chat manual's page on reading a task page promised only that a caption is never cut with an ellipsis. It now also promises that a caption ends where its sentence ends, so a filename, a version number or a path keeps its whole self."
---

The word cap did not move: a caption is still one sentence of about five to ten
words, still shortened without a trailing glue word, and still never with an
ellipsis. What changed is where a sentence is judged to end. Everything a person
narrates while work runs carries dots — `config.json`, `v1.2.3`, `~/.aforge` —
and cutting inside one of them did worse than shorten the line, because a caption
that lost all but its first two words was thrown away and the one a person read
began after the dot instead.
