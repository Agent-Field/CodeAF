---
kind: changed
title: the spend place's models table is a table — bars and counts in columns, and money in cents
pr: 1040
surface: [chat]
invalidates:
  - "A model row on the spend place hung its bar one space behind the name and
    the role word — `· opus 4.1 · conversation ████████████ 312 calls` — so every
    bar and every call count in `what ran it` started in a different column. Both
    now stand in columns measured off the widest name-and-role actually on the
    page ([spendReading.modelCols]), capped at `spendModelHeadCap`. The counts
    clear the bar's whole 12-cell reservation rather than each bar's own length."
  - "`spendMoneyWord` wrote the words `under a cent` under half a cent and fell
    through to `dollars` above it, so the money column mixed `$21.40`, `$0.0068`
    and a phrase. It is a column of cents with a floor now: anything above zero
    and under a cent reads `$0.01`. It rounds UP, so rows of slivers can add to
    more than the window total above them — the heading, the pointer line and the
    Spending tab all still carry the exact arithmetic through `dollars`."
  - "The detached-turn note used `spendMoneyWord` and so said `it spent under a
    cent`, which internal/manual/chat/keys.md quotes in those words. That phrase
    is `spendSliverWord` now and the note is unchanged; the old rule lives there
    and nowhere else."
---

Each bar is that model's share of the dearest one, and a share is read off the
bars' ends — which says nothing while their starts are ragged. The first cut of
this used a fixed 30-cell column and was exactly one cell short of
`· claude-opus-4.1 · conversation`, putting the only row with a role word out of
line with every row without one: a constant is a guess, and a guess one cell
short of some real pair of words fails on somebody else's models. The table
measures itself instead.
