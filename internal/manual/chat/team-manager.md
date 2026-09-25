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

Every member has a **handle**: one lowercase word that says what the conversation is about,
like `@security` for `santosh dev2 branch code complexity & security review`, `@milestones`
for `CodeAF repo issue tags & milestones` or `@gravity` for `quantum gravity research updates`.
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
conversations view on that team instead, and the hint says so.

## Making a manager

The **teams page** (`/teams`, `alt+2`) offers `+ Manager` on every team that has none, and
`All teams` offers one manager over every team. Choosing a team there puts its manager's
conversation in the page's pane, where you talk to it beside the tree of your teams.

A team has no manager until you make one, and until then nothing about managers costs anything.
While a team is shown, the first place on the tab strip is the manager's, pinned at the left
like a pinned browser tab so it never scrolls away:

- **`+ Manager`**, a quiet button, while the team has none. A press starts a new conversation
  in the team's folder and makes it the manager. Point at it and the hint line says so. On a
  window under 100 columns the button leaves the strip; the team switcher still offers it.
- **`◆ Make this harbor's manager`** in the team switcher (the `● harbor ▾` chip) makes the
  conversation in front the manager, and in a tile's Teams list on the conversations view makes
  that one the manager. When the team already has one, the row says which one it replaces:
  `◆ Make this harbor's manager (replaces Shipping the parser)`. On the manager itself the row
  reads **`◇ Make an ordinary member`**, which turns it back into an ordinary conversation with
  all its history.

Once there is a manager the place reads **`◆ Manager`**, and on the conversations view its tile
comes first, titled `◆ Manager · <its title>`. Point at the tab to see the team and the title in
the hint line. `alt+m` goes to the manager from any conversation in the team.

## The manager's screen

While the manager is in front, the message box says `to ◆ manager`, and keeps saying it on the
rule above the box once you start typing: everything you type goes to the manager and nowhere
else. With a member in front the box says `to @web` the same way.

On the right is the **Traffic** rail: who told whom inside the team, as threads. A thread is
one message and everything that answered it, and the thread that moved last is at the top,
straight under the header:

```
Traffic                                  hide alt+l
◆ manager → @agent @checking @review  do        2m
  Please provide a brief status update on your part
├ @checking  ✓ Status update: the lexer is in   1m
├ @agent  working…
└ @review  asking: may I run the migration?    now
```

- The header says who sent it, to whom, and `do` for a directive or `fyi` for a note. When the
  rail is too narrow for every handle it names whom it can and counts the rest, `+2`.
- The message is on its own dim line under it.
- Each member who answered, or was started on it, has one line of the tree, in the order it
  happened. `working…` is a member the message woke that has not answered yet; `✓` is a member
  that answered and finished its turn, `✗` one whose turn failed, and `asking:` is a member
  waiting on you, the only line in the needs-you amber.
- `◆ manager stopped @web · going in circles`, `◆ manager started @lexer` with the brief under
  it, and `codeaf  @review is now @security` are lines of their own, as is anything written
  before threads.
- A start that made a sub-team reads `◆ manager started @api to run backend`. The name is
  the team's name now, so a rename replaces `backend` on the next frame.
- `◆ manager ruling → @web` (or `you ruling → @web` when you decided it) heads the decision on
  a conflict, written into every party's team, with the ruling on its own line under it: it is
  a ruling, not that team's manager's own order, and pointing at its words names the conflict
  it settles.
- A member's clarifying question to its manager reads `@web → ◆ manager  asks`, and the
  manager's answer is a line of the tree under it. A decision packet raised, decided or
  escalated (`codeaf  answered @web: JSON`), and a team closing or reopening, are lines of
  their own.

Headers and answers end with their age (`now`, `2m`, `3h`); on a narrow rail only the header
does. Your own messages are not on the rail; they are in the manager's conversation. Every
`@handle` is a link, as in the chat: point at it for the member's title in the hint line, press
it to open that member (resumed first when this window does not have it open) **scrolled to the
message**: a handle on a thread's header opens the member at the directive as it was told it,
and a handle on an answer opens it at its own post. The message is brought into view and
lifted for a moment; the focus stays on that conversation. A message from before the
conversation's history opens it at the bottom, and the hint line says `that message is older
than this chat's history`. Point at a message's words to read them whole in the hint line,
press them to lay them out in full under the row, and press again to fold them; the press also
brings that thread's card in the manager's conversation into view. Nothing on the rail moves
your focus but a handle.

With the manager in front the right-hand column is the Traffic, whatever you last told the task
column with `ctrl+g`: the task column is not drawn beside it, not even as an edge. When the
manager has live tasks of its own the header grows a second word, `Traffic · Tasks 2`; press
`Tasks 2`, or `ctrl+g`, to lay the manager's tasks in the same column at the same width, and
press the header line, or `ctrl+g` again, to go back to the Traffic. With no tasks there is no
second word and `ctrl+g` does nothing here. Every other conversation's task column works as it
always has. The rail's header reads `Traffic` with `hide alt+l` at its right. Put away, the rail is the word
`Traffic` down the right edge with a count of what arrived since you last looked; press it or
`alt+l` to bring it back. This window remembers whether you put it away.

On a window too narrow for the column (under 84 columns) the rail is only that edge. Pressing it, or `alt+l`, lays the Traffic over the lower part of the
conversation as a card with `Close esc` in its foot; the top of the conversation stays in view.
`esc` closes it.

Over `--host` teams and the manager work as they do locally. The teams and their Traffic are
kept on the machine the conversations run on, and the window reads and writes them there, so
the rail is the team's own and a manager set from the laptop is the one the far session
follows. Against a far machine running an older codeaf, `+ Manager` and the menus say managers
are not available over `--host`, and there is no rail.

When the manager starts a member with `team_start`, you are asked first, on a card that reads
`◆ manager wants to start @lexer`, with the brief under it and the clause `a new conversation;
it spends until it stops`. When you allow it, this window opens the new conversation in the
team's folder **behind** the one you are in, never in front of it: what you were typing stays
where it was. Its tab arrives at the end of the team's run, named `@lexer` until it has a title,
and its working mark is the only thing that moves. With the team's auto-wake on, the member
starts on its own: it is handed the brief on its first request, marked `◆ brief from manager`,
and its page shows the brief as a quoted card headed `◆ manager → @lexer`, never as your
message. With auto-wake off the conversation is still opened behind the one you are in, and
no turn is started; the traffic says `opened @lexer; this team's auto-wake is off, so no turn
was started. It reads the brief when it next runs.`, and the brief arrives on that next turn
the same way. When the manager stops a member,
this window stops it the way your own Stop would. Both happen only in a window that has those
conversations open.

## What members say without being asked

A member of a team that has a manager tells the manager, through the traffic, three things it
would never write in a message:

- `finished` when a turn ends, `stopped` when somebody stopped it, and `failed: ` with the
  error's first line when a turn ends on one;
- when it starts waiting on you: the permission line for a permission prompt (`needs your ok
  to run bash`), or `asks: ` and the question for anything else;
- `no longer waiting` when the last of those questions comes down.

Each is one line of traffic per change, never one per step. They are the session's own words;
nothing you type is ever written to the traffic. `team_status` reads them too, so a member held
on a permission prompt shows as `asking` rather than `running`, for as long as some process
holds that conversation's transcript and the wait is under 30 minutes.

## Why a member stays asking after it crashed

It does not. `asking` means a live process is held on the prompt. If that process dies, the
transcript lock is free and the member reads as idle. The same happens when the wait is older
than 30 minutes, even if a process still holds the transcript: nothing is still asking.

## Who a member reports to

A conversation can be in more than one team, but it **reports to** exactly one manager, its
home: the nearest manager above it, picked for you and never changed by itself. Its home
manager directs it; every other manager whose team it is in is a **link**, which may read it
(`team_read`) and send it a note, and nothing more. A link's `team_send` of a directive, and
its `team_stop`, are refused with a sentence saying whose the member is; a directive to
`everyone` goes to the members who report to that manager and names the ones it left out.
In `team_status` a shared member reads `reports to dock`, and `busy for dock` while it runs.

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
| `team_status` | every member's handle, title and state (running, asking, idle, failed), the question waiting, the files touched, and recent traffic | no |
| `team_read` | the end of one member's conversation, bounded; the member is not told | no |
| `team_send` | a message to one member, to several (one message, every handle in `to`), or to everyone, as a note (information, which waits) or a directive (an instruction, which starts an idle member) | no |
| `team_stop` | ends one member's current turn, the way your own Stop does: nothing is deleted, and its background tasks and jobs keep running | no |
| `team_start` | a new member conversation with a handle and a brief; it opens in the team's folder and is handed the brief, marked as the manager's, on its first request. With kind `team` it starts a sub-team instead (see **Sub-teams**) | yes |
| `team_decide` | answers a decision packet waiting on the manager, most often a member's question: an option, or its own words | no |
| `team_escalate` | sends a packet waiting on the manager up, to its own manager or to you, with the reason it is not the manager's to decide | no |
| `team_close_report` | brings you the team's closing report (done, left, where the files are) after you asked it to wrap up | no |
| `team_raise` | raises a conflict to the manager above every party (see **Conflicts between members and teams**); members have it too | no |

`team_start` asks because a new conversation spends money for as long as it runs, and it is
refused while the team is at its daily cap. The others act only inside the team you made, and
every one of them is logged in the team's traffic. Questions, packets, caps and wrapping up are
on the page **Team questions, decisions and caps**.
Like any tool, each can be set to ask or allow in `/settings` under the tool approvals.

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
are asked first, on the same card as any start, which then reads `a new team "backend" under
yours`. When you allow it:

- the new team is made under the manager's team, with its share of the pool: the parent's daily
  cap times `sub-team share` (`/settings`, **Teams**; 50% by default), written on the new team.
  A parent with no cap gives none, and the new team spends from whatever pool is above it;
- the members named move into it, and its manager is the new conversation, which opens behind
  the one you are in like any start, is a member of the parent team, and is handed the brief
  marked as the manager's together with the words `you were started to manage the team
  "backend"`. It makes itself that team's manager and reports to the manager that started it;
- the traffic of both teams says so: the start in the parent's, and in the new team's a line
  naming who made it and who runs it.

It is refused, with the reason, past the team depth (`team depth` in `/settings` under
**Teams**, three levels by default, a team's own override first), under a closed team, at the
team's daily cap, or when the name or the handle is taken.

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
the wake that started it, answer the same message, which is how the rail and the chats draw
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
  `◆ directive from manager`, never as if you had typed it.
- A **note** wakes nobody. An idle member reads it when it next runs, for whatever reason.
- A member's **reply to the manager** (`team_post` to the manager), and a member finishing,
  failing or starting to wait on you, start an idle manager's turn. Replies that arrive within
  a few seconds of each other are gathered into one turn rather than one turn each.

A member no window has open is opened by codeaf in the background so it can run, and a window
that opens it later joins the running conversation. When that cannot be done, the traffic says
`could not wake @web:` and why, and the message waits for the member's next turn.

Every wake is a line in the traffic, `◆ woke @web` or `@web woke ◆`, so the rail shows why a
conversation is running. A wake spends through the same limits a turn you start does, and two
more bound it: one conversation is woken at most 20 times an hour, and a manager woken 10 times
by its team with nothing from you stops being woken and asks you instead, as a waiting line in
the traffic. It is woken again after you next say something to it.

A team's auto-wake can be turned off. `team messages wake` in `/settings` under **Teams** is
the default every team inherits (on), and a team can override it for itself and the teams
under it: its entry in `teams.json` in the profile carries `"wake": false` (or `true`). With it
off, every message waits for each conversation's next turn, and a member the manager starts
with `team_start` is opened and handed its brief on that next turn, with no turn started for
it. The traffic says so.

A member busy in one long command reads a message when that command returns, which is what
`team_stop` is for. Nothing is delivered twice, and a conversation that joins a team is not
handed the team's earlier history. A conversation reopened later is handed what was said to it
while it was closed.

The traffic itself is kept in the profile of the machine the conversations run on, in
`teams/<id>/traffic.jsonl`, one line per message, only ever added to.
