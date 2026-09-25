# Conversations and teams: every open conversation at once, grouped, and one day managed

Written 2026-09-24 against draft PR #1429 (`feat/conversation-overview`, head `c520c363c`).
Shaped with the owner over 2026-09-23 and 2026-09-24. Sections 1 to 5 describe what is
built, the manager's first version included; section 7 is what is deliberately left for
later.

## The problem, in the owner's framing

A person running codeaf the way it is meant to be run has many conversations going at once:
several agents working, one waiting on an answer, two finished an hour ago. The tab strip
says which conversations exist and nothing about what they are doing. The switcher says what
they were called. Nothing lets a person see the swarm working, move between it quickly, or
group it by the work it belongs to. And nothing coordinates it: the person is every
conversation's manager, carrying facts from one chat to the next by hand.

## 1. The wall: every open conversation, live

A full-frame grid of tiles, one per conversation open in this window, each drawing the live
tail of its transcript.

**What it shows is what is open in this window** (ruled 2026-09-24). The wall and the tab
strip are the conversations this window has open, narrowed by a team when one is shown; they
are not a view of the team. The title says so: `Conversations · open in this window`, and
`· in test` while a team is shown, with `1 open` on the right. A shown team's members that
this window does not have open are neither tiles nor tabs. While there are any, the title
carries one quiet word button, `2 more in test · Open them` (key `r`, hint `Resume the 2
not open here · r`), which resumes them behind
the conversation in front, on the door line, off the loop, so they arrive as tiles and tabs
without moving the front or the focus. When every member is open there is no mark at all.
The Teams row counts open members; its hint says the team's size (`test · 1 open here · 3
members`). The first build drew every member on the strip while the wall kept only the open
ones, and the owner's screen read `test 1` over three tabs. A team's whole membership, open
or not, belongs to the teams page on home (section 7).

**Doors in.** `alt+v`, `/wall`, the dock under the input box, and `▦ All` beside the tab
strip's `+`. The dock is a one-row map of every open conversation coloured by state, led
by `▦ All`, the strip's own door spelled the same way and drawn as one button (one hover
ground over the glyph and the word). It used to lead with a dim `chats`, which is the
nav's word for the way back to the conversations: one word on one frame led two places,
so the dock now says what it opens. A click on a square switches to that conversation
without opening the wall. A click anywhere on `▦ All` opens the wall. The
pointer explains the piece it rests on: `Go to <title> · running · click` (or `waiting on
you`, or `idle`), `<title> · you are here` on the square in front, and `The grid of your
open tabs, and your teams · alt+v` on both `▦ All` doors: with no team yet it is the only
door on the strip that leads to making one. The word `All` is the first thing dropped when
the row is too narrow. The squares stay, then fewer of them, then the dock is not drawn. Amber is only
the square that is waiting on you.

**A tile, in reading order.** Title, then state, then now, then history:

- the top border carries the team dots and the title, the brightest thing in the tile;
- a meta line: `⠿ running bash · 2m`, `? waiting on you · 3m`, `updated 14m ago`, or
  `seen 4m ago` for a snapshot this window cannot refresh;
- one blank row, then the body, drawn with the chat's own pieces (the person's words under
  `›`, replies through the transcript's markdown and chroma, tool calls on the rail);
- the body fades with age through the depth-fade ladder (`depthfade.go`): the newest rows
  keep full ink, older rows step down, so a wall of six conversations is not six walls
  asking to be read at once;
- the bottom border carries the activity sparkline at rest and, on hover or focus, a row of
  word buttons: `Open ↵  Select ␣  Teams m  Close x`.

**The state ladder, one step per claim.** Rest is a dim rounded border. Hover lifts the whole
tile onto the hover ground. Keyboard focus is weight (a heavy border), never colour.
Selected is the selected ground and a `☑`. Needs-you is an amber border, the only amber on
the screen.

**Clicks are the familiar ones.** One click on a tile opens it (Mission Control, browser tab
overviews). Selection is an explicit mode entered from a tile's Select button; in it a click
toggles. The wheel scrolls the view by one tile row per notch, clamped, and never moves focus
unless focus would leave the screen.

**Discovery.** Every control explains itself in the toolbar while hovered
(`Add this conversation to teams · m`). The sentence outranks the buttons: while it shows,
`Columns` and then the acts on the right step aside until it is whole (never the button under
the pointer, never `Help`), and come back when the pointer leaves. Before, it was cut between
`Back` and the buttons and read `Resume the …` at 80 columns and lost its key at 110. `?` opens a sheet of everything the page can do,
every row clickable. Next-needing-you is `n`.

**Sizes.** A tile is at least 44 columns and 12 rows; past that the grid scrolls rather than
squeezes. Columns follow width (one under about 100, up to four).

**Motion.** Tiles appear row by row when the wall opens (45ms apart, 135ms in all); opening
a tile switches at once and the new conversation grows out of the tile's rectangle over
about 100ms. Both are off on the linear tier, on ASCII and over a remote link, and any key
finishes them. Hover is instant.

**The laws it keeps.** The frame never reads disk: tails are read off the loop on a stir or
a tick and cached against a version; the painter draws the cache (`framedisk_law_test.go`).
Tab order never moves: the wall's order is the strip's. Closing a tile closes a view and
never ends work. The emptiness law: idle draws no mark. Over a shared engine handle only the
front conversation is live; the other tiles are snapshots and say so.

## 2. Teams: groups a conversation can belong to many of

**The word.** Team. "Space" read as a place with one occupant; "Folder" is already the word
for the directory a conversation works in (`/folder`, `choosing-a-folder.md`) and would have
meant two things on one screen; "Team" fits the manager that is coming.

**The model** (`teams.json`, version 2, resolved through `config.ProfilePath`):

- a team has a stable random `id`, separate from its `name`; everything refers to a team by
  id, so a rename or a reorder never retargets anything;
- `members` are conversations, and a conversation may be in any number of teams, because
  work belongs to several contexts at once;
- `parent` makes a tree: one parent per team, loops refused, deleting a team moves its
  children up; one parent keeps a single chain of authority for managers;
- `manager` is reserved, a member's conversation key;
- unknown fields survive a load and a save, so later features extend the file safely;
- an empty profile directory is the ordinary launch and resolves to the process's own
  profile (`emptyprofile_test.go`); the first build read it as "keep in memory", and no team
  outlived the window it was made in.

**Colour.** Generated, not picked from a fixed list. OKLCH, farthest-point assignment (each new
team takes the hue farthest from every hue in use), with bands of ±25° kept clear around every
hue that already means something (amber, running green, error red, the accent blue), measured
from the live ramp. Two lightness tiers alternate after the sixth team. Stored as a hue in
degrees; rendered truecolor, nearest xterm-256, or the team's initial where there is no colour.

**Naming, once, from what is inside.** The shared project folder's name when there is one;
otherwise one cheap background call over the titles (`Agent.NameTeam`, the conversation-title
path); a curated word while it runs. Typing wins. A team never renames itself: people find
things by the name they remember.

**Where teams show.** Dots before a tile's title; a segmented Teams row on the wall; when a
team is shown, a `● name ▾` chip on the tab strip and the rule under it in the team's
hue. The strip reads `● name ▾   ◆ Manager   tabs…   +   ▦ All`: the chip filters the tabs so it
stands first, right before them, and the manager's place follows it; as the row narrows the
chip goes, never the tab in front. The strip's `home` piece is gone: home is the first word of
the nav on the row above (the top nav, below). Showing a team narrows the strip to its members open in this window, plus the tab in
front (section 1). The chip is the switcher: teams, All, add or remove this conversation, new team, team
settings. New conversations started while a team is shown join it.

**The way back from the places.** The places bar reads `home  teams  chats  sessions  spend
settings` (the owner's order, ruled 2026-09-24; `sessions` is the tasks place's word), then
standing, memory and search off the bar. The list is data (`placeOrder` in `pages.go`), and
the digits follow it: teams `alt+2`, chats `alt+3`, sessions `alt+4`, spend `alt+5`, settings
`alt+6`, standing `alt+7`, memory `alt+8`, search `alt+9`. `chats` is not a room: a click, its digit or `enter` on it returns to the conversation
in front, or opens a new chat when none is open. It is the lit word inside a conversation, `tab`
steps over it, and `alt+k` stays the switcher that chooses a conversation. Its hint says
`every conversation, one at a time`, and `▦ All` says `The grid of your open tabs`: the place
and the grid are two doors, one row apart, and they say so.

**The top nav (ruled 2026-09-24, replacing "one top bar").** The places move onto the
wordmark's row and the strip is on every page, so the head is the same four rows everywhere:

```
 >● codeaf   home  teams  chats  sessions  spend  settings      3 moving · $1.20  thu 10:31pm
   ● harbor ▾   ◆ Manager ×   Refactor the rail… ×   openrouter price scrape ×   +   ▦ All
──────────────────────────────────────────────────────────────────────────────────────────
```

- Row 0 (`navRow`, topnav.go): one cell of inset, the wordmark, two cells of air, then the place
  words as buttons that touch (each ` word ` with a one-cell pad either side, so two blank cells
  between two words), at least two cells, the pulse, one cell of inset. The gaps never change
  with the width. The current place is lit in the accent (`chats` in a conversation, on the wall
  and on the work tab); the rest are muted; on a no-colour terminal the lit word wears brackets
  in its pads. The hover ground covers the pads, the press target is exactly that ground, and a
  terminal that cannot show a ground puts `·` in the leading pad. The hint line says
  `alt+N word · what it opens`, read off each place's `about()`.
- Row 1 (`tabStripRow`): the chat strip on every page. On a place no tab is lit, a press on a
  tab (or `+`, or the manager's place) leaves the place for that chat, and the chip and
  `▦ All` open their menu and the grid over it. The strip keeps its own narrowing.
- The ladder on row 0, in the owner's order: the clock (and `on <machine>` over `--host`), then
  `moving`, then the words of `2 want you` shorten to `2 ?` (same count, same amber, the mark
  a waiting tab wears), then the allowance, then the trailing places fold one at a time into
  `more ▾`, then the day's figure. The needs-you count never folds: it was allowed to for one
  wave, and at 80 columns a conversation's top line then carried no count of what was waiting
  on the person, the one fact a glance at that row is for. At 80 the row is all six places
  and `2 ? · $1.20 / $20`. The wordmark, the lit place, the
  bar cursor's word and a counted place never fold. `more ▾` opens a small menu of exactly the
  folded places with their keys (navmore.go): hover ground, enter or click goes, esc or a press
  off it closes, and focus never moves anywhere else.
- Measured on Spark after the change, with a conversation of three tabs in team harbor: at 80
  columns all six places fit beside `$1.20 / $20`; at 60 `spend` and `settings` fold into
  `more ▾` beside `$1.20`; at 110 the counts and the money fit and the clock does not; at 160
  everything fits. The scroll frame's allocation count fell from 229 to 210 under the 230
  ceiling, because the row is memoised whole (`navMemo`), pulse included.
- `TestTheHeadIsTheSameFourRowsOnEveryPage`, `TestOneTopNavOnAChatAndOnAPlace` and
  `TestTheTopLineGivesThingsUpInTheOwnersOrder` pin the rows, the cells and the order.

## 3. Organize: one button, a proposal, never a silent change

On the All view, `✦ Organize` (key `o`), with a quiet count when five or more conversations
are in no team. It opens a card of proposals, each with a checkbox: new teams, and additions
to existing teams. Proposals come from shared project folders first (exact, free), then one
cheap model call for themes across folders (`Agent.ProposeTeams`, 10s timeout). Apply saves
once and offers Undo for six seconds. Running it again is the refresh: it only ever proposes
new teams and additions, and never renames, removes or moves what the person made. It never
runs by itself.

## 4. What was argued and settled

| Proposal | Ruling | Why |
|---|---|---|
| One team per conversation (Arc) | Rejected | Work belongs to several contexts; managers need that |
| "Folder" as the word | Rejected | Collides with the working-directory folder |
| AI that reorganizes on its own, or a typed "organize like…" box | Rejected | Reshuffling breaks the positions people learned; the chat is already the place to type |
| Teams that rename themselves as they grow | Rejected | Stable names are how people find things |
| Glyph controls in tile corners | Replaced by word buttons on hover | Nobody could tell what `●+` did |
| The wall's door only at the top | Added the dock under the input | Where the hands are |

## 5. The manager (ruled and built 2026-09-24)

**The goal.** Today the person is every team's manager. The manager chat takes the
coordination load: one place to talk to a whole team, run by something that knows what every
member is doing, and a place where members coordinate with each other in the open.

**It is a special conversation.** It is recorded in the team's `manager` field, one per team,
and removing it returns it to an ordinary chat with its history. What makes it special:

- it is the team's first tab, pinned at the left like a browser's pinned tab (`◆ Manager`),
  and the pinned first tile on the wall (`◆ Manager · <title>`); until one exists, that tab is
  a `+ Manager` placeholder, and nothing costs anything;
- its layout is split: the person's conversation with the manager on the left, a **Traffic**
  rail on the right. Whatever the person types goes to the manager, always, and the composer
  says so (`to ◆ manager`, on the box's rule once there is text; on the teams page
  `to ◆ harbor manager`). Both read the team's name when they are drawn, so a rename
  does not leave the old name in the box. Traffic is drawn as
  threads (below, **Traffic is threaded**). With the manager in front the
  right-hand column is the Traffic (ruled 2026-09-24): the task column is not drawn and not
  reserved whatever the saved `ctrl+g` answer says. When the manager has live tasks the
  header reads `Traffic · Tasks 2`, and Tasks (a press or `ctrl+g`) lays them in the same
  column at the same width, and back; with none there is no word. The rail
  is put away with `hide alt+l` to a `Traffic` edge that counts what arrived, and on a narrow
  window it is that edge and a card laid over the lower conversation;
- each turn it carries a small team digest (members, handles, states, questions waiting,
  files touched, recent traffic), never whole transcripts;
- its messages reach members marked `◆ from manager`, never as if the person had typed them;
- nothing it does moves the person's focus: a member it starts opens behind the conversation
  in front, as a tab named `@handle`. With the team's auto-wake on it takes its first turn on
  its own (the session sees itself started and wakes), reading the brief as the manager's.
  With auto-wake off the member is still opened and the brief still waits for its first turn,
  but no turn is started; the Traffic says `opened @handle; this team's auto-wake is off, so
  no turn was started. It reads the brief when it next runs.`

**Why not one stream of chat bubbles.** Members do not all read everything; each receives only
what is addressed to it. A shared stream would look like a group chat and teach the person
that every member saw what they typed. The split puts the person's conversation where it is
unambiguous and makes member traffic a log of who told whom.

**Authority.** The person's words in a member's own chat, then the manager's directives, then
other members' messages. A directive never overrides the person; a conflict comes to them.

**Tools.** The manager has every ordinary tool under the same approval rules as any chat: a
rule against editing could not be enforced once it has bash, and a manager that cannot run a
script to fetch data is crippled. What replaces the rule is visibility: its edits show in
Traffic like anyone's, and its brief tells it to hand real work to members. Team tools are
split read from write, because the approval gate keys on tool names:

| Tool | Who | Default |
|---|---|---|
| `team_status`, `team_read` | manager | allow |
| `team_send` (to a handle or everyone; note or directive) | manager | allow |
| `team_stop` (ends a member's current turn, logged in Traffic) | manager | allow |
| `team_start` (a new member chat with a brief and handle) | manager | ask |
| `team_post` (to the room, a handle, or the manager) | members | allow |
| `team_raise` (a conflict, to the lowest manager above every party; 8.9) | members and managers | allow |

`team_stop` exists because a message only lands at a turn boundary: a member stuck in a long
tool call reads nothing until it returns, and a directive is advice a model may misread. Stop
is the person's own Stop: it ends the current turn, deletes nothing, and leaves background
tasks and jobs running.

**What it may not do.** Approve members' permission prompts. Those are the person's safety
gate, and a manager that could answer them would make every approval rule meaningless. If
that is ever wanted, it is a separate, explicit per-team setting.

**Handles.** Titles are too long to address, so each member gets a handle: ONE lowercase word
naming what the conversation is about (`@security`, `@milestones`, `@gravity`). A word list
cannot do this well; measured on the owner's team it made `@review`, `@reviewing` and
`@session` of "santosh dev2 branch code complexity & security review", "CodeAF repo issue
tags & milestones" and "quantum gravity research updates / session monitor". So:

- the word list (`teams.DeriveHandle`) is the instant guess, made when the member first has
  a title, so it is addressable at once;
- the title model chooses the word when the conversation's title is made
  (`internal/session`'s `handlepick.go`): one call on the title role, a few tokens, asking
  for the subject and two alternates. A timeout or a network failure is asked once more
  after a short wait; a refusal is not, and the word-list guess stands. Still one question
  per title. Over `--host` it runs on the engine, which owns the model and the store;
- `teams.File.ChooseHandle` writes it under the store's lock: the first free word, else the
  first with a title word in front (`@api-security`), numbered only when nothing else fits;
- `Member.HandleBy` records who chose (`words`, `model`, `typed`); a handle a person or the
  manager gave (`team_start`'s) is never replaced, and a model's word is not chosen again;
- one time, every existing guessed handle is chosen again the same way on its conversation's
  next turn, and every rename is a Traffic event from `system` to `everyone`,
  `@review is now @security`, told to the manager and every member at their next step; the
  manager's role note is rebuilt when the teams file moves, so it shows the new handle.

**Every team reference in a chat is a door.** An `@handle` of a member of any team the
conversation is in, and a team's name written as a team (`team test`, `the test team`,
`"test"`), are links in the model's prose, the surface's notes, a team's quoted cards (the
brief included) and a team tool's call rows (`team_send @security`). They go through the
task link's own pass (`markdown.go`, `teamlink.go`): columns recorded on the row, the press
resolved before the row's own answer, the hover held as (block, ordinal) with team
references numbered from their own offset. The hover is a ground; the hint line says
`Open @security · santosh dev2 branch… · click` (`Resume` when it is not open here). A press
opens the member through the strip's door, resuming it first; a team's name opens the teams
page with that team selected (the rail's cursor on its row, the pane showing it, the keyboard
left on the row). A closed team is selected inside `Closed`, and that fold is opened. The
hint is `Open harbor on the teams page · click`. Over `--host` against an engine without the
teams doors (`teamsOff`) the page cannot open, so the press still opens the wall on that team
and the hint says the conversations view. They are resolved from memory only; an `@word` that is no member's handle stays
text.

**Where it lives.** The manager is an ordinary session file. Traffic is
`<profile>/teams/<id>/traffic.jsonl`, append-only, rotated at 4 MB with one old file kept;
the team id is its channel. The store is `internal/teams`, shared by the UI and the tools:
every write is a read-modify-write under a file lock (`Update`), so neither side overwrites
what the other wrote.

**Whose profile.** The store is the one in the profile of the machine the SESSION runs on,
because that is where the team tools write it. The UI never opens it directly: it asks
through a seam (`tui3.TeamsSeam`: `Load`, `ReadSince`, `Update`, `Traffic`). Locally the
seam wraps `internal/teams` at the profile directory. Over `--host` the door hands a seam
that asks the engine over three wire methods, answered from the engine's own profile
(`internal/remote`'s `wire_teams.go`):

- `Teams.Read(stamp)` answers the file, or `same` when it is still at the stamp the window
  holds. A stamp is one stat (size and modification time; every write moves the time
  forward), so an unchanged answer is a few bytes.
- `Teams.Update(base, teams)` writes the whole list only while the file is still at `base`
  (`teams.ChangeIf`), and answers `stale` otherwise; the window reads again, makes its change
  again on the fresh list, and retries up to three times. A manager or handle the far session
  wrote in between is kept.
- `Teams.Traffic(team, after, limit)` is one log after a cursor, a page at most. The engine
  stats the log before it reads (`teams.Watch`), so a quiet log costs a stat and an empty
  answer.

Every seam call that can wait is made off the update loop: an edit changes what the window
holds at once and is queued, and the queue is written after the message on the door line; the
Traffic clock's turn reads through the seam beside it. The one call on the loop is `Load`, at
an opening, which must not block: locally one small file, over `--host` what is held (the
first read is asked off the loop). The welcome's `Teams` flag says an engine has the doors; an
engine without it gets no seam, and the window turns teams and the manager off with the line
it has always said rather than reading the laptop's file, which the far session never sees.

**Traffic is threaded (ruled and built 2026-09-24).** Measured on the owner's screen, one
question to three members was twelve rows at the bottom of an empty column: the question three
times, three wakes, three replies cut to two words, the manager's wake and two finishings, with
nothing saying which reply answered which question. So:

- **One entry per message.** `team_send` takes several handles (`to: "@agent @checking"`) and
  writes ONE entry, `to: several` with `handles`, or `to: everyone`; delivery asks
  `teams.Entry.Addressed`, so each named member is told once and nobody else is.
- **An answer names what it answers.** `teams.Entry.Answers` is the id of the entry a line
  answers. A member is told each line's number (`◆ directive from manager #42: …`) and
  remembers the last line its manager sent it; its next `team_post` to the manager answers that
  line unless it names another with `thread`, and the events its turn raises (finished, failed,
  asking, the wake that started it, a failure to wake it) answer the same line. `team_send`'s
  answer carries the number too. Entries written before this answer nothing and read as
  threads of their own; the field travels in the entry's JSON, so `--host` needs nothing new.
- **The rail is threads, the newest activity at the top**, straight under the header with no
  space above; inside a thread everything is in the order it happened. A thread is a header
  (`◆ manager → @agent @checking @review  do  2m`, `+2` for handles that do not fit, never the
  tag), the message on its own dim line, and the answers as a tree (`├ @checking  ✓ Status
  update: …  1m`, `└ @review  working…`). Wakes are not rows: a woken member reads `working…`
  until it answers; a finishing is the `✓` on its answer or its own `✓ finished` line, a failure
  `✗`, and a member asking the person is the one line in the needs-you amber. Stops, starts,
  handle changes and unthreaded entries stay one line each.
- **Delegation's entries are threads too** (section 8). A ruling (`teams.IsRuling`) heads its
  own thread as `◆ manager ruling → @web` (or `you ruling`, the one entry of the person's the
  rail draws), the ruling's words under it with the conflict's packet named in their hint; a
  start that made a sub-team reads `◆ manager started @api to run backend`; a member's
  clarifying question is a header tagged `asks`, and the manager's answer (a `KindAnswer`,
  whose `Reply` stands in for `Answers`) is a line of the tree under it; a packet raised,
  decided or answered (`codeaf  answered @web: JSON`), a closing and a reopening are one line
  each, like an event. `team_send`'s directive to everyone, when some members report to
  another team's manager, is one entry to the several who report here. The teams page's
  hosted manager draws this same rail and takes the same jump.
- **Handles are links there too**, inked and grounded as in the chat, hint `Open @x at this
  message · title · click` (`Resume` when not open here), a press opening or resuming the member
  scrolled to the message: a header's handle at the directive as delivered, an answer's handle at
  the member's own post. Every place an entry sits in a transcript carries its number (a
  delivered line's ` #N`, `team_send`'s `(#N)`, `team_post`'s ` as #N`), and the surface finds
  the newest entry carrying it, scrolls it a third of the way down the view and lifts it for
  1.6 s without taking the focus. A jump waits for a conversation still opening, for up to ten
  seconds; a number in no entry opens at the bottom with `that message is older than this
  chat's history` in the hint line. A message's words are a door: hover puts them whole in the
  hint line, a press lays them out under the row and brings its thread card in the manager's
  conversation into view, a second folds them. Rows are cached on the entries, width, pointer, minute and what is laid out.
- **A manager's steps are captioned as team work.** The fold over a manager's turn read
  `▾ team_send 1 call … 1 call`, the tool's own name and the count twice; the team tools now
  gloss like the others: `messaging @scrape @model` (past, `messaged`), `posting to manager`,
  `starting @lexer`, `stopping @web`, `reading @web`, `checking the team`, and a run of sends
  `sending 3 messages`. The tool row under it keeps the tool's name, as every tool row does.
- **The manager's chat has the thread where it asked.** A `team_send` row reads
  `team_send ◆ to @agent @checking @review · do`, the words quoted under it, and each member's
  answer attached under that in muted ink as the Traffic cache brings it, one line each; the
  same press and hover as the rail. A turn with a `team_send` in it is not folded into a work
  chip, because its answers arrive after it ends.
- **The member's chat mirrors it.** The manager's line is the quoted card it always was, and the
  member's own answers to it hang under it the same way.
- **No answer is drawn twice.** A member's reply also reaches the manager's model as a delivery
  note, which replays as a team card. In the manager's chat an answer already under its
  question's card is left out of that card, and a card left with nothing is one dim line,
  `· @checking @review answered · in the thread above`. The model's transcript is unchanged; only
  the drawing folds. A delivery inside a wake note is read as one too, so a woken member's
  directive draws as the manager's card rather than a dim line.

**Traffic is the only channel between the UI and the session.** A conversation's identity in
a team is its transcript path, the same key the tab strip uses. The session side writes
messages, stops and starts as Traffic entries and delivers what is addressed to it before
every model request, with a cursor kept in its session folder so nothing arrives twice and a
new member is not handed old history. The UI tails the same log off the loop: it draws the
rail, and it carries out stops and starts for the conversations its window holds, once per
entry. Neither package calls the other for team features.

**Team traffic wakes (built 2026-09-24).** A manager that hands out work and then waits for
the person to type again is not running a team, so the lines that ask for an answer start one:

- A **directive** (`team_send` kind `directive`) to a member's handle or to everyone starts
  each idle member's turn. A **note** wakes nobody and is read at the member's next turn. A
  member already working is not started again; it reads the directive at its next step
  boundary, the steering road it always had.
- A member's `team_post` **to the manager**, and a member's own **finished**, **failed** and
  **asking** events, start an idle manager's turn. They coalesce: the first arms a five second
  settle window, measured from that first line and never extended, and one turn carries
  everything that arrived in it, so members finishing on one burst of work are one thing to act
  on.
- The woken turn is never the person's: it opens on the same marked note a step boundary
  hands over, queued as the session's line with the wake bit, so the spend limit, the wall and
  a stopped session still decide whether it may run.
- The watch lives with each conversation on the engine (`internal/session`'s
  `team_wakewatch.go`), locally and over `--host` alike. It costs nothing while a turn runs,
  one stat of the teams file per second shared by every conversation in the process while
  idle, and one stat of each team log only while idle in a managed team with wake on.
- **A conversation nobody holds is opened headless.** The side that wrote a waking line probes
  the target's journal lock; when nothing holds it, the engine sends a hello naming its
  transcript to the session host of its folder (`cmd/codeaf`'s `team_resume.go`), which opens
  it with no surface and keeps it while it works. A window that opens it later joins the
  running conversation. Where no road exists (a `--no-host` or `--once` process, no folder
  recorded, a host that will not start), the Traffic says `could not wake @x: <reason>`.
- **Limits.** One conversation is woken at most 20 times an hour (`teamWakesPerHour`). A
  manager woken 10 times by its team with no word from the person (`teamLoopRounds`) stops being
  woken and asks the person instead: an asking event from the manager in Traffic, drawn in the
  needs-you amber, and a note for its next turn. The person's next message to it resets the
  count. Wakes spend through the ordinary budgets.
- **Visibility.** Every wake is a Traffic event, `◆ woke @web` and `@web woke ◆`, and every
  refusal is one too. The rail draws a member's wake as `working…` in the thread it answers and
  does not draw the manager's; a refusal is the member's line in that thread.
- **Off switch.** `wake` is one of the inheritable team settings (8.2): a team's own `wake`
  in `teams.json` (a file from before it was inheritable wrote only `"wake": false`, which
  reads unchanged as an override to off), else the nearest ancestor's, else the profile's
  `teams.wake` (on). The settings tab's Teams group carries the default as its own row, `team
  messages wake`. With it off, a directive, a reply and a
  `team_start` start no turn: the new member is opened and reads its brief on the first turn
  something else starts, and the Traffic says `opened @handle; this team's auto-wake is off, so
  no turn was started.`

**Mentioning a team or a chat from the composer.** `@` is still the one list
(`internal/tui3`'s `files.go`, `mention.go`). Its first row is the words team, chat
and file, each a press that types `@team:`, `@chat:` or `@file:` and keeps that
section. The word under the pointer takes the cursor ground, and the hint is
`only teams · click` (or conversations, or files). Typing filters every section that
is showing. Argument completion (`/image `, `/export `, `/attach `) stays files only.

Under the words: teams from the window's in-memory list, a colour dot and the name;
then conversations, open tabs in this window first and then the recent snapshot the
door already holds, loaded once inside a command; then the task sections; then
files. A prefix hides the other sections, including tasks. A conversation in no team
is still offered. The conversation in front is not.

Choosing a team replaces the `@` token with `●` and the team's slug (`●harbor`),
drawn in that team's colour. The runes are the token, so the caret's column does not
move. Choosing a conversation keeps `@` and writes the handle, or `TaskSlug` of the
title when the conversation has no handle. The row's note is the title.

After send, the same link pass inks those tokens on the person's own message
(`mentionLinkPass`). A `●slug` opens the teams page on that team, the same door a team's
name uses, including the wall fallback over `--host` when the engine has no teams doors.
An `@handle` or `@slug`
of a conversation this window can name, including one in no team, opens it through
the tab strip. Model prose keeps the older door: an `@handle` of a member of a team
this conversation is in, and a team name written as a team.

The digest is built on the engine (`internal/session`'s `mention.go`), inside
`Submit` and before the lock, so `--host` works and the frame never reads it. The
journal stores the person's words (`user.said`). The model reads those words plus
one block per reference: a team is `teams.Digest` at 800 runes (members, handles,
states, recent traffic); a chat is its title, its state and an excerpt of the last
reply, cut at 1536 runes. The read is `teams.Load`, `teams.ReadTraffic`,
`journalState` and `Peek`. It does not call `teamRouse`, `AppendTraffic` or
`Submit` on the conversation it names. A token with a slash is a file path and is
not a chat. An unknown `@word` is left as text. A standing mark does not take this
road; `Submit` does, and steering goes through `Submit`.

**Known limits of v1.** A stop or a start takes effect only in a window that holds those
conversations. A member waiting on a permission prompt shows as running off its journal alone,
because the prompt is not in its session file; its asking event says `asking` while a process
holds the transcript lock and the event is under 30 minutes old (`askingStaleBound`). When the
lock is free, or the event is older than that, it reads idle. The loop breaker's needs-you is
the Traffic's asking row and a note, not a question on the manager's tab. Over `--host` against an engine older than the teams doors, teams and the manager
are off and say so. An unreadable teams file on the engine is not moved aside from a window over
`--host`; the window holds no teams until it can be read.

**Not in v1.** Collision flags when two members touch the same files, and dispatch of whole
plans. (Nested managers, a sub-team's manager a member of the parent team with reports flowing
up and directives down, were built later: 8.9.)

## 6. The laws this design leans on

- The frame reads no disk; readings happen on openings, stirs and ticks, off the loop.
- The update loop starts no process of its own; model calls are commands.
- Tab order never moves; a close is a view, never work.
- One accent on the screen; state claims one step of the ladder each.
- An empty profile directory is the ordinary launch.
- A block's live edge settles through `livestate.go`; nothing else names a field `edge`.

## 7. Later, each needing its own go

- Agent tools for teams from any chat ("put the nvda chats in a team"), through the same
  store, which over `--host` is the engine's (section 5, "Whose profile").
- A Teams place on home showing the tree and each team's whole membership, open or not;
  nesting in the UI; drag a tile onto a team.
- The manager waking on events, collision flags, nested managers, dispatch.

## 8. Delegation (ruled 2026-09-24; store and contract built, session and screens next)

**The goal.** The person talks to the top manager and steps away. Work is handed down a tree
of teams; questions and decisions travel up it; only what no manager may or can decide reaches
the person, and it reaches them as a card they can answer without reading a transcript.

This section is the contract two builders work from at once: the session side (the team
tools, delivery, caps and wrap-up, in `internal/session`) and the interface side (the teams
page, the settings tab, the cards, in `internal/tui3`). Both meet only in `internal/teams`
and its Traffic, as section 5 already requires; everything named here exists at the
foundation commit and is tested.

### 8.1 The rulings, as built

- **Home and links.** Every conversation that sits in a team with a manager somewhere above
  it reports to exactly one manager, its **home**. The home is auto-picked, stored, never asked
  and never moved by itself: nearest manager up the chain first, else the membership the
  manager's `team_start` made, else the first managed team it joined. Every other manager it
  can be reached by is a **link**: it may read the conversation and send it an fyi, nothing
  else. The person can move the home (`Reports to ◆ harbor ▾`).
- **Questions go up.** With `questions_up` on (the default), a member's clarifying question
  goes to its home manager first, logged in Traffic; the manager answers it or sends it up.
  Permission prompts never go up: they are the person's, always (section 5, "What it may not
  do").
- **Decision packets.** Everything that must be decided above the conversation that met it
  travels as one self-contained packet (question, parties and each side's context, options
  each with its consequence, a recommendation with its reason). The same packet is answered
  by a manager or by the person.
- **Conflicts go to the LCA of all n parties**, in one hop: the lowest open team above every
  party that has a manager who is not one of them. No such team: the person. Parties declare
  a conflict (`team_raise`); nothing detects one.
- **Orders go one level down, reports one level up.** A manager directs its own team's members,
  a sub-team's manager among them; it does not reach past them. Managers of different teams
  talk only through the tree.
- **The optional global manager is the manager of the root.** With several top-level teams
  there is no top manager until the person makes one on the `All teams` row (8.2, "The root").
- **Caps.** A team may have a daily cap; reaching it raises a cap packet to the person.
- **Lifecycle (c-9).** A team is open or closed. Closing is a card (8.5); a closed team is
  folded away, can be reopened, and only a closed team can be deleted.

### 8.2 The store: the exact API

Everything below is `internal/teams` unless named otherwise.

**Per-team settings** (`teamsettings.go`). Five optional overrides, stored flat on the team in
`teams.json`, each unset meaning inherit:

| teams.json field | Go | Meaning | Band |
|---|---|---|---|
| `questions_up` | `*bool` | members' questions go to the home manager first | |
| `cap_usd_day` | `*float64` | dollars per local day for the team and everything under it; `0` is an explicit no cap | `>= 0` |
| `depth_limit` | `*int` | levels of teams, the top counting as one | 1 to 10 |
| `sub_share` | `*float64` | fraction of this team's cap a new sub-team is made with | (0, 1] |
| `wake` | `*bool` | team traffic wakes idle conversations (section 5); the old `"wake": false` reads as off | |

- `type Settings struct{ QuestionsUp *bool; CapUSDDay *float64; DepthLimit *int; SubShare *float64; Wake *bool }`,
  `Team.Settings`, `(Settings).Empty()`.
- `(*File).SetSettings(id, func(*Settings)) error`: set a field to override, nil it to
  reset; out of band is `ErrSetting` and nothing changes. tidy drops an out-of-band value in a
  hand-edited file (it reads as inherit).
- `type Defaults struct{ QuestionsUp bool; CapUSDDay float64; DepthLimit int; SubShare float64; Wake bool }`
  and `DefaultsAt(profileDir) Defaults`, from config.json in one read.
- `(*File).Effective(id, Defaults) Effective`: each value with its `Origin`
  (`QuestionsUpFrom`, `CapFrom`, `DepthFrom`, `SubShareFrom`). `Origin{Kind, Team, Name}`,
  Kind one of `OriginTeam`, `OriginAncestor`, `OriginSettings`, `OriginClosed`;
  `(Origin).Inherited()`, `(Origin).Words()` = `""`, `from Settings`, `from harbor`,
  `closed`. The walk skips closed ancestors; a closed team's own cap is none.
- `(*File).Depth(id) int` (the root is 0, top level 1), `(*File).CanNest(parent, Defaults) bool`
  (parent open and one more level inside its effective limit), `(*File).SubTeamCap(parent,
  Defaults) float64` (parent's effective cap times its share, to the cent; 0 when the parent
  has none). The session writes that figure on the new sub-team with `SetSettings`, so a later
  change of the share moves no team that exists.

**A cap is a pool.** A team's spend counts every team under it, so an inherited cap is the
ancestor's one pool, shared, never a second allowance of the same size. `Effective.CapFrom.Team`
names the pool's owner; when the cap comes from Settings, the owner is the top of the chain
(the root when there is one). The spend drawn beside a cap is always the owner's.

**Config keys** (`internal/config/teamdefaults.go`, flat dotted keys, category `teams`, one
settings tab `Teams`, each a row with a named reader `TeamDefaultsAt` and a ledger line):

| key | kind | default | registry label |
|---|---|---|---|
| `teams.questions_up` | on/off | on | questions go to the manager |
| `teams.cap_usd_day` | dollars, `no cap` at 0 | 0 | team daily cap |
| `teams.sub_share_pct` | whole %, 1 to 100 | 50 | sub-team share |
| `teams.depth_limit` | levels, 1 to 10 | 3 | team depth |
| `teams.wake` | on/off | on | team messages wake |

All five are guarded from model self-service (`teams.wake` as pressure: a wake starts a turn
nobody typed), the first four (`selfservice.go`): a manager is a model, and
each is a rail on managers (money, pressure, consent).

**Home and links** (`home.go`).

- `Member.Home bool` (`home`): the one membership whose chain names the manager the
  conversation reports to. `Member.Started bool` (`started`): the membership a manager's
  `team_start` made; the interface sets it when it carries out a `KindStart`.
- `(*File).Home(key) (Report, bool)`, `(*File).Links(key) []Report`,
  `(*File).SetHome(key, teamID) error` (`ErrNoManagerAbove` for a chain with no manager).
  `Report{Via, Team, Manager, Distance}`: the membership's team, the managed team, its
  manager's key, levels between.
- tidy (every load and write) gives every conversation with something to report to exactly
  one flag and takes it from any other; a valid flag is never moved. The pick order: nearest
  manager (own team before one a level up, then the deeper team), then `Started`, then file
  order (which is join order at the first write that gives a conversation a manager).
- A manager is never its own home: a sub-team's manager reports one level up.
- The flag is stable; the manager at the end of its chain is whoever the person made manager
  there. A conversation in an unmanaged sub-team reports to the manager above; when the
  person gives that sub-team a manager, the same flag resolves to it. That is the person's
  act, not a home changing by itself, and a sub-team manager nobody reported to would manage
  nothing.

**LCA** (`home.go`). `(*File).LCA(keys...) (Team, bool)`: the deepest open team at or above
some membership of every key, with a manager who is not one of the keys (a party never judges
its own case). false is the person. One key gives its nearest manager.

**Lifecycle** (`lifecycle.go`).

- `Team.State` (`state`: `TeamOpen` or `TeamClosed`; empty reads open), `Team.ClosedAt`
  (`closed_at`), `Team.ClosedWith` (`closed_with`: the team whose close closed it),
  `Team.Report` (`report`: the closing report's packet id). `(Team).Closed()`.
- `(*File).Close(id, at, reportID) error`: closes the team and every open team under it;
  a sub-team closed earlier on its own keeps its own close. Closing the root is `ErrRoot`.
- `(*File).Reopen(id) error`: reopens the team and exactly the teams its own close closed;
  under a closed parent it is `ErrParentClosed`.
- `(*File).Open() []Team`, `(*File).ClosedTeams() []Team` (newest close first),
  `(*File).Descendants(id) []Team`.
- `Delete(profileDir, id) ([]string, error)`: only a closed team (else `ErrOpen`), with every
  team under it; the file first, then `TeamDir(profileDir, id)` (Traffic and packets).
  Conversations are never touched.
- `Quiet(profileDir, f, now, idle) ([]string, error)`, `QuietAfter` (7 days): open teams with
  no Traffic, packet or member-transcript activity for idle and no packet waiting on or raised
  from them. Organize's input; it closes nothing.
- A closed team is outside every walk: never a home, never found by `managerUp`, never an LCA,
  skipped by `Effective`, no cap. A conversation whose home closes is given the next home on the
  same write, or none (it is then an ordinary chat).

**The root** (`root.go`). `Team.Root` (`root`), `RootName` (`All teams`),
`(*File).Root() (Team, bool)`, `(*File).MakeRoot(at) string` (moves every top-level team under
it; then `AddMember` and `SetManager` in the same write), `(*File).DissolveRoot()`. tidy keeps one
open root at the top and puts any later top-level team under it. The root is not a level
(`Depth`), cannot close or move (`ErrRoot`). Its override is what `from All teams` means.

**Decision packets** (`decision.go`). One file per team, `<profile>/teams/<id>/decisions.jsonl`,
append-only JSON events (`raise` with the whole packet, then `decide` and `escalate` naming it by
id), folded by id. A packet lives in the file of the team it was raised from and never moves;
escalating changes who decides and adds a hop. Writes are under the file's lock and re-check the
fold under it; reads are stat-first and read only the bytes appended since the last read.

```go
type Packet struct {
    ID       string          // "p" + 12 hex, minted by Raise
    Team     string          // who decides now: a team id (its manager), or Person ("you")
    Origin   string          // the team it was raised from; its file holds it
    Kind     string          // question | conflict | cap | judgement | closing
    RaisedBy string          // raiser's handle, "manager", or "you"
    Parties  []Party         // {Key, Handle, Team, Context}: each side in its own words
    Question string
    Options  []Option        // {ID, Label, Consequence}; ID defaults to "1", "2", ...
    Recommendation *Recommendation // {Option, Reason}, optional
    Report   *ClosingReport  // {Done, Left, Files, SpendUSD, Incomplete}, closing only
    State    string          // open | decided | escalated (escalated still waits, at Team)
    DecidedBy string         // the deciding manager's handle, or "you"
    Decision string          // an option id, or the person's own words
    Reason   string          // the decider's, or the last escalation's
    Trail    []Hop           // {From, To, By, Reason, At}, oldest first
    Raised, At time.Time
}
```

- `Raise(profileDir, Packet) (Packet, error)`. Required: a kind, the question, the raiser, and
  on every option a label and a consequence; a question may have no options, every other kind
  needs one; a recommendation names an option and gives a reason. `Team` is found by the caller
  (`LCA` for parties, `Home` for a member's question) and must be open; `Origin` defaults to
  `Team` and must be `Team` or under it (`ErrSideways` otherwise).
- `Decide(profileDir, id, by, decision, reason) (Packet, error)`. `by` is the handle of the
  manager the packet waits on (or `"manager"`, recorded as the handle), or `Person`, who may
  decide any packet. `ErrNotDecider`, `ErrDecided`, `ErrNoPacket`.
- `Escalate(profileDir, id, by, to, reason) (Packet, error)`. `to` is an open team strictly
  above the one it waits on, or `Person`; down, sideways, closed or past the person is
  `ErrSideways`.
- `OpenPackets(profileDir, scope) ([]Packet, stamp, error)`: waiting packets for a team id,
  `Person`, or `ScopeAll` (`""`). `Packets(profileDir, teamID)`: a team's whole history,
  decided included (the closed view and its reports). `PacketByID`. `PacketsStamp(profileDir)`:
  one stamp over every packet file (a directory listing and a stat per team).
  `DecisionsPath(profileDir, teamID)`.
- Option ids the two sides act on: `OptionClose` (`close`), `OptionCloseNow` (`close-now`),
  `OptionKeepGoing` (`keep-going`), `OptionRaiseCap` (`raise`), `OptionStopToday` (`stop`).
- Every raise, decision and escalation also appends a `KindPacket` Traffic entry to the origin's
  log and to every team asked to decide it, so a window tailing Traffic learns of it without
  polling, and a manager's session is handed it by ordinary delivery.

**Spend** (`spend.go`). The usage ledger (`<home>/v3/usage.jsonl`, internal/session's
`usage_ledger.go`) already records every call's local day, cost, conversation id (`session`)
and, for work a conversation started, that conversation's id again (`root`). A member's id is
its transcript folder's name. So:

- `TeamSpend(profileDir, teamID, day) (Spend, error)`: the day's lines whose session or root
  is a member of the team or any team under it, each line once, each conversation once.
  `Spend{Team, Day, USD, Calls, ByMember map[key]float64}`. `TeamSpendIn(profileDir, ledger,
  ...)` for tests. `Today()`, `UsageLedgerPath()`, `SpendStamp(profileDir, ledger)`,
  `TeamSpendStamp(profileDir, teamID, day)`.
- The ledger is folded once per process into totals by day and (session, root), then only
  appended bytes are read; a quiet ledger costs a stat. `internal/session`'s
  `TestTeamSpendReadsTheSessionsLedger` pins the path and field names.
- A sub-team closed today still counts toward its parent's day: the money was spent.
- Nothing here enforces a cap; the session does (8.8).

**Traffic kinds added** (`traffic.go`):

| Kind | From → To | Meaning |
|---|---|---|
| `question` | member handle → `manager` | a clarifying question to the home manager |
| `answer` | `manager` → member handle | the answer; `Entry.Reply` is the question's entry id |
| `packet` | `system` → `manager` | a packet was raised, decided or escalated; `Entry.Packet` is its id, `Entry.State` its new state |
| `close` | `you`, `manager` or `system` → `everyone` | the team was closed |
| `reopen` | `you` → `everyone` | the team was reopened |

`ToYou` (`you`) is added as an address for the person.

**Over `--host`** (`internal/remote`'s `wire_delegation.go`, `delegation.go`). Seven additive
methods, answered from the engine's profile and ledger: `Teams.Defaults`, `Teams.Packets(scope,
stamp)` (answers `same` in a few bytes), `Teams.Raise`, `Teams.Decide`, `Teams.Escalate`,
`Teams.Spend(team, day, stamp)` (answers `same`), `Teams.Delete(team)`. `Welcome.Delegation`
says the engine has them. Close, reopen, overrides, homes and the root are edits to the teams
file and cross by `Teams.Update` like every other edit.

**The seam** (`tui3.TeamsSeam`). Beside `Load`, `ReadSince`, `Update` and `Traffic`:
`Defaults`, `Packets(scope, since)`, `Raise`, `Decide`, `Escalate`, `Spend(team, day, since)`,
`Delete(team)`, every one asked off the loop. `localTeams` wires all of them to this profile;
cmd/codeaf's `hostTeams` wires them to the wire only when the welcome says `Delegation`, and an
engine with the teams doors and not these gets a seam whose delegation doors are nil
(`TeamsSeam.delegation()` false). The interface then says the inbox and the spend are not
available over that connection; it never reads the laptop's packet files. The frame law
forbids every new store door in a frame (`framedisk_law_test.go`).

### 8.3 What the session side builds on this (d1)

- **Identity** is section 5's: a session's key is its transcript path; its home is
  `Home(key)`; it manages team T where `T.Manager == key`.
- **Questions up.** When `Effective(team).QuestionsUp` and `Home(key)` exists, a member's
  clarifying question is a `KindQuestion` entry to `manager` in the home team's log. The manager
  answers with `KindAnswer` (`Reply` set), delivered like any message. A manager that cannot
  answer raises a `question` packet to its own home's team, or to `Person` when it has none
  (the top manager's own questions reach the person). Permission prompts never take this road.
- **Conflicts.** `team_raise` takes the parties' handles, finds `LCA(keys...)`, and raises a
  `conflict` packet there (or to `Person`). The LCA manager is woken by the `packet` entry, may
  read and ask the parties, rules with `Decide` and directives to every party (logged in each
  involved team's Traffic), or `Escalate`s up. Never sideways.
- **One level.** `team_send`, `team_stop` and `team_start` reach only the manager's own team's
  members (a sub-team's manager is one). Links may send fyi notes (`KindNote`) and nothing else.
- **Caps.** Before each model request of a member, the session compares
  `TeamSpend(owner, Today())` with the pool owner's effective cap (`Effective.CapFrom.Team`, or
  the top of the chain). Reached: it raises one `cap` packet to `Person` for that team and day
  (not one per request; look for an open one first) with options `raise` (`Raise to $10`,
  twice the cap) and `stop` (`Stop for today`) and a recommendation, and holds new member turns
  in that pool until it is decided. Managers never raise a cap: money is the person's.
- **Sub-teams** (nesting step): `CanNest(parent)` gates `team_start` of a team; the new team
  gets `SubTeamCap(parent)` written as its own `cap_usd_day`, and its manager is a member of the
  parent.
- **Wrap-up** (c-9): when the person picks `Wrap up first`, the manager tells members to finish
  and commit, answers what it can, and raises a `closing` packet to `Person` with a
  `ClosingReport` and options `close` / `keep-going`, bounded by time and spend; out of bound, it
  raises it with `Incomplete` and the option `close-now`. The interface closes the team only when
  the person picks close.
- **Closed** teams spend nothing: a session whose only teams closed takes no team turns.

### 8.4 The interface (d2)

The house rules hold everywhere below: every clickable thing grounds on hover; actions are
word buttons with a dim hint line and a key; one accent on the screen; padding around every
block; nothing moves the person's focus; every panel closes (`esc` and a word); everything is
findable from `?`. Amber is used for one thing only: something needs the person (a packet
waiting on them, a member's permission prompt). A packet waiting on a manager is not amber:
somebody is on it.

**The settings tab `Teams`** (built at the foundation commit, between `Tasks` and `Providers`).
Four rows, in this order, each with the registry's hint:

```
  questions go to the manager   on
  daily cap per team            no cap
  sub-team share                50%
  team depth                    3 levels
```

The tab is titled `Teams` and is the one-to-one reading of the `teams` category, like
Spending, Safety and Tasks. A line under the rows, dim: `a team can override any of these on
its card`.

**The teams page.** A main tab `teams`, right after `home` (the digits after it shift by one).

- **Left rail: the tree.** One row per open team, indented by level, each with its colour dot,
  its name, and at most one mark: `●` working (dim, a member is running), `◆ needs you`
  (amber, a packet or a prompt waits on the person). Above the teams, the `All teams` row: with
  no root team it is the whole list and carries `+ Manager` (the optional global manager; the
  click makes the root, 8.2); with one it is the root team and selects like any team. Below
  them: `+ New team` and `Organize`, word buttons. At the bottom, folded: `Closed · N`
  (8.5). `↑↓` walks the rail, `enter` selects; the selection never moves focus out of the
  composer on its own.
- **Right pane: the selected team's REAL manager conversation** (the full chat of section 5:
  typing steers it, its prompts are approved here), under a compact header:

  ```
  ◆ harbor   @web ● running  @api idle  @docs ◆ asking        $1.20 of $5 today   Settings   Open ▦
  ```

  members with their states (each clickable, opening the member), today's spend against the
  effective cap (`$1.20 today` with no cap; the pool owner's figure with `· harbor's cap` when
  the cap is inherited), `Settings` (the team card), `Open ▦` (the team on the wall). A member's
  permission prompt shows as a needs-you row in the header with `Allow once` / `Always` /
  `Deny`, the person's own gate (a manager never answers it).
- **The inbox.** Above the conversation, the packets waiting on the person or on this team's
  manager, one card each, newest last, folded to one line each beyond three. Hosted, the
  cards also have a height: about a third of the frame (at least 8 rows); the newest, or the
  one a press unfolded, is always whole, older ones stay whole while they fit, the rest fold
  to `▸ kind · question   waiting on you`. Measured at 110x34 with three packets before the
  change: the cards took every pinned row and left the manager's conversation one row, the
  Traffic edge cut to `T`. A card leads with whose it is: `?` in the needs-you amber when it
  waits on the person, `◆` when it waits on a manager (every card led with `◆` before, which
  read as the manager's own question):

  ```
  ? conflict · raised by @web                              waiting on you
  which shape does the signup form send?
    @web   the form posts JSON
    @api   the endpoint takes form data
  [ JSON ]  @api changes the handler; the form stays          recommended: matches the rest
  [ form data ]  @web rewrites the submit; the handler stays
  [ Your own answer… ]   [ Send up ▴ ]
  ```

  Options are word buttons with their consequence as the dim line; the recommended one says
  so beside it; `Your own answer…` opens a one-line box (`enter` decides with the words);
  `Send up ▴` escalates to the next manager up, or to the person (shown only when the viewer is
  a manager's page and there is somewhere up to go). A packet waiting on a manager is shown
  dim with `waiting on ◆ dock`, and the person may still decide it (authority: person first).
  Decided packets leave the inbox and stay in the team's history.
- **No manager:** the right pane is the members (states, open) and one `+ Manager` button with
  the line `a manager takes your messages to the team and asks you only what it cannot decide`.
- **No teams at all:** an explainer (two sentences: what a team is, what a manager does) and
  `Organize` / `New team`.
- **A cap reached** is a `cap` packet, drawn like any card:

  ```
  ◆ harbor reached its $5 cap today                         waiting on you
  [ Raise to $10 ]   harbor and its sub-teams go on until $10 today
  [ Stop for today ] members finish their current turn and start no new one  recommended: …
  ```

  The team's rail row carries `◆ needs you` until it is decided.

**The team settings card** (from `Settings` in the header). Overrides only: each of the four
values on its own row, the effective value in ink when the team overrides it, with `reset`
beside it; an inherited value dim with its origin, `$5/day · from Settings`,
`on · from harbor`, `3 levels · from All teams` (`Origin.Words()`). Editing a dim value makes
it an override. Below the four: `Reports to` for a selected member (its home and `▾` to move
it), and `Close team…`. Closable with `esc`.

### 8.5 Closing, reopening, deleting (c-9)

- **`Close team…`** (card from the team settings card, the rail's context menu, and `?`):
  - with work running: `Wrap up first` (default, the accent), `Close now`, `Cancel`. The hint
    under `Wrap up first`: `the manager asks everyone to finish and commit, then brings you a
    closing report`. The team closes only when the person picks `Close` on the report.
  - `Close now` stops every member turn (the person's own Stop), ends the manager's turn,
    closes the team's tabs and moves it to Closed, in one step with `Undo` on the notice line.
  - with nothing running: one `Close` with `Undo`.
  - Closing a team closes its sub-teams; their reports roll up into the parent's. A sub-team
    closing alone reports to its parent's manager.
  - A conversation that is also in another open team is never stopped; its home moves by the
    auto rule, or it becomes an ordinary chat.
- **The closing report** is a `closing` packet: `done`, `left`, where the files are, what it
  spent, and `Close` / `Keep going` (or `Close now` when the wrap-up ran out of time or money,
  marked `wrap-up incomplete`).
- **`Closed · N`**, folded at the bottom of the rail and of the team switcher, never on the
  strip or the wall. Opening a closed team shows its closing report, members, spend, opened and
  closed dates, and `Reopen` (tabs reopen, the manager resumes) and `Delete…` (confirm; it
  forgets the grouping, the Traffic and the packets; conversations stay in history). Delete
  exists only here.
- **Organize** adds one proposal kind: `Close N quiet teams` (`Quiet`, 7 days without activity
  and no open packet), each named, with `Undo`, never applied without the person. Its row is
  the sentence whole and then the teams (`☑ Close 2 quiet teams  harbor, orbit`): it is not a
  team, so it wears no colour dot and no second count (it read `☑ ● Close 2 quiet t… 2`).
- **A team's cards stand over the pane.** The settings, close and move cards are centred over
  the teams page's pane while that page stands, so the rail beside them still says which team
  they are about; they covered the rail before. Elsewhere they are centred on the frame.

### 8.6 Where this departs from the brief, and why

- **The home flag resolves to the nearest manager up its chain**, so giving an unmanaged
  sub-team a manager takes its members with it. The alternative (store the manager's team,
  never move) leaves a new sub-team manager managing nobody; making a manager is the person's
  act, so this is not a home "changing by itself".
- **`decided_by` is `you`, not `person`**, because Traffic already spells the person `you`
  (`FromYou`, now `ToYou`); one word for one party.
- **The settings group is its own tab, `Teams`.** The settings tabs are one-to-one with their
  categories for the four newer tabs; a group inside Tasks would be a row filed under one
  category and drawn under another.
- **A cap is a pool, and the header says whose.** `$1.20 of $5 today · from harbor` beside a
  sub-team would read as a second $5; the header shows the pool owner's spend and says
  `harbor's cap`. The settings card still says `from harbor`, which is true of the value.
- **Cap packets always go to the person.** A manager that could raise its own cap would make
  the cap advice. Managers may stop their own team early; they may not spend more.
- **The global manager is a real root team**, not a special case beside the tree, so every
  rule above holds for it unchanged; the root is not a level and cannot close.
- **Team defaults are rails**, refused to model self-service like the spending and consent
  rows. A team's own overrides are written only by the interface for the person; the team
  tools must not write them (d1).
- **Packets waiting on a manager are not amber**, only those waiting on the person. The brief
  said "amber only for needs-you"; this is that rule applied to packets.

### 8.7 Open questions

- Over `--host` the settings tab writes this laptop's config.json, while the engine reads its
  own `teams.` defaults (`Teams.Defaults`). Until settings cross the wire, the `Teams` tab over
  `--host` should show the engine's values read-only with `on <host>`; the other tabs have the
  same gap today.
- Spend over `--host` is the engine machine's ledger only. A conversation whose model calls
  were made on another machine (a laptop-run member of a far team) is not counted; no such
  arrangement exists today.
- (Settled by d1, 8.8: the packet file now rotates.) A packet file was never rotated. A team that raises thousands of packets grew it without
  bound; if that happens, rotate like Traffic and keep undecided packets in the new file.
- The ruling does not say who may reopen a team whose parent is closed; the store refuses it
  (`ErrParentClosed`) and the card should offer `Reopen harbor` instead.

### 8.8 What the session built (d1)

Everything below is `internal/session` unless named otherwise, on branch `task/deleg-session`.
The interface side (d2) meets it only through `internal/teams` and its Traffic.

**Wake is an inherited setting.** `teams.Settings.Wake`, `Effective.Wake`/`WakeFrom`,
`Defaults.Wake`, config key `teams.wake` (8.2). A conversation's roles are resolved against the
profile's `teams.` rows, which are read again only when `config.json` moves (one stat per
boundary). A closed team gives no role at all: no verb, no delivery, no team turn.

**Home and links (routing).** A conversation's home is `File.Home(key)`. In a managed team whose
manager is not its home it is **shared**: that manager is a link.

- `team_send` kind `directive` and `team_stop` from a link are refused, in words that name the
  home team (`@web reports to the manager of "harbor", not to you: here you are a link …`); a
  note goes through. A directive to `everyone` is written once per member who reports here
  (`To` = handle) and names the shared ones it left out; with none left it is refused.
- A member reads a link's directive (from an older writer) as `◆ fyi from the manager of …`
  and is not woken by it.
- `team_status` and the digest mark a shared member `reports to <team>`, and `busy for <team>`
  in place of `running` (`teams.MemberState.ReportsTo`). `team_status` also lists the packets
  waiting on the team.

**Questions up.** `ask` of kind clarification, choice or confirmation from a member whose home
team has `questions_up` effective raises a `question` packet (`Team` = home team, `Origin` = the
membership, `RaisedBy` = handle, one party with the asker's reason as context, the options with
their consequences, the pick as the recommendation) and returns at once, saying so; nothing is
shown to the person, and the manager is roused if nobody holds it. A permission, landing,
assumption or ratify never goes up. The manager's own clarifying question (and its judgement
calls) is a packet too: to its home team when it has one and questions go up there, otherwise
to `you`. New manager verbs, all on the approval floor (allow):

| Verb | What it does |
|---|---|
| `team_decide` | `Decide(id, "manager", answer, reason)` on a packet waiting on a team it manages; an option may be named by its label; refuses cap and closing packets (the person's) |
| `team_escalate` | `Escalate` to its own home team (`to: up`, the default) or to `you` |
| `team_close_report` | raises the `closing` packet to `you` (done, left, files, the team's spend today) |

Delivery reads `KindPacket` lines by packet id: a manager is handed a packet newly waiting on
its team whole (question, contexts, `[id] label: consequence`, recommendation, trail), and is
woken by it; the raiser (member or manager) is handed `◆ answered: <label or words> (by ◆
@boss, because …). Your question was: …` and woken; an escalation of its question is told
without waking. `Decide` on a question now logs `answered @web: <label>` (the rail draws it
after the decider's mark).

**Caps.** At every point the team would START something (a member or manager wake, a new
member's brief, `team_start`), the pool (owner = `CapFrom.Team`, or the top of the chain for a
Settings cap) is checked: at or over the ceiling, the start is held and the Traffic says `held
@web: harbor reached its $5 cap today …` once per reason; a running turn is never cut; the
person's own typing is never held. The first holder raises ONE `cap` packet to `you` for the
pool and ceiling (`harbor reached its $5 cap today`; `raise` = `Raise to $10`, twice the
ceiling; `stop` = `Stop for today`; recommended `stop`), carrying `Packet.Cap` =
`CapFacts{Team, Day, CapUSD, SpentUSD, RaiseTo}`. The raise is idempotent across processes:
under the decisions file lock, the crossing is the pool team id, the local day and the ceiling
(`CapUSD`). A second raiser, another window or a headless wake, finds that packet and writes
nothing. A later day, or the same day after a `raise` when the new ceiling is crossed, is a
different crossing and a new packet. A decided `raise` lifts the ceiling to
`RaiseTo` for that local day; meeting it raises one more packet at the new ceiling; `stop` or
the person's own words hold until the day turns or the cap is changed. Spend is read through
`TeamSpend` only when `TeamSpendStamp` moved (per pool, per session), never per model request.

**Wrap up first: the door and the marker (for d2).** The interface appends ONE Traffic entry to
the team's log:

```go
teams.AppendTraffic(profile, teamID, teams.WrapUpRequest("")) // or the person's own words
// = Entry{Kind: KindDirective, From: FromYou, To: ToManager, State: StateWrapUp ("wrap-up"), Text: …}
```

`teams.IsWrapUp(e)` is the only reader. The manager is woken by it and handed an instruction
(tell every member to finish and commit, answer what it can, start nothing new, then
`team_close_report`), bounded by `wrapUpFor` (15 minutes) and `wrapUpSpendUSD` ($2 of the
team's spend since the clock's first look). The start and the bound are written on the team
(`teams.Wrap`, through `Change` / `ChangeIf`) when the clock starts, and cleared when the
report goes out. A session that opens reads it back (`teamWrapUpResume`): the time left is
the bound minus how long since the start, and a wrap-up already past its bound raises the
incomplete report on that start, once. Past either with no report, codeaf raises the
`closing` packet itself with `Report.Incomplete`, question `close harbor? (wrap-up incomplete)`,
options `close-now` / `keep-going`, recommended `keep-going`. A complete report has `close` /
`keep-going`, recommended `close`. At most one closing packet waits per team.

**Accepting closes.** `teams.AcceptClosing(profile, packet)` closes `packet.Origin` with the
packet as its report and appends a `KindClose` line, only for a decided closing packet whose
decision is `close` or `close-now`; it is idempotent. The interface calls it after the person's
`Decide`; the manager's session calls it again when its delivery reads the decision (and tells
the manager). `Close now` stopping member turns and closing tabs stays the interface's (8.5).

**Packet file rotation.** `decisions.jsonl` rotates past 1 MB (`decisionsRotateBytes`) to
`decisions.1.jsonl`, and the new file opens with one `carry` line per packet still waiting (the
packet whole, trail and escalated state included). The reader folds the rotated file, then the
current one; a carry replaces what the older file said of that id. A waiting packet is never
lost; a decided one stays readable for one more rotation.

**Over `--host`.** All of the above runs where the conversations run, the engine: questions,
caps, the wrap-up clock and the verbs are the engine's session reading the engine's profile and
ledger, so a window over `--host` needs nothing new. The clock travels with the team:
`Teams.Read` and `Teams.Update` already return `[]teams.Team`, and `wrap` is a field of that
record, so a restarted engine resumes the same countdown. It sees packets through `Teams.Packets`
and decides through `Teams.Decide`. Traffic has no general writer over the wire, so the wrap-up
has two narrow doors of its own, said by `Welcome.WrapUp`: `Teams.WrapUp` (`WrapUpArgs{Team,
Text}`) appends exactly `teams.WrapUpRequest(Text)` to the engine's log, and
`Teams.AcceptClosing` (`AcceptClosingArgs{ID}` → `AcceptClosingReply{Closed, Stamp}`) runs
`teams.AcceptClosing` on the engine. Client: `(*remote.Client).TeamsWrapUp(team, text)`,
`TeamsAcceptClosing(id)`. The interface wires them into its seam (d2); an engine without the
flag gets `Close now` only, said as such.

**Known gaps.** A conversation in an unmanaged sub-team whose home
manager is a level up gets no member verbs and no questions-up (its membership has no manager). (Closed by 8.9: such a member answers to that manager for
questions and `team_post`.)
A person's decision on a packet whose raiser nobody holds is delivered when that conversation
next runs; only a manager is roused.

### 8.9 What the interface built (d2), and where it departs from 8.4 and 8.5

Everything below is `internal/tui3` unless named otherwise, on branch `task/teams-page`. The
person's guide is `internal/manual/chat/teams-page.md`.

**As specified.** The `teams` place right after home (`placeOrder`, one entry, so the bar is
data and not restyled); the rail with `All teams`, the tree, `+ New team`, `✦ Organize` and
`▸ Closed · N`; the pane with the header (spend against the pool, `Settings`, `Close…`,
`Open ▦`), the members line, the inbox cards and the manager's real conversation under them;
the team card with provenance and `reset`; the close card; the Closed fold with `Reopen` and
`Delete…`; Organize's quiet-team proposal with `Undo`; the `Teams` settings tab's dim line.
The session's contract (8.8) is used as built: `teams.WrapUpRequest` through the seam's
`WrapUp` door (locally `teams.AppendTraffic`, over `--host` `Client.TeamsWrapUp`), and
`teams.AcceptClosing` after the person's Decide on a closing packet (over `--host`
`Client.TeamsAcceptClosing`), both only when `Welcome.WrapUp` says so. Cap packets draw
`Packet.Cap`; Raise and Stop are the person's alone. Shared members read `reports to <team>`
or `busy for <team>` (`MemberState.ReportsTo`).

**Where it departs, and why.**

- **The marks are `⠿` (a member working, dim) and `? N` (amber, N things wait on you from the
  team or a team under it)**, not `●` and `◆ needs you`. `●` is already every team's colour
  dot and `◆` already marks a manager; a count says how much waits, which the rail row had no
  room to say in words.
- **The team card is the one settings surface.** The wall's `e`, the chip menu and the
  switcher's `Team settings…` all open it, so the wall's old name-and-colour popover is gone
  from use, and its delete with it.
- **Delete exists only on a closed team** (8.5 said so), so the wall's `D` now closes the shown
  team instead of deleting it, with Undo when nothing runs and the close card when something
  does.
- **The keyboard reaches the page's buttons by `alt+↑` `alt+↓`**, and `esc` gives it back to
  the message box. The pane hosts a real conversation whose box takes every plain key, so the
  page's letters (`s c w n o m r d u`) and arrows work only once the person has stepped onto
  the buttons; without a manager in the pane they work at once.
- **The Traffic rail is folded on this page** (`alt+l` unfolds it), because the teams rail has
  the left edge and the pane is narrower than a conversation's own screen.
- **Choosing a team with a manager brings that manager's conversation in front.** It is the
  person's own selection, so this is not focus moving by itself; the conversation they were in
  stays open behind on the strip. Resuming a member not open opens it behind, without moving.
- **The manager comes into the pane on every road, and the pane says why when it cannot**
  (`teamsopen.go`). The attempt is state the page holds: `teamsSync`, run after every message,
  starts one whenever the selected team's manager is not in front and none was made for it, so
  teams arriving after the page opened or a manager set on the file by a session are brought
  in too. A manager this window is not holding is opened off the loop in its own folder, held
  behind, and brought forward only if the page still wants it, so the person stays on the page
  (the switcher's door it used before steps off any place standing). A refusal, or an open with
  no answer after 4 seconds, reads `couldn't open ◆ <team>'s manager: <reason>` with `Retry` and
  `Open in chats`; a manager whose transcript is gone offers `+ Manager`, which replaces it.
  Before this the pane read `opening ◆ <team>'s manager…` whenever the manager was not in front,
  with nothing behind the word (reported by the owner, 2026-09-24).
  One open per manager is out at a time: choosing the team again while it is out takes that
  open up instead of asking the door twice, an answer for the manager the page is asking about
  is the page's whichever attempt carried it, and a refusal about a conversation the window now
  holds is no refusal. Only `Retry` asks again while an open is out. Over a connection that holds
  one conversation at a time the swap is asked on the ordered door line, never from Update.
- **How the page loads.** The rail, the header and the pane's frame are drawn from memory on the
  opening frame (the teams file is the one read made on the loop, and it does not block), and
  every reading fills in place: the spend is the header's last piece, members start as members
  and become chips, and nothing on the page says it is loading. The reads are one command off
  the loop, on the opening, a choice, a gesture and the router's beat while a team has a
  manager. It asks the packets, the defaults, the selection's pool spend and the members' rows
  side by side, because over `--host` each is a round trip. The rows are the open teams'
  members only: read by name off this machine's disk (`session.ReadRows`), or cut out of the
  world a connection already holds. The page used to walk every session on the machine on its
  opening and every beat to find them. One read is out at a time and none is dropped: a read
  asked while one is out is made when that one is folded, for the selection as it then stands.
  Measured on Spark over a 320-session, 5-team, 25-member fixture (2026-09-24): opening to the
  first full reading 33 ms before, 3.3 ms after (the walk was 22 to 29 ms of it, the member
  rows are 2 ms); a team chosen while the beat's read was out never showed its spend until the
  next beat, and shows it in 3 to 12 ms now; a double press on a cold manager's team left the
  pane saying `open in another window` in 2 of 5 runs, and in none now. The store's own reads
  are microseconds locally and were left alone; so was the in-process door's resume, whose
  bucket scan costs 3 to 5 ms of a 10 to 40 ms open.
- **A message handed to the hosted manager that puts another conversation in front takes the
  person to it.** A Traffic row goes to its member through the chat surface's own door
  (`trafficGo`), and the page steps down for it as a press on a member row does.
- **`Close now` keeps the front tab.** Every other member's tab closes, but the conversation the
  person is looking at stays, so a close never moves them.
- **Over `--host` the seam's `History` and `Append` doors are nil**: a closed team's report is
  not read over the connection, and close and reopen write no Traffic lines there. The page
  says so where the report would be. Organize's quiet-close proposal is offered locally only,
  because quietness is read from the Traffic log.
- **The `Teams` settings tab is read only over `--host`** and says the teams inherit the other
  machine's Settings (8.7's first question, answered on the edit side; the engine's own values
  are not read across yet).
- **Wake is on the card** (`team messages wake`, with its provenance), which 8.4's four rows
  predate; the settings tab's row order is questions, wake, cap, depth, share.
- **The interface writes no cap raise.** The person's `Raise to $10` is a Decide; the session
  applies it (8.8), so the setting has one writer.
- **Under 72 columns the rail stacks above the pane**, because a 24-column rail beside a
  conversation leaves the conversation too narrow to read. A hosted manager at that width is
  shown without the rail.
- **The manual's digits follow the whole order** (`home  teams  chats  sessions  spend
  settings`, `alt+1` … `alt+9`): when `chats` landed third, every digit after `teams` in the
  manual and the help moved by one.

- **The header is one line, and the members are a card** (owner feedback 2026-09-24, built on
  `task/nest-ui`, `teamcrew.go`). The two wrapped rows of every member as prose
  (`@review not open, reports to test 1d · …`) are gone. The line is the name, `◆ Manager` (a
  door to the manager), a chip for each member with something happening (`⠿ working`, `?
  asking` in the needs-you amber, `✗ failed` from its newest Traffic event), one quiet word for
  everyone else (`+4 idle`, or `6 members` when nobody is doing anything), the spend (only with
  a spend or a cap to set it against, the cap in whole dollars, `$0.42 of $5 today`), and the
  three buttons. Narrow, it drops the idle word first, then the spend, then the chips from the
  last, then `◆ Manager`, then the buttons from the right, never the name. The idle word (or
  `p`) opens the **members card**, titled with the header's own count (`harbor · ◆ Manager ·
  1 member`, where it used to say `2 members` beside a header saying `1 member`): handle,
  title, state, last active, `also in test` for a
  shared member (its hint says whose manager it reports to) and `Open` or `Resume`. `not open`
  is said nowhere on the page: it is a fact about the window, and the button carries it. The
  card hangs over the pane, clear of the rail, because its rows are the drag source for adding
  a member to another team (8.11). On the root with a manager the header and the card list
  `TopManagers()` (8.10), never their members.

**Known gaps.** A switcher row `Closed · N` has no hover hint. The wall popover's delete code
is unreached and kept until the wall is next reworked. Over `--host` the Settings tab cannot
show the engine's `teams.` rows.

### 8.10 Nesting, as the session built it

Everything below is `internal/session`'s `team_nest.go` unless named otherwise, on branch
`task/nest-session`. The interface meets it only through `internal/teams` and its Traffic; it
needs nothing new to carry any of it out.

**A sub-team is started like a member.** `team_start` takes `kind` (`member`, the default, or
`team`), and for a team `name` and `members` (handles of the manager's own members to move in):

```json
{"handle":"api","brief":"…","kind":"team","name":"backend","members":["parser"]}
```

In one `teams.Update` it checks the manager still manages the team, that the team is open and
`CanNest` (refused with the level, the limit and its origin: `A team under "harbor" would be
level 3, past its depth limit of 2 (from Settings)`), that the handle and the name are free and
that each member to move reports here; it makes the child under the manager's team, writes
`SubTeamCap(parent)` on it as its own `cap_usd_day` when that is above zero (a parent with no cap
gives none, and the child spends from the pool above), and moves the members (added to the
child with their handles, removed from the parent). Then it writes ONE start to the parent's
Traffic, `Entry{Kind: start, From: manager, To: <handle>, Text: <brief>, Team: <child id>}`
(`teams.Entry.Team` is new), and a `note` from `system` to `room` in the child's Traffic naming
who made it and who runs it. The approval is `team_start`'s (ask), and the card's gloss reads
`a new team "backend" under yours, managed by it.` before the brief. The cap holds it like any
start.

The interface carries the start out as it carries out every start: it opens the conversation
behind the one in front and adds it to the PARENT team under its handle (`Started`). The new
conversation reads its brief at its first boundary (`team_wake.go` starts that turn, as for any
start); a start that names a team is handed as `◆ you were started to manage the team
"backend", under "harbor": …` above the brief, and the session then makes itself the child's
manager (`Agent.claimSubTeams`: `AddMember` with its parent record, `SetManager`, in one
`Update`; a child gone, closed or already managed is left alone and the parent's Traffic says
`could not make the new conversation a team's manager: …`). This happens before the role note is
composed, so the request that carries the brief also says it is the manager. Its manager is a
member of the parent, so its home is the parent's manager by the ordinary rule (8.2): it reports
up and takes the parent manager's orders.

**Conflicts (`team_raise`, members and managers, allow).**

```json
{"question":"…","parties":["@api","back/@api"],"context":"my side",
 "options":[{"label":"JSON","consequence":"…"},{"label":"form data","consequence":"…"}],
 "recommend":"1","reason":"…","team":"optional"}
```

A party is a handle, looked for in the raiser's own teams first and then in every open team, or
`team/@handle` (a team by name or id); one handle answering to two conversations is refused
with both spelled `team/@handle`. The raiser is a party already (from its membership where it
is not the manager, the first). The decider is `LCA(all party keys)`; none is `you`. The origin
is the raiser's membership at or under the decider, and each party's `Team` is the membership
it was found in, or its membership under the decider (not one it manages, the deepest, first),
so every party's line runs through a team under the decider. At least two options, each with a
consequence. The LCA manager is roused if nobody holds it.

In the store (`teams` `decision.go`): every packet line now goes to the origin, the decider and
every party's team (`involved`), so the other parties are told `◆ @web raised a conflict naming
you (p…), for a manager above to decide: …` (not woken). A decided conflict appends to each
party's team log a ruling, `Entry{Kind: directive, From: manager | you, To: <handle>, Member:
<key>, Packet: <id>, State: decided, Text: "ruling on the conflict p…, by ◆ @boss (manager of
"harbor"): JSON: <consequence>. Because: … The conflict was: …"}`, whoever decided it (the
manager's `team_decide`, a manager above after `team_escalate`, or the person on the teams
page, here or over `--host` through the engine's own `Decide`). `teams.IsRuling(e)` is the one
reader. The session delivers a ruling to the party it names (`Member`, else `To`) whatever it
is in that team, member or manager, shared or not, as `◆ ruling on … Follow it unless the
person said otherwise in this conversation.`, and it wakes that party (`teamWakes`). The
decided packet line hands the parties nothing more, so a ruling is delivered once. `mayDecide`
refuses a manager who is one of the parties (`ErrNotDecider`); the person still may. A
manager handed a conflict sees `parties: @web, @api` and may `team_read` a party in a team
under its own as `team/@handle` (a read reaches down the tree; it changes nothing).

**One level.** `team_send` (directive or note) and `team_stop` naming a handle that is not the
manager's own member but a member of a team under it are refused with the sentence that names
whom to send to: `@parser is in "backend", a team under yours, and orders go one level down: it
takes them from the manager of "backend", not from you. Nothing was sent. Send a message to
@api, which passes on what it should.` A sub-team's manager who is not a member of the parent
(a tree the person nested by hand) is named as such; a member of a sub-team with no manager is
told to ask the person for one. `everyone` was already the manager's own members.

**The global manager** (`teams` `root.go`). While the root has a manager, tidy seats every open
top-level team's manager as a member of the root with its own handle when free (only ever
added), and `(*File).TopManagers()` is the root's members who manage an open top-level team now.
The session's view of the root for its manager (`managedView`) is its manager and
`TopManagers()`, never their members: the roster, the digest, `team_status`, `team_send`,
`team_stop` and `everyone` all read it. The top-level managers therefore take its directives by
ordinary delivery, report to it by the ordinary home rule (their questions reach it before the
person), and conflicts across trees meet at it. `team_start` of kind team at the root makes a
top-level team. With no root manager nothing changes.

**The unmanaged sub-team.** A member of a team with no manager whose nearest open ancestor has
one (`bossOf`) is a managed member (`teamRole.boss`, `bossName`): it has `team_post` and
`team_raise`, its clarifying questions go up as packets to that manager (origin its own team),
and its `team_post` to the manager is written to that manager's team log, marked `(from
"dock", a team under yours with no manager of its own)`. Its room posts stay in its own team.
That manager does not direct it.

**Roles.** A sub-team manager's role note says `Your team is a team under "harbor": you report
to its manager, @boss. Post to it with team_post; your questions go to it before the person.`,
a top-level manager under a global manager `Your team is a top-level team, under the global
manager of "All teams": …`, and a manager with sub-teams `Teams under yours: "backend" (run by
@api). Direct their managers, never their members.` The global manager is told `You are the
global manager: "All teams" holds every team, and your members are the managers of the top-level
teams, never their members. …`. A top-level manager's membership of the root is told it reports
to the global manager. A member of an unmanaged sub-team is told whom it answers to. The laws are
seven: law 4 says one level down, law 7 says how a conflict travels. Members' notes gained one
sentence (`A conflict you cannot settle with another member or team goes up with team_raise.`).
None of this is in the fixed prefix: the verbs arm at a boundary and the role rides a note, so
`TestTheFixedPrefixStaysUnderItsBudget` and `TestTheLeanPrefixStaysUnderItsBudget` are
unchanged.

**Over `--host`.** Nothing new crosses: every step above runs where the conversations run, and
the window's `Teams.Raise` / `Teams.Decide` are the engine's store calls, so a conflict the
person rules over `--host` writes its rulings on the engine
(`TestAConflictRuledOverTheWireReachesEveryPartyOnTheEngine`).

**For the interface (d3u).** A start line may carry `team` (a sub-team start: draw `◆ started
@api to run backend`); carrying it out is unchanged. A ruling is a directive with `packet` set
(`teams.IsRuling`), in each party's team: draw it as the ruling on that packet, not as that
team's manager's own directive (`From` is `manager` for any deciding manager, `you` for the
person). Packet lines now also appear in every party's team. The root's members include the
top-level managers once it has a manager; the teams page's `All teams` header should list
`TopManagers()`.

**Known gaps.** A root membership seated for a manager who later stops managing a top-level team
stays until the person removes it (the session's view ignores it). A sub-team start whose new
conversation never runs (the window refused the start) leaves an unmanaged child team; the
parent's Traffic shows the refused start only in the window. The root's folder for a
top-level start is whatever the interface's `teamWhere` gives the root.

### 8.11 Nesting, as the interface built it (d3u)

Built on `task/nest-ui` from rulings c-12 and c-13, in `internal/tui3` (`teammove.go`,
`teamdrag.go`, `teamcrew.go`) and one store file, `internal/teams/move.go`. The person's guide
is `teams-page.md`, *Moving a team inside another team, and adding a chat to a team*.

**The store answers two questions** (`move.go`), because the session's own restructuring tools
will ask them too. `(*File).MoveCheck(ids, parent, d)` says whether teams may go inside a
parent ("" is the top level, which is the root when there is one, `MoveTarget`) and when not a
`MoveBlock` with its facts: `self`, `inside` (a team under the moved one), `closed`, `depth`
(the target's depth, the levels the moved subtree takes, the target's effective limit and its
origin), `here` (already there), `root`, `gone`. A team picked together with its parent rides
inside it (`MoveRoots`). `(*File).MoveEffects(ids, parent, d)` is what the move changes, on
tidied copies of the file: every carried conversation whose `Home` changes, every moved team
whose capped pool above it changes (the pool owner by the `teamsPool` rule), and every moved
team whose conflicts `LCA` over its own conversations changes. `(*File).Move` writes it through
`SetParent`.

**Move into…** (`m` on the rail, or `Inside: harbor ▾` on the team's card) opens one picker: a
card with a filter box (every printable key filters; only the arrows walk), `Top level`, then
the open tree indented. Every row is drawn; a row that cannot take the move is dim and its
reason is the card's foot and the hint line, in the ruling's words
(`orbit is 2 levels deep · limit 2 · Settings`, `set on harbor` when a team set the limit).
`enter` on a dim row moves nothing. `space` on a rail row picks teams (the wall's `☑` in place
of the dot) and `m` moves them all; `esc` clears the picks.

**The drag** is the rail's shortcut. A press on a team row selects it as a click always did; it
becomes a drag only after two cells of held movement. A member's chip or a members card row is
a drag source too, and its press waits for the release so a drag never opens it. During a drag
only a target that takes the drop is grounded, the dragged row is dim, the first blank row under
the tree reads `↳ Top level` (the empty rail under the tree is the top level), and the hint
says `Drop to move api into harbor` or the block's reason. A member dropped on a team is
ADDED (`AddMember` with what its own team kept of it) and never removed from where it came
from. `esc`, or a second press before the release, drops the drag.

**The consequence line.** Every move goes through one door (`teamMoveAsk`): with no changes it
is written at once; otherwise one line on the pane (or on the card) says
`api will report to harbor's manager · its $3/day becomes part of harbor's $10 pool` with
`Move` (the accent, where the keyboard lands) and `Cancel` (`esc`). A conflicts clause is said
only when the conflicts go somewhere the report clause has not named. Every move, confirmed or
not, is offered back for `teamsUndoFor` (six seconds) with `Undo` or `u`: the old parents are
written back, and every carried conversation's home flag is put back where the membership can
still hold one. The pane shows one Undo at a time, the newer of a close and a move.

**`+ New team in harbor`.** With a team chosen the rail's `+ New team` makes the team inside it
(the wall's naming card says `New team in harbor`), and writes `SubTeamCap(parent)` as its own
cap when that is above zero, as the session's `team_start` of kind team does. A rail too narrow
for the words draws two rows rather than cutting the name. A team at its depth limit dims the
row and its hint and press say why.

**The switcher and the wall.** The strip's switcher lists the open tree, each sub-team indented
under its team. The wall's Teams row stays flat and names a nested team `harbor › api`, each
part cut on its own so the team's own name is never the part lost.

**Traffic (8.10's asks).** A start carrying `Team` reads `◆ started @api to run backend`,
and the name is read from the teams held in memory when the rail is drawn, so a rename
does not leave `to run` on the old name. A
ruling (`teams.IsRuling`) reads `◆ ruling → @web` (or `you ruling`) with the decision's words
and the packet named in the hint, never as the team's own manager's `do`.

**Where it departs, and why.**

- **`m` is Move into…, so starting a manager moved to `M`.** The ruling names the letter; a
  manager is started once per team, a move whenever the tree is reshaped.
- **The picker is a card over the frame, not a menu hung from the row**, because the same
  picker opens from the team's card, which is itself a card in the middle of the frame.
- **A drag from a team row still selects it on the press.** The shared place laws hold that a
  press on a row does what `enter` on it does; a team's select is harmless under a drag, while a
  member's door (which opens a conversation, possibly leaving the page) waits for the release.
- **The members card's rows, not the header's chips, are the everyday member drag source**,
  because idle members (most of them) are only on the card.
- **A consequence line wraps on a narrow pane** rather than cutting the thing being decided; the
  buttons follow the last line, or stand on their own row when it is full.

**Traffic of a move.** A committed move appends one `KindEvent` to each affected team's log,
through the store, beside the write (`MoveNotices`, `MemberMoveNotices`, `WriteMoveNotices`).
The team left and the team that moved say `@handle moved to harbor`. The team joined says
`@handle joined from ops`. For a team moved under another, that is the moved team and its new
parent (and the parent it left, when it had one). A refused move appends nothing. The lines are
from `system` to `everyone` with no member state, so the existing Traffic read hands them to a
manager on its next wake and they do not start a wake of their own.

**Known gaps.** The picker has no pointer scroll; a list longer than the frame scrolls with the
cursor only. Multi-select is keyboard only (`space`); there is no pointer gesture for picking. Over `--host`
the move writes through the seam like every edit, but the defaults the picker reads for the
depth limit are the engine's only once the page has read them (`Teams.Defaults`); before that a
depth block is not shown and the store's own `SetParent` is the only check.
