# The team manager

## What a manager is

A team can have one **manager**: a conversation that runs the team for you. You talk to the
manager, and it hands work to the team's members, keeps track of what each is doing, and tells
you where things stand. It is an ordinary conversation with every ordinary tool, under the
same permission rules as any other; what makes it the manager is the team verbs below, an
instruction from codeaf on its first request that it is this team's manager (with its members'
handles and the rules under **Who outranks whom**), and a short account of its team that it
carries on every turn: each member's handle and state, the question a member is waiting on, the
files each has touched, and the last few lines of the team's traffic. It never carries members'
whole conversations. A member of a team with a manager is told the same way which team it is
in, its handle, and that it reports with `team_post`.

Every member has a **handle**: a short lowercase word that says what the conversation is about,
like `@security` for `project branch code complexity & security review`, `@milestones`
for `codeaf repo issue tags & milestones` or `@gravity` for `quantum gravity research updates`.
The moment a member has a title it gets a quick guess from the title's words, so it can be
addressed at once; then, when the conversation's title is made, the same cheap model that
names conversations chooses the word, once, for a few tokens. A timeout or a dropped
connection is asked once more, a moment later; a refusal is not, and the guess stands. If another member of the team
already has that word, the member takes the model's second choice, or the word with one word
of its title in front, like `@api-security`. A handle a person or the manager gave, like the
one `team_start` names a new member by, is never replaced, and a handle the model chose is not
chosen again, so a line in the traffic keeps meaning the member it meant. Messages in a team
are addressed by handle.

Handles made before this were guesses, and each is chosen again the same way on its
conversation's next turn. Every change is written to the team's traffic as
`codeaf  @review is now @security`, which the manager and every member are told at their next
step, and the manager's account of its team shows the new handle. Over `--host` the choosing
happens on the far machine, where the conversation and the model are.

In the manager's replies, in the team's quoted cards and in a team tool's call, every member's
handle is a link: point at it for its title in the hint line, press it to open that member,
which resumes it first when this window does not have it open. A team's name in those same
places, written as a team (`team harbor`, `the harbor team` or `"harbor"`), is a link too. A
press opens the **teams page** with that team selected. The hint says `Open harbor on the
teams page · click`. Over `--host`, against an engine with no teams doors, the press opens the
team-agnostic conversations view instead, and the hint says so.

## How long can a crew member's handle be — handle length, name limit, characters

A handle is 2 to 24 characters: lowercase letters, digits and hyphens, starting with a
letter or digit. That is the limit for a handle you type, or the manager gives with
`team_start`. A handle codeaf picks itself, from the title's words, the title model or a
proposed crew, stays within 12 characters so it remains one short word; one numbered
for a clash stays within 12 too.

A computer, or another app on the same profile, still running an older codeaf accepts only 2
to 12. When it loads a team with a longer handle it writes a guess from the title's words in
its place, and that change syncs back: the typed handle is lost everywhere, and the guess may
be chosen again by the title model. Update every computer that shares the team first.

## Making a manager

The **teams page** (`/teams`, `alt+2`) offers `+ AI manager` on a team that has none, and
`All teams` offers one manager over every team. Choose a team to talk to its manager
beside the tree of teams.

Managers cost nothing until you make one. A team's manager is pinned at the left of
its tab strip and never scrolls away:

- **`+ AI manager`**, a quiet button, while the team has none. A press starts a new conversation
  in the team's folder and makes it the manager. Its hint says so. Under 100 columns the
  button leaves the strip; Teams still offers manager controls.
- **Choose AI manager** on Teams assigns an existing member. To use a new conversation,
  first use Add chat, then Choose AI manager. The Chats dropdown and conversation grid
  offer no manager-assignment controls.
- **`+ New from this session`**, last in the Teams dropdown, in the conversation you are
  working in: a new team is made at once with that conversation as its manager, and
  `/helpers` then asks it for helpers (see **Turn this conversation into a team**).

The manager's place reads **`◆ Manager`**. Its tile in the team-agnostic grid keeps its ordinary title and position. The tab's hint names its team and title. `alt+m` goes to the
manager from any conversation in the team.

An untitled manager is addressable immediately: codeaf derives its fallback handle from
the team's name, for example `@second` for Second. The manager over `All teams` uses
`@lead`; a team name that cannot supply a valid handle also falls back to `@lead`.
If the handle is already taken in that team, a number is added. Another manager can use
`team_send` before its first turn. Once its title is made, the ordinary title-based
handle choice may replace that fallback.

## Turn this conversation into a team — + New from this session, /helpers, convert a chat, bring in helpers

You are working in a conversation, the work has grown, and you want helpers with this
conversation in charge. With no team selected, open the **Teams ▾** dropdown at the left of
the tab strip and choose its last row, **`+ New from this session`**. On the spot:

- A new top-level team is made with **this conversation as its manager** and only member,
  in the next free colour, and a moment later named after this conversation (see **How a
  team made from a conversation is named**). The strip narrows to it and reads `◆ Manager`,
  and the column turns to the team's Traffic. Every team's rules apply. Once it is saved:
  `made the team guitar pickups · this conversation is its manager`.
- The message box is given this, in place of what was there (kept in `/drafts`):
  `/helpers Suggest two or three helpers for this work, and I'll say yes, change them, or cancel.`

Edit it and press enter. **`/helpers`** asks the team this conversation manages for helpers:
it sends your words after it, exactly as the transcript shows them, and a bare `/helpers`
sends that default. It makes no team. Reply to the proposal in your own words. Yes, and it
starts each helper with `team_start`, under your approval posture; each opens as a tab on
this team's strip and starts working at once, while you stay in front of the manager. A
different list, and it proposes again. Cancel, and it takes the team back with
`team_dissolve`. Changed your mind before sending anything? `Disband` the team on Teams.

## How a team made from a conversation is named — + New from this session's team name, rename it

`+ New from this session` names the team twice. On the click it takes the conversation's
folder name, lowercased (`codeaf` for `~/Code/CodeAF`), or, when a team already has that
name, a word such as `harbor`, so the strip can change at once. At the same time the
model that names your conversations is asked, once, for one or two words naming the work,
from the conversation's title or its opening words; it waits at most five seconds. When
the answer comes the team takes it, and the chip changes to it. If it came after the
receipt, a line says `named the team guitar pickups`.

The first name stays when there is no naming model or no answer in time, when the answer
is not a name, when another team already has that name, or when the team was renamed or
disbanded first, here or in another window. A suggested name never replaces one you chose.
Rename the team any time in Teams, under `Settings`. It costs one short call on the naming
model.

## When + New from this session or /helpers says no

They change nothing and say why:

- `/helpers` in a conversation that manages no team: `/helpers asks this conversation's team for helpers, and it has none yet · make one with + New from this session in the Teams menu`.
- On home, or any page: `/helpers asks the team a conversation manages for helpers · open that conversation first`.
- The row on the welcome screen of a new chat: `+ New from this session makes a team of a conversation you have started · say what you are working on first`.
- The row in a conversation that already manages a team: `this conversation already manages harbor · ask it with /helpers`, and the strip shows that team.
- With a team selected on the strip the dropdown has no `+ New from this session` row: the team it makes is always top-level, and a team inside a team is `+ Add subteam` on Teams.
- On a connection that holds one conversation at a time: `helpers open beside this conversation, and a connection holds one conversation at a time`.
- Over `--host` to an engine without teams: `managers are not available over --host yet`.
- The row chosen again while its team is being saved: `the team is still being saved`.

If the save is refused — another window made this conversation another team's manager —
the strip goes back, the box is emptied, and the reason is said. A `/helpers` sent before
the save finished is held and sent once it is saved.

## Why the chat suggested bringing in helpers

A conversation that has gone on may end a reply with one sentence like "This could be
split up and worked on in parallel by a small team. Want to bring in some helpers? Choose
+ New from this session in the Teams menu and I'll suggest who." That is codeaf letting it
say so: after your fourth message, a conversation in no team is told, once, that it may
suggest a team when the work is long and has parts a few helpers could work on at once. It
is told to say it at most once, never for small or quick work, never instead of what you
asked, and never again after you pass on it. Reopening a long conversation tells it once
more, and it can see in its history that it already asked.

It cannot make the team itself; only you can, with `+ New from this session` in the Teams
dropdown. Ignore it, or say no, and nothing changes. A conversation already in a team, a
task's worker, an automation's run and home's `ask here` are never told this.

## Cancel a team nobody joined — team_dissolve

Say no to the proposed helpers, or "cancel", and the manager calls `team_dissolve`. The
team is forgotten as if it had never been made: no row under `Show closed`, nothing
stopped, and the conversation, its history and its tasks stay. The strip goes back to all
your chats and the column to `Tasks`.

A manager has `team_dissolve` only while its team has never held anyone but it: no
member, no former member, no subteam, and nothing in its Traffic. The rule is the same for
every team, however it was made. It does not ask first, because it can only remove a team
nothing happened in. Once anyone joins or Traffic is written, it leaves at the manager's next step, and a
remembered call says `team_dissolve is no longer one of your tools: the team has held
members or exchanged messages, so only the person can disband it, from Teams.` A team
that was used ends with `Disband` on Teams, which keeps its history.

## The manager's screen

While the manager is in front, the message box says `to ◆ manager of harbor`, using the
selected team's name, and keeps saying it on the
rule above the box once you start typing: everything you type goes to the manager and nowhere
else. With a member in front the box says `to @web of harbor` the same way. A shared
conversation uses the selected team's own alias and name; renaming the team updates this
label immediately. The manager can have a title-derived or explicitly chosen alias just
like other members; `@member1` is not a required manager name.

On the right is the conversation's column, the same one every chat has. Its header is two
words, `Tasks 14 · Traffic 8` with `alt+l` at its right: the word in front is in bold ink and
the other is dim, each is a button (point at it for what it shows, press it to bring it to
the front), and while the tasks are in front the Traffic word keeps counting what arrived,
`Traffic 3 new`. With the manager in front the column opens on the **Traffic**; with a member
in front it opens on the tasks. Each kind of chat remembers the word you last chose for the
rest of the session. With the column holding the keyboard (`alt+t`), `←` and `→` switch the
words. Nothing here moves the conversation: both words are drawn in the same columns.

Under the header is what needs you, from both words: a member's question to you (`? @model → ◆
keep the old schema?`), a decision put to you, and your tasks that are waiting on
you, in the needs-you amber; a task that failed and that you have not opened yet is its `✕` in
ordinary ink. Three rows at most, then `+2 more`; a line closes the band, and when nothing
needs you there is no band at all. Pressing a question opens the member at the question; a
decision opens the teams page. What is in the band is not drawn again under it.

In the manager's chat the Traffic is the team's **work**, one line each, the newest at the
top. Every row reads who it is from and who it is for, then the words:

```
Tasks 14 · Traffic 8                 alt+l
? @model → ◆  keep the old schema?  3m
✕ parser bench incomplete
──────────────────────────────────────────
◆ → @scrape +2  Please provide a st… ▸ 2m
◆ → @model      Refactor the rail…  ▾ 1h
  ↳ @model → ◆  ✓ done, 3 files…      4m
  ↳ @model → ◆  working…              now
General                     2 msgs   ▸ 1d
```

- A row is a thread (below): `◆ → @scrape +2` is the manager to the first member, and `+2`
  for the ones that do not fit, then what it said. The state its answers leave it in
  (`running`, `asking`, `done`, `failed`) and how many messages it holds sit at the right
  when they fit, and the hint line always says them. How long ago sits at the right of
  every row, dim: `now`, `2m`, `3h`, `1d`, and past a month `30d`, `12w`, `1y`. A task row
  switches to a date after thirty days. A Traffic row does not. The band's
  questions carry it too. In a narrow column the arrow and the names stay, then the age,
  and only the words are cut, at a word, with `…`.
- `▸` lays the thread's replies open under it, one `↳` line per member, each `from → to`
  the same way (`↳ @model → ◆  ✓ done, 3 files changed  4m`): `✓` on an answer from a member
  that finished its turn, `✗` for one that failed, `working…` for a member the message woke
  that has not answered. Each reply keeps its own age at the right. `▾` (or
  `enter` on the row, with the column holding the keyboard) folds them. `▸` and `▾` are the
  only part of a thread row that folds it. The rest of the row opens the message.
- Everything that answers nothing and is answered by nothing (notes, starts, stops, handle
  changes, anything written before threads) is the one **General** thread, laid open the
  same way. A start that made a sub-team reads `◆ manager started @api to run backend`; the
  name is the team's name now, so a rename replaces `backend` on the next frame.
- A thin `new` line stands under what arrived since you last looked, and stays put while you
  read.

Every `@handle` is a link: point at it for the member's title in the hint line, press it to open
that member (resumed first when this window does not have it open) **scrolled to the
message**: a handle on a thread's row opens the member at the directive as it was told it, and a
handle on a reply opens it at its own post. A press anywhere else on the row opens the
conversation that message belongs to, at that message, and lifts it the same way. A message the
sender wrote opens the sender's chat: `◆ → all` opens the manager at that directive, and
`you → ◆` in a member's chat scrolls that chat to the reply. A message to you opens the
manager's chat. If that conversation is already in front, the row scrolls it in place and does
not open another. The hint says `Open ◆'s message · 2m ago · click` (or `Open your message`
when the row is yours, and `now` with no "ago" when it just arrived). The message is brought
into view and lifted for a moment; nothing here takes the keyboard, and nothing opens a new
window. If a hosted conversation's history is still arriving, the jump waits up to ten seconds
for its message. A message from before the conversation's history opens it at the bottom, and the hint
line says `that message is older than this chat's history`.

In a member's chat the Traffic is the messages to or from that member (and to the whole team),
one line each, newest first, and that member is `you`: `◆ → you  parser numbers?  2m` from the
manager, `you → ◆  ✓ p50 41ms, p99 180ms  now` back, `you → @gravity  rebase done  3h` to another
member. Press the member's own line to scroll this chat to it. Press a line the manager wrote
to open the manager at it. General, laid open, uses the same `from → to` and the same age on
each of its lines.

`alt+l` puts the column away and brings it back, and this window remembers the answer (`ctrl+g`
is the same key under its older name). Put away, the column is an edge down the right with a
count of what arrived in the Traffic since you last looked; press it or `alt+l` to bring it
back. On a window under 100 columns there is no column and no edge: `alt+l` lays the column
over the conversation, and `esc` or `alt+l` takes it off again.

Over `--host` teams and the manager work as they do locally. The teams and their Traffic are
kept on the machine the conversations run on, and the window reads and writes them there, so
the Traffic is the team's own and a manager set from the laptop is the one the far session
follows. Against a far machine running an older codeaf, `+ AI manager` and the menus say managers
are not available over `--host`, and the column has no Traffic word.

## When a manager starts a member — team_start and approvals

When the manager starts a member with `team_start`, the conversation's approval posture
applies. The default `◇ YOLO` lets it start without a card; `◇ asks` raises a permission
card headed `◆ manager wants to start @lexer`, with the brief under it and the clause
`a new conversation; it spends until it stops`. With a card, `1 allow once` permits this
start, `2 always` permits this tool for the session, `3 deny` refuses, and `esc` leaves it
for later. An allowed start opens the new conversation in the
team's folder **behind** the one you are in, never in front of it: what you were typing stays
where it was. Its tab arrives at the end of the team's run, named `@lexer` until it has a title,
and its working mark is the only thing that moves. With the team's auto-wake on, the member
starts on its own: it is handed the brief on its first request, marked `◆ brief from manager`,
and its page shows the brief as a quoted card headed `◆ manager → @lexer`, never as your
message. If opening or configuring the conversation delays its team membership, it still
starts from that brief once it joins; you do not need to send a second message.
With auto-wake off the conversation is still opened behind the one you are in, and
no turn is started; the traffic says `opened @lexer; this team's auto-wake is off, so no turn
was started. It reads the brief when it next runs.`, and the brief arrives on that next turn
the same way. When the manager stops a member,
its current turn ends the way your own Stop would, including when codeaf opened it in the
background without a window. Starting a new member still needs a window holding the manager.

The member runs in the team's folder, which is this workspace, so it carries the
same memory as any conversation there: every line kept for you everywhere, for
this machine and for this project rides in front of its first request and every
one after. The team itself owns none of them, and the manager reads the same
lines the same way. Nothing is fetched later or picked per message. The block is
bounded (what-i-remember, *How many lines ride along*), and a line that would not
fit is refused when it is kept rather than dropped later. The one exception is a
store an earlier build filled with no bound: then the earlier build's rules and
the project's lines ride first, and the conversation says how many did not.

## Who a Traffic row is from and who it is to

Every Traffic row reads `from → to`, then the words. The manager is `◆`. Several recipients
are the first handle and `+2` for the rest. In a member's chat that member is `you`, so a
question to it reads `◆ → you` and its answer reads `you → ◆`. A reply under a thread is the
same shape after `↳` (`↳ @model → ◆  working…  now`). The band's question is the same shape in
amber (`? @model → ◆  keep the old schema?  3m`). How long ago is on the row, at the right,
dim, at every width: `now`, `2m`, `3h`, `1d`, and past a month `30d`, `12w`, `1y`. A narrow
column keeps the arrow and the names,
then the age, and cuts only the words, at a word, with `…`. Press a handle to open that
member at the message. Press anywhere else on the row to open the chat that message belongs
to, at that message. The hint says `Open @model's message · 3m ago · click`.

## How old a Traffic row is, and what pressing it opens

Every Traffic row ends with how long ago it was, dim and at the right: `now` (under a minute),
`2m`, `3h`, `1d`. Past a month it stays a compact age: `30d`, then weeks from six weeks
(`6w`, and `12w` at about three months), then years (`1y`). A task row and a home session switch
to a date after thirty days (`26 Aug`). A Traffic row does not. It is on a thread
(`◆ → @scrape +2  Please provide… ▸  2m`), on a reply (`↳ @model → ◆  ✓ done  4m`), on General,
on the band's question, and on a member's own lines (`you → ◆  now`). A narrow column keeps
the arrow, the names and the age, and cuts only the words, at a word, with `…`.

Press anywhere on the row, not only on a handle, to open the conversation that message belongs
to, at that message. A message the sender wrote opens the sender's chat: `◆ → all` opens the
manager at the directive, and `you → ◆` in a member's chat scrolls that chat to the reply. A
message to you opens the manager's chat. If that chat is already in front, the row scrolls it
and does not open another window. The focus stays where it was. The hint says
`Open ◆'s message · 2m ago · click`, or `Open your message · now · click` for your own line.
`▸` and `▾` still only fold the thread. A handle still opens that member.

A deliberate message jump opens the activity and tool-call groups that contain the target before scrolling to it. Other history stays collapsed.

## What members say without being asked

A member of a team that has a manager tells the manager, through the traffic, three things it
would never write in a message:

- `finished` when a turn ends, `stopped by the person` when you stopped a member that was on
  its manager's work (a directive, or the brief it was started on), `stopped` for every
  other stop (its manager's, codeaf ending it, your pause, or your stop when none of the
  manager's work was under way), and `failed: ` with the error's first line when a turn ends on one;
- when it starts waiting on you: the permission line for a permission prompt (`needs your ok
  to run bash`), or `asks: ` and the question for anything else;
- `no longer waiting` when the last of those questions comes down.

Each is one line of traffic per change, never one per step. They are the session's own words;
nothing you type is ever written to the traffic. `team_status` reads them too, so a member held
on a permission prompt shows as `asking` rather than `running`, for as long as some process
holds that conversation's transcript and the wait is under 30 minutes.

When you stop a member that is on the manager's work, its manager is woken once and told
the work is not done, that you chose to stop it, and not to hand it out again unless you ask.
The work is under way from the hand-off until a turn finishes with no background command
left that the member waits on. So a turn such a command's ending starts is still on it, and
so is a turn you opened by typing to the member while it waits on one. Stopping a member
with none of the manager's work under way tells the manager nothing, and neither does pausing
a member, codeaf quitting or a paused member's stray wake being ended. The global
manager is not woken by a member of `All teams` being stopped (see **The global manager**).

## A stopped member's command is still running — why it did not start again when the command ended

Stopping a turn leaves a command it started in the background running, as it does in any
conversation. In a crew, that command's ending does not start the conversation again. From
a stop of a running turn (yours, or the manager's `team_stop`, with or without a window on
the member) until a turn next begins, the
ending of a background command, a watch firing, or a background task landing is written into
the conversation as a record and starts no turn. This holds for a member and for the manager.

A member often starts a long command, ends its turn and waits to be told how it ended. It
reads idle, and a stop still reaches it: **Stop** on a crew's row stops such a member too,
the command's ending starts no turn, and its manager is told the work it handed out was
stopped, as for a stopped turn. That is said only for a command the member asked to hear the
end of, or a watch that has not fired. A command left running for its own sake, such as a dev
server, is held the same way and the manager is told nothing: the work before it was finished.
The manager's `team_stop` holds such a member the same way, and the manager is told nothing.

Your own message to the conversation and the manager's next hand-off still start it. That
turn ends the hold and reads the record, which says it arrived after the stop and is no reason
to take the stopped work up again.

To end the command itself, use **Stop all** on the conversation's Processes tab. A
conversation in no crew is unchanged: there a command's ending still starts its turn.

## Why a member stays asking after it crashed

It does not. `asking` means a live process is held on the prompt. If that process dies, the
transcript lock is free and the member reads as idle. The same happens when the wait is older
than 30 minutes, even if a process still holds the transcript: nothing is still asking.

## Who a member reports to

A conversation can be in more than one team, but it **reports to** at most one manager, its
home: the nearest manager above it, picked for you and never changed by itself. Its home
manager directs it; every other manager whose team it is in is a **link**, which may read it
(`team_read`) and send it a note, and nothing more. A link's `team_send` of a directive, and
its `team_stop`, are refused with a sentence saying whose the member is; a directive to
`everyone` goes to the members who report to that manager and names the ones it left out.
In `team_status` a shared member reads `reports to dock`, and `busy for dock` while it runs.
Removing the reporting membership leaves it independent; remaining memberships allow notes
only until an explicit reporting reassignment. Its roster and status say `independent; notes only`.

## Who outranks whom

Your own words come first. What you say in a member's own conversation stands over anything
the manager tells it, then the manager's directives, then other members' messages. A member
reads a manager's message marked `◆ from manager` and a teammate's marked `from @web`, never
as if you had typed it.

The manager **cannot answer a member's permission prompt**. Those are yours; a member waiting
on one waits for you.

## The manager's verbs

| Verb | What it does | Asks you first |
|---|---|---|
| `team_status` | every member's handle, role, title and state (running, asking, idle, failed, paused by the person, on standby), a directive it has not started on, the question waiting, the files touched, and recent traffic | no |
| `team_read` | the end of one member's conversation, bounded; the member is not told | no |
| `team_send` | a message to one member, to several (one message, every handle in `to`), or to everyone, as a note (information, which waits) or a directive (an instruction, which starts an idle member). The answer to a directive says for each member whether it started, is already in a turn, or did not start and why | no |
| `team_stop` | ends one member's current turn, the way your own Stop does, whether a window has it open or codeaf opened it in the background: nothing is deleted, and its background tasks and jobs keep running | no |
| `team_add` | list saved conversations (optional query), then add an existing conversation by its returned id or transcript path; context and existing reporting authority remain | under the approval posture |
| `team_remove` | release one membership by its handle; preserve the session, work and other teams. Removing the reporting membership leaves it independent. Replace the manager before removing it | under the approval posture |
| `team_start` | a new member conversation with a handle and a brief; it opens in the team's folder and is handed the brief, marked as the manager's, on its first request. With kind `team` it starts a sub-team instead (see **Sub-teams**) | under `◇ asks` |
| `team_decide` | answers a decision packet waiting on the manager, most often a member's question: an option, or its own words | no |
| `team_escalate` | sends a packet waiting on the manager up, to its own manager or to you, with the reason it is not the manager's to decide | no |
| `team_close_report` | brings you the team's closing report (done, left, where the files are) after you asked it to wrap up | no |
| `team_dissolve` | takes back a team that has never held anyone but its manager, when you decline it; the team is forgotten and the conversation goes on alone. Only offered while the team is that empty (see **Cancel a team nobody joined**) | no |
| `team_link` | records a file, pull request, issue or job the crew made as the crew's work, so the desktop app lists it with the crew (see **Link what the crew made**) | no |
| `team_raise` | raises a conflict to the manager above every party (see **Conflicts between members and teams**); members have it too | no |

A manager hands work to members. It is told a task (`propose_task`) is not a member: a task
is for work no member is there for, or for when you ask for one, and never a way round a
member that did not start. When a member did not start, `team_send` says why, and the
manager is to tell you that.

## Link what the crew made — team_link, add a file or pull request to the crew

A manager records what its crew produced with `team_link`: `ref` is a file path (relative to
the manager's folder, or absolute; it must exist and be a file), a pull request or issue URL,
or a job id, and `kind` says which (`file`, `pr`, `issue`, `job`; a file by default). `title`
is how the app names it (a file's own name by default). It answers
`Linked <ref> to "<crew>" as <kind>; the app lists it with the crew's work.` and the item
appears in the crew's work in the desktop app at once. Linking the same thing twice keeps one
item.

The tool is only on a manager's belt where something can keep the record: in the desktop app,
or in an engine started for it. A manager in a terminal chat has no `team_link`, and its
members never have it; a member hands its files to the manager, which links them.

## Adding existing sessions and removing members through the manager

Ask the manager to add an existing conversation or remove a named member. `team_add` without
`conversation` lists up to 40 saved conversations; `query` filters by title or workspace.
Copy a returned id or transcript path into `conversation`. Ambiguous ids and sessions absent
from the listing are refused. Use `team_start` for a new session.

`team_remove` takes a handle in the selected team and releases only that membership. It
never stops the session or deletes its transcript. The manager cannot remove itself or edit
All teams' automatic manager memberships. Appoint a replacement through Teams first. A
conversation's remaining memberships do not silently acquire reporting authority when its
reporting membership is removed. Its next role note describes it as independent; former
team directives cannot be delivered through the ended membership.

## Why team_start may ask before it starts

`team_start` follows the conversation's approval posture because a new conversation can
spend money; the default `◇ YOLO` allows it, while `◇ asks` raises the card. It is
refused while the team is at its daily cap. Membership tools `team_add` and `team_remove` also follow the conversation's approval posture.
They update the selected team's membership record. The messaging and decision tools keep their
existing approval rules and traffic history. Questions, packets, caps and wrapping up are
on the page **Team questions, decisions and caps**.
Like any tool, each can be set to ask or allow in `/settings` under the tool approvals.

## What happens when a conversation stops being the manager

At its next step, a conversation removed as manager loses `team_status`, `team_read`,
`team_send`, `team_stop`, `team_start`, `team_add`, `team_remove`, `team_decide`, `team_escalate`,
`team_close_report`, and `team_dissolve` if it had it. A team it dissolved with
`team_dissolve` takes them all the same way. If it is still a member of a managed team, it keeps `team_post` and
`team_raise`; outside a team it loses those too. A remembered call to a removed manager
tool says `team_send is no longer one of your tools: this conversation no longer manages a
team.` A removed member tool says `team_post is no longer one of your tools: this
conversation is no longer a member of a team with a manager.` Made a manager again, it gets
the manager tools back at its next step.

## The member's verbs

A member of a team that has a manager has `team_post`: a message to the room (every member and
the manager), to one teammate by handle, or to the manager. Members use it to report progress,
share a finding, ask a teammate, or say they are blocked. It also has `team_raise`, for a
conflict it cannot settle with the other side itself.

## A manager is told when a member moves

Moving a conversation from one team to another, or moving a team under another, writes one
line to the Traffic of each team it touches. The team that lost the member says
`@web moved to harbor`. The team that gained the member says `@web joined from ops`. For a
team moved under another, that is the moved team's Traffic and its new parent's. A manager
reads those lines the next time it wakes, in the Traffic it already reads. Nothing about the
move wakes it by itself, and a move that was refused writes no line.

## Sub-teams

A manager can start a **sub-team**: `team_start` with kind `team`, a name for the new team, a
handle and a brief for its manager, and optionally members of its own team to move into it. You
are asked first under `◇ asks`, on the same permission card as a member start, which then
reads `a new team "backend" under yours, managed by it.` The default `◇ YOLO` starts it
without that card. When allowed:

- the new team is made under the manager's team, with its share of the pool: the parent's daily
  cap times `sub-team share` (`/settings`, **Teams**; 50% by default), written on the new team.
  A parent with no effective cap gives no derived child cap, and the new team follows the
  ordinary cap rules: its own profile-default pool unless an explicit ancestor cap applies;
- the members named move into it, and its manager is the new conversation, which opens behind
  the one you are in like any start, is a member of the parent team, and is handed the brief
  marked as the manager's together with the words `you were started to manage the team
  "backend"`. It makes itself that team's manager and reports to the manager that started it;
- the traffic of both teams says so: the start in the parent's, and in the new team's a line
  naming who made it and who runs it.

It is refused, with the reason, past the team depth (`team depth` in `/settings` under
**Teams**, three levels by default, a team's own override first), under a closed team, at the
team's daily cap, or when the name or the handle is taken.

## Sub-team orders and reports

**Orders go one level down, reports one level up.** A manager directs its own team's members,
and a sub-team's manager is one of them; it never directs a sub-team's members. A `team_send`
directive or a `team_stop` naming one is refused with a sentence that names the sub-team's
manager to send to instead. A message to `everyone` reaches the manager's own members only. A
manager may still read a conversation in a team under its own with `team_read`, naming it
`backend/@parser`. A sub-team's manager reports to the manager above: its `team_post` to the
manager and its own questions go there first.

**A sub-team with no manager of its own** answers to the nearest manager above it: its members'
questions go to that manager, and their `team_post` to the manager reaches it, marked with the
team it came from. Their posts to the room stay in their own team. That manager does not direct
them; give the sub-team a manager, or move them up, for that.

## The global manager

With several top-level teams there is no one manager over all of them until you make one on the
teams page's `All teams` row. That conversation is the **global manager**: the manager of a
team that holds every other team. Its members are the managers of the top-level teams, and
only them: codeaf adds each one to `All teams` for you, its account of its team lists only
those managers, and its directives reach them and never their members. The top-level managers
report to it, so their questions and conflicts between teams come to it before they come to
you. It can start a new top-level team with `team_start` of kind `team`. Without a global
manager, nothing here changes: each top-level manager reports to you.

No turn in `All teams` that finishes or is stopped starts the global manager's turn. You talk
to the top-level managers directly and each answer ends a turn, so the endings stay in
`All teams`' traffic, where `team_status` reads them. The global manager is woken by what a
member posts to it with `team_post`, by a member's question or a permission it is held on, and
by a member's turn failing. So a directive the global manager sent, and the brief it started
a team on, are answered with `team_post`: each top-level manager is told so, and apart from
those answers to post only what the global manager should act on or tell you. A manager that
finishes a turn without posting has told it nothing, the first turn of a team it started
included: `team_start` says so, and `team_status` is where it finds out. The global manager
is told never to send a manager your own request to that manager back as a directive.
`All teams` can also hold a conversation that manages no team now, such as a manager you
replaced. The global manager neither lists nor directs it, its turn ending is as quiet, and
it is told what a member of any team is told.

## Conflicts between members and teams

When two or more conversations need incompatible things (the form posts JSON, the endpoint
takes form data) and cannot settle it between themselves, one of them raises it with
`team_raise`: the question, the other parties by handle (`@api`, or `back/@api` for a member
of another team), its own side, and the options, each with what happens if it is chosen. A
conflict is never detected for you; a party declares it.

It goes, as one decision packet, to the **lowest manager above every party** who is not one of
them, in one hop: two members of one team go to its manager, members of two sibling sub-teams to
the manager of the team both sit under, and two top-level teams to the global manager. With no
such manager it comes to you, in the inbox on the teams page. That manager is woken and handed
the packet whole; the other parties are told it was raised. It rules with `team_decide`, or
sends it up with `team_escalate`, never sideways. **The ruling reaches every party as a
directive**, marked as a ruling on that conflict, in each party's own team's traffic, and wakes
each of them, whoever ruled: a manager, or you. A party never decides its own case, even when
it manages the team the packet waits on.

## Threads: what answers what

Every line a member is handed carries its number, `◆ directive from manager #42: …`. A
member's `team_post` to the manager answers the last message the manager sent it, by itself;
to answer another it names it, `thread: #41`. The member's finishing, failing or asking, and
the wake that started it, answer the same message, which is how the Traffic and the chats draw
them under it. A message to several members is one message, so the question and its answers
are one thread however many were asked.

**In the manager's chat** a `team_send` reads as the head of its thread,
`team_send ◆ to @agent @checking @review · do`, with the message quoted under it and each
answer attached under that in muted ink as it arrives, one line each. Press an answer's words
to read it in full and again to fold it; point at them for the whole text in the hint line; a
handle opens its member at the message: on an answer, at the member's own post, and on the
card's header, at the message as the member was told it. A turn that sent one is not folded into a `worked` chip, so the
thread stays where you can see it. The step over it is captioned as the work it is,
`messaged @agent @checking @review`, never by the tool's name, and a run of sends reads
`sending 3 messages`.

The answers also reach the manager as its team's note. So nothing is shown twice, a note
whose answers are already under their question reads as one dim line,
`· @checking @review answered · in the thread above`; a line that answers nothing is drawn in
full as before.

**In a member's chat** the manager's message is the quoted card it always was, headed
`◆ manager → @web  do`, and the member's own answers to it hang under it the same way.

## When messages arrive

A message reaches a conversation at the start of its next step. When the conversation is
working, that is straight away. When it is idle, it depends on the kind of message:

- A **directive** starts an idle member's turn. The member is handed the directive, marked
  `◆ directive from manager`, never as if you had typed it. A member you paused does not
  start (see **Pausing a crew member**).
- A **note** wakes nobody. An idle member reads it when it next runs, for whatever reason.
- A member's **reply to the manager** (`team_post` to the manager), and a member finishing
  without a reply, failing, starting to wait on you, or being stopped by you while on the
  manager's work, start an idle manager's turn. A reply
  and the end of the same turn are one wake, even if the ending arrives after the manager
  already ran. Replies arriving within a few seconds are gathered into one turn. The global
  manager is the exception: a member of `All teams` finishing or being stopped does not
  start its turn (see **The global manager**).

A member no window has open is opened by codeaf in the background so it can run, and a window
that opens it later joins the running conversation. A manager that moved to another window, and
whose window has since closed, is woken the same way: in the desktop app it is brought back to
the app and run there.

The manager is told what became of each directive in `team_send`'s answer, which waits up to
four seconds to see it: `@web started a turn on it`, `@web is in a turn and reads it at the
next step`, `@web is waiting on the person and reads it once they answer`, `@web did NOT
start:` with the reason, or `@web has shown no sign of starting yet`. A manager reports work
as under way only where the answer says a member started.

## could not wake ◆ — a wake that cannot be delivered is kept, not dropped

When a wake cannot be delivered (the conversation is held by a window that is closing, or
nothing on this computer can open it yet), the traffic says it once:
`could not wake ◆: <why>; codeaf keeps the wake and starts it as soon as it can be reached`
(or `could not wake @web: …`). The message itself is never lost: it waits in the traffic, and
codeaf tries the wake again, first after 15 seconds and then on a doubling wait of at most five
minutes, until the conversation runs. A retry that fails again says nothing more. If a window
opens the conversation first, that window picks up the waiting messages and the retries stop.

codeaf stops trying only when there is nothing left to wake, and says so:
`stopped trying to wake ◆: its crew is closed`, `…: it is no longer in the crew`, or
`…: its transcript is no longer on this machine`. A retry is kept by the running codeaf
process; one that quits before the wake is delivered leaves the messages waiting for the
conversation's next turn.

Every wake is a line in the traffic, `◆ woke @web` or `@web woke ◆`, so the Traffic shows why a
conversation is running. A wake spends through the same limits a turn you start does, and two
more bound it: one conversation is woken at most 20 times an hour, and a manager woken 10 times
by its team with nothing from you stops being woken and asks you instead, as a waiting line in
the traffic. It is woken again after you next say something to it.
The ten wakes count ten replies when each member turn replies and then finishes.

A member already in a turn when a directive reaches it was not woken and gets no `woke` line:
it reads the directive at its next step. A wake the member's own limits refuse says why once
(`could not wake @web: its spending limit is reached; it reads this at its next turn`) and
leaves the directive unread in the traffic for that turn.

## Pausing a crew member — pause, resume, why a directive did not start, held

You can pause a member of a crew in the desktop app: **Pause** on its row in the Members tab
or in the member's menu, and **Resume** to lift it. A pause ends the member's current turn,
bounded as the app's Stop is (a turn that has not let go ten seconds later is let go of),
which ends `stopped` and wakes nobody: a pause is not a stop, so the manager is not told
`stopped by the person` and the work is kept for Resume. The pause is kept on the team's own
record, so the member's session, its manager and every window read the same answer. A
crew's manager, and a member that manages a crew of its own, cannot be paused; stop its turn
instead.

While a member is paused, team traffic starts no turn for it, in any crew it belongs to, and
Resume from any of those crews lifts it:

- A directive waits on the crew's log, unread. The traffic says
  `held @web: the person paused it; it starts on this when they resume it`, once per directive.
  A member paused before its first turn says it once for the brief it was started with.
- `team_send` tells the manager `@web did NOT start` with the same reason, and that only you
  can resume it. The manager cannot lift a pause.
- `team_status` shows the member as `paused by the person; it starts nothing until they
  resume it`, with `has not started on #22` when a directive is waiting.
- codeaf does not open the member's conversation for traffic it would only hold.
- A command ending, a watch firing, a task landing and news at its last turn's end start no
  turn either: each is written into the conversation as a record that says it arrived while
  the conversation was paused.

## Resume after a pause — what starts again, the paused work carried on, a stop while paused

**Resume** writes `resumed` to the log of each crew the member is in, and a directive that
waited starts the member's turn within a second or two, including after codeaf was closed in
between. With the team's auto-wake off, a resumed member reads it at its next turn like any
other message.

With no directive waiting, Resume starts one turn when the pause ended a turn, whatever
started it, or held a record. That turn opens with "The person paused this conversation
and has now resumed it, which started this turn. Carry on with the work you were on; anything
above that arrived while it was paused is a record of what happened meanwhile." A member
paused while it waited on a command for that work starts again when the command ends, or at
Resume if it ended meanwhile. The crew's hourly wake limit and daily cap hold that turn as
they hold a directive, and it starts once they allow. That turn is kept in memory only: if
codeaf is closed while the member is paused, Resume starts only a directive that waited.

A pause is temporary and a stop is final. **Stop** on a paused member that is in a turn, or
whose pause cut a turn or waits on a command, ends the work as any stop does: its manager is
told `stopped by the person` when the work was the manager's, and Resume starts nothing, not
even on a directive that waited; that directive is read at the member's next turn. A
directive sent after the Stop still starts the member at Resume. A Stop that finds nothing
to end changes nothing about Resume.

Not held: your own words, including one queued behind the turn Pause ended (that turn reads
what was waiting, so Resume starts nothing more), your answer to its question, a decision you
hand it, and codeaf continuing a reply it was
closed in the middle of. An earlier app's pause moves onto the team's record when the
updated app first opens.

## A new crew's members wait on standby — why nobody started after Make the crew

A crew made from a proposal (**Bring in helpers**, then **Make the crew**) joins on standby.
Each helper is created with its handle and role, and nobody begins work: the proposal
describes who the crew is, and accepting it is not a request to start.

- The traffic says `opened @web on standby; it starts on its manager's first directive`.
- `team_status` shows `on standby; no work handed out yet` until the member's first turn.
- The line the member was made with is handed to it at its first turn marked
  `◆ your standing role from manager`, together with the directive that started that turn.
- A member's role name is told to it in its team note (`Your role in this team: …`) and shown
  to the manager beside the handle, `@web (Frontend developer)`.
- Standby lasts across restarts: however much later the first directive comes, the member is
  still handed its role line with it.

The first work starts when you ask the manager for it and it sends a directive. A member the
manager starts itself with `team_start`, or one you add with a brief from the crew's page,
still starts on its brief at once.

## How do I turn team message wakes off?

A team's auto-wake can be turned off. `team messages wake` in `/settings` under **Teams** is
the default every team inherits (on), and a team can override it for itself and the teams
under it: its entry in the profile's teams store, `teams.db`, carries `"wake": false` (or `true`). With it
off, every message waits for each conversation's next turn, and a member the manager starts
with `team_start` is opened and handed its brief on that next turn, with no turn started for
it. The traffic says so.

A member busy in one long command reads a message when that command returns, which is what
`team_stop` is for. Nothing is delivered twice, and a conversation that joins a team is not
handed the team's earlier history. A new member receives only the start that created its
membership from before it joined; reusing a removed member's handle does not bring back that
member's brief. A conversation reopened later is handed what was said to it
while it was closed.

## Team sharing and Traffic on another computer

Hosted sync storage approval is required for team sharing; storage is off by default while its privacy terms are being finished. Pairing alone does not enable it: teams, shared team settings and Traffic wait. Pairing, device presence and live `--at` still work. After pairing, press enter on **Settings → Devices → sync storage** to ask for approval. A self-hosted sync service with `--store` needs no request.

The traffic itself is kept in the profile of the machine the conversations run on, in
`teams/<id>/traffic.jsonl`, one line per message, only ever added to. It holds only
messages written on this computer. Team sharing is on by default for paired
computers through **Settings → General → share teams**. Turning it off on either
computer stops teams moving between them: that computer stops sending and receiving.
What was already exchanged stays, including encrypted copies in the vault. History
from the other computer is hidden while sharing is off. Unpaired computers are unaffected.

Once both computers share teams, Traffic also shows recent history from the other
computer, kept beside it in `teams/<id>/elsewhere.jsonl`. That history
cannot deliver a message, start, stop, answer or wake a chat here. A message to a
member or manager known to be on another computer is refused before writing;
continue that chat here from Home first, then send again. A room post or send to
several members reaches local recipients and names those not reached. If all
recipients are elsewhere, nothing is written. An unavailable relay does not
block local delivery. Decisions and escalations also check their recipients
before changing anything. Over `--host` or `--at`, a remote member shows an
"on the other computer" notice; open it there. A history link whose message
cannot be found says it is on the other computer's history.

## Which approval rules do new members inherit?

A member started by the manager inherits the manager's approval posture before
it joins and receives work. The conversation saves its own setting for resume.
If the new conversation cannot apply the posture, the start is refused and the
manager sees the reason. An untitled manager receives an available handle from
the team store, so it can be addressed immediately.

## Quiet updates from the team

Team exchanges are work details: they stay in the compact activity view while
work runs and fold behind the work line afterwards. Open the work line and its
step to read the complete exchange, or open Traffic. The manager is instructed
to tell you meaningful new results, blockers, decisions, and requested updates,
without repeating an unchanged status. Its exact `[no change]` response is kept
in the model record but does not add another answer to your conversation.

## Opening a new member’s brief from Traffic

Press a new-member Traffic row’s words to reveal the accepted `team_start` call in
the manager’s conversation. Its receipt carries the message number. Press the
member’s handle to open the delivered brief in that member’s conversation.
Collapsed work opens at the selected message. Older unnumbered receipts are
matched only when the accepted handle and brief identify one successful start
in the current team; ambiguous or missing history is not guessed.

## Managing one team and its subteams

One conversation may manage a team and descendants of that team, while remaining an
ordinary member of other teams. It cannot manage unrelated teams. The global manager is
separate from every ordinary team's manager. Choose AI manager or a team move refuses a
change that introduces conflicting responsibilities, naming the teams. A sharing merge
clears conflicting appointments automatically. A cleared appointment goes into
former-manager history only when that slot is empty or already holds it. An existing
former appointment stays; the copy made before the first exchange keeps the original teams.
Review manager choices in Teams.

## Shared team spending caps and continued members

On the hosted sync service this needs sync storage approval (Settings → Devices → sync storage).

Teams and their settings share automatically between paired computers by default;
an unpaired computer is unaffected. A shared team's spending cap counts spending
on each computer separately. A member continued on the other computer is metered
there. The cap does not combine ongoing spending across computers, so inspect
each computer's spending when work is split between them.

## First sharing merge and two global managers

Before the first exchange, this computer keeps a copy of its teams as
`teams.before-sharing.json` beside the original, once, never replacing an earlier copy.
If both computers had a team with the same name, both appear unchanged. Rename
one in its Settings to tell them apart.

If both computers had a global manager, the older global team stays global; the
other becomes an ordinary team under it and keeps its active manager. Demotion
does not add that active manager to former-manager history. Cleared conflicting
appointments enter that history only if the slot is empty or already holds the
same appointment. An existing former appointment stays; the copy made before the
first exchange keeps the original teams. Review manager choices in Teams.
