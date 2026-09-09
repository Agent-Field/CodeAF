---
kind: added
title: A question opens into a page you can read, compare, annotate and ask back at
pr: 732
surface: [chat, engine]
invalidates:
  - "`internal/manual/chat/questions.md` said `there is no page you can open a question out into` — no longer true. `o`/`enter` opens one and `esc` closes it; the manual's own denial is gone from the same file."
  - "A question could be drawn by a surface and answered by nothing but its own lane's per-lane frame. `internal/remote` now carries `ResolveQuestion` — one wire frame, `session.Answer` whole — so every question a lane raises can be answered from the surface holding a `*remote.Agent`, which is every local chat window."
  - "The composer's prompt family was two slots. It is three: `tokens.GReplyIn` (`↳`) is what came back, and `GlyphPromptChat` (`›`) stays the person typing."
  - "There was no shared key table for questions. `internal/tui3/questionkeys.go` is DESIGN.md's `ONE KEY GRAMMAR` as a table, and every form reads its offer row out of it."
---

A question whose evidence is three paragraphs, a diagram and a diff per answer
does not fit on a card, and a person handed one on a card either answers without
reading it or asks the model to say it all again in the thread — which is the
same evidence, unstructured, in the one place it cannot be compared. So the
evidence sets the size of the drawing, and this is the size it sets when there is
a lot of it.

The page is a view and never a modal: the turn under it keeps streaming, the box
stays live, and `esc` restores the conversation with its scroll untouched,
because the conversation was never moved. A bare letter is an answer only while
the box is empty, and once `c`, `?` or `n` has pointed the box at a part, every
key but `enter` and `esc` is text — watching that fail on a real screen was what
found it, and it had been eating the first two letters of every comment.

`x` lays the answers against each other on the asker's own dimensions, drawing
only the rows they differ on and saying so; where no dimensions were given it
derives the rows from the `+`/`−` lines askers actually write, and where there is
neither it is not offered. `c` writes a note under the part it was pressed on and
`?` puts one question back with this one still open — the reply lands in place
under the row that asked for it, read live off the transcript rather than frozen
on whatever the first frame caught. `d` shows the pick and the asker's reason
BEFORE the second press hands the decision over, and what is written down
afterwards says the asker decided, not you.

Below the question sit the four shapes that stop an answer being free text: a
sentence with holes validated by their kinds, a checklist that ticks and orders,
a run of two-way questions whose third answer is "it does not matter", and a dial
with a sentence under it saying what the setting does — drawn as a number rather
than a picture on the reader tier. An answer's evidence is drawn by six block
kinds, each by the rule its own content carries: a diagram is never reflowed, a
diff wears the diff glyphs, a picture goes through the surface's existing picture
path, and two panes stand side by side above a hundred columns and stack below.
