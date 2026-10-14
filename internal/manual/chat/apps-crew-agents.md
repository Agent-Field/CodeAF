# An app's agent in a crew

## Add an app agent, its badge, and one live copy per crew

`POST /app/v1/crews/{cid}/members` accepts
`{"kind":"app_agent","app":"crm"}`. The member's `badge` names the app,
its agent, the adder's app role, and what that role allows: for example
`can read deals, draft emails`. `crewagent.badge` describes one app's agent;
`crewagent.candidates` lists agents from kept apps the caller can open.

A Viewer adds an agent that only reads. The copy's role remains a ceiling
when another person asks it for work. Adding a second live copy of the same
app's agent to one crew returns `the app agent is already in this crew`.
The member tree returned by the crew includes the badge.

## Ask the CRM agent, linked deals, and the crew log

A manager uses `team_send` and a crew member uses `team_post` to address
an app-agent member by its crew handle, normally `crm-agent` for CRM.
The app agent answers in that crew's log as itself. Its conversation belongs
to that app and crew; it has no separate ordinary agent chat.

Linked records are read live as the person who asked for the work. Masked
and blind fields are excluded from the linked context. The agent can use
only that app's records and allowed connectors. It cannot manage the crew
or use another app's records or connectors.

## Changed in CRM through the CRM agent, record history and app versions

An app agent's answer lists the records it changed during the turn under
`Changed in CRM through the CRM agent`. Each line names the record, the
change, and the app version it ran on, such as `CRM v12`. Changes stay in
the app's records; the crew holds links rather than copies.

`GET /app/v1/crews/{cid}/log` includes the app badge, changed records,
approval cards, an optional `shape_this` request and the app-agent cost.
The cards keep their approval ids, so the crew and app refer to the same
approval. Reading the log refreshes their current approval state.

## Approval needed, who approves, and the app's budget

Approvals raised by the app agent belong to the person who asked the crew
for the work, never the manager. Each approval names the crew and the app.
The agent cannot send an outbound action without approval.

The app agent spends from its app's budget, rather than the crew's budget.
The app's Budget sheet includes those costs. Once that monthly budget is
used up, another crew request returns `the app's monthly budget is used up`.

## That changes the app, Shape this, and Finish crew

An app's agent cannot change fields, screens, runs or its own instructions.
When it emits `That changes the app`, the crew answer offers `Shape this`
with the person's words. Linked record content is never included in that
Shape request; the Builder changes the app.

Finish crew cancels its app-agent copies and removes them from the live
roster. Their answers and approvals stay in the log. The app's own agent
remains available in the app. Reopening keeps the same record links;
add the app's agent again if the reopened crew needs it.

The ordinary chat-member pause, resume, move and remove routes do not
currently operate on app-agent copies: they return `no such member in this
crew`. Finish crew is the door that removes the copy.
