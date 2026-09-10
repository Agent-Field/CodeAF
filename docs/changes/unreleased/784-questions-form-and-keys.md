---
kind: fixed
title: a question's answers are each a row, its keys are digits, a checklist is answered on the card, and a switch leaves the other conversation's questions behind
pr: 784
surface: [chat, engine]
invalidates:
  - "The question block drew a `form: line` ask exactly as the model wrote it, so a checklist of eight with keys like `landscape` came out as one row cut at the edge with nothing to press past the fourth answer. Now the asker's `line` is a wish the evidence decides: structured input (checklist, blanks, pairs, dial) or an answer with a consequence or body beside it draws as a card whatever was asked for, and only plain answers that fit stay a line."
  - "The card kept its own cap of four answers under the engine's (`questionCardOptions`). It is gone; the engine's cap (four, eight on a checklist) is the only bound, every answer is a row, and a long label wraps onto rows that all press the same answer instead of being cut with `…`."
  - "A checklist could only be ticked in the sheet. On the card a digit ticks its row (again unticks), `space` ticks the cursor's, `tab` and `shift+tab` walk, and `enter` sends what is ticked — nothing when nothing is; the key row says `[enter] send what is ticked`."
  - "`ask` drew whatever key the model wrote. The engine now renumbers keys to `1`… in order and gives the answer back in the model's own keys with `labels` beside them (`Answer.Labels`); the tool schema says so."
  - "The question block and its records belonged to the app, so a conversation switch carried the withdrawn line of the one left above the box of the next. `attachConversation` forgets them when the agent changes; each lane's replay brings back its own."
  - "A withdrawn `ask` said `no longer needed · it is no longer needed`. Its reason is now `the turn moved on without it`."
---
