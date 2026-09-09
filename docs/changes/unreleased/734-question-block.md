---
kind: added
title: one block above the box for every question, with esc meaning later rather than no
pr: 734
surface: [chat]
invalidates:
  - "There was no question block in internal/tui3. Every lane drew itself — consent.go's block, connect.go's offer, harness.go's row, task.go's proposal card, standing.go's card — each with its own layout loop and its own key set. internal/tui3/question.go is one renderer now, for the line, the card and the ratify row, fed by Agent.WatchQuestions() and answered through Agent.ResolveQuestion."
  - "A sub-harness's own question, an adaptive run standing at its fuel gate, and a ground-shift conflict could not be drawn or answered anywhere in the product — the resolvers existed and nothing called them. They are drawn on the question block now, which is the first surface any of the three has ever had."
  - "esc on a question meant deny. On the question block it means LATER: the rows fold away, the question stays open, whatever was waiting on it goes on waiting, and the count in the status line does not drop. Nothing is cancelled by making something go away. The older blocks — the approval question above all — keep esc-denies until each is retired."
  - "The consent block's law that a question SUSPENDS THE KEYBOARD is retired for everything on the question block. The block takes only the keys it has drawn; a letter is the question's only over an empty box; and with words in the box every printable key is the composer's and enter sends what was typed as the ANSWER, on a question the turn is waiting on. It still holds for consent.go itself."
  - "There was no count of open questions in the status line. There is: `? N questions · alt+a`, amber, on every page, raising the newest open question. internal/manual/chat/questions.md said this was designed and not built; that sentence is gone."
  - "A key pressed in the moment a question appeared was applied to it. A key that arrives before the question has been on screen for 250ms is now DROPPED — never applied late — because it was aimed at whatever was there before."
  - "A question arriving while the box held a half-typed sentence took its rows immediately. It now waits until the box is empty or the hands have been still for three seconds; it is open and counted in the status line the whole time."
  - "Answering left nothing behind above the box. It leaves one dim receipt now — `decided <head> → <picked> · you · 14:02 · c change` — which is session.DecisionRecord.Line's own rendering, so the line a person reads and the line the model reads are one sentence. A withdrawn question leaves `⊘ <head> — no longer needed · <reason>` in the same slot."
  - "Nothing offered to make a repeated yes stand. The third same-shaped yes now puts `[r] make it a rule for <scope>` on the row, with the scope written on the key before it is pressed."
  - "The answer strip on home and the switcher drew `y` and `n` on whatever two answers came FIRST. On the consent lane, whose answers are `1 allow once · 2 always · 3 deny`, that put a widening approval under the key a person presses for yes. It draws session.AnswerOption.Key now, and an answer whose key is not one rune is left off the strip rather than renamed."
  - "internal/tui2/tokens had no mark for a question that stopped needing an answer. tokens.GWithdrawn is ⊘ (nf-fa-ban), tinted tertiary and never amber: amber is a person being waited on, and nobody is being waited on by a question that has been taken back."
---

The block is the surface half of `docs/design/questions/DESIGN.md`, over the
object lane E1 landed in #712. What it settles is not the drawing so much as the
bargain underneath it: a question used to be the one thing on this surface a
person could not put down. It owned the keyboard, its only two answers were
"decide now" and "say no by pressing escape", and a decision somebody wanted five
minutes to think about had no way to be given five minutes.

So `esc` is *later* — the rows fold to a chip in the status line, the work stays
paused exactly where it was, and the box underneath is theirs again — and once
there is a way out there is no longer any reason to hold the keyboard, which is
what makes the last rung of the ladder reachable at all: free text is always
available, and typing it is now something a person can actually do.

The older blocks are not moved yet, and the manual page says which is which
rather than claiming the new grammar reaches them. `app.questionDrawnHere` is the
one place that records how far the migration has got, with a test that fails if a
lane is ever drawn twice.
