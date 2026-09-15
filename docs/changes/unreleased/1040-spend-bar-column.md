---
kind: changed
title: the spend place is three tables in four columns each — no bars, one right edge, money in cents
pr: 1040
surface: [chat]
invalidates:
  - "THE MODEL BARS ARE GONE. Every model row on the spend place carried a bar —
    that model's share of the dearest one — and the row now reads `· opus 4.1 ·
    conversation · 312 calls · 18.1M tokens · $21.40` and nothing else. The list
    is sorted dearest first and each row says what it cost, which is the same
    comparison in figures; the bar cost a reserved column and a second
    reservation in front of it so a role word on one row could not push it out of
    line. `spendBar` and `spendModelBarCap` are deleted."
  - "Both tables were one run of text per row, so every field landed wherever the
    row's name happened to end. Each table is measured once and drawn into
    columns now ([spendMeasured], [spendRowIn], shared by both), and the counts
    are RIGHT-ALIGNED — digits are compared from the right, and with the bar gone
    the figures are the whole of the comparison. The unit word rides behind each
    numeral (`9,400 calls`, `163M tokens`) because this surface has no header
    row."
  - "`what ran it` is name · ROLE · calls · tokens · money, the role first after
    the name: what a model IS on this machine reads with the name it follows,
    while the calls, the tokens and the money are three readings of one quantity
    and stand together. It held a reserved column of its own floored at the
    widest `config.ModelSlots()` label — that existed only to keep one bound
    row's bar in line — and `spendReading.modelCols` and `roleCol` are deleted."
  - "The role no longer wears the `·` that used to introduce it. A column needs
    no mark saying a column has begun."
  - "The token column carries NO UNIT WORD: `3.2B`, not `3.2B tokens`. Beside
    `128,400 calls` it is already plainly a different kind of number, and the
    word repeated down a column said nothing the k/M/B did not."
  - "`what it was for` is name · KIND · project · money, the kind word first
    after the name for the role's reason. Where a field stands and what it is
    worth are two different questions: the kind word leads the block and is still
    the first thing a narrow frame gives up."
  - "THE NAME IS LEFT-ALIGNED AND EVERY OTHER COLUMN IS PUSHED RIGHT AND
    RIGHT-ALIGNED. Fields used to start in their columns and run left to right
    behind the name. What a row is about is read from the left; what it cost and
    how many calls it took are read by comparison with the row above, and a
    comparison is made on a figure's right-hand edge — so the facts travel
    together in one block and the names run out to meet them."
  - "AND THAT BLOCK ENDS WHERE THE CHART ENDS ([spendReading.rule]) rather than
    at the frame. The money was flushed to the frame's right edge, which on a
    wide terminal put the one figure every row is read for forty cells from the
    counts it belongs with, on a page that already draws a horizontal scale. The
    tables now end on the chart's `today` point, with the last bucket's date
    standing under it. A window zoomed until its chart is a few cells wide falls
    back to the frame, with every field back — a table measured against a rule it
    cannot meet must not give up fields the frame behind it had room for."
  - "STANDING PROMISES ARE A TABLE OF THEIR OWN, under a new heading `what kept
    running · standing orders, and what a firing cost`, with name · firings ·
    what a firing cost · money. They used to share `what it was for`, where the
    promise wore a `standing · 88 firings` tag crammed into the project's column
    and a kind word that had to be suppressed to stop the row saying `standing`
    twice. The fold under the two tables still counts the whole subject list, and
    `enter` on a promise still opens the standing place."
  - "The words `under a cent a run` are gone with that suppression: a promise's
    per-firing figure is written by the money column's own rule, so a firing that
    cost a twentieth of a cent reads `$0.01 a run`."
  - "`session.UsageSubjectWord` answered `a task` and `a conversation`. Those are
    column words now — `task` and `chat` — because the spend place stands them in
    a column beside a project and a figure, where the article says nothing and
    `conversation` is twelve cells. `standing` is unchanged and no longer reaches
    that column at all. The long spelling stays in prose (home's `start a new
    conversation`)."
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
    it ([spendProjectField])."
  - "`dollars` did not mark thousands, so the spend place drew `128,400 calls`
    and `$4210.55` on one row — the count grouped and the money not. Money of a
    cent or more now wears the same mark a count does (`$4,210.55`), through one
    `groupDigits` in placeprose.go, and so do whole limits through `railFigure`
    (`$50,000`). Every surface that quotes a figure through `dollars` moves with
    it: the status line's bill, /cost, the Spending tab, home. Figures under a
    thousand are unchanged, and the sub-cent floor `<$0.0001` is untouched."
  - "`tokenWord`'s ladder stopped at the million, so a fortnight of agent work
    read `7062.1M tokens` and one model's row `3210M`. It has a `B` rung now —
    `842`, `12.4k`, `1.2M`, `3.2B` — turning over at 999,950,000 for the reason
    the rung below turns at 999,950. It is the status line's context meter and
    tok/s reading too, though nothing there comes near a billion."
  - "The models table gave up its bar and counts below a flat 80 cells. A narrow
    frame now gives up WHOLE FIELDS in a named order of worth, measured against
    what each table actually holds — the token volume, then the role, then the
    calls; the kind word, then the project. The money is never given up. A name
    column is never squeezed to keep a field behind it, because a column narrower
    than what it holds is a column the longest rows fall out of."
  - "The demo home's ledger was a fortnight of small change, so `make demo-home`
    could not show the spend page at the top of its range. It now carries a
    four-day heavy stretch (`demoHeavy` in cmd/aforge-demo-home/seed_spend.go)
    across three models within a few per cent of each other, which puts
    five-figure call counts and hundred-million token volumes into the columns —
    the arrangement where a table of figures has to be read digit by digit."
---

The two headings are two partitions of one ledger, and they had drifted into two
layouts — one with a bar in it, one without, each measuring its own fields its
own way. They share one measurer and one row painter now, which is what made the
third table cheap enough to add: a promise's facts are not a task's, so it gets
its own columns rather than a tag crammed into somebody else's.

Worth knowing about the bar, since it took three cuts to get right before it was
deleted: a fixed 30-cell column was exactly one cell short of `· claude-opus-4.1
· conversation`, and measuring name-and-role as one field still let the single
bound model's bar sit two cells right of every other. The lesson survives the
bar — a constant is a guess, and a field shared with something optional is a
column that moves.
