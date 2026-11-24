# A codeaf organization you host yourself

One `docker compose up` brings up everything an organization needs, on your own machine or
server: the **organization server** people join with their codeaf desktop app, the **AgentField**
control plane that routes work, and the reasoner nodes the factory sends work to, **SWE-AF** (builds an
issue into a pull request) and **PR-AF** (reviews one). Nothing in an image is a secret; model keys
come from your environment.

| Service | What it is | Published port | State (volume) | Runs as |
| --- | --- | --- | --- | --- |
| `codeaf` | the organization server, `codeaf factory serve` in team mode | `127.0.0.1:8787` | `codeaf-data` | uid 10001 |
| `agentfield` | the AgentField control plane (published image, pinned by digest) | `127.0.0.1:8080` | `agentfield-data` | uid 65532 |
| `runner` | the factory runner: a codeaf engine that registers the factory on AgentField as the node `codeaf-factory` | none | `runner-data` | uid 10001 |
| `swe-af` | SWE-AF, node `swe-planner`, dials AgentField over its node link | none | `swe-af-workspaces` | uid 10002 |
| `pr-af` | PR-AF, node `pr-af`, called by AgentField over HTTP | none | `pr-af-workspaces` | uid 10003 |
| `modelstub` | a scripted model, only with `--profile stub` (the keyless smoke test) | none | none | nonroot |

Every container starts as root only long enough to hand a freshly mounted volume to its user, then
drops to that user (`scripts/drop-root.sh`); nothing long-lived runs as root. Published ports listen on
127.0.0.1 until you change `CODEAF_BIND` / `AGENTFIELD_BIND` in `.env`.

## What you need

- Docker with Compose v2.24 or newer (`docker compose version`), about 4 GB of free memory and 5 GB of disk.
- For anyone who is not on the same machine: **HTTPS**. An organization server takes a password only over
  HTTPS (or from its own machine), so put a proxy that serves HTTPS in front of port 8787 (see "Other people").
- For real work: an OpenRouter key (`OPENROUTER_API_KEY`) and, to let the nodes clone private repositories and
  open pull requests, a GitHub token (`GH_TOKEN`). The smoke test needs neither.

## Start it

```sh
cd deploy/org
./setup.sh                      # writes .env with a fresh AgentField key and organization token (once)
$EDITOR .env                    # optional: OPENROUTER_API_KEY=..., GH_TOKEN=..., CODEAF_ORG_NAME=...
docker compose up -d --build    # the first build takes several minutes; later starts take seconds
docker compose ps               # every service says "healthy"
```

The first build downloads the pinned codeaf release (checked against its sha256) and builds SWE-AF and PR-AF
from pinned commits of their public repositories.

## Claim the organization, invite a second person

```sh
./operator-link.sh              # prints the operator access link (a secret; it works once)
```

On a laptop the link names `http://[::]:8787`. That is the address the server gives itself (its listen address) and a
link is only accepted with exactly that address, so it is not rewritten. A connection to `[::]` reaches the local
machine on Linux and macOS, so the desktop app on the same machine can use it; Windows cannot, and nobody on another
machine can. For them, serve the organization on a public HTTPS name (see "Other people") and the link names that.

1. Open that `codeaf://factory/join?operator=true...` link in the codeaf desktop app (or paste it into
   **Join an organization...**). Choose a username and a password. Whoever uses the link first is the operator,
   so keep it to yourself until you have.
2. From **People**, the operator makes an **invite link** for the second person. They paste it into
   **Join an organization...** on their own computer and choose their own username and password.
3. A codeaf client connects to this organization with the server's URL, which is inside the link: your public
   HTTPS name, or `http://[::]:8787` on the machine running Compose.

If the operator link is lost before anyone used it, `docker compose restart codeaf` prints a fresh one.

### Other people: HTTPS

Put any proxy that serves HTTPS in front of port 8787, then tell the server its public name and which proxy
to believe:

```sh
# .env
CODEAF_PUBLIC_DOMAIN=org.example.com          # no scheme
CODEAF_TRUSTED_PROXIES=172.16.0.0/12          # the proxy's address or range (Docker's default bridge range here)
CODEAF_BIND=127.0.0.1                         # the proxy runs on this machine; keep the port closed
```

For example Caddy, on the same host: `org.example.com { reverse_proxy 127.0.0.1:8787 }`. Links the server prints
then name `https://org.example.com`. Without `CODEAF_TRUSTED_PROXIES` the server sees every request as plain HTTP
and refuses passwords with "Passwords are only sent to this server over https".

## Real work

Put your keys in `.env` and `docker compose up -d`. `ORG_MODEL`, `SWE_DEFAULT_MODEL` and `PR_AF_MODEL` choose the
models; the defaults are inexpensive. A factory job runs on the pipeline a repository declares in
`.codeaf/factory.yaml` (a step of `kind: agentfield` with `target: swe-planner.build_issue` sends it to SWE-AF),
and SWE-AF pauses at its plan so the owner can approve it or ask for changes.

## The keyless smoke test

```sh
./smoke.sh
```

It restarts the stack against the scripted stub (so nothing is spent, whatever keys `.env` holds) and checks,
printing `PASS` or `FAIL` for each:

- every service is healthy; AgentField, SWE-AF, PR-AF and the factory are registered; PR-AF answers a call;
- the operator link claims the organization once, a second person joins by invite, roles are enforced;
- a factory job reaches SWE-AF through AgentField with **strict run-owner proofs on**, its plan question reaches
  the owner, "ask for changes" resumes the run, and rejecting the plan ends it; AgentField's proof counters agree;
- after `docker compose restart`, the nodes register again and AgentField, the runs and the people are all still there.

The stub's answers are minimal schema-valid text, so the "work" is not real. `docker compose up -d` afterwards
puts your own keys back.

## Hosted chats (optional)

By default a person's chat runs on their own computer. With hosted chats on, a person can instead run a
chat on a machine of the organization's: `codeaf chat --org https://your-organization` opens it from their
terminal, the work goes on there, and it is still there when they close the terminal and come back. Nothing
here changes while it is off: with `CODEAF_HOSTED_URL` empty the organization server answers exactly as it did.

```
person's terminal --wss--> organization server --(shared secret)--> sessionhost --> one chat
 codeaf chat --org           signs the person in,                    one Linux user,
                             names them to the sessionhost           one folder, fenced commands
                             mints the chat's model access
```

The **sessionhost** (`sessionhost` in `compose.yaml`) is the one container that starts as root and stays root,
because it has to become a different Linux user for every chat. It is built from the same image as the others
(`CODEAF_ROLE=sessionhost` on a platform that cannot pass arguments). What it does for each chat:

- **A user and a folder of its own.** The chat runs as its own Linux user (numbers from 200000 up) in
  `/chats/people/<person>/<chat>/`, mode 0700, with `home`, `work` and `tmp` inside. Another chat's user
  cannot read, write, list or signal it, and the organization server's own data is not on this machine's disk.
  The folders above a chat (`/chats`, `people` and one per person, mode 0755) are readable by every chat's
  user on purpose: the saved-history helper syncs every folder above a new file, and a folder that could only be
  passed through made the first command of every new chat say "calls are not being sealed". The price is that a
  command can list the names of other chats (a hashed name per person, a random identifier per chat), never what
  is inside them.
- **A fence on every command**, switched on and not removable from inside the chat. If this machine cannot
  apply one, the chat's commands are refused, never run open; if it cannot switch users it starts no chats
  and says so (`codeaf sessionhost probe` prints what it measured; `ok: false` is a refusal).
  `CODEAF_SESSIONHOST_REQUIRE=uid` (default `uid+fence`) lets an operator whose kernel has no fence run on the
  user switch alone; it says so once at start and in every chat's `warning`, and it is not a default. The fence
  also scopes abstract Unix sockets (they belong to no user, so nothing else keeps one chat from connecting to
  another's), which needs Landlock ABI 6. **Requires Linux 6.12 or newer; on older kernels set
  `CODEAF_SESSIONHOST_REQUIRE=uid`.** Without it an older kernel starts no chats and the refusal at start names that
  exact setting (`codeaf sessionhost probe` prints the same reason); with it the chats run on the user switch alone, with
  no limit on what their commands reach, the port limit below included. Chats also start with `umask 077`, because
  `/tmp` and `/dev/shm` are shared by every chat on the machine; a chat can still open its own files up.
- **A port limit on the network** (`CODEAF_SESSIONHOST_CONNECT_PORTS`, default `443,80,22,9418`). A private network
  with no policies of its own (Railway's, and any Compose stack that puts a chat's machine beside other services)
  lets a chat's command connect to the node APIs, the model stub and anything else listening there. The fence
  therefore carries a Landlock TCP-connect allowlist: a command may connect to those ports and to the port of the
  organization server's own address (the model door and git, taken from the chat's credentials), and every other
  port is refused by the kernel with `permission denied` and one plain line saying so. `none` allows the organization
  server's port alone. **It filters by port, not by address**: a hostile service listening on an allowed port is still
  reachable, a service of the chat's own on another port (a dev server on 3000) is not, and AgentField, the nodes and the
  stub must keep their own sign-in because a service that shares an allowed port is open to a chat. Keep AgentField's
  port (8080), the nodes' (8003, 8007), the model stub's (8890) and the sessionhost's own (8790) out of the list. Needs
  Landlock ABI 4 (Linux 6.7); a chat whose kernel cannot do it is refused, never run with the network open.
- **No keys.** The sessionhost holds no organization secret except the one it shares with the organization
  server, no AgentField key, no GitHub token and no model key, and a chat holds none of a person's keys either. A
  chat reaches a model only through the organization server's `/v1/model/` door with a bearer the server minted for
  that chat (seven days, ended at once on stop or delete, kept as a hash in the organization's data so a restart of
  the server does not end it); the door adds the real key itself, metered into the person's spend. Git goes the same
  way: the chat's `git clone`/`push` of an allowed host is rewritten to the organization server's
  `/v1/hosted/git/` door, which adds the person's own GitHub access to each request, so the chat never holds a GitHub
  token (a credential helper, `codeaf git-credential`, hands git only the chat's own bearer, and only to that one
  address).
- **Limits.** `CODEAF_SESSIONHOST_MAX_RUNNING` chats at once (default 20) and `CODEAF_SESSIONHOST_MAX_PER_PERSON`
  per person (default 3). A chat counts while it has a process; a person over their limit can still make a
  chat but cannot start a third and reads "You already have N chats running; stop one first."
  A chat that is stopped or deleted leaves no process behind, including ones that detached themselves.
- **A turn goes on without the client.** The chat's engine keeps its conversation behind a socket in the chat's
  own home, so closing the client in the middle of a turn loses nothing and the next attach shows the result. A
  socket path has a hard length ceiling, so `CODEAF_SESSIONHOST_DATA` (default `/chats`) must be short: the
  sessionhost refuses to start with a folder that leaves no room, and says how many characters it may have.
- **Only the owner drives, and a chat is private until it is shared.** The organization server names the signed-in
  person to the sessionhost, which knows only owners, so every call it makes for a chat is made as the chat's owner;
  whether someone else may reach the chat at all is decided by the organization's access core first (below, "Sharing,
  guests and bots (P2)"). A chat nobody has shared answers "There is no such chat.", word for word as an unknown one does.

The five capabilities the container is given (`cap_drop: [ALL]`, then `cap_add`), and why:

| Capability | Why the sessionhost needs it |
| --- | --- |
| `SETUID`, `SETGID` | become the chat's user and group |
| `CHOWN` | hand over one freshly made, empty folder to that user (never an existing one) |
| `KILL` | end every process of a stopped chat's user |
| `SETPCAP` | empty the chat's capability set before it starts (it cannot be done without this one) |

The supervisor narrows itself to these five when it starts, whatever the platform gave root.

What this does NOT do, plainly: whoever is root on the machine (the operator, or the platform) can read every
chat, and the volume is not encrypted by this stack; a chat can reach the internet and any service that listens on
an allowed port of the private network (the limit is by port, not by address), so each service on that network
must keep its own sign-in (the node APIs of AgentField's reasoner nodes do not yet ask for one: an agentfield
follow-up); the model access a chat holds is its own scoped bearer, and the chat's own shell can read the bearer
of the chat's engine process and of the private file git's helper reads; the git door is not limited to one
repository, because the person's own GitHub access decides what they reach (GitHub App installation tokens per
repository are a next step); and a person's own model key counts toward both spending limits like the
organization's. Each of those is a next step, not a hidden property.

### Keys, spend and git (P1)

- **The real model door.** `CODEAF_HOSTED_MODEL_UPSTREAM` (OpenRouter's when empty) is called with
  `CODEAF_HOSTED_MODEL_KEY`, or the key the organization server already holds for model calls, or no key at all for a
  keyless stub. The key never leaves the organization server. Every call is metered to the cent into the signed-in
  person's My space; a chat stops at `CODEAF_HOSTED_SESSION_CAP_USD` (default 5) and a person at the monthly limit the
  operator sets in Settings (`PUT /v1/myspace/limit`, default 20). A refused message is a plain sentence and makes no
  upstream call.
- **People's own keys.** `PUT /v1/hosted/keys/openrouter` and `.../github` with a person's sign-in store their own key
  or GitHub access, sealed with a data key of that person's own, itself sealed by `CODEAF_SERVER_KEY` (64 hex
  characters; `./setup.sh` makes it) with AES-256-GCM, in `person-keys.json` beside the server's other data. The
  server key is held by the `codeaf` service only. **Back it up with `.env`**: a server that finds keys sealed with a
  different server key, or people's keys and no server key, refuses to start with a sentence and touches nothing;
  with no server key at all, personal keys are refused with a 409 and chats use the organization's key. Never the
  other way round: a person's own key that cannot be opened pauses their chat; it is never replaced by the
  organization's. A key the provider rejects (a 401 or 403) is a plain refusal in the chat's transcript and is not
  retried: the organization's key reads "The organization's model access was refused by its provider, so this chat
  paused. Ask the organization's operator to check the model key.", a person's own key reads "Your own model key was
  refused by its provider, so this chat paused. Replace it or remove it, then send your message again." The
  server's log gets one line naming the cause (the setting `CODEAF_HOSTED_MODEL_KEY`, or the person), never the
  key, and repeats at most every 30 minutes while the key stays refused.
- **Git.** `CODEAF_HOSTED_GIT_HOSTS` (default `https://github.com`) are the hosts a chat's git reaches through the
  server. One audit line per operation goes to `hosted-audit.jsonl` (`GET /v1/hosted/audit` shows a person their own).

### Sharing, guests and bots (P2)

A hosted chat is a `session` resource of the organization's access core, registered when it is made: the owner is
the person who made it (for a bot, the person who made the bot, with `created_by` the bot), and it is **private
unless the policy's default for `session` says otherwise**. The list, the chat's own routes and the event stream
all ask the same core, so another person's list and live updates never carry it, and the operator has no more
right to a private chat than anyone else (the policy's `operator.sees_private` is false by default).

- **The list.** `GET /v1/hosted/sessions` answers `sessions` (the caller's own chats, the rows P1 gave, so the
  client's `--org` pick is unchanged), `shared` (chats of other people the caller may open: `id`, `owner`, `role`,
  `access`, `state`, `created`, `last_active`, and nothing about the model access or warnings; the operator's rows,
  only for a chat the policy keeps from being private, hold the same without `role` and have `access: operator`) and
  `hosting` (`local_sessions`, below).
- **Sharing** is the generic route: `POST /v1/resources/session/<chat>/grants` with a person, a team or the
  organization and a role, and `DELETE .../grants/<grant>`. Share, revoke and a change of the policy publish
  `resource.changed` events to the audience as it stood before and as it stands after.
- **A guest's wire.** An owner (by handle, holding `run`) is attached by the same byte-for-byte proxy as in P1.
  Anyone else who may view the chat gets a wire the organization's server terminates itself: it reads each call the
  guest's window sends, checks its method against a table in `internal/factory/serve/hosted_guest_methods.go` and
  forwards only what the guest's verbs allow, re-encoded so that what is forwarded is exactly what was checked.
  `view` watches and reads; `steer` adds `Steer` and `Interrupt`; `answer` adds `ResolveQuestion`; `run` adds the calls
  that put words into the conversation or run a command the chat's own way (`Submit`, `SubmitBash`, `SubmitImage`,
  `SubmitFiles`, `FollowUp`, `UnqueueFollowUp`, `ReplaceQuestion`, `Typing`, `Take`); **every other method is the
  owner's alone**, whatever role the guest holds. Whether a non-owner may send prompts at all is therefore a policy
  edit, not code: the default `edit` and `full` roles hold `run`, `view` and `comment` do not, and taking `run` out of
  a role (`PUT /v1/org/policy`) refuses the next prompt of everyone who held it only through that role, within a
  heartbeat. The organization-wide default role is `view` (the policy's `org_role`); the generic share route has no default
  role, the caller names it. **A person given `run` acts with the owner's GitHub identity, files and spending limits**: the
  chat runs in the owner's folder, as the owner's user, with the owner's model access, git access and caps.
  The table is an allow-list: a method added to the wire later is owner-only until someone chooses otherwise, and a
  test fails until it is classified. A refused call is answered in place with one of four sentences (`This chat is
  shared with you. Only its owner can do that.`; `You can watch this chat, but you were not given permission to send
  messages in it. Ask its owner.`; `... to steer it. Ask its owner.`; `... to answer its questions. Ask its owner.`)
  and never reaches the chat. The guest's hello is rewritten so it cannot launch, resume or take over anything and
  does not take the keyboard from the owner on arrival.
- **Access taken away.** Every `CODEAF_HOSTED_HEARTBEAT` (default `5s`, `1s` to `1m`) the server re-reads the
  guest's sign-in, the grants and the policy; when the right is gone it writes `Your access to this chat was
  removed.` and closes the wire. A role changed in between takes effect on the next call. For a bot the same check
  also re-reads that the token is live and that its creator is still on the team.
- **Money.** A guest's attach reaches the chats' machine as the owner and the model access it holds is the owner's, so
  every call is the owner's: the owner's own key if stored, the organization's otherwise, against the owner's chat cap
  and monthly limit. The spend row also records who last steered or answered through this server in a new `asker`
  column (`ALTER TABLE spend ADD COLUMN`, added on first start, harmless on an existing database). It is
  approximate by design: a model call is credited to whoever last put words into the chat through this server, and
  the owner's own attach puts it back to the owner. Each message a guest sends writes one `kind: "prompt"` line in
  the owner's `GET /v1/hosted/audit` (who, never what), and every `git` line carries `asker` when it was not the owner.
- **Bots.** A bot token may use `/v1/hosted/sessions` (and not `/v1/hosted/keys` or `/v1/hosted/git`, which answer
  `a bot cannot use this yet`): it makes, lists, opens, stops and deletes exactly what its creator may, narrowed by
  its scope, decided again at every request. A bot's chat is owned by the person who made the bot, and `created_by`
  records the bot, as for any other thing a bot makes.
- **The hosting policy.** `PUT /v1/org/policy` takes `"hosting": {"local_sessions": "allow|warn|refuse",
  "max_session_cost_usd": 0}`. Empty `local_sessions` means `refuse` for an organization that has chats' machine and
  `allow` for one that does not; the list reports the value that applies, with `words` to show for `refuse` (that
  chats do not start on this computer and that the personal account is the way to work here) and for `warn`. An
  organization is its own account in the app, separate from the person's personal one: inside it every chat is
  hosted and there is no local escape; switching to the personal account is the escape. A positive `max_session_cost_usd` replaces
  `CODEAF_HOSTED_SESSION_CAP_USD` for every chat. Nothing in the client acts on `local_sessions` yet: this release
  reports and stores it.

Known limit of this version: a guest's `Steer` or prompt is refused by the chat itself, in the chat's own words about
the keyboard, while the owner's window holds the keyboard, because the guest's window does not take it on arrival; it
goes through when the owner has no window open, or after the guest sends `Take` (allowed with `run`, and the owner's
window is told). `Interrupt` and `ResolveQuestion` are not affected. A tool-use approval stays the owner's, so a
command that needs one waits for the owner. The proper fix for both is a guest class inside the engine itself, which
is a change to the wire's own protocol and not made here.

### Turning it on

1. `./setup.sh` writes `CODEAF_HOSTED_SECRET` (64 hex characters, never printed) into `.env`; an `.env` from
   before this feature gets one appended, once.
2. In `.env`: `COMPOSE_PROFILES=hosted`, `CODEAF_HOSTED_URL=http://sessionhost:8790`, the organization's model
   endpoint in `CODEAF_HOSTED_MODEL_UPSTREAM` (for OpenRouter `https://openrouter.ai/api/v1`, which is also the default
   when empty) and its key in `CODEAF_HOSTED_MODEL_KEY`, and the model chats use in `CODEAF_SESSIONHOST_MODEL`.
   `./setup.sh` also writes `CODEAF_SERVER_KEY`, which seals people's own keys: keep a copy.
   `CODEAF_HOSTED_MODEL_URL` is the address chats use to reach the organization server's model door
   (`http://codeaf:8787/v1/model` by default).
3. `docker compose up -d`. The image must hold a codeaf that has `codeaf sessionhost`; until a release does, build
   from a local binary: `cp ../../bin/codeaf codeaf-local` then `CODEAF_BINARY_FROM=local docker compose build codeaf sessionhost`.
   (`codeaf-local` is ignored by git; `CODEAF_BINARY_FROM` defaults to `fetch`, the pinned release.)

The sessionhost sits on a network of its own, shared only with `codeaf`, so a chat's commands cannot reach
AgentField, the nodes, the runner or the model stub. On a platform with no such network (Railway), the port limit above
is what keeps them out. Its API (`/v1/sessions` ...) wants the shared secret and a
person's name from the organization server, and answers anything else with an empty `401`.

`SMOKE_HOSTED=1 ./smoke.sh` runs everything above against the scripted stub (so it spends nothing): two people
run chats; the test checks that they cannot see each other's files, processes or environment, that no secret
is in a chat's shell, folder or process list, that a chat cannot reach the other services (and that the kernel refuses
a port nobody allowed), that only the owner can attach, that closing the client mid-turn loses nothing, that a stopped
chat leaves no process, and that a restart of the sessionhost leaves the files in place. It also checks, against the
stub model (each call reports it cost a cent) and a stub git host: the upstream sees only the organization's key, spend
is exact to the cent, the monthly and per-chat limits refuse in plain words, a person's own key is used for them alone
and sits sealed on disk, clone and push work through the organization with one audit line each and no key or GitHub
access in any chat folder, process environment or argument list, a chat's access still works after the organization
server restarts and ends the moment the chat is stopped, and the first attach after a restart of the chats' machine
needs no retry. P2 adds a third person and a bot: a chat is invisible to others in the list, at every door and on the
event stream until it is shared; a guest with `view` watches the owner's turn live and every message, steer and
command it tries is refused in the exact sentence and never reaches the chat; with `edit` (which holds `run`) its
message reaches the chat, runs as the owner and is recorded against it, it may interrupt a running turn, and the
owner's approvals and settings stay refused; taking `run` out of the role by a policy edit refuses its next message;
taking the access away ends its open window within the heartbeat;
the operator sees only metadata of a chat the policy keeps from being private and never its wire; a bot sees
exactly what its creator sees, starts chats that are its creator's alone, and loses a shared chat the moment its
creator does; the hosting policy is reported, changed only by the operator and refused when it is not one of the
three words. It prints PASS or FAIL per check. A live check against a real provider is the one step it does not do.

On Railway the sessionhost is a sixth service from the same image: `CODEAF_ROLE=sessionhost`, a volume at
`/chats`, no public domain, `CODEAF_SESSIONHOST_SECRET=${{codeaf.CODEAF_HOSTED_SECRET}}`, and on `codeaf`
`CODEAF_HOSTED_URL=http://${{sessionhost.RAILWAY_PRIVATE_DOMAIN}}:8790` and
`CODEAF_HOSTED_MODEL_URL=http://${{codeaf.RAILWAY_PRIVATE_DOMAIN}}:8787/v1/model`. Railway runs services as root with
a fixed capability set; the sessionhost takes five from it, and `codeaf sessionhost probe` run in that service
says whether the fence works there (it has Landlock but not the namespace fence, so the mode there is the
user switch plus Landlock).

## State, backups and upgrades

State lives in five named volumes (`docker volume ls`, prefix `codeaf-org_`); `docker compose down` keeps them,
`docker compose down -v` deletes them. A backup of one volume:

```sh
docker run --rm -v codeaf-org_codeaf-data:/d -v "$PWD":/b busybox tar czf /b/codeaf-data.tgz -C /d .
```

Everything is pinned: the codeaf release (`CODEAF_RELEASE` and its two sha256 values at the top of
`codeaf.Dockerfile`, `swe-af.Dockerfile` and `pr-af.Dockerfile`), the SWE-AF and PR-AF commits (`SWEAF_REF`,
`PRAF_REF`) and the AgentField image (`AGENTFIELD_IMAGE`, tag and digest together in `agentfield.Dockerfile`).
To upgrade, change the pin, then `docker compose up -d --build`. Changing the line in the file is what makes
Docker rebuild that layer; a loose version would keep restoring the cached one.

## Secrets

No secret is baked into an image. The AgentField key, the organization token and the model keys come from `.env`
or your environment. A service that is given a key as a mounted file can use `AGENTFIELD_API_KEY_FILE`,
`OPENROUTER_API_KEY_FILE` or `GH_TOKEN_FILE` instead. Treat `.env` like a password file (`setup.sh` writes it
mode 600); it is ignored by git and by the image build.

## Railway

The same Dockerfiles run on Railway as one project: five services, a volume on each, private networking
between them, and a public domain on `codeaf` only. Nothing needs Compose there; each service builds its own
Dockerfile from this folder. (A one-click Railway template of this is the next step; until then it is built by hand,
below, and the POC that proved it ran the keyless smoke test on itself.)

Create an empty project, then one service per row. For the four built services choose **GitHub repo**, set
**Root Directory** to `deploy/org`, and set the variable `RAILWAY_DOCKERFILE_PATH` (or the service's
**Dockerfile Path**) to the Dockerfile in the row. Add a volume at the mount path in the row. Reference variables
(`${{agentfield.RAILWAY_PRIVATE_DOMAIN}}`) are Railway's way of naming another service; keep them as written.

| Service | Dockerfile | Volume | Variables |
| --- | --- | --- | --- |
| `agentfield` | `agentfield.Dockerfile` | `/data` | `AGENTFIELD_API_KEY=${{secret(64, "abcdef0123456789")}}`, `AGENTFIELD_HOME=/data`, `AGENTFIELD_CONFIG_FILE=/dev/null`, `AGENTFIELD_PORT=8080`, `PORT=8080`, `AGENTFIELD_STORAGE_MODE=local`, `AGENTFIELD_STORAGE_LOCAL_DATABASE_PATH=/data/agentfield.db`, `AGENTFIELD_STORAGE_LOCAL_KV_STORE_PATH=/data/agentfield.bolt`, `AGENTFIELD_RUN_OWNER_REQUIRE_PROOF=true`, `AGENTFIELD_TELEMETRY_ENABLED=false`. Health check path `/api/v1/health`. |
| `codeaf` | `codeaf.Dockerfile` | `/data` | `CODEAF_ROLE=server`, `PORT=8787`, `CODEAF_ORG_NAME=...`, `CODEAF_PUBLIC_DOMAIN=${{RAILWAY_PUBLIC_DOMAIN}}`, `CODEAF_TRUSTED_PROXIES=100.64.0.0/10`. Generate a public domain for this service only, target port 8787. |
| `runner` | `codeaf.Dockerfile` | `/data` | `CODEAF_ROLE=runner`, `AGENTFIELD_URL=http://${{agentfield.RAILWAY_PRIVATE_DOMAIN}}:8080`, `AGENTFIELD_API_KEY=${{agentfield.AGENTFIELD_API_KEY}}`, `ORG_MODEL=...`, `OPENROUTER_API_KEY=...`. |
| `swe-af` | `swe-af.Dockerfile` | `/workspaces` | `NODE_ID=swe-planner`, `AGENTFIELD_SERVER=http://${{agentfield.RAILWAY_PRIVATE_DOMAIN}}:8080`, `AGENTFIELD_API_KEY=${{agentfield.AGENTFIELD_API_KEY}}`, `SWE_HITL_NATIVE=1`, `SWE_FENCE=off`, `SWE_PRO_ENGINE=0`, `SWE_DEFAULT_MODEL=openrouter/...`, `OPENROUTER_API_KEY=...`. |
| `pr-af` | `pr-af.Dockerfile` | `/workspaces` | `NODE_ID=pr-af`, `PORT=8007`, `AGENTFIELD_SERVER=http://${{agentfield.RAILWAY_PRIVATE_DOMAIN}}:8080`, `AGENTFIELD_API_KEY=${{agentfield.AGENTFIELD_API_KEY}}`, `AGENT_CALLBACK_URL=http://${{RAILWAY_PRIVATE_DOMAIN}}:8007`, `PR_AF_MODEL=...`, `OPENROUTER_API_KEY=...`. |
| `modelstub` (keyless only) | `modelstub.Dockerfile` | none | `STUB_DELAY=2s`; then on `runner`, `swe-af` and `pr-af` set `CODEAF_BASE_URL=http://${{modelstub.RAILWAY_PRIVATE_DOMAIN}}:8890/api/v1`, `OPENROUTER_API_KEY=stub-key`, and the model variables to `stub/scripted` (SWE-AF: `openrouter/stub/scripted`); on `swe-af` also `SMOKE_FIXTURE_REPO=/workspaces/smoke-fixture`. |

Two things differ from Compose. Railway's edge is an HTTPS proxy in front of `codeaf`, so
`CODEAF_TRUSTED_PROXIES=100.64.0.0/10` (the edge's range) is required: without it every password sign-in is
refused as plain HTTP. And there is no `docker exec`, so set `RUNNER_SELFTEST=1` on `runner` to have it run the
smoke test's control and job sections on itself at start (PASS or FAIL lines in its log; `RUNNER_SELFTEST=after`
runs the after-a-restart section instead). The operator link is in the `codeaf` deploy log.

## Known limits

- The runner is a small Python host of the codeaf engine (`scripts/runner.py`), because `codeaf factory serve`
  does not itself register the factory on AgentField. When codeaf ships a headless runner this goes away.
- The organization keeps no canvases: the server needs a `canvas` program beside it, which this stack does not
  carry. It starts, says so in its log, and leaves the canvas routes off.
- The server names itself by its listen address, so without `CODEAF_PUBLIC_DOMAIN` its links say `http://[::]:8787`,
  which only the same Linux or macOS machine can use. A real deployment serves HTTPS on a name.
- GitHub sign-in (`CODEAF_GITHUB_CLIENT_ID`) and invitation mail (`CODEAF_SMTP_URL`) are optional and untested here;
  username and password is the default way in.
- One replica of each service; no high availability.
