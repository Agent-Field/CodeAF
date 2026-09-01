---
kind: fixed
title: a standing card's hint is built from the chips it drew, so it cannot name an absent answer
pr: 204
surface: [chat, docs]
invalidates:
  - "The errand pane's hint said `1 yes · 2 change when or where · 3 just once · 0 no` on every card, from a literal in `internal/tui3/homeexchange.go`'s `exchangeHint`. It says what the card drew: `1 yes · 2 change when or where · 0 no` on a one-off reminder, which offers no `just once`, and the four-answer line where the chip is there."
  - "`standProposalHint` and `standTwoHint` were the two hint lines in `internal/tui3/standing.go` and a third copy sat in the errand pane. None of the three exists. `standHintFields(card, decline)` walks `standingCard.row()` — the same row `app.standChips` paints — so the line names each drawn chip under its own digit and an absent answer is unnameable rather than merely unnamed; `standAskHint` and `standAskHintShort` are the conversation's two renderings of it."
  - "How the way out is named was baked into each hint. It is the caller's word now, because it differs by pane: a conversation passes `standNoEscWord` (`or esc, no`, built from `standNoWordChip` rather than respelling `no`) and the errand pane passes `standNoWordChip`, since esc there hands the keyboard back to the list instead of declining."
  - "`internal/tui3/rowfit.go` offered `rowAll`, every fact at its longest spelling, and no other end. `rowShort` is the other end — every known fact at its SHORTEST spelling — for a ladder that measures a line rather than being told a room, which is what the legend's hint slot does. The standing card's hint is the first caller: `app.hintShorter` now offers its brief form (`1 yes · 2 change · 3 once · 0 or esc, no`) instead of the legend dropping the slot whole on a narrow frame."
  - "`internal/e2e/tuiwords_test.go`'s `exchangeAnswerHint` waited for the four-answer line, and #191's entry recorded that as a known defect. It waits for the three a reminder's card offers, and its `source` is `change when or where` — the sentence is no longer a literal in `internal/tui3` for the gate to find."
  - "`internal/manual/chat/asking-from-home.md` spelled the pane's hint `1 yes · 2 change · 3 once · 0 no`, which the code has never drawn, and described `3` as one of four answers every card takes. It quotes both real lines, says the line is built from the chips, and marks `3 just once` as an answer a one-off reminder's card does not offer."
---

The law was already written down in `standing.go` — a hint offering a digit the
chips do not is the same defect as a chip that does nothing — and the errand pane
broke it by holding its own copy of the sentence. Two copies were right and the
third was wrong, which is what a second copy is for.

So the repair is not a fourth spelling chosen more carefully. A sentence somebody
types cannot know what was drawn; a sentence walked out of the chip row cannot
name what is missing from it, because the missing answer is not in the list.
`internal/tui3/standing_test.go`'s `TestEachStandingAnswerWordIsSpelledOnce`
parses the package and fails the build if any of the three answers is spelled as
a string literal outside its own constant.
