---
kind: changed
title: the standing card asks on the question block, and esc on it now means later rather than no
pr: 780
surface: [chat, engine]
invalidates:
  - "`esc` on a standing card was the OUTRIGHT NO — it declined the reminder and the session was told. On the question block `esc` means LATER on every question, including this one: nothing is set up either way, the card stays open, the question folds to the `? N questions` chip and the count goes on counting it. The outright no is `0 no`, which is drawn on the card as an answer you can see and click — where `esc` never was."
  - "The standing card drew its own answers: a walkable chip row (`[ 1 yes, set it up ]  [ 2 change when or where ]  [ 3 just once ]  [ 0 no ]`), a cursor moved with ←/→, `enter` to take the one under it, and a hint line under the card. All of it is deleted. The card in the transcript keeps its head, its `when ·`/`where ·`/`costs ·` bands, its draining meter and its news line; the answers are on the block above the message box with everything else being waited on, each with what it costs beside it."
  - "`2 change when or where` was how you corrected the time or the place. It is `c change` now — the key comes from the block's one key table (`internal/tui3/questionkeys.go`) rather than from a chip on the card, and it turns the message box into the correction lane exactly as it did."
  - "A digit pressed while a standing card was up was answered by the card's own key router. The block is NOT MODAL: a digit is the question's only over an EMPTY box, and every printable key belongs to the composer the moment there are words in it."
---

The card SHOWS and the block ASKS, which is the split the whole questions wave is
about: one renderer for every decision the engine hands a person, and the thing
being decided about left where it is, scrollable, in the conversation.
