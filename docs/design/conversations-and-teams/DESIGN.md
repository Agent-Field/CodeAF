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

**Doors in.** `alt+v`, `/wall`, the `▦` dock under the input box, and `▦ All` beside the tab
strip's `+`. The dock is a one-row map of every open conversation coloured by state; a click
on a cell switches to that conversation without opening the wall.

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
(`Add this conversation to teams · m`). `?` opens a sheet of everything the page can do,
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
hue. The strip reads `Home   ● name ▾   ◆ Manager   tabs…   +   ▦ All`: Home is a fixed door and
stands first, the chip filters the tabs so it sits right before them, and the manager's place
follows it; as the row narrows Home goes first, then the chip, never the tab in front (ruled
2026-09-24). Showing a team narrows the strip to its members open in this window, plus the tab in
front (section 1). The chip is the switcher: teams, All, add or remove this conversation, new team, team
settings. New conversations started while a team is shown join it.

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
  says so (`to ◆ manager`, on the box's rule once there is text). Traffic is drawn as
  threads (below, **Traffic is threaded**). The rail holds the right-hand
  column while the manager is in front, the task column folding to its edge beside it; it
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
opens the member through the strip's door, resuming it first; a team's name opens the wall
on that team. They are resolved from memory only; an `@word` that is no member's handle stays
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
- **Handles are links there too**, inked and grounded as in the chat, hint `Open @x · title ·
  click` (`Resume` when not open here), a press opening or resuming the member. A message's words
  are a door: hover puts them whole in the hint line, a press lays them out under the row, a
  second folds them. Rows are cached on the entries, width, pointer, minute and what is laid out.
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
- **Off switch.** A team's `wake` field in `teams.json`, on when absent and written only as
  `"wake": false`. With it off, a directive, a reply and a `team_start` start no turn: the
  new member is opened and reads its brief on the first turn something else starts, and the
  Traffic says `opened @handle; this team's auto-wake is off, so no turn was started.` The
  settings to turn it off from the interface come with the delegation work; until then it is
  the field and the manual's line.

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
(`mentionLinkPass`). A `●slug` opens the wall on that team. An `@handle` or `@slug`
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

**Not in v1.** Collision flags when two members touch the same files, nested managers (a sub-team's manager is a member
of the parent team; reports flow up, directives down), and dispatch of whole plans.

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
