# The factory


## factory v2 jobs — states, reason, done and job id

The factory API reports contract 2. A job has exactly one state: `needs_you`,
`running`, `queued`, `backlog` or `done`. Its human `id` is the short repository
and issue number, for example `api#1498`; `document_id` preserves the binary's
numeric document identity. `/v1/jobs/{id}` accepts either identity (encode `#`
as `%23` in a URL). Existing runner routes keep numeric lease and job fields.

A published PR stays Running while CI or reviews run (`reason: github`). Changes
requested or a merge conflict → Needs you. Merged → Done with `done_how: merged`; closed without a
merge → Done with `done_how: closed`. A published no-diff answer is
`done_how: answered`. Drop is `done_how: dropped`. The app can keep Done jobs in
its Running view for one day using `done_at`; the binary keeps their history.

`reason` explains Needs you or Running: `approval_plan`, `approval_proof`,
`approval_start`, `question`, `budget`, `out_of_rounds`, `failed`,
`runner_offline`, `changes_requested`, `conflict`, `publish_failed`, `step`, `publishing`
or `github`. Other states omit it. Someone other than the assignee sees
`waiting_for: @login`. `overlap` names two Running jobs in the same repository
whose actual branch changes include the same file; planned paths alone do not
make an overlap.

## factory v2 priority, size labels and a job's own budget

Priority is Urgent, High, Medium or Low. It is a person's choice, not an AI
ranking. `POST /v1/jobs/{id}/priority` takes `{"priority":"high"}` and records
`set_by`. Without a choice, labels supply defaults: `priority:urgent` or `P0`,
`priority:high` or `P1`, `priority:medium` or `P2`, `priority:low` or `P3`.
`bug` gives Medium only when there is no explicit priority label; no label
leaves priority unset and sorts last. Runners take higher priority first, then
older jobs. Start now moves a job ahead of the ordinary queue.

Size comes from `size:XS`, `size:S`, `size:M`, `size:L` or `size:XL`. No size
label leaves it unset; the app shows “—”. The API never uses the AI read's
size or priority for these fields.

A job's `budget` starts at the repository pipeline's `default_budget` (default
$3), separate from an agent task cap. `POST /v1/jobs/{id}/raise_budget` takes
`{"budget":10}`. Only the assignee may raise it, to a higher $5, $10, $20 or $30
limit. Reaching the budget asks the assignee; raising a reached limit resumes
the job. A personal pipeline may tighten the default, never widen it.

## factory v2 approvals — plan, proof, start, Approving and Undo

`approval.kind` is `plan`, `proof` or `start`. Start means a rule asked first;
`matched` keeps the rule and the fact that matched (path, size or budget).
Only the assignee approves. An unassigned job must be assigned first.

`POST /v1/jobs/{id}/approve` takes the job's `head_commit` as `commit` for proof,
plus `edited: true` when consenting to an unproven claim. It first checks stale
reads and proof. Success records an `approving` decision with `decided_by`,
`at` and `undo_until`, and returns the approval plus `undo_until`. For five
seconds no answer or publish door opens. The app says “Approving… Undo”.
`POST /v1/jobs/{id}/undo` withdraws that caller's decision before the deadline.
After five seconds the server writes the approval receipt and resumes or
publishes. The decision survives server restart. A changed head or refused
door leaves the approval waiting with an error; it is not published.

A plain question may be answered by anyone who can see the repository. The
first accepted answer wins. A second writer receives HTTP 409 and the exact
copy “@jm answered this 10s ago. Your answer wasn't sent.”, with the actual
person and elapsed seconds. Approval and budget asks are assignee-only.

## factory v2 Run, Start now, Stop, Dismiss, Drop and Unassign

`run` queues Backlog work; `start_now` puts it first in the queue. A rule that
asked first needs start approval before either verb can run it.
`stop` ends Running, Queued or Needs you work and returns it to Backlog. Its
branch stays; `undo` within five seconds restores the saved run (Running
returns through Queued for a fresh runner lease).

`dismiss` sets a per-person flag without changing state or stopping work.
GitHub activity clears it. The default queue hides that person's dismissed
jobs; `GET /v1/queue?q=is:dismissed` retrieves them.

`drop` is final: Done, `done_how: dropped`. It stops the runner, leaves the
GitHub issue open, and keeps the branch seven days. The service then deletes
the factory branch and records `branch_deleted` in Activity. If deletion fails
it retries; it never claims the branch was deleted before success. Run and
Run again cannot revive Drop.

`assign_to_me`, `change_assignee` (`{"assignee":"mei"}`), and `unassign` change
ownership. Assign to me works only when unassigned, including refusing a job
already assigned to you. Only the current assignee can Change assignee or
Unassign; other people cannot reassign in the app. Changing Running work stops it first; Unassign leaves Backlog with
no assignee. Assignment is mirrored to GitHub. `nudge` sends a person-facing
Activity event to the assignee, never a steer to their AI. `run_again` retries failed work or queues
a PR closed without merge; it refuses merged or dropped work.

## factory app access token and hand-off from an agent

An app-hosted factory identifies itself with `served_by: app` and
`access: token`. Its per-launch bearer token is required even on loopback,
including `/v1/about`; another local process and a browser cookie are not
the owner. Runner credentials still authenticate the runner protocol.
The factory mount and app hand-off card are supplied by the app integration
lane; this factory API support alone does not connect an app factory.

`POST /v1/jobs` accepts repo, title, text, `source: hand-off`,
`from_ref: {kind:"agent", id, title}`, assignee and start. The originating
agent ref is kept under `ask.from_ref`. `start: now` queues with precedence;
`queued` joins the ordinary queue; `backlog` runs nothing. A start applies
the pipeline's default budget. An explicitly empty team assignee is allowed
only for Backlog. Invalid source/ref/start and combining start with legacy
autostart are refused before creation. Repeated Idempotency-Key sends return
the original response. New metadata requires a store-backed factory.

## factory v2 Activity and compatible runner fields

`GET /v1/activity` is the append-only Activity record. `?format=csv` exports
it, and `?since=<RFC3339 timestamp>` limits it to later events. Events cannot
be edited or deleted. Activity says assigned and unassigned where older
records said took or released. `GET /v1/activity` remains a compatibility alias.
Failure fields expose `rounds` and `runner`; older `attempts` and `machine`
fields remain for existing clients. The older `gate` field remains alongside
`approval`; new clients use approval. Contract 2 is required because the job
id is now text, Drop is Done, and an open published PR is Running.

## codeaf factory serve — HTTP service, listen address and worker execution

`codeaf factory` (also `codeaf factory serve`) runs the factory HTTP service alone,
without opening chat or running a job itself. It prints the address to give people:
`https://` and the domain when it serves HTTPS, otherwise its bound `http://` address.
The default is `127.0.0.1:7420`. Use `--listen 127.0.0.1:0` for an available port,
or `--listen 0.0.0.0:443` with `domain` set to accept connections from other machines
over HTTPS (see hosting a team factory over HTTPS).
`GET /v1/queue` reads the saved jobs over loopback without an access token.
`GET /` is a one-line pointer to codeaf (TUI), the codeaf app and `/v1`, not a dashboard.
The team view lives in the codeaf app (`Agent-Field/codeaf-desk`).

This service process has no GitHub polling or worker loop.
It does read who the token belongs to (`GH_TOKEN`, `GITHUB_TOKEN`, a saved token,
or gh once allowed), so `GET /v1/github` names the login and an approval from the
app is recorded under that handle. Run queues a job; an attached worker pulls it,
runs it on its own machine and reports back. Stop, pause, answers and steering
reach that worker through its lease. With no worker attached, a job stays `queued`.

## factory host config — config.yaml, access token and watched repositories

The factory host config is `~/.codeaf/factory/config.yaml`; `codeaf factory --config path`
selects another. `--listen` overrides its `listen` value. Supported keys are optional `name`, `listen`,
`access_token`, `repos` (an array of `owner/name` strings), `github_client_id`
(the OAuth app client id with device flow enabled), optional `team` (GitHub
handles, such as `team: [priya, "@sam"]`), and the HTTPS keys `tls`, `domain`,
`cert_file`, `key_file`, `cache_dir` and `trusted_proxies`. A missing token is generated
and saved in a private file. Unknown keys, invalid repository names and multiple YAML
documents are refused. A supplied `repos` list replaces the watched repositories;
an omitted list keeps the saved selection, and `repos: []` clears it.
This host config is separate from a repository's pipeline file.
`CODEAF_HOME` moves the default config and factory discovery paths.

## host a team factory over HTTPS — tls auto, a certificate, domain and port 443

Off this machine `codeaf factory serve` serves HTTPS. The host config key `tls`
picks how: `auto`, `files` or `off`. Unset, it is `off` when `listen` is a loopback
address, `files` when `cert_file` is set, and `auto` everywhere else.

`tls: auto` gets a free certificate from Let's Encrypt for `domain`, the public
name (for example `domain: factory.example.com`; no `https://`, no port). DNS for
the name must point at the machine. Use `listen: 0.0.0.0:443`; port 80 on the same
interface, when it can be opened, proves the name and redirects plain HTTP to
HTTPS. The certificate is requested on the first HTTPS connection, renewed on its
own, and kept in `cache_dir` (default: a `tls` folder beside the config file).
Ports below 1024 need root or `cap_net_bind_service` on the binary.

`tls: files` serves your own PEM pair from `cert_file` and `key_file`, and reads
them again when either file changes, so a renewal needs no restart. A pair that
cannot be loaded refuses to start.

It refuses to start, in plain words, when `tls` is auto (or unset off loopback)
and `domain` is missing: `auto needs domain, the public name its certificate is
for`, followed by the three fixes. Other refusals: `tls must be auto, files or
off`, `cert_file and key_file go together`, and `tls: auto cannot work on` a
loopback address.

Over HTTPS every response carries Strict-Transport-Security, the session cookie
is Secure, and `GET /v1/about` reports `scheme` `https`. GitHub device sign-in and
access links work unchanged. On this machine, windows and `revoke-sessions` keep
using plain HTTP on a loopback port that `serve.json` names.

## a team factory behind a reverse proxy — tls off, trusted_proxies, Caddy and nginx

When Caddy, nginx or a load balancer already serves HTTPS, set `tls: off`, keep
`listen` on loopback (for example `127.0.0.1:7420`), point the proxy at it, and
list the proxy's address in `trusted_proxies` (addresses or ranges such as
`10.0.0.0/8`). Set `domain` to the proxy's public name and codeaf prints
`https://` and that name.

Only a peer in `trusted_proxies` is believed about `X-Forwarded-For` and
`X-Forwarded-Proto`; that decides the Secure cookie, Strict-Transport-Security and
the `scheme` in `/v1/about`. A request with forwarding headers from any other peer
is never treated as this machine, so an unlisted proxy cannot open the loopback
floor; callers through it need a token or a sign-in.

The event stream `/v1/events` must not be buffered: Caddy needs `flush_interval -1`,
nginx `proxy_buffering off` (codeaf also sends `X-Accel-Buffering: no`).

With `tls: off` and a `listen` address that is not loopback, `codeaf factory serve`
prints a `warning: serving plain HTTP` line naming these options: tokens and
sign-in sessions would cross the network unencrypted.

## codeaf factory name — show or change the factory display name

`codeaf factory name` prints the factory's display name. `codeaf factory name "Our team"`
saves `name: Our team` in the host config and prints it. Both accept `--config path`
before the text. Words can be quoted or passed as separate arguments.
The name is trimmed, limited to 40 Unicode characters, and cannot contain control
characters (including tabs or newlines). An empty quoted string clears the setting.
An unset name uses the owner of the first watched repository in the saved, sorted
selection; with no repositories it uses the factory machine's short hostname.
Reading a default does not save it or create an access token.

Restart the factory service after changing its name or watched selection. The name
is chosen at start and is the same for every caller. Every `/v1` response carrying
`factory` includes `factory.name` beside `address` and `version`. `/v1/about` also
keeps its top-level `name` with the same value. Contract remains 1.

## factory discovery — serve.json, bound address and stale service receipts

While serving, `~/.codeaf/v3/factory/serve.json` contains `{url, pid, token}`;
its `url` is the actual bound address, including the selected port. The file is
private and removed on normal exit, interrupt or SIGTERM. A forced kill can leave
a stale file: codeaf ignores it unless the process is alive and the factory answers
`GET /v1/queue`. One service may run per codeaf home; a second refuses instead of
replacing its discovery. `CODEAF_HOME` moves the discovery path.
A window serving its factory in-process removes `serve.json` when it closes.

## how do I attach to a team factory — codeaf attach and codeaf detach

Run `codeaf attach <url> [--name name] [--token token]`, then open a new `codeaf`
window and `/factory`. The URL is the factory service's address, such as
`https://factory.example.com` or `http://team-host:7420`. `--name` is how this machine is
named there (default: its hostname). For a factory off loopback, ask its operator
for the access token printed by `codeaf factory token` on the factory's machine,
or one minted by `codeaf factory token add`, and supply it with `--token`.
Remote calls without that token are refused.

`codeaf attach` saves the address, name and token in the private
`~/.codeaf/v3/factory/attach.json`; it does not dial the factory or prove it is reachable.
New windows use the saved factory. Windows already open keep their factory until
restarted. One codeaf home attaches to one factory at a time.
`codeaf detach` removes `attach.json`; new windows then find or serve this machine's
factory again. `CODEAF_HOME` moves the attachment path.

The factory's jobs run on workers, using the worker machine's checkouts and model keys.
An ordinary codeaf launch runs a worker against its selected factory; `--no-worker`
turns it off. For a remote factory, this machine needs its own connected GitHub login
before its worker can start. With no worker available, a job stays `queued`.

## which factory a window talks to — service discovery and the factory address line

The factory page reads its jobs through HTTP. When an ordinary local `codeaf`
window starts, it selects the factory in this order:

1. `~/.codeaf/v3/factory/attach.json`, written by `codeaf attach <url>`.
2. A live `serve.json`: another codeaf on this machine already serves it, its process
   is alive and it answers `GET /v1/queue`.
3. Otherwise the window serves this machine's factory service on `127.0.0.1:0`, loopback
   only and without an access token, and writes `serve.json` for the next window.

A second window on the same machine reads the first window's factory. If the serving
window closes, another window finds or starts a service on its next read. The handover
line ends with its address, for example `factory · 127.0.0.1:53121`; a narrow line drops
it first. Run, stop, pause, answer and the other HTTP job actions go to that factory.
Repository checkouts and recipes stay local. Job keys that need a local run are offered
on an attached remote factory only while this machine holds that job's live worker lease
(see attached factory on another machine — local job keys and worker leases).

## factory over --host or --at — attach to a team service from a remote chat window

A chat window using `--host` or `--at` can read the factory named by
`codeaf attach <url> [--token token]` on this machine. Attach before opening the
window, then use `/factory`. The attachment comes from this machine's `attach.json`,
not the far engine's home. The far engine's own factory listens on its loopback;
this window does not discover or forward that service automatically.

## factory one-model pin — triage, size and estimate

`codeaf factory --one-model --model X` pins every text call the factory server makes to `X`,
including the triage read that supplies size and estimate. Without `--model`, `--one-model`
uses the same saved or default chat model as the desk. The `small work` row stands down while
pinned. A factory served in-process by plain `codeaf --one-model --model X` inherits the desk's
pin; new manager and stage conversations on that desk follow it too.
An already running factory has its own launch: restart it with the flags to pin its calls.

## codeaf factory token — print or rotate the access token

`codeaf factory token` prints the host config's access token, creating and saving
one if it is missing. `codeaf factory token --rotate` replaces it with a fresh
random token and prints that token. Both accept `--config path`.
`codeaf factory token add` mints a separate access token with an expiry (see
give someone access to this factory). `codeaf factory token list` lists those
rows by id and expiry, without printing the secret. `codeaf factory revoke <id>`
removes one.
Restart a running factory after rotation: the running service and its discovery
file keep the host token loaded at startup until they exit. A row from `token add`
is read from disk on the next request, so it does not need that restart. Remote
callers send `Authorization: Bearer <access token>` or use a live browser session;
loopback callers do not need either.

## give someone access to this factory — codeaf factory token add, token list, revoke

`codeaf factory token add [--expires 7d]` prints one access token and nothing else.
`--expires` defaults to `7d`. `24h` and other Go durations work. Zero or a
negative duration is refused: `expiry must be a duration like 7d or 24h`.
`--config` selects the same host file `codeaf factory` uses.

The token is a row in `tokens.json` beside `config.yaml`, with `expires`.
`codeaf factory token list` prints each row as `id time` and does not print the
secret. A revoked row is hidden. `codeaf factory token` still prints the host token.
`codeaf factory revoke <id>` retires that row and prints `revoked <id>`. The
secret is replaced by a hash, so a later use can be told the link was revoked.
Revoking an unknown id, or a row already revoked, is refused: `no access token <id>`.
A running service reads the file on the next request, so a revoked or expired
token stops working without a restart, including a browser session created from
it. `codeaf factory revoke-sessions` signs every browser out and does not delete
these rows. A row from `token add` cannot revoke sessions; that still takes the
host config's access token.

Off loopback, send `Authorization: Bearer <token>` or enter the token at
`/access`. `codeaf attach <url> --token <token>` saves the address and token in
`attach.json`. `CODEAF_HOME` moves `tokens.json` with the rest of the factory
host config.

## a refused link — expired, revoked or unknown

A bearer token or an `/access` token that was sent and refused answers HTTP 401
`{"reason":"expired"|"revoked"|"unknown"}`. `expired` is `this link has expired`.
`revoked` is `this link was revoked`. `unknown` is `this link is not recognized`.
`invited_by` and `created_at` are set for an expired or revoked row the factory
still has, so the app can say ask @santosh for a new link. An unknown token
has neither. A request that sent no token keeps
`a bearer access token or live session is required; open /access to enter the token`.

## factory browser access — /access, session cookie and revoke-sessions

On another machine, open the factory service's `/access` page and enter the access
token from `codeaf factory token`. The form submits by POST; alternatively visit
`/access?token=<access token>` once. Success sets a random session cookie and
redirects to `/`, removing the token from the URL. A token that is expired,
revoked or unknown returns HTTP 401 with `reason` `expired`, `revoked` or
`unknown`, and creates no session. `invited_by` and `created_at` are set when
the row is still known. The token is never stored in the cookie.
Malformed forms or submissions larger than 4096 bytes return HTTP 400 with
`invalid access form`.

The cookie is HttpOnly and SameSite=Strict, covers `/v1`, and
expires after 30 days. It is Secure when the caller's connection was HTTPS,
received directly or through a proxy listed in `trusted_proxies`.
Sessions are saved under the factory home and survive restarting the service.
Only a hash of the cookie credential is saved; the cookie itself stays on the device.
An expired, unknown or revoked cookie returns HTTP 401 off loopback.
A session that reaches its own expiry returns `reason` `expired` and
`error` `factory session has ended; sign in again`.

`codeaf factory revoke-sessions` signs every browser out of the running service
for this codeaf home and prints `factory sessions revoked`. It accepts no arguments
and uses the private discovery receipt to reach the local service. Without a
running service it refuses. Bearer callers keep working; the token is not rotated.
Rows from `codeaf factory token add` are not deleted; revoke one with `codeaf factory revoke <id>`.
After revocation, enter the token at `/access` again.

## codeaf factory steps — print built-in step prompts

`codeaf factory steps` prints pipeline's built-in prompts and stop sentences: plan, approve,
write, test, review, security, proof and read. These are the YAML defaults; security runs
only when a pipeline names it.
`approve` is a person's approval rather than a model prompt. Copy a prompt under
the same name in `.codeaf/factory.yaml` to override it; add a new name to `pipeline` to
run a custom step. This command needs no model key, changes no jobs and accepts no arguments.

## workers, taken jobs and lease expiry

The factory service's claims desk keeps one lease per job and one job per worker in
`leases.json` beside the saved jobs. A worker pulls queued work; it does not receive
backlog jobs. A job belongs to its forge assignee, otherwise its taker, otherwise the
person who started it. The issue's author is not its person. Only that person's workers
may pull it. Unassigned work requires the worker's explicit opt-in for that repository;
that opt-in never permits taking someone else's assigned job.

Taking a job records its person without starting it. A worker's heartbeat renews its
live lease. When the lease expires, running work returns to the queue with its person
kept; completed work and work waiting on a person keep their state. A late heartbeat
cannot renew an expired lease. Releasing a lease early also returns running work to
the queue.

Workers introduce their person by the connected GitHub login, using the same
identity resolution as Take, Approve and `/v1/me`. With no local GitHub login,
the worker uses the factory's connected login. The operating system's username
is never a forge handle. Without either login, attach is refused rather than
registering a different person from the one who takes the job.

**Where a job runs.** One codeaf process per home runs the worker loop against its
factory (the one it serves, the one in `serve.json`, or the one in `attach.json`).
Other terminal or app windows share that runner; they do not register more slots.
The runner holds `worker.lock` until its slots stop. A waiting process takes over
within one second after it exits and reuses its saved registration credentials.
`--no-worker` turns off participation for that process,
and `CODEAF_FACTORY_NO_WORKER=1` does the same for a session host. The worker attaches once
per slot (its credentials are kept in `~/.codeaf/v3/factory/worker.json`, per factory
address), pulls a job, and runs it with this machine's runner in this machine's checkout
and the job's own worktree. Two jobs run at once by default (`CODEAF_FACTORY_BENCHES`). In a solo
factory every job is yours. In a team a runner takes only its owner's assigned jobs;
unassigned jobs do not run. At the 60-second heartbeat deadline, a running job
becomes Needs you: `Runner went offline mid-step`. Only a worker on the machine
holding its worktree may resume it or publish it; a new slot or process there goes on at
the step it stopped in. While queued it says `waiting for <machine>`. If that machine
is offline, it waits for that machine to return.

**How your verbs reach the worker.** Run puts the job in the queue. Stop puts it back to
backlog at once and takes its lease away; the worker stops its run when it next hears from
the factory. Pause, resume, an answer and steering are left on the job's lease, and the
worker carries them out within about two seconds. `shape steps` on a job a worker holds is
left on that lease too; a job nobody holds is shaped by the factory the window is attached
to, which is where that job's worker lives. Approve and request changes act on a
landed job; request changes and re-run checks queue it for a worker. There is no `run.lock`
and no mailbox any more.

## /factory — the factory page

The **factory page** is a place, like home, activity and spend. Work split off by chat or
sent by a connected repository stands in rows grouped by the state of each job. Open
it with `/factory`, `alt+9` (`opt+9` on a Mac), the map (`alt+.`), or the `Factory` button on the
tab bar, third after `Home` and `Chats`. `esc` goes back to the conversation.

The page reads the selected factory's jobs through its HTTP service (see which factory a
window talks to). Jobs you add with `n` or from a chat are kept by that factory. Until the
first one arrives the factory draws one dim line under the handover,
`work arrives here from chat, from n, and from the repositories you connect` (or, with
repositories watched, what their read is doing; see after connecting github), and `n new` still
works. When no factory connection is available, the page draws
`nothing connected yet · the factory arrives here when a chat splits work off or a repo is connected`.
The only key it offers then is `esc back`.

The factory is one layout in three widths:

- **120 columns and wider**: the handover runs across the whole width at the top (one line, or
  four rows after `h`; see the handover), then a blank row, then two columns: the rows on the left (58% of the width to start with, never under 70
  columns), a dim `│`, and the **peek** on the right (never under 40 columns), which shows the
  item under the cursor. The divider moves (see resize the split). With no item under the
  cursor (an empty factory, or a filter that matches nothing) there is no peek and no `│`: the rows
  take the whole width.
- **90 to 119 columns**: the handover on top, then the rows across the full width, with no peek.
  Press `enter` on a row to see the item whole.
- **Under 90 columns**: no handover; each row is only its mark, its short name and its title.

At every width the rows, the peek and the item page stop 2 columns short of the right edge, as
they start 2 columns in from the left.

The rows window follows the cursor when they do not all fit. With the cursor on the first item
the list is back at its top, its first group heading showing. A group heading moves with its
first row: walking onto a group's first item shows its heading above it, and a heading is never
left alone on the last row with its rows below the edge.

## the keys on the factory page

The factory names its keys in three places, each short:

- **The bottom bar is the way around**, never the row's verbs:
  `n new · R repos · m foreman · / filter · tab next place · esc back · ? keys`. A clause whose
  door is not there is left out (`n new` too while the row under the cursor needs you, where `n`
  answers no), and when the line is too long it drops clauses from the right of the factory's own,
  keeping `esc back` and `? keys`. `esc` says `esc clear` while a filter or a repository narrows
  the rows.
- **The peek's last row names the row's verbs**, at most five (see the peek's verbs).
- **`?` opens the key sheet**: every key with its word (see the ? key sheet).

The keys themselves:

- `↑` and `↓` (or `ctrl+p`, `ctrl+n`, the wheel over the rows) walk the items. Resting the
  pointer on a row selects it and the peek previews it. A click selects a row, a second click
  opens it; a click in the peek moves nothing, except on `▾ more`.
- `enter` opens the item on its own page (see the item page).
- `J` and `K` scroll the peek's description, `pgdn` and `pgup` a page; so does the wheel over the
  peek.
- `{` and `}` move the divider, `|` puts it back (see resize the split).
- `h` switches the handover between one line (the default) and four rows; remembered.
- `z` switches compact rows (one line each, the default) and comfortable rows (a long title on
  two lines, a dim line with the factory's read, a blank row between items). Not saved.
- `O` re-sorts the rows inside each group (see order the factory); the `?` sheet says the order
  in use, `O order · priority`.
- `g` opens the item on github (see open the item on github).
- `u` refreshes the item; `U` refreshes all, after asking (see when the factory is doing
  something).
- `p` is `shape steps`: the item's manager sets its steps now and runs nothing (see does opening
  an issue run anything).
- `space` selects or unselects a new item, and pauses or resumes a running one. `T` opens a chat
  about the item.
- `m` opens the foreman, the factory's own conversation for what to take first (see the foreman).
- `/` filters, `[` and `]` show one repository at a time, `A` shows the whole backlog.
- `esc` clears a filter or a repository first; pressed again, it goes back to the conversation.
- `R` repos, `E` recipe and `$` rail open the factory's settings.

## the peek's verbs — at most five, by the row's state

The peek's last row names what you can do to the item under the cursor, **at most five**, in the
order a person reaches for them, and only keys the factory can do:

- **New:** `enter open · r run · T chat · space select`. While a row is selected,
  `L run selected` takes the fifth place.
- **Running:** `enter open · x stop · T chat · space pause · S steer` (`space resume` while
  paused). Queued: the same without pause.
- **Needs you:** `enter open · y yes · n no · a in words · T chat`. At an approve step it reads
  `y continue · n send back · a in words`: `y` continues, `n` sends the run back to the step
  before, and `a` continues with your words.
- **Landed:** `enter proof · s approve · B request changes · v re-run checks · T chat`, with
  `e approve with changes` in place of `s approve` when a row on the sheet was not shown.

When the peek is narrow the row drops whole clauses from its right end, never half of one. Every
other key (the budget, the thinking, the stages, dismiss, refresh, open on github) is on the
`?` sheet and the item page.

## what a row is

A row in the factory is one **item**: an issue, a pull request, a red CI run, a chore,
or work a chat split off. Rows stand on one grid so they line up down the page: a state mark,
the **priority** in one cell, the short name right-aligned so the numbers line up by digit
(`#1538`, or `ci`), the title, the repository's short name, the facts, and at the far right how
long ago the item last moved (`12m`, `7h`). Under 90 columns a row is the mark, the priority, the
short name and the title.

The priority is the cheap read's guess at what to take first: `▇` first (in the accent), `▅`
second, `▃` third, `▁` fourth, dim; nothing for an item nobody ranked. A patched font draws
chevrons instead; a screen reader hears `!`, `:` and `.`. While the item is being read the cell
is a spinner instead. During a re-read of every job (`U`) only the row being read right now spins;
a row still waiting its turn shows a still dim `·` in that cell and `waiting to read`.

On a forge item the short name is a link to its page where the terminal takes links.

Groups have muted capital headings (`NEEDS YOU · 1 ───`) and one blank row between them, in
this order: `NEEDS YOU` (the question in its colour, `[y/n]` before the age), `LANDED`
(`3✓ 1✕`, waiting for you to approve, so it sits beside what waits on you, above the backlog),
`STREAMS` (a stage strip `●●◐○` and `review 26m`, or `queued`), `NEW` (`bug · S`,
`pr · ci ✓`, `ci red`) and `SHIPPED` (`shipped 06:00`). An empty group is not drawn.

After the first fact come the money (`$1.42/$5` spent, in money's green, or `~$3` estimated,
muted like the other facts, because it is a guess), the author, tags (`thin`, `dup #950?`,
`from chat ▸`, `terminal only`). While the item is being read, `reading…` or `refreshing…` (or
`waiting to read`) stands right after the state fact in place of the money, author and tags, and it
is the first fact a narrow row drops.
The title keeps at least 28 columns: when a row is too narrow for the title and every fact, the
facts drop as a column, the same fact from every row at once, the rightmost first and the state
fact last, so a fact missing from a row means the row has none, never that it ran out of room.
A long title then takes the width the facts leave. In the comfortable density (`z`) the title's
second line is as wide as its first. The row under the cursor sits on the same lifted background the
Chats list and the Teams page use.

## paused, stopped and skipped — what the marks mean

Each mark in the factory means one thing, so a held, ended or skipped stage never looks like one
that is running, failed or still to come:

- **Paused** (`space` on a running item): the row's lead mark and the held stage are a pause
  mark (`=` on a plain font, a pause icon on a patched one), muted. The row says `paused 13m`,
  counted from when the factory first saw it paused; the peek says `paused 13m · write ×3` and
  never `13m left` or what it was editing. The item page's head says `paused 13m`.
- **Stopped** (`x`): the item is new again and its lead mark is a stop square (`■`, or a stop
  icon), muted. The row says `stopped · branch kept`, the stage it stopped on wears the square
  with `· stopped`, and the line under the peek's stages says `test · stopped · branch kept`,
  never `✕` or the failure the stage was in the middle of.
- **Skipped**: a stage switched off, one whose condition does not fit (`when thin`), or one the
  run went on past, is a dim stroke (`–`, or a minus icon; `-` for a screen reader) with
  `· skipped`, never the `○` a stage still to come wears. On a shipped item a stage plan
  skipped reads `– neaten · skipped`.
- **Waiting to read**: during `U`, a row the read has not reached yet shows a still dim `·` and
  `waiting to read`; only the row being read spins.

## filter the factory, search for an item

`/` in the factory page opens a one-line filter box at the top of the list, and the list narrows
as you type. Every word you type has to hold. A plain word matches anywhere in an item's title,
repository, area, kind or author. These words mean something more:

- `risky`: any risk the triage read named (`touches money`, `has ui`), or a large item. `cheap`: estimated at $2.50 or less.
- `strangers`: written by someone outside the project. `mine`: assigned to, taken by,
  started by, or written by your linked GitHub handle. Assignment takes precedence over
  taking, and taking over starting; the author also matches independently.
- `spend`: about spend, billing, the ledger or caps. `ui`: in the tui, pages or render areas.
- `prs`: pull requests. `bugs`: bugs. `thin`: too underspecified to run without questions.

`enter` keeps the words and closes the box; `backspace` takes a letter back; `esc` throws the
words away. With the box closed, the words stay under the repository line, and `esc` clears
them before it leaves the page. When nothing matches, the list says
`no item in the factory matches`.

`mine` uses the factory's linked GitHub login, refreshed when you open the factory or
connect GitHub. Handles match without regard to case or a leading `@`. With no known
login, `mine` matches nothing and the filter hint says `mine needs your GitHub login`.

## repository identity and checkout keys

Factory jobs and repository keys on `/v1` use GitHub `owner/name`. A local
checkout key such as `api` is resolved from the clone's `origin` remote or a
matching watched `owner/api` entry. Existing jobs saved with that key migrate
when loaded; their state and age stay the same. A full checkout key keeps its
owner, so two owners' repositories with the same short name stay distinct.
An ambiguous or unresolved checkout alias is not advertised as a repository.
Viewer access and CODEOWNERS ownership use the resolved GitHub identity.

## show one repository in the factory

The first line of the list says which repositories it shows: `all repos`, or one repository's
trail, `Factory › codeaf · 7`: the repository's short name and how many items it has. `]` steps
to the next repository and `[` to the one before, and past the last one the list shows all of
them again. An item shows under a repository it arrived on or touches. That line, and the
filter's words under it, stay at the top while the rows scroll.

## go back to all repositories — the factory shows only one repo

Clicking the repository on an item page's trail, or `[` and `]`, leaves the factory showing one
repository. To see every repository again, press `esc` (the hint says `esc all repos`) or click
`Factory` at the start of the first line, `Factory › codeaf · 7`; resting the pointer on it
highlights it. The cursor stays on the item it was on. A second `esc` leaves the factory.

## older new items, the backlog, and A

The `new` group shows only what arrived in the last three days. Older open items are kept back,
and a dim line at the end of the group says how many, such as `12 older open items behind A`.
`A` shows the whole backlog under `new`; `A` again keeps the older ones back. Dismissed items
(`d`) are never drawn.

## select new items and run them together — space and L, what is select

`space` on a new item selects it, and its lead turns to an accent dot; `space` again unselects
it. Only new items can be selected. The selection is kept through every re-read and when you
leave the page and come back, until codeaf exits. While anything is selected the peek's verbs
offer `L run selected`, and `L` asks first, with the triage estimates summed:
`run 3 selected · ~$7? [y] go · [n] not now`. `y` runs every selected item, clears the
selection and says `launched 3`; `n` or `esc` keeps it. With nothing selected `L` runs the item
under the cursor. What the foreman selects (`m`) is kept with the factory on this machine and is
still there after a restart.

## pipeline and steps — a step is a prompt

A job runs a **pipeline**: its steps in order. Each step is a prompt run as an ordinary
conversation in the job's worktree, with the job, repository rules and earlier results as
context. Conditions belong in the prompt, such as "if this touches auth, review it for
secret handling; otherwise stop". YAML has no step kinds, `when` or `fanout` knobs.

`approve` is the reserved exception: a person, with no model prompt. The default issue
pipeline is plan → approve → write → test → review → proof. PRs use read → review → proof;
red CI uses read → write → test → proof. Publishing still needs a person's approval.
`codeaf factory steps` prints the installed built-in prompts to copy or override.

The peek shows a waiting job's steps as names and a running job's progress as marks.
The job page lists the steps. Steps skipped or switched off there are dim. On a waiting
job, `1` to `9` switch steps, `s` adds one in words, and `b` banks them as the repository's
shared YAML pipeline in `.codeaf/factory.yaml`.

The current runner holds nine steps per job. YAML names allow lowercase letters, digits
and hyphens, 2 to 32 characters; the runner removes hyphens and accepts one word, 2 to 12
letters or digits. A name it cannot run is reported as a pipeline problem. Keep names
short and distinct after removing hyphens. Once running, a step already started is never
changed; edits to later steps are read at their next round.

## the pipeline file — .codeaf/factory.yaml, factory yaml and the specification

Commit `.codeaf/factory.yaml` on the repository's default branch. codeaf reads it through
GitHub, so no local checkout is needed to read the rules. A PR cannot rewrite the rules
for its own run. Every key is optional; no file means the built-in pipeline, with no
automatic starts. The full file specification is `docs/design/factory/FACTORY-YAML.md`
in the codeaf repository.

```yaml
pipeline:
  issue: [plan, approve, write, test, review, proof]
  pr: [read, review, proof]
  ci: [read, write, test, proof]
steps:
  test: run the test suite this repo uses; fix what fails
  proof:
    prompt: show each claim in its own medium
    until: every claim has evidence
    max: 3
fixed: [approve, proof]
rules:
  - tests pass before anything posts
```

`start` controls automatic intake; `size` controls size measurement (see automatic intake
and size rules). `pipeline` lists steps per kind; `steps` overrides a built-in by the same
name or defines a new prompt. Merely defining a new step does not put it in a pipeline.
`fixed` keeps named steps from being changed or skipped. `rules` are sentences every job
obeys: recognised publishing rules are enforced by code; other sentences reach the model.

When YAML is absent, `.codeaf/factory.md` is read and converted in memory as read-only
compatibility input. YAML takes precedence. `b`, `E` save, habit banking and `factory_recipe` all write
`.codeaf/factory.yaml` through the same encoder. Banking preserves existing start rules,
size thresholds and unrelated prompts; policy and habits become `rules`. Use
`codeaf factory convert` to print a conversion and review its warnings.
Unknown keys and invalid values are named as problems; the rest loads. Invalid YAML syntax
stops automatic intake. A personal `~/.codeaf/factory.yaml` may only tighten repository rules.

## step prompt, until and max — repeating a step, built-ins and approve

In `.codeaf/factory.yaml`, a step is a sentence or a map with only `prompt`, `until` and `max`:

```yaml
steps:
  security: if this touches auth, review for injection and secret handling; otherwise stop
  review:
    prompt: review the diff carefully; fix what you find
    until: no findings
    max: 2
```

`prompt` is the work. `until` is the stopping condition; omitted on a new step, it means
the step reports done. An override keeps a built-in's omitted settings. `max` is the
number of rounds before the job stops and asks you, at least 1; without a round cap the
current runner allows one round. A second round gets the previous findings and must fix
them before checking again.

The runner checks `done`, `clean` / `no findings`, `green`, and `proven` / `every claim has
evidence` mechanically. Other `until` sentences become `Stop when: …` in the prompt;
the model must report completion. A round that ends without `stage_result` does not pass.
These are model conversations: "green" on a prompt step is its reported outcome, not
a YAML command executor.

The built-ins are plan, approve, write, test, review, security, proof and read. Run
`codeaf factory steps` for their exact prompts and pipeline groups. `approve` has no model prompt: it remains a
person even if a prompt is supplied for that name. Conditions and parallel work belong
in the prompt; the older `kind`, `when`, `fanout`, `gate`, `effort`, `proof` and `off`
knobs are not YAML step keys. `fixed` is a top-level list of step names, defaulting to
approve and proof, rather than a step knob.

## who may change a job's steps — the manager, plan, you and fixed steps

The manager may shape steps before a run. Plan may propose changes during it, but never
to steps already done or running, or before one. You may edit waiting steps from the
job's settings or in words. Each change goes through the same bounds in code.

In `.codeaf/factory.yaml`, `fixed: [approve, proof]` is the default. A fixed step's prompt,
thinking and rounds cannot be changed, and it cannot be skipped or switched off by the
manager, plan or you. Switching it on is allowed. Change the repository's pipeline file
on its default branch to change that constraint. The refusal says
`security is fixed by the recipe · change .codeaf/factory.yaml to change it`.

The runner also keeps names unique and limits a job to nine steps, limits a new ask to 240 cells,
and refuses to skip proof, an approve step or a step named by a publishing rule. One
refused part refuses the whole edit. Changes keep who changed them and why on the job.

An older Markdown recipe can still restrict plan with a section's `adapt`, `ask` or
`fixed` word. YAML has no section mode; its `fixed` list protects individual steps.
Converting a section-wide restriction warns about that difference.

## the pipeline is read from the default branch — a pull request cannot change its rules

codeaf reads `.codeaf/factory.yaml` through GitHub from the repository's **default branch**,
whatever it is named, then `.codeaf/factory.md` only when YAML is absent. No local checkout
is needed to read start rules. A pull request cannot change the rules for its own run.

Parsed files are cached by blob SHA and refreshed on each intake tick. If GitHub is not
connected or the connection is offline, a known local checkout is the fallback:
`origin/HEAD` (else `main`, else `master`), with the working file used only when no default
branch can be read. An absent file on GitHub means defaults; it never selects a different
local file. An HTTP refusal or YAML syntax error stops automatic intake and shows its
reason in the pipeline's `/v1` payload and the recipe page (`E`). A failed read without an
offline checkout also shows its reason. Personal `~/.codeaf/factory.yaml` limits still
tighten the result. Banking writes are not effective until the default branch contains
them, and cannot override an existing YAML file.

## the handover

The **handover** is what happened in the factory since you last looked. By default it is
one line across the top of the factory, then a blank row:

`◆ 2 shipped · 3 arrived · ? 5 waiting · $8.44 / $60 · polled 14s ago`

what shipped and arrived this stretch, what waits on you (in the question colour), the day's
spend against the day's rail, and when a source was last read. **Waiting counts the items that
ask a question and the landed items waiting for you to approve,** so `? 2 waiting` is one
question and one proof sheet; the full strip's `? 2 waiting on you` and the tab bar's `? 2`
count the same, and `nothing waits on you` means neither. A part whose count is zero is
left off; a narrow window drops parts from the right. While a source is being read the last part
says where the read is, `⠋ reading Agent-Field/CodeAF · 1 of 3`, or `github · ⠋ polling` when
the source does not say; while every item is read again, `⠋ refreshing 8 items · 3 done`. A
factory with nothing on it while a read is out shows that part alone, never `quiet`.

`h` switches to the four-row strip and back, and the choice is remembered in `factory.json` in
the codeaf home, beside the split:

1. `◆ handover · since 23:12 · 7h 12m · $8.44`, with a line out to the edge.
2. `✓ 2 shipped #1661 #1663 · 3 arrived · 1 question handled`, or
   `quiet · nothing happened while you were away`.
3. `? 2 waiting on you` or `nothing waits on you`, and at the right a `24h` sparkline.
4. `3 repos · github · chat · benches 2/6 · polled 4m ago`, and `$11.31 / $60 today` at the
   right.

The handover is drawn at 90 columns and wider, when the window leaves the rows six lines.

## the peek

At 120 columns and wider, the right column shows the item under the cursor as a short document:
blocks with one blank row between each, and a block with nothing to say is left out. Its title
stands on one fixed row, level with the first group heading when the list is at its top, and
stays there while the rows scroll under the cursor, so the peek keeps its whole height. In order:

1. The short name and title (the short name is a link to the item's page), then dim: repository,
   kind, size, author, age, and `github ↗` when the item has a page there.
2. For an item that needs you, the question exactly as the run asked it (`?` in amber, the
   words in ink) and, where the factory can answer, `[y] yes · [n] no · [a] in words`.
3. The factory's one-sentence read, or `⠋ reading…` while the first read is out.
4. Facts, dim: `touches money`, `maybe a duplicate of #7`, `thin`, `stranger`.
5. The budget and the thinking, `budget  $5   thinking  strong`, each only when the item has
   one: an item with no budget and no thinking of its own has no line here at all. Where the run
   holds for you is an approve step among the stages, never a chip here.
6. The stages: `●` done, `◐` running, `?` waiting on you, `✕` failed, `○` to come, the pause
   mark held, the stop square where a person stopped it, and `–` skipped, every cell of the
   strip one width (its longest name, at most 16 columns; a longer name ends in `…`), with the
   round as `2/2` on a stage that may run more than one. When the strip does not fit, whole
   cells drop and a dim `…` stands where they were; the running stage always stays. Under them,
   dim, why a stage stopped: `review · review is not clean after 2 rounds`, or
   `test · stopped · branch kept`.
7. A running stage's line (`review 1/2 · 3 findings · fixing`), a paused one's
   `paused 13m · write ×3`, what frees a queued item, or `merged 06:00 · $1.90`.
8. The description, rendered from Markdown (headings, lists, code, links as their words, never
   raw backticks), 60 columns wide, at most six rows, cut with `…` and `▾ more`. A landed item
   shows its claims here instead.
9. `chat`, when the item has its own conversation.
10. What the forge says, each block absent when empty: `comments` (the last three, `author · 2h`
    then the words), `checks` (one row per check: mark, name, state), `files` (`+218 −44 · 6
    files`, then up to six paths), `activity` (the last five events). The item's page on GitHub is the `github ↗` link in the meta row.

The bottom row names the row's verbs, at most five, starting with `enter open` (`enter proof`
on a landed item), with one blank row above it, and only keys the factory can do: a still factory
with no verbs says just `enter open`. It is cut from its end to the peek's width (see the peek's
verbs).

## resize the split — { } |

The divider between the factory's rows and the peek can be moved at 120 columns and wider. `{`
moves it 4 columns left (a wider peek), `}` 4 columns right (wider rows), and `|` puts it back at
58% of the width. You can also drag the divider with the mouse: press on the `│`, move, let go.

The rows never get narrower than 70 columns and the peek never narrower than 40; a key or a drag
past either stops there. The position is remembered as a share of the width in `factory.json`
in the codeaf home, beside `config.json`, so it comes back on the next launch and keeps its
proportion on a narrower or wider terminal. Under 120 columns there is no peek and the keys do
nothing.

## read the whole issue — J K on the peek, the issue row on the item page

The peek shows at most six rows of an item's description. When there is more, its last row ends
in `…` with a dim `▾ more` at the right. To see the rest:

- `J` scrolls it down a row and `K` up a row; `pgdn` and `pgup` move a page.
- The wheel over the peek scrolls it three rows a notch. Over the rows the wheel walks the items.
- A click on `▾ more` scrolls it a page.
- `enter` opens the item's page, where the whole description stands (below).

The scroll stays where you left it while the factory reads itself again. Moving to another item
starts that item at its top.

To read everything, open the item (`enter`, or click its row twice). The item page's left column
starts with `issue`, and its pane is the whole description rendered from Markdown at 72 columns, then
`read 3m ago · u again` (or `⠋ reading · 4s` while a read is out, `waiting to read` while it
waits its turn, `not read yet · u reads it` before the first read, and `u reads it again` when
the time of the read was not kept), the factory's read, what codeaf read in one dim line (type,
size, estimate, priority and its reason, `bug · M · ~$3 · priority 2 · …`), the facts and risks,
the questions it would ask the author, what plan changed about the stages, and then the comments
(each whole), checks, files, activity and links. `J`, `K`, `pgdn` and `pgup` scroll it, so do the
wheel over the pane and a click on `▾ more`, and its bottom row says `J K scroll` while there is
more to see. Over the left column the wheel walks its rows.

On a wide terminal (the center at least 106 columns) the details stand in a column to the right
of the description instead of under it: the read line, the factory's read and what codeaf read,
the facts, the questions, what plan changed, the checks, the activity and the item's address on
github. The description, its comments and its files stay on the left at 72 columns. Narrower,
everything stacks under the description. Both columns scroll together.

Nothing on the item page is cut short with `…` where you need to read it: a step's ask, its
knobs, a stage's note, the settings' keys and a long log line all wrap to the width of the pane.

## the item page — issue, manager, steps, settings

`enter` on a row of the factory (or a second click on it) opens that item on its own page,
across the full width; `esc` closes it and puts the cursor back on the same row. Nothing about
the factory (filter, repository, selection) changes while it is open.

The top row is the page's bar: the run control, then a trail of breadcrumbs,
`Factory › codeaf › #1551 filters lost on compact`, and at the right the step the run is on, its
round and the spend over the budget, `review · round 1/2 · $1.42 / $5` (see the run control).
While the run waits on you the second row is its question with the keys that answer it. Then a
blank row, the left column, a dim `│`, and the center.

The left column is the **issue's map**, one row per part of the item, the steps nested two
cells under `steps`, then the item's actions under a blank row:

```
issue
manager
steps  running 4m · $0.31
  ● plan ?
  ● write
  ⠋ review ↻ 1/2 until clean
  ○ test ↻ 2 until green
  ○ arch +
  ○ approve ?
log
settings

shape steps
open on github
refresh
dismiss
```

A step row is its mark, its one-word name, then what fits of its loop: `↻ 2` the most rounds it
may take, before it runs (only where it may take more than one); `↻ 1/2` the round it is on over
its most, once it runs; `until clean` what it runs until; how long it has run. `+`, dim: the
manager, the plan or you added or changed this step; the recipe did not set it. `?`, dim: the
run stops to ask you here (an approve step, and the last step for the sign-off on an item
that has an approve step). A step held for you says `waiting for you` in amber. A step that ran as a
conversation wears the conversation's mark.

- `issue`: the whole issue and what codeaf read (see read the whole issue).
- `manager`: the item's own conversation, the team's lead (only where `T chat` works). Resting
  the cursor on it opens it in the center at once, as an ordinary idle chat with the issue
  already in it; making it asks no model anything, and nothing runs until somebody types.
- `steps`: where the run stands, and every step as written with its knobs.
- `log`: the run's log, once it has said anything (see the log).
- `result`: the proof sheet, the diff and the checks, once something came out.
- `settings`: thinking, budget and which steps are on.
- `shape steps`, `open on github`, `refresh`, `dismiss`: each does what its key does (`p`, `g`,
  `u`, `d`). `shape steps` is there while the item has steps still to come (see the manager sets
  the steps when you ask).

The page opens on the step waiting on you, else the running step, else `result` for a landed
item, else `issue`. Under 72 columns the left column is one line above the center.

## the run control on the item page — run, pause, continue, space

The bar's first cells are one button that does what the run needs next, by `space` or a click:

- `▶ run` on an item that has not run, and on a run that is paused (it goes on in the same chat);
  on an item the manager never shaped, the cursor first goes to `manager`, its chat opens in the
  center, and the run starts there, so you watch the manager shape the steps before they run;
- `= pause` on a run that is moving;
- `▶ continue` on a run held at an approve step (a step of kind gate, a person): the step's row
  says `waiting for you`, the second row says `approve · waiting for you` (or the run's own
  question) with `space continue`, and `space` or a click lets the run go past it.

A queued or landed item has no control. `?` names `space` by what it does now. On the item page
`space` never selects: select is the factory's.

The button sends what it shows, not a toggle: pressing `pause` twice (or a click and a key landing
together) holds the run once and leaves it held, and `run` on a paused item goes on once. A
doubled press never undoes itself.

## move around the item page — arrows, tab, click, hover, the wheel

`↑` and `↓` walk every row of the left column in order, the steps and the actions included,
and the center follows the row. A step's chat the cursor only passed over is let go of when the
cursor moves on or you go back to the factory, so walking the steps leaves no tabs; one you gave the
keys to (`enter`, `→` or a click on its box) stays a tab. `→` or `tab` gives the keys to the center's chat box, `esc` or
`tab` gives them back, and `esc` on the page goes back to the factory. `enter` acts on the row: the
issue and the manager open the manager's chat, a step with a chat puts the keys in its box, an
action does what it says.

A click does what the key does: on a row it moves the cursor there (a second click is `enter`),
on an action it acts at once, on the control it runs, pauses or continues, on a crumb it goes
there, and on the chat's box it gives the box the keys. Resting the pointer on any of these
highlights it, one thing at a time, on a ground apart from the cursor's; the highlight goes when
the pointer leaves. The wheel scrolls what is under the pointer: the chat in the center, or the
left column when it is longer than the page.

## the settings of an item — where to change the budget, thinking, which stages run, add or remove an approve step

Open the item and walk to its last row, `settings`. The center is a table:

```
thinking   auto          e
budget     $3            c

1 plan     on
2 approve  on
3 write    on
4 test     off

1-9 stages · s add a stage · w set in words · b save stages as the recipe
```

`e` turns how hard the model thinks, through `auto` (the crew picks for this kind of work),
`cheap` and `strong`; `c` raises the budget; `1` to `9` switch that stage on or
off (an approve step too: switched off, the run goes on past it), `s` adds a stage in words
(`after test, approve` adds an approve step after test), `w` sets them in words and `b` saves
the stages as the repository's recipe. `e` and `c` work from any row of the page. `1-9`, `s`, `w` and `b` answer on this row only: on any other
row they do nothing at all, so a letter typed toward the manager never opens
`+ stage › “after review, make it neater”`. A key is
shown only where it works: a running item's budget has no key, because it cannot change
mid-run, and its stages carry no numbers. `enter` here does nothing.

A stage the recipe file fixes is the team's law, and it shows it: the lock `§` stands where its
`on` would, the row is dim, and its number does not switch it off. Pressing it says
`security is fixed by the recipe · change .codeaf/factory.yaml to change it` on the note line,
with the stage's own name. The recipe page `E` refuses the same way: its number, `w` on it, and
`s` words that name it all say that sentence, and its pane shows the lock with the ask dim.
Switching a fixed stage that is off on is never refused.

## breadcrumbs on the item page — how to get back to the factory from an item

The item page's top row, `Factory › codeaf › #1551`, is a row of buttons. Click `Factory` to go
back to the factory, exactly as `esc` does. Click the repository, `codeaf`, to go back to the
factory showing only that repository (as `[` and `]` choose one; `esc` there clears it). Click the
number, `#1551`, to open the item on github, as `g` does; on an item with no page on github the
number is not a button. Resting the pointer on a crumb highlights it.

## start a conversation about an issue — enter on the issue row, when enter on the item page does nothing

To chat about an item before anything runs, open it and press `enter` on its `issue` or
`manager` row, or `T` anywhere on the page: the cursor goes to `manager`, and the item's own
conversation opens in the center of the page, the same chat view as anywhere else, with your
keys in its box. The page stays: the bar and the left column are still there, and `esc` gives
the keys back. An item with no conversation yet asks for one first; while it opens the line above
the keys says `⠋ opening #12's conversation…`. On the issue row the bottom line says
`enter chat`, on the manager `enter talk`.

Once you type in that box, every key is the chat's until `esc` (or `tab`, or a click outside
the chat): page letters such as `p` never fire while you type. If the chat is opened again under
you (a step's round ended), what you typed stays in the box and the keys you pressed meanwhile
land in it, in order, when it is back.

From the factory, `T` opens the item's conversation as a chat of its own instead, and the first
`esc` on its empty box comes back to the factory (see chat about an item). So does `enter` on the
page when the window is under 72 columns, too narrow to keep the page beside the chat.

Where this window cannot make the item's conversation, `enter` on the issue row opens the item
on github, as `g` does, and the bottom line says `enter open on github`. Where neither works,
`enter` says on the pane's last row `nothing to open yet · r run` (without `r run` where `r`
does nothing). `enter` on the `log`, `result` and `settings` rows opens nothing.

## a step is a room — enter on a step, its conversation in the center of the item page

A step that ran as a conversation (plan, review: see a stage is a conversation) is shown whole
in the center when its row is under the cursor: the same view as any conversation, live while
the step is going, scrolled with the wheel, its tasks beside it, and a box to type into. The trail reads
`Factory › codeaf › #12 › review` and the hint says `enter conversation`. `enter`, `→`, `tab`
or a click on the box gives the box your keys; `esc` gives them back. The conversation you were
in before goes on running behind. Under 72 columns `enter` opens it on its own, and the
first `esc` on its empty box comes back to the item page on the same step.

A step with no chat says why in the center instead, and opens nothing:
`review has not started`, `review has no conversation yet · it opens when a round ends` (a
round's conversation is kept once the round ends), `test is a check · its log is below`,
`plan is your answer · it has no conversation` for a gate, `post is a write to github · it has
no conversation`, `neaten is off on this item`, `neaten is skipped on this item`.

## the log

Once a run has said anything, the item page's left column has a `log` row under the steps. Its
pane is the run's log, oldest at the top and the newest at the bottom, as many of the last lines
as fit; each line starts with its time, `12:04`, dim at the margin, then a mark for its kind and
its words. Three voices: a **thought** (the run thinking aloud, a round starting again) is dim;
**your own words**, the `steer: …` lines `S` and the conversation leave, are ink; everything else
(what a step said, a test, a failure `✕`, a success `✓`, a question `?` in amber) is the quieter
second voice. A claim a step made that is not for the proof sheet is a `claimed: …` line, and a
batch of reads handed to a quick task is one line, `read ×4 · find handed to quick task 1`. An
item that never ran has no log row. The log keeps the last 400 lines.

## typing to the manager — enter the box, the keys type, esc gives the keys back

With the `manager` row under the cursor the center is the manager's own conversation, the chat
view itself, your words and the manager's replies live as they are written. Your keys go into its box by `enter` on the manager
row, `→` or `tab`, or a click on the box.

While the box has your keys every key types, letters that are keys elsewhere (`s`, `r`, `t`,
space) included, and the arrows move in what you typed; nothing on the page acts. `enter` sends
your words to the manager as your message and you stay on the item page. `esc` or `tab` gives
the keys back to the page.

Before the item has a conversation the center is a box of its own,
`› enter or click to talk to the manager` (`› enter or click to talk · r runs it` before a run),
and `⠋ manager is thinking` while it works; the first words make the conversation. Where this
window cannot make the item's conversation there is no box.

## answering a question — y, n, a

A run stops and asks when it cannot go on alone, and the item moves to **needs you**. The
question is the run's own sentence, drawn as is in the peek (under the title) and on the item
page's second row, its `?` amber: `plan is ready · continue, or send it back?`,
`review is not clean after 2 rounds: 3 findings · try once more, send it back, or cancel the job?` (one round
is `after 1 round`, one finding `1 finding`),
`budget of $5 reached · $5 more, or stop?`. A step that errored does not ask. The job carries
`failure` and stops.

- `y` answers yes: continue, try once more, or $5 more. The bottom line says
  `answered #12 · yes`.
- `n` answers no: at an approve step it sends the run back to the step before; for a spent
  budget it stops the item; when the rounds are used up it cancels the job. It says `answered #12 · no` (`answered #12 · send back` at an approve step).
- `a` opens `answer ›` for words (`go, but keep the old flag`); they reach the next round's
  brief (at an approve step: the run continues with them) and it says `answered #12 in words`.

`[y] yes · [n] no · [a] in words` (`[y] continue · [n] send back · [a] in words` at an approve
step) is drawn only where the factory can answer. `y`, or `a`, on an
item that is not waiting says `#12 is not waiting on you` on the bottom line and changes nothing
(`n` there is new work, and `a` on a thin new item asks its author).

## the manager shapes the run — what happens when you press r

The manager is the author of the run. Shaping is its first act, not a stage: when you press `r`
(or `L`), after the manager is made and before the first stage, the runner gives it one turn of
its own conversation with the ask `Shape the run for this item now. What the person said: …`
(what you said to it beforehand, if anything). That turn thinks cheap, unless
codeaf read the item as large (`L`). The manager answers by calling `factory_run` once: it may
give a stage a new ask, change a stage's thinking, add a stage after one it names, switch a
stage on, or skip one, within the bounds under who may change an item's stages. Then the run
goes on:

- what it set goes on the item's log and into the manager's conversation as one line,
  `manager set review: thorough on security, code and architecture · added arch after review ·
  why: touches the call row`, and the item page's stages redraw;
- when it changes nothing, the line is `the recipe stands`;
- when its turn fails or takes longer than a minute, the line is
  `the manager did not answer · the recipe stands`, and the recipe runs as it is;
- when the manager's window was in the middle of answering you, the runner waits for that answer,
  asks once more, and if it is still busy the line is
  `the manager was busy in the window · the recipe stands`;
- when the bounds refuse what it set, the line is
  `the manager's change was not applied: <why> · the recipe stands`.

Shaping never stops the run: you read what the manager set at the first approve step (see approve
steps). An item the manager already shaped
because you talked to it first (see tell the manager what you want before it runs) is not shaped
again: that is the run's shape.

Talking first in the window still shapes: if you told the manager what you want and pressed `r`
with its conversation still open, the shaping turn runs in that same window, so you see the
manager think, at the thinking you set there. If the conversation is open in another codeaf
instead (a window on the engine host), the runner cannot give it a turn, and the line is
`the manager is open in another window · the recipe stands`.

Mid-way, each line you say to the manager is the steer, as always (see talk to the manager), and
it is answered once, by the manager's own turn on it. That turn may reshape the stages not yet
started with its `factory_run`, so `do a thorough review on security, code and architecture`,
said during plan, can change review's ask before review starts; the log then says `manager set
review: …`. The runner gives the manager no second turn on the same words. A stage that started
is never changed.

## does opening an issue run anything — no; the manager sets the steps when you ask, shape steps, p

Opening an item's page runs nothing and spends nothing. The steps on the left are the recipe's:
the repository's `.codeaf/factory.yaml` as the default branch has it, else its Markdown
pipeline converted in memory, else the built-in pipeline
(plan, approve, write, test, review, proof). The manager row opens at once as an
idle chat with the issue in it; no model is asked anything until somebody types.

The manager shapes the steps in exactly three ways:

- **`▶ run`** (or `r`) on an item it never shaped: its shaping turn (see the manager shapes the
  run) streams in its chat in the center, the steps on the left redraw as it sets them, then they
  run one by one, holding at each approve step.
- **You ask it in its chat**, `make the review about security`, and its `factory_run` changes the
  steps at once. Nothing runs.
- **`shape steps`** (`p`, or the row under the item's facets; a press or a click does the same):
  the cursor goes to `manager`, its chat opens, and the manager takes one shaping turn there with
  the ask `Shape the run for this item now.` Nothing runs. Before a run it may change every step;
  during one, only the steps not yet started. The note line says what it set, `manager set …`,
  `the recipe stands`, or why its turn did not happen (`the manager did not answer · the recipe
  stands`). An item whose run is over has nothing to shape and says so. An item the manager
  already shaped takes no turn: the note line says `the manager already shaped the steps · say
  what to change in its chat`.

After a `shape steps` that changed nothing, the item says `manager kept the recipe`, and `▶ run`
does not shape it again. When the manager calls `factory_run` more than once in one turn, the
last call wins for each step: a step it skipped and then switched on again is on, and the line
says only what changed. The manager's own verbs, `factory_run`, `factory_start` and
`factory_answer`, never wait on an approval card: a shaping turn nobody is at the keyboard of
could not answer one.

A window attached to a session host asks with `POST /v1/jobs/{id}/shape`. Opening the page
still runs nothing. The factory runs the turn where the job's worker lives, or, when a
worker already holds the job, leaves `shape` on the lease for that worker (see factory HTTP
job actions).

## start the run from the manager's chat — say start

Say `start` (or `run it`, `go`) in the manager's chat and the manager starts the run with
`factory_start`, the same control as `▶ run` on the item's page. A run that has not started is
launched with the steps as they stand (the manager is the one you just talked to, so the launch
does not shape them again), a paused run goes on with the same step in the same chat, and a run
held at an approve step goes past it, which the manager does only when you clearly said so. It
says what happened in one line, `#12 started`, `#12 resumed`, `#12 went past approve`, or why
nothing changed (`#12 is already running`, `#12 is queued · it starts when a bench frees`).

The runner runs the steps; the manager reports them. It may read a step's conversation to tell
you where it stands, but it never sends a step work, stops one or starts one of its own: in an
item's team the manager has the team's reads and nothing that drives a member.

## tell the manager what you want before it runs — one word per stage, it sets the asks

Open the manager (`T`, or `enter` on the issue row) and say what you want before you press
`r`: `do a thorough review on security, code and architecture`, `skip neaten, this is one line`.
Nothing is running, so the manager sets it at once with `factory_run` and no card is shown: the
stages on the item page redraw, and the manager says what it set in three lines at most. Each
stage is one lowercase word (`review`, `arch`, `e2e`), at most nine of them, and each has an ask
that says what done looks like; the manager changes asks before it adds stages, and adds a stage
only for work no stage covers. It never drops proof. It may add an approve step after a stage
you want to read first (`hold for me after test`), or skip one when you ask it to; a fixed one
stays.

Budget, thinking for every stage and notes for the stages still go through
`factory_item`'s card (see changing an item from its conversation). `factory_run` is on the
manager's belt only; a stage's own conversation does not have it, and a conversation that manages no
item is told `this conversation is not the manager of any item in the factory`.

## approve steps — where the run holds for you, continue, send it back, plan approve write

An **approve** step is a stage that is you. codeaf's default recipe runs plan → approve →
write → test → review → proof, so the run holds after the plan until you say continue; a pull
request and a red main end on one instead. When the run reaches an approve step the item moves to
**needs you**, the step's mark is the amber `?`, and the question is the step before it:
`plan is ready · continue, or send it back?` (`ready to start · continue?` when nothing ran
before it). The manager says the same into its conversation, `asking you: …`, and can explain
what it waits on if you ask it there.

- `y` (or `space` on the item page, its top bar's `▶ continue`, or `continue` said to the
  manager) continues. `answered #12 · continue`.
- `a` with words (`keep the old flag`) continues with your words handed to the steps after as
  your note.
- `n` (or `no, use the other file` said to the manager) sends the run back to the step before
  with your words: that step runs again in a new round with them in its brief, the log says
  `sent back to plan: use the other file`, and the approve step holds again after it. With
  nothing before it, `n` stops the item.

An approve step with nothing after it is the landing itself: the run lands and the proof sheet
waits for your approval (see approve, request changes, re-run checks). An item with no approve
step at all ships itself when its proof is all green.

The recipe file places approve steps (`3. approve`, or a line `## approve` inside a kind's
section; see the recipe file), the manager may add or remove one, and so may you: `s` on the item
page with `after test, approve`, or a digit on its row in settings to switch it off. They replace
the old `ask me at`: an item or recipe that still says it is read as approve steps (plan is an
approve after plan, pull request one before the result, never none).
The shaping question `run these stages?` went with it: you read what the manager set at the
first approve step.

## approve, request changes, re-run checks — s, B, v, how do I approve a landed item

A landed item's page opens on its **proof** sheet: one row per claim and policy row, `✓` shown or
`✕` with `— not shown`, its evidence dim at the right and its medium (`test`, `screenshot`,
`policy`) as a chip in its own column at the right edge. The evidence never repeats the medium:
a test row reads `0.3s` beside `test`, not `test · 0.3s`. The sheet's last line counts it and names its keys:
`all 6 shown · s approve`, or `1 of 6 not shown · e approve with changes · B request changes`.

- `s` **approves** it and publishes it: the factory says `#12 publishing`, and a worker opens the
  pull request with your approval's trailer (see approving at the proof gate publishes). On a
  sheet with a row not shown the run refuses, in its words: `#12 has a claim not shown`.
- `e` approves saying you changed something first, the one that ships a sheet with a row not
  shown: `#12 publishing with changes`. It does not count toward a habit.
- `B` **requests changes**: it opens `request changes ›` for what to prove
  (`prove restart survival`); the item goes back on a bench with one more stage, `prove`, and
  says `#12 changes requested · prove restart survival`.
- `v` **re-runs the checks**: every check stage runs again and puts what it shows on the sheet:
  `#12's checks are running again`.
- `enter` on the sheet is the default: approve when every row was shown, and when any was not,
  request changes (`enter request changes`), with `prove` and that row's words already typed.

`d` says `the diff is the appendix` and that it opens in your editor later. Each key is named only
where the factory can do it.

## what goes on the proof sheet — claims, evidence, repeats

The proof sheet holds what was shown, not what was said. Only the **proof** stage (and a
send-back's `prove`) and the **check** and **post** stages put rows on it; what plan, write,
test or review claim goes to the log as `claimed: …` lines instead. One claim is one row: the
same words again, in any case and with any punctuation (`Refunds count once.` and
`refunds count once`), update the row rather than add one. A claim given with no evidence is
not shown: it reads `✕` with `no evidence given`, whatever it said of itself. A later claim
with evidence replaces an earlier one without, and a later one without evidence never undoes
an earlier one that had some. Policy rows (`policy`) are kept the same way.

## steer a running item — S

`S` on a running, queued or waiting item opens `steer ›`: words handed to the round running now
and to every round after it (`keep the old flag`). The bottom line says
`steered #12`, and the log shows `steer: …` in ink. `space` pauses a running item (`#12 paused`)
and resumes it (`#12 resumed`); a round cut by the pause starts over. `x` stops it and keeps the
branch: `#12 stopped · branch kept`, and the item is new again. On an item that cannot be
steered, `S` does nothing. Words typed into the item's chat while it runs steer it the same way
(see talk to the manager).

## who is the manager of an issue — the item's chat runs it

Every item's run has a manager: the item's own chat, the conversation `T` opens. When you run an
item (`r` or `L`) that has no chat yet, the run makes it first, exactly as `T` would, without
asking any model. Either way the chat is the **lead** of the item's team (`#12 · <title>`, under
the one `factory` team), and each stage's conversation joins that team as a member, so the teams
rail and the team menu show the manager above its stages.

Its opening brief tells it so, in six blocks, before the item's facts (its read, its stages with
their asks and loops, the recipe's policy and habits, budget and thinking):

    You are the manager of #12 in acme/web.
    You know: the issue and its comments, what codeaf read of it, the repository's recipe, policy and habits, the checkout, and what the person has said here.
    You do: shape the run, start it with factory_start when the person asks (nothing to look up first), report each stage here, answer the person, and hold at each approve step until the person says continue, saying what you wait on. The runner runs the steps: read a step to report on it, and never send one work, stop it or start one yourself.
    Shape the run with factory_run: stages are one lowercase word each, at most nine. Each stage has an ask that says what done looks like, a loop (until, rounds, fanout) and one line of why. Keep the recipe's stages unless the item says otherwise; change asks before adding stages; add a stage only for work no existing stage covers. Never drop proof. An approve step is where the run holds until the person says continue: keep the recipe's, add one (named approve) after a stage the person wants to read first, and skip one only when the person asks. Nothing posts outward before an approve step.
    You are the inbox: every question a stage asks comes to you first, and you reply with factory_answer. Answer it yourself when the issue, the recipe, the stages, the notes or what the person said here settle it. Send it to the person (ask_person, one line of why) when it turns on taste, scope, risk, credentials or access, money, or anything those do not settle; the stage waits, and the person answers here.
    Say what you set in three lines at most, then stop. Do not narrate.

What the manager does:

- **It hears every stage.** The run writes one line into the chat per event (`plan started`,
  `plan done · 2m · $0.04 · …`, `landed · proof sheet ready · your approval`); the lines are
  listed under what the manager hears (the stage conversations page).
- **What you type there reaches the run** as its brief or its steer (see talk to the manager).
- **It is the inbox**: every question a stage asks reaches it first, and it answers with
  `factory_answer` or sends the question to you (see a stage asks a question).
- **It shapes the run with `factory_run`** (see the manager shapes the run), with no card; the
  budget, thinking and notes go through `factory_item`, whose card still asks you
  before anything lands. It cannot run, stop, approve or post: those stay your keys in the factory.

An item whose chat cannot be made (no folder known for it) runs without a manager; its log says
`the item's conversation could not be made: …`.

## a stage asks a question — the manager answers it, or asks you and the stage waits

A stage never asks you directly. When a stage's conversation asks a question (a clarification, a
choice, a confirmation or a judgement call), it goes to the item's manager first, and the stage
waits for the answer:

- the manager's chat says `plan asks: which storage shape should this use?`, and the runner gives
  the manager one turn on it, with the question, its answers and the stage's own pick;
- the manager replies with `factory_answer`. If the issue, the recipe, the stages, the notes or
  what you said in the chat settle it, it answers itself: the chat says
  `manager answered plan: sqlite, the recipe says so`, and the stage goes on at once with it;
- if it turns on taste, scope, risk, credentials or access, money, or anything those do not
  settle, the manager sends it to you with one line of why: the chat says
  `plan asks you: which storage shape should this use? · sqlite / jsonl · a taste call`, the
  item needs you with the question on it, and the stage waits. A manager turn that fails, takes
  longer than a minute, or calls neither also sends the question to you.

Answer in the manager's chat at any time, in any words (`sqlite`, `use jsonl, it streams`), or
with the factory's answer keys; the chat says `answered: …` and the stage goes on with your words.
The question and its answer go on the run's notes, so the stages after it read them too.

A pause keeps the question on the item: answer it while paused and the answer is kept
(`noted for plan: …`), the item shows paused again rather than needs you, and when you run it the
step carries on in its own chat, told your answer. The question also survives codeaf being
closed, and an answer given then is kept the same way. Unanswered, it is dropped when the stage's round
starts again, and the stage asks again if it still needs to. Permission prompts never take
this road. One question at a time: a second one waits for the first.

## the factory's verbs, keys that change an item

On your own factory the keys that change an item's own settings always work (`n`, `c` budget, `e` thinking, `w`, the stages, `space`, `d`), and `r` and `L` run it (see running an
item). Stop, pause, answers, steering, approve, request changes and re-run checks work in every
window too, whichever process runs the factory's items. `enter` on a row never runs anything; it
opens the item page. The peek names at most five of these for the row's state (see the peek's
verbs); `?` lists them all.

- **New:** `r` runs it; it holds at each approve step (see approve steps). `c` sets the
  **budget**, the most it may spend: $2, $5, $8, $15, $30. `e` sets the **thinking**, how hard
  the first stage's model thinks: none, cheap, strong. `w` opens `in words ›` for both at once
  (`$8, stronger`). The digit keys `1` to `9` switch a stage on or off and
  `s` adds a stage (named on the `?` sheet). `space` selects, `L` runs the selected (see select
  new items). On a terminal-made item `g` puts it on github too, or takes it off; on any other
  item with a page on github, `g` opens that page. `a` asks a thin item's author its questions.
  `d` **dismisses** the item: it leaves the factory until it changes.
- **Running or queued:** `x stop · space pause · S steer` (see steer a running item); `e` steps
  the running stage's thinking.
- **Needs you:** `y yes · n no · a in words`, and `S steer`, `x stop` (see answering a question).
- **Landed:** `s approve` on a clean sheet, `e approve with changes` when a row was not shown,
  `B request changes`, `v re-run checks`, `d diff` (see approve, request changes, re-run checks).
- **Any state:** `T chat` opens a chat about the item (see chat about an item), in the factory and
  on the item page alike; it is named only where the factory can make one. `u` refreshes it.
- **Anywhere:** `n new` opens `new work ›`, at the bottom of the rows column, on the repository the list shows (or the first one;
  on a factory with no items yet, the name of the folder this window was opened in), and the new
  item's card is under the cursor when it is made.

A key that takes words opens a one-line box at the bottom of the right column, just above the
peek's verbs (which stay the column's last row, one blank row between them), except `n`'s,
which stands at the bottom of the rows (and every box stands under the rows when there is no
peek): `enter` sends, `backspace` edits, `esc` cancels. After a key the bottom line says what
became of the item (`#12 is running`, `#12 stopped · branch kept`); a refusal, such as
`#1540 is on a bench; stop it first`, is said there in the factory's own words instead.

## what does ask me at mean, how do I make it stop after the plan

`ask me at` is gone (2026-10-09): where an item's run stops to ask you is an **approve** step
among its stages (see approve steps). codeaf's default already stops after the plan: plan →
approve → write. To stop somewhere else, add one there: `s` on the item page with
`after test, approve`, or ask the manager to (`hold for me after test`). To let a run go
without stopping, switch its approve steps off on the settings row; with none, a green proof
ships by itself. An item or recipe that still says `ask me at` (or `gate plan`, `gate ship`,
`gate none`) is read as approve steps.

## what is chat on an item, what was talk in the factory, what is select, what is dismiss — the factory's words

The factory names each key by what it does:

- `r` **run**: run every stage; it holds at each approve step.
- `T` **chat**: a conversation about this item with the issue loaded (see chat about an item).
- `space` **select**: tick the row to run several together; `L` **run selected**.
- `e` **thinking**: how hard the model thinks, the chat's Thinking dial.
- `c` **budget**: the most this item may spend.
- `1`-`9` **stages**: turn a stage on or off.
- `g` **open on github**, `u` **refresh** (read the issue again: size, cost, priority), `d`
  **dismiss** (take the row from the factory; it is then `dismissed`).
- On a landed item: `s` **approve**, `B` **request changes**, `v` **re-run checks**.
- `m` **foreman**, `U` **refresh all**, `h` **handover**.

These replaced older words on 2026-10-08: `T` was talk, `space` was mark, `t` was gate, `c` was
cap, `e` was effort, `u` was read again, `d` was hide, and the landed keys were sign off, send
back and check again; `p` (plan first) is gone, because the approve step after plan and `r` do
it. On 2026-10-09 `t` (ask me at) went too: an approve step is where the run holds.

## the ? key sheet — what the question mark key shows, every key in the factory and the item page

`?` in the factory or on the item page opens the key sheet over the page: every key that works
where you stand, beside its word, in four groups, `do` (the row's verbs), `set` (budget,
thinking, stages, in words, and on the item page `↑↓ settings`, the row they stand on), `also` (open on github, refresh, dismiss, new, foreman,
refresh all, handover, repos, recipe, rail, backlog, density, order, repo, the split, scroll)
and `move` (walk, or `↑↓ rows` on the item page, open, filter, tab next place, esc back, ? keys). Only keys the factory can do are
listed. The hint says `esc close`; `esc` or `?` puts it away and leaves you where you were. Other
letters do nothing while it is up.

## open the item on github — g

An item that came from GitHub has a page there, and the factory offers it three ways: its short
name on the row and on the peek is a link (where the terminal takes links, it opens on click,
and takes no extra room), the peek's dim meta row ends `github ↗`, and `g` on the item opens the
page in your browser. The `?` sheet names it `g open on github` where it works. After `g` the bottom line
says `opened #1662 on github`; on a machine with no browser it says
`could not open your browser` with the address to copy.

`g` is offered only when the factory can name the page; with no such door there is no key. On an
item typed into the terminal with `n`, which has no page on GitHub, `g` keeps its other meaning:
it puts the item on GitHub too, or takes it off.

## when the factory is doing something — the spinner

Everything the factory does in the background says it is happening, with the same braille spinner
the transcript uses (`⠋`), and nothing is drawn when nothing is in flight. The spinner turns for
as long as a read, a door or a poll is in flight, and never stands still while one is, so a
spinner that moves is work still going:

- **An item being read:** its priority cell spins and the fact after its state says `reading…`
  or `refreshing…`. An item waiting its turn in a re-read of every job does not spin: it shows a
  still dim `·` and `waiting to read`. The peek's read says `⠋ reading…` while the first read is out, and the item
  page's read line says `⠋ reading · 4s`.
- **`u` refreshes the item under the cursor.** The line above the keys says `re-reading #6…`
  until the read is over, then the bottom line says `#6 refreshed · ~$0.0004`. The item page's
  read line says when it was made and the key, `read 3m ago · u refresh`.
- **`U` refreshes all, and asks first:** `re-read 8 items · ~$0.004? [y] go · [n] not
  now` stands at the bottom of the rows. `y` goes, `n` or `esc` does not. While it runs the
  handover says `⠋ refreshing 8 items · 3 done`.
- **A source being read:** the handover says `⠋ reading Agent-Field/CodeAF · 1 of 3` (or
  `github · ⠋ polling` when the source does not say where it is), then `polled 14s ago`.
- **A stage running on the open item page:** its mark in the left column spins, and
  its time counts up each second (see the item page).
- **A key waiting on its answer:** `T` says `⠋ opening #1's conversation…`, `b` says
  `⠋ banking…`, saving the repositories or the recipe says `⠋ saving…`, and `R` says
  `⠋ asking gh…`, each until the answer arrives.

`u` and `U` are offered only where the factory can read items again.

## bank a habit after clean approvals

After three approvals in a row without edits, the bottom of the right column offers a habit:
`habit forming — 3 approvals without edits on codeaf` and
`factory PRs from your own issues self-ship when the proof is green? [y] bank it · [n] not yet`.
`y` writes that sentence into the repository's `rules` in its
`.codeaf/factory.yaml`, the way a taught line is written: on a branch `factory/recipe-<word>` in a
temporary worktree, committed as `recipe: <the sentence>`, pushed when a remote exists and opened
as a pull request when `gh` exists, so a teammate reviews it like code. The note line says exactly
one of `written · pull request #<N> opened for the team`,
`written · branch factory/recipe-<word> pushed · open the pull request when you want`,
`written · committed on factory/recipe-<word> · no remote to push to` or
`written · .codeaf/factory.yaml (not a git repository)`. `n` puts the offer away. The offer comes only where banking can be written.

## no recipe in the repo yet — write it so the team shares it

When an item of a repository lands and that repository's checkout has no `.codeaf/factory.yaml`,
the bottom of the right column offers, once, where the habit offer stands:
`no recipe in codeaf yet` and
`write the recipe into the repo so the team shares it? [y] write it · [n] not now`.
`y` writes the built-in YAML pipeline (or the existing Markdown recipe converted in memory) into the checkout's `.codeaf/factory.yaml`, and
the note line says `recipe written · .codeaf/factory.yaml on main` with the branch the checkout
stands on. `n` puts the offer away for that repository for good: the factory's store keeps the
no, so no window asks again. The offer comes only where the recipe can be written, a file that is
there already is never written over, and a habit offer standing at the same time is answered first.

## from chat to the factory — factory_add, hand this to the factory

A conversation can offer a piece of work to the factory with the `factory_add` tool. The
chat calls it when you say something belongs in the factory, or when it judges a piece of
work is self-contained enough to run on its own later. It is there only when this conversation
has a factory behind it; without one the chat has no such tool and says it cannot. It is
there on every ordinary launch on this machine: plain `codeaf`, whose conversation runs in the
session host, as well as first-run setup, `--no-host` and `--debug`. A window that comes back to
a conversation while its card is still up is shown the card. It is absent over `--host` or `--at`
to another machine (that machine's factory is not the one your page reads), from `--once`, and
inside a task; add work there with `n` in your own factory.

Calling it adds nothing. A card asks you first, in one question: `hand this to the factory?`.
Under it is the item drawn the way the factory will show it (the title with `repo · kind · size`
on the right, then `new` and the estimate with the stages it would run), then the chat's reason.
It answers to `1 add it`, `2 not now`, or words typed into its box
(`say what to change… (enter sends it)`).

- **`add it`** writes one item to the factory as `new`, from chat, and the chat is told
  `#<id> <title> is in the factory`. The card's foot says `added · #<id>` and its body
  becomes the item's live card (see the item card in a conversation).
- **`not now`** writes nothing: `nothing was added: the person said no.`
- **Words** write nothing either. The chat is told `the person changed it: <your words>` and
  `Nothing is in the factory yet. Propose it again with that`, and asks again with a new card.
- **No answer** writes nothing. There is no clock that adds: after fifteen minutes the card
  comes down with `nothing was added to the factory`, and the chat is told
  `nothing was added: the card was never answered`.

**The chat does not launch it.** An added job waits in backlog until you run it. Automatic
intake can start eligible forge work separately (see why did this start by itself).

## teaching the recipe in conversation — factory_recipe, always run a security review in a repo

Say a rule out loud ("in my repo always run a security review when auth is touched",
"never post without green tests", "for PRs, two review rounds") and codeaf can offer it to
the repository's recipe, `.codeaf/factory.yaml`, with the `factory_recipe` tool. It is there
exactly where `factory_add` is, on the ordinary launch on your own machine, and absent over
`--host`, `--at`, from `--once` and inside a task. It writes YAML: steps are prompts
with optional `until` and `max`, and policy or habit sentences go under `rules`.
Older stage conditions are carried in the prompt; retired knobs such as thinking and
fanout have no YAML representation.

It offers a single line: a stage for `issue`, `pr` or `ci` in the file's grammar
(`security · read it for auth holes · when touches auth`), a policy sentence, or a habit.
A stage line the file cannot read is refused before any card, with the reason. A stage of the
same name already in that section is replaced where it stands; otherwise it is added at the end.

**Nothing is written before `1`.** A card asks first, in one question:
`add this to <repo>'s recipe for <kind>?` (or `add this to <repo>'s policy?`,
`add this to <repo>'s habits?`). For a stage its body says `now:` with the stages that kind runs
today and `after:` with the new one marked `+`, then the line itself; for a policy or a habit,
the sentence; and `why:` with the reason when the chat gave one. It answers to
`1 bank it`, `2 not now`, or words (`say what to change… (enter sends it)`).

- **`bank it`** writes the line and, in a checkout that is a git repository, **opens a change for
  the team** instead of writing silently, because the YAML pipeline is shared and a
  teammate should review a taught line like code. The line is written on a branch
  `factory/recipe-<word>` (the first word of the line; a taken name gets `-2`, `-3`) cut from the
  default branch, committed as `recipe: <the sentence as typed>` with no trailers, pushed when a
  remote exists, and opened as a pull request with `gh pr create` when `gh` exists (the title is
  the sentence, the body the one-line why and the line it adds). It is made in a temporary
  worktree that is removed afterwards, so your working tree, even a dirty one, is not touched and
  the checkout stays on the branch it was on. What happened is said once, on the card's line and
  in the answer, as exactly one of:
  - `written · pull request #<N> opened for the team`: the branch was pushed and `gh` opened the pull request.
  - `written · branch factory/recipe-<word> pushed · open the pull request when you want`: pushed, and `gh` is not there.
  - `written · committed on factory/recipe-<word> · no remote to push to`: committed on the branch, kept in the checkout's repository.
  - `written · .codeaf/factory.yaml (not a git repository)`: the file was written in place.
  A line the file already has changes nothing: `already in .codeaf/factory.yaml · nothing to change`.
  codeaf is told `<line> is in <repo>'s recipe for <kind>`
  (or `… is in <repo>'s policy`, `… is in <repo>'s habits`) and the card says `banked`.
- **`not now`**: `nothing was banked: the person said no.`
- **Words**: `the person changed it: <your words>` and
  `Nothing is banked. Propose it again with that` (card: `changed in words`).
- **No answer** for fifteen minutes: `nothing was banked: the card was never answered`
  (card: `expired · nothing banked`).

It writes only where codeaf knows the checkout; otherwise the answer is
`codeaf does not know where <repo> is checked out; open codeaf there once`.

## chat about an item — T, the item's own conversation

`T` (**chat**) on an item, in the factory or on its page, opens a conversation that belongs to
that item, so you can talk it over or plan it before you run it, or while it runs. **It is the
item's manager** (see who is the manager of an issue). Nothing makes it before the first `T` or
the item's first run; after that every `T` opens the same one, and the item keeps it.

The first press makes, on this machine and without asking any model:

- a team named by the item, `#12 · fix the ledger double count`, nested under one team named
  `factory` (made once, the first time any item gets a chat);
- one conversation in that team, in the folder where the item's repository is checked out (or
  this window's folder when codeaf does not know the checkout). It opens with the item in front
  of it: the title, the repository, author and tier, where it asks you, its budget and labels,
  the body, its stages as numbered lines, the factory's read, the sentence that makes it the
  manager (`You are the manager of this item. …`), and
  `This is the item's own conversation in the factory. Nothing here runs it; the person does that in the factory.`

You land in the conversation, as when you open one from its tab; the one you were in goes on
running behind it. The first `esc` on its empty box, with nothing running, takes you back to the
factory on the same row, however many turns the model has answered (and onto the item page if that is
where you pressed `T`). That way back is taken once; reopened later from its tab it is an ordinary conversation, and there `esc` means
what it always means, so a first `esc` arms the rewind (`esc again to rewind`).

**Where it appears:** a tab on the tab strip, like any conversation you open. Its team sits under
the one `factory` team in the team menu and on the teams rail, and that team is folded: a hundred
items with a chat are one `factory` row until you stand in it. When the item is dismissed with
`d`, its conversation is put away (home's archive line) and its team is closed.

`T` is not named, and does nothing, over `--host` or `--at` to another machine, from `--once`,
or on the still made-up factory: nothing there can make a conversation.

## the foreman — m, what should I take first, factory_floor, why can't my chat read the factory

`m` in the factory opens the foreman: one conversation for the whole factory, for deciding
what to take first ("what should I take first this morning and why?", "select the three
cheapest bugs"). **It is never made by default.** The first `m` makes it and every later `m` opens the
same one. It is named `foreman`, sits in the one `factory` team beside the items' own teams, and
opens with `You are the foreman of this factory.` and each repository's policy lines.
You land in it as with `T`, and the first `esc` on its empty box goes back to the factory.

It reads the factory with the `factory_floor` tool: `waiting` (items that need you), `risky`,
`cheapest` (ten, by estimate), `oldest` (ten), `all` (up to fifty), or one `item` in full (its
read, facts, stages, the question it waits on and the first 2000 characters of its body). A row
is the ref, title, kind, repository, size, estimate, priority and its reason, state, age and
risk; what is not known is left out.

**It proposes by selecting.** It selects new items, and the tool tells it
`selected #1 #4 #6 · press L in the factory to run them`. In the factory a row it selected wears the
accent lead, as a `space` select does, and the peek offers `L run selected`. What it selects is
kept with the factory on this machine, so it survives a restart, until you run or unselect it; a
selection on an item that is no longer new is dropped.

**It never runs, ships or posts.** Only a new item can be selected (the tool says
`#3 is running, and only a new item can be selected`), and `L` in the factory is the only way to run
them (see what the factory does not do yet).

**Only the foreman reads the factory.** `factory_floor` is on the foreman's belt and on no other
conversation's: an ordinary chat, an item's own conversation (`T`) and a stage do not have it.
A chat that wants something in the factory proposes a new item with `factory_add`, an item's own
conversation changes its item with `factory_item`, and to ask about the whole factory you open the
foreman with `m`. `m` does nothing over `--host` or `--at`, from `--once`, or on the still made-up
factory.

## changing an item from its conversation — factory_item, skip a stage, add an approve step, raise the budget, leave a note

An item's own conversation (`T`) is the item's hub. In it codeaf can propose a change to that
item with the `factory_item` tool: stages to add (`after review, read it for auth holes`, or
`after test, approve` for an approve step where the run holds for you), skip or switch on; its
budget in dollars (the tool's `cap`); its thinking (the tool's `effort`: `cheap`, `strong`,
`default`); or a note its stages will read. The card speaks the factory's words: `budget` and
`thinking`; only the tool's own field names (`cap`, `effort`) are the engine's. It is for the item the conversation is
about, and it is on the belt wherever `factory_add` is. (It replaced `factory_stages`, which
could change only the stages.)

**Nothing changes before `1`.** A card asks one question built from what changes:
`#1 · skip the review stage?`, `#1 · add an approve step?`, `#1 · raise the budget to $8?`, `#1 · add a note for the stages?`,
or `#1 · change the plan?` for several. Its body is only what changes: `now:` and `after:` for the
stages, `budget  $5 → $8`, `thinking  — → strong`, `note: …` and `why: …`.
It answers to `1 yes`, `2 keep it`, or words (`say what to change… (enter sends it)`). There is
no clock on it.

- **`yes`** applies it and codeaf is told the item now, only what is set:
  `#1 now: plan · approve · write · test · proof · budget $8`, and that the change is made. The
  card says `changed`.
- **`keep it`**: `nothing changed: the person said no.` (card: `kept as it was`).
- **Words**: `the person changed it: <your words>` and `Nothing changed. Propose it again with that`.
- **No answer** for fifteen minutes: `nothing changed: the card was never answered`.
- A change that leaves the item as it is asks nothing: `nothing changed: the item already runs that way`.

The recipe's bounds hold before any card: proof and a stage the policy names are never skipped,
a fixed stage never changes, a stage that has run is never touched, and under `fixed` a stage change is refused
with `nothing changed: the recipe for issue is fixed; plan may not change the stages` (and nothing
else in that card changes). Notes are kept on the item, and every stage's brief opens with them.
Nothing launches from it.

## the item card in a conversation — live item status, #12 in a reply

Wherever a conversation is about an item in the factory, the item is drawn as one live card:
`▤ #1 <title>` with `repo · kind · size` on the right, and under it where it stands —
`new · budget $5`, `running 24m · $1.42 / $5`, `? plan is ready`, `landed · 3✓ 1✕`,
`shipped · $1.90` — with its stages on the right (`○ plan  ○ approve  ○ write  ○ test  ○ proof`, the running
one lit).

It appears in three places: after `factory_add` adds the item (as that card's body); at the top
of an item's own conversation (`T`), in place of the opening note the model reads; and under a
reply that names an item the factory knows, `#12` in the current repository, once per item per
turn.

It stays live: the factory is read again when a card settles and every three seconds while a
conversation with a card is on screen. `enter` on the selected card, or a click on it, opens the
item page, as `enter` in the factory does. Over `--host` it draws from the same news the engine
sends, and opens nothing, because there is no factory page there.

## connecting github — watch a repository, issues and pull requests in the factory

The factory reads open issues and open pull requests from the GitHub repositories you watch.

- **Which repositories:** the ones you tick with `R` in the factory (see choose which
  repositories the factory watches), kept in `repos.json` in the factory folder
  (`~/.codeaf/v3/factory/repos.json`), each written `owner/name`, as
  `{"repos": ["acme/api"]}` or a plain `["acme/api"]`. The file can still be edited by hand. A
  running poll reads the list on every tick, and a window with no poll yet looks again every 30
  seconds, so a change reaches the factory without a relaunch.
- **Why your issues are not showing up yet after you watched a repository:** the first read
  starts the moment you save the picker (`enter` on `R`), within about a second, wherever the
  poll on this machine runs; while it reads, the factory says which repository it is on, how
  many of how many are done and how many items it has listed so far. GitHub is polled every
  minute after that.
- **Rows arrive repository by repository as the read goes.** A repository's issues stand on
  the factory as soon as its list is in, before any comment is read, and its pull requests once
  theirs are; you do not wait for every repository to finish. A repository that fails to read
  keeps no one else's rows from the factory and is read again from where it was on the next tick.
- **On a busy repository the comments fill in over the next minutes.** One read spends at most
  40 requests on comments and on pull requests' lines and files; past that, items arrive
  without them and the next reads, a minute apart, fetch the next ones until all are in. An
  item whose comments are not read yet says `comments not read yet` on its page; one with
  none says nothing.
- **The token, in this order:** `GH_TOKEN`, then `GITHUB_TOKEN`, then a token kept in
  the profile, then what `gh auth token` answers, and `gh` is asked only after you have said yes
  to it in the factory. The token is never shown or logged.
- **No watched repository or no token: nothing is connected.** Nothing polls, and the factory's
  facts line says only `terminal · chat`.
- **Who polls:** the codeaf window you are sitting in, whether or not a background engine
  holds your chats, and the engine too when it has the factory open. They take turns through the
  factory's lock, so only one of them reads GitHub at a time, and only one of them reads new items
  for triage, so a read is never paid for twice.
- **How often:** every 60 seconds while a codeaf window is open on this machine; after a
  failure the wait doubles, up to 10 minutes. Only one window polls at a time. An unchanged
  list costs no rate limit.
- **What is read:** number, title, the first 2000 characters of the text, author, labels,
  times, its page on GitHub and its last three comments; for a pull request also its checks
  (`ci ✓`, `ci running`, `ci ✕`), `+N −M` lines, its 20 most changed files and each check run
  by name (see what github gives an item). Size is S, M or L (up to 50, 400 changed lines, or
  more). The author is `owner` for your own login with write access, `collaborator` for others
  with write access, otherwise `stranger`.
- **A change on GitHub updates the row's words** (title, text, labels, checks, lines, comments)
  and never where it asks you, its budget or its stages. A changed title or text clears the item's read so triage
  reads it again; a label change alone keeps the read. A dismissed item that changes comes
  back as new.
- **The facts line** says `github` and `polled 4m ago`; a failed read says
  `github · not reachable` (or `token refused`, `repository not found`).
- **Nothing is ever written to GitHub by itself.** The poll only reads: no comment, label,
  close or pull request is made by it.
- **Pull requests the factory opened are read again** on that same poll, including after
  they merge or close. See following a pull request.

## after connecting github — I watched repos but the factory is empty, I selected repositories but see no issues and nothing loading

Nothing: an empty factory right after you watched repos is the read starting. Once you watch
repositories the factory reads them for you, starting the moment you save
with `R`, and it says the read is loading while it runs. The bottom line says
`watching 3 repositories · reading them now` for as long as the handover says it is reading,
and from then until rows stand the factory says which of four things is true, one sentence each:

- **The first read is under way.** From the moment you save, before the read has said
  anything, the handover's line is `⠋ reading the repositories you watch`, the factory reads
  itself every second, and it keeps saying so until the read reports where it is or rows stand.
  Once the read reports, the handover's line says where it is,
  `⠋ reading Agent-Field/CodeAF · 1 of 3`, and counts the items listed so far once there are
  some, `⠋ reading Agent-Field/CodeAF · 2 of 3 · 200 items so far`; under it the factory says
  `reading the repositories you watch · rows stand here as issues and pull requests arrive`.
  It does not say `quiet` while a read is out: quiet means nothing is happening and nothing
  happened. Each open issue and pull request becomes a row as its repository is read, one
  repository's rows at a time, and comments keep filling in for a few minutes after.
- **The read has not answered.** After half a minute with no word, the spinner stops and the
  handover's line says `no word from the read yet` (then the source's trouble, if any), and under
  it `the read was asked for and has not answered · R to check the repositories · if codeaf was just updated, restart it`.
  The bottom line keeps `watching 3 repositories` without `reading them now`. Never `quiet`:
  check the list with `R`, and restart codeaf if it was just updated.
- **The read is done and nothing is open.** The factory says
  `nothing open in 3 repositories · github polls every minute · n adds work by hand`: the
  repositories you watch have no open issue or pull request. A new one arrives within a minute of
  being opened on GitHub, and `n` makes an item by hand meanwhile.
- **Nothing is connected.** The page says
  `nothing connected yet · the factory arrives here when a chat splits work off or a repo is connected`;
  see connecting github.

A factory with a source but no watched repository says
`work arrives here from chat, from n, and from the repositories you connect`. With the four-row
handover (`h`) the facts row carries the same progress in the source's own clause,
`github · ⠋ reading Agent-Field/CodeAF · 1 of 3`, and the shift row says
`nothing happened while you were away` without `quiet` while the read is out, unanswered or
failing; a failing read's line says `github · not reachable`.

## does the factory show only new issues or every open one

Every open one. The first read takes every open issue and pull request of each repository you
watch, however old, and later reads take what changed. `NEW` in the factory means codeaf has not
done anything with the item yet, not that it is new on GitHub.

## my chats run but the factory never fills — the engine is another build

The background engine that runs your chats outlives the codeaf that started it, so after you
install or build a new codeaf, the engine can still be an older build, without the factory's
tools or its manual page. Your window attaches only to an engine of its own build.
Any other engine, including one built from a changed working tree, a `dev` build, or one that
cannot say which build it is, does not get your conversations: the window opens them in itself,
the way `codeaf chat --no-host` does, reads the factory itself, and says so once on the notice
line, for example
`this workspace's engine is another build (0ld0ld00 built 2026-10-06 09:00) · this window runs its own · the old engine keeps the chats it already has; stop it when they are done: codeaf engine --stop --workspace '/srv/app'`.
The old engine keeps every chat it was already running, and nothing is stopped for you. When
those chats are done, run that command; the next codeaf you open starts the engine from your
build.

## what github gives an item — comments, changed files, check runs, its page

Each GitHub item in the factory carries, beside its row's words:

- **Its page:** the issue's or pull request's address on GitHub, which opens it in your
  browser. An item typed in the terminal or split off a chat has none, and opening it says
  `this item is not on github`.
- **The last three comments**, oldest first, each kept to its first 1000 characters, with who
  said them and when. They are read only when the item is new or its comment count changed, so
  an item nobody commented on costs no extra request. On a busy repository they arrive over
  the minutes after the row does, and until then the page says `comments not read yet`.
- **For a pull request, the changed files:** the 20 with the most changed lines, each with its
  added and removed lines, read once each time the pull request changes.
- **For a pull request, the check runs** on its head, by name, each with its state (`success`,
  `failure`, `in_progress`, `queued`) and its page. The row's short `ci ✓` stays the summary.
- **Its activity**, the last 20 things that happened to it: `arrived from github`,
  `changed on github` (its title, text or labels changed), `read` (triage read it), `talked` (its
  own conversation was made with `T`), and `stages changed by plan`.
- **While github is being read** the factory says so; it is marked for the length of each read and
  cleared when the read ends, or when codeaf next starts after a crash.

## refresh an item — u, refresh all — U

`u` (**refresh**) reads the item in front of you again from GitHub now, without waiting for the next poll: the
issue or pull request by its number, its last three comments (read even when the count did not
move), and a pull request's files and check runs. Then its read is cleared, so triage reads it
again. A terminal item has nothing upstream; `u` only clears its
read. While it runs the row says `refreshing`; while triage reads an item it says `reading`.

`U` (**refresh all**) does the same for every item in the factory except the dismissed, in turn, and says
`refreshing 8 items · 3 done` while it runs. **Before it starts, the factory shows what it would
cost:** the number of items times the average cost of a triage read here, or $0.0005
an item when no read has been priced yet. Nothing is read or spent until you say yes. Each read it
causes follows the factory's one-model pin when set, otherwise the cheapest seat, and writes
a row on the spend ledger named `factory triage`.

With no GitHub connected, `u` on a GitHub item says `github is not connected`. `u` and `U` are on
your own factory only, never on the still fixture.

## choose which repositories the factory watches — R, find a repo in the list, why a repo says here

`R` in the factory opens a list over the factory headed
`watch · repositories github sees as <login>`, with `3 watching · 143 listed` at the right. Under
it is the filter line, `› type to filter`, and then the repositories in sections:

- **`WATCHING` first:** every repository you already watch, in one place at the top, so the
  ticks are never scattered through a long list. A watched repository GitHub no longer lists
  stays here so you can untick it. A repository you tick now stays where it is until the list
  opens again, so the cursor never jumps.
- **Then one section per owner** (your account and each organisation), owners ordered by their
  most recently pushed repository, and inside each the most recently pushed first.
- **Each row** is `[x] owner/name` (watched) or `[ ] owner/name`, then, each in its own column:
  `12 open` (open issues and pull requests together, as GitHub counts them), `private`, `here`
  and `pushed 3m`. A column no row has anything in is not drawn.

To find a repository in the list, just type: any key the list does not use goes into the
filter, no `/` first (a `/` typed first works too). Words match anywhere in `owner/name`, case
aside; `agent-field/` keeps only that owner's repositories, and `agent-field/api` that owner's
repositories with `api` in the name. Nothing is scored: the list keeps its sections and order.

A repository says `here` when it is checked out on this machine and codeaf knows where, and
`clone on first run` when it is not: the factory's stages run only in a checkout, so the first
`r` on one of its items asks to clone it into `~/.codeaf/v3/factory/repos` (see it says CodeAF
is not checked out). The list's last line says so for the repository under the cursor:
`not checked out here · it is cloned on the first run`, or `checked out at <folder>` for one
that is. Where codeaf cannot clone, the column is blank and the line says
`codeaf does not know where owner/name is checked out · it is watched and read, and its stages
wait until it is cloned`.

- `↑` `↓` walk, `space` ticks or unticks, `enter` saves, `ctrl+o` orders every section by how
  many are open (the most first) and again back to the last push, `backspace` takes a letter off
  the filter, `esc` clears the filter and then closes without saving:
  `space watch · enter save · ctrl+o by open · esc close · type to filter`. `o` is always a
  letter, so a name that starts with one is typed like any other; `ctrl+o` is offered only when
  some repository has an open count.
- Saving writes `repos.json`, starts the first read and says
  `watching 3 repositories · reading them now`
  (or `watching no repositories · the factory keeps what chat and n bring`); see after connecting
  github for what the factory says while the read runs.
- **With no GitHub token** `R` asks first. When `gh auth status` answers within 3 seconds it asks
  `connect github: gh is logged in as <login>, use it? [y]` (`y use gh · n a token instead ·
  esc not now`); otherwise it opens a row `github token ›` whose words are drawn as dots. A yes
  or a token GitHub accepts says `github connected` and opens the list. The token is kept in your
  profile beside the model key, never shown and never logged; `github did not take that token`
  and `gh answered no token · gh auth login, then try again` are the two refusals.
- `R` is not offered on a factory whose store cannot keep repositories (over `--host`, or the still
  made-up factory).

## the recipe page — E

`E` in the factory opens the recipe page for the repository the factory's `[ ]` line shows (or the
factory's only repository). With every repository showing it says
`pick a repository with [ ] first · a recipe is one repository's`.

It is the item page with no item: the kinds `issue · pr · ci` across the top with the current one
in ink and `recipe · <repo>` at the right, the stage rail at the left and the selected stage at
the right (its knobs, its sentence, `runs first` or `runs after <stage>`, or
`switched off in this recipe`). A pipeline problem is one dim line
above the rail, `line 7: until is one of done, clean, green, proven`.

- `[` `]` (or `h` `l`) switch the kind, `↑` `↓` walk the stages, `esc factory` goes back.
- Where codeaf knows the checkout: `1-9 stages` switch a stage on or off, `s add a stage` adds
  one in words (`+ stage ›`), `e thinking` steps its thinking, `w` sets its knobs in the recipe
  file's own words
  (`until clean, max 3`; a word it cannot read says why), and `b` saves the whole
  recipe, every kind, to `.codeaf/factory.yaml`:
  `saved to .codeaf/factory.yaml · new work on codeaf runs it`. Start and size settings
  are preserved. Conditions and proof requests become prompt text; thinking and
  fanout have no YAML representation and are omitted. Merge changes into the default branch
  before new forge jobs read them.
- Where it does not, the page is drawn but not changed, and the note line says
  `codeaf does not know where <owner/name> is checked out`.

## the day rail — $

`$` in the factory opens the row `day rail ›` (with the rail already set in it, `$60`). Type a dollar
figure and `enter`: `the day rail is $60`. `0` or `off` takes it off: `the day rail is off`. A word
that is not a figure says `a rail is a dollar figure, like $60`.

The rail is kept with the factory's store (`sources.json` in the factory folder) and the
handover's money reads it: `$11.31 / $60 today`, or `/ $60 today` before anything is spent. With
no rail set there is no `/ $N` at all. Once today's spend has reached the rail, a launch is refused
with `the day rail is $60 and today's spend has reached it`; an item already running is not
stopped by it. `$` is not offered on a factory whose store cannot keep it.

## connections on the settings page — github

The settings page's **Connections** tab has a `github` row when this window can ask how it
reaches GitHub: `github  santoshkumarradha · via gh` (gh's own login, after you said yes to it),
`<login> · token` (a token kept in your profile), `<login> · from the environment` (`GH_TOKEN` or
`GITHUB_TOKEN`), `not connected`, or `not reachable`. It is read once when the page opens.
`enter` on it opens the factory with the same connect prompt `R` asks (`enter connects`), so a
token can be changed there.

## triage on arrival — the read, size, estimate and risk, from one cheap call

Every new item in the factory is read once, by one call on the cheapest seat (the `small work`
row in /settings; a row you cleared follows the conversation's model). With `--one-model`,
the factory follows the launch's model pin, including triage, size and estimate; the `small work`
row stands down. The call sees the item's
title, its text cut to 3000 characters, its labels, its kind, its repository and its last three
comments (each cut to 600 characters, so the read sees where the discussion stands), and answers:

- a one-sentence read (shown under the row in the comfortable density, `z`, and on the peek),
- a type (`bug`, `feat`, `chore`, `question`) and a size (`S`, `M`, `L`),
- an estimate in dollars (the `~$3` on the row),
- risks in short phrases, such as `touches money`, `touches auth`, `has ui`, `migration`,
- a possible duplicate (`#950`), only when the text names one,
- a priority from 1 (take it first) to 5 (later) and a reason of at most five words, used by
  the `first` order.

It only fills what is empty: a type GitHub's labels gave, a size the poll measured, and anything
you set win over the model. One item is read every two seconds, newest first; with nothing new it
looks again every thirty seconds. Each item is read ONCE: an answer that cannot be understood is
not asked for again, and the item simply has no read. A call that fails is tried three times.
Reading does not change a row's age. An item whose title or text changes on GitHub, or that you
refresh with `u` or `U`, is read again; the row says `reading` while its call runs, and the factory
quotes the last read's cost.

The read itself never dismisses, labels or comments. The v2 intake decision uses
explicit labels, budget and paths independently of this older terminal read;
its AI estimates and rule judgments cannot grant permission to start.
Without start rules it remains facts drawn dim. It runs in the window that opened the
factory or in `codeaf factory`, one process per machine, and only while a key resolves; with no key there is no reading
at all. Each call is a row on the spend ledger named `factory triage`, on the low seat.

## order the factory — O

The rows inside each group are ordered by `priority` by default: priority 1 first, unranked items
last, then the newest issue number first (the factory's own number for chat and terminal items).
`O` cycles `priority`, `first` (priority, then older first), `age` (oldest arrival first) and
`cost` (cheapest first). The groups never move: `NEEDS YOU` stays on top in every order.

**The rows do not move under you.** An order is applied when the factory is first read and when
`O` is pressed; after that a re-read never reorders the rows, even when it changes an item's
priority. Two things move a row: a new arrival, which slides in at the top of its group, and a
change of state, which moves the row to the top of its new group. Press `O` round to sort again.

The bottom line names the current order, `O order · first`. In the `first` order each row shows
the triage reason dim before the age (`main is red    6h`), the first thing a narrow row drops.
`O` does nothing on the item page. The choice lasts until codeaf closes; it is not saved.

## factory HTTP read endpoints and access token

The factory HTTP handler exposes `GET /v1/queue` (jobs, repositories, pipeline strip,
capacity and the caller's handle), `GET /v1/jobs/{id}` (one full job),
`GET /v1/jobs/{id}/thread` (that job's manager thread), `GET /v1/events`
(a stream of changed ticks), `GET /v1/repos` (watched names and available repositories),
`GET /v1/repos/{owner}/{repo}/pipeline` (the pipeline read through the repository's
recipe), `GET /v1/github` (login and connection method), `GET /v1/me`,
`GET /v1/members`, `GET /v1/people/{handle}` and `GET /v1/me/sessions`.
Every JSON response, including an error, carries `factory: {address, version}`.
The pipeline includes any lines that could not load. Rules come from GitHub's default
branch even without a checkout; an absent file returns `source: "defaults"`. A file
that cannot be read or parsed includes the reason in `problems`.

Loopback connections are open. Connections from any other address require
`Authorization: Bearer <access token>` using the host configuration's access token
or an unexpired token from `tokens.json`, or a live session cookie created by entering that
token at `/access`. With no host token and no unexpired access token, remote
access is refused. Forwarded headers do not grant loopback access. Without either
credential the service answers HTTP 401 with
`a bearer access token or live session is required; open /access to enter the token`.

`/v1/me` returns the caller's lowercase `handle`, GitHub `name`, `avatar_url`,
`scopes` array and public `session_id`.
`codeaf_version` identifies the build and `contract` is 2. `machines` lists
that handle's own machines. `takes_unassigned` is the map last saved for that
handle, or `{}` when none has been saved. Before GitHub sign-in, an access token
uses the factory owner's connected GitHub login. After sign-in the same token
returns that holder's GitHub profile, including after a factory restart. A host
access token or a credential-free loopback request uses that same owner login
(blank without a connection), with blank profile fields and no session id.
These reads do not create a viewer session. A read whose seam
door is absent answers HTTP 501 with `this factory does not provide that read`;
an unknown job answers HTTP 404 with `job not found`. These read endpoints accept GET.
Run this handler with `codeaf factory` or `codeaf factory serve`.

## factory GitHub sign-in — device code, poll, profile and team allowlist

Set `github_client_id` in the factory host config to your GitHub OAuth app's
client id, enable device flow in that app, and restart `codeaf factory serve`.
With no client id, start answers HTTP 501:
`GitHub device sign-in needs github_client_id in the factory host config`.
Both sign-in routes use the existing access gate: remote callers need a host
token or an unexpired access token, or the cookie from `/access`; loopback stays open.

`POST /v1/auth/device` (no body or `{}`) returns `{factory, device_code,
user_code, verification_uri, interval, expires_in}`. Enter `user_code` at
`verification_uri`; poll with `POST /v1/auth/device/poll {"device_code":"..."}`
using the same credential. The code expires after `expires_in` seconds. Wait at
least `interval` seconds between polls. HTTP 202 returns `error` and `interval`:
`authorization_pending` means wait; `slow_down` increases the interval by five
seconds. Expiry answers HTTP 410 `expired_token`; a declined sign-in answers
HTTP 403 `access_denied`. A different caller or reused code answers HTTP 400. GitHub profile failures
answer HTTP 502 and require starting a new flow.

Success checks GitHub's `/user`, sets an HttpOnly session cookie, and returns
`{factory, access_token, token_type:"bearer", expires_in, me}`. The app sends
that factory token as a bearer. The factory requests `read:user` and `repo`;
`me.scopes` reports the scopes GitHub granted. GitHub credentials are encrypted
in `sessions.json` beside `tokens.json`, with a private `sessions.key`; both files
are mode 0600, like the host config. No GitHub token is returned to the app.
Sessions last 30 days and survive restart, until sign-out, revoke-sessions,
or the originating access token's revocation or expiry.
The public `session_id` is separate from the credential. An access token binds to its
first GitHub handle; another handle cannot replace it.

Optional `team` restricts signed-in handles. Someone outside it gets HTTP 403:
`this GitHub handle is not on the factory team`. The host token and open loopback
retain owner access. HTTP job-action audit handles use the caller; a worker's
posted event uses that worker's registered handle.

## Enable GitHub sign-in — create an OAuth App with Device Flow

In GitHub Settings → Developer settings → OAuth Apps, select **New OAuth App**.
Name it for your factory and set Homepage URL to your factory address. GitHub
requires an Authorization callback URL; use your factory address there too
(the factory uses Device Flow, so no callback endpoint is needed). Register the
app, open its settings, and enable **Enable Device Flow**. Copy the **Client ID**,
not a client secret. A client secret is not needed for device sign-in or refresh.

Set the id in `~/.codeaf/factory/config.yaml` (or the file passed with `--config`):

```yaml
github_client_id: "your OAuth App Client ID"
```

Restart `codeaf factory` to read the config. Existing factory sessions survive.
People off loopback still need a factory access token before linking GitHub.
The factory asks for `read:user repo`; classic OAuth's `repo` scope also grants
write access on private repositories because GitHub has no read-only equivalent.
See [GitHub's device flow documentation](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps#device-flow).

## GitHub token refresh and expired sign-in — github_expired

An expiring GitHub token includes `expires_in` and `refresh_token`. On the first
request within five minutes of expiry, the factory refreshes it using the OAuth
App's client id, saves the replacement access and refresh tokens encrypted, and
continues the existing factory session. Refresh still works after restart.
Tokens without an expiry need no refresh.

If GitHub returns HTTP 401 or `bad_refresh_token`, or the refresh token has
expired, the factory ends that sign-in and returns HTTP 401 with
`reason` `github_expired` and `error` `GitHub sign-in has expired; sign in again`.
The app must ask for GitHub sign-in again. The factory keeps only the credential
hash and public facts for the ended sign-in until its 30-day expiry so the same
reason survives restart. A GitHub outage returns HTTP 502 and retains the session
for retry. A repository's HTTP 403 or 404 still means it is not visible, and does
not end the session. Deleting a sign-in or revoking all sessions removes its
saved GitHub credentials.

## factory members — who has joined

`GET /v1/members` lists GitHub people who have signed in. Each member has
`handle`, `name`, `avatar_url`, `joined_at`, `last_seen` and `sessions` (how many
of their sign-ins are still live).
The list is the same for every caller who can open the factory. A restart
keeps someone who signed in with an access token: the profile and `joined_at` are stored
on that token row. A loopback sign-in that did not use a token row is remembered
across restarts while its saved session is live. `sessions` is 0 when they have no live sign-in.
Contract is 2. This list does not sign anyone out.

## sign out a factory session

`GET /v1/me/sessions` lists this caller's own sign-ins. Each has `id`, `device`
(the User-Agent at sign-in, or `this device` when the client sent none),
`created`, `last_seen` and `current`. `current` is true for the credential on
this request. `DELETE /v1/me/sessions/{id}` signs out that one session when it
is yours. Another person's id answers HTTP 404 `session not found`.
`POST /v1/me/sessions/signout-others` signs out your other sessions, leaves the
one you called it with, and returns `{factory, signed_out}`. Signing out does
not revoke an access token and does not stop a machine that attached with its
own worker credential. `codeaf factory revoke-sessions` ends all sessions and unassigns their
unfinished jobs. The operator can also revoke a person with
`DELETE /v1/people/{login}/sessions`; their runner credentials stop working. Sessions last 30 days
and survive service restarts. Sign-out and revoke-sessions are saved immediately.

## repos you can see — no_visible_repos

A caller who has signed in with GitHub sees only repositories that account can
read. The factory calls GitHub `GET /repos/{owner}/{repo}` with that person's
token and remembers each answer for 10 minutes. The queue's jobs, repositories,
pipeline strip, queued count and daily total follow that set, and so do audit
events, `GET /v1/repos`, pipeline, dry run and a changed tick that names a job.
A `*` tick still arrives: the following read is filtered, and the tick names no
repository. When any repository is hidden, since-counters, `machines_online`,
`busy` and `spent_today` are cleared because they are factory-wide, and
`capacity.daily` is the sum of the visible jobs' costs. A viewer who can read
every repository still sees the factory-wide totals. A job or repository they
cannot read answers HTTP 404, `job not found` or `repository not found`. A
viewer who can read none of the factory's repositories gets HTTP 200 with an
empty queue and `reason` `no_visible_repos`. Loopback without a signed-in
session, and the host access token, still see the whole floor. Watched repositories
appear in the queue even before they have jobs, when the caller can read them.
Saved sign-ins restore repository access after restart. A GitHub 401 ends that
sign-in with HTTP 401 and `reason` `github_expired`, rather than an empty queue.

## Your runners — idle, busy, offline, asleep and slots

`GET /v1/runners` lists computers with `id`, `name`, `owner`, `kind`
(`this_mac`, `mac`, `ci`), `slots` (default 2), `state`, `checkouts`, `jobs`
and `last_seen`. Your runners run only your jobs in a team. Unassigned team
jobs do not run. In solo mode every job is yours and there is no assignee.
`GET /v1/me` includes your `runners`, `mode` and `operator` flag.

`offline` means no heartbeat for 60 seconds. A step that was running makes the
job Needs you with `runner_offline` and `Runner went offline mid-step`.
`POST /v1/runners/presence` with a runner credential and `{asleep:true}` reports
macOS sleep: jobs wait and leases stay. `{asleep:false}` reports wake and starts
a fresh deadline. The app supplies these native notifications.
`PUT /v1/runners/{id}` changes your positive slot limit and checkouts. The local
execution count is `CODEAF_FACTORY_BENCHES`, default 2.

The older `machines` and `held_by` fields stay for earlier clients. The saved
`PUT /v1/me/takes_unassigned` map remains readable, but never permits an
unassigned team job to run. Runner facts and credential hashes survive restart.

## factory about, pipeline file, dry run and spent today

`GET /v1/about` answers before any token. The body is `factory`, `scheme`, `name`, `codeaf_version`,
`contract`, `needs`, `access`, `polling_every` and `served_by`. `scheme` is `https` or
`http`: how this caller reached the factory. `needs` is `token`,
`device` or `none`. `access` is `loopback`, `token` or `tailnet`. `served_by` is
`tui` when a window is serving and `factory` when `codeaf factory` is. `polling_every`
is present when this host polls GitHub, and absent when it does not. `name` and
`factory.name` are the configured display name, the first watched repository's
owner, or the host's short hostname without repositories. A loopback
address with no token is `access` `loopback`, `needs` `none` and
`served_by` `tui`. A configured access token is `needs` `token` and `access` `token`.
This read names no job and no repository. `GET /v1/me` carries the same `access`,
`polling_every` and `served_by`.

`GET /v1/repos/{owner}/{repo}/pipeline` returns the file that was read. `text` is the
repository file. `personal` is true when `~/.codeaf/factory.yaml` tightened it, and
`personal_text` is that file. `origin` is `main`, `working tree` or `defaults`.
The `start` object is `labels`, `max_size`, `max_cost`, `never` and `ask_first`.
`size` is `pr_lines` and `issue`. Each step's `until` stays `done`, `clean`, `green`
or `proven`. `until_sentence` is the file's own until line, such as
`the plan names its files`.

`GET /v1/repos/{owner}/{repo}/dryrun` adds `would_wait`, each `{job, reason}`.
One named label yields `no agent-ready label` when that label is `agent-ready`.
No named label yields `nothing starts on its own`. A GitHub job stored as the short
repository name matches `owner/name`. `GET /v1/queue`'s `since` fills `shipped`,
`asked`, `handled` and `spent` from the floor. `spent_today` is what was spent today.
`spent` counts only streams that began after the window opened. Every error body,
including HTTP 401
`a bearer access token or live session is required; open /access to enter the token`,
carries `request_id`, eight hex characters. Taking a job records the attached
machine's name when that machine has registered.

## factory HTTP job actions — run, stop, pause, resume, drop, answer, reply and shape

Send `POST /v1/jobs/{id}/run`, `/stop`, `/pause`, `/resume`, `/recheck` or `/shape`
without a body to run, stop, hold, continue, re-run a job's checks, or give its
manager one shaping turn. `/drop` takes no body, or `{"words":"why"}`; the words
are the audit detail, a holding worker is told to stop, and the branch is kept. `/shape` runs nothing and launches nothing. When a worker holds
the job the factory leaves `shape` on the lease and the response has no `line`; otherwise
`line` is what that turn recorded. A factory with no shaping turn and no holder answers
HTTP 501 with `this factory does not provide that action`.
Pause and resume carry explicit intent; repeating pause never resumes the job.
`/answer` takes `{"yes":true}`, `{"yes":false}` or `{"words":"your answer"}`;
yes and words may be combined. `/steer`, `/request_changes` and `/reply` each take
`{"words":"your words"}`. Words must contain text. Steering goes to the run,
request changes goes back through the proof gate, and reply goes to the manager
thread. `/prioritize` takes `{"to":"top"}` and selects the job through the
factory's existing selection door; no other priority destination is supported.

Every response includes `factory: {address, version}`. Successful actions answer
HTTP 200. An absent action answers HTTP 501 with
`this factory does not provide that action`. Invalid input answers HTTP 422
with `reason` `bad_input`; an unknown job answers HTTP 404 with `job not found`
and `reason` `not_found`. A verb the caller may not use because they are not
the job's person is HTTP 403 with `reason` `not_job_person` and `job_person`
`@sam`. Release of someone else's take stays HTTP 403 with `reason` `not_allowed`.
Wrong methods answer HTTP 405 with an Allow header. A crash answers HTTP 500
with no `reason`. A deadline is HTTP 504 and cancellation is HTTP 408.
A job that is already running answers HTTP 409 with `reason` `conflict` and
its sentence, `#12 is already running`. A stale `expect` is also HTTP 409;
see already approved, already taken, or the job moved on.
Actions accept the same bearer token or live session as reads off loopback.
Bodies with fields must be one JSON object, at most 1 MiB; extra fields are refused, and a larger
body answers HTTP 413 with `JSON body exceeds 1 MiB`.

## already approved, already taken, or the job moved on — expect, drop and release

Every `POST /v1/jobs/{id}/<verb>` accepts an optional `expect`:
`{"state":"needs_you","step":"approve","version":4}`. Leave out a field you did
not read. When one no longer matches, the factory answers HTTP 409 and does not
change the job. `reason` is `stale`. `now_state` is the state now. `by` and `at`
are set when the factory knows who moved it and when.

The `error` sentence is one of three. A job already done, or already publishing,
says `already approved by @raj at 11:58` (`by` is `@raj`, `at` is that time,
`approved_by` the same, `event` the approval id, `commit` the approved commit).
Take of a job someone holds says `already taken by @mei`, with `taker` `@mei`,
`at` when they took it, and `machine` when the worktree or the hold names one.
Anything else that moved says `job moved on while you looked`. A head that
moved since the page opened is `changed_since_opened`, with `by`, `at`,
`commit`, `current_commit` and `head_commit`.

`POST /v1/jobs/{id}/drop` takes `{"words":"why"}` or no body. The words are the
detail on the `dropped` Activity event. A runner holding the job stops.
The job becomes Done with `done_how: dropped`. Its issue stays open; its branch
is kept seven days, then deleted with a `branch_deleted` Activity event.

`POST /v1/jobs/{id}/release` is un-take. The person who took the job clears that
take and the record says `released`. The branch is kept. A job nobody took
answers HTTP 409 with `job is not taken`. Someone else answers HTTP 403 with
`only @mei can release this job`. Without a lease desk the answer is HTTP 501.

`POST /v1/jobs/{id}/approve` takes `{"edited":true}` when a claim is not shown.
Without it the answer is HTTP 409 `#N has a claim not shown` and no `approved`
event is written. A plan approval writes `approved` only after the answer door
accepts.

A question carries `kind`: `step`, `cap`, `max_rounds`, `plan`, `proof` or
`out_of_attempts`. These are compatible stored question kinds; the Contract 2
needs-you kind is `out_of_rounds`. `plan` is an approve step. `max_rounds` is an
older rounds question. `out_of_attempts` is the stored one, and its options are
`try_once_more`, `send_back` and `cancel_job`. `options` lists the choices
that step offered. A cap question also carries `limit`, `spent` and
`next_limit`, and `next_limit` is the cap a yes would allow.

## who may act — can on every job, not_job_person

Every job carries `can`: `approve`, `run_again`, `cancel_job`, `retry_publish`,
`answer`, `assign_to_me` and `reply`. Each is true or false for this caller.
Approve, run again, cancel job and retry publishing are the job's person.
A plain question's answer is anyone who can see the repository. Reply is
anyone who can see the job. `assign_to_me` is true only when the job has no
person yet. A job with no person yet still allows the person's verbs.

When `can` is false for that verb, the POST answers HTTP 403
`{"reason":"not_job_person","job_person":"@sam"}` and `only @sam can do this`.
An answer keeps the sentence `only @sam can answer this`. A job in the wrong
state is still HTTP 409. Take of a job someone else holds stays HTTP 409
`already taken by @mei`. Release of someone else's take stays `not_allowed`.

## needs you — Contract 2 kinds, failed, changes requested, conflict and who may answer

A waiting job carries `needs_you`: `kind`, `since`, `who_can_answer`, and
`question` when the wait is a question. The Contract 2 kinds are `approve_start`, `approve_plan`,
`approve_proof`, `question`, `budget`, `out_of_rounds`, `publish_failed`,
`runner_offline`, `failed`, `changes_requested` and `conflict`. The queue groups jobs by that kind. Adding a kind is a
change to codeaf, not a value an app can invent.

`question` is `id`, `kind` (`text`, `yes_no` or `options`), `text`, `options`,
`from_step`, `asks` (a handle) and `who_can_answer`. A free question's
`who_can_answer` is `anyone_with_repo`. Plan approval, proof approval, a
budget, out of rounds, failures, PR review/conflict and runner-offline
attention are `job_person`. When
the job has a person, someone else gets HTTP 403 `only @raj can answer this`.
A job with no person yet can still be answered. `answered` is `{by, text, at, event}`
once it has been answered.

## a spending limit is an answer, not a setting

When a job reaches its cap it waits as `budget`; the compatible `spending_limit` object carries: `limit`, `spent`,
`next_limit` and `currency` (`USD`). The run asks `budget of $5 reached · $5 more, or stop?`.
Answering `continue`, or yes, raises this one job to `next_limit`. That number
is codeaf's: the cap doubled. The app does not send it, and a body that tries
to is refused. Stop does not raise it. The raise is an `answered` event. It is
not a setting, and it does not change any other job.

## out of attempts, and a step that failed

A step that used its rounds without its until holding asks
`try once more, send it back, or cancel the job?`. That is needs-you `out_of_rounds`,
a question, options `try_once_more`, `send_back` and `cancel_job`. Only the
job's person answers. `send_back` needs a reason (`send back requires a reason`
otherwise) and returns to the previous step. `cancel_job`, or no, stops the job.
There is no going on as is.

A loop step keeps one line per finished round in `rounds`: `round`, `summary`
and `at`. The list stops at the step's max, or at the round a try-once-more
was allowed to reach, and never past 32. Out of attempts can show what each
try did.

A step that errored — a crash, a tool or provider error, or the machine lost
after the lease expired — is `failure` on the job and on that step: `step`,
`code`, `detail`, `attempts`, `machine`, `next` and `at`. `code` is `crash`,
`tool_error`, `provider_error` or `machine_lost`. `next` is `stopped` or
`requeued`. `attempts` is how many tries that step had made. `machine` is the
one that was holding the work. It is not a question. A stopped step appears in Needs you as `failed`; a
lost runner appears as `runner_offline` with “Runner went offline mid-step”.
It waits for the assignee rather than silently requeuing. The record says
`step_failed`.

A publish that fails stays in needs you as `publish_failed`, in the same place
as the other waits, with `could not publish: …` on its log and `publish_error`
on the job. The approval already recorded stays valid for that same commit.
Retry publishing does not write a second approval.

## a job without a GitHub issue

Work typed in the app or in the terminal carries `ask`: `text`, `by`, `at` and
`source`. `source` is `app`, `tui` or `hand-off`. A hand-off also carries
`ask.from_ref: {kind:"agent", id, title}` so the brief links back to its chat. A job that started from a GitHub issue
carries `ask` with `issue` (`number`, `url`, `title`) and `text`, and does not
also store a typed ask. The pull request body starts with that ask, then
`Closes #12.` when the job closes an issue. A job with no ask text leaves the
body as it was.

## approving at the proof gate publishes — the pull request, the commit and the trailer

Approving a landed item (`s`, `e`, the web's approve, `POST /v1/jobs/{id}/approve`) is
the one way its work leaves your machine, and it always waits for a person. The factory
records your approval (`evt_` and eight hex digits, under your GitHub handle) only
after the claim check accepts, then
queues the job again with a publish for a worker; its log says `approved · publishing`,
then `publishing · Factory-Approval: evt_… @handle <commit>`.

Before opening the proof gate, the holding worker commits the worktree with the
item's title and reports `head_commit`. Approval names that exact commit. The publishing
worker refuses a moved head or uncommitted edits; it pushes the approved item's
branch (`factory/<number>-<slug>`) to `origin` with your connected GitHub token, never
with force, and opens the pull request into the default branch. The body starts with the
ask when the job has one, then `Closes #12.`
for an issue with new commits, `Proof · N of M claims shown`, one `✓`/`✕` line per claim, and the trailer
as its last line. The item becomes shipped with its pull request (`pr` on the wire), and
the record says `posted · pr · <url>`. A pull request already open from that branch is
updated with this approved proof and trailer, without opening another PR.

**A pull request item** is not pushed to: approving it posts one comment on that pull
request with the same body and trailer, and the item's `pr` is that pull request.

## the assignee's runner opens the pull request — team and solo GitHub publishing

The PR is opened as the job's assignee. The runner holding the job's lease on the
assignee's machine pushes the approved commit and opens or updates the PR, or posts
the proof comment. It uses that machine's connected GitHub account: a token handed
over by the app, a terminal token, or `gh` after you have agreed to use its login.
One publish attempt uses the same token for the push and the PR or comment.
An app's GitHub handoff takes precedence over `GH_TOKEN`, `GITHUB_TOKEN` and the
profile; removing the app's key does not reconnect a different account.

The runner checks that its connected GitHub login matches the assignee before it
pushes or writes. A different account fails publishing with `no_push_permission`;
connect the assignee's account on that runner before retrying. The job's repository
comes from the lease; the runner need not watch it locally. The factory server
reads GitHub for checks, reviews and merge or close state and records the runner's
publish receipt. It never opens the job's PR or posts its proof comment with the
server's own GitHub connection. A solo factory follows this same lease and runner
path on your machine. The approval trailer still names the approved commit, and
repeating an HTTP action with the same `Idempotency-Key` replays its first response.

## publishing failures, approval consent and the worker machine

**Never without consent.** An approve with no recorded approval publishes nothing and
says `job requires a recorded approval before publishing`. A push or a write that fails
lands the item again in needs you as publish failed, with `could not publish: …` on its log.
The approval stays valid for the same commit: use **Retry publishing**, without another
approval. A changed commit needs approval again. The lease records the worker's stable
machine id, and the job keeps that id after the lease ends. Publishing, re-running checks and resumed runs are offered only to workers on
that machine, including a new process or another slot there. While queued it says
`waiting for <machine>` (or `publishing · waiting for <machine>` after approval).
If that machine is offline before publishing starts, the job waits for it; another
machine never takes it. If its lease expires during publishing, the job becomes
Needs you with `runner_offline` and `Runner went offline mid-step`. Choose
`try_once_more` to resume publishing on that machine with the same approval, or
`cancel_job` to stop. Returning online alone does not retry an interrupted write.
The TUI says `publishing` between approval and the pull request, then `shipped`.

## no diff at the proof gate — approve with an issue comment

If the worktree is clean and its branch has no commits beyond
the default branch, approving posts the proof summary as a comment on the issue,
with `Factory-Approval: evt_… @handle <commit>` as its last line. Nothing is pushed and no
pull request is opened. The proof gate says `Approve → comment on #N`; the wire
says `publish: "comment"` instead of `publish: "pr"`. The job becomes done with
`pr: null` and `comment: "<comment url>"`, and the record says `posted · comment · <url>`.
A clean worktree with commits of its own still publishes a pull request.

## factory HTTP approve — plan approval, proof approval and the event receipt

`POST /v1/jobs/{id}/approve` takes `{"commit":"<head_commit>"}`; add
`"edited":true` when approving a result with a claim not shown. Use the job's
`head_commit` as it stood when you opened it. A moved head returns HTTP 409
`{"reason":"changed_since_opened","head_commit":"<new head>"}` with
`This job changed since you opened it`, plus `by`, `at`, `commit` and
`current_commit`. A reported head without `commit` returns
422. Only older jobs with no reported head retain `{}` or no-body approval. A job waiting at an approve
step receives a yes answer. A ready proof goes through the factory's sign-off
door, which still enforces the claims and policy gate and then publishes (see
approving at the proof gate publishes). Any other job answers
HTTP 409 with `job is not waiting for approval`.

Success answers HTTP 200 with `approval` and `undo_until`. The server waits
five seconds before the answer or publishing door opens; `undo` cancels the
pending decision. After the deadline, the accepted decision writes its
`approved` event. A moved head or refused door stays waiting with an error.
A claim not shown without `edited` answers HTTP 409 and writes no approval.
A factory with no record refuses with `approval requires a factory record`.

## factory publishing states, publish failed and Retry publishing

Jobs and queue rows carry optional `publishing` with `state` (`queued`, `pushing`,
`opened`, `failed`), `code`, `detail`, `pr: {number,url}`, `attempt`, and `at`.
An attempt starts at 1 and increases on retry. `opened` means the PR was opened
or the proof comment was posted. Whether that pull request is still open, merged
or closed is `pr.state`, read on later polls (see following a pull request). The existing
`desk.publishing` boolean remains available.

A failed publish is in Needs you with `gate: "publish_failed"`; an app draws it
in red and offers **Retry publishing**. `POST /v1/jobs/{id}/retry_publish` runs
publishing again with the same recorded approval, without a new approval event.
Only the job's person can retry (the approving person when the job has no owner).
A changed commit returns 409 `changed_since_opened`: open the new version and
approve again. A job that is not failed returns 409. Run again, request changes or recheck clears
the previous publishing outcome and opens fresh proof. A failed publish can be
run again when its failure code calls for rebuilding the work. When the holding machine
is away, publishing stays `queued` with `code: "machine_offline"`; it waits for
that machine to return, keeping the approval.

## following a pull request — open, merged, closed, reviews and checks

After the factory opens a pull request, or posts proof on one, every GitHub poll
reads that pull request again. The open list drops it once it merges or closes,
so this read is separate. A comment-only publish has `pr: null` and is not followed.

The job stays `running` while the published PR is open, with `reason: github`.
Changes requested make it `needs_you`; merging or closing makes it `done`.
`pr.state` carries the GitHub facts:

- `open` — still open. The design calls this PR open.
- `merged` — someone merged it. The design calls this Merged. `merged_by` is the
  login and `merged_at` is when.
- `closed` — closed without a merge.

`pr` also carries `draft`, `review_decision` (`approved`, `changes_requested` or
`review_required`), `reviews` (each `by`, `state`, `at`), `required_reviews` (how
many approving reviews the base branch asks for; absent when it asks for none)
and `checks` (`passed`, `failed`, `pending`). `pr.ci` stays the short line
(`ci ✓`, `ci running`, `ci ✕`). A zero check count is left off.

The first poll that sees `merged` or `closed` appends `pr_merged` or `pr_closed`
to the record, with detail `pr · <url>`. A later poll that sees the same state
does not write it again. The queue's `since.merged_today` and `since.merged_week`
count merged pull requests, today and this week from Monday. Contract is 2.

## factory publish failure codes and next steps

`GET /v1/publish/codes` returns `{factory, codes: [{code,next}]}`. The factory owns
this table so an app can show the next action without guessing from GitHub's message.
`publishing.code` names its row; `publishing.detail` preserves the failure message.

Codes are `branch_protected`, `no_push_permission`, `push_rejected` (the remote
branch moved), `pr_create_forbidden`, `repo_archived`, `github_unreachable`,
`rate_limited`, `machine_offline`, `worktree_missing`, and `unknown`. Fix access
or protection before retrying. A moved branch or missing worktree needs the job
run again; an archived repo needs Cancel job. A connection or rate limit failure
can be retried when GitHub is available. The machine-offline case waits on its own.

## factory HTTP double click, repeat requests and Idempotency-Key

Every POST accepts `Idempotency-Key`. Repeating a key on the same route with the
same credential within 24 hours returns the first status, headers and body and
does not run the action again, even if the repeated body differs. Choose a new
key for a new action, including a later Retry publishing. A repeat may replay an
error; fix it and send a new key. Store-backed factories retain responses across
restarts. A handler without a store retains them only until that handler stops.
Keys from different credentials or routes do not share responses. Concurrent
double approval creates one approval receipt and one publish signal even without
a key. An affirmative `answer` at a plan gate also takes the job's `commit`.

## factory HTTP add a job, watched repositories and connect GitHub

`POST /v1/jobs` takes `{"repo":"owner/repo","title":"work","text":"brief",
"autostart":false}`. Repo and title are required. Text and autostart are optional.
The handler passes the title and text, separated by a blank line, to the
factory's new-job door. Success answers HTTP 201 with `{id, factory}`.
Autostart also calls the run door with the new id; without that door it refuses
before creating the job. If starting fails after creation, the job stays created
and the request returns the start error.

`PUT /v1/repos` takes `{"watched":["owner/repo"]}` to replace the watched list;
an empty array clears it. `POST /v1/github/connect` takes `{"token":"…"}` to
connect GitHub; `{"token":""}` selects gh's existing login. Both answer HTTP 200
with the factory identity. An omitted watched array or token answers HTTP 422.
These routes use the same access check and error statuses as job actions.

## where is the web ui — factory dashboard address and team view in the codeaf app

The team view lives in the codeaf app (`Agent-Field/codeaf-desk`). Connect that app to
the factory service's HTTP address. In the terminal, open `/factory` in codeaf.
The app's views, browser approval and screen instructions belong to that repository.
The factory binary serves no dashboard pages.

`GET /` answers `text/plain` with one line:
`codeaf factory <version> — open it in codeaf (TUI) or the codeaf app; API at /v1`.
Paths outside `/v1`, such as `/jobs/12`, `/settings`, `/new` and `/audit`, answer the same
pointer. The public `/access` token form remains available. Outside loopback, the pointer
and API need a bearer access token or a live session from `/access`.
Unknown `/v1` paths return the JSON error `factory endpoint not found`.

The app reads `/v1` JSON and the `/v1/events` change stream. `codeaf attach` uses the
same address for new terminal windows; `serve.json` records the bound address on its host.

## factory HTTP Activity — events and CSV export

`GET /v1/activity` reads the factory's record as JSON `{factory, events}`.
`since=<RFC3339 timestamp>` limits the read to later events.
`GET /v1/activity?format=csv` downloads `factory-record.csv`, with columns
`id`, `at`, `kind`, `job`, `handle`, `cost`, `detail`.
A factory with no record says `this factory does not provide that read`.
These reads require the same access as the other factory API calls.
There is no record page at `/audit` on this service; the team view is in the app.

## factory event stream and a job's manager thread

`GET /v1/events` is a server-sent event stream. `changed` keeps its plain job
id or `*` data; read that job or the queue again. Filesystem pushes are batched
within 100ms even during continuous writes. A missing watch falls back to a
2-second root scan.

Typed JSON events also arrive with `id: live_<number>`:
- `job.state {job, state, reason}` names creation and every projected state or
  reason change, including PR review/conflict; an empty reason clears it.
- `needs_you {job, kind, since}` uses the Contract 2 needs-you kinds; an empty
  kind removes the job from Needs you.
- `question {job, kind}`, `approved {job, by}` and `taken {job, by}` name a
  changed ask or the person whose accepted action moved the job. Free asks use
  text, yes_no or options; approval asks use their Needs you kind.
- `publishing {job, state}` sends queued, pushing, opened and failed transitions;
  `published {job, pr}` carries `{number,url}` for a PR (null for a comment);
  `publish_failed {job, reason}` carries the failure code, or detail without one.
- `runner {runner, owner, state, last_seen}` names idle, busy, offline or asleep;
  `activity` carries each recorded action. The older `machine` event remains
  compatible. Silence sends offline at the 60-second deadline.

The factory saves every typed transition before returning from its write, so
rapid publishing changes survive batching. The last 200 typed events survive a
restart. Reconnect with `Last-Event-ID` to read events strictly after that id.
An expired or unknown id replays the saved window and sends `changed` with `*`;
read the queue for current state. A first connection starts with live events.
Only visible jobs reach the viewer, including replay. Machine events name your
own idle machines or machines tied to visible jobs. Slow clients catch up within the
saved window. Every 20 seconds `: keepalive` keeps an idle stream alive without
changing its event id or requesting a read. A factory without a store sends
keepalives only. `/v1` Contract is 2.

## factory manager thread on another machine, pending replies and delivered messages

`GET /v1/jobs/{id}/thread` returns `{factory, messages}` from the job's stored
thread, including jobs running on another person's machine. Workers report
spoken manager lines from their own journal; the factory never opens that
machine's transcript. Tool rows are left out. A job with no reported lines or
replies returns `messages: []`; reading creates no conversation.

Messages keep `{at, from, text}` and may add `id` and `state`. `from` is `manager`
for the manager and the connected GitHub handle (or `user` when absent) for a
reply. `POST /v1/jobs/{id}/reply {"words":"your words"}` adds the reply immediately
with `state: pending`. The holding machine collects it on its next heartbeat,
or an eligible machine pulls the idle manager. The worker reports the reply's
ID after delivery; its state becomes `delivered`. An idle reply runs no job
steps and leaves the job's state alone. Pending replies survive lease expiry
and a service restart. An offline machine leaves the reply pending.

A thread keeps at most 200 messages, each at most 16 KiB. Pending replies do not
scroll away. Reply refuses over 16 KiB with `reply exceeds 16384 bytes`, or 200
pending replies with `job has too many pending replies`. A job without a manager
that is neither queued nor running says `the job has no manager conversation`.
Queued and running jobs can receive a reply while their worker makes the manager.

An unknown job answers `job not found`; an invalid id says
`job id must be a positive integer`. The reads follow the usual factory access
rule and return `factory: {address, version}`. Contract is 2.

## factory answered question history — who answered, words, time and event

`POST /v1/jobs/{id}/answer` closes the open question and keeps it in the job's
`questions` with `state: answered` and `answer: {by, text, at, event}`. `by` is
the connected GitHub handle; a bare yes or no is kept as `yes` or `no`.
`event` links to the factory's answer or approval receipt when a record exists.
The latest 100 answered questions survive worker reports, pauses and restarts.
An answered question no longer holds the run or becomes its open question when
a client reads the job back. New asks still appear with `state: open`.

## factory HTTP workers — attach, pull a job and heartbeat

`POST /v1/workers/attach` takes `{"machine":"laptop","name":"laptop (pid 123)","handle":"sam"}` and answers
HTTP 201 with `{factory, worker_id, credential}`. Loopback attach needs no access
token; remote attach requires the factory access token. Each attach creates a
fresh worker identity. A signed-in caller can attach only under their own
handle (omitting it fills that handle); a different supplied handle answers
HTTP 403 `worker handle must match the signed-in caller`. Runner credentials are stored as hashes and survive a server restart.

On loopback, an empty or omitted `handle` uses the factory's connected GitHub
login, the same identity as Take, Approve and `/v1/me`. Without that login it
answers HTTP 422 with `worker attach requires name and handle`. Off loopback,
an empty handle answers HTTP 422 with `worker attach requires a handle`, even
with a valid access token or cookie. Only the socket peer decides loopback;
forwarded headers and the requested hostname do not.

The built-in worker sends this machine's own connected GitHub login. Attached
to a remote factory without one, it refuses to start with
`codeaf needs your GitHub login to work for this factory`.

All other worker endpoints require `Authorization: Bearer <credential>`, even
on loopback. The factory access token cannot replace a worker credential, and a
worker credential cannot read the remote factory. Missing or wrong credentials
answer HTTP 401 with `a bearer worker credential is required`.

`GET /v1/workers/pull` waits at most thirty seconds for a queued job and answers
HTTP 200 with `{factory, lease, job}`, or HTTP 204 without a body. One worker holds
one job, and two workers cannot pull the same job. In a team, only jobs belonging to the runner's owner are offered.
`takes_unassigned` and its saved map cannot permit unassigned team jobs to run.
In a solo factory every job is yours. `os` and
`codeaf_version` name the machine for `GET /v1/me`.

The lease includes `id`, `job`, `worker`, `machine`, `handle` and `expires_at`.
`machine` is stable across processes and slots; the built-in worker uses its hostname.
An older attach without `machine` uses `name` as its machine id.
`POST /v1/workers/leases/{id}/heartbeat` answers `{factory, lease, signals}` with a
renewed expiry. `signals` are the verbs a person asked of the job since the last
heartbeat, oldest first: `{"verb":"hold","yes":true}` (pause; false resumes),
`{"verb":"answer","yes":true,"words":"…"}` and `{"verb":"steer","words":"…"}`. A pull
may carry `{"verb":"recheck"}`: run the job's checks again instead of its steps. The default hold lasts one minute. Expired running jobs become Needs you with `Runner went offline mid-step`. A renewed hold keeps its id; a new hold gets a different id. Another
worker's hold answers HTTP 403 with `lease belongs to another worker`; an expired
or completed hold answers HTTP 409 with `lease is no longer live`.

## factory changed files, diff stat, branch, test counts and draft pull requests

`GET /v1/jobs/{id}` and `GET /v1/queue` can include `change` on any job,
including an issue or a job written in codeaf without a GitHub issue. The machine
holding the job reads its worktree after a step finishes, including failed
checks and re-run checks. `change.files` lists each `path`, `status` (`added`,
`modified`, `removed`) and any nonzero `added` and `removed` line counts.
`change.stat` totals the files and lines; `change.branch` names the actual branch.
Committed, staged, unstaged and untracked files count; ignored files do not.
Renames list the old path removed and the new one added. Binary files have no
line counts. The list is complete, unlike GitHub's twenty-file detail list.
The comparison stays at the issue's initial commit or the PR's merge base, so
committing during a step does not hide the earlier work.

`change.tests` has one row per check that reports counts:
`{"step":"test","passed":12,"failed":1,"skipped":2}`. The same counts appear
at `steps[].tests`. Counts are read from Go verbose or JSON output, TAP, or a
pytest summary, before the output tail is cut. A plain package `ok`, an exit
code, or green CI cannot tell how many tests ran; counts are omitted then.
Each check keeps only its latest counts; running it again without counts clears
the previous counts. Unknown facts and zero totals are absent. Before the
machine reports, `change` is absent. If a later file read fails, the last
readable snapshot stays. A source PR job carries `draft: true` or `false`;
a published PR receipt carries `pr.draft`.

These fields are additive in Contract 2. The holding machine reports them
through `POST /v1/workers/leases/{id}/report`; they survive pauses and restarts.

## factory HTTP worker reports — step results, logs, cost, claims, questions and published receipts

`POST /v1/workers/leases/{id}/report` takes an incremental JSON object:

```json
{"steps":[{"name":"proof","state":"done","round":1,"result":{"done":true,"output":"checks pass","cost":0.25}}],"log":[{"tone":"test","text":"checks pass"}],"cost":0.25,"claims":[{"text":"checks pass","proven":true,"evidence":{"kind":"test","ref":"go test"}}],"pr":{"number":42,"url":"https://github.com/owner/repo/pull/42","ci":"green"}}
```

Every field is optional. Cost is newly incurred dollars, added once per report;
a step result's cost is descriptive and is not added again. Send each increment
once. Worker reports also carry `publish: "pr"` or `publish: "comment"`; a published
issue comment is reported as `comment: "<comment url>"` with no PR.
Log lines are stamped by the factory and keep the runner's newest 400 lines.
Step results keep its output limit of 16 KiB, notes, findings, exit status, claims
and chat id. Claims from a proof step join the proof sheet; policy evidence joins
the policy sheet. Top-level claims merge by their text. Reports cannot answer an
approve step. `pipeline` (the job's steps, as a job read lists them) reshapes the
job when the worker's manager or plan changed its steps; it is refused if it takes
an approve step away. `talk` names the job's manager conversation and `notes` adds
sentences to the job's notes.

A `question` takes `text`, optional `from_step`, `kind`, `options`, `pick` and
`why`; it puts the job in `needs_you`. There is one open question per job.
`GET /v1/jobs/{id}` returns that reported ask once in `questions`, keeping its
`kind`, step, options, pick and reason together with `with: "you"` and the
factory's timestamp. Answering the question removes it from the job read.
`pr` keeps a published PR's number, URL and CI separately from the source issue.
Reports update the job reads through the runner's existing store writes, without
replacing the job or its ownership. Invalid reports answer HTTP 422 and write
nothing. Bodies obey the same one-object, 1 MiB limit as other job actions.

## factory HTTP worker done — release the lease and keep the approval gate

`POST /v1/workers/leases/{id}/done` releases the hold after writing its final
state. An empty body or `{}` uses the runner's landing behavior: proof waits for
your approval, or a clean proof with no approve step finishes automatically.
`{"state":"needs_you","ready":true}` also lands the proof. `{"state":"needs_you"}`
requires a question already on the job. `backlog` and `dropped` report interrupted
or dropped work. `{"state":"done"}` is accepted for clean proof with no approve
step, or for a published job: one with a recorded approval and a reported pull
request or issue comment URL, which ships and records `posted`. Otherwise it answers HTTP 422 with
`job requires proof approval`.
Success answers HTTP 200 with the factory identity. Completed holds reject later
heartbeats and reports. Every `codeaf` speaks this protocol as its worker loop.

## attached factory on another machine — local job keys and worker leases

When `attach.json` names a factory outside loopback, job keys that need this
machine's store are absent unless a worker on this machine holds that job.
An unrelated local item with the same number never enables them. This covers
the pause fallback, budget, thinking, stage switches and additions, manager
edits and chat, shape steps, refresh, sync, ask the author, save stages as a
recipe, and open on GitHub.

For a held job, available local keys act on the worker's mirror of its run,
never on this machine's own factory. A receipt identifies the factory,
job, worker and lease expiry; a completed, expired or dead worker removes those
keys on the next read. Local calls check the receipt again before acting; a
failed receipt check says `this job is no longer running on this machine`.
Shaping needs this process's own configured worker; another process's shaping
door stays absent. Repository checkout and recipe keys keep their local meaning.

The HTTP job actions still go to the attached factory: run, stop, pause and
resume, answers, steering, approval, request changes and re-run checks work
through its worker leases, including when the job runs on another machine.

## what the factory does not do yet

Be plain about this when asked:

- **Items arrive three ways:** from a chat with `factory_add` (after you answer its card), from
  `n` in the factory, and from the GitHub repositories you watch (see connecting github). They
  are kept on this machine and are still there next launch.
- **New jobs run on whichever codeaf's worker pulls them; resumed work and publishing stay on the first worker's machine** (see workers, taken jobs and lease
  expiry). `r` and `L` queue items (see running an item), and stop, pause, answers, steering,
  approve, request changes and re-run checks work from any window in the factory.
- **Opening an item page does not shape its steps in a window attached to a session host.**
  The manager's reading on open runs only in a codeaf that runs its own conversations.
- **Only GitHub is connected.** No GitLab or Linear.
- **Writing a recipe still needs a local checkout.** Reading rules through GitHub
  needs no checkout. Without one, `b` and banking stay absent; the recipe page (`E`)
  shows the shared pipeline and any read problems, but cannot save edits.
- **The day rail stops launches, not running items.** A job pulled past it goes back to
  backlog with the reason on its log; an item already running goes on.
- **Only an approval or a post stage writes to GitHub.** Approving a landed item opens its
  pull request or posts proof-only work as an issue comment (see approving at the proof gate publishes); a post stage writes a comment,
  labels, a close or a pull request, only what the recipe's policy allows, and only after
  the job's approval is recorded.
- **The diff does not open yet.** `d` on a landed item says where it will be.
- **The chat does not launch work.** `factory_add` adds a backlog job; eligible forge jobs
  can start through automatic intake under repository rules.
- **The foreman only reads and selects.** `m` opens it (see the foreman); it cannot run,
  ship, post or change an item, and the person's `L` is the only way to run its selection.
- **`CODEAF_FACTORY_FIXTURE=1`** shows a still, made-up factory (three repositories, ten items)
  in place of yours, with no verbs.
- **Over `--host` or `--at`**, the factory page can read the service named by this machine's
  `codeaf attach` (see factory over --host or --at). The remote chat has no `factory_add`.
- **`Factory ? N` on the tab bar appears only after the first open** of `/factory`.

## running an item — r and L, where the run stops

`r` runs the item under the cursor: every stage, holding at each approve step (see approve
steps). `L` runs every selected item (or this one). There is no plan-first key: the default
holds after the plan already. The bottom line says `#12 is running` once the factory's next read
sees it start, and `#12 is queued · a bench frees it` only when that read still finds it queued
behind full benches. A run item is `queued` until a worker pulls it, takes one of that
runner's slots (two at once; `CODEAF_FACTORY_BENCHES` pins another number) and runs its
stages in order. It lands with its proof sheet for you to approve, or, with no approve step among its
stages, ships by itself when every claim was shown.

**What runs today:**

- A **check** stage runs its command in the item's own worktree (see where an item's work
  lives); the exit code is the answer. With no known checkout it says
  `codeaf does not know where <repo> is checked out`.
- A **post** stage writes through the connected GitHub account (comment, label, pr, close),
  when the recipe's policy allows it. `pr` first pushes the item's branch to `origin`.
- A **chat** stage (plan, write, review; in codeaf's default recipe test and proof too, so test
  runs what the change implies itself, and `3. test · check · go test ./...` in the recipe file
  makes it a check) is a conversation, named `#12 · review`, made in the
  item's own team under `factory` in the team menu, where you can open it. It works unattended,
  for at most two hours, and ends with `stage_result` (see a stage is a conversation).
- A **gate** waits for you.

**Every HTTP job action works from any window on the same factory.** Run queues the job
for an attached worker. Stop releases its lease; pause, answers and steering reach the
worker through its heartbeat, within about two seconds. Approval and request changes
also go to that factory. No window owns execution: a worker runs the job on its own
machine. Local job keys need that machine's live lease when attached to a remote factory
(see attached factory on another machine — local job keys and worker leases).

**What a stage may do.** A chat stage runs with the allow posture (what `--yolo` gives) in the
item's own worktree, never your checkout, so it edits files and runs commands without asking; your own approval
settings do not narrow it (see what a stage may do).

**Spend.** A chat stage's calls are on the spend ledger under its own conversation, and the
item's spend counts them against its budget and the day rail.

## it says CodeAF is not checked out — clone it, where does it go

A run never starts without a checkout. `r`, `L` and `L` on the foreman's selection first look
for where the item's repository is checked out on this machine. When codeaf knows no folder for
it, nothing starts and the question stands at the bottom of the factory, such as
`CodeAF is not checked out on this machine · clone it into ~/.codeaf/v3/factory/repos? [y] clone · [n] not now`.

- `y` clones it, saying `⠋ cloning Agent-Field/CodeAF…` while it runs (a large repository takes
  a minute), into `~/.codeaf/v3/factory/repos/<owner>/<name>` (`v3/factory/repos` under
  `CODEAF_HOME` when that is set), records that folder as the checkout, and then runs the item.
  It uses `gh repo clone` when gh is installed, else `git clone https://github.com/<owner>/<name>`,
  over the connected GitHub account. A clone that fails says
  `could not clone Agent-Field/CodeAF · <git's last line>` and nothing runs.
- `n` (or `esc`) runs nothing and says
  `not run · clone CodeAF first, or tell codeaf where it is in the repos list`.
- Where codeaf cannot clone, it says
  `not run · codeaf does not know where CodeAF is checked out · open it from that folder once`.

`L` over several repositories without a checkout asks once per repository, in turn; a `y` to
each clones it, and the items run once every one has a folder. A `n` keeps the selection.

**Your own checkout comes first.** Opening codeaf inside a watched repository records that
folder as its checkout, and a repository with any known folder is never cloned. The repos list
(`R`) says `here` for a repository checked out on this machine and `clone on first run` for one
that is not.

## where an item's work lives — a worktree and a branch per item

An item never works in your checkout. The first round that needs a folder makes the item its
own git worktree, `~/.codeaf/v3/factory/work/<repo>-<number>` (such as `work/api-12`), on its
own branch, `factory/<number>-<slug>`, the slug from the title, at most forty characters (such
as `factory/12-total-double-counts`). Every later round, and every stage, works in that same
folder, so two items running at once never write over each other. The item's log says
`branch: factory/12-total-double-counts` once, when the worktree is made.

The branch starts from your remote's default branch (`origin/HEAD`) when git knows it, else
from the branch your checkout is on. **A pull request's branch starts at the pull request's
head**: codeaf fetches `refs/pull/<number>/head` from `origin` (GitHub serves it for a fork's
pull request too) with the base branch beside it, so the change is `git diff origin/<base>...HEAD`
in the worktree. A worktree made before that, or one whose pull request was pushed to since, is
moved to the head the next round when nothing of its own is on it, and the log says
`at the head of #12: <commit>`; one a step already committed into stays where it is. A
repository with no `origin` cuts a pull request's branch the way it cuts an issue's, and a fetch
that fails says `could not fetch the head of #12: <git's last line>` and the stage does not run. **Your checkout is never touched**: uncommitted changes
in it stay where they are, are not an error, and do not follow the item.

A `pr` post stage pushes the branch first, with `git push -u origin <branch>` and never with
force, then opens the pull request from it. A failed push says
`could not push factory/12-…: <git's last line>`; a repository with no `origin` says
`#12 has no remote to push to`. A worktree git could not make says
`could not make a worktree for #12: <git's last line>`, and the stage does not run.

Stop, approve and request changes all keep the worktree and the branch. Nothing deletes either yet: to
clean one up, `git worktree remove <folder>` in your checkout, and the branch stays.

## where factory items are saved

Factory items are saved on this machine, in the v3 factory folder under the codeaf home:
`~/.codeaf/v3/factory`, or `v3/factory` under `CODEAF_HOME` when that is set. Each item is one
small file named by its number, such as `1.json`, and a change rewrites that file. They are not
in any repository. The HTTP service keeps worker reports in its host store; ordinary
local runs keep them on this machine. Saving a job does not itself post to GitHub. The same folder
holds `repos.json` (the GitHub repositories you watch), `repos/` (the repositories a run cloned
because this machine had no checkout of them), `sources.json` (when GitHub was last
read, the day rail and what a triage read costs) and `busy.json` (what is being read right now). A chat's
`factory_add` and the page's `n` write to the same folder, so an item added from a chat is a row
in the factory straight away. Deleting the folder empties the factory.

## the Factory button on the tab bar

The tab bar at the top of every page shows `Factory` third, after `Home` and `Chats`. When items
in the factory wait on your answer it carries their count in the question colour:
`Factory ? 5`. With nothing waiting it carries no number at all. The count is what the factory said
the last time it was read, and the factory is read while the factory page is open, so it appears
after you have opened `/factory` once. On a narrow bar the places after it fold into `More ▾`
first; a `Factory` with a count stays on the bar. `alt+9` still opens it, whatever its place on
the bar.

## factory Activity history and CSV export

A factory service supplied with a record keeps append-only `events.jsonl` beside its
saved jobs. Each event has `id`, `at`, `kind`, `job`, `handle`, `cost` and `detail`.
The record accepts starts (`started`, `auto_started`), stops (`stopped`), intake questions (`asked_first`), approvals (`approved`),
answers (`answered`), posts (`posted`), takes (`took`), releases (`released`), drops (`dropped`),
requests for changes (`request_changes`), step failures (`step_failed`), and a pull request this factory opened merging or closing (`pr_merged`, `pr_closed`). Service run, answer, drop and request
changes doors record successful actions; the stop door records the stop before returning
the job to backlog. Automatic starts through Add work record
`auto_started`. The claims desk's Take wiring remains a separate service connection.

`GET /v1/activity` returns `events` with the factory identity. Add `?since=<RFC3339 timestamp>`
for events strictly after that instant. Add `?format=csv` (or combine it with `since`)
to download `factory-record.csv`, with the same events in append order. Both reads
use the service's normal access check. A service without a record cannot provide
these reads. This change adds no record key to the terminal factory.

## factory approval receipts and pull request trailers

The service's Approve door writes an `approved` event with an `evt_` id followed by
eight hexadecimal digits before asking the runner to approve the plan or proof.
It returns that id as `event`, and the record carries the approved `commit`.
Answering yes at an approve step also records consent
before the runner continues. The handle comes from the caller's signed-in GitHub profile, or the access
credential's identity before sign-in; the caller's body cannot choose it. A missing identity, unavailable record or failed disk write
refuses approval. A runner refusal can leave the recorded approval attempt; it
does not make the job done or bypass the proof gate.

Approving at the proof gate publishes with that trailer (see approving at the proof
gate publishes). A post stage reads the same record: it appends
`Factory-Approval: evt_<8hex> @handle <commit>` to a pull request's or a comment's body, and
refuses every verb (comment, label, close, pr) without a recorded approval for that job. A new start, a drop or
requested changes withdraws earlier approval; the next post needs fresh consent.
After a successful source write the executor records `posted`, including cost and
the receipt URL. A failed post is never retried automatically. The worker reads the
record of its own machine's factory, so on a worker attached to a factory on another
machine a post stage refuses for want of the approval there.


## why did this start by itself — automatic intake, start rules and auto start

A repository opts into automatic intake in `.codeaf/factory.yaml` on its default branch:

```yaml
start:
  labels: [agent-ready]
  max_size: M
  max_cost: 2
  never: [auth/**, migrations/**]
  ask_first:
    - anything that changes how secrets are read or stored
```

After polling and triage, a backlog job with any named label is queued through the ordinary
Run door when size and estimated cost fit. A worker pulls it and runs the configured
pipeline. Defaults are M and $2; the job's own cap also bounds the estimate. Labels match
without regard to case. No matching label means it waits. No `start` or an empty `labels`
list means nothing starts automatically.

Personal `~/.codeaf/factory.yaml` may remove allowed labels, lower size or cost, add `never`
paths or `ask_first` sentences, and tighten other constraints. Looser personal values are
ignored and shown as problems; they cannot enable work the repository did not allow.

The record stores `auto_started` with `inside start rules`. Check `GET /v1/activity` or the
CSV record to see why it began. Intake does not repeatedly start the same job, and a
stopped job stays stopped until you run it. A chat's `factory_add` card only adds work;
a forge job with matching start labels can start later through intake. Publishing still
requires a person. The day rail and restrictions on writing strangers' work still apply.

## asked first — what does asked first mean, why didn't a labeled job start

A candidate outside the start rules needs `approval.kind: start`, with
“Approval needed: start · <reason>”. Reasons use label size, job budget and
explicit paths. `approval.matched` keeps the rule and fact. Approve waits five
seconds for Undo; only the assignee may approve. The matched reason remains
on the job as `asked_first_reason`, and Activity keeps `asked_first`.
Intake does not ask again. The question this approval answers is “Start this job?”.
Sending `yes` through the older answer door uses the same five-second delay.

Intake applies rules in code without an AI read. `never` globs match paths in
the issue, plan or changed files. `*` covers one segment and `**` any number.
`ask_first` path rules use `touches <glob>`, `touch <glob>` or `path:<glob>`.
An unrecognised sentence asks first; a model cannot clear it. Without a start
label nothing starts. An unreadable pipeline cannot start a job.

## size rules — measured PR lines, issue model or size labels

The v2 job field and automatic intake use `size:XS`, `size:S`, `size:M`,
`size:L` or `size:XL`. No label leaves size unset and a start candidate asks
first. Job budget is the actual pipeline default or the person's raised
limit; an AI estimate is never permission to start.

The older `size.pr_lines` and `size.issue` keys remain readable for the
terminal's legacy read and compatibility dry-run clients. They do not set
the v2 job's size or automatic intake decision.

## stopped job stays stopped — stop, run and go under automatic intake

A stopped job stays stopped until you run it. Its `stopped` event keeps intake from
starting it again, even if the start label remains or the factory server restarts.
Run (or go) starts it again through the person's door; intake never restarts a job
you stopped or asks to start it again.

## codeaf factory convert — convert Markdown to YAML

`codeaf factory convert [path]` reads a Markdown pipeline (default `.codeaf/factory.md`)
and prints YAML to stdout. Conversion warnings go to stderr. It does not write a file.
To save it, run `codeaf factory convert > .codeaf/factory.yaml` from the repository.
Review the warnings before committing: old checks and posts become prompts, and some
old step knobs cannot be represented. A section-wide Markdown `fixed` setting remains
in force when Markdown is read; saved YAML uses individual fixed steps instead, and
conversion warns about that difference. Conversion never adds start rules, so it cannot
turn on automatic intake. YAML takes precedence once committed on the default branch.

## factory people cards — avatar, handle, role, assigned jobs and questions

`GET /v1/people/{handle}` returns a person card: `handle`, GitHub `name`,
`avatar_url`, `html_url`, `role` (`member` or `outside`), optional `last_seen`,
`owns`, optional `assigned_now`, `waiting_on`, `questions`, and `machines`.
Contract is 2. Handles are lowercase; a leading `@` is accepted.

GitHub's public users API supplies profiles, including issue authors who never
signed in. Successful reads are cached for 24 hours, then checked with ETag.
Members and sessions supply role and last seen; the connected host is also a
member. Outside people have no last seen. Unknown last seen and zero counts
are omitted. `assigned_now` counts visible jobs assigned to this person except
shipped or dismissed. `waiting_on` counts those with a needs-you reason;
`questions` counts plain questions among them. Opening an issue does not assign
its author. The existing job reply/question routes are the way to ask; this
read creates no question.

`GET /v1/queue` includes `people: {handle: {name, avatar_url}}` for every handle
its visible jobs mention, including assignees, issue authors, approvers,
reviewers, answerers, comment authors, owners and live holders. Rows share that
map without fetching each profile. A profile outage keeps the queue readable
with member data or blank profile fields. An unknown person with no profile or
visible association answers 404 `person not found`; an invalid handle answers
422 `person handle must be a GitHub handle`.

## factory CODEOWNERS — Owns paths, planned files and changed-file owners

A card's `owns` is `[{repo, path}]`, with each declared CODEOWNERS pattern for
that handle qualified by a visible repository. The factory reads GitHub's default
branch, in this order: `.github/CODEOWNERS`, `CODEOWNERS`, `docs/CODEOWNERS`.
The first existing file wins. Files, absence and failures are cached until the
next poll. The job branch never supplies ownership.

`job.owners` lists sorted individual handles owning its planned files, source PR
files and worker-reported changed files. The last matching rule wins for each
file; a rule without owners clears earlier ownership. Directory patterns,
`*`, `**` and `?` are supported. Team and email owners are not expanded into
people; negation, bracket ranges and escaped patterns are skipped. An ownership
read failure leaves no ownership facts for that repository.

Viewer access applies before enrichment: cards count only visible jobs and list
only visible ownership. A partial viewer sees only machines holding a visible
job; hidden holds and idle machines are omitted. A viewer with no visible
repositories gets no ownership, counts or machines. A whole-floor viewer gets
that person's full roster with `busy`, `idle` or `offline`, using the same
heartbeat rule as `/v1/me`.

## Factory People — invited, active, away and operator

`GET /v1/people` is a team directory with `login`, `name`, `avatar_url`,
`you`, `operator`, `status`, `runners` and visible job counts. It returns no
People entries in solo mode. Status is `invited` before first sign-in,
`active` after sign-in and `away` after 15 minutes idle. Sessions are shown to
that person and the operator only. Nobody gains another person's runner or
approval authority by becoming operator.

`DELETE /v1/people/{login}/sessions` lets the operator end that person's
sign-ins and runner credentials. Their unfinished jobs stop and go to
Unassigned in Backlog. Their branches and Activity stay. Operator identity
comes from the host's GitHub login, not a claim sent by a client.

## Access links — Sign in with GitHub through an access link

The binary owns access links. The operator creates one with
`POST /v1/access/links`, optionally naming `login`, `name` and `expires_at`.
The response is `{factory,link}` with a `codeaf://factory/join` URL. Its
secret is in the fragment and returned only at creation. `GET /v1/access/links`
lists metadata without reusable secrets. A named link binds that GitHub login.

Use the link token at `/v1/auth/device` and `/v1/auth/device/poll`, then use the
returned factory session. A link without live GitHub sign-in cannot read jobs:
`Sign in with GitHub through an access link`.
`DELETE /v1/access/links/{id}` revokes the link and its person's execution.
`POST /v1/access/revoke-sessions` ends all sessions and unassigns their work.
The app confirms Revoke all sessions before calling the door.

## Factory Activity — actor, verb, object and CSV export

`GET /v1/activity` reads append-only events with `actor` (person, agent or
factory), `verb`, `object`, `detail`, optional `rule` and `cost`. Events cannot
be edited or deleted. Operator actions carry `actor.operator:true` and
`as operator` in their detail. Connecting repos, adding runners and making or
revoking links join the durable job decisions in this record.

Filter with `kind`, `person`, `on`, `when` (UTC date), or `since` (RFC3339).
`format=csv` exports exactly the selected events the caller can see. CSV keeps
the receipt columns and adds actor, verb, object, rule and as_operator.
Only the operator can change connected repositories with `PUT /v1/repos`.
An ordinary signed-in caller gets `only the operator can do this`.

## Add a runner — codeaf runner join

Run `codeaf runner join <url> [--name name] [--token session]` on another
computer. This saves the factory connection through the same door as
`codeaf attach`; new codeaf windows run that computer's worker loop. It does
not leave a daemon running after the command exits. Use the factory session
returned by GitHub sign-in. `CODEAF_FACTORY_BENCHES` sets the number of
execution slots, default 2. A team runner runs only its owner's assigned jobs.
