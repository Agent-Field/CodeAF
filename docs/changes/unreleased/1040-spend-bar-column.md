---
kind: changed
title: the spend place's model bars stand in one column instead of hanging off the end of each name
pr: 1040
surface: [chat]
invalidates:
  - "A model row on the spend place hung its bar exactly one space behind the
    name and the role word — `· opus 4.1 · conversation ████████████` — so every
    bar in `what ran it` started in a different column and the set of them could
    not be compared by eye at all. They now begin a constant distance from the
    start of the model name (`spendModelBarCol`, 30 cells), and a name and role
    too wide for that column push their own bar rather than being cut down to
    fit it."
---

Each bar is that model's share of the dearest one, and a share is read off the
bars' ends — which says nothing while their starts are ragged. On the demo
fixture the second-dearest model's bar began further right than the fourth's
ended, so the one column a person opens this table to compare was the one thing
on it they could not.
