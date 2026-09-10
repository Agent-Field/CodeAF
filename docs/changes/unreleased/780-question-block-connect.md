---
kind: changed
title: the connect offer asks on the question block and its key is typed in the message box
pr: 780
surface: [chat, engine]
invalidates:
  - "The connect offer had a three-row block of its own directly under the approval question, ending in `[enter] connect · [esc] not now`, and it was MODAL: it took every keystroke on the frame while it was up, `y` and `n` included, and a key that was not one of its two answers typed nothing. It is a card on the question block now — `1 connect`, `2 not now`, `esc` means LATER — and the block is not modal, so you can keep typing under a question you have not answered."
  - "Saying yes to an account connected by a KEY opened a masked box inside the offer's own row, as a second step. There is no yes step: a bare yes to one of those is read as a decline by the engine anyway, so the question now arrives asking for the key, with what to type written under it and the MESSAGE BOX collecting the answer. The key is still never drawn — `session.InputShape` gained `Secret`, and the box masks itself one bullet a character with the count beside it."
  - "`enter` on an empty key box was a DECLINE. It answers nothing now: `enter` sends what is in the box and there is nothing in it. The way out is `2 not now`, which a key question keeps for exactly this reason — a question the turn is waiting on with no visible no is a question nobody can end."
  - "Over `--host` a browser sign-in drew its row with only `[esc] not now` on it. It draws only `2 not now`, and the reason line still says `connecting an account is not available over --host yet`."
---

The OAuth panel, the browser handoff and the waiting animation are untouched.
They are a REPORT rather than a question — a thing that happened, drawn in the
conversation where the reports are — and the answer still raises them.
