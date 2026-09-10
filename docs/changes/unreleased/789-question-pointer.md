---
kind: fixed
title: every question has a pointer the arrows walk and enter takes, and the task page shows its question
pr: 789
surface: [chat]
invalidates:
  - "Only the stop/close confirmation had a cursor; every other question answered to digits and to `enter` on the model's pick, and `↑`/`↓` did nothing on the block. Every question with answers now has the person's pointer — `▸` on a card, the selected band on a line — walked by `↑↓`, `←→`, `tab`/`shift+tab`; `enter` takes it and a digit still answers at once. It starts on the asker's pick, else the first answer, and on a confirmation on the answer that loses nothing."
  - "`▸` on a card marked the asker's recommendation. It is the person's pointer now everywhere; the recommended answer says `suggested` in its consequence column instead."
  - "The key row said `[enter] take the pick`, and only when the asker had picked. It says `[enter] take it` on every question with answers, with `[↑↓] choose` on a card and `[←→] choose` on a line while there is room."
  - "A card drew the note under an answer and the consequence beside its label in the same dim ink. The note reads in the prose ink now; the consequence stays dim."
  - "`enter` on a needs-you row opened the task's record page with no question on it and no `a`/`n`/`s` — the landing question was only on the block above the conversation's box, which a place covers. The record page now draws the node's landing (or conflict) question above its foot — head, reason, `[a] accept · [n] not right · [s] tell it` — walks it with `←→`, and answers through the block's own door; `s tell it` puts the page down and opens the room. Only this conversation's nodes get a question there."
  - "`c change` and `? ask back` on the block changed nothing on the screen, and the words typed after them went out as a chat message unless the question owned the box. Both now turn the answers row into a prompt (`change: say what you want different, then enter · it goes with [2] …`, `ask back: type your question, then enter · the question stays open`); every letter types while it is up, `enter` sends the words with the pointed answer (`c`) or to the asker with the question still open (`?`), and `esc` gives the box back without answering."
  - "A card drew an answer's consequence above its note. The note (what the answer means) comes first, in the prose ink; the consequence follows, dim."
  - "`alt+a` (the questions chip) did nothing from the tasks place or a task's record. It closes them and raises the block, as it already did from settings, expand and home."
---
