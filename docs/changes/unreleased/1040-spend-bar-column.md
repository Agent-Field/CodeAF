---
kind: changed
title: the spend place is three tables in four columns each — no bars, and money in cents
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
  - "`what ran it` is name · calls · tokens · money, and the role it is bound to
    is part of the NAME FIELD rather than a column of its own. The role held a
    reserved column, floored at the widest `config.ModelSlots()` label, only so
    that one bound row's bar stayed in line; with no bar to protect, a column
    held open on every machine for a word at most one row wears is a column of
    air. `spendReading.modelCols` and `roleCol` are deleted."
  - "`what it was for` is name · project · kind · money."
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
    frame now drops WHOLE FIELDS FROM THE RIGHT, measured against what each table
    actually holds — the tokens before the calls, the kind word before the
    project. A name column is never squeezed to keep a field behind it, because a
    column narrower than what it holds is a column the longest rows fall out of,
    and the longest name is very often the row that also wears the role word."
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
