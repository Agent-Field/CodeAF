---
kind: changed
title: A your-call landing opens its report with the question its row is asking
pr: 696
surface: [engine, chat, docs]
invalidates:
  - The landing report for work nobody could check opened "finished, but needs your look — ",
    and so did the report for a branch that would not merge. Both leads are gone. The report
    now opens with the same reason sentence the row is asking — "nobody could check it — " for
    a landing nobody could judge, "conflicts with your branch — " for one whose branch would
    not merge — built by yourCallLead from taskAskOf's own constants in
    internal/session/task_status.go. There is no needsLookLead any more.
  - A check that answered nothing wrote "nobody could say whether it holds". It now writes
    "the checker never answered".
  - The lead is never said twice. Three of the checker's own sentences already open with the
    question — checkerRanOut, checkerAskedTwice, checkerWindowClosedAlone — and where one of
    them does, that sentence is the lead and nothing is put in front of it (withYourCallLead).
  - waitWord (internal/session/principal_wire.go) said a prerequisite "needs your look". It
    says "is your call". taskGradeOutcome wrote "needs your look" into the ratings file and
    now writes "your call". Both spell it from taskWordYourCall, which is the one spelling,
    and so does pending.go's taskUnverifiedNews.
  - CLAUDE.md's vocabulary law listed the allowed words as "running, finishing, done,
    incomplete, or needs your look". The last one is "your call"; "needs your look",
    "awaiting review" and "unverified" are deleted as person-facing text.
  - A string literal in internal/session may no longer spell "needs your look" or
    "awaiting review". TestNoStringInThisPackageSpellsADeletedTaskWord walks the package
    with go/ast and names the file and line. Go identifiers — TaskUnverified and the rest —
    are untouched, and so are comments, several of which record on purpose which word used
    to be there.
---

The tiers landed the words; four places in the engine were still spelling the old
ones, each correct on its own page. A report that opened with one question while
the card beside it asked another was two accounts of one landing, which is the
disagreement the three tiers were drawn to end — so the lead is now read off the
projection rather than written a second time beside it. The manual pages that
quoted the old lead are corrected in the same change.
