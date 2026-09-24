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
| `team_start` | a new member conversation with a handle and a brief; it opens in the team's folder and the brief is its first message | yes |

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
