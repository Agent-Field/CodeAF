# AgentField and the factory

## connect AgentField — the factory as a node on the AgentField control plane

AgentField (the open-source agent control plane) already takes webhooks from
Linear, Sentry, Slack, GitHub, Stripe and any signed caller, runs cron
schedules, stores and de-duplicates every event, and dispatches each one to an
agent. The factory on this machine can register on a control plane as an
AgentField **node**, so a trigger on AgentField becomes factory work.

Connecting takes: the control plane's address (`url`, such as
`http://agentfield.local:8080`), its API key if it has one, and the address
the control plane reaches this factory on (`base_url`, such as
`http://10.0.0.5:8765`; it needs a port). Optional: a node id (default
`codeaf-factory`), the address to listen on (default: base_url's port, on
127.0.0.1 when base_url is loopback, else on every interface), and a trust
ceiling (default `collaborator`).

The app's engine connects with `factory.agentfield_connect`. Before keeping
anything it asks the control plane for its source catalog with the key, starts
listening, and registers; if any of those fails ("AgentField did not accept
that API key", "AgentField could not be reached", "the factory could not
listen on …") nothing is kept. The API key is kept the way the GitHub token
is (by the desktop app, else in the profile) and never sent back. There is no
slash command for this in the terminal chat, and only the factory on this
machine can be connected.

## connect my own tool or script to the factory — no HTTP API, no SDK, only AgentField

A person using the factory never writes code to send it work. Built-in
connectors (GitHub, Jira) are set up in the app with clicks. **Anything
programmatic — your own tool, a script, a form, a helpdesk, an alerting
service, Zapier, another agent — comes through AgentField**: connect the
factory as a node (above), then aim an AgentField trigger at it, or call its
`intake` reasoner directly.

The factory itself has no open door for code: no `POST /api/intake`, no HTTP
route that says more about a job or reads its status, no webhooks, no SDK.
`codeaf factory add` and `codeaf factory webhook` were removed.

- **Send a comment to a job from outside**, or add more details to a job your
  tool filed: AgentField's `message` reasoner (below).
- **Check the status of a factory job from my script**: AgentField's `status`
  reasoner, or the execution the trigger started, which reports the job's
  progress and its pull request.
- **A token that only files its own tickets**: the tool holds AgentField's
  credential (a trigger's signing secret, the control plane's API key), never
  the factory's, and the connection's trust ceiling caps what it files.

## webhooks from the factory — set a webhook, webhook events, signature, an endpoint that is down

The factory sends no webhooks of its own, so there is no webhook to set for a
source, no event list, no signature to verify and no retry schedule. A tool
that wants to hear how its work is going calls through AgentField: the
execution a trigger started is told when the job runs, asks a question, opens
its pull request, finishes or gets stuck (see "the AgentField execution"
below), and AgentField delivers that onward its own way. Jira tickets hear
back as Jira comments.

## the control plane — what the factory registers and how its calls are trusted

The node registers three reasoners, each with a JSON Schema:

- `intake` (takes webhook triggers): a trigger's event, or an entry from any
  caller, becomes a job (see the sections below).
- `message`: `{source, id, text, author?}` or `{job, text}` says more about a
  job filed from outside, read as a comment on a GitHub issue is: the words
  become a comment on the job, a stranger's job (or a connection trusted no
  further) reads them as context only, and it answers `{job, accepted,
  outcome, line}`.
- `status`: `{source, id}` or `{job}` answers `{job, state, steps_done, steps_total,
  route?, pr_url?, question?, updated}`.

The node also answers `/health`, `/status` and `/discover`. Every restart of
the factory registers again as a new instance, and the node renews its lease
with the control plane every minute or so.

AgentField sends no credential when it dispatches a trigger, so the node is
protected by its address instead: it registers `<base_url>/n/<secret>` with a
random 64-character secret and answers nothing outside that path (anything
else is "404 not found"). Only someone who can read the control plane's node
list with its API key can see that address.

## trigger from Linear / Sentry / Slack — what each event becomes

Each AgentField source has one translator; nothing else is decided there.
Routing rules pick the repository and pipeline afterwards.

- **Linear**, an issue created or updated: the title, description, link,
  creator and labels, plus a `linear-team:<KEY>` label. Linear's priority:
  Urgent p0, High p1, Medium p2, Low p3, none none. The id is the issue's
  identifier (`ENG-142`), so an update edits the same job. Comments, projects
  and cycles file nothing.
- **Sentry**, an issue created or come back (unresolved): its title, culprit,
  level, event count and link, with `sentry-project:<slug>` and
  `sentry-level:<level>` labels. fatal is p0, error p1, warning p2. The id is
  the short id (`FACTORY-DEMO-7`). A resolution or an alert files nothing.
- **Slack**, a message or an app mention: the text (mentions taken out) is
  the title and body, the sender is the author, with a `slack-channel:<id>`
  label. A bot's message, an edit or a deletion files nothing.

A person on Linear, Sentry or Slack is trusted as a `collaborator`, never more
than the connection's ceiling.

## the source name of AgentField work — routing rules for agentfield sources

Every job the node files has the source `agentfield:<source>`, an underscore
written as a hyphen: `agentfield:linear`, `agentfield:sentry`,
`agentfield:slack`, `agentfield:generic-bearer`, `agentfield:generic-hmac`,
`agentfield:cron`, and `agentfield:execute` for a direct call. A routing rule
with `sources: [agentfield:linear]` sends Linear work to a repository; a rule
with `sources: [agentfield]` matches all of it. The job's id on the source is
the source's own id, else the event's `external_id`, else AgentField's
delivery key, so a re-delivered event updates the job it made and never makes
a second one.

## a webhook, a script or another agent — generic_bearer, generic_hmac, a direct execute

For `generic_bearer` and `generic_hmac` triggers, and for a direct call
(`POST /api/v1/execute/codeaf-factory.intake`), the event must already be an
entry: an object with a `title`, or `{"entry": {...}}`. Its fields are the
factory's intake fields: `title` (required), `body`, `external_id`, `url`,
`author`, `labels`, `priority` (`p0`..`p3`), `repo` (`owner/name`) and
`trust`. The `source` it names is ignored: the source is always the trigger's
own. Its trust is held to the connection's ceiling, so with the default
ceiling an entry that says `owner` is filed as a collaborator's. An entry the
factory refuses (a bad priority, a repo that is not `owner/name`, a title too
long) answers 422 with the reason, and AgentField records it as the event's
error.

## cron — scheduled factory work from AgentField

A `cron` trigger's tick carries only when it fired, so **a bare tick files no
job** and answers "a cron tick that carries no entry". A tick files a job
only when its event carries an entry (for example a replayed event that was
given one). For a job on a schedule, send the entry from a scheduler you own
to a `generic_bearer` trigger instead.

## GitHub through AgentField — a webhook makes the GitHub read instant, never a job

An AgentField `github` event never files a job, because GitHub is the
factory's own source. If the event's repository is one the factory watches,
it asks the factory's GitHub read to read now, the same thing "Check GitHub
now" does, and the issue arrives the ordinary way: same job, same labels,
Pick up issues, comments and close-on-merge exactly as before. A repository
the factory does not watch is ignored.

## add a trigger on AgentField — the source catalog and the triggers aimed at the factory

`factory.agentfield_sources` lists the control plane's sources, each with its
kind (`http` posts to AgentField, `loop` is run by it, as cron), whether it
needs a signing secret, and its config schema. `factory.agentfield_trigger_add`
creates a trigger for one of them aimed at `<node id>.intake`, with its config,
the name of the control plane's environment variable that holds the signing
secret (`secret_env_var`, required by a source that needs one) and an
optional list of event types. `factory.agentfield_triggers` lists the
triggers aimed at the factory, each with its `ingest_url` (the control
plane's `/sources/<id>`, what the outside system posts to), how many events it
had in a day and when the last one came.

## the AgentField execution — progress, the pull request link and stuck jobs

A dispatched trigger is one AgentField execution. When the intake files a job
it answers 202 and reports on that execution until the job settles:

- while the job runs: running, with progress as the share of steps done, and
  the job's question while it waits on an answer;
- when it opens its pull request: succeeded, with the result `{job, state,
  pr_url, steps_done, steps_total, …}`;
- when it ends with no pull request: succeeded, with no `pr_url`;
- when it stops and needs a person (a step failed, someone stopped it, its
  publish failed): failed, with the job's own words.

Each change is reported as it happens, from the same moments Jira hears. An execution not told anything for three minutes is told again,
so a long job is never taken for stale, and a job that ended with nothing to
announce it (a person dropped it) completes its execution within those three
minutes. An event that files nothing completes its execution at once with
the reason as the result.

## why didn't my AgentField trigger make a job

Look at `factory.agentfield_status`: `last_event_what` says what became of the
last call ("filed job 12 (ENG-142)", "filed nothing: …", "refused: …").

- **Filed nothing**: a Linear comment, a Sentry resolution, a Slack bot
  message, a cron tick or a generic event with no `title` all file nothing on
  purpose; a `github` event never files a job (see above).
- **Refused**: the entry was not one the factory takes; the reason is on the
  execution in AgentField too.
- **No call at all**: the trigger is disabled, its event type is filtered
  out, AgentField could not reach `base_url` (look at the trigger's failed
  events in AgentField), or the factory is not connected.
- **A job, but it did not start**: that is the routing and start rules, and on
  this machine **Pick up issues** must be on for anything to start by itself.

## AgentField status, lease and disconnect

`factory.agentfield_status` answers whether the factory is connected, the
control plane, node id, base_url, listen address and ceiling, whether a key is
kept (never the key), the address the node is listening on, when it last
registered, its lease, the last call it had and what became of it, the last
error, and how many executions are waiting on jobs. An engine that serves
these methods says `factory.agentfield` in its hello.

`factory.agentfield_disconnect` tells the control plane the node is offline,
stops listening, and forgets the settings, the key and the secret. Jobs
already filed stay as they are.

## what the AgentField node cannot do

- Only the factory on this machine connects; a team factory (`codeaf factory
  serve`) does not run the node.
- No DID or verifiable-credential signing: the node does not sign or check
  AgentField DIDs; its address secret is its protection.
- A pipeline step cannot call an AgentField reasoner yet, and a job's steps
  are not child executions in AgentField's workflow view.
- Jira is not an AgentField source; it has its own connector (see "connect
  Jira").
