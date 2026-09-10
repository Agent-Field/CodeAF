---
kind: fixed
title: the questions wave keeps six promises it was only making
pr: 772
surface: [chat, engine, docs]
invalidates:
  - "A conversation stopped on the model's own `ask` told home, the switcher and every other window that it was `working`, because Agent.waitingOnPerson listed every question lane except Agent.askWaits and presenceSnapshot writes the question only where that predicate says somebody is waiting. It says `waiting on you` and carries the question now."
  - "session.WriteAnswer refused an answer for any lane with no entry in AnswerOptions — the model's own `ask`, a running sub-harness, the stuck-turn question — so every chip home drew for those lanes came back `could not leave that answer — open the conversation and answer it there` when it was pressed. The kind's table is consulted only where the kind HAS one; everywhere else the question carries its own options and the surface has already checked the key against them."
  - "`[u] undo` was in the key table and in the manual and no ratify line ever drew it: nothing outside a test set questionShown.undoable. It is read off the object now — a ratify, reversible stakes, and an unwind the asker wrote down — so the ladder's third rung has the way back its whole existence is about."
  - "An assumptions card wore tokens.GNeedsHuman, the amber `?` whose binding says `waiting on a human (always amber)`, about a card that waits for nobody. The vocabulary has a slot of its own for it — GAssumed, `≈` on the floor and nf-fa-lightbulb_o on the tier, tertiary and never amber — and internal/tui3's questionAskSlot is the one place a shape's mark is decided."
  - "An assumptions card's countdown read `starts on its own in 9m 57s`, which is the task proposal's sentence about work beginning, on a card that begins nothing. A kind with no pick puts its own words there: an assumption reads `goes on in 9s`."
  - "Answering on the page a question opens into left TWO records — the page's own account where its foot had been, and the block's receipt — and the block's said `another window` about a key pressed in that same window. The page folds on the answer and the block's receipt is the one account; questionroom.go's questionAnsweredWord and the questionRoom.answered field it read are deleted."
  - "The receipt above the box was cut from the right, so a long `with:` clause took `· you · 14:02 · c change` off the end with it. DecisionRecord.LineClauses carries the rank beside each clause and the surface gives up whole ones; who decided, `cannot change` and `c change` are reserved rather than ranked, and a row too narrow even for the question cuts the question around them."
  - "A dial's reading was the asker's label with `it will ` in front of it wherever the label was short enough to look like a verb phrase, so a how-many dial read `it will five times`. The reading is the asker's words, word for word, and a dial with no labels has no reading row at all — its face already carries the number."
  - "manualListCap in internal/session was a QUARTER of one read (12,800 bytes), and the tasks page's 138 headings grew past it, so the cut notice at the foot of a long page stopped naming its last section and TestEverySectionTheCutNamesComesBackWhole went red on dev on a section nobody had touched. The comment on that number already claimed it was room for the longest page's headings twice over; it is half a read now, which is what that claim measures to, and internal/manual/chat/what-i-can-do.md states the bound and what the list does if anything ever reaches it."
  - "internal/manual/chat/questions.md said `nothing yet asks you to strike an assumption or to unwind something already done`. The `ask` tool raises both shapes; that sentence is gone."
---

Lane T's proof run against a real model (#765) is what found all of these, and
every one of them is a promise some other part of the wave was already making —
a key in a table, a mark in a vocabulary, a sentence in the manual — with
nothing behind it. Nothing here changes a design; each restores something
docs/design/questions/DESIGN.md already states.

Two of the eight are not surface bugs at all but the road an answer takes from
another window, and they were invisible from either end: the presence file said
`working` while a turn sat inside the `ask` tool, and the doorstep refused a key
the surface had already checked against the question's own options.
