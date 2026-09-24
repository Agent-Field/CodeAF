# The team manager

## What a manager is

A team can have one **manager**: a conversation that runs the team for you. You talk to the
manager, and it hands work to the team's members, keeps track of what each is doing, and tells
you where things stand. It is an ordinary conversation with every ordinary tool, under the
same permission rules as any other; what makes it the manager is the team verbs below and a
short account of its team that it carries on every turn: each member's handle and state, the
question a member is waiting on, the files each has touched, and the last few lines of the
team's traffic. It never carries members' whole conversations.

Every member has a short **handle**, like `@web` or `@parser`, made from its title when it
joins. Messages in a team are addressed by handle.

## Making a manager

A team has no manager until you make one, and until then nothing about managers costs anything.
While a team is shown, the first place on the tab strip is the manager's:

- **`+ Manager`**, a quiet button, while the team has none. A press starts a new conversation
  in the team's folder and makes it the manager.
- **`◆ Make manager`** in the team switcher (the `● harbor ▾` chip) makes the conversation in
  front the manager, and in a tile's Teams list on the conversations view makes that one the
  manager. On the manager itself the row reads **Remove manager**, which turns it back into an
  ordinary conversation with all its history.

Once there is a manager, the first tab reads `◆ harbor`, and on the conversations view its tile
comes first, marked `◆`.

## The manager's screen

While the manager is in front, the message box says `to ◆ manager`: everything you type goes
to the manager and nowhere else. Beside the conversation, on the right, is the **Traffic** rail,
the log of who told whom inside the team: `◆ → @web` for the manager's messages, `@parser →
@web` for a member's, and a line for each stop and start. Press a row to go to that member.
On a narrow window the rail folds to a `◆` at the right edge; press it to lay the traffic over
the conversation, and again to put it away.

When the manager starts a member with `team_start`, you are asked first, on a card that reads
`◆ manager wants to start @lexer`, with the brief under it and the clause `a new conversation;
it spends until it stops`. When you allow it, this window opens the new conversation in the
team's folder and gives it its handle. The member is handed the brief on its first request,
marked `◆ brief from manager`: it is the manager's assignment, never your message, and the
member's page shows it as the manager's. When the manager stops a member, this window stops it
the way your own Stop would, and the reason the manager gave is kept in the traffic. Both
happen only in a window that has those conversations open.

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
| `team_send` | a message to one member or to everyone, as a note (information) or a directive (an instruction) | no |
| `team_stop` | ends one member's current turn, the way your own Stop does: nothing is deleted, and its background tasks and jobs keep running | no |
| `team_start` | a new member conversation with a handle and a brief; it opens in the team's folder and is handed the brief, marked as the manager's, on its first request | yes |

`team_start` asks because a new conversation spends money for as long as it runs. The others
act only inside the team you made, and every one of them is logged in the team's traffic.
Like any tool, each can be set to ask or allow in `/settings` under the tool approvals.

## The member's verb

A member of a team that has a manager has `team_post`: a message to the room (every member and
the manager), to one teammate by handle, or to the manager. Members use it to report progress,
share a finding, ask a teammate, or say they are blocked.

## When messages arrive

A message reaches a conversation at the start of its next step: straight away when it is
working, and when it next runs when it is idle. A member busy in one long command reads it when
that command returns, which is what `team_stop` is for. Nothing is delivered twice, and a
conversation that joins a team is not handed the team's earlier history. A conversation
reopened later is handed what was said to it while it was closed.

The traffic itself is kept in your profile, in `teams/<id>/traffic.jsonl`, one line per
message, only ever added to.
