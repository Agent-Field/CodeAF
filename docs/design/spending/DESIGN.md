# Spending — the rails a person sets, and every door that leads to them

*Date: 2026-08-31. Status: design → build. Lives at `docs/design/spending/DESIGN.md`.*

## The one sentence

Money has **one editor** (the settings registry's spending rows, shown on one
tab in one order) and **many doors** (the status line, the spend place, the
moment a rail trips, first run, `/budget`). No door is a second editor; every
door lands on the same row.

## What is wrong today

- The money rows live on the **Workspace** tab, between "workers", "ask before
  running", "guardian", "task repair rounds" and "memory floor" — twenty rows
  answering four different questions. A person looking for "how much may it
  spend" reads about load averages first.
- Rows are named like keys (`daily budget`, `session ceiling`, `practice
  budget`) and read like keys (`$20`). Nothing on the row says what is
  happening against it *now*.
- `0` means "no limit" on two rails and "zero dollars" nowhere, but the row
  shows `$0` either way — the emptiness law is broken exactly where it matters.
- First run asks the daily budget only; the other rails are discovered when
  they trip.
- The spend place answers "what did it cost" and, by its own law, never edits;
  but it also never *points* at the rail, so the person who just read the bill
  has nowhere to go.

## The information hierarchy

Three questions a person has about money, in the order they have them:

| question | where it is answered | kind |
|---|---|---|
| **What is it costing right now?** | status line money segment; phase clock during a turn | reading |
| **What did it cost?** | the spend place (by day, model, purpose) | reading |
| **What may it spend?** | settings → **Spending** tab | the editor |

Everything else that used to share the tab is a different question:

| question | new tab | rows |
|---|---|---|
| What may it run without asking? | **Safety** | ask before running, tool approvals, shell command rules, guardian, approval countdown, task countdown, who settles a task nobody could check |
| How does it run tasks? | **Tasks** | starting a task, task audit, task repair rounds, tasks at once, busy machine, memory floor, task model, workers |

Tab order: Session · Context · Workspace · Display · **Spending** · **Safety**
· **Tasks** · Providers · Connections. Spending before Providers because "may
it spend" is asked before "on which machine".

## The Spending tab

```
 today             $3.42 of $500 · resets at midnight
 per day           $500
 per conversation  no limit
 per plan          asks first above $100
 per task          $50 · 6h
 per standing run  $5
 practice          $50 of the day
```

Rules, each with the reason:

1. **The first row is a receipt, not a setting.** `today` is where the eye
   lands and it answers question one before the person edits anything. It is
   not selectable; `enter` on it moves to `per day`. It reads `$3.42 of $500`,
   or `$3.42 · no limit`, or nothing at all before the first call of the day
   (emptiness law).
2. **Rows are ordered by how often a person worries about them.** Day first
   (the bill), conversation (this window), plan (the ask), task, standing,
   practice (aforge's own slice). Not alphabetical, not by key.
3. **A row's label is the scope; its value is a sentence fragment that
   completes "it may spend…".** `per plan · asks first above $100` — because
   that rail does not stop, it asks. `per task · $50 · 6h` — two limits, one
   row, because a task has both and a person thinks of them together.
4. **No limit is a word, never a number.** Typing `none`, `no`, `off`, `∞`,
   `unlimited` or `0` all land the same value; the row reads **no limit**. `$0`
   never appears. This is the emptiness law applied to money.
5. **Every row carries its receipt** on the dim right side when there is one:
   `per conversation  no limit        this one $0.41` ·
   `per task  $50 · 6h        3 running · dearest $4.10` ·
   `per plan  asks first above $100        asked twice this week`. Unknown →
   nothing.
6. **The hint answers "and then what".** Every hint ends with what happens at
   the line: *"…when the day's calls reach it, new turns wait for midnight or
   for you to raise it here."* A rail whose consequence is unstated is a
   surprise, not a setting.
7. **Editing is the existing text box**, with the value pre-filled as the
   person would type it (`500`, `none`), and `$` optional. `esc` cancels,
   `enter` saves, and the receipt re-reads on the next beat.

## The doors

Each door opens the Spending tab **with the cursor on the row it came from**.
None of them edits in place except the money segment, which already does and
whose edit goes through the same registry row.

| door | where | what it says | lands on |
|---|---|---|---|
| **status line money segment** | always | `$3.42` (of the day) — press or `enter` when focused | `today` |
| **the rail trips** | the turn that was refused | `daily limit reached · $500 spent today · press b to change it` (one line, the key named) | the rail that tripped |
| **80% of a rail** | status segment | the `$` figure takes the warm ink; nothing else changes — a glance, not an alarm | — |
| **spend place** | top of the page | one dim reading `today $3.42 of $500 · b sets the rails` — a pointer, not an editor, so the place's own law holds | `today` |
| **`/budget`** | composer | bare → the tab on `per day`; `/budget 50` sets the day; `/budget none` removes it; `/budget task 20` sets a task | the row named |
| **first run** | before the welcome box | the rails screen (below) | — |
| **home machine band** | home | already names the rail a firing is against; its `enter` opens the row | the rail |

Key `b` (budget) is the one chord for "the rails" wherever a rail is named;
it does nothing on a line that names none.

## First run — the rails screen

After the key and the crew, one screen, same rows, same words, same registry:

```
 What may aforge spend?  enter keeps a default · type a number · none means no limit

 per day            $500
 per plan           asks first above $100
 per task           $50 · 6h

 The rest (per conversation, standing runs, practice) start with no limit or
 a small one; change any of them later with b.
```

Three rows, not seven: the three a new person can answer. Skipping keeps the
defaults. It shows once, ever, like the rest of first run. `none` is offered
in the header so "no limits" is a choice a person sees, not a trick they
learn.

## Narrow widths

Rows go through `rowfit`: the label is the identity and stays whole; the
value's spellings are `asks first above $100` → `asks > $100` → `$100`; the
receipt is the first to go. The `today` line degrades `$3.42 of $500 · resets
at midnight` → `$3.42 of $500` → `$3.42`. The money segment on the status line
degrades the same way and never shows the limit without the spend.

## Words

- "limit" in every person-facing line; never "rail", "ceiling", "budget cap",
  "consent" (those are the code's words). The tab is **Spending**; the rows
  say **limit** only in hints and trips (`daily limit reached`).
- "asks first above" for the plan rail — it is a question, not a stop.
- "no limit", never "unlimited", "off", "0", "∞" in what is displayed
  (all are accepted when typed).
- Numbers: whole dollars when whole (`$500`), two decimals under ten
  (`$4.10`), the day's spend always to the cent.

## Acceptance (tests the build must add)

1. The Spending tab shows exactly the seven rows in the order above and no
   other; Safety and Tasks hold the rows moved out; Workspace no longer names
   money (frame tests at 80 and 160 columns).
2. `today` is a receipt: not selectable, reads `$x of $y`, `$x · no limit`, or
   nothing before the first call.
3. Typing `none` / `0` / `∞` / `unlimited` on any money row lands "no limit"
   and the row reads **no limit**; `$0` appears nowhere in the tab.
4. Each row's receipt appears when known and is absent when not.
5. The money segment press opens the tab on `today`; `b` on a trip line opens
   the tab on the tripped rail; `/budget`, `/budget 50`, `/budget none`,
   `/budget task 20` do what the table says; the spend place's first line is a
   pointer and `enter` on it opens the tab.
6. A tripped rail's message names the limit, the figure and the key, in one
   line, and nothing else about it is said twice.
7. First run's rails screen writes through the registry rows, byte-identical
   to a settings edit; skipping writes nothing; `none` writes no limit.
8. At 60 columns every row keeps its label whole and its value readable;
   receipts are dropped first (rowfit tests).
9. Manual: a page section per row in the asker's words ("how do I remove the
   daily limit", "why did it stop and ask me about money", "what does per plan
   mean"), the three tabs named, `b` and `/budget` in the keys and commands
   tables. Gates green.
