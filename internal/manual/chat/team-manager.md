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

Every member has a short **handle**, like `@web` or `@parser`, made from its title when it
joins: the word that says what the conversation is about, so `checking branches for the qa
binary` becomes `@qa-binary` and `Fix the login bug` becomes `@login`. A handle is never changed after that, so a line in the traffic keeps meaning the
member it meant. Messages in a team are addressed by handle.

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

On the right is the **Traffic** rail, the log of who told whom inside the team, newest at the
bottom:

- `◆ → @web  do  take the scope model` for a directive, and `fyi` for a note;
- `@parser → @web  fyi  the lexer is in` for a member's message;
- `◆ stopped @web  going in circles` and `◆ started @lexer  rewrite the lexer…`, with the
  reason and the brief;
- `@web  finished`, `failed`, and `asking`, which is the only line in the needs-you amber.

Each row ends with its age (`now`, `2m`, `3h`). Your own messages are not on the rail; they are
in the manager's conversation. Point at a row to read its whole text in the hint line, and press
it to go to that member; a row about the whole team (`◆ → all`) is not a door.

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
and its working mark is the only thing that moves. The member starts on its own: it is handed
the brief on its first request, marked `◆ brief from manager`, and its page shows the brief as a
quoted card headed `◆ manager → @lexer`, never as your message. When the manager stops a member,
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
on a permission prompt shows as `asking` rather than `running`.

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
| `team_send` | a message to one member or to everyone, as a note (information, which waits) or a directive (an instruction, which starts an idle member) | no |
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
off, every message waits for each conversation's next turn.

A member busy in one long command reads a message when that command returns, which is what
`team_stop` is for. Nothing is delivered twice, and a conversation that joins a team is not
handed the team's earlier history. A conversation reopened later is handed what was said to it
while it was closed.

The traffic itself is kept in the profile of the machine the conversations run on, in
`teams/<id>/traffic.jsonl`, one line per message, only ever added to.
