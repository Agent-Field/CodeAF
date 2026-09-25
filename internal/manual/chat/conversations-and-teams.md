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
open. Under it is the **Teams** row, which ends in `✦ Organize` while every conversation is
shown, and at the bottom a toolbar with `Filter /`,
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
count was, opens the team's **card**: its name, which you edit as you type, its colour,
**`Inside: harbor ▾`** (which team it sits in; a press opens the Move into… picker), the
settings it overrides (each one saying where an inherited value comes from) and
**Close team…**. The **teams page** has the card whole.

**Teams inside teams.** A team can sit inside another. The Teams row stays one flat row and
names a team inside another with its parent first, `harbor › api`; the team switcher and the
teams page draw the tree. You move a team on the **teams page** (`m`, Move into…, or a drag in
its rail) or with `Inside` on its card, and every move can be undone for a few seconds.

**Closing a team.** `D` closes the team that is shown: at once, with Undo, when nothing in it
is running, and with a card offering **Wrap up first**, **Close now** and **Cancel** when
something is. A closed team leaves the Teams row and the switcher and waits under
`▸ Closed · N` on the teams page, where it can be reopened, and deleted once you are sure.
Closing or deleting a team never deletes a conversation.

## Organize: teams suggested for your conversations

While the view shows **All**, the Teams row ends in `✦ Organize`. Press it, or `o`, and a card
suggests teams for the conversations that are open. Nothing changes until you apply it:

```
╭─ Organize ────────────────────────────────────────────────╮
│  New teams                                                │
│  ☑ ● codeaf          5  from the folder                   │
│  ☑ ● nvda research   3  cpu profiling, nvda deep…, 10-K   │
│  Add to existing                                          │
│  ☑ ● harbor        + 2  relay audit, footprint table      │
│                                                           │
│  about $0.0020                    Cancel esc   Apply ↵    │
╰───────────────────────────────────────────────────────────╯
```

The suggestions come from two places:

- **Folders.** Conversations that share a project folder, two or more of them, are suggested
  as a team named after the folder (`from the folder`). If a team already has that name, the
  ones it is missing are suggested for it instead, and a folder whose conversations are
  already together in one team is left alone. This part is free and always the same.
- **The model you use for names**, asked once per press (the same cheap one that names
  conversations and teams), over the conversations' titles and folders and your teams, for
  groupings a folder cannot see and conversations that belong in a team you already have.
  `thinking…` shows while it works, and it is given ten seconds. Where the two disagree the
  folders win. The line at the bottom says about what the ask cost.

If the model cannot be asked or does not answer, the card shows the folder suggestions alone
and says `suggestions from folders only`. With nothing to suggest it says
`Everything is organized` beside a `Close`.

Every row starts ticked. `↑` `↓` move, `space` or a press ticks and unticks a row, `enter` or
**Apply** makes the ticked ones in one go, and `esc`, **Cancel** or a press off the card puts
it away with nothing changed. A new team gets the name and the colour the card showed; each
new team's colour is its own. A conversation can be suggested for several teams, as it can be
in several.

After an Apply the Teams row says what it did for a few seconds, like
`Organized · 2 new teams, 2 added   Undo`. **Undo**, or `u` while it is there, puts your teams
back exactly as they were.

When some teams have had no activity for a week and nothing waiting on them, the card also
offers **Close 3 quiet teams** under `Quiet for a week`, ticked like the rest; Apply closes
them and Undo reopens them. Apart from that Organize only ever adds: it never renames a team
and never takes a conversation out of one, so pressing it again is how you refresh the
suggestions. Nothing runs by itself. The button
counts the conversations in no team once there are five or more, `✦ Organize 7`, and after a
run that found nothing to suggest it reads `Organized ✓` until your conversations or teams
change; it can still be pressed.

## The team switcher on the tab strip

While a team is shown, the tab strip starts with a chip naming it, `● harbor ▾`. With teams
but none shown, it is a quiet `Teams ▾`; with no teams at all there is no chip. A press on the
chip opens the **team switcher** under it, on any page the strip is on, the conversations view
included:

```
╭─ Teams ────────────────────╮
│ ◉ ● harbor              2  │
│ ○   ● orbit             1  │
│ ○ ● dock                0  │
│ ○   All                 3  │
│     Closed · 2 ▸           │
│ ────────────────────────── │
│ − Remove this conversation │
│ + New team…                │
│   Team settings…           │
╰────────────────────────────╯
```

- a team, or **All**, narrows or widens the strip; the conversation in front stays in front
  unless it is not in the team, and then the team's first conversation comes forward. The
  teams are the tree: a team inside another stands indented under it
- **+ Add this conversation** puts the conversation in front into the team that is shown, and
  the row turns into **− Remove this conversation**
- **Closed · 2** is there while you have closed teams: a press opens the teams page with its
  Closed fold open. A closed team is never one of the switcher's teams
- **+ New team…** opens the conversations view with the new-team card, the conversation in
  front already picked
- **Team settings…** opens the shown team's card, over whatever page you are on

`↑` `↓` move, `enter` chooses, `esc` or a press anywhere off it puts it away.

## A team's manager

A team can have one **manager**, a conversation that runs the team for you: you talk to it, it
hands work to the members and tells you where things stand. While a team is shown, the first
place on the tab strip is the manager's, pinned at the left: a quiet `+ Manager` until there is
one and `◆ Manager` after; `◆ Make this harbor's manager` in the team switcher or in a tile's
Teams list makes an existing conversation the manager. While the manager is in front its
**Traffic** rail is on the right (`alt+l` shows or hides it), and `alt+m` goes to the manager. What a manager can do, and how members talk to each other, is on the
**team manager** page. The **teams page** (`/teams`, `alt+2`) lists every team as a tree
and puts the chosen team's manager conversation beside it, with what waits on you.

## Where teams are kept

Teams are saved in your profile, in `teams.json`, every time one changes, and never in
`config.json`. Over `--host` that is the far machine's profile, where the conversations run. A `spaces.json` from an earlier build is read once, its groups keep their
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
| `e` | The shown team's card: name, colour, settings, Close team… |
| `D` | Close the shown team; its conversations stay open |
| `o` | Organize: suggest teams for your conversations (while All is shown) |
| `u` | Undo the last Organize, while the Teams row offers it |
| `tab`, `shift+tab` | Next or previous team, then All |
| `1` to `9` | That team; its digit again goes back to All |
| `/` | Filter conversations by name; `esc` clears it |
| `-`, `+` or `=` | Fewer or more columns |
| `0` | Columns back to automatic |

In the new-team card: type the name, `ctrl+r` another name and colour, `←` `→` the colour,
`enter` create, `esc` cancel.

In the Organize card: `↑` `↓` move, `space` tick or untick, `enter` apply, `esc` cancel.
