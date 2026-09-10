---
kind: changed
title: the stop card and the close-a-tab card are the question block now, answered by a cursor and enter
pr: 758
surface: [chat]
invalidates:
  - "internal/tui3/stop.go drew its own card — its own two-answer row, its own cursor, its own pointer targets, its own key set. It draws nothing. The question block (internal/tui3/question.go) draws it as a session.AskConfirmation, and what is left in stop.go is the LANE'S: which work `x` was pressed at, what stopping it does not take away, and the engine door that ends it."
  - "internal/tui3/tabclose.go drew its own card the same way and is the same size smaller. Its three answers are the block's answers, one to a row, each with what taking it does written beside it."
  - "The close-a-tab card was answered by `k` (keep running) and `s` (stop work) OUTRIGHT — one keystroke, no cursor, work ended. `k` and `s` are not keys on it. Every answer carries its own digit, a digit MOVES THE CURSOR onto the answer it names, and `enter` is what decides. That was always this card's law and it is now spelled the way the block spells it."
  - "Both cards drew their answers as a row of bracketed words with a `▌` beside the cursored one. They draw one answer per row — `  1  keep running  it keeps going here…` — the whole row is pressable and the cursored row is lit on the ground ladder's selected band. No answer is ever cut to fit."
  - "stopTarget.question() returned `Stop this task? Its work halts; the branch it wrote on is kept.` — the question and the promise in one sentence. It returns `Stop this task?` and the promise is stopTarget.promise(), which travels as the question's Reason and is drawn on its own row. On the old card the two ran together; on the block the head and the reason are separate rows and one sentence carrying both said it twice."
  - "A question raised by the person's own gesture now sorts to the FRONT of the block's queue (questionRaisedHere), ahead of anything older waiting there. Age is still the order for everything the engine asks. Without this, `esc` on a stop card raised over a five-minute-old permission folded the permission and left the card standing."
  - "The block refused a key to any question it had not drawn and dropped one that arrived inside the 250ms settle guard. Neither applies to a confirmation the person raised themselves: the screen a moment ago was the one they pressed `x` or `ctrl+w` on, and making them wait a beat to cancel their own gesture is a card that eats the `esc` they pressed to take it back."
  - "Every question on the block leaves the message box alone — a printable key is text and `enter` sends the sentence. A confirmation raised by a gesture is the exception: while it is up the draft is parked and unsendable, because `enter` landing in the conversation would send a message to an agent the card is offering to stop. Both cards have always taken the whole keyboard while they were up; that is unchanged."
  - "A question the block answers leaves a receipt above the box — `decided needs your ok to run bash → allow once · you · 14:02`. A card the person raised themselves leaves NONE. The act is the receipt: the tab is gone, or the work stopped and the engine's own sentence is in the conversation, or nothing happened. `decided Close this tab? … → cancel` is a row about a question somebody took back."
  - "The card's answers were drawn as ragged rows — `1  keep running  it keeps…`, `2  stop work  the reply…` — with each consequence starting wherever its word ended. Every answer's word is padded out to the widest word on the card, so the consequences stand in a column. This is the block's own card form and it changes every card, not only these two."
  - "app.guardRows drew the steer guard AND the stop card in the room's slot. It draws the steer guard alone; the room shows the block's card in the shared chrome above the box, which is where the conversation shows it."
---

The two cards a person raises with their own hand were the only blocks on this
surface whose laws were already the block's laws. `NOTHING IS DECIDED BY ONE
KEYSTROKE`, `the cursor starts on the answer that loses nothing`, `esc is never
the act`, `no bypass key and no don't-ask-me-again`, `a tab with nothing in
flight is not asked about at all` — every one of them survives the move
verbatim, because `session.AskConfirmation` is the shape they were written into
the block as.

What the move actually costs is `k` and `s`, and they are the reason it was
worth making. They were the one bypass on a card built to have none: `s` on a
busy tab ended a turn and every running task in it, from one keystroke, with the
cursor still sitting on `keep running`. The digits that replace them cannot do
that — they move the cursor and stop.
