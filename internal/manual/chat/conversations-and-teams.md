# Conversations and teams

## The conversations view: every open conversation at once

The **conversations view** shows every conversation this window has open as a grid of
live tiles, so you can see at a glance which ones are working, which are waiting on you
and which are at rest, and go to any of them with one press. It is also where you group
conversations into **teams**.

Open it any of these ways:

- `alt+v` from anywhere in a conversation (on a Mac keyboard that is not sending alt,
  `option+v` types `√`, and that works too)
- `/wall`
- the `▦` at the right end of the row under the message box, drawn once a second
  conversation is open
- `▦ All` on the tab strip, after the new-chat `+` (just `▦` on a narrow window)

Close it with `alt+v` again, `esc`, the `‹ Back` button at the bottom left, or a press on
the strip's `▦ All`. Nothing you do in the view ends any work: closing a tile closes its
view in this window, and the conversation keeps running.

The title bar says what is shown, `every open conversation, live` or `the conversations in
harbor` while a team is shown, and counts what is running, what needs you and how many are
open. Under it is the **Teams** row, and at the bottom a toolbar with `Filter /`,
`New team s`, `Columns − +` and `Help ?`. While the pointer rests on any control, the middle
of the toolbar says in one dim line what it does and which key does the same.

## Reading a tile: title, what it is doing, and the newest lines

A tile reads top to bottom:

- **the title** on the top border, the brightest thing in the tile, after the dots of the
  teams it is in (up to three, then `+N`)
- **one dim line** saying what the conversation is doing now, like `running bash · 2m`,
  `writing` or `? waiting on you · 3m`, or when it last moved, like `updated 5m ago`
- **the conversation's own newest lines**, drawn the way the conversation draws them, fading
  with age so the newest are where your eye lands; lines that just arrived are lifted for a
  moment and then settle
- a small **activity line** on the bottom border while there is activity to show

A conversation this window can only show as a snapshot (over a shared connection only the one
in front is live) says `seen 6m ago` and draws no spinner.

**A tile that needs you** has the amber border, the only amber in the view, and its body ends
in the question and an `Answer ↵` button. `n` jumps to the next one waiting on you, and the
title bar's `needs you` count does the same when pressed.

## Pointing, focusing and opening a tile

A press on a tile opens its conversation, the way a thumbnail opens its window. The tile
grows into the frame for a moment while the conversation is already live under it, so a key
typed at once lands in its box.

The pointer resting on a tile lights it and turns its bottom border into its **action row**:

`Open ↵ ── Select ␣ ── Teams m ── Close x`

`Open` becomes `Answer` on a tile waiting on you. The keyboard has its own **focus**, drawn as
a heavy border; the arrows (or `h j k l`) move it, and the pointer never does. The focused tile
shows its action row too.

## Selecting several conversations

`space` (or `Select` on a tile) picks the focused conversation. Once one is picked, the view is
in **selection mode**: every tile shows its box, `☐` or `☑`, and a press anywhere on a tile
picks it or puts it back instead of opening it.

While anything is picked, a tray rises over the bottom of the grid:
`2 selected   Make team s   Add to… ▾   Close views   Clear esc`.

- **Make team** starts a new team from the picked conversations
- **Add to…** opens the teams list for all of them
- **Close views** closes their views in this window; the work keeps running, and one with
  work in flight is asked about first
- **Clear** (or `esc`) unpicks them all

## Teams: named groups of conversations

A **team** is a group of conversations you name, like `harbor` for everything about one
project. A conversation can be in any number of teams: a team is a grouping, not a place a
conversation lives. Showing a team narrows both the conversations view and the **tab strip**
to its members, and nothing else changes: no conversation is opened, closed or stopped.

**Making a team.** Press `s` (or `+ New team` on the Teams row, `Make team` in the tray, or
`+ New team…` in a tile's teams list). With nothing picked, the team starts with the focused
conversation. A card opens with a name and a colour already chosen:

- if the conversations all sit in **one project folder**, the name is that folder's name
- otherwise a pleasant word is there at once, and codeaf asks the model you use for names
  (the same cheap one that names conversations) once, over the conversations' titles, for a
  one to three word name; `naming…` shows beside the field while it asks. What comes back
  replaces the word only if you have not started typing, and if it fails or takes more than five
  seconds the word stays

Type to replace the name, `ctrl+r` for another word and colour, `←` `→` to pick among the
colours offered, `enter` to create it or `esc` to put the card away. After that the name
changes only when you change it, in the team's settings.

**Colours.** Each team gets a generated colour, as far from the others' as it can be, and
never the colours that already mean something here: the amber of a question, the colour of
running work, the red of a failure and the cursor's accent. A team's colour is drawn as its
dot, on the tiles it holds, on the rule under the tab strip while it is shown, and on the
view's `▦` door. On a terminal without enough colours, the dot is the team's first letter.

**Putting a conversation in and out of teams.** `m`, or `Teams` on a tile, opens a list of
every team with a box: `☑` in it, `☐` not, `▣` when some of the picked conversations are and
some are not. A box pressed is saved at once, and so is `+ New team…` at the foot.

**Showing a team.** Press its segment on the Teams row, `tab` and `shift+tab` to step through
the teams, or `1` to `9` for a team by its place (the digit of the team already shown goes
back to All). Each team keeps its own focus and scroll while the view is up, so looking into
one team and back to All returns you to where you were. The tab you are on never vanishes from
the strip: if it is not in the team, it stays at the end.

**A conversation started while a team is shown joins it.** `/new`, the strip's `+` and the
start page, `ctrl+t`, and a folder typed on home all start a new conversation, and it goes into
the team that is shown. Going back to a conversation that already exists changes no team.

**Team settings.** `e`, the dot on a team's segment, or the `⋯` the pointer brings up where its
count was, opens the team's settings: its name, which you edit as you type, and its colour.
**Delete team** asks first, and deleting a team never closes or changes a conversation; only
the group's name goes.

## The team switcher on the tab strip

While a team is shown, the tab strip starts with a chip naming it, `● harbor ▾`. With teams
but none shown, it is a quiet `Teams ▾`; with no teams at all there is no chip. A press on the
chip opens the **team switcher** under it, on any page the strip is on, the conversations view
included:

```
╭─ Teams ────────────────────╮
│ ◉ ● harbor              2  │
│ ○ ● orbit               1  │
│ ○   All                 3  │
│ ────────────────────────── │
│ − Remove this conversation │
│ + New team…                │
│   Team settings…           │
╰────────────────────────────╯
```

- a team, or **All**, narrows or widens the strip; the conversation in front stays in front
  unless it is not in the team, and then the team's first conversation comes forward
- **+ Add this conversation** puts the conversation in front into the team that is shown, and
  the row turns into **− Remove this conversation**
- **+ New team…** opens the conversations view with the new-team card, the conversation in
  front already picked
- **Team settings…** opens the shown team's settings

`↑` `↓` move, `enter` chooses, `esc` or a press anywhere off it puts it away.

## Where teams are kept

Teams are saved in your profile, in `teams.json`, every time one changes, and never in
`config.json`. A `spaces.json` from an earlier build is read once, its groups keep their
names, members and colours, and it is renamed to `spaces.json.migrated`. A teams file that
cannot be read is moved aside as `teams.json.unreadable-<number>` rather than written over,
so nothing you made is lost.

## Every key in the conversations view

`?` (or `Help ?`) opens a sheet of all of these, and every row on it is a button that does
what its key does.

| Key | What it does |
|---|---|
| `alt+v` | Open or close the conversations view |
| `esc`, `q` | Back one step: the list or card that is up, then the selection, then the filter, then the view |
| `?` | The help sheet |
| arrows, `h j k l` | Move the focus |
| `g`, `G` (`home`, `end`) | First and last conversation |
| `pgup`, `pgdown` | A screen of rows; the wheel moves one row |
| `n` | Next conversation waiting on you |
| `enter` | Open the focused conversation |
| `space` | Pick the focused conversation, or put it back |
| `x` | Close the focused view, or the picked ones; the work keeps running |
| `m` | The teams list for the focused conversation, or the picked ones |
| `s` | New team of the picked conversations, or the focused one |
| `e` | The shown team's settings |
| `D` | Delete the shown team; its conversations stay open |
| `tab`, `shift+tab` | Next or previous team, then All |
| `1` to `9` | That team; its digit again goes back to All |
| `/` | Filter conversations by name; `esc` clears it |
| `-`, `+` or `=` | Fewer or more columns |
| `0` | Columns back to automatic |

In the new-team card: type the name, `ctrl+r` another name and colour, `←` `→` the colour,
`enter` create, `esc` cancel.
