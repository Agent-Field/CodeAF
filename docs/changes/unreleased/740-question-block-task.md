---
kind: changed
title: the task proposal is the question block now, answered by digits, and its card is the assignment
pr: 740
surface: [chat]
invalidates:
  - "The task proposal drew its own question in the transcript — a row of `[ yes ]  [ redirect ]  [ no ]` chips, a draining countdown meter, a row of model chips, and a claim on the keyboard. All four are deleted. The question block (internal/tui3/question.go) draws every proposal now, above the message box where every other decision on this surface is put, and what is left in the transcript is the ASSIGNMENT: the name, the sentence, where the work will run, what it will run on, and the brief behind ctrl+e."
  - "The proposal was answered with `enter` on a focused chip and `←`/`→` to move that focus. It is answered by the option's own digits — `[1] start it · [2] no` — which is the ONE KEY GRAMMAR every question on this surface takes. There is no cursor on the row: what is marked is the asker's PICK, which is what the clock is about to take."
  - "The proposal's two answers were `yes` and `no`. They are `start it` and `no` (session.AnswerOptions), because the clock spells the pick's own label in front of the time left and `yes in 9s` named no action at all. The row now reads `start it in 9s`."
  - "`esc` on a task proposal DECLINED it. It is LATER: the question folds to the chip `? 1 question · alt+a`, the proposal stays open, the engine stays waiting, and nothing is answered. Note the consequence — a proposal you esc and forget is a proposal the clock still starts."
  - "A COMPLETE BARE WORD TYPED INTO THE BOX ANSWERED THE PROPOSAL. `no`, `nope`, `n`, `stop`, `cancel`, `don't`, `dont` declined and `yes`, `y`, `ok`, `okay`, `go`, `sure` approved — thirteen answers, none of them drawn anywhere on the screen, each a different answer from the sentence that merely began with it. They are gone. Every sentence in the box is a correction, and a correction is a yes to the corrected version; the answers are on the row with their keys."
  - "session.Answer had no way to carry a task correction: the door read the key and dropped the words, so a typed answer to a proposal was refused as naming nothing. An answer with words and no key is now approve-with-redirect in session's applyToLane — the same reading task.go's own box had, said once, in the engine."
  - "THE MODEL SHORTLIST IS NOT OFFERED ANY MORE, and this is a capability lost rather than moved. A word that fitted several models raised a row of chips answered by `1`–`4`; those digits are the question's answers now. The work runs on the closest match — what the chips opened on and what the clock would have taken — and the block's meta line names it. Correcting it from the proposal is OWED; until it lands, saying so in the words `c change` takes is the way."
  - "The countdown was app.taskMeter — a twenty-cell draining bar and `auto-starts in 3.2s`, which is machinery describing itself. It is the block's policy line: the pick's own word and the time, `start it in 9s`. A proposal with no clock said `starts on your word`; it says `waiting`, which is what every question on this block says while something is waiting on it."
  - "Only a typed rune or a paste held the proposal's clock (app.holdTask off input.go and app.paste). EVERY KEY THE QUESTION READS holds it now, through the block's own hold (questionShown.held) — the same one-way door F41's reading clock goes through, and the first place on this block where a hold has to cross a connection. The pick survives the hold: what a held proposal stops being is a question that answers itself, not one the asker has an opinion about."
  - "The block took keys from a question it had never drawn. app.questionKey now refuses one with no shown stamp, which is either a half-typed sentence in the box or a fullscreen page over the top — and while home was up over a question the block was swallowing the letters somebody was typing into home's own box."
  - "A card-form question's answer rows were not pressable: only the line form's answers row and the narrow sheet's bands carried targets. Every answer row on the card is a band now, pressable along its whole width (question.go's questionCardRows), which is how a pointer reaches a proposal's answers at all."
  - "hitChoice, hitModel and app.choicePress are deleted with the rows they resolved. The standing card's own hitStandChoice is untouched."
  - "A REBROADCAST PROPOSAL DREW A SECOND BLOCK AND A SECOND RECEIPT. Answering holds the clock, holding rebroadcasts the notice (session.Agent.HoldTask), and app.proposeTask took anything that was not the still-open card for a new proposal — so one keystroke left two blocks in the transcript, re-raised the question, and wrote the receipt twice. A proposal whose id this window already has is an update, and a settled one is nothing at all."
  - "app.questionDrawnHere covered four lanes. It covers session.QuestionTask as well; the task proposal is drawn in exactly one place."
---

The proposal is the second block onto the one renderer and the first that had to
give something up. Everything the block asks for it already had — the settle
guard, the receipt, the chip, the never-modal keyboard, a typed answer — and
what it had that the block has no room for was a row of model chips answered by
the same digits the block spends on answers. Two readers for one keystroke is
the exact defect this wave exists to end, so the chips are gone and the loss is
written down here rather than quietly absorbed.

The other thing worth reading twice is `esc`. On this one question it used to be
the outright no, which was the honest reading of "get this off my screen" while
the block was modal. It is *later* now, and a proposal put off is a proposal the
clock will still start — which is a real change in what silence buys you, and
the reason the answers row says `start it in 9s` instead of counting down at
nothing.
