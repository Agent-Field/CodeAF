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
handover (what happened while you were away) above the item under the cursor. On a terminal
under 72 columns the list takes the whole width and the right column is not drawn.

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

The right column shows the item under the cursor, drawn by where it stands: a new item as its
**card**, a running, queued or waiting item as its **stream**, a landed item as its **proof
sheet**, and a shipped item as one line saying when it merged. The page re-reads the floor every three seconds while
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

A new item's card lists the stages it would run, numbered, and the stream of a running item
shows them as a strip of phases. A stage can carry a condition (`thin`, `large`,
`touches auth`, `has ui`); on an item it does not fit, the stage is drawn dim with
`· skipped: not thin` (or the condition it missed) after it. You cannot change an item's stages
from the page yet.

## the handover

The **handover** is what happened on the factory floor since you last looked. It is the top of
the right column, four rows and a blank row above the item under the cursor, and it is drawn
only when the right column is (72 columns and wider).

1. A muted heading with a line out to the edge: `◆ handover · since 23:12 · 7h 12m · $8.44`,
   when the stretch began, how long ago, and what it spent. The spend is left off when nothing
   was spent.
2. What happened: `✓ 2 shipped #1661 #1663 · 3 arrived · 1 question handled`. A part whose
   count is zero is left off. When nothing at all happened the row says
   `quiet · nothing happened while you were away`.
3. What waits on you: `? 2 waiting on you`, in the question colour, or
   `nothing waits on you`. At the right, labelled `24h`, a sparkline of the last
   twenty-four hours of activity, one cell an hour, the current hour last. It is not drawn on
   a right column under 60 columns, or when no hour had anything in it.
4. The floor in one dim line: `3 repos · github · chat · benches 2/6 · polled 4m ago`, how many
   repositories, which sources are connected, how many benches are busy out of how many, and
   when a source was last read. At the right, the day's money: `$11.31 / $60 today`, what was
   spent against the day's limit. With nothing spent today it draws only `/ $60 today`. The
   made-up moving floor adds how fast its clock runs, such as `· 150×`; a real floor never does.

A narrow column drops the shipped items' names first, then the last facts on a row; the money
keeps its place.

## the card

A new item on the factory floor is drawn as a **card** in the right column. From the top:

- Its short name and title, and on the right the repository, the kind, the author and how far
  they are trusted (`priya (collaborator)`), how long ago it arrived, and where it came from:
  `github`, `from a chat`, `terminal only`, or `github` with a check when it is on GitHub too.
- Up to two lines of its body, with `…` when there is more.
- The factory's read: one line of facts (`bug · M · tui · ready 72% · ~$3 · risk low`, or for a
  pull request `pr · +218 −44 · 6 files · ci` and its checks), then one sentence of what it makes of
  the item. A thin item adds `thin · it would ask` the author its questions, and
  `nothing posts without you`. An item from a stranger says their words may be drafted on, never
  shipped on, without you.
- The chips: `gate ship [t]   cap $5 [c]   effort — [e]`. The dash means no effort word, so the
  crew picks the effort it would for that kind of work.
- The stages, numbered: `●` for a stage that is on, `○` dim for one switched off, and a skipped
  stage dim with its reason. `×2` is the rounds a stage may take, `until clean` its condition.
- `must show:` and the repository's policy lines.
- `[enter] go` and what the gate means: `runs to a PR · you sign off`, `comes back with the plan
  first`, or `self-ships when the proof is green`. Under it, dim: `[s] add a stage in words ·
  [b] bank · [w] chips in words · [g] also on github · [d] hide`.

None of those keys work yet; see what the factory does not do yet.

## the stream

A running, queued or waiting item is drawn as its **stream**. The first line is a mark (working,
`?` when it waits on you, `○` queued, paused), the item, and on the right the repository, the
bench it is on, how long it has run, and what it has spent over its cap (`$1.42 / $5`).

Under it is the **phase strip**: each stage as a mark and its name, with its round (`1/2`) and how
many tasks it split into (`×3`). The running stage says its minutes left (`review 1/2 · 4m left`);
done stages are muted, pending ones dim, a stage waiting on you in amber, a failed one in red.
Then the recipe in one line (`plan · write · test · review · neaten · proof`), a rule, and the
newest lines of its log that fit, each with its time and a mark for what kind of line it is.

When the item **needs you**, the question is drawn under the log with a `?`, and
`[y] yes · [n] no · [a] answer in words · it waits; the other benches do not`. Those keys do not
answer yet. A **queued** item says `queued · benches full · a bench frees it`.

## the proof sheet

When an item lands, the right column draws its **proof sheet**: `sign-off ·` the item, and
`its claims` (or for a pull request `their claims, from the PR body`). Each claim is one row,
`✓` when it was shown and `✕` when it was not, with the evidence on the right in the claim's own
medium (`test · 0.3s`, a screenshot with `[▦ view]`). A claim nothing showed is listed with
` — not shown` after it, never left out. The repository's policy follows as
`policy · every PR on codeaf must show`, the same way. Then how long it ran, what it cost, and
`diff +218 −44 — the appendix` with `[d] open diff`.

When every row was shown, the last line is `[enter] ship`, then `[c] send back · [o] check
again · [s] the stream`. When any row was not shown, **send back is the default key**:
`[enter] send back — "prove survives a codeaf restart"`, with `[a] ship anyway · [o] check again`
and the line `a failed claim makes the blocking action the default key`.

None of these keys sign off or send back yet. A shipped item shows when it merged, what it cost,
and `[enter] the room`, which does not open anything yet either.

## what the factory does not do yet

Be plain about this when asked:

- **No GitHub, GitLab or Linear is connected.** There is no way to connect a repository yet, so
  no item arrives from one.
- **Nothing posts anywhere.** The factory never comments, labels, opens a pull request or opens
  an issue on any service.
- **There are no verbs.** The page cannot launch, stop, pause, answer, steer, sign off or send
  back an item. The keys walk, filter, pick a repository, show the backlog and mark new items;
  a mark launches nothing. The card, the stream and the proof sheet draw their keys
  (`[enter] go`, `[t]`, `[c]`, `[e]`, `[s]`, `[b]`, `[w]`, `[g]`, `[d]`, `[y]`, `[n]`, `[a]`,
  `[o]`, `[enter] ship`, `[enter] send back`) so the layout is settled, but none of them does
  anything yet.
- **The page is empty on an ordinary launch** and draws the `nothing connected yet` line. When
  codeaf is started with the environment variable `CODEAF_FACTORY_FIXTURE=1` the page reads a
  still, made-up floor instead (three repositories, ten items, one in every group) so the page
  can be looked at. That floor never changes and none of its items are real.
- **Over `--host`** the factory page draws the same `nothing connected yet` line.
