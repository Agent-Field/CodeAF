---
kind: fixed
title: a landing's report counts as evidence, and an absolute cd is dropped from a declared check
pr: 1212
surface: [chat, engine]
invalidates:
  - "After a hand-off landed and the conversation answered the question it owed, the completion check said the task's report never came back and the turn carried on once more with nothing to add. The landing's report was the turn's own message, and the check read all of it as the ask. The ask it reads is now the person's question alone, and the report is shown to it as something that came back."
  - "A check declared as `cd <absolute directory> && <command>` was refused as shell composition and a `the call was refused` card was drawn. A check runs from the root of the task's own copy and the absolute path is the person's checkout, so that leading step is dropped and the command is kept. Any other composition is refused as before."
---

Both were seen on the real binary on 2026-09-18, belt on, in a two-file
repository: the first proposal of a hand-off was refused for its check's
spelling, and after the landing replied correctly the conversation was told
`the ask is not finished` and took a turn that said nothing.
