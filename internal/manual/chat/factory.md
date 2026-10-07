# The factory

## /factory — the floor

The **factory** is a place, like home, activity and spend: the floor where work a chat splits
off, or that a connected repository sends, stands in rows grouped by where each item is. Open
it with `/factory`, `alt+9` (`opt+9` on a Mac), or the map (`alt+.`). On a narrow tab bar it
folds into `More`. `esc` goes back to the conversation.

Today nothing is connected, so the page draws one dim line:
`nothing connected yet · the factory floor arrives here when a chat splits work off or a repo is connected`.
The only key it offers then is `esc back`.

With something on the floor, the left column is the list of items and the right column is the
item under the cursor. On a terminal under 72 columns the list takes the whole width and the
right column is not drawn.

The keys on the floor, as the hint line names them:
`↑↓ walk · space mark · / filter · [ ] repo · A backlog · esc back`.

- `↑` and `↓` (or `ctrl+p` and `ctrl+n`, or the wheel) walk the items.
- `space` marks or unmarks the new item under the cursor (offered only on a new item).
- `/` opens a filter box at the top of the list.
- `[` and `]` show one repository at a time, then all of them again (offered with two or more).
- `A` shows the whole backlog of new items, or only the recent ones.
- `esc` clears a filter or a repository first; pressed again, it goes back to the conversation.

## what a row is

A row on the factory floor is one **item**: an issue, a pull request, a red CI run, a chore,
or work a chat split off. It is written as a mark, its short name and its title, cut with `…`
when it does not fit: `#1538 budget caps per task`. A red CI run is written `ci`.

The rows are grouped under muted headings that carry a count, in this order, and a group with
nothing in it is not drawn at all:

- `needs you · 1`: an item waiting on your answer, such as a plan to approve. Its mark is `?`.
- `streams · 2`: items running now (a half-filled circle) and items queued behind them (`○`).
- `new · 5`: items that arrived and have not started. They carry no mark; a red CI run wears `✕`.
- `landed · 1`: items that finished and wait for your sign-off (`✓`).
- `shipped · 1`: items you signed off (a dim dot).

At the right of each row is the one fact its group is about: how long a `needs you` item has
waited (`7h`), the stage a running item is in and what it has spent (`review · $1.42`), the
word `queued`, a new item's kind and size (`bug M`, or `pr`, or `ci`), how many of a landed
item's claims were shown and not (`3✓ 1✕`), and the time an item shipped (`06:00`). When the
list is narrower than 30 columns the fact is left off and the title gets the room.

The right column shows the item under the cursor: its short name and repository, its title, and
the factory's one-sentence read of it. The page re-reads the floor every three seconds while
you are on it, and the cursor stays on the same item when the floor changes under it. If a
read fails, the floor read before stays on screen and the line above the hint says
`the factory could not be read` with the reason.

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

The factory page does not yet show an item's stages or let you change them. That part of the
page has not been built; the right column says
`stages, the running stream and the proof sheet arrive in this pane next`.

## the handover

The **handover** is what happened on the factory floor since you last looked: how many items
shipped, how many arrived, how many asked you something, and what it cost. It is meant to be
the first thing the factory page shows after you have been away.

It is not drawn yet. The factory page today shows the floor's rows and the item under the
cursor, and nothing else.

## the proof sheet

When an item lands, its **proof sheet** lists each claim the work makes ("fires on first true,
never again", "survives a codeaf restart") beside whether it was shown and the evidence, in the
claim's own medium: a test, a screenshot, a transcript, a benchmark. A claim nothing showed is
listed as not shown rather than left out. The repository's policy lines (for example "no new
dependencies without asking") are checked the same way. You sign off on a landed item, or send
it back with words.

The proof sheet is not drawn on the factory page yet, and there is no sign-off or send-back key.

## what the factory does not do yet

Be plain about this when asked:

- **No GitHub, GitLab or Linear is connected.** There is no way to connect a repository yet, so
  no item arrives from one.
- **Nothing posts anywhere.** The factory never comments, labels, opens a pull request or opens
  an issue on any service.
- **There are no verbs.** The page cannot launch, stop, pause, answer, steer, sign off or send
  back an item. The keys walk, filter, pick a repository, show the backlog and mark new items;
  a mark launches nothing.
- **The page is empty on an ordinary launch** and draws the `nothing connected yet` line. When
  codeaf is started with the environment variable `CODEAF_FACTORY_FIXTURE=1` the page reads a
  still, made-up floor instead (three repositories, ten items, one in every group) so the page
  can be looked at. That floor never changes and none of its items are real.
- **Over `--host`** the factory page draws the same `nothing connected yet` line.
