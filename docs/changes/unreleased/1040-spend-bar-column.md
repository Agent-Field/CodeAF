---
kind: changed
title: the spend place's two tables are tables — every field in a column, and money in cents
pr: 1040
surface: [chat]
invalidates:
  - "A model row on the spend place was one run of text — `· opus 4.1 ·
    conversation ████ 312 calls` — so the bar and the counts began wherever that
    row's name and role happened to end. Every field stands in a measured column
    now ([spendReading.modelCols]): the name, the role, the bar, then counts that
    clear the bar's whole 12-cell reservation rather than each bar's own length."
  - "THE ROLE HAS A COLUMN OF ITS OWN AND HOLDS IT WHETHER OR NOT ANY ROW USES
    IT, floored at the widest label in `config.ModelSlots()`. Sharing a field
    with the name put the one row wearing a role word out of line with every row
    without one — and since exactly one slot answers on this surface, that was
    the ordinary table, not an edge of it. Bars no longer move when the
    conversation's model changes under them."
  - "Rows of `what it was for` were the name and then two facts joined by ` · `,
    so the project and the kind word landed wherever each name ended. They stand
    in columns now too ([spendReading.subjectCols])."
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
  - "A subject whose ledger line named no workspace drew `· talk-1 · . · a
    conversation`: `filepath.Base` answers `.` for the empty string. An unknown
    project draws nothing now, which is what the emptiness law always demanded of
    it ([spendSubjectTag])."
  - "The models table gave up its bar and counts below a flat 80 cells. The gate
    is measured against what the table actually holds now, so a frame drops them
    when it genuinely cannot carry them rather than at a round number."
---

Each bar is that model's share of the dearest one, and a share is read off the
bars' ends — which says nothing while their starts are ragged. Two earlier cuts
of this got it wrong in the same way and are worth knowing about: a fixed
30-cell column was exactly one cell short of `· claude-opus-4.1 · conversation`,
and measuring name-and-role as one field still let the single bound model's bar
sit two cells right of every other. A constant is a guess, and a field shared
with something optional is a column that moves — so each field measures itself
and the optional one holds its width unconditionally.
