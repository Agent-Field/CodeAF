---
kind: changed
title: every row of work but the landing card draws one tier, one glyph and one word
pr: 694
surface: [chat]
invalidates:
  - "`needs your look` was the word for a landing nobody could check. It is `your call`, plus the reason, everywhere a row is drawn — the rail, the roster, the record, the room header, home, and home's standing rows. Only the landing card still says the old word, until the card lane lands."
  - "`awaiting review` was a state a node took when the settle policy handed its card to the model. It is not a state and never was: the row keeps its tier, its word and its reason, and who is holding the question is `session.TaskAsk.Owner`. The row is PARKED, never done."
  - "`failed` was what the room header and the record said about a run the wire, a threshold or a check ended. A person reads `incomplete` plus the reason internal/session spells for that ending; `failed` stays the engine's own state name and reaches no screen."
  - "`stopped — branch kept` was one sentence for a landing whose branch never came home. The state and the branch are two facts now: `stopped · task/parser`, `incomplete · ran out of steps · task/parser`. On a narrow column the reason gives way and the branch stays."
  - "A row could read a bare `waiting` or a bare `your call`. It cannot: the reason travels with the word wherever either is drawn (`session.TaskStatus.RowWord`)."
  - "The rail, the roster, home and the record page each had their own table over `TaskState`/`TaskPresence` for the glyph and the word. There is one, `internal/tui3/tasktier.go`, and it reads `session.TaskStatus.Tier`. No surface works a tier out of a state for itself."
  - "`!` was the cell for work that did not finish and `✕`/`○`/`◐` were the roster's own alphabet. There are five cells on the whole surface — `◌ ▸` moving, `✓ ⊘ ✗` over, `?` your call — and the roster draws the same five, with one stated exception: it keeps `◐` for work in flight because `▸` is already the mark that shuts a fold on that page."
  - "The proposal meter read `waiting on you` when the engine held it open with no clock. It reads `starts on your word` — the reading's own sentence for that question. `waiting on you` stays home's phrase for a conversation that wants somebody."
  - "`taskUnverifiedWord`, `taskUnverifiedGloss`, `taskUnverifiedWaits`, `taskStoppedKept`, `roomFailedWord` and `glyphUnverified` were constants in task.go and room.go. They are gone; the four the landing card still reaches for stand in `internal/tui3/tasks1words.go`, which is the card lane's to delete."
---

Four tables answering one question is four answers, and they disagreed — a task a
person stopped drew `⊘` on the roster and `✗` on its own page, and one landing had
three names on three screens a keypress apart. The question every row answers is
`do I need to do anything`, it has three answers, and both the cell and the word
for it are written down once: the tier in `internal/session`, the drawing in
`internal/tui3/tasktier.go`. Two structural tests ride `make test-laws` and fail on
any source in the package that spells a deleted state word again.
