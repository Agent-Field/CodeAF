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
handover (what happened while you were away) above the item under the cursor. `↑` and `↓` (or `ctrl+p` and `ctrl+n`, or the wheel) walk the items;
the hint line says `↑↓ walk · esc back`. On a terminal under 72 columns the list takes the
whole width and the right column is not drawn.

## what a row is

A row on the factory floor is one **item**: an issue, a pull request, a red CI run, a chore,
or work a chat split off. It is written as its short name and its title, `#1538 budget caps
per task`; a red CI run is written `ci`.

The rows are grouped under dim headings, in this order, and a group with nothing in it is not
drawn at all:

- `needs you`: an item waiting on your answer, such as a plan to approve.
- `streams`: items running now, and items queued behind them.
- `new`: items that arrived and have not started.
- `landed`: items that finished and wait for your sign-off.
- `shipped`: items you signed off.

The right column shows the item under the cursor: its short name and repository, its title, and
the factory's one-sentence read of it. The page re-reads the floor every three seconds while
you are on it, and the cursor stays on the same item when the floor changes under it. If a
read fails, the floor read before stays on screen and the line above the hint says
`the factory could not be read` with the reason.

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
  back an item. Only `↑`, `↓` and `esc` work.
- **The page is empty on an ordinary launch** and draws the `nothing connected yet` line. When
  codeaf is started with the environment variable `CODEAF_FACTORY_FIXTURE=1` the page reads a
  still, made-up floor instead (three repositories, ten items, one in every group) so the page
  can be looked at. That floor never changes and none of its items are real.
- **Over `--host`** the factory page draws the same `nothing connected yet` line.
