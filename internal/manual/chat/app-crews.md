# Crews and members in the codeaf app

## Bring in helpers — propose a crew, keep the same chat

The app engine's `POST /app/v1/agents/{id}/bring-in-helpers` asks that agent
for an editable crew proposal. The agent must be idle and outside a crew.
Nothing forms until `POST /app/v1/crews` accepts its `proposal_id`.
The existing agent becomes the manager and keeps its conversation and history.
A proposal has a name, one of six tints, helpers with roles/models/caps, a
crew budget and an optional end date. Proposals expire after 24 hours;
a changed source requires a fresh proposal. Accepting one twice reuses its crew.

## Make a crew from agents — history stays in each chat

`POST /app/v1/crews` with `from:"agents"` needs at least two selected agents,
a name, a goal, and a manager choice: `"new"` or one selected agent id.
The manager gets short handover notes and writes its own summary.
Histories are kept separately. An agent already in another live crew joins
as a new conversation carrying its role. A crew has at most 64 members.

`GET /app/v1/crews` lists crews; `GET /app/v1/crews/{cid}` gives its member tree.
`PATCH /app/v1/crews/{cid}` changes name, tint, goal, budget or end date.
Names have 1–40 characters. Tints are iris, sage, sand, tide, rose and graphite.
A fresh crew's budget is $20. Zero spend and an idle state line draw nothing.

## Replace manager — every crew keeps one manager

`POST /app/v1/crews/{cid}/replace-manager` chooses a member or a role.
The former manager stays a member. Removing or moving the current manager
returns 409 with `a crew must have a manager; use replace-manager`.
The replacement gets a directive to read the former manager's notes and log.

## Add, pause, move and remove a member — caps and instructions

Member routes add an existing agent or a role, change instructions/cap,
pause, resume, move to another crew, or remove from this crew.
Removing keeps the agent's history and returns it to Agents, idle.
A duplicate member returns 409 `already in <crew name>`.
Paused members need Resume before messaging.

Caps are positive USD cents and cannot exceed their crew's budget.
At its cap a member stops; at the crew budget all its members stop.
The binary saves one budget question; raising that cap clears it.
Crew spending starts when members join, excluding earlier chat work.
Removing a member keeps its accrued spend; work done while a crew is closed
does not charge that crew when it reopens.

## Message any member and questions climb — app resource availability

Each member keeps its own agent id and chat. `POST /app/v1/agents/{member.agent_id}/turn` checks paused members and records
accepted messages in the native log for the manager to read.
Native clarifying questions climb from member to manager and through parent
crews. Permission decisions remain yours; the visible question path keeps its
original member id. Ask-answer and Inbox routes belong to their own resource lanes.
The crew page shows linked items and the manager's task snapshot.


## Roles — one live copy per crew, edit every copy

`GET /app/v1/roles`, `POST /app/v1/roles` and `PATCH /app/v1/roles/{rid}`
read, make and edit reusable roles. Names have 1–40 characters; instructions
have 1–16000 bytes. A model is required; effort is low, medium or high,
defaulting to medium. Definitions survive relaunch. Existing inline crew
roles stay readable and editable.

Editing updates each live copy's name, model, effort and instructions.
Instructions arrive through the native log at its next step. Each copy keeps
its own conversation, memory and work. Adding or moving a second copy of the
same role into one crew returns 409 `already in <crew name>`.

## Sub-crews — bring helpers to a member, depth and budget share

`POST /app/v1/crews/{cid}/bring-in-helpers` needs `member_id` for an idle
non-manager member of that crew. It proposes helpers without making a crew.
Accept with `POST /app/v1/crews` using `from:"proposal"` and its `proposal_id`.
The same member becomes the sub-crew's manager, keeping its chat. A changed
parent or source requires a new proposal.

Crews nest at most three deep. At depth three the route returns 409
`Crews nest 3 deep at most.` A sub-crew's budget fits within 50% of its
parent's budget. Edits enforce that limit too. Parent spending includes
sub-crews, counting each conversation once; at a parent cap descendants stop.

## Wrap up, report and keep going — five parts and Markdown

`POST /app/v1/crews/{cid}/wrap-up` asks the manager to finish and bring a
report. It saves the native fifteen-minute wrap-up clock once; asking twice
does not restart it. `GET /app/v1/crews/{cid}/report` gives what shipped,
your decisions, files, left-open work with hand-offs, cost and the same text
as Markdown. Until a report is ready it returns 409
`the manager's report is not ready`.

`POST /app/v1/crews/{cid}/keep-going` keeps it active, clears its pending
report and end date, and tells the manager to continue. An elapsed end date
asks once; it never silently closes the crew.

## Finish crew, archive and reopen — members stop, history stays

`POST /app/v1/crews/{cid}/finish` refuses with 409 and `open_items` while
items aren't done. Open tasks, unresolved report work and unfinished links
all count. Unknown linked state counts as unfinished. Solo factory jobs use
their canonical state; a published PR still waiting on GitHub is unfinished.
Unlinking removes the reference without touching the work.

When no report exists, Finish asks the idle manager to write one through its
normal engine, with a two-minute ceiling. It rechecks state before closing.
Finish keeps that report and stops members; agents still in another open
crew keep working there. Linked jobs keep running.

`POST /app/v1/crews/{cid}/archive` requires a finished crew.
`POST /app/v1/crews/{cid}/reopen` restores its roster and report from Closed
or Archived. A closed parent must be reopened first. History is kept forever.

## Items, log and while you were away — link, unlink, read

`POST /app/v1/crews/{cid}/items` accepts exactly one `ref` or HTTP(S) `url`
without credentials, up to 256 links per crew. A URL gets an opaque ref.
`DELETE /app/v1/crews/{cid}/items/{ref}` unlinks that reference only.
The crew GET includes its linked items.

`GET /app/v1/crews/{cid}/log` shows everyone talking, newest first, up to
100 entries; use its `before` cursor to read older entries. This log is read
only; message a member to speak. `summary-since?at=<RFC3339 time>` gives a
plain log digest after at least an hour away with new entries. With no new
entries or less than an hour away, its text stays empty.
