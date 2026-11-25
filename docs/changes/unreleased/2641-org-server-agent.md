---
kind: added
title: an organization's server runs its workstreams' agents with the organization's own model key
pr: 2641
surface: [engine, factory]
invalidates:
  - "Was true: `agent_runner: server` was always refused (422) and `agent_runners` was always `[driver]`. Is true now: `codeaf factory serve` runs workstream agents itself, and offers `server` once the operator has given it a model key; choosing it lets go of every driver's hand."
  - "Was true: an organization server had no model key of its own; it used whatever key its machine's profile held. Is true now: the operator keeps the organization's key on the server (GET/PUT/DELETE /v1/org/keys, operator-only; engine factory.org_keys, org_key_set, org_key_delete), it is answered back only as its last four characters, and the server's agents use it and nothing else."
  - "Was true: in an organization, a workstream's agent existed only on a driver's laptop. Is true now: in `server` mode each workstream has one agent on the server, one conversation everyone who can see the workstream reads (GET /v1/workstreams/{id}/agent, events agent.entry/agent.state, engine factory.org_workstream_agent + event factory.org_workstream_agent); those who can edit it talk to it and stop it (…/agent/messages, …/agent/stop), one message at a time (conflict, detail.reason `busy`). Hello word `org_agent_server`."
  - "Was true: nothing the server's agent could do was defined. Is true now: its belt is `workstream_pages` alone — read the workstream, read/write/make a page, propose a decision — with no shell, files, repository, jobs or web; it writes as the person who asked, keeps its text as a page version (reason `agent`) when the page is being co-edited, and its spend is posted under the asker with chat `agent`."
---
