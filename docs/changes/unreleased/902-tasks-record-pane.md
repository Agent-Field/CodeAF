---
kind: added
title: The tasks place draws the cursor row's record beside the list, and answers it there
pr: 902
surface: [chat]
invalidates:
  - "The tasks place was a list and nothing else, and the only way to learn what a
    row did was to press enter and read its card. It still is under 110 columns;
    at 110 and over the body SPLITS — the list keeps the left, a dim seam divides
    it, and the right is the cursor row's record: title, `state · reason`, one
    line of figures (files, cost, model, how long ago), the branch and the ground
    ladder's word for the copy of the folder the work happened in, the first
    paragraph of what the task said at the end, up to five file paths then `N
    more`, and a verb line. It follows the cursor; nothing is opened."
  - "`1` and `2` were typed into the tasks place's filter like every other
    printable key. They still are, EXCEPT over a row the pane is drawing answers
    for — a task of this conversation's that is waiting on you, on a frame wide
    enough for the pane — where they answer it. The landing's own letters `a` and
    `n` are unchanged and still answer on the card; the digit is a second name for
    the same answer, taken from the answer's position on the question's own row."
  - "A click on a row of the tasks place opened it on the first press. Where the
    pane is drawn the first click now moves the cursor and the pane follows, and a
    second click on the same row opens it — the pointer grammar every place with a
    preview keeps. Under 110 columns there is no preview for a first click to buy,
    so one click opens as it always did. A click on one of the pane's own answers
    presses that answer and opens nothing."
  - "`place.wheel` answered a bare `bool` while `place.press` answered `(tea.Cmd,
    bool)`, so a place with work to do about a cursor the WHEEL moved had no way
    to say so. `wheel` now answers `(tea.Cmd, bool)` like `press`."
  - "`cmd/aforge-demo-home` wrote each task's transcript as a bare filesystem
    PATH. `session.TaskRecordPath` answers \"\" for anything that is not a
    `file://` URI, so on the demo home every record card drew a row that named no
    journal and said nothing the node said — the fixture looked like a machine
    whose transcripts had all been deleted. It writes `file://` URIs now, and its
    one your-call row carries a branch and a ground rung, which nothing in that
    fixture ever had."
---

The list is a column of names, and a name is almost never what somebody came for:
"what did this do", "what did it cost", "where did it leave my files", "do I
accept it". Every one of those used to cost a keypress into the card, a read and
an `esc` back to a list you then had to find your place in again, so reading down
a morning's work was ten opens. The pane answers all four as the cursor passes.

It is the record card's own rows at a narrower width rather than a second
renderer — the two draw from one set of builders in `taskrecord.go`, so they
cannot disagree about one piece of work — and the seam between the halves is
`railSeam`, the same rule the roster and the room already split on.

Nothing on the draw path touches a disk. The report under the title is read off
the loop once the cursor has stood still for a beat, and kept per row, so a held
`↓` through twenty rows reads nothing at all and walking back up is free. Until
the read lands that part of the pane is empty rather than a spinner, which is the
emptiness law applied to a fact that is merely late.
