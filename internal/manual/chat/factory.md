# The factory

## /factory — the floor

The **factory** is a place, like home, activity and spend: the floor where work a chat splits
off, or that a connected repository sends, stands in rows grouped by where each item is. Open
it with `/factory`, `alt+9` (`opt+9` on a Mac), the map (`alt+.`), or the `Factory` button on the
tab bar, third after `Home` and `Chats`. `esc` goes back to the conversation.

Today nothing is connected, so the page draws one dim line:
`nothing connected yet · the factory floor arrives here when a chat splits work off or a repo is connected`.
The only key it offers then is `esc back`.

The floor is one layout in three widths:

- **120 columns and wider**: the handover runs across the whole width at the top, then a blank
  row, then two columns: the rows on the left (58% of the width, never under 70 columns), a dim
  `│`, and the **peek** on the right, which shows the item under the cursor.
- **90 to 119 columns**: the handover on top, then the rows across the full width, with no peek.
  Press `enter` on a row to see the item whole.
- **Under 90 columns**: no handover; each row is only its mark, its short name and its title.

The rows window follows the cursor when they do not all fit.

## the keys on the factory floor

The keys on the factory floor, as the hint line names them:
`↑↓ walk · space mark · / filter · [ ] repo · A backlog · esc back`.

- `↑` and `↓` (or `ctrl+p` and `ctrl+n`, or the wheel) walk the items.
- `enter` opens the item under the cursor on its own page (see the item page).
- `z` switches between compact rows (one line each, the default) and comfortable rows (a dim
  second line under each row with the factory's one-sentence read of it, and a blank row between
  items). The choice is not saved; every launch starts compact.
- `space` marks or unmarks the new item under the cursor (offered only on a new item).
- `/` opens a filter box at the top of the list.
- `[` and `]` show one repository at a time, then all of them again (offered with two or more).
- `A` shows the whole backlog of new items, or only the recent ones.
- `esc` clears a filter or a repository first; pressed again, it goes back to the conversation.

## what a row is

A row on the factory floor is one **item**: an issue, a pull request, a red CI run, a chore,
or work a chat split off. Rows are laid out in fixed columns so they line up down the page:
a mark, the short name right-aligned (`#1538`, or `ci` for a red CI run), the title (cut with
`…`), the repository's short name (`codeaf`), the facts, and at the far right how long ago the
item last moved (`12m`, `7h`, `3d`). Under 90 columns a row is only the mark, the short name and
the title.

The rows are grouped under muted capital headings with a count and a dim line out to the edge
(`NEEDS YOU · 1 ───`), in this order, with one blank row between groups. A group with nothing in
it is not drawn at all:

- `NEEDS YOU`: waiting on your answer. Its mark is `?`; its first fact is the question itself,
  in the question colour, and `[y/n]` stands just before the age.
- `STREAMS`: running items (a half-filled circle) and queued ones (`○`). A running row's first
  fact is a strip with one mark per stage (`●` done, `◐` running, `○` to come) and the stage it is
  in with how long it has run: `review 26m`. A queued row says `queued`.
- `NEW`: arrived and not started; no mark, except `✕` on a red CI run. The first fact is its kind
  and size: `bug · S`, `pr · ci ✓`, or `ci red`.
- `LANDED`: finished, waiting for your sign-off (`✓`). The first fact counts its claims: `3✓ 1✕`.
- `SHIPPED`: signed off (a dim dot), with `shipped 06:00`.

After the first fact come, in this order: the money (`$1.42/$5`, spent against the cap, or `~$3`
estimated), the author (`olu (stranger)` when the author is a stranger), and tags: `factory`,
`thin` (too underspecified to run without questions), `dup #950?`, `from chat ▸` and
`terminal only`. A row carries at most four facts and never wraps: as the window narrows, facts
are dropped whole from the right. The row under the cursor sits on a lifted background.

## filter the factory floor, search for an item

`/` on the factory page opens a one-line filter box at the top of the list, and the list narrows
as you type. Every word you type has to hold. A plain word matches anywhere in an item's title,
repository, area, kind or author. These words mean something more:

- `risky`: high risk, or a large item. `cheap`: estimated at $2.50 or less.
- `strangers`: written by someone outside the project. `mine`: written by you.
- `spend`: about spend, billing, the ledger or caps. `ui`: in the tui, pages or render areas.
- `prs`: pull requests. `bugs`: bugs. `thin`: too underspecified to run without questions.

`enter` keeps the words and closes the box; `backspace` takes a letter back; `esc` throws the
words away. With the box closed, the words stay under the repository line, and `esc` clears
them before it leaves the page. When nothing matches, the list says
`no item on the floor matches`.

## show one repository on the factory floor

The first line of the list says which repositories it shows: `all repos`, or one repository's
short name and how many items it has, such as `codeaf · 7`. `]` steps to the next repository and
`[` to the one before, and past the last one the list shows all of them again. An item shows
under a repository it arrived on or touches. `esc` goes back to all repositories before it
leaves the page.

## older new items, the backlog, and A

The `new` group shows only what arrived in the last three days. Older open items are kept back,
and a dim line at the end of the group says how many, such as `12 older open items behind A`.
`A` shows the whole backlog under `new`; `A` again hides the older ones. Dismissed items are
never drawn.

## mark new items

`space` on a new item marks it, and its mark turns to an accent dot; `space` again unmarks it.
Only new items can be marked. The marks are kept through every re-read
and when you leave the page and come back, until codeaf exits. A mark does nothing yet: there is no key that launches the marked items.

## stages, not a pipeline

An item runs through **stages** in a fixed order that a repository or team writes once: for
example plan, write, test, review, neaten, proof. Each stage is one sentence ("read it as a
stranger would"), and it runs as an ordinary task with the ordinary crew picked for that kind of
work. There is no graph to draw and no model to choose per stage; the one word a stage may carry
is its effort, cheap or strong.

A stage can repeat until a condition holds (until green, until clean) up to a number of rounds,
and then it stops and asks you. Nothing inside a stage can add a stage. A stage can also be a
gate: `plan` comes back with the plan before any code, and `ship` waits for your sign-off.

The peek shows a new item's stages as one line of names and a running item's as a strip of
marks; the item page lists them down its stage rail. A stage can carry a condition (`thin`,
`large`, `touches auth`, `has ui`); on an item it does not fit, the stage is drawn dim, with
`· skipped` on the stage rail and `skipped · not thin` (or the condition it missed) beside it.
You cannot change an item's stages from the page yet.

## the handover

The **handover** is what happened on the factory floor since you last looked. It is four rows
and a blank row across the whole width at the top of the floor, above the rows and the peek. It
is drawn at 90 columns and wider, and only when the window is tall enough to leave the rows at
least six lines under it.

1. A muted heading with a line out to the edge: `◆ handover · since 23:12 · 7h 12m · $8.44`,
   when the stretch began, how long ago, and what it spent. The spend is left off when nothing
   was spent.
2. What happened: `✓ 2 shipped #1661 #1663 · 3 arrived · 1 question handled`. A part whose
   count is zero is left off. When nothing at all happened the row says
   `quiet · nothing happened while you were away`.
3. What waits on you: `? 2 waiting on you`, in the question colour, or
   `nothing waits on you`. At the right, labelled `24h`, a sparkline of the last
   twenty-four hours of activity, one cell an hour, the current hour last. It is not drawn
   when no hour had anything in it.
4. The floor in one dim line: `3 repos · github · chat · benches 2/6 · polled 4m ago`, how many
   repositories, which sources are connected, how many benches are busy out of how many, and
   when a source was last read. At the right, the day's money: `$11.31 / $60 today`, what was
   spent against the day's limit. With nothing spent today it draws only `/ $60 today`. The
   made-up moving floor adds how fast its clock runs, such as `· 150×`; a real floor never does.

A narrow window drops the shipped items' names first, then the last facts on a row; the money
keeps its place.

## the peek

At 120 columns and wider, the right column of the factory floor shows the item under the cursor.
It has the same shape for every item: five fixed rows, a rule, what that item's state has to say,
and one dim line of keys on the bottom row.

1. Its short name and title, and on the right the repository, the kind, the author and how far
   they are trusted (`priya (collaborator)`), how long ago it arrived, and where it came from:
   `github`, `from a chat`, `terminal only`, or `github` with a check when it is on GitHub too.
2. The factory's one-sentence read of it.
3. The chips: `gate ship · cap $5 · effort —`. The dash means no effort word, so the crew picks the
   effort it would for that kind of work.
4. For an item on a bench, its stages as a strip: `● plan  ◐ review 1/2 · 4m left  ○ proof`, with
   rounds (`1/2`), how many tasks a stage split into (`×3`) and the most rounds a stage to come may
   take (`×2`). For a new item, the names of the stages it would run, with a stage that is
   switched off or does not fit the item drawn dim.
5. A rule.

Under the rule: the question and `[y] yes · [n] no · [a] in words` when it needs you; the newest
log lines of a running item, with its activity as a sparkline and its spend on the first one;
`queued · benches full · a bench frees it`; a landed item's claims, then the repository's policy
lines; two lines of a new item's description, the questions it would ask the author when it is
thin, and a note when the author is a stranger; or `merged 06:00 · $1.90` for a shipped one.

The bottom line names the keys for that item: `enter open · space mark · L launch · p plan first ·
d hide` for a new item, `y n answer · a in words · x stop` when it needs you,
`enter open · s steer · x stop` while it runs, and
`enter open · a ship anyway · c send back · o check again` once it has landed.

## the item page

`enter` on a row of the factory floor opens that item on its own page, across the full width;
`esc` closes it and puts the cursor back on the same row. Nothing about the floor (filter,
repository, marks) changes while it is open.

The top two rows are the item: its short name, title, repository, author, where it stands and for
how long (`running 26m`), with spend over the cap on the right (`$1.42 / $5`); then the chips with
their keys, `gate ship [t] · cap $5 [c] · effort — [e] · places: codeaf`.

Below them, on the left, is the **stage rail**: one row per stage, `●` done (with `×3` when it split
into tasks), `◐` running (with its round, `review 1/2`), `○` to come, a stage switched off dim, and a
stage whose condition does not fit dim with `· skipped`. It opens on the running stage of a running
item, the stage waiting on you for an item that needs you, `proof` for a landed item, and the
first stage otherwise. `↑` and `↓` (or `←` and `→`) walk it. Under 72 columns the stages are one
line above the stage instead of a column beside it.

On the right is the stage under the cursor: its settings
(`review · chat · until clean · max 2 · fanout per-finding · when always`), what it is asked to do,
a rule, then what it has to say: `runs after test` for a stage still to come, the log for the
running one, the question for one waiting on you, its result once done, and the claims on `proof`.
The bottom line names its keys, such as `enter open @review · s steer · w edit the ask · esc floor`.

`enter` on a stage does not open it yet; it says
`the stage's conversation opens here once streams are conversations`.

## what the factory does not do yet

Be plain about this when asked:

- **No GitHub, GitLab or Linear is connected.** There is no way to connect a repository yet, so
  no item arrives from one.
- **Nothing posts anywhere.** The factory never comments, labels, opens a pull request or opens
  an issue on any service.
- **There are no verbs.** The page cannot launch, stop, pause, answer, steer, sign off or send
  back an item. The keys walk, filter, pick a repository, show the backlog, mark new items,
  switch the row density (`z`) and open and close the item page (`enter`, `esc`); a mark
  launches nothing. The peek and the item page name the keys an item will take (`L`, `p`, `d`,
  `y`, `n`, `a`, `x`, `s`, `c`, `o`, `w`, and the chips' `[t]`, `[c]`, `[e]`) so the layout is
  settled, but none of them does anything yet.
- **The page is empty on an ordinary launch** and draws the `nothing connected yet` line. When
  codeaf is started with the environment variable `CODEAF_FACTORY_FIXTURE=1` the page reads a
  still, made-up floor instead (three repositories, ten items, one in every group) so the page
  can be looked at. That floor never changes and none of its items are real.
- **Over `--host`** the factory page draws the same `nothing connected yet` line.

## the Factory button on the tab bar

The tab bar at the top of every page shows `Factory` third, after `Home` and `Chats`. When items
on the factory floor wait on your answer it carries their count in the question colour:
`Factory ? 5`. With nothing waiting it carries no number at all. The count is what the floor said
the last time it was read, and the floor is read while the factory page is open, so it appears
after you have opened `/factory` once. On a narrow bar the places after it fold into `More ▾`
first; a `Factory` with a count stays on the bar. `alt+9` still opens it, whatever its place on
the bar.
