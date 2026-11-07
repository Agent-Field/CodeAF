---
kind: added
pr: 0
title: The factory registers as an AgentField node, and AgentField's triggers are its intake
surface: [engine, app, factory, docs]
invalidates:
  - "Outside work reached the factory only through GitHub, the Jira connector, factory.intake_add, codeaf factory add and POST /api/intake. factory.agentfield_connect {factory, url, api_key?, base_url, node_id?, listen?, trust?} now registers this machine's factory on an AgentField control plane as node codeaf-factory with reasoners intake (accepts webhooks), message and status; every AgentField trigger aimed at codeaf-factory.intake files a job through the inbox under source agentfield:<source> (linear, sentry, slack, generic-bearer, generic-hmac, cron, execute), its trust held to the connection's ceiling (collaborator by default). factory.agentfield_status, _disconnect, _triggers, _sources and _trigger_add read, forget and add to it, and hello announces factory.agentfield."
  - "A dispatched trigger used to be something the factory could not answer. The intake now answers 202, keeps the execution id in agentfield-executions.json beside the floor (not on the job), and reports POST /api/v1/executions/:id/status: running with progress, then succeeded with {job, state, pr_url, steps_done, steps_total} when the pull request opens or the job ends, or failed with the job's words when it stops for a person."
  - "An AgentField github event never files a job: it wakes the factory's own GitHub read for a watched repository (the same as Check GitHub now), so GitHub identity, labels and side effects are unchanged. A cron tick files nothing unless its event carries an entry."
  - "AgentField's trigger dispatcher sends no credential, so the node is protected by a 64-hex secret in its registered base_url (<base_url>/n/<secret>) and answers 404 outside it; the API key and the secret are kept like the GitHub token and never answered. The node registers as a new instance_id on every start and renews its lease; DID signing is not done."
---
