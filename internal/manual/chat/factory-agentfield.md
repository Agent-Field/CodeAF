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
nothing is kept. The API key and internal token are kept the way a pasted
GitHub token is. In the desktop app they are held in memory only and never
written to disk: the engine hands them to the app, and the app hands them over
again each time it starts the engine. A key or token the app hands over after
the factory has started (its keychain answered late) restarts the node with it
at once, under the same instance; the same one handed over again changes
nothing. Outside the app they are kept in the
profile, owner-readable only. They never reach a log, Disconnect forgets them,
and they are never sent back. The desk has no settings screen to connect AgentField yet:
`factory.agentfield_connect` is the one way in. There is no slash command for
this in the terminal chat, and only the factory on this machine can be
connected.

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

Every restart of the factory registers again under the SAME instance: the
instance belongs to the factory's home (kept in `agentfield-instance.json`
beside its jobs), not to the engine process. See "the engine restarted" below.

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

## add a trigger on AgentField — the source catalog, the triggers aimed at the factory, the webhook address and its secret

A trigger can be added from the app; the control plane's own page is not
needed. `factory.agentfield_triggers` lists the triggers aimed at this
factory's node only, beside the control plane's catalog of sources. Each
source has its name in words (`Linear`, `Signed webhook`, `Schedule` for
cron), its kind (`http` posts to AgentField, `loop` is run by it), whether it
needs a signing secret, and its config fields, a secret one marked to be drawn
as a password. Each trigger says the source its jobs carry
(`agentfield:linear`, what a routing rule names), its event types, whether it
is on, its `webhook_url` (the control plane's `/sources/<id>`, what the
outside system posts to; a cron trigger has none) and how many events it had
in a day.

`factory.agentfield_add_trigger` makes one, aimed at `<node id>.intake`, with
the source, optional event types and config, and answers the trigger and what
to set up: the webhook address and the name of the secret's variable. **The
signing secret is never typed into codeaf**: AgentField reads it from its own
environment variable, named by `secret_env_var`, and never hands it back. A
source the catalog lacks answers "AgentField has no source named …"; one that
needs a secret, given no variable, answers "… needs a signing secret: name the
control plane's environment variable that holds it (secret_env_var)". Not
connected answers "this organization is not connected to AgentField; connect
it first". A config is sent to the control plane and nowhere else, and no
refusal quotes it. (`factory.agentfield_sources` and
`factory.agentfield_trigger_add` are the older pair of the same calls.)

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

## the engine restarted, crashed or the app quit — what happens to my AgentField execution

Nothing AgentField can see. The app's engine may stop and start again (a
crash, the Mac sleeping, the app quit and opened again, a late key restarting
the node): the job keeps its place on disk, and so does the list of
AgentField executions waiting on it. The engine registers again under the
instance it always uses, so AgentField does not take the restart for a new
process and does not fail the executions with "agent_restart_orphaned". Its
first lease lists them as still in flight, it reports where each job stands
now, and each execution later gets its job's real result.

A factory that last ran on a build that registered a new instance per start
adopts the instance AgentField holds when it first starts with executions
open, so the upgrade itself loses none.

The limit is AgentField's own stale timeout (30 minutes by default): an
execution nothing has touched for that long is failed by the control plane,
and an engine that stays down that long cannot touch it. Once failed, it stays
failed; the job itself goes on in the factory.

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

## send jobs to an AgentField agent with a routing rule — run_on in a rule, a reasoner, work with no repository

A routing rule's `then` can name `run_on`, `"<node>.<reasoner>"`: every job
the rule matches is run by that reasoner instead of by a pipeline, exactly as
a job filed with `run_on` is (below). The job needs no repository, so a Slack
message, a Sentry issue or a cron tick with no code to change still runs. With
`start: auto` it starts at once, `ask` holds it for a person's yes, `backlog`
files it. Such a rule names no `pipeline`; a `repo` beside it is kept as the
job's repository.

The reasoner is checked when the rules are saved, against the reasoners the
control plane lists (the same list an agentfield step is picked from): one it
does not have refuses the table with "routing rule <name>: AgentField has no
agent <node.reasoner>; pick one it lists", and a factory that is not
connected refuses a rule with `run_on`. A team server's rules are checked for
spelling only. `when.sources: [agentfield:*]` matches every AgentField source,
as `agentfield` does.

A rule may not run jobs on this organization's own node (its `intake`,
`message` or `status`): that would file every run here as a new job the same
rule runs again. Saving one is refused with "routing rule <name>: that agent
is this organization itself; pick another". An agentfield step on it is
refused the same way when its pipeline is saved ("step <name>: …"), and the
list of agents a rule or a step is picked from leaves it out.

Saving the rules routes the jobs already waiting at once, and so does turning
the runner on: a job filed while the runner was off, or before its rule
existed, does not wait for the next entry to arrive. A job the rule runs on a
reasoner drops its "waiting for a repository" line when it is routed.

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
  **Only Stop cancels it.** Quitting the app, closing the engine or this
  machine's runner letting the job go leaves the run going on AgentField; the
  next start follows it again and catches up on what it did meanwhile.
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
"summary": "Plan rejected: …"}`) fails the step, beside any pull request it
opened; how that reads is under "an AgentField step that did not succeed". A pipeline made only of agentfield steps and approvals ends
when its runs do, with no proof to approve, and needs no checkout on this
machine: its job never says it is cloning the repository here, and Start is not
refused because this machine cannot clone it. The same is true of a `run_on` job.

## give an AgentField step its input in the app — the pipeline editor's input fields, plan_review, config, a field the reasoner does not list

In the app's Pipelines editor, **Add a step › Run on AgentField** asks for a
name and the reasoner (`node.reasoner`, from the reasoners the control plane
lists). Picking one shows **Input**: one line per field the reasoner lists,
in its order, marked "needed" where the reasoner requires it. A field whose
name matches a job fact starts filled with it (`title` with `{title}`,
`base_branch` with `{base}`, `issue_number` with `{issue_number}`); the rest
start empty.

Each line takes a job fact in braces, several facts with words around them
(`{title}: {body}`), or words of your own. A yes/no or an object is written
as its JSON text: `true`, `{"check_ci": false}`. **An empty line is not
sent.** **Add a field** names a field the reasoner does not list. A word in
braces that is not a job fact is refused on the line ("{branch} is not a fact
of the job") and the step cannot be added until it is fixed; a needed field
left empty is said ("Nothing fills plan_review; the run may refuse it") but
does not stop the add.

Add step writes exactly those lines as the step's `input:` in
`.codeaf/factory.yaml`, through the same preview and pull request as any
other edit. The input of a step already in the file is not edited in place:
remove the step and add it again, or edit the file.

## an AgentField step that did not succeed — why my job failed, no pull request was opened, the project does not build

When an agentfield step's run ends without doing its work, the job reads
**Failed** and the step is named by its label, never by its reasoner
(`node.reasoner` is only the pipeline file's word). The failure reads
"<step> did not succeed: <why>", for example "Build on AgentField did not
succeed: The project does not build or its tests fail, so no pull request was
opened". The short line on the job, its Brief and Afi's notice is the `why`
alone.

The `why` is the run's own last word, read from the shape of its words and
never from a pipeline's or a step's name:

- the note the run left on the control plane that says how it ended, else
  the first sentence of its result's summary. A note is chosen by what it
  says, not only by when: one that says no pull request was opened, or that
  the change was empty, wins over one that says something failed, which wins
  over a note that only reports progress. So a tidy-up note written after
  the decision ("Repo finalized: …") is never the reason a build failed; among
  notes that say as much, the newest wins;
- a report that starts with a grade (`OVERALL: FAIL.`) gives the reason after
  it, cut before any command it quotes, so what a command printed is never on
  the line;
- a note that starts "No pull request:" reads "<reason>, so no pull request
  was opened";
- when the run's words name the command that failed, short and plain (a
  quoted `` `go build ./...` exited 1 ``), the line names it, never what it
  printed: "The build did not pass its checks (go build ./... failed), so no
  pull request was opened". A command too long for the line, or not plain, is
  left off;
- a run that failed or was cancelled on the control plane reads "<step>
  failed: <reason>" or "<step> was cancelled: <reason>".

The run's whole report, command output included, is kept as the step's
output: open the step to read it in full. A step that finished reads
"<step> · done · finished", or "finished and opened <pull request link>".

## a failed, rejected or stopped job on disk — its file says so; a step reads as its label

A failed job's file on disk says `"State": "failed"`, a rejected one
`"State": "rejected"` and a stopped one `"State": "stopped"`; each is read back
as a job that is not running with that end on it, so Run again, Reopen and
Carry on work as before.

A step's details read its label ("Build on AgentField"), never the
`node.reasoner` the pipeline file names it by.

## an AgentField run that changed nothing — nothing to change, no pull request, already done

When a run ends having changed nothing — the work the issue asks for is
already on the default branch, so its notes say the change came out empty
("No commits between main and …", "the run changed nothing") — the step is
done and nothing failed, but nothing shipped either. The job reads **Nothing to
change** with the line "Nothing to change: the run made no changes, so no pull
request was opened." It is a finished job and stays in All jobs; it is not
shown as shipped, and its log says that line instead of "the AgentField run
finished". Close the issue yourself if the work really is there, or Run again
with clearer words if it is not.

## an empty pull request — 0 files, changes nothing, closed, no Merge

A run may still open a pull request that changes no files (its commits are
empty, so the forge never said "No commits between"). Once GitHub's file list
for the pull request's head is empty and the pull request's own counts agree
(0 files, 0 lines), the job reads **Nothing to change** with "Nothing to
change: the pull request changed no files, so there is nothing to merge." and
offers no Merge ("the pull request changes no files"). An empty list GitHub's
counts contradict is not yet read, never empty.

A pull request this factory's job opened is then closed with a comment saying
it changes no files, and the job notes "Nothing to change: pull request #<n>
changed no files, so it was closed." One someone else opened is left open; its
job still offers no Merge.

## merge a pull request an AgentField run opened — which commit is approved, "no approved commit is recorded"

A job whose pull request was opened by a run on another machine (an
agentfield step, or a `run_on` job) has **Merge** like any other job, once
the run has ended and the job reads shipped. No proof was approved on this
machine, so the approved commit is the head the pull request carries when the
factory first reads it after the run ended: the run's own last commit. The
merge is held to exactly that commit, as a native job's merge is held to the
commit its proof approved. A push to the branch after that is refused with
"the pull request has commits you did not approve". Checks, conflicts, drafts
and reviews are read as for any other job.

This is the same whether the job started from the backlog or on a person's
yes to "Start this job?" from a route that asks first: that yes approves the
start, not a commit, and the job still ends as its run did, with no Follow PR
step of its own.

Until the factory has read the pull request once after the run ended (its
next GitHub read, within a minute) Merge says "no approved commit is recorded
for this job"; that refusal never lasts past that read. Such a job is not
watched for CI repairs: there is no checkout on this machine to repair it in.

## approve the plan an AgentField run asks about — Needs you, ask for changes, reject

When a reasoner pauses for a person (a plan review, or any `Pause`), its
job goes to Needs you with the reasoner's question and the WHOLE plan or body it
asks about, readable in full. Three answers:

- **Approve** — the run goes on.
- **Ask for changes** — your words go to the run, which may make a new plan and
  ask again. The new round shows the new plan, the plan before it, and the
  words you sent. It needs words: "say what to change: Ask for changes sends
  your words to the run".
- **Reject** — the run stops and the job ends **Rejected**, with your words if
  you gave any (at most 4000 characters). It is done: it offers Reopen or Run as
  new, never Carry on, and it leaves Needs you. The control plane cancels a
  rejected run at once, and the job still ends Rejected, never "Step failed" or
  Try again, whichever reaches the factory first. The step the plan was asked
  in carries no failure: the rail reads it rejected there, as a Reject of one
  of this factory's own approve steps reads. A no given on the control
  plane's own page ends it Rejected too, with the words its cancel carries
  (`approval_rejected: <words>`). A run that failed on its own is still a
  failed step.

Only the job's person answers; the manager never does. Anyone else is refused
with "only @<person> can answer this". The answer goes to the control plane,
which keeps the one approval: if it was answered first somewhere else (hax, the
control plane's page) the job's question comes down by itself, and a late
answer says "this was already answered somewhere else, and that answer stands".

## restart while an AgentField step runs — the run goes on, not a retry, no "interrupted" round

Restarting codeaf, or its runner going away, while an agentfield step's run is
still going on the control plane interrupts nothing: the run keeps going there.
The job waits for its runner again ("Waiting to start"), and the runner that
takes it follows that same run. There is no second run, no "interrupted"
round, no "trying <step> again", and no automatic retry is spent, however many
times it restarts. Only a person's Stop cancels the run.

This holds wherever the job was when its runner went away: running, or
waiting in Needs you on the run's approval. A job whose run goes on with no
runner following it is taken again by the next runner, even if you approved
it while no runner held it, and the run's end (its pull request, done, or its
own failure) lands on the job. It never sits reading "running" while its run
finishes somewhere else.

A **Stop** reaches the run whether or not a runner holds the job when you
press it. With no runner holding it, the job is stopped at once and the next
runner to come back cancels the run on the control plane and writes its end,
so the run does not go on to open a pull request.

When the run a re-taken job follows ends, the job lands on what it did: the
pull request it opened, done, or its own failure. Only the agentfield step
reports. The run's own calls (its plan, its coder, its review…) are drawn
under that step and are never steps of the job, so a run picked up after a
restart cannot end in "The factory refused this step's results (HTTP 422):
report names an unknown step". Picking a job up again records nothing on its
activity: no blank "failed" line. The step keeps the start time it had before
the restart, and its chat reads "resumed" without a second "<step> started".

## quit just after an AgentField run ended — read after the restart, never started again by itself, Manual too

A run that ended while its step was still running here — you quit codeaf a
few seconds after the run finished — is never "Runner went offline mid-step".
Its end is already on the job and is owed to its step, so the runner that
takes it after the restart reads that end as the step's: done with its pull
request, or the run's own outcome ("No pull request: …"). No second run is
started, it does not ask for the plan's approval again, and this holds in
Manual too, however the job came back (a clean quit's hand-back, a hold that
ran out) and whatever the step's start time says. A step you start again
yourself after it read its run's end (Run again, Try again, Reopen) starts a
run of its own; a run someone stopped or rejected is never read as a later
round's end.

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
- A Reject ends the job, rejected with your words, once the runner has passed
  it on.
- An answer survives a restart of the factory, the runner or the desk: the
  runner that takes the job again sends any answer the control plane has not
  had yet.
- If the control plane refuses the answer, the question comes back and the job
  says "the AgentField control plane did not take the answer: …".
- A Reject given while no runner holds the job ends it rejected at once, and
  the control plane's pause then runs out on its own.

## a question from an AgentField step — the harness step asks, the manager or you answer

A codeaf harness step on a node may ask a question while it works. It no longer
takes its own default with nobody to ask: the step waits, and the question
comes to the job like a question from one of this factory's own steps, first to
the job's manager and then, if the manager sends it on, to you in Needs you with
its options and the step's own pick. Your answer goes back to the step, which
reads "the person answered: …" and carries on. The options are its answers:
the app shows each as a button beside "In my words…", and the job's question
says it is a step's (`needs_you.question.kind` is `step`).

A question is asked once. After you answer, the step keeps working until it
reads your answer, and the question is not put to you again in that time,
even if the factory restarts. When the step takes its question down without
an answer, or the run ends first, the question comes off the job: it leaves
Needs you and reads running again, and the run's end still ends the step,
with the pull request it opened on the job.

A question the step marks as one only a person can answer (the `ask` tool's
judgement kind) skips the manager and comes straight to you, saying "the step
said only a person can answer this", exactly as one asked on this machine. So
does every question of a job in Manual.

## what the AgentField node cannot do

- Only the factory on this machine connects; a team factory (`codeaf factory
  serve`) does not run the node.
- The runner still needs GitHub, even when every job comes from AgentField.
  Turning it on without a GitHub sign-in is refused with "connect GitHub
  before turning the runner on: the factory needs to know whose jobs these
  are": every job the runner takes is held under a person's GitHub login.
  AgentField entries are still filed while the runner is off, and are routed
  and started once it is on.
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
