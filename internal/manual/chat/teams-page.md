# The teams page

## What the teams page is, and how to open it

The **teams page** is where you run your teams: every team you have, what waits on you from
them, and an overview of the team you choose. Member cards show activity and recent updates;
interaction links take you into the conversation where an exchange happened. It is the second
place on the tab bar, right after home: `home  teams  chats  sessions  spend  settings`. Open it with
`/teams`, `alt+2` (`opt+2` on a Mac), a click on the word `teams`, `tab` from home, or the map
(`alt+.`). A number beside the word on the bar counts the decisions waiting on you that arrived
since you last looked.

A team is a group of conversations you name, and a team's **manager** is a conversation that
runs it for you (the **team manager** page). The conversations view (`alt+v`) is where you see
every open conversation at once; All teams on this page is where you organize your teams.

## Where a team link in a chat opens

A team link in a chat opens this page. That is a team's name written as a team (`team harbor`,
`the harbor team`, `"harbor"`), a team name on a team card or in a team tool's row, and a
`●harbor` you sent with `@`. A press selects that team: the rail's cursor on it, the pane
showing it. A closed team turns on `Show closed` and selects its retained history. The hint says
`Open harbor on the teams page · click`. Over `--host`, when the engine has no teams doors,
the press opens the team-agnostic conversations view instead, and the hint says so.

## The rail: your teams as a tree

The left column is the **rail**:

```
 All teams               │
 ● harbor ◆          ? 1 │
   ● orbit           ⠿   │
 ● docs                  │
 + New team              │
                         │
 ☐ Show closed · 2       │
```

- **`All teams`** is the top row. With no manager over every team, its Global manager card offers `+ Global manager`,
  which starts a separate global manager conversation: you talk to it, and it talks
  to the managers of open top-level teams. Subteam managers report to their parent team.
  Once there is one, All teams still opens the overview; its Global manager card opens that conversation.
- **Every open team**, a sub-team indented under the team it belongs to, with its colour dot.
  A team with a manager wears a dim `◆`.
- **A mark only when something is happening.** A dim `⠿` says a member of the team is
  working; an amber `? 2` says two things wait on you from it or from a team under it: a
  decision addressed to you, or a member stopped on a question only you can answer. A team
  where nothing is happening draws no mark at all.
- **`+ New team`** stays visible below the active team list and always creates a top-level team.
  **`+ Add subteam`**, beside and after **`+ Add member`** in a selected team's header,
  creates inside that team. The depth limit still applies.
- **`✦ Organize`** in the All teams header suggests groupings and disbanding quiet teams. Its dialog stays on Teams; nothing changes until Apply.
- **`Show closed · N`** is a toggle at the bottom of the rail. Turn it on to include retained
  teams in the list and All teams cards, marked `closed · read-only history`. The toggle stays
  visible while long lists scroll, and turning it on reveals the retained sidebar rows.
  Each record shows its full ancestry, such as `harbor › orbit`; long paths wrap. Turning it
  off hides those teams again; it never deletes their history.

`↑` `↓` walk the rail, `enter` or a press chooses a team, and `←` `→` cross between the rail
and the pane beside it. Choosing a team changes the overview without changing the chat in
front or its draft. The Chats dropdown selects the same team; returning to Chats restores
its last conversation. A team chosen in Chats also selects its Teams overview. None in
Chats maps to All teams here. Choosing All teams here enables its global overlay when the
optional global manager exists. Clicking a member opens Chats with that team's view selected.

## All teams overview: parent cards and nested subteams

Choose **All teams** in the Teams sidebar to see one card per top-level team, in stored order.
Each card shows the team's name, conversation count, activity and unread counts separately,
spending when available, the manager's alias and conversation title, and up to two lines
of its latest saved assistant update. Smaller subteam cards sit inside their parent's card;
deeper descendants remain nested. `Show closed` includes retained teams in this hierarchy;
their cards lead to read-only history. Narrow panes use one column. The existing preview limits
and remote restrictions described below also apply here.

Click a team's name or card background to inspect its Teams overview without opening a chat.
Click a manager alias to open its conversation with that team's Chats overlay. Reading a
preview does not mark a conversation read. The dedicated Global manager card precedes the team cards;
it shows its alias, saved update, activity, spending and reporting teams. Its alias and
preview lead to its conversation. Pending decisions remain directly actionable below the global manager and above the team cards.
A bold white `teams` heading separates the global manager from the team cards.

Arrow keys walk the cards; `space` picks teams and `m` moves the focused or picked teams.
Drag a team card onto another card or sidebar team to move it; the empty sidebar below the
team list means Top level. Drag a manager alias onto a team to add that conversation as a
member. A membership drag never removes other memberships. Long overviews reveal the card
reached by the keyboard. Move confirmations and Undo remain on Teams.

**Organize** opens its suggestions here, including when reached from the conversation grid.
Apply changes only the checked suggestions; Undo appears here after the change is saved.
Closing the dialog returns to All teams without changing the Chats overlay or draft.

## The pane: the team you chose

The sidebar selection names the team; the pane does not repeat that title. Its controls row
shows today's spending and its cap when available. `Settings`
opens the team's settings and spending controls. Member cards provide the conversation
links directly; there is no separate `Members` button. `p` opens the keyboard member list
for dragging a conversation onto another team.

The manager comes first in a full-width card, about forty percent shorter than the previous
large preview. It ranges from eight to fourteen rows to keep authors and recent text readable.
The Recent interactions panel follows the manager and separates it from the compact
ordinary member cards, which retain a plain border and no role title. The manager card shows a labelled latest excerpt from up to eight
recent messages, preserving authors and paragraph breaks. A clipped message says `continued`.
On short windows, Down from the manager alias or a wheel tick on its card reveals the latest
excerpt lines; Up or a wheel tick upward returns to the alias. Clicking either opens Chats.
Messages currently displayed in the front conversation take precedence over saved previews.
Other conversations use the saved transcript tail, which may describe earlier work while
another turn is running. Ordinary cards retain two lines of the latest saved assistant update. Clicking an alias, title or preview opens the conversation in Chats
with this team's view selected. The card's `working`, `asking`, `failed` or `idle` state is
separate from `unread`; looking at a preview does not mark the chat read.

A preview reads only the last 64 KiB of a local transcript. `Updates appear here` means no
assistant update was found in that tail. `Conversation unavailable` means the file could
not be found. Saved remote transcript previews are not available over `--host`; the front conversation
can still show its already displayed messages. Local files are never read as substitutes
for a remote conversation.

## Adding and removing team members

`+ Add member` on the selected team's overview opens a searchable conversation picker.
It lists existing saved conversations across projects, including ones already in other
teams; members of this team are left out. Type a title or workspace to filter, use arrows
and `enter`, or click a row. Adding an existing conversation keeps its context, other
memberships and reporting manager.

`+ New conversation` in the picker asks for its first assignment. `enter create`
creates a session in the team's workspace, adds its membership, opens it in Chats with this
team's overlay, and submits the assignment. It follows the conversation's ordinary tool
approval rules. `esc cancel` dismisses the sheet; Esc also cancels a pending creation.

Each ordinary member card has a small `x` that removes only this membership. Its session,
draft, transcript, running work and other memberships survive. A manager cannot be removed
with `x`. Before permanently deleting its conversation, assign another manager in Teams
for every active team it manages. There is no member Actions menu. Removing the reporting membership leaves that
conversation independent; another manager is never assigned automatically.

`a` opens Add member. The picker uses `Name` and `Project` columns and does not display IDs.

## Recent interactions: scrolling, expanding replies and opening their conversations

`Recent interactions` is one boxed table with a fixed header and compact exchange rows.
The table has no lines between rows. Its log keeps the latest 200 entries, grouped by
exchange, with the most recently active exchange first. A reply count counts messages,
not work-status events. Click the disclosure at the start of a row to expand the full
message and its replies inline; click it again to collapse them.

The wheel over the panel scrolls only its contents. One down control, `Next page`, advances
by a full viewport: with six rows, 1–6, 7–12, then 13 onward. A short final page does not
repeat the previous page's rows; the control says `Last page` and stays there. `pgup` /
`pgdown` move back or forward by a page. The rest of the overview stays in place. At smaller heights,
walk into the panel with the arrow keys to bring it into view.

Clicking a participant opens that participant's conversation at the exchange. Clicking
the message opens its sender's conversation; a reply opens its author's conversation.
When the retained chat history has no copy of that message, Chats opens at the bottom and
says `that message is older than this chat's history`.

## What waits on you: decisions, permissions and spending controls

Expanded decisions and member permission prompts have a bounding box on the overview.
Each box groups the question with its answer choices. Click an option to decide, or `Your own answer…`
to enter your own response (`enter` submits, `esc` cancels). The newest three cards show
whole; older cards collapse to one line and can be expanded. Each shows who raised it,
what waits on you, its options and any recommendation.

Members stopped on permission prompts show the prompt's actual answer options, including
the conversation already in front. Cap decisions retain `Raise to $10` and `Stop for today`
when those are the offered actions. Closing reports keep their account of completed work,
remaining work, files and spending. A long overview scrolls as you walk its controls.

## Renaming a team updates the message box

Renaming a team updates the overview's name and the team labels in Chats. Teams itself has
no message box. To talk to the manager, click its card and type in its normal chat.

## The pane said the manager was open in another window

Selecting a team no longer opens or locks its manager's conversation. The overview can be
read without resuming any member. Clicking a member uses the ordinary Chats door, which
says any refusal there. If a local manager's conversation is missing, `+ Manager` offers a
new manager. Starting a manager opens its fresh conversation in Chats immediately.

## What does +4 idle mean on a team

The earlier overview grouped idle members into `+4 idle`. Every member now has its own
card, including idle members. `p` still opens the keyboard member list; aliases lead to
Chats without a separate `Open` or `Resume` button.

## Moving a team inside another team, and adding a chat to a team

Teams nest: a team can sit inside another, the way `orbit` sits inside `harbor` on the rail.
You move a team by choosing where it goes.

**Move into…** Choose a team on the rail and press `m`, or drag its team card onto another team. A picker opens with every team as a tree and `Top level` first:

```
╭─ Move dock into ─────────────────────────────╮
│  Filter    ▏type to filter                   │
│  ─────────────────────────────────────────── │
│  Top level                                   │
│    ● harbor                                  │
│      ● orbit                                 │
│    ● dock                                    │
│  ─────────────────────────────────────────── │
│  orbit is 2 levels deep · limit 2 · Settings │
╰──────────────────────────────────────────────╯
```

Typing filters the list and the tree keeps its indent. `↑` `↓` walk, `enter` moves, `esc`
cancels. **A team that cannot take the move is dimmed, not hidden**, and the line at the foot
(and the hint line) says why: the team itself (`a team cannot go inside itself`), a team inside
it (`orbit is inside dock`), a closed team, a team already at the **depth limit**
(`orbit is 2 levels deep · limit 2 · Settings`, and `set on harbor` when a team above set the
limit), or where it already is. The depth limit is `team depth` under **Teams** in
`/settings`, and any team can override it on its card.

**Several teams at once.** `space` on a team's row picks it, and it wears `☑` in place of its
dot; `m` then moves every picked team with one choice. `esc` clears the picks. A picked team
inside another picked team moves along inside it.

**Dragging.** On the rail you can also drag a team with the pointer. A drag starts only after
you move two cells with the button held, so a click still only chooses the team. While you
drag, only a team that can take it is highlighted, and the hint line says
`Drop to move dock into harbor`, or why the team under the pointer cannot take it. The empty
rail under the teams is the top level, marked `↳ Top level` while you drag. `esc` drops the
drag and nothing moves.

**Adding a chat to another team.** Open the members card and drag a member's row onto a team
on the rail: the hint says `Add @crane to harbor`, and the conversation is **added** to that
team. It stays in the team it came from; a drag never takes a conversation out of a team.
Removing one is always its own step, in the team switcher or the conversations view.

**When a move changes who is in charge, you are asked first**, in one line at the top of the
pane (or on the team's card):

```
 dock will report to harbor's manager · its $3/day becomes part of harbor's $10 pool   Move   Cancel
```

It appears only when the move changes one of three things: which manager the team's
conversations report to, which capped pool its spending counts toward, or which manager decides
a conflict inside it. Any other move happens at once.

**Every move can be undone.** For a few seconds after a move, confirmed or not, the pane (or the
card) says `dock is in harbor now   Undo`; `Undo` or `u` puts it back where it was.

## A move is written to Traffic

When a move is kept, Traffic records it. The team that moved, and the team it left, each get
`@crane moved to harbor`. The team it joined gets `@crane joined from ops`. One line per
member, once. A move that was refused (the team could not go there) writes nothing. A manager
whose team gained or lost someone reads that line the next time it wakes, from the Traffic it
already reads. The move does not start a wake of its own.

## Keys on the teams page

Teams owns its keyboard. Typing here does not edit a manager's draft.

| Key | What it does |
|---|---|
| `↑` `↓` | walk the rail or overview |
| `←` `→` | move along a row or between the rail and overview |
| `enter`, `space` | activate the selected control |
| `pgup`, `pgdown` | previous or next interaction page |
| `tab`, `shift+tab` | next or previous place |
| `alt+1` … `alt+8`, `alt+.` | jump to a place, show the map |
| `s` | team settings |
| `c` | confirm disbanding the selected team |
| `w` | conversations view for this team |
| `n` | new top-level team |
| `o` | Organize |
| `m` | Move into… |
| `space` on a team | pick it for a move of several |
| `p` | member list |
| `a` | Add member picker |
| `M` | start a manager when offered |
| `d` | confirm permanent team deletion |
| `u` | Undo a move while offered |
| `esc` | cancel a drag, pending move or picks; otherwise return to Chats |

After `M` starts a manager, Chats opens and its message box receives the keyboard.

## A team's card: its settings, and where each value comes from

`Settings` on the Teams header or `s` here opens the team's **card**:

```
╭─ Team settings ────────────────────────────────────────────╮
│  Name      orbit                                           │
│  Colour    ◉ ● ● ● ● ●                                     │
│  ────────────────────────────────────────────────────────  │
│  questions go to the manager    on                            │
│  team messages wake             on                            │
│  daily cap                      $5.00 a day · from harbor  │
│  team depth                     2 levels        reset      │
│  sub-team share                 50%                          │
│  ────────────────────────────────────────────────────────  │
│  Delete team                                              │
│  up/down move · enter choose · esc done                    │
╰────────────────────────────────────────────────────────────╯
```

The card holds only what the team **overrides**. A value the team takes from somewhere else
is dim. Profile defaults omit the repeated provenance; an inherited parent override still
says `· from harbor` so spending and depth constraints remain clear. A value the team sets itself is drawn plain with
`reset` beside it, which gives the value back to what it inherits. `enter` on a row changes
it (the two on and off rows flip; the others take a figure), and `r` resets the row the
cursor is on. Wake follows the same styling, reset control and `r` shortcut as the other rows.
Team movement remains on Teams with `m` or dragging; Settings has no Inside field. A cap is dollars a day (0 for none), a depth is 1 to 10 levels, a share is 1 to
100 percent. The name is edited as you type and kept with `enter` or when the card is put
away; `←` `→` choose a colour. `esc` puts the card away. `Delete team` is its only button for an ordinary team;
disbanding remains on the main pane. Global settings have no deletion button.

## Disbanding a team — does current work stop?

`Disband`, `c`, or `D` on the conversations view asks for
confirmation. It lists the selected team and every descendant. Disbanding ends all their
memberships and coordination. Current turns finish, conversations survive, and memberships
in other active teams survive. A conversation losing its reporting manager becomes independent.
An affected team overlay returns to None; the retained team history remains selected in Teams.

Long confirmations scroll with `pgup`, `pgdown` or the wheel. Cancel changes nothing.
Disbanding cannot be undone or reopened. `Show closed` reveals the disbanded history.

## Closed teams: read-only history and permanent deletion

Turn on the bottom `Show closed · N` toggle and choose a retained team in the list or
All teams cards. The pane retains its
roster, interactions, decisions, report and spending history. Surviving conversation links
open bare Chats. Deleted conversations are unavailable, while copied exchanges and decisions
remain readable. New messages and decisions cannot change disbanded history.

`Delete` (`d`) permanently deletes the selected team and every descendant, including their
interactions and decisions. Settings offers `Delete team` for active teams too: they are
first disbanded. The confirmation names all affected teams and stacks `Keep` above `Delete`,
with `Keep` selected by default and `up/down` choosing between them. Conversations and current work
survive. Deletion cannot be undone. Older hosted engines without checked deletion refuse it.

## Organize disbands quiet teams

`✦ Organize` suggests groupings and **`Disband 3 quiet teams`** when teams have no activity
for a week and nothing waiting. Nothing changes until Apply. Undo restores grouping changes;
it does not reopen disbanded teams. Over `--host` quiet-team suggestions are not offered yet.

## With no teams yet

The Global manager card offers its optional creation button before the first team exists.
Below it, the page says what a team is in one sentence and offers two buttons: **`✦ Organize my
conversations`**, which suggests teams from the conversations you have open, and
**`+ New team`**. `o` and `n` press them.

## Over --host: why a closed team's report is not readable

Over `--host` the page shows the teams of the machine the conversations run on: their
decisions, their spend and their managers. The **Teams** tab of `/settings` edits that
machine's defaults. Team Settings omit the repeated profile-default provenance. An older engine keeps the tab
read only and says `changing them is not available over this connection`. A closed team's
report is not read over the connection yet, and the page says
`its closing report is kept where the team ran, and is not readable over this connection`
where the report would be. A plain local launch reads the report from this machine's engine
profile and shows it in the closed team's pane.

## Why the page looks the way it does

The overview keeps teams visible while their conversations work independently. Cards share
the same geometry, with the manager distinguished by its role. Updates can be read without
opening a conversation; aliases and exchanges lead into Chats when detail is needed.
The interaction table keeps many exchanges visible in one panel and expands replies inline.
Choosing a team never changes the current chat or takes the keyboard into its draft.

## Reporting after membership removal

A conversation has one reporting manager, chosen by its reporting membership. Removing that
membership or disbanding its team leaves it independent. Other memberships remain links.
There is no reporting-assignment action on the member cards.

## Will closing a sub-team stop its manager if that manager is also in the parent team?

The former Close/Reopen actions have been replaced by Disband. Disbanding a sub-team lets
its manager's current work finish, preserves its conversation and keeps its parent-team
membership. It recursively disbands only the selected sub-team and its descendants.
The confirmation lists them. History stays read-only with Show closed; there is no Reopen.

## New team placement and removing a member

The Teams sidebar keeps `+ New team` directly after the displayed teams list. `Show closed`
remains at the bottom of the sidebar. `+ Add member` and `+ Add subteam` stay together
on the left of the selected team's header, followed by `Choose manager`; `Settings` and `Disband` sit on the right.

A member card's `x` opens a confirmation naming the member and team. `cancel` is selected
by default; choose `yes` to remove only that team's membership. Its work finishes and its
conversation remains. Removing the manager requires replacing it first.

## Create an empty team or select several conversations

`+ New team` opens a creation dialog over Teams, keeping the overview behind it.
`+ Add subteam` uses the same dialog with the selected parent named in its title.
Enter Name, choose Colour, and optionally select members from the searchable Name/Project list.
Click several rows to toggle their checkboxes; filtering preserves hidden selections. `tab`
changes fields, arrows move, `space` selects a highlighted member, `enter` creates, and `esc`
cancels. No internal IDs are shown. A duplicate name or invalid parent is refused.

You may create an empty team and add members afterward. Creation never assigns a manager or
opens its members. Both routes land on the new team overview. A failed save is reported there.
Chats also offers `+ New team` in its full saved-conversation grid: select cards, then name
and create. The full conversation grid itself lives only in Chats.

## Keyboard hints in team menus

Team menus show grey instructions together at the left of the box footer, with the key first:
`up/down move · space select · esc cancel · enter create` in New team and
Add subteam. Narrow boxes wrap the instructions rather than hiding them. Only text fields,
colour, and members are tab stops; the creation hint stays in that footer. Add member and
Choose manager show `up/down move · enter choose · esc cancel`; creating a new conversation
shows `enter create · esc cancel`. Settings, Move, Organize, and the Chats team-naming card
use the same key-first format. Clickable action hints act on the same choices as their keys.

## Choose manager — change a team's manager from existing members

`Choose manager` in the selected team's header opens a filtered Name/Project list of its
existing members, excluding the current manager. `cancel` is selected by default. Choose
a member with Enter or click; Esc cancels. Only this team's manager changes. The former
manager remains a member, and both conversations keep their work and history.

The picker cannot create a conversation or add membership. All teams also offers
`+ Add member` for adding a global-manager candidate; automatic memberships stay protected. For a new manager, first use
`+ Add member` to add or create its conversation, then use `Choose manager`.
An ordinary team manager cannot be removed or permanently deleted while it manages an active team.
The deletion confirmation names every active team it manages and says
`Assign another manager before deleting it.` Replace it in every named
team before deleting its conversation. Missing conversations are not replacement candidates.

## Can one conversation manage multiple teams?

A conversation can manage one team and any descendants of that team. It cannot also
manage an unrelated team. Managing sibling teams requires also managing their common
ancestor. The global All teams manager is a separate conversation; it cannot also manage
an ordinary team. Ordinary membership in multiple teams remains allowed.

Choose manager and team moves refuse changes that introduce conflicting responsibilities,
naming the affected teams. Replacing a parent manager can also be refused if it would
leave the former manager managing sibling teams without a managed ancestor; replace those
subteam managers first. Older conflicting assignments remain visible and can be repaired
explicitly in Teams. Loading them does not silently change managers.

A new manager's empty conversation survives navigation and restart. If its transcript is
missing, its card says `Conversation unavailable`; add or select a replacement explicitly.

## Reading a long manager deletion refusal

A blocked deletion names every active team the conversation manages. In a short terminal,
scroll the message with the mouse wheel or Page Up/Page Down. Cancel/delete and the
keyboard hints remain visible. Choose replacements through Teams → Choose manager first.

## Global manager — create, delete and recreate

All teams shows a dedicated **Global manager** card above its team cards. The global
manager is optional. `+ Global manager` appears only in this card and creates a separate
conversation; the managers of open top-level teams become its reports automatically.
Ordinary members and subteam managers are not direct reports. The card shows the current
reporting teams and up to four lines of the latest saved assistant update. Read the preview
without marking the conversation read; click its alias or preview to open and respond.

The card's top-right `x` permanently deletes the global-manager conversation with the usual
`cancel` / `delete` confirmation. Its teams, their managers, running work and history remain.
This same optional-global-manager exception works from Home, Chats and Sessions.
`+ Global manager` reappears so it can be created again; assigning it reconnects current
top-level managers as reports. Ordinary members retain their reporting choices. A global manager cannot also
manage an ordinary team; conflicting old assignments must be repaired before deletion.

Without a global manager, the card shows only `Optional · coordinates the managers of
top-level teams` and `+ Global manager`. This same state returns after deletion and when
the saved manager conversation is missing. With a manager, the creation button disappears;
`+ Add member` and `Settings` remain alongside the conversation's `x` delete control.
`Choose manager` is unavailable for the global role. `Settings` retains global spending controls.

## Model names on manager and member cards

Every manager and member card shows its conversation model beside the white alias, such as
`@picker · ~deepseek/deepseek-v4-flash-latest`. The separator and model are grey. A long model
ends in `...` to fit the card; the alias keeps its space. Unknown models show no label.
Live conversations use their current model; other conversations use their saved model,
refreshed with the Teams overview. Changing a model saves that choice immediately, so
another window can reflect it without waiting for another message. The All teams sidebar
row has a white circle; the selected main navigation tab is bold with the hover highlight.
