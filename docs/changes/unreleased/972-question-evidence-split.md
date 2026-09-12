---
kind: changed
title: An answer's evidence stands beside the answers, and the page a question opens is two panes
pr: 972
surface: [chat]
invalidates:
  - "A question's panel drew no evidence at all — the diagram, the diff or the table an answer brought was reachable only through `o open full`. No longer true: where the answers carry blocks and the frame is a hundred columns or wider, the panel splits and the evidence of the answer the pointer is on stands beside the list; narrower, it unfolds under that answer's own row. `railSlimFloor` (100) is the one reading of `two columns fit`, for the task column and for a question alike."
  - "The page `o` opens was one scrolling column of folding sections, one per answer, opened and folded with `→`/`←` and with `o` again. It is two panes now — the answers on the left, the evidence of the one you are standing on on the right — and one column only where the body is under a hundred cells. Nothing folds: the answer the pointer is on is the open one."
  - "On that page `→` opened the answer you were on and `←` folded it. `→ detail` now hands the arrows to the evidence pane, `↑↓ scroll` moves it, and `← back to the answers` hands them back."
  - "On that page a digit answered the question at once. It walks the pointer to that answer now and `enter` takes it; on the block above the box a digit still answers at once."
  - "The page drew `‹ back` and named the asker's answer `my pick`. It draws neither: the recommendation wears `◆ recommended` on its own row in every view, and `esc` is the way out."
  - "The asker's two-pane layout block went side by side above a hundred columns and stacked below. It decides by its content now — each pane's widest line fits its half, or the two stack — so a layout drawn in the pane beside a list is laid out on what it is rather than on how wide the window is."
  - "A question put off drew its fold rule wherever the block stood, including under the page that was showing it. The page's own question no longer draws that rule: `o` folds what it opens, and `space open` under an open page was offering what was already open."
  - "The tmux suite waited for `[o] open it`, a spelling the surface stopped using when the keys lost their brackets. The needle is `o open full`."
  - "`what it showed you` — the attachments the asker brought for the whole decision — stood ABOVE the answers, which was itself a fix to an owner complaint that the evidence was buried under the list. It stands UNDER the answers now, in the left pane below the list, because the pane on the right is where an ANSWER's own evidence goes and the two would otherwise read as one column of attachments belonging to nothing in particular. `TestTheQuestionsOwnEvidenceIsOneTitledSectionAboveTheAnswers` carried the old position and is deleted; `questionroom_test.go` pins the new one."
  - "The panel's split and the page each worked out the answer column's width and how wide the list wanted to be, and the two formulas disagreed: labels of 5 and 30 in a forty-cell list padded to 20 in one drawing and 5 in the other, and the panel reserved fourteen cells for `◆ recommended` on questions that have no recommendation. `questionLabelPad` and `app.questionListWant` are the one account of each, called from every drawing of the list."
  - "The page's foot re-derived the seam its rule meets, and drew that rule at the FULL frame width while the body draws at `bodyWidth()` — so with the task column up on a 140-column terminal the page's top rule stopped at the body's edge and the foot's rule ran on underneath the rail. The foot reads the seam the body actually drew (`app.questionRoomSeam` is now a read, not a calculation) and draws at the body's width, and the screens table covers a split with the column UP, which every earlier shot stowed."
  - "An open answer's label was `ordinary ink` everywhere it was drawn. In the EVIDENCE it is bold ink — the answer's own word is the pane's title, and the pane is the one drawing where that word is a heading rather than a row. It is still INK: #933's colour ruling puts the hue on the three marks alone, and weight is not hue. On a row — the panel's list, the page's list, a card — the label is plain ink, unchanged."
  - "`questionPanelBody` — the door the task record's card and home's errand pane draw a question's answers through — briefly unfolded the FULL evidence of the answer under the pointer, every block included, on both of those surfaces. It draws none: both callers put these rows inside a card, and a card that grew by every diagram an answer brought would push the rest of its column off the screen with no way to say that it cut. The split and the unfold belong to the panel and the page, which own their own height."
---

A question is a decision handed to a person with its evidence attached, and the
evidence is per answer — so it is drawn where the answer is, at every size the
question has. One account of the case (`then ·`, `why this one ·`, `would switch
if`, `confidence ·`) and one renderer for the blocks now serve the panel's pane,
the panel's unfold, the page's pane and the page's one column; before this the
page spelled the labelled lines itself and the panel spelled its one-line case
itself, and the two disagreed about whether a model's `If you…` was joined onto
`would switch if` twice.
