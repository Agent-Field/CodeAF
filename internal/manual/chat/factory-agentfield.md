# AgentField and the factory

## connect AgentField — the factory as a node on the AgentField control plane, even behind NAT

AgentField (the open-source agent control plane) already takes webhooks from
Linear, Sentry, Slack, GitHub, Stripe and any signed caller, runs cron
schedules, stores and de-duplicates every event, and dispatches each one to an
agent. The factory on this machine can register on a control plane as an
AgentField **node**, so a trigger on AgentField becomes factory work.

Connecting takes the control plane's address (`url`, such as
`http://agentfield.local:8080`) and its API key if it has one. That is
enough: the node dials the control plane's **link** and needs no address of
its own, so a laptop behind NAT works. Optional: `base_url` (an address the
control plane can reach this factory on, such as `http://10.0.0.5:8765`) and
the control plane's `internal_token`, for a control plane that offers no
link; a node id (default `codeaf-factory`); the address to listen on
(default: base_url's port); a trust ceiling (default `collaborator`).
With no base_url the factory binds no port at all for AgentField.

The app's engine connects with `factory.agentfield_connect`. Before keeping
anything it asks the control plane for its source catalog with the key, then
registers; if that fails ("AgentField did not accept that API key",
"AgentField could not be reached", "the factory could not listen on …")
nothing is kept. The API key is kept the way the GitHub token
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

Every restart of the factory registers again as a new instance.

**The link.** The node opens one WebSocket to the control plane
(`/api/v1/nodes/<id>/link`, the API key as its bearer, never in the URL) and
keeps it, reconnecting with backoff (half a second up to thirty) when it
drops. Dispatches and cancels come down it, each acked once and deduped by
delivery, execution and idempotency key; the job's progress and completion
and the lease's heartbeat go up it. A control plane without the link is
reached over HTTP instead, and the node says the link is "not offered".

**Over HTTP**, with a base_url, the node answers `/health`, `/status` and
`/discover` openly, and a reasoner or cancel call only when it carries the
control plane's internal token as `Authorization: Bearer`; anything else is
refused 401. The old `<base_url>/n/<secret>` path is still answered for one
release when an old secret is kept; a fresh connect registers a plain
base_url.

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

Each change is reported as it happens, from the same moments Jira hears, up the link when it is up and over HTTP otherwise. A long job is
never taken for stale: every lease lists the open executions (`inflight`)
and the control plane keeps them alive. A report that could not land (the
control plane was down), or a job that ended with nothing to announce it (a
person dropped it), is caught up at the next lease or reconnect. Cancelling
the execution in AgentField stops the job, as a person's Stop does. An event
that files nothing completes its execution at once with the reason as the
result.

## why didn't my AgentField trigger make a job

Look at `factory.agentfield_status`: `last_event_what` says what became of the
last call ("filed job 12 (ENG-142)", "filed nothing: …", "refused: …").

- **Filed nothing**: a Linear comment, a Sentry resolution, a Slack bot
  message, a cron tick or a generic event with no `title` all file nothing on
  purpose; a `github` event never files a job (see above).
- **Refused**: the entry was not one the factory takes; the reason is on the
  execution in AgentField too.
- **No call at all**: the trigger is disabled, its event type is filtered
  out, the link is down and AgentField could not reach `base_url` (look at
  the trigger's failed events in AgentField, and `link` in the status), or
  the factory is not connected.
- **A job, but it did not start**: that is the routing and start rules, and on
  this machine **Pick up issues** must be on for anything to start by itself.

## AgentField status, lease and disconnect

`factory.agentfield_status` answers whether the factory is connected, the
control plane, node id, base_url, listen address and ceiling, whether a key
and an internal token are kept (never either), the address the node is
listening on (none for a link-only node), the link (`up`, `connecting`, `not
offered`) and how many times it came up, when it last registered, its lease,
the last call, which way it came (`link` or `http`) and what became of it,
the last error, and how many executions are waiting on jobs. An engine that serves
these methods says `factory.agentfield` in its hello.

`factory.agentfield_disconnect` tells the control plane the node is offline,
closes the link, stops listening, and forgets the settings, the key and the
token. Jobs
already filed stay as they are.

## a job run by an AgentField reasoner — run_on, the job's steps are the reasoner's calls

A job filed with `"run_on": "<node>.<reasoner>"` in its intake payload (for
example a `generic_bearer` trigger's body) is run by that reasoner on its own
node instead of by this factory's pipeline. It is an ordinary job everywhere:
the same inbox, queue, Working, Needs you and All jobs lists, the same job
page and step rail, Start and Stop.

- **Before Start** it waits as plain Backlog with no steps: its steps are
  the run's, and the run has not said what they are yet.
- **Start** dispatches the reasoner with `{"job", "title", "body", "event"}`.
  The node needs no inbound port; the factory only calls the control plane.
- **Its steps are the run's executions**, in the order they began. A
  step's name is one word from its reasoner (`draft_reply` is `draftreply`;
  a second call is `investigate2`) and its label is the reasoner in words
  (`Draft reply`; see "the names of an AgentField run's steps"). A nested call carries its parent
  and is not counted as a step of its own (`3 / 3`, not `4 / 4`). Calls
  that ran at once share a group, drawn as one row, whose `group_label` is
  the words they share (`Investigate`). A harness call is a conversation
  step with its own chat; any other call reads as a command step, with no
  chat. No step names a model: the node chooses its own.
- **It ends with the run**: done when the reasoner succeeded, Needs you with
  the run's own words ("the AgentField run failed: …") when it failed.
  **Stop** cancels the whole run tree on AgentField; the stopped step reads
  skipped, the run reads cancelled, and the step's last lines are still kept.
- It needs no repository and no model key on this machine — the work and
  its model calls happen on the node — and it has no proof to approve: a run
  that succeeded is done.
- A factory that is not connected refuses the job: "this job runs on
  AgentField and this factory is not connected to AgentField".

The node's developer writes nothing for any of this: a reasoner that calls
`a.Harness(...)` on the codeaf provider is watched and steerable by itself.

## the names of an AgentField run's steps — why parallel steps read the same, a call's subject, a label tag

A run's step takes the label its reasoner declares on the control plane, in
the reasoner's tags: `label:<words>` is the step's label (`label:Code` reads
`Code`). Without one, the step is the reasoner's name in words, every word
kept: `draft_reply` is `Draft reply`, `run_coder` is `Run coder`. codeaf
guesses at no pipeline's own naming habits, so a reasoner that wants another
label declares it.

A call is told apart by its subject, read once from that call's input. A
reasoner that declares `label_fanout:<field.path>` names the field
(`label_fanout:issue.title`), and that value follows every call of it, even
one called once (`Code · add Len() to Ledger`). Otherwise only a reasoner
called several times gets a subject: a `subject`, `topic`, `title` or `name`
field, or one inside an object of it, else the first line of a `goal`, `task`
or `description`, cut short. One word follows the label (`Investigate
goccy/go-json`), a phrase follows a dot. Calls that ran at once are one group,
named by the words their labels share (`Investigate`). A call whose input names
nothing short keeps its label alone.

## an AgentField job's budget, status and finish time — why Start is not greyed out

A `run_on` job is filed with the factory's default spending cap, the one a
native job gets: the cap set in the factory's spending settings, else $5.00.
So Start is open the moment it arrives. Its model calls are paid on the
node, so the cap is only what the job page shows beside it.

- **Arriving**, its activity reads "arrived from AgentField · <node>", and
  it never says it is waiting for a repository: it needs none.
- **Started but not yet running** (dispatched, the run has not reported),
  it reads "Starting" ("Starting on AgentField · <node>.") and shows no
  step, never the factory's pipeline (`plan`) or a wait for a runner.
- **Running**, its step is the run's own current step.
- **Ended**, `done_at` is when the run ended on AgentField (else when this
  factory saw it end), so it sorts and reads like any finished job.

A job a trigger files is announced on the app's wire as
`factory.jobs_changed {factory, job}`, once when it is filed and once after
the intake pass, so a Backlog on screen can read it again at once instead of
on its next refresh.

## where did this job come from, and where does it run — the AgentField node, its machine, reasoner, trigger and labels

A job an AgentField reasoner runs (`run_on`) says where it came from on its
ordinary read, with nothing for the node's developer to write:

- **source** is `agentfield`, its reference the reasoner that runs it
  (`support.triage`), and its link the run's page on the control plane's UI
  (`/ui/runs/<run>`) — only when the control plane is on `https`. A plain
  `http` control plane gives no link, because the app opens only `https`.
- **origin** names the node, the reasoner and the trigger that filed it (the
  trigger's source, such as `generic-bearer`; a direct execute names none). A
  job this factory runs itself has no origin, even when a trigger filed it.
- **runs on** (`runner`) is the node and the machine it named to the
  control plane — `support · dumb`, or just `support` when the node named
  none. It is never this machine: the factory here only watches the run.
- **labels** are the event's `labels` and `tags` and `node:<node>` —
  lowercased, each once, at most 20. A trigger on the control plane has no
  tags of its own to add.

Every other job's labels are read the same way: a GitHub issue's labels, and
a Jira ticket's labels, `jira-component:<name>` and `jira-project:<key>`.

Filtering jobs by `source:`, `node:` or `label:` happens in the app's job
list, over these fields.

## watch and steer an AgentField step — open its chat, send it a note mid-run

A step that ran the codeaf harness has a chat like any step this factory runs:
open it from the job's rail and it shows the same entries — words, thinking,
tool cards, diffs — streamed as the node works, because the node relays the
run's own conversation record line by line and the factory keeps it as an
ordinary conversation on this machine. The factory is the record: the control
plane holds a line only until the factory has kept it, and a factory that was
offline gets every line once when it is back.

**Steering is the step chat's ordinary send.** While the step works, what you
type goes to that run on its node, which reads it at its next turn boundary;
the record then shows it as your steer. Refusals, word for word:

- "this step has ended" — the run finished; nothing is listening.
- "this step is not running the codeaf harness, so it cannot be steered".
- "the run has notes it has not read yet; send this again in a moment".
- "this factory is not connected to AgentField, so the step cannot be steered".

Only codeaf harness steps can be steered. A plain reasoner's step has no chat
at all, so there is no box to type in.

## run a step on AgentField — an agentfield step in a pipeline, a reasoner that builds an issue, node.reasoner and its input

A pipeline step can be a run on AgentField: `kind: agentfield` in
`.codeaf/factory.yaml`, naming the reasoner as `node.reasoner`:

```yaml
pipeline:
  build-issue: [build]
steps:
  build:
    label: Build on AgentField
    kind: agentfield
    target: planner.build_issue
    input:
      title: "{title}"
      body: "{body}"
      repo_url: "{repo_url}"
      issue_number: "{issue_number}"
```

`input` maps the reasoner's own field names to the job's facts: `{title}`,
`{body}`, `{repo}`, `{repo_url}`, `{base}`, `{issue_url}`, `{issue_number}`,
`{job}`. Each value is sent in the type the reasoner's input says for its
field (`{issue_number}` is a number for a number field), and one that is a
fact alone is left out when the job has none. An object or a yes/no field is written
as its JSON text (`config: '{"check_ci": false}'`, `plan_review: "true"`). With no `input` the
step sends `job, title, body, repo, repo_url, base_branch, issue_url,
issue_number`. A fact not on that list is a problem in the file, and a step
whose target is not `node.reasoner` holds for a person.

The app's pipeline editor and Change with AI add the same step. A routing rule
that sends GitHub issues to that pipeline makes each one an ordinary job: on
Start the factory starts the reasoner, the run's executions appear under the
step on the job's rail (harness steps with their chats, steerable), Stop
cancels the whole run, and the step ends when the run does. A pull request the
run opened becomes the job's the moment the call that opened it ends, while the
rest of the run is still going, and stays the job's whatever the run says after.
A run that ends saying it did not succeed (a result of `{"success": false,
"summary": "Plan rejected: …"}`) ends the job with those words, beside any pull
request it opened. A pipeline made only of agentfield steps and approvals ends
when its runs do, with no proof to approve, and needs no checkout on this
machine: its job never says it is cloning the repository here, and Start is not
refused because this machine cannot clone it. The same is true of a `run_on` job.

## approve the plan an AgentField run asks about — Needs you, ask for changes, reject

When a reasoner pauses for a person (a plan review, or any `Pause`), its
job goes to Needs you with the reasoner's question and the WHOLE plan or body it
asks about, readable in full. Three answers:

- **Approve** — the run goes on.
- **Ask for changes** — your words go to the run, which may make a new plan and
  ask again. The new round shows the new plan, the plan before it, and the
  words you sent. It needs words: "say what to change: Ask for changes sends
  your words to the run".
- **Reject** — the run stops and so does the job.

Only the job's person answers; the manager never does. Anyone else is refused
with "only @<person> can answer this". The answer goes to the control plane,
which keeps the one approval: if it was answered first somewhere else (hax, the
control plane's page) the job's question comes down by itself, and a late
answer says "this was already answered somewhere else, and that answer stands".

## answer an AgentField approval from another computer — a team factory, a server, a desk attached to a remote factory

The plan is answered the same way whichever factory holds the job: the one on
this machine, a team factory on a server (`codeaf factory serve`), or a factory
on another computer that this desk is attached to. Approve, Ask for changes and
Reject are that factory's own job answers.

- The factory checks who may answer (the job's person; anyone on a job with no
  person yet), keeps the answer on the job with who gave it, and takes the
  question down at once.
- The runner that started the run carries the answer to the control plane. It
  holds the AgentField key and is the run's owner there, so the control plane
  still lets only that node answer the run. Your handle goes with it as
  `answered_by`; your words go to the reasoner as they are.
- A Reject stops the job once the runner has passed it on.
- An answer survives a restart of the factory, the runner or the desk: the
  runner that takes the job again sends any answer the control plane has not
  had yet.
- If the control plane refuses the answer, the question comes back and the job
  says "the AgentField control plane did not take the answer: …".
- A Reject given while no runner holds the job stops the job at once, and the
  control plane's pause then runs out on its own.

## a question from an AgentField step — the harness step asks, the manager or you answer

A codeaf harness step on a node may ask a question while it works. It no longer
takes its own default with nobody to ask: the step waits, and the question
comes to the job like a question from one of this factory's own steps, first to
the job's manager and then, if the manager sends it on, to you in Needs you with
its options and the step's own pick. Your answer goes back to the step, which
reads "the person answered: …" and carries on. When the run ends first, the
question comes down.

## what the AgentField node cannot do

- Only the factory on this machine connects; a team factory (`codeaf factory
  serve`) does not run the node.
- No DID or verifiable-credential signing: the node does not sign or check
  AgentField DIDs; the link's key and the HTTP bearer are its protection, and
  both are the control plane's global values, not one per node.
- An agentfield step runs a reasoner on its node, never on this machine,
  and the control plane reports no money for it: the job's spend and budget
  do not count what the run spent.
- Only a step that ran the codeaf harness has a chat and takes a note. A
  plain reasoner's step, or a step on another harness, shows its state and
  timing only: it has no chat and no message box.
- A note to a step reaches it at its next turn boundary, not mid-sentence.
- A harness step on a node is not sandboxed. It runs codeaf with every tool
  allowed, the same gate a factory step has by default, so its shell can
  install software or pull images on the node's machine; only codeaf's
  built-in floor of critical commands is refused, with nobody there to ask.
  Confine the node's machine itself if that matters.
- Jira is not an AgentField source; it has its own connector (see "connect
  Jira").
- A node's runs do not have your full permissions. They go through
  `codeaf chat --once` and `codeaf exec`, whose commands may write only their
  own checkout and their own temp and cache folders. They cannot read keys and
  sign-ins or use docker unless `"jail.allow"` says so ("What an unattended run
  may touch").
