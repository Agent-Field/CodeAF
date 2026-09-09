---
kind: changed
title: the landing card asks one question, in one word, with two answers
pr: 689
surface: [chat]
invalidates:
  - >-
    The landed card used to spell its own state words. It drew `failed`,
    `awaiting review`, `needs your look`, `delivery needs attention`,
    `stopped — branch kept` and `what it produced was not taken as done` out of
    its own switch over three booleans. All six are gone from it: the head now
    reads `session.TaskStatus.Word` — `done`, `stopped`, `incomplete` or
    `your call` — and the reason comes from the engine too. The card computes no
    tier and spells no state of its own.
  - >-
    The answers row was `[a] accept · [l] look again · [n] not right · [d] decide
    these for me`, above a line reading `finished, but nobody has checked it —
    your call`. It is now `[a] <yes> · [n] <no> · [s] tell it · [d] let aforge
    decide this one`, where the first two words are `session.TaskAsk`'s and
    change with the question (`resolve it`/`drop it` on a conflict, `accept
    anyway` on a held landing). The ask line is gone; the reason row stands in
    its place.
  - >-
    `[d]` no longer writes `task.settle`. It used to flip the persistent setting
    to `auto` on the way past, which is a preference disguised as an answer; it
    now hands over THIS card only. The standing row lives in `/settings` under
    Session and nowhere else.
  - >-
    `l` no longer does anything on a landing card, and `check again` is no longer
    offered to a person: the engine retries a check that never answered by
    itself. `s`, `d` and `t` are the new letters — `t` only while the card says
    `aforge is deciding`.
  - >-
    `internal/tui3/taskdeliverylabel.go` is gone with its `doneDeliveryWord` and
    `deliveryProblem`. A conflicted or aborted merge is a fact line (`branch
    kept`) beside a state word, not a label of its own.
  - >-
    The card's head no longer names where in-place work landed. `merged`,
    `branch kept` or nothing is the whole of the merge fact; work done in the
    person's own folder has no delivery to report. The expansion still names the
    place beside the branch.
  - >-
    The e2e needles `settleAskWord`, `settleAccept`, `settleNotRight` and
    `taskLookWord` are now sourced from `internal/session` rather than
    `internal/tui3`, because that is where those words are spelled now.
    `settleTellIt` is new.
---

The card is the only place a delegated piece of work reports back, and it had
grown four vocabularies for three states. It reads the engine's one reading now
(`session.ProjectTask`, #668): the glyph is the tier, the word is
`TaskStatus.Word`, and the chips are `TaskAsk`'s own two verbs — so a chip, a
rail row and the note the model reads are three drawings of one sentence.

`[r] rerun from its branch` is absent rather than dead: nothing in this process
re-runs a finished task, so the verb is not named. `[s] tell it` is absent on a
window whose agent has no task pages, and `[a]` is absent on a conflict whose
engine has no merge round — each column is dropped on its own, and its letter
with it.
