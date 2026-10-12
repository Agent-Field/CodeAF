# Factory runners, people and access

The source is codeaf-desk canvas v2, commit `d39636d`, boards S0, S1, S2 and
X3. The binary owns factory state, authentication, execution rules and Activity.
The app owns presentation and sends macOS sleep/wake through the presence door.

## Solo and team

`serve.Config.Mode` is `solo` or `team`; host YAML accepts `mode` and
`operator_login`. The window's local factory selects solo;
`codeaf factory serve` selects team. A solo directory has no People entries or
job assignee control. Every solo job is yours. In a team, a runner runs only
its owner's assigned jobs; `takes_unassigned` never permits team execution.

## Runner contract

Contract 2 names the changed ownership and expiry rules.

`GET /v1/runners` returns `{factory,runners}`. A runner has `id`, `name`, `owner`,
`kind` (`this_mac`, `mac`, `ci`), `slots` (default 2), `state`, `checkouts`,
`jobs` and `last_seen`. Only visible repository checkout and job facts cross.

`POST /v1/runners/attach` accepts `runner`, `owner`, `name`, `kind`, `slots`,
`checkouts` and returns a private credential with `runner_id` and `worker_id`.
Each execution slot has one lease. Slots on one computer share the runner's
capacity; attaching another slot does not bypass its limit. The binary loop
starts two slots by default; `CODEAF_FACTORY_BENCHES` sets its execution count.
`PUT /v1/runners/{id}` sets the owner's positive slot limit and checkout list.
`codeaf runner join <url> [--name name] [--token session]` saves the connection
for new codeaf windows through the existing attach door; it does not run a daemon.
The earlier `/v1/workers` execution endpoints remain readable by older callers.
The new execution endpoints are `/v1/runners/pull` and
`/v1/runners/leases/{id}/{heartbeat,report,done}`.

A runner becomes offline after 60 seconds without a heartbeat. A running job
becomes Needs you with `runner_offline` and exactly
`Runner went offline mid-step`. A stale report cannot reclaim the expired lease.
`POST /v1/runners/presence` with the execution credential and `{asleep:true}`
retains leases during macOS sleep and refuses new work. `{asleep:false}` renews
the deadline on wake. Sleep is explicit; silence is never guessed to be sleep.
Runner facts and credential hashes survive a server restart in `runners.json`.

## People, sessions and access links

`GET /v1/people` returns `{factory,mode,people}`. Each team person has `login`,
`name`, `avatar_url`, `you`, `operator`, `status`, `runners` and their visible
job counts. Status is invited before first sign-in, active after sign-in, and
away at 15 minutes idle. Sessions are shown only to that person or the operator.
The older person card and member fields remain for existing callers.

The operator identity comes from the host's GitHub account or
`serve.Config.OperatorLogin`; access links never supply it. The host bearer
and a credential-free loopback request are also operator doors. An ordinary
session cannot change connected repositories or manage links.

`POST /v1/access/links` accepts optional `login`, `name`, `expires_at` and returns
`{factory,link}`. Its `codeaf://factory/join` URL carries the access secret in a
fragment, returned only at creation. `GET /v1/access/links` omits secrets.
Sign in with GitHub through an access link: use the link token for device
sign-in, then use the returned factory session. A named invitation binds only
that GitHub login. The link alone cannot read jobs or act as the operator.
`DELETE /v1/access/links/{id}` revokes it and its person's live execution.

`DELETE /v1/people/{login}/sessions` is an operator action. It ends that person's
sessions and runner credentials, withdraws leases and approval, and moves their
unfinished jobs to Unassigned in Backlog. `POST /v1/access/revoke-sessions`
ends all sign-ins and applies the same ownership rule. These operations retain
history and branches; user-facing confirmation belongs to the app.

## Append-only Activity

`GET /v1/activity` returns the durable record. Every event has an actor (person,
agent or factory), verb, object, detail, optional rule and cost, plus stable
receipt fields. Operator events carry `actor.operator:true` and `as operator`
in the detail. The record remains append-only with no edit/delete endpoint.

`kind`, `person`, `on`, `when` (UTC date) and `since` (RFC3339) select events.
`format=csv` exports exactly those selected, visible events. The CSV retains the
original receipt columns and adds actor, verb, object, rule and as_operator.
Neither links nor Activity list reusable credentials. Typed SSE `runner` and
`activity` events share the durable replay stream; `machine` remains for earlier clients.
`PUT /v1/repos/{owner}/{repo}` with `{on:true}` or `{on:false}` changes one repo.
