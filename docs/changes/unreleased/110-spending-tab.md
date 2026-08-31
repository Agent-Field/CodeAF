---
kind: changed
title: money has one editor — the Spending tab — and every door lands on it
pr: 110
surface: [chat, engine, docs]
invalidates:
  - "The settings panel had six tabs. It has nine: Session · Context · Workspace · Display · Spending · Safety · Tasks · Providers · Connections."
  - "The money rows were on the Workspace tab and the session ceiling was on Session. All four are on Spending now, labelled `per day`, `per conversation`, `per plan` and `practice`; the registry keys and every environment variable are unchanged."
  - "`config.CategorySpending` held twenty rows. It holds only dollar rows now; the approval gate and its clocks moved to `config.CategorySafety`, and the task rows and `workers` to `config.CategoryTasks`."
  - "A money row at zero read `$0` with `no limit` beside it in the receipt. It reads `no limit`, `never asks` or `practice off` as its VALUE — through `Setting.EmptyLabel` — and `$0` is rendered nowhere; the receipt now carries a live figure instead."
  - "A dollar row only accepted a number. It also accepts `none`, `no`, `off`, `unlimited`, `∞` and `no limit`, all landing the same zero."
  - "There was no `/budget` command. There is: bare it opens the Spending tab, `/budget 50` sets the day, `/budget none` removes it, `/budget plan 20` sets one row by name, and `/limits` is its alias."
  - "The refused-turn message was `session: the spend rail was reached: this session has spent $x of its $y rail — raise it to keep going`. It is `conversation limit reached · $x spent of $y · /budget changes it`, and `ErrSpendRail`'s own words no longer reach a person."
  - "First run asked one number, the daily ceiling. It asks three — per day, per plan, per conversation — on one rails screen that offers `none` in its header."
  - "The status line's money segment was telemetry. It is a door: pressing it opens the Spending tab, and the figure takes the warm ink at four fifths of this conversation's own limit."
  - "The spend place drew nothing about limits. Its first line is a dim pointer — `today $x of $y · /budget sets the limits` — whose `enter` opens the tab, and `→` there offers `b` on the verb strip. The place still edits nothing."
---

A person looking for "how much may it spend" read about load averages first: the
money rows sat on Workspace between `workers`, `guardian` and `memory floor`,
the conversation's own ceiling sat a tab away, and `0` on a limit rendered as
`$0` — the emptiness law broken exactly where it matters.

`docs/design/spending/DESIGN.md` is the design and the record of every place the
code bent it. Two of the seven rows on the tab are **readings** rather than
settings: a v3 task carries no dollar cap of its own (`docs/LIMITS.md`) and a
standing order's per-firing limit is written per item, so the tab says what
those limits are instead of growing knobs that write nowhere.
