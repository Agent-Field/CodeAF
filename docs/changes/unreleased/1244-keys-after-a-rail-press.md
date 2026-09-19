---
kind: fixed
title: what is typed after a press on a run's row goes to its page, never the conversation
pr: 1244
surface: [chat]
invalidates:
  - "A task's page is read off the update loop, and until the answer came back the conversation's box still took the keyboard: a note typed right after the press was sent to the model as a message. From the press on, keys are kept in order and handed to the page's own keyboard when it opens; `esc` withdraws the press; a row that turns out to have no page opens its room and those keys are dropped."
  - "The stop card's key is read above every page, so with a task running a note holding that letter raised the card while the page was still on its way. A page on its way stands the card down, as a page that is up already did."
---

Seen on the real binary, hosted, on 2026-09-19: `keep the examples short` and
`enter`, typed at once after a press on the run's row, was answered by the model
and saved as a standing preference nobody asked for. The same drive after the
change: the page opened with the note under `notes` as `you · now`, and the
words were in the run's record and nowhere in the conversation.

Measured the same day: the read behind the page takes two milliseconds. The
seconds a person waited were spent in line behind one other door, the run's
summary, which asks a model with a ten second budget on the same ordered line.
That is its own change.
