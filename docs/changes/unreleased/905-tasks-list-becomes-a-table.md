---
kind: changed
title: The tasks list becomes a table — two columns, a sort key you can see, and every fold shut
pr: 905
surface: [chat]
invalidates:
  - "A row of the tasks place was a name and a RANKED TAIL of up to six facts —
    `2 files · done · 5h · $0.27` — which gave up a fact at a time as the frame
    narrowed, so the figure under a person's eye moved sideways on every row and
    meant something different on each one. A row is now a name and TWO COLUMNS in
    the same cells on every row of one frame: `state` at twenty cells, and the
    column the list is SORTED BY at six, right-aligned. Under 90 cells the state
    column goes and the sorted column stays; under 60 the phone cards are
    unchanged. `tasksFacts`, `tasksMiddleField`, `tasksSpendEarnsCells` and
    `tasksPaintTail` are gone."
  - "The tasks list had one order — newest first — and no way to ask for another.
    It now sorts by age, name, state, files or cost, and THE SORT KEY IS THE
    COLUMN YOU SEE: files and cost draw their own figure, name and state show the
    age. The sort is applied INSIDE each level of the tree and the tree never
    flattens — sections keep their order, conversations order by their aggregate
    inside their section, and the work under one conversation orders among
    itself. A blank cell is a true answer and sinks to the bottom of its group
    whichever way the column points."
  - "spec.md §2 and #884 both said the sort key was `s`. It is `alt+s` (walk the
    keys) and `alt+shift+s` (turn the column round), by the ruling of 2026-09-11:
    every printable key on this place goes into the filter, so a bare `s` would
    cost a person `sweep`, `stop` and `site`. The chord is declared through the
    router's `alt+<letter>` class — the class a VIEW belongs to — because that
    class swallows an undeclared letter rather than passing it down. The two
    column labels on the control row are clickable and are the same door."
  - "Money was drawn on every row of the tasks list, last in the tail and first to
    be given up. It draws in ONE place now — the sort-key column, while the list
    is sorted by cost — so a list sorted by age carries no figure a person did
    not ask for, and the one they did ask for is a column they can read down."
  - "Every conversation on the tasks place opened SHOWING its work, and a family
    opened shut. Everything opens shut now (the owner's ruling, 2026-09-11): a
    hundred conversations with a hundred and ninety subtasks under them is a page
    nobody can scan. `→` opens one, a shut root's state cell is its own count in
    that section (`5 your call`, `9 done`), and a typed filter opens every
    conversation it matched — then gives a person their own folds back when it
    clears."
  - "A shut conversation's row said how many rows ONE press would reveal. It says
    how many pieces of work are under it ALTOGETHER, at every depth, because that
    is the claim a shut fold makes: `1 done` beside a heading reading `2 folded
    away` was two numbers about the same rows and the smaller one read as the
    size of the thing you were deciding whether to open."
  - "The filter's words were echoed on a dim `filter · port` note line UNDER the
    rows the keystrokes had just changed. They are on the CONTROL ROW at the top
    of the list now — `⌕ port`, with the dim `type to filter` standing in it while
    nothing is typed — with the two column labels at its right. The note line
    under the list keeps only `nothing matches`. The composer at the foot is still
    the same editor and still rests on `type to filter this list`; what moved is
    where its letters are drawn (`place.boxOnBody`)."
  - "The tasks foot named neither the sort nor the filter. It now ends `alt+s
    sort: <key> · type to filter`, and `esc clear the filter` takes the second
    slot while a filter is on — the ruling asks for both by name, because a chord
    nobody can find is a chord that does not exist."
  - "`tokens` has a `GFilter` slot: `⌕` plain, nf-fa-search in the nerd-font
    repertoire, `/` in ASCII. It shares its plain glyph with `GSearch`, argued in
    `TestOneGlyphOneMeaning`'s deliberate table — one lens means one thing, which
    is finding, and the two places it is drawn are both a box you type a query
    into."
  - "`checkerStalled`'s duration and the row's raw outcome landed on #895 and are
    unchanged here."
---

The list a person opens in the morning is a list they READ DOWN, and a ranked
tail cannot be read down: every row gave up a different fact, so the same cell
held a price on one line, an age on the next and nothing on the third. Two fixed
columns is the whole change, and everything else follows from it — the state has
somewhere to always be said, the sort has somewhere to show its answer, and the
reason, which was what made the right edge prose, moves to the pane beside the
list (#902) or to one dim line under the cursor's own row where there is no room
for a pane.

The columns are measured from the LIST'S WIDTH and the row's lead — its family
connectors and its mark — is spent out of the NAME. A root has no mark and a
worker three levels down has four cells of connectors, so a row that measured its
columns from what its own lead left would put them in a different place on every
line: a table whose columns move is a tail with extra steps.

Folded-by-default is the owner's ruling and it changes what the page IS: it opens
as an index of conversations, each saying how much is behind it and how urgent
the most urgent of it is, and a person opens the one they came for. A filter is
the exception, because a row that matched and is sitting behind a shut fold is a
row the query appears not to have found.
