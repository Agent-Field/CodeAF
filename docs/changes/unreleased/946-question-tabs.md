---
kind: changed
title: questions from one step are one panel with tabs, and permissions from one batch one frame
pr: 946
surface: [chat, docs]
invalidates:
  - "Two or more questions from one step were a SHEET (internal/tui3/questionsheet.go): a
    list grouped by shape, answered row by row, sent with `s`, with `g` spreading one answer
    over every row alike. The sheet, `s`, `g` and formsSheet are deleted. Those questions are
    now ONE panel with a tab per question in its top edge and a review tab last
    (internal/tui3/questionset.go): `←→` move between questions, `↑↓` still choose, `enter`
    or a digit HOLDS the answer and moves on, and the review sends every held answer through
    answerQuestions in tab order, in one command. A question nobody answered stays open."
  - "What counted as 'the same step' was the SURFACE's guess: questionStep, a three-second
    gather (questionGatherMsg) and questionReach.pending held a quiet question back until a
    step boundary. All of it is deleted. The one key is the engine's step token,
    session.Question.Batch (#910); nothing is held back, and a question is pinned the moment
    it arrives."
  - "Several permissions from one tool batch each got a frame of their own, one after
    another. Two or more with the same Batch, each with a plain grant and a safe answer, are
    now ONE frame: what each call wants, then `allow all N · one by one · deny all`, with the
    pointer on `deny all` where each of them asked alone would open on `deny`. `one by one`
    opens them as tabs. The frame carries no `how long` row, for the reason a single
    permission carries none (#953). An irreversible or
    confirmation question never joins a set or a frame."
  - "docs/design/questions/DESIGN.md said questions were BATCHED AT THE BOUNDARY. It now says
    ONE STEP, ONE PANEL, and the gallery's 'Several at once — the sheet' screens are replaced
    by the tabs screens, with a 'Several approvals at once — one frame' section beside them."
---

The step was always the engine's to know: a model that calls three tools at once raises
three questions in one step, and only the engine saw the step begin and end. The surface's
own copy of it guessed with a clock, and while it held a question back the turn waiting on
that question could not move. Keying the set on `Question.Batch` removes the guess and the
clock together, and the review sending through the one door keeps F's law that one gesture
is one command, in order.
