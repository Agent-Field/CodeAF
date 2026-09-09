---
kind: changed
title: the chat manual learns the three tiers, and stops teaching five vocabularies for one state
pr: 679
surface: [chat, docs]
invalidates:
  - "The corpus taught `needs your look` as the word for a landing nobody could check — on the card, the roster, home, the tasks place's own section heading and the rail. That word is not on a task anywhere any more: the pages say `your call`, always with the reason beside it (`your call · nobody could check it`, `your call · conflicts with your branch: parser.go`)."
  - "`awaiting review` was documented in `task-rooms-after-restart.md` as what a card, page footer and sidebar said after a handoff to the chat. The page now says the word is gone and that a handed-over card reads `nobody could check it · aforge is deciding · [t] take it back` instead."
  - "`failed` was a word on cards, home rows and the landing note (`task 7 failed:`). Every page now says `incomplete` and the reason beside it, with the whole `TaskReasonOf` table written out ending by ending, and states plainly that `failed` is the engine's own name and never something a person reads."
  - "The answers row was documented everywhere as `[a] accept · [l] look again · [n] not right · [d] decide these for me`. It is `[a] <yes> · [n] <no> · [s] tell it`, whose verbs change with what is being asked (accept/not right, resolve it/drop it, approve/decline, start/don't, accept anyway/not right, raise the cap/stop it), with `[d] let aforge decide this one` drawn dimmer under them."
  - "`[l] look again` was described as a person's answer. It is gone: the pages now say the engine has the check asked a second time by itself before anything reaches you, and that the verb survives for the model alone."
  - "`[d] decide these for me` was documented as flipping `task.settle` to `auto` for good. `[d]` hands over the one card in front of you and CHANGES NO SETTING; the standing preference lives in `/settings` only."
  - "Under `task.settle = auto` the pages said the card simply draws no choices. It says why, on the reason row, with the way back on it — `aforge is deciding · [t] take it back` — and a task never stays unowned past the end of a turn."
  - "The pages said a task's state was read off one word. They now teach the three tiers and their glyphs — moving (`◌`/`▸`), over (`✓`/`⊘`/`✗`) and your call (`?`) — and the law that a row never reads a bare `waiting` or a bare `your call`."
  - "`unverified` was used in `tasks.md` and `how-tasks-run.md` for files nobody built. The word is off the banned list's wrong side: both now say nothing looked at them."
  - "`internal/manual/chat_test.go` had no probe for any of the new words. Twelve were added — what does your call mean, why does it say incomplete, what does incomplete mean vs stopped, the task has a conflict, it says aforge is deciding, how do I take a task back, why is there no check again, does saying looks good accept the task, and the rest."
---

The engine spelled the words once (#668); this is the only account of them the
chat has. Where the code still spells an old word — the landing report's own
`finished, but needs your look — ` lead, `waitWord` in `principal_wire.go`, and
a STANDING ORDER's `needs your look`, which is not a task — the pages follow the
code and say so, so the corpus stays a reading of the program rather than of the
design.
