---
kind: changed
title: the harness offer and a finished design ask on the question block, and five old keys are gone
pr: 780
surface: [chat, engine]
invalidates:
  - "The offer to run a saved program answered to `enter` or `y` for yes and `esc` or `n` for no, on a row of its own that took every key on the screen while it was up. It answers to `1 run it` and `2 not now` on the question block; `esc` means LATER; `y` and `n` are ordinary letters again; and every key the block has not drawn falls through to the message box."
  - "A finished harness design answered to `enter` save, `e` change and `esc` drop, on columns drawn along the bottom of the card in the feed. It answers to `1 save it`, `2 change it` and `3 drop it` above the message box. The card in the feed carries NO answers at all now — it is the page being judged, and it keeps only the line saying what became of it (`saved as <name> v1`, `dropped`)."
  - "A design waiting on you inside its own room had a SECOND row pinned above the box there, reading `waiting on your approval — this design saves only if you say so` with `[ctrl+k] save it · [ctrl+x] drop it`. That row and both chords are deleted (`internal/tui3/roomapproval.go` is gone). The question block is pinned above the box on every screen that has one, a room included, so the same three digits answer it from in there."
  - "`AnswerOptions(QuestionHarness)` returned `run it · not now` for BOTH of that lane's questions, so a design's three answers were unreachable through the one door. `session.HarnessOptions(ask)` now holds both lists and `session.HarnessQuestion` picks between them by reading the finished PAGE off the event."
  - "A finished design's question carried `Blocking{Turn: true}`. It does not: a design stops its own node, not the conversation. A question that stops the turn OWNS the message box, so `enter` over a half-typed sentence sent it as that question's answer — and an answer to a design with no key on it resolves the lane, which is the DROP."
---

`change it` was labelled `improve` and it DROPPED the page: it called
ResolveHarness with false and prefilled the message box, so asking for a change
destroyed the thing you were asking about. It is a door into the design's own
room now, and the engine's one door refuses to resolve a design on that answer at
all (`session.AnswerResolves`), so the page is still there when the rewrite lands.
