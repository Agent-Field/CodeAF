# The teams page

## What the teams page is, and how to open it

The **teams page** is where you run your teams: every team you have, what waits on you from
them, and the manager of the team you choose, which you talk to right there. It is the second
place on the tab bar, right after home: `home  teams  chats  sessions  spend  settings`. Open it with
`/teams`, `alt+2` (`opt+2` on a Mac), a click on the word `teams`, `tab` from home, or the map
(`alt+.`). A number beside the word on the bar counts the decisions waiting on you that arrived
since you last looked.

A team is a group of conversations you name, and a team's **manager** is a conversation that
runs it for you (the **team manager** page). The conversations view (`alt+v`) is where you see
every conversation at once and group them; this page is where you steer the teams you made.

## Where a team link in a chat opens

A team link in a chat opens this page. That is a team's name written as a team (`team harbor`,
`the harbor team`, `"harbor"`), a team name on a team card or in a team tool's row, and a
`●harbor` you sent with `@`. A press selects that team: the rail's cursor on it, the pane
showing it. A closed team is selected inside `Closed`, and that fold is opened. The hint says
`Open harbor on the teams page · click`. Over `--host`, when the engine has no teams doors,
the press opens the conversations view on that team instead, and the hint says so.

## The rail: your teams as a tree

The left column is the **rail**:

```
 All teams     + Manager │
 ● harbor ◆          ? 1 │
   ● orbit           ⠿   │
 ● docs                  │
                         │
 + New team in harbor    │
 ✦ Organize              │
                         │
 ▸ Closed · 2            │
```

- **`All teams`** is the top row. With no manager over every team it offers `+ Manager`,
  which starts one conversation that manages all of your teams: you talk to it, and it talks
  to each team's own manager. Once there is one, the row is that manager's.
- **Every open team**, a sub-team indented under the team it belongs to, with its colour dot.
  A team with a manager wears a dim `◆`.
- **A mark only when something is happening.** A dim `⠿` says a member of the team is
  working; an amber `? 2` says two things wait on you from it or from a team under it: a
  decision addressed to you, or a member stopped on a question only you can answer. A team
  where nothing is happening draws no mark at all.
- **`+ New team`** makes a team of the conversation in front (the new-team card of the
  conversations view). With a team chosen it reads **`+ New team in harbor`** and the new team
  is made inside that team; on a narrow rail it takes two rows, `+ New team` and `in harbor`.
  A team already at its depth limit dims it and says why. **`✦ Organize`** suggests teams for
  your conversations and offers to close the quiet ones (see *Organize closes quiet teams*
  below).
- **`▸ Closed · N`**, folded at the foot, holds the teams you closed. A press opens the fold
  and lists them; a press on one shows what it left behind.

`↑` `↓` walk the rail, `enter` or a press chooses a team, and `←` `→` cross between the rail
and the pane beside it. Choosing a team with a manager brings that manager's conversation in
front, in the pane; the conversation you were in stays open behind, one `tab` away on the strip.

## The pane: the team you chose

The right side is the team you chose, from the top:

**The header** is one line:

```
 ● harbor  ◆ Manager   @news ⠿ working   @review ? asking   +4 idle     $0.42 today   Settings  Close…  Open ▦
```

The team's name; **`◆ Manager`**, a door to its manager (the conversation below it on this
page); then only the members that are doing something, each a door with its title and state
in the hint (`@news · weekly news digest · working · click opens`): `⠿ working`, `? asking` in
amber when a member is stopped on a question for you, `✗ failed` in red when its last turn
failed. Everyone else is one quiet word, **`+4 idle`**, or **`6 members`** when nobody is doing
anything, and a press on it opens the members card. Then what the team spent today, shown
only when there is a spend or a cap to compare it with: `$0.42 today`, or `$0.42 of $5 today`
under a daily cap. When the cap is inherited from the team above, the header names whose it
is, `$1.20 of $5 today · harbor's cap`, because a cap is one pool for a team and every team
under it. Then three word buttons: **`Settings`** (the team's card, `s`), **`Close…`** (`c`),
and **`Open ▦`** (the conversations view narrowed to this team, `w`).

On a narrow screen the line gives up its parts in order: the idle word first, then the spend,
then the members' chips from the last, then `◆ Manager`, then the buttons from the right. The
team's name always stays. `p` opens the members card whatever the width.

**The members card** (`+4 idle`, `6 members`, or `p`) is titled the way the header counts,
`harbor · ◆ Manager · 1 member`, and lists every member, one row each: its
handle, its title, what it is doing, when it last moved, and **`Open`** for a conversation this
window has open or **`Resume`** for one it does not. A member that reports to another team's
manager carries a small `also in test` tag, and its hint says `reports to test's manager`.
`Open` goes to the conversation. `Resume` opens it **behind**, in a tab of its own, without
moving you: the page says `@docs is open behind, in its own tab`, and the row turns to `Open`.
`↑` `↓` walk the rows, `enter` opens or resumes, `esc` or `Close` puts the card away. The
page never says `not open`: whether a conversation is open is a fact about this window, not
about the team.

**What waits on you.** Each decision addressed to you is a card, led by the same `?` the rail
and the tabs use for something that needs you:

```
 ? conflict · raised by @boss                         waiting on you
 Which lexer do we keep?
   @parser   the new lexer is 3x faster and passes every test
   @model    the old one is what the grammar tool emits
  Keep the new lexer   we maintain a fork  ✓ recommended
  Keep the old lexer   3x slower, no fork
   recommended because speed is the goal of this team
  Your own answer…
```

One press on an option's word decides it, and the answer goes back to whoever raised it.
`Your own answer…` opens a line for words of your own; `enter` decides with them and `esc`
puts it away. A closing report shows what the team did, what is left, where the files are and
what it spent; a cap card shows `spent $5.20 of $5.00 today · harbor's cap` and offers
`Raise to $10` or `Stop for today`, which only you can decide. A member stopped on a
permission prompt shows `? @web asks` with the same answer buttons home offers for it, the
prompt's own options. The newest three cards show whole; older ones fold to one line each,
`▸ question · which port?   waiting on you`, and a press unfolds one. While the manager's
conversation is under them, the cards take about a third of the window: the newest (or the
one you unfolded) is whole, older ones stay whole while they fit, and the rest fold, so
there is always room to read and steer the manager. A card waiting on a manager instead of
you leads with `◆`, reads `waiting on ◆ harbor`, dim, and you can still decide it: you
outrank every manager. The **team questions and caps** page says what a packet is and where questions
go.

**The manager's conversation.** Under all of that is the team manager's own conversation, the
real one, with its transcript, its prompts and its message box, which says
`to ◆ harbor manager`. Typing talks to the manager. The conversation's right-hand column
(`Tasks` and `Traffic`) is folded on this page, because the teams rail already has the left;
`alt+l` or its edge unfolds it. A press on a handle in the Traffic goes to that member's
conversation, off the page, exactly as it does in the conversation's own screen.

While the manager's conversation is on its way in, the pane says `opening ◆ harbor's
manager…`. It never waits silently: if the conversation cannot be opened, or has not answered
within four seconds, the pane says why, `couldn't open ◆ harbor's manager: <reason>`, and
offers **`Retry`** and **`Open in chats`** (the manager as an ordinary conversation, off the
page, where anything else wrong is said on its own line). When the manager's conversation is
gone from the disk, the pane offers **`+ Manager`** instead, which starts a new conversation and
makes it the team's manager.

**A team with no manager** shows `+ Manager` under its members, beside one line on what a
manager does; a press starts a new conversation in the team's folder and makes it the manager.
Choosing `All teams` shows every decision waiting on you from any team.

## Moving a team inside another team, and adding a chat to a team

Teams nest: a team can sit inside another, the way `orbit` sits inside `harbor` on the rail.
You move a team by choosing where it goes.

**Move into…** Choose a team on the rail and press `m`, or open the team's card and press its
**`Inside: harbor ▾`** row. A picker opens with every team as a tree and `Top level` first:

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

## Keys on the teams page

While the manager's conversation has the message box, keys type into it, as in any
conversation. The page keeps these:

| Key | What it does |
|---|---|
| `alt+↑` `alt+↓` | put the keyboard on the page's buttons and walk them (`opt+↑` `opt+↓` on a Mac) |
| `esc` | from the page's buttons, back to the message box |
| `tab`, `shift+tab` | the next or previous place |
| `alt+1` … `alt+9`, `alt+.` | jump to a place, draw the map |

On the page's buttons, and on a team with no manager in the pane:

| Key | What it does |
|---|---|
| `↑` `↓` | walk the rail, or the pane |
| `←` `→` | along a row, and across between the rail and the pane |
| `enter`, `space` | press the button the cursor is on |
| `s` | the chosen team's card (its settings) |
| `c` | close the chosen team |
| `w` | open the conversations view on the chosen team |
| `n` | new team (inside the chosen team) |
| `o` | Organize |
| `m` | Move into…: move the chosen team, or the picked ones, inside another team |
| `space` | on a team's row, pick it for a move of several |
| `p` | the members card |
| `M` | start a manager for the chosen team |
| `r` | reopen a closed team |
| `d` | delete a closed team (it asks first) |
| `u` | Undo a close or a move, while it is offered |
| `esc` | cancel a drag or a move's question, clear the picks, then back to the message box, or home when there is none |

Any letter not in that list goes back to the message box and types there.

## A team's card: its settings, and where each value comes from

`Settings` on the header, `Team settings…` in the team switcher on the tab strip, `e` on the
conversations view, or `s` here opens the team's **card**:

```
╭─ Team settings ────────────────────────────────────────────╮
│  Name      orbit                                           │
│  Colour    ◉ ● ● ● ● ●                                     │
│  ────────────────────────────────────────────────────────  │
│  questions go to the manager    on · from Settings         │
│  team messages wake             on · from Settings         │
│  daily cap                      $5.00 a day · from harbor  │
│  team depth                     2 levels        reset      │
│  sub-team share                 50% · from Settings        │
│  ────────────────────────────────────────────────────────  │
│  Close team…                                     Done ⏎    │
╰────────────────────────────────────────────────────────────╯
```

The card holds only what the team **overrides**. A value the team takes from somewhere else
is dim and says where: `· from Settings` for the defaults under **Teams** in `/settings`, or
`· from harbor` when a team above it set it. A value the team sets itself is drawn plain with
`reset` beside it, which gives the value back to what it inherits. `enter` on a row changes
it (the two on and off rows flip; the others take a figure), and `r` resets the row the
cursor is on. A cap is dollars a day (0 for none), a depth is 1 to 10 levels, a share is 1 to
100 percent. The name is edited as you type and kept with `enter` or when the card is put
away; `←` `→` choose a colour. `esc` or `Done` puts the card away.

## Closing a team

`Close…`, `c`, `Close team…` on the card, or `D` on the conversations view closes a team.

- **Nothing running:** it closes at once, and `harbor is closed   Undo` stays at the top of the
  pane for a few seconds. `Undo` or `u` reopens it with its tabs.
- **Something running:** a card says who is still working and offers **Wrap up first**,
  **Close now** and **Cancel**. When the team has a manager, `Wrap up first` leads: the manager
  is asked to have everyone finish and commit and to bring you a closing report, which arrives
  as a card on this page with `Close` and `Keep going`; the team closes when you choose Close.
  `Close now` stops every member's turn and closes their tabs at once. `Cancel` or `esc`
  changes nothing.

Closing a team closes the teams under it. A conversation that is also in another open team is
never stopped by the close. The conversation you are looking at keeps its tab, so a close never
moves you. A closed team spends nothing, is not on the conversations view or the strip, and
waits under `▸ Closed · N`.

Over `--host`, against an engine that does not offer the wrap-up, the card says
`Wrap up first is not offered over this connection` and offers `Close now` and `Cancel`.

## Closed teams: reopening and deleting

Open `▸ Closed · N` on the rail and choose a team. The pane shows when it was opened and
closed, its closing report when it closed on one (what was done, what was left, where the files
are, what it spent), and its members, each still a door to its conversation. Two buttons:

- **`Reopen`** (`r`) opens the team again: its members' tabs come back and its manager is
  brought in front. A team whose parent is closed too offers **`Reopen harbor too`**, because a
  sub-team cannot be open under a closed team.
- **`Delete…`** (`d`) asks first, then forgets the team, its Traffic and its decisions. Its
  conversations stay in your history. Only a closed team can be deleted.

## Organize closes quiet teams

`✦ Organize` on the rail (or on the conversations view) suggests teams for your conversations,
and when some teams have had no activity for a week and nothing waiting, it also suggests
**`Close 3 quiet teams`**, ticked like every other suggestion. Nothing closes until you Apply,
and `Undo` on the Teams row for a few seconds after reopens them. Organize never closes a team
by itself. Over `--host` this suggestion is not offered yet.

## With no teams yet

The page says what a team is in one sentence and offers two buttons: **`✦ Organize my
conversations`**, which suggests teams from the conversations you have open, and
**`+ New team`**. `o` and `n` press them.

## Over --host

Over `--host` the page shows the teams of the machine the conversations run on: their
decisions, their spend and their managers. The **Teams** tab of `/settings` there shows this
computer's rows and says `on <machine> the teams inherit that machine's Settings`; it does not
edit them, because no team you are looking at reads them. A closed team's report is not read
over the connection yet, and the page says so where the report would be.

## Why the page looks the way it does

- **Marks appear only when something happens**, so a glance down the rail finds the one team
  that needs you. Amber means a person is needed, and nothing else on the page is amber.
- **The pane is the manager's own conversation**, not a copy of it, because talking to the
  team means talking to its manager. Everything you could do in that conversation you can do
  here.
- **Choosing a team is the one thing that changes which conversation is in front.** Nothing
  else on the page moves you, and resuming a member opens it behind.
- **A team moves by choosing where it goes**, not by indenting it: the rail keeps teams in the
  order they were made, so a move changes one team's place and nothing else's. A drag adds a
  chat and never removes one, so a slip of the pointer cannot lose a conversation from a team.
- **`m` is Move into…, so starting a manager is `M`.** The move is the everyday gesture; a
  manager is started once per team.
- **A team is deleted only once it is closed**, so the everyday gesture is a close you can undo,
  and the one that forgets things asks first.
