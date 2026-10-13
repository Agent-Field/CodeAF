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

## Message any member and questions climb — app resource availability

Each member keeps its own agent id and chat. `POST /app/v1/agents/{member.agent_id}/turn` checks paused members and records
accepted messages in the native log for the manager to read.
Native clarifying questions climb from member to manager and through parent
crews. Permission decisions remain yours; the visible question path keeps its
original member id. The ask-answer, Inbox and sub-crew HTTP routes are separate
resource lanes and remain reserved until those lanes land.
The crew page's plan and items remain empty until their producers are connected.
