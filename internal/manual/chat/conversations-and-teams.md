# Conversations and teams

## All, team overlays, membership badges and stable tabs

Ordinary Home, saved-session and Chats-menu navigation opens the original conversation
with **None** selected in the Teams dropdown. None disables the team's visual overlay;
it does not end membership, change reporting authority, or stop background work.
The composer has no team-recipient hint and the sidebar uses the ordinary task view.

Team badges below the strip name this conversation's active memberships. Clicking a badge
selects its overlay while keeping the conversation in front. Choosing a team in the dropdown
restores that team's last conversation. Following a member or interaction from Teams selects
its originating overlay, including for conversations shared by several teams.

An overlay shows every member as a tab, manager first, using aliases. Saved members have tabs
before this window attaches their conversations; selecting one opens it and reports unavailable
or locked sessions honestly. Team tabs have no close action: `ctrl+w` keeps them visible and
points to removal in Teams. With None selected, ordinary tab close never removes membership.

Each overlay remembers selection and strip browsing separately. Drafts and reading positions
belong to the conversation and are shared across overlays. New members do not steal focus.
Removing the selected membership selects the nearest remaining tab; if none remain, the view
returns to None. Narrow strips keep their scroll arrows and a fixed `▦ All` grid button.

## The conversations view (the wall): how do I see all my conversations at once?

The **conversations view** shows every conversation this window has open as a grid of live
tiles. It is independent of teams: choosing an overlay in Chats does not filter this grid.
Saved conversations not open here remain accessible through Home and Sessions.

Open the grid with:

- `alt+v` (or `√`, composed by `option+v` on some Mac keyboards)
- `/wall`
- `▦ All` at the permanently right-aligned end of the Chats tab strip, including when tabs overflow
- `▦ All` on the dock under the message box, drawn once a second conversation is open

The strip belongs to Chats; from a place use `alt+v` or `/wall`. On strips narrower than the
12-column header floor it is absent. The title reads `Conversations · open in this window`.
Membership dots on tiles are context; they do not filter the grid or edit teams.
The toolbar offers `Filter /`, `Columns − +`, `Help ?` and `Back`.

Cancel with `alt+v`, `esc`, `Back`, or the strip's `▦ All`. Cancelling restores the original
team overlay, conversation and draft. Opening any tile enters ordinary Chats with no overlay,
even when it is the current conversation. You can select an overlay afterward with its badge
or the Teams dropdown. Closing a tile only removes its view; work keeps running.

Team creation, membership, manager assignment, settings and organization live on Teams.
The grid has no team filter or management controls. Its hover hint reads
`The grid of your open conversations · alt+v`.

## The tabs dock under the message box

Once a second conversation is open, the row under the message box ends in `▦ All`, the
same door the tab strip has, then one square per open conversation, in the same order as
the tab strip. The square in front is `▣`. The others are `■`, coloured the way the strip colours
a tab: the live colour while it is running, amber while it is waiting on you, and dim
while it is idle. Amber is only for waiting on you. One open conversation draws no dock.

Rest the pointer on a square and that square takes the hover ground. The hint line says
`Go to Shipping the parser · running · click`, with `waiting on you` or `idle` in the
middle. On the square in front it says `Shipping the parser · you are here`. On `▦ All`
it says `The grid of your open conversations · alt+v`, as the strip's `▦ All` does,
and the hover ground covers the glyph and the word together: they are one button.

A press on a square goes to that conversation and does not open this view. A press
anywhere on `▦ All` opens the conversations view, the same as `alt+v`. A press on the
square in front does nothing. On a narrow row the word `All` is the first thing to
go, leaving `▦`. The squares stay, then fewer of them with a `+N`, then the dock is not drawn.

## Reading a tile: title, what it is doing, and the newest lines

A tile reads top to bottom:

- **the title** on the top border, the brightest thing in the tile, after the dots of the
  teams it is in (up to three, then `+N`)
- **one dim line** saying what the conversation is doing now, like `running bash · 2m`,
  `writing` or `? waiting on you · 3m`, or when it last moved, like `updated 5m ago`, and at
  its right end **what the conversation has spent**, like `$0.42`: the same figure its own
  status line shows, the work it started included. A conversation that has spent nothing
  shows no figure, and on a narrow tile the figure gives way before the words do
- **the conversation's own newest lines**, drawn the way the conversation draws them, fading
  with age so the newest are where your eye lands; lines that just arrived are lifted for a
  moment and then settle. A turn a team started shows what the team sent (`◆ manager →
  @api  do` and its words) and the reply, never the note codeaf wrote to start the turn, the
  same as the conversation itself
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

`Open ↵ ── Select ␣ ── Close x`

`Open` becomes `Answer` on a tile waiting on you. The keyboard has its own **focus**, drawn as
a heavy border; the arrows (or `h j k l`) move it, and the pointer never does. The focused tile
shows its action row too.

## Selecting several conversations

`space` (or `Select` on a tile) picks the focused conversation. Once one is picked, every
tile shows its box, `☐` or `☑`, and a press toggles its selection instead of opening it.

The tray reads `2 selected   Close views   Clear esc`. Close views removes the picked views
from this window; the work keeps running, and work in flight is asked about first.
Clear or `esc` unpicks them all. The tray cannot create teams or change memberships.

## Teams: named groups of conversations

A **team** is a named group of conversations, such as `harbor`. A conversation can belong
to several teams. The Chats overlay shows all the selected team's members, manager first;
the `▦ All` grid always shows all conversations open in this window regardless of team.

Create a team with **+ New team** below the Teams sidebar list. **+ Add subteam** beside
Add member creates one under the selected team. The naming card starts with the conversation
in front selected and offers a name and colour. A shared project folder supplies the name;
otherwise a word is offered and the naming model may suggest a short name until you type.
Type a name, `ctrl+r` for another name and colour, arrows to change colour, `enter` to create,
or `esc` to cancel. Rename afterward in Settings.

Use **+ Add member** in the team's overview to add an existing conversation or create one.
Use the member card's **x** to remove that membership with confirmation. Current work finishes,
and the conversation survives with its other memberships. Assign another manager before
removing the current one. **Choose manager** selects an existing member; to use a new
conversation, add it first, then choose it. A manager may manage its team's descendants but
cannot also manage an unrelated team or be both the global and an ordinary team's manager.

Selecting a team in the Chats dropdown enables its overlay; None removes it. Starting a new
conversation while an overlay is selected joins that team; returning to an existing one
changes no membership. Team colours mark memberships and overlays, not the global grid button.

## Team settings and teams inside teams

**Settings** at the right of a selected team's overview opens its name, colour, inherited
settings and overrides. **Inside** opens the Move into picker. You can also move a team with
`m` or a drag in the Teams sidebar; each move can be undone briefly. The sidebar and Chats
Teams dropdown show the hierarchy. All teams nests smaller subteam cards inside parent cards.
The grid offers no team settings or hierarchy controls.

## Moving a conversation between teams is written to Traffic

Moving a conversation from one team to another writes one Traffic line on each side:
`@web moved to harbor` on the team it left, `@web joined from ops` on the team it joined.
Moving a whole team under another writes the same kind of line on the team that moved and on
its new parent. A move that does not go through writes nothing. The manager of a team that
gained or lost a member is told on its next wake, from the Traffic it already reads.

**Disbanding a team.** `D` asks for confirmation naming the selected team and every subteam.
Current work finishes. Conversations and other memberships survive; lost reporting memberships
leave conversations independent. The bottom `Show closed` toggle reveals read-only history.
Disbanded teams cannot be reopened. Permanent team deletion removes team records and history,
never their conversations. Permanent conversation deletion removes all its memberships instead.

## Organize: teams suggested for your conversations

Choose **All teams** on Teams and press **Organize**. Its card suggests groupings of open
conversations. Nothing changes until Apply. Suggestions come from two sources:

- **Folders:** two or more conversations sharing a project folder become a suggestion
  named for it. An existing team of that name receives only missing conversations.
  A folder already covered by one team is left alone. This pass is free.
- **The naming model:** one request uses conversation titles, folders and current teams
  for other groupings or additions. Folders win disagreements. It has ten seconds;
  `thinking…` shows while it works, and a known estimated price appears below.

If the model is unavailable, the card shows `suggestions from folders only`. With no
suggestions it says `Everything is organized` beside Close. Rows start selected.
Arrows move; space or a press toggles; enter or Apply takes the selected suggestions.
Escape, Cancel or a press outside leaves everything unchanged. Suggestions can include
one conversation in several teams. New teams use the offered names and colours.

After Apply, All teams shows **Organized** and offers **Undo** briefly. Undo removes the
new groups and membership additions from that organization pass. The card may also offer
**Disband 3 quiet teams** for teams idle for a week with nothing waiting. Apply disbands
them; Undo does not reopen them. Organization otherwise only adds memberships: it does
not rename teams or remove conversations. Nothing runs automatically. Press Organize
again to refresh suggestions. The conversation grid has no Organize or Undo control.

## The team switcher on the tab strip

The dropdown is first on the Chats tab strip. With no overlay its label is **Teams ▾**;
with one selected it shows that team's name and colour. Its first choice is **None**,
followed by the active team hierarchy. It has no All option and no management actions.
Closed teams are available through Show closed on Teams, not through this picker.

Selecting a team restores its conversation and horizontal strip position. A new overlay
keeps the current conversation if it is a member, otherwise selects the manager first.
None restores ordinary Chats. `↑` and `↓` move, `enter` chooses, and `esc` or a press
outside cancels. Long pickers scroll with the arrows or wheel, keeping the selected row visible. On very narrow strips the dropdown gives way to the current tab and
the fixed right-edge `▦ All` button. The grid button opens all conversations open here;
it does not select an overlay.

## A team's manager

A **manager** is a conversation that runs the team: you talk to it, it sends work to members
and reports progress. Teams offers **Choose manager** for existing members and **+ Manager**
when a team has none. Add a new member first when you need a new conversation to replace
an existing manager. All teams has separate controls for the global manager.

In a selected overlay the manager's tab is pinned at the left. It reads `◆ Manager`, or
`+ Manager` while there is none. The grid does not pin managers or relabel their tiles.
`alt+m` goes to the selected team's manager. A manager can manage descendants of its team,
but cannot manage two unrelated teams. Ordinary memberships in multiple teams are allowed.

A team chat's sidebar offers Tasks and Traffic. A traffic row names its sender and recipient;
in a member's chat that member reads as `you`. Clicking a row opens the interaction in its
conversation. `←` and `→` switch the sidebar view, and `alt+l` shows or hides it.

The **team manager** manual describes manager capabilities. Teams (`/teams`, `alt+2`) shows
member cards, saved updates, recent interactions and decisions waiting on you. Member aliases
and interaction links open Chats with their originating overlay.

## What clicking a team name in a chat does, and how team names and handles are links

In a conversation that is in a team, every **handle** of a member of that team, like
`@security`, is a link wherever it appears: in a reply, in a team's quoted card, in the
surface's own notes and in a team tool's call such as `team_send @security`. So is a team's
name where it is written as a team: `team harbor`, `the harbor team` or `"harbor"`. The
pointer on one puts a ground under it and the hint line says what a press does, like
`Open @security · santosh dev2 branch code… · click`. A press opens that member, resuming it
first when this window does not have it open. A press on a team's name opens the **teams
page** with that team selected: the rail's cursor on it and the pane showing it. The hint
says `Open harbor on the teams page · click`. A closed team is selected with Show closed enabled. Over `--host`, against an engine that has no teams doors, the teams page
cannot open, so the press opens the team-agnostic conversations view instead and the hint says
so. An `@word` that is no member's handle is left as plain text.

## Mention a team or another conversation with @

In the message box, `@` opens the same list files use. Teams are a section of it,
each row a coloured dot and the team's name. Conversations are the next section:
the ones open in this window first, then recent ones. A conversation in no team is
on that list.

The first row is the words **team**, **chat** and **file**. Each is a button with a
background under the pointer and a one-line hint (`only teams · click`). A press
types `@team:`, `@chat:` or `@file:`, and the list keeps only that section. Typing
filters every section that is still showing.

Choosing a team inserts `●harbor` in the team's colour. Choosing a conversation
inserts `@handle`, or a short slug of its title when it has none, and the row's
hint is the full title. After you send, both stay links. A press on the team opens
the teams page with it selected. A press on the conversation opens that conversation.
Over `--host`, against an engine with no teams doors, a press on the team opens the
team-agnostic conversations view instead.

The model receives a short digest of each reference: for a team, its members,
handles, states and recent traffic; for a chat, its title, its state and an excerpt
of the last reply. It does not receive the transcript. Mentioning a conversation
does not message it and does not wake it. Your transcript keeps the words you typed.

## Where teams are kept

Teams are saved in your profile, in `teams.json`, every time one changes, and never in
`config.json`. Over `--host` that is the far machine's profile, where the conversations run. A `spaces.json` from an earlier build is read once, its groups keep their
names, members and colours, and it is renamed to `spaces.json.migrated`. A teams file that
cannot be read is moved aside as `teams.json.unreadable-<number>` rather than written over,
so nothing you made is lost.

## The wall said my team was not saved

A change to your teams shows in this window at once and is saved a moment later. What says
it happened waits for the save: `Made harbor · 2` on the Teams row, `Organized · 1 new team`,
and on the teams page `harbor is closed` or a move's words. When the save is refused (another
codeaf holding the file, a file that would not read, a far machine whose teams kept changing),
those words never show. The row says `harbor was not saved · <the reason>` instead, and the
teams page says `the close of harbor was not saved` or `the move was not saved`. The change is
still in this window, and Undo is still offered where it was.

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
| `tab`, `shift+tab` | Next or previous conversation in the grid |
| `/` | Filter conversations by name; `esc` clears it |
| `-`, `+` or `=` | Fewer or more columns |
| `0` | Columns back to automatic |

In the new-team card: type the name, `ctrl+r` another name and colour, `←` `→` the colour,
`enter` create, `esc` cancel.

In the Organize card: `↑` `↓` move, `space` tick or untick, `enter` apply, `esc` cancel.

## Does deleting a team close its conversations?

No. Permanently deleting a team removes the selected team and every descendant record and
their saved interactions and decisions. Active teams are disbanded first. Their conversations,
context and current work survive as ordinary sessions. Other memberships survive too.
The confirmation names every affected team. Deleting a conversation itself is separate:
`/delete` stops that conversation, deletes its transcript and removes all its memberships.
