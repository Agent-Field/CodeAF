---
kind: changed
title: moving a conversation out of another window asks with a card, and home can ask at all
pr: 758
surface: [chat]
invalidates:
  - "MOVING A CONVERSATION WAS TWO ENTERS AND THE SECOND ONE DID IT. `enter` on a held row armed it and put a sentence on home's foot line — `open in another window · working — enter again to move it here (that window's reply stops there; its tasks resume here)` — thirty rows from the row it was about, and the next `enter` ended the other window. It is a confirmation on home's own card now: `Move this conversation here?`, with `1 move it here` and `2 leave it there`, the cursor on `leave it there`. Leaning on `enter` answers `leave it there`. Moving it is `1` or `→` and then `enter`."
  - "takeoverArmedWord, takeoverAgainWord and takeoverCostWords are deleted. What moving it costs is the card's reason row — `its reply stops there; its tasks come here` — which wraps rather than cutting, because home lends this card fifty columns and a promise cut in half is worse than two rows of it. That wrapping is questionCardRows' own rule now and applies to every card the block draws."
  - "Below homeCardMin there is no card on home at all, and the door was told only on the foot. The QUESTION is told there instead, on one row, built out of its own words — `Move this conversation here? · 1 move it here · 2 leave it there · its reply stops there; its tasks come here · esc leave it there` — and cut by the same fitter every other long sentence on home is cut by. The way out is the last clause, because that is the one hintFit protects."
  - "The row under the cursor grew `another window · enter brings it here` while its own question was already on screen beside it. It keeps the short word while a question this window raised is up: an offer already taken up is not an offer (switcherPaint.asking)."
  - "HOME COULD NOT ASK ANYTHING. It takes the frame whole (view.go's placeFrameNow), so the question block pinned above the message box is not on screen at all while somebody is standing there — which is why every door on home that ended something grew its own way of asking. homeconfirm.go is the seam: home holds ONE question, draws it with the block's own questionCardRows, and routes its keys through the block's own questionKeyOn. Nothing about the grammar is re-decided."
  - "app.questionKey was one function that found the head, checked the two block guards, and routed. The routing is questionKeyOn now, against a question the caller has already found, so home and the block answer to one grammar rather than to two."
  - "moveQuestionPick and moveQuestionHole scanned app.questions. They go through app.questionHeld, which looks in the block's queue AND at home's card — a lookup that knew about one of them left the other with a cursor that could not be moved."
  - "The `armed` field on homeView is still the row's state and still cleared by anything that moves the cursor. What it no longer carries is the promise: the card does."
---

Home is the one place on this surface with doors that end things and no way to
ask about them. The reason is structural rather than an oversight — home takes
the frame whole, so the block every other question on this surface is drawn on
is not there — and the cost of it was a two-key arm whose second key was `enter`,
on a screen that is a list you walk with `enter`.

So the seam had to be built before the door could move, and building it is most
of this change. `homeconfirm.go` is small on purpose: one question, the block's
renderer, the block's router, and a rule that walking off the row takes it down.
The takeover confirm is its first user and the standing card is meant to be its
second.

The thing worth reading twice is what the move costs in keystrokes. It was
`enter enter`; it is `enter`, `1`, `enter`. That is one more press for the
deliberate act and one fewer way to end another window by accident, which is the
trade `stop.go` made for the same reason and in the same words.
