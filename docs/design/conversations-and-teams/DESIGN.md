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

A full-frame grid of tiles, one per open conversation, each drawing the live tail of its
transcript.

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
team is shown, a `● name ▾` chip leading the tab strip and the rule under it in the team's
hue. The chip is the switcher: teams, All, add or remove this conversation, new team, team
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
  says so (`to ◆ manager`, on the box's rule once there is text). Traffic shows routed
  messages (`@parser → @web  fyi`, `◆ → @web  do`) with their age, stops and starts with the
  reason and the brief, and events (finished, failed, asking in the needs-you amber), every
  row with a member behind it clickable to open that member. The rail holds the right-hand
  column while the manager is in front, the task column folding to its edge beside it; it
  is put away with `hide alt+l` to a `Traffic` edge that counts what arrived, and on a narrow
  window it is that edge and a card laid over the lower conversation;
- each turn it carries a small team digest (members, handles, states, questions waiting,
  files touched, recent traffic), never whole transcripts;
- its messages reach members marked `◆ from manager`, never as if the person had typed them;
- nothing it does moves the person's focus: a member it starts opens behind the conversation
  in front, as a tab named `@handle`, and takes its first turn on its own (the session sees
  itself started and wakes), reading the brief as the manager's.

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

**Handles.** Titles are too long to address, so each member gets a short handle (`@parser`),
derived from its title when it joins, or when it first gets a title if it joined untitled;
unique in the team, never changed on its own, clickable everywhere it appears.

**Where it lives.** The manager is an ordinary session file. Traffic is
`<profile>/teams/<id>/traffic.jsonl`, append-only, rotated at 4 MB with one old file kept;
the team id is its channel. The store is `internal/teams`, shared by the UI and the tools:
every write is a read-modify-write under a file lock (`Update`), so neither side overwrites
what the other wrote.

**Traffic is the only channel between the UI and the session.** A conversation's identity in
a team is its transcript path, the same key the tab strip uses. The session side writes
messages, stops and starts as Traffic entries and delivers what is addressed to it before
every model request, with a cursor kept in its session folder so nothing arrives twice and a
new member is not handed old history. The UI tails the same log off the loop: it draws the
rail, and it carries out stops and starts for the conversations its window holds, once per
entry. Neither package calls the other for team features.

**Known limits of v1.** A stop or a start takes effect only in a window that holds those
conversations. A member waiting on a permission prompt shows as running, because the prompt is
not in its session file, until its own asking event says so. A message never wakes an idle
conversation; it is read when that conversation next runs; the one exception is a member's
own start. Over `--host` the manager is off: the store is in the engine's profile and this
window can read only its own, so the doors say so and no rail is drawn.

**Not in v1.** Waking on team events by itself (with a budget and an off switch), collision
flags when two members touch the same files, nested managers (a sub-team's manager is a member
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
  store, refused over `--host` until profiles are reconciled.
- A Teams place on home showing the tree; nesting in the UI; drag a tile onto a team.
- The manager waking on events, collision flags, nested managers, dispatch.
