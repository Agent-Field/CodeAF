---
kind: added
title: AgentField triggers are added from the app, and a routing rule can run a job on a reasoner
pr: 2525
surface: [engine, app, factory, docs]
invalidates:
  - "factory.agentfield_triggers answered {factory, node_id, triggers} only. It now also carries sources (the catalog, each with label, config_fields [{name, label, secret?}] and, when the control plane lists them, event_types), and each trigger adds entry_source (agentfield:<source>, _ written -), label, webhook_url (absent for a loop source such as cron) and created_at. factory.agentfield_sources sources carry label and config_fields too."
  - "A trigger could be added only with factory.agentfield_trigger_add, answering the bare trigger. factory.agentfield_add_trigger {factory, source, event_types?, config?, secret_env_var?} now answers {factory, trigger, setup?: {webhook_url?, secret_env_var?}}, refuses a source the catalog lacks or a missing secret variable in words, and hello announces factory.agentfield_triggers. setup.secret is never filled: AgentField reads its secrets from its own environment. Neither add quotes its config in a refusal any more (trigger_add's decode error used to echo the input)."
  - "A routing rule's then was {repo, pipeline, priority, start}, so only a job filed with run_on could run on an AgentField reasoner and a job with no repository waited in Backlog. then.run_on \"<node>.<reasoner>\" now sets the job's run_on: it needs no repository, gets no pipeline steps, and runs as a run_on job does. A rule with run_on names no pipeline; on this machine's factory, factory.routes_set refuses a reasoner the control plane does not list and refuses run_on when not connected. hello announces factory.route_run_on."
  - "when.sources matched agentfield, agentfield:linear and so on, but agentfield:* matched nothing. A trailing * after : or / now means every source under it."
  - "The not-connected refusal of the AgentField trigger calls said \"this factory is not connected to AgentField\"; it now says \"this organization is not connected to AgentField; connect it first\"."
---
