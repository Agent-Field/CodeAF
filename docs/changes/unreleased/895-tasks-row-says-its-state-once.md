---
kind: fixed
title: a row of the tasks place says its state once, and a root names its folder only where that is news
pr: 895
surface: [chat, engine]
invalidates:
  - "A row of the tasks place used to carry the record's own outcome sentence — `2 files · Annual is the default and the monthly price stays visible beside it.` It does not any more. What a row carries is what the work touched, then its state and the reason behind it (`your call · nobody could check it`, `incomplete · lost the connection`, `stopped`), then its cost. The landing's report sentence is on the record card, one `enter` away."
  - "`tasksMiddle` (internal/tui3/tasksplace.go) used to compose the state word and its reason itself, three different ways, and `tasksNote` spelled a fourth state word for a row nobody is behind. There is one join now and it is the engine's: `session.TaskStatus.RowWord()`. `tasksNote` says only which window the work is in."
  - "`checkerStalled` (internal/session/task_audit.go) used to print the abandoned call's bound with `time.Duration.String`, so a landing read `one call ran 29.24078975s without answering and was abandoned`. It is `taskSpanWord` now — `29s`. `checkerRanOut` still spells its window with `String()`, because that figure is a configured ceiling rather than a measured elapsed."
  - "A conversation root on the tasks place used to name any folder whose word was not the chat's own title, so a chat opened in a home directory wore a lone `~` at the right of its name and every row of the folder you were already in wore the same project word. Both were retired by docs/design/home-mission-control/DESIGN.md §1 and are now drawn as nothing. The rule lives in internal/tui3/projecttag.go as `chatProjectWord`, and home's `chatProjectTag` delegates to it."
---

Three defects on one screenshot, and every one of them was a single fact with
two writers. `internal/session` grades a node somebody stopped with the outcome
`stopped`, and the page hung that outcome off its own state word, so the row read
`stopped · stopped` — one reading said twice because each writer believed it was
the one saying it. The join already existed: `session.TaskStatus.RowWord()` puts
the word and its reason together and drops a reason that merely restates the
word, and it is what the column beside a conversation has always read. The page
asks it now instead of composing the pair itself.
