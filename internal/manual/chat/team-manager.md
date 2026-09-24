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
which resumes it first when this window does not have it open.

## Making a manager

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

Headers and answers end with their age (`now`, `2m`, `3h`); on a narrow rail only the header
does. Your own messages are not on the rail; they are in the manager's conversation. Every
`@handle` is a link, as in the chat: point at it for the member's title in the hint line, press
it to open that member (resumed first when this window does not have it open). Point at a
message's words to read them whole in the hint line, press them to lay them out in full under
the row, and press again to fold them. Nothing on the rail moves your focus but a handle.

The rail takes the right-hand column while the manager is in front. The task column folds to its
edge beside it; press that edge or `ctrl+g` to bring the tasks back, which puts the Traffic away.
The rail's header reads `Traffic` with `hide alt+l` at its right. Put away, the rail is the word
`Traffic` down the right edge with a count of what arrived since you last looked; press it or
`alt+l` to bring it back. This window remembers whether you put it away.

On a window too narrow for the column (under about 84 columns after the task column's edge) the
rail is only that edge. Pressing it, or `alt+l`, lays the Traffic over the lower part of the
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
| `team_start` | a new member conversation with a handle and a brief; it opens in the team's folder and is handed the brief, marked as the manager's, on its first request | yes |

`team_start` asks because a new conversation spends money for as long as it runs. The others
act only inside the team you made, and every one of them is logged in the team's traffic.
Like any tool, each can be set to ask or allow in `/settings` under the tool approvals.

## The member's verb

A member of a team that has a manager has `team_post`: a message to the room (every member and
the manager), to one teammate by handle, or to the manager. Members use it to report progress,
share a finding, ask a teammate, or say they are blocked.

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
handle opens its member. A turn that sent one is not folded into a `worked` chip, so the
thread stays where you can see it.

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

A team's auto-wake can be turned off: its entry in `teams.json` in the profile carries
`"wake": false`. With it off, every message waits for each conversation's next turn, and a
member the manager starts with `team_start` is opened and handed its brief on that next turn,
with no turn started for it. The traffic says so.

A member busy in one long command reads a message when that command returns, which is what
`team_stop` is for. Nothing is delivered twice, and a conversation that joins a team is not
handed the team's earlier history. A conversation reopened later is handed what was said to it
while it was closed.

The traffic itself is kept in the profile of the machine the conversations run on, in
`teams/<id>/traffic.jsonl`, one line per message, only ever added to.
