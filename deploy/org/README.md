# A codeaf organization you host yourself

New to organizations? Start with [docs/organizations.md](../../docs/organizations.md); this page is the operator's
walkthrough for running one yourself.

**Contents**

- [What it runs](#what-it-runs)
- [What you need](#what-you-need)
- [Start it](#start-it)
- [Claim the organization, invite a second person](#claim-the-organization-invite-a-second-person) (and [HTTPS for other people](#other-people-https))
- [Real work](#real-work)
- [The keyless smoke test](#the-keyless-smoke-test)
- [Hosted chats (optional)](#hosted-chats-optional): [keys, spend and git](#keys-spend-and-git-p1), [sharing, guests and bots](#sharing-guests-and-bots-p2), [turning it on](#turning-it-on)
- [State, backups and upgrades](#state-backups-and-upgrades)
- [Secrets](#secrets)
- [Published images](#published-images) (and the leak gate)
- [Railway](#railway)
- [Known limits](#known-limits)

## What it runs

One `docker compose up` brings up everything an organization needs, on your own machine or
server: the **organization server** people join with their codeaf desktop app, the **AgentField**
control plane that routes work, and the reasoner nodes the factory sends work to, **SWE-AF** (builds an
issue into a pull request) and **PR-AF** (reviews one). Hosted chats (the **sessionhost**, below) are an
opt-in profile of the same file. Nothing in an image is a secret; model keys come from your environment.

| Service | What it is | Published port | State (volume) | Runs as |
| --- | --- | --- | --- | --- |
| `codeaf` | the organization server, `codeaf factory serve` in team mode | `127.0.0.1:8787` | `codeaf-data` | uid 10001 |
| `agentfield` | the AgentField control plane (published image, pinned by digest) | `127.0.0.1:8080` | `agentfield-data` | uid 65532 |
| `runner` | the factory runner: a codeaf engine that registers the factory on AgentField as the node `codeaf-factory` | none | `runner-data` | uid 10001 |
| `swe-af` | SWE-AF, node `swe-planner`, dials AgentField over its node link | none | `swe-af-workspaces` | uid 10002 |
| `pr-af` | PR-AF, node `pr-af`, called by AgentField over HTTP | none | `pr-af-workspaces` | uid 10003 |
| `sessionhost` | the machine hosted chats run on, only with `--profile hosted` (or `COMPOSE_PROFILES=hosted`); see "Hosted chats" | none | `sessions-data` | root, with five capabilities |
| `modelstub` | a scripted model, only with `--profile stub` (the keyless smoke test) | none | none | nonroot |
| `gitstub` | a scripted git host, only with `--profile gitstub`; `smoke.sh` turns it on with the hosted part and makes the token it will not start without | none | none | root (a test stub) |

A plain `docker compose up` starts the first five; the other three need their profile.
Apart from the `sessionhost` and the `gitstub` test stub, every container starts as root only long enough to hand a
freshly mounted volume to its user, then drops to that user (`scripts/drop-root.sh`, or the control plane's own
entrypoint); the `sessionhost` stays root on purpose, because it has to become a different Linux user for every chat
(see "Hosted chats"). Published ports listen on 127.0.0.1 until you change `CODEAF_BIND` / `AGENTFIELD_BIND` in `.env`.
Ports 8787 (`codeaf`) and 8080 (`agentfield`) must be free on the host: if one is taken, `docker compose up` stops with
`port is already allocated` and leaves the services it had not reached created but not started. Set `CODEAF_PORT` or
`AGENTFIELD_PORT` in `.env` (the template lists both) and run it again; the services reach each other over Compose's own
network, so AgentField's host port is only for you to look at it.
State is in six named volumes, one per row that names one. There are two networks: `default`, which every service
but the `sessionhost` is on, and `hosted`, which only `codeaf` and the `sessionhost` share.

## What you need

- Docker with Compose v2.24 or newer (`docker compose version`), `git`, about 4 GB of free memory and 5 GB of disk, and
  ports 8787 and 8080 free on the host (see "What it runs").
- For anyone to sign in with a password, **you on the machine running Compose included**: **HTTPS**. An organization server
  takes a password only over HTTPS or from its own machine, and a request through Docker's published port does not count as
  its own machine, so put a proxy that serves HTTPS in front of port 8787 (see "Other people", and "Trying it on one machine").
- For real work: an OpenRouter key (`OPENROUTER_API_KEY`) and, to let the nodes clone private repositories and
  open pull requests, a GitHub token (`GH_TOKEN`). The smoke test needs neither.

## Start it

The files are this repository: the images are built from your checkout, so clone it first (the installer on the front page
puts only the `codeaf` program on a computer, not these files).

```sh
git clone https://github.com/Agent-Field/codeaf
cd codeaf/deploy/org
./setup.sh                      # writes .env with fresh secrets: AgentField key, organization token, hosted-chats secret, server key (once)
$EDITOR .env                    # optional: OPENROUTER_API_KEY=..., GH_TOKEN=..., CODEAF_ORG_NAME=...
docker compose up -d --build    # the first build takes several minutes; later starts take seconds
docker compose ps               # every service says "healthy" (the scripted model, if you turn it on, says only "Up")
```

The first build compiles codeaf from this checkout (several minutes; the repository root is the build context) and builds SWE-AF
and PR-AF from pinned commits of their public repositories. codeaf is built from source because hosted chats need
`codeaf sessionhost` and no release has it yet. If you leave hosted chats off and want to skip the compile,
`CODEAF_BINARY_FROM=fetch docker compose up -d --build` downloads the pinned release instead (checked against its sha256);
`compose.yaml` says how to tell when `fetch` can be the default again.

### Without a model key

To see the whole organization work before you spend anything, point every model call at the scripted model (profile `stub`),
which answers a message with `STUB noted: <your message>`. Add these to `.env` (they are the values of `.env.stub`, plus the
hosted ones of `.env.stub.hosted` that matter for a first chat). Add them at the end of `.env` (the last line for a key wins),
then `docker compose up -d --build`:

```sh
COMPOSE_PROFILES=hosted,stub
CODEAF_HOSTED_URL=http://sessionhost:8790
CODEAF_HOSTED_MODEL_UPSTREAM=http://modelstub:8890/api/v1
CODEAF_HOSTED_MODEL_KEY=stub-hosted-org-key-0001
CODEAF_SESSIONHOST_MODEL=stub/scripted
CODEAF_BASE_URL=http://modelstub:8890/api/v1
OPENROUTER_API_KEY=stub-key
ORG_MODEL=stub/scripted
SWE_DEFAULT_MODEL=openrouter/stub/scripted
PR_AF_MODEL=stub/scripted
```

Put your own keys back and drop `stub` from `COMPOSE_PROFILES` when you are done. `./smoke.sh` is a different thing: it runs a
scripted check of the stack and uses the operator link up itself (see "The keyless smoke test").

## Claim the organization, invite a second person

```sh
./operator-link.sh              # prints the operator access link (a secret; it works once, and for 24 hours)
```

The link names the address the server knows itself by, and that decides who can use it:

- **With `CODEAF_PUBLIC_DOMAIN` set and an HTTPS proxy in front** (see "Other people: HTTPS") the link names
  `https://<your name>` and works from any computer. That is how an organization is run, and the setup in which a password
  sign-in is accepted.
- **Without it** the link names `http://[::]:8787`, the server's own listen address, which a link is only accepted with.
  Do not use it, even on the machine running Compose. The published port listens on 127.0.0.1 only, and `[::]` did not
  connect to it when this was tried. Over `http://127.0.0.1:8787` the server answers, but it refuses a password
  (`Passwords are only sent to this server over https.`): Docker hands a request to the container from its bridge, not from
  loopback, so the server does not take it for its own machine. For a trial on one computer, give the stack a name and an
  HTTPS proxy as in "Trying it on one machine".

`./operator-link.sh` prints a link only while the server says it can still claim the organization. Once the organization is
claimed, or the link is older than 24 hours, it says so and prints nothing. If nobody has claimed it yet,
`docker compose restart codeaf` prints a fresh link and retires the older ones.

1. Open that `codeaf://factory/join?operator=true...` link in the codeaf desktop app (or paste it into
   **Join an organization...**). Choose a username and a password. Whoever uses the link first is the operator,
   so keep it to yourself until you have. Without the desktop app, the same thing is one request to the server:
   see "From a terminal" in [docs/organizations.md](../../docs/organizations.md#from-a-terminal).
2. From **People** (Settings, Organizations, your organization), the operator uses **Invite someone** for the
   second person. Without `CODEAF_SMTP_URL` nothing is emailed and the button reads **Copy an invite link**; the link
   works for 7 days and for one person. They paste it into **Join an organization...** on their own computer and
   choose their own username and password. (From a terminal: the same page, "From a terminal".)
3. A codeaf client connects to this organization with the server's URL, which is inside the link: your public
   HTTPS name.

**A request from the server's own computer is not trusted.** An organization server never takes a request to be a
person because of where it came from: a request from the server's own computer that carries no sign-in, token or bot
token is refused with a 401, like one from anywhere else (`/v1/about` and the sign-in routes stay open), and a request
that came through a proxy is never counted as local unless a proxy listed in `CODEAF_TRUSTED_PROXIES` names a
loopback caller. A script on the server's machine must send a token. Solo servers (a desktop window's own) do trust
their own computer; a team server does so only when its host config file says `loopback_trust: true`. The file is
`<codeaf home>/factory/config.yaml`, or the one `codeaf factory serve --config` names. **In this stack the key cannot
be set:** `scripts/serve.sh` writes that file from the environment on every start (`/data/codeaf/factory/config.yaml`
in the `codeaf` container) and has no variable for `loopback_trust`, so the server here always runs with it off, and a
line added by hand is replaced at the next start. Nothing in the stack needs it on: the health check treats any HTTP
answer, a 401 included, as up, and the smoke test signs in.

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
and refuses passwords with "Passwords are only sent to this server over https". Check it from outside:
`curl https://org.example.com/v1/about` must say `"scheme": "https"`. With `CODEAF_TRUSTED_PROXIES` set the server
says nothing about plain HTTP at start: it speaks plain HTTP to the proxy, and you told it so. Without it, a server
listening on a non-loopback address logs `warning: serving plain HTTP on [::]:8787 ...` at every start, because then
nothing in front of it is known to serve HTTPS.

### Trying it on one machine

A trial needs the same three things a real name does: a name, an HTTPS proxy on it, and the proxy's certificate trusted by
whatever connects. `localhost` is refused as `CODEAF_PUBLIC_DOMAIN` (no certificate authority issues one for it), so use a
name with a dot that resolves to 127.0.0.1: every name under `localtest.me` does in public DNS (offline, put one in
`/etc/hosts`). Caddy can run as a container on Compose's network and make its own certificate authority. The default project
is `codeaf-org` (`name:` in `compose.yaml`); `docker network ls` shows the network if you set another.

```sh
# .env
CODEAF_PUBLIC_DOMAIN=codeaf.localtest.me
CODEAF_TRUSTED_PROXIES=172.16.0.0/12          # or the range `docker network inspect codeaf-org_default` prints

docker compose up -d --build
docker run -d --name org-https --network codeaf-org_default -p 127.0.0.1:443:443 caddy:2.10.2 \
  caddy reverse-proxy --from https://codeaf.localtest.me --to http://codeaf:8787 --internal-certs
docker cp org-https:/data/caddy/pki/authorities/local/root.crt ./caddy-root.crt     # the authority to trust
curl --cacert ./caddy-root.crt https://codeaf.localtest.me/v1/about                  # "scheme": "https"
```

Run `./operator-link.sh` after that, so the link names `https://codeaf.localtest.me`. On Linux the `codeaf` command trusts the
authority through `SSL_CERT_FILE=./caddy-root.crt`; a program that uses the system's trust store (the desktop app) needs the
file added to it, which was not tried here. Port 443 on the host must be free. `docker rm -f org-https` removes the proxy.

- **Browser chat connections.** A WebSocket upgrade with an `Origin` header is accepted only if the header names the host
  the request was sent to or the public name set with `CODEAF_PUBLIC_DOMAIN`. Otherwise the server returns `403` with
  `This page is from another site, so it cannot open a chat connection here.` Connections with no `Origin` header are accepted
  because the codeaf command line, the desktop app's engine and other non-browser clients send none. The sign-in cookie is
  `SameSite=Strict`. The origin check also protects against pages on sibling names of the same site. There is no setting to
  allow another origin because no client needs one.

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
printing `PASS` or `FAIL` for each. **Run it on a stack nobody has claimed**: it claims the organization itself, with
throwaway people, which uses the operator link up (afterwards `./operator-link.sh` has nothing to print), and on a stack
that is already claimed it stops at that step. To go from a smoke run to a real organization, `docker compose down -v`
(that deletes every volume) and start again:

- every service is healthy; AgentField, SWE-AF, PR-AF and the factory are registered; PR-AF answers a call;
- the operator link claims the organization once, a second person joins by invite, roles are enforced;
- a factory job reaches SWE-AF through AgentField with **strict run-owner proofs on**, its plan question reaches
  the owner, "ask for changes" resumes the run, and rejecting the plan ends it; AgentField's proof counters agree;
- after `docker compose restart`, the nodes register again and AgentField, the runs and the people are all still there.

The stub's answers are minimal schema-valid text, so the "work" is not real. `docker compose up -d` afterwards
puts your own keys back.

## Hosted chats (optional)

An organization is its own account in the desktop app, separate from a person's personal one. With hosted chats
on, every chat made in the organization's window runs on a machine of the organization's, never on the person's
computer, and the window starts, lists and reopens them; there is no local escape inside it, and the way to work on
one's own computer is to switch to the personal account, which never sees the organization's chats or files.
`codeaf chat --org https://your-organization` opens the same chats from a terminal. The work goes on there, and it is
still there when they close the window or terminal and come back. Nothing here changes while it is off: with
`CODEAF_HOSTED_URL` empty the organization server answers exactly as it did.

```
person's app or terminal --wss--> organization server --(shared secret)--> sessionhost --> one chat
 codeaf chat --org                  signs the person in,                    one Linux user,
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
  Every mode of `codeaf sessionhost reap` (stop, delete and the cleanup before a chat starts) also sweeps `/tmp`, `/dev/shm`,
  `/var/tmp` and the System V shared memory, message queues and semaphores of the chat's Linux user. It runs as that user
  without following links, so the next chat given that Linux user cannot read what the previous chat left there.
- **A port limit on the network** (`CODEAF_SESSIONHOST_CONNECT_PORTS`, default `443,80,22,9418`). A private network
  with no policies of its own (Railway's, and any Compose stack that puts a chat's machine beside other services)
  lets a chat's command connect to the node APIs, the model stub and anything else listening there. The fence
  therefore carries a Landlock TCP-connect allowlist: a command may connect to those ports and the ports used for the
  organization server's own address (the model door and git, taken from the chat's credentials), and every other TCP
  port is refused by the kernel with `permission denied` and one plain line saying so. `none` allows the organization
  server's port alone. **It filters by port, not by address**: a hostile service listening on an allowed port is still
  reachable, a service of the chat's own on another port (a dev server on 3000) is not, and AgentField, the nodes and the
  stub must keep their own sign-in because a service that shares an allowed port is open to a chat. Keep AgentField's
  port (8080), the nodes' (8003, 8007), the model stub's (8890) and the sessionhost's own (8790) out of the list. Needs
  Landlock ABI 4 (Linux 6.7); a chat whose kernel cannot do it is refused, never run with the network open.
- **No keys.** The sessionhost holds no organization secret except the one it shares with the organization
  server, no AgentField key, no GitHub token and no model key, and a chat holds none of a person's keys either. A
  chat reaches a model only through the organization server's `/v1/model/` door with a bearer the server minted for
  that chat (seven days, ended at once on stop, delete or removal of the person from the organization, kept as a hash
  in the organization's data across restarts). The door adds the real key and meters calls into the person's spend. Git goes the same
  way: the chat's `git clone`/`push` of an allowed host is rewritten to the organization server's
  `/v1/hosted/git/` door, which adds the person's own GitHub access to each request, so the chat never holds a GitHub
  token (a credential helper, `codeaf git-credential`, hands git only the chat's own bearer, and only to that one
  address).
- **Removing a person.** The server first ends the person's chats' model and git access, so any chat still running can no
  longer spend the organization's key or reach git as that person. It clears their stored keys and GitHub access and deletes
  their chats on the machine that runs them. If that machine does not answer, removal still succeeds. The server ends the
  shares of each chat it could not delete and makes it private, so every route answers `There is no such chat.` It keeps
  the deletion owed in its store (`hosted-removals.json`, beside the saved model access) and retries by itself: the first
  try five seconds after the removal, then twice as long each time up to five minutes, also after the server restarts,
  until the machine confirms every delete. The log says so when it starts waiting (again after the 10th, 100th ... failed
  try) and once when it finishes. It deletes only the chats the person had when they were removed, so a chat made later by
  someone who joined again under the same handle is left alone. The operator does not repeat the removal.
- **Limits.** `CODEAF_SESSIONHOST_MAX_RUNNING` chats at once (default 20) and `CODEAF_SESSIONHOST_MAX_PER_PERSON`
  per person (default 3). A chat counts while it has a process; a person at their limit can still make a
  chat but cannot start another and reads "You already have N chats running; stop one first."
  A chat that is stopped or deleted leaves no process behind, including ones that detached themselves.
  `CODEAF_SESSIONHOST_IDLE` (default `30m`, never under `5s`) is how long a chat's own program waits with nobody
  attached before it stops itself; the conversation and files stay and the next open starts it again. Each chat that
  exists holds one of `CODEAF_SESSIONHOST_UID_COUNT` user numbers (default 10000, from `CODEAF_SESSIONHOST_UID_BASE`,
  default 200000), and a deleted chat's number rests for 24 hours; with none free a new chat reads "This server has no room for another chat right now."
- **A turn goes on without the client.** The chat's engine keeps its conversation behind a socket in the chat's
  own home, so closing the client in the middle of a turn loses nothing and the next attach shows the result. A
  socket path has a hard length ceiling, so `CODEAF_SESSIONHOST_DATA` (default `/chats`) must be short: the
  sessionhost refuses to start with a folder that leaves no room, and says how many characters it may have.
- **The sessionhost knows only owners, and a chat is private until it is shared.** The organization server names the
  signed-in person to the sessionhost, which knows only owners, so every call it makes for a chat is made as the chat's
  owner, a guest's included. Whether someone else may reach the chat at all, and what they may do in it, is decided
  first by the organization's access core and the guest's wire (below, "Sharing, guests and bots (P2)"); the chat
  itself is the owner's. A chat nobody has shared answers "There is no such chat.", word for word as an unknown one does.

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
  `CODEAF_HOSTED_MODEL_KEY`, or else the organization key an operator saved in Settings, Organizations, or no key at all for a
  keyless stub. The door never reads the `OPENROUTER_API_KEY` variable (that one is for the runner and the nodes). The key never leaves the organization server. Every call is metered to the cent into the signed-in
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
  only for a chat the policy does not make private, hold the same without `role` and have `access: operator`) and
  `hosting` (`local_sessions`, below). Every row of `sessions` and `shared` also carries `verbs`: what the policy
  gives the caller in that chat right now, in the order of the policy's verbs, decided the way the guest's wire
  decides it (an operator-only row holds none, and the answer to `POST /v1/hosted/sessions` carries the same field
  for the chat just made). A client shows its controls from `verbs` and works nothing out from a role's name, since
  the roles are the organization's to change; the wire still refuses what the caller does not hold.
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
  A guest opening a chat that is not running starts it for its owner; when the owner's limit on running chats or the
  machine's says no (a 429 from the chats' machine), the guest reads `This chat is not running, and there is no room
  to start it right now. Try again later.` (never the owner's own sentence, and no number).
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
  `a bot cannot use this yet`). It lists, opens, stops and deletes chats within its creator's access, narrowed by its scope
  on every request. It can start a hosted chat only when the organization's policy lets agents create things and its scope
  holds `run`. A bot's chat is owned by the person who made the bot, and `created_by` records the bot.
- **Who may move a chat.** `PUT /v1/org/policy` takes `"chats": {"move_by": "owner"}` or `"chats": {"move_by": "manage"}`. Absent or
  empty means `owner`: only the person who owns a chat moves it, which is what the server always did, and a policy stored before the
  field existed reads that way. `manage` lets anyone who holds the `manage` verb on the chat move it, so a person it is shared with at
  `full`; `edit` still cannot and a bot never can, and a person who is not the owner can move it into a workstream they can edit, never
  into their own My space. The refusals read `Only the person who owns this chat can move it, and they need permission to manage it.`
  (the default) and `Only the person who owns this chat, or someone with permission to manage it, can move it.` Any other word is
  refused with a 422.
- **Who approves a tool call.** `PUT /v1/org/policy` takes `"chats": {"approvals_by": "owner"}` or `"chats": {"approvals_by": "answer"}`.
  Absent or empty means `owner`: a command a shared chat wants to run, which needs approval under its owner's approval mode,
  waits for the owner whatever role anyone else holds, on both of the chat's approval doors (`ResolveConsent`, and an answer to
  the chat's question of kind `consent` sent through `ResolveQuestion`); a guest who tries either reads `This chat is shared with
  you. Only its owner can do that.` `answer` lets a person who holds the `answer` verb (the `edit` and `full` roles) allow one
  call once, or refuse it, by either door. Allowing a tool for the rest of the chat, putting the approval away, revising an
  answer and the chat's own approval setting stay with the owner, and a person who cannot answer is refused as always. The
  guest's approval is recorded as the chat's latest asker. The setting is re-read on each heartbeat, so it reaches a window
  that is already open. Any other word is refused with a 422 that names the two.
- **Who may start a chat that is not running.** `PUT /v1/org/policy` takes `"chats": {"wake_by": "view"}`, `"run"` or `"owner"`.
  Absent or empty means `view`: opening a shared chat that is not running starts it for its owner, in its owner's place among
  the chats they may have running, for anyone who can see it (a `view` guest's `--once` included), which is what the server always
  did. `run` lets only a person who holds `run` start it, and `owner` only its owner; anyone else reads `This chat is not running.
  Starting it takes permission to send messages in it. Ask its owner.` or `This chat is not running. Only its owner can start it.
  Ask them to open it.`, and the chats' host is never asked to start it. A chat that is already running opens for everyone who can
  see it under every setting, and the owner's own window is not affected. The rule is checked when the window opens; if the
  chat's state cannot be read, `run` and `owner` refuse with the "not answering" sentence. Any other word is refused with a 422
  that names the three.
- **The hosting policy.** `PUT /v1/org/policy` takes `"hosting": {"local_sessions": "allow|warn|refuse",
  "max_session_cost_usd": 0}`. Empty `local_sessions` means `refuse` for an organization that has chats' machine and
  `allow` for one that does not; the list reports the value that applies, with `words` to show for `refuse` (that
  chats do not start on this computer and that the personal account is the way to work here) and for `warn`. An
  organization is its own account in the app, separate from the person's personal one: inside it every chat is
  hosted and there is no local escape; switching to the personal account is the escape. That does not depend on
  `local_sessions`: the organization's window is always hosted, and no client acts on `allow` or `warn` yet, so the
  setting is reported and stored and nothing more. A positive `max_session_cost_usd` replaces
  `CODEAF_HOSTED_SESSION_CAP_USD` for every chat.

Known limit of this version: a guest's `Steer` or prompt is refused by the chat itself, in the chat's own words about
the keyboard, while the owner's window holds the keyboard, because the guest's window does not take it on arrival; it
goes through when the owner has no window open, or after the guest sends `Take` (allowed with `run`, and the owner's
window is told). `Interrupt` and `ResolveQuestion` are not affected, except that an answer to a tool approval is the
owner's unless `chats.approvals_by` says `answer`. A tool-use approval stays the owner's by default, so a command that
needs one waits for the owner. The proper fix for the keyboard is a guest class inside the engine itself, which is a
change to the wire's own protocol and not made here.

### Turning it on

1. `./setup.sh` writes `CODEAF_HOSTED_SECRET` (64 hex characters, never printed) into `.env`; an `.env` from
   before this feature gets one appended, once.
2. In `.env`: `COMPOSE_PROFILES=hosted`, `CODEAF_HOSTED_URL=http://sessionhost:8790`, the organization's model
   endpoint in `CODEAF_HOSTED_MODEL_UPSTREAM` (for OpenRouter `https://openrouter.ai/api/v1`, which is also the default
   when empty) and its key in `CODEAF_HOSTED_MODEL_KEY`, and the model chats use in `CODEAF_SESSIONHOST_MODEL`.
   `./setup.sh` also writes `CODEAF_SERVER_KEY`, which seals people's own keys: keep a copy.
   `CODEAF_HOSTED_MODEL_URL` is the address chats use to reach the organization server's model door
   (`http://codeaf:8787/v1/model` by default).
3. `docker compose up -d --build`. The image must hold a codeaf that has `codeaf sessionhost`, and a plain build gives it one: Compose
   builds codeaf from this checkout by default (`CODEAF_BINARY_FROM=source`, see "Start it"). Do not set `CODEAF_BINARY_FROM=fetch` for
   hosted chats: the pinned release it downloads does not have the sessionhost until one ships it. To skip the compile, use the
   published image (`--env-file images.env`, see "Published images"). `codeaf`, `runner` and `sessionhost` all run that one image. Do not build
   from a binary of your own machine: the image should be this commit's source, which is what `codeaf version` inside it reports.

Compose puts the sessionhost on a network shared only with `codeaf`. See [Known limits](#known-limits) for what the port
fence allows and how to isolate other services. The sessionhost API (`/v1/sessions` ...) wants the shared secret and a
person's name from the organization server, and answers anything else with an empty `401`.

`SMOKE_HOSTED=1 ./smoke.sh` runs everything above against the scripted stub (so it spends nothing). It needs an image
whose codeaf has `codeaf sessionhost`, and a plain run builds one from this checkout (step 3 above). To skip that compile,
give it an image that is already built or pulled with `SMOKE_BUILD=--no-build` (and `SMOKE_ENV='--env-file images.env'` for the
published ones), or build from a binary copied beside the Dockerfile as `codeaf-local` with
`CODEAF_BINARY_FROM=local` (the header of `smoke.sh` has the command). `CODEAF_BINARY_FROM=fetch` does not work here for the
reason in step 3. Two people
run chats; the test checks that they cannot see each other's files, processes or environment, that no secret
is in a chat's shell, folder or process list, that a chat cannot reach the other services (and that the kernel refuses
a port nobody allowed), that nobody but the owner can attach to a chat that is not shared, that closing the client mid-turn loses nothing, that a stopped
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
the operator sees only metadata of a chat the policy does not make private and never its wire; a bot sees
exactly what its creator sees, starts chats that are its creator's alone, and loses a shared chat the moment its
creator does; the hosting policy is reported, changed only by the operator and refused when it is not one of the
three words. It prints PASS or FAIL per check. A live check against a real provider is the one step it does not do.

On Railway the sessionhost is a service of its own from the same Dockerfile; its variables are in the table under "Railway", and
`codeaf sessionhost probe`, run in that service, says whether the fence works there. Railway gives root eleven capabilities, the
sessionhost takes five of them, and the kernel has Landlock but not the namespace fence, so the mode there is the user switch plus
Landlock (`"mode": "uid+fence"`, `"fence_mode": "landlock"`).

## State, backups and upgrades

State lives in six named volumes (`docker volume ls`; each name is the Compose project's name, `codeaf-org` unless you set `-p` or
`COMPOSE_PROJECT_NAME`, then `_` and the volume, as below): `agentfield-data`, `codeaf-data`,
`runner-data`, `swe-af-workspaces`, `pr-af-workspaces`, and `sessions-data` (the hosted chats' folders, used only with
the `hosted` profile). `docker compose down` keeps them, `docker compose down -v` deletes them. The organization server's own
state (its people, access links, spend and sealed keys) is in `codeaf-data` and the hosted chats' folders are in
`sessions-data`. **Back up `codeaf-data` together with `.env`** (which holds the server key that seals people's own keys), and
`sessions-data` too if the hosted chats' files matter: the server's list of chats and the chats' machine hold one state between
them, so copy the two at the same moment and restore both. The archive holds people's password hashes, their sign-in sessions
and the organization's access token, so treat it like `.env`: make it readable by you alone (`umask 077`) and keep it off the
host it came from. Stop the services that write while you copy, so the files come from one moment:

```sh
umask 077
docker compose stop codeaf sessionhost       # leave sessionhost out where hosted chats are off
docker run --rm -v codeaf-org_codeaf-data:/d busybox tar czf - -C /d . > codeaf-data.tgz
docker run --rm -v codeaf-org_sessions-data:/d busybox tar czf - -C /d . > sessions-data.tgz    # "socket ignored" lines are fine
docker compose start codeaf sessionhost
```

The archive goes to standard output, so it is a file of yours; with a `-v "$PWD":/b` mount instead, root owns it and the host's
default mode applies, and some Docker setups refuse the mount. `agentfield-data` holds the runs AgentField has recorded: copy it
the same way if you want them (the nodes register again by themselves on every start).

**Restore** into the same Compose project, services stopped and the volume emptied first. On a new machine put the saved `.env`
back before the first start: without its `CODEAF_SERVER_KEY` the server refuses to start rather than carry on.

```sh
docker compose stop codeaf sessionhost
docker run --rm -v codeaf-org_codeaf-data:/d busybox sh -c 'rm -rf /d/* /d/.[!.]*'
docker run --rm -i -v codeaf-org_codeaf-data:/d busybox tar xzf - -C /d < codeaf-data.tgz
# the same two lines with sessions-data and sessions-data.tgz
docker compose up -d codeaf sessionhost
```

The people, their sign-ins, the invitations and the chats come back as they were.

Everything outside this checkout is pinned: the codeaf release that `CODEAF_BINARY_FROM=fetch` downloads (`CODEAF_RELEASE` and
its two sha256 values at the top of `codeaf.Dockerfile`), the SWE-AF and PR-AF commits (`SWEAF_REF`, `PRAF_REF`) and the AgentField
image (`AGENTFIELD_IMAGE`, tag and digest together in `agentfield.Dockerfile`). The default build does not download a codeaf at all:
it compiles this checkout, so updating codeaf means updating the checkout (`git pull`) and building again.
To upgrade a pin, change it, then `docker compose up -d --build`. Changing the line in the file is what makes
Docker rebuild that layer; a loose version would keep restoring the cached one. From the published images, change
`ORG_TAG` (see "Published images") and pull instead.

## Secrets

No secret is baked into an image. The AgentField key, the organization token, the hosted-chats secret, the server key
and the model keys come from `.env` or your environment. SWE-AF, PR-AF and the runner (not `codeaf` or `agentfield`) can be
given a key as a mounted file instead, with `AGENTFIELD_API_KEY_FILE`, `OPENROUTER_API_KEY_FILE` or `GH_TOKEN_FILE`.
Treat `.env` like a password file (`setup.sh` writes it mode 600). The image builds ignore it, and so does git: `deploy/org/.gitignore`
keeps `.env` and any `.env.*` copy of it (a backup, a second environment) out of the tree, apart from the committed templates
(`.env.example`, `.env.stub`, `.env.stub.hosted`). A key file or anything else you add to the folder yourself is not covered.

## Published images

Every image of this stack is built by `.github/workflows/org-images.yml` on each push to `dev` (and on a manual run), scanned
for leaks, and, in a repository that has Docker Hub credentials, pushed. You can run the stack from them without building
anything, and the Railway template (`railway/make-template.sh`) names them, because a Railway template cannot take a
repository from an organization that has not installed Railway's GitHub App but can take public images.

| Image (Docker Hub, namespace `agentfield`) | What it is | Compose service |
| --- | --- | --- |
| `agentfield/codeaf-org` | codeaf **built from source at the tagged commit** with `make build` (the organization server, the factory runner and the hosted chats' sessionhost; the first argument or `CODEAF_ROLE` picks the role) | `codeaf`, `runner`, `sessionhost` |
| `agentfield/codeaf-org-agentfield` | the published AgentField control plane, pinned by digest (built with `-tags "embedded sqlite_fts5"`), plus a health check and a volume hand-off | `agentfield` |
| `agentfield/codeaf-org-swe-af` | SWE-AF at the commit `swe-af.Dockerfile` pins | `swe-af` |
| `agentfield/codeaf-org-pr-af` | PR-AF at the commit `pr-af.Dockerfile` pins | `pr-af` |
| `agentfield/codeaf-org-modelstub`, `agentfield/codeaf-org-gitstub` | the keyless smoke test's scripted model and git host | `modelstub` (profile `stub`), `gitstub` (profile `gitstub`) |

The names all start with `codeaf-org` so none can be mistaken for `agentfield/control-plane`, which `codeaf-org-agentfield` is
built on. Tags: `<12-character commit id>` is one exact build and never moves; `dev` moves with every push to `dev`;
`branch-<name>` moves with every push to that branch (a `/` in the branch name becomes `-`). Each of those is a multi-platform image
(`linux/amd64` and `linux/arm64`); the single-platform images it is made from stay as `<12-character commit id>-amd64` and `-arm64`.

**Run from them.** `images.env` points the `*_ORG_IMAGE` variables at the published names; `.env` still holds your keys:

```sh
docker compose --env-file .env --env-file images.env pull
docker compose --env-file .env --env-file images.env up -d --no-build
ORG_TAG=<12-character commit id> docker compose --env-file .env --env-file images.env pull   # one exact build: how to run it for real
```

If `pull` says an image is not found, nothing has been published under that name or tag yet (a run of the workflow in a repository
without Docker Hub credentials builds and scans the images and pushes none): build them here instead.

Without `images.env`, `docker compose up -d --build` still builds everything here, as before.

**Your own registry.** Set `ORG_REGISTRY` (a Docker Hub namespace or a registry path such as `ghcr.io/my-org`) when you pull. To
have the workflow publish there, give the repository the secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` and, for a
namespace other than `agentfield`, the variable `DOCKERHUB_NAMESPACE`. Without both secrets the workflow still builds and
scans every image and pushes nothing. To build the codeaf image yourself from the repository root:

```sh
docker build -f deploy/org/codeaf.Dockerfile --build-arg ORG_DIR=deploy/org --build-arg CODEAF_BINARY_FROM=source \
  --build-arg SOURCE_COMMIT=$(git rev-parse HEAD) --label org.opencontainers.image.revision=$(git rev-parse HEAD) -t codeaf-org/codeaf:local .
```

**Check which commit an image is.** The workflow labels every image with the commit it was built from, and `codeaf version`
inside the codeaf image names it too:

```sh
docker image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' agentfield/codeaf-org:dev
docker run --rm --entrypoint codeaf agentfield/codeaf-org:dev version
```

The label is the full commit id; compare it with `git rev-parse` on a checkout, or open `https://github.com/Agent-Field/codeaf/commit/<id>`.
An image built any other way has the label only if its builder passed `--label` as above.

**The leak gate.** The workflow runs `deploy/org/gate/image-gate.py` on each built image BEFORE any push, and a finding fails
the job. The image it scanned is the image it pushes: nothing is rebuilt between the two. It reads the image's layers (including
a layer a later layer deleted), its configuration and its build history, and never prints a value it finds: only the check,
the layer, the path, the rule, and the first four characters and length of the match.

| Check | What fails |
| --- | --- |
| A1 | a finding of trivy's secret scanner (pinned by digest; its built-in rules plus `gate/trivy-secret.yaml`, which adds OpenRouter and Anthropic keys) |
| A2 | a key or token shape (OpenRouter, Anthropic, OpenAI, GitHub, AWS, Google, Slack, Stripe, JWT, a PEM private-key block, a registry `auth` blob) in the bytes of any file of any layer, binaries included. Skipped: package docs, man pages, locale and zoneinfo data, package databases, CA bundles, the Python and Perl standard libraries |
| B1 | an `ENV` or label whose name says secret (key, token, secret, password, credential, auth) with a value that is not empty, a path, a number or a boolean, or any value shaped like a key |
| B2 | the build history shows an `ARG` whose name says secret (valued or not: a secret is never a build argument), a `NAME=value` that B1 would refuse, or a key shape |
| C1 | `railway.app`, `up.railway`, `railway.internal`, or credentials in a `localhost` URL anywhere, compiled programs included; any other `.internal` host name (except `metadata.google.internal` and Docker's own) and any `10.`, `192.168.` or `172.16`-`172.31.` address in the TEXT files our build added, outside the distribution's own trees (`/usr/lib`, `/usr/share`, `/bin`, `/usr/bin`, ...). Compiled programs come from public source, so an example address in an error message is not a leak; a range with a `/prefix` is not an address |
| C2 | the image's revision label is not the commit that was built |

A file that dpkg or pip installed and that is still byte for byte what it installed is not content-scanned: dpkg records an md5
for every file (and for configuration files, in its status database) and pip a sha256 in each `RECORD`, the gate hashes the file
and only a match exempts it, so an edited file, or one planted under a package's name, is scanned like any other. Those files
are the package's published content, and they are where a real base image gives false positives (a TLS library that embeds test
private keys, a PAM sample file with example addresses). The gate prints how many files it exempted; our own files are never
in those lists.

`gate/selftest.sh` proves the gate fails what it should: it builds throwaway images with fake keys, a key in a deleted layer, a
secret `ENV`, label and build argument, and private hosts planted in them, requires each to fail the check meant to catch it, and
requires a clean image (with a network range, the metadata host and a documentation URL in it) to pass, and an unmodified
package file to be exempt and a modified one not. `gate/context-check.sh`
plants `.env` files, key files and a profile folder across a copy of the repository and requires that none enters the codeaf
build context, which is deny-by-default (`codeaf.Dockerfile.dockerignore`; the other Dockerfiles share `.dockerignore`). Both run
in CI before any image is built, beside `gate/pins-check.sh`, which fails a Dockerfile whose base image or syntax frontend has no
digest. To move a pin, resolve the tag again (`docker buildx imagetools inspect <tag> --format '{{.Manifest.Digest}}'`) and change
tag and digest together.

## Railway

The same Dockerfiles run on Railway as one project: six services (seven with the keyless model stub), a volume on each but
the model stub, private networking between them, and a public domain on `codeaf` only. Nothing needs Compose there; each
service builds from this repository's source, on Railway's builders. The table below is the configuration by hand. It was followed from
an empty project, with the keyless stub, and every service came up: see "Check it" below. (A Railway template needs images, not a
repository: `railway/make-template.sh` makes a draft one from the published images above, sessionhost included, and is a dry run
until those images exist.)

**What you need:** a Railway account and the workspace that should own the project, and the Railway CLI (`railway`, version 5.x,
signed in with `railway login`). The dashboard can do every step of the table, but "Check it" and the operator link below use the CLI.
For real work add a model key (see the `codeaf` row for where it goes); the keyless stub needs none.

### Get the source onto Railway

Pick one; the table's Root Directory and Dockerfile Path are the same for all three.

- **Connect the GitHub repository** (`Agent-Field/codeaf`, which is public: the branch you want, or one pinned commit). This worked
  from a Railway account whose linked GitHub login can read the repository, while the `Agent-Field` organization had not
  installed Railway's GitHub App. Connecting builds at once, so create the service empty, set its variables, Root Directory, Dockerfile Path and volume,
  and connect the repository last (the dashboard's service settings and the CLI both let you do that).
- **Your own fork or a private copy**, connected the same way. Authorize Railway's GitHub App for that fork.
- **Upload it with the CLI**, with no GitHub involved: from a fresh clone of the branch (so no file of yours goes up, and nothing
  untracked), run `railway up --service <service> --project <project-id> --environment <environment-id> --detach` once per service. The
  Root Directory and Dockerfile Path you set on the service apply to an upload too: the whole repository is uploaded (about 65 MB compressed)
  and Railway builds the part the service names. From a checkout of an older commit, `railway up` stops while indexing with an I/O
  error on `internal/skills/testdata/links/home/.claude/skills/dangling` (a tracked link that pointed at nothing; `gate/upload-check.sh`
  keeps it from coming back): use a newer commit, or delete that one link from your clone.

### The order that worked

1. Create the project in the workspace, then every service empty, `modelstub` too if you will use it (a variable that names another service needs that service to exist, and a service deployed before its target existed can be left with an empty host name: see "Looking inside").
2. On each service: its variables, Root Directory, Dockerfile Path, a volume of the size in the row, and (`codeaf`) the health check path and
   the public domain. Give the domain first (`CODEAF_PUBLIC_DOMAIN` is read at start), target port 8787.
3. Connect or upload the source of each service. The three codeaf-image services compile codeaf (about four minutes each); the other
   four take about a minute. A service reads **SUCCESS** when it started (and, where the row sets a health check, answered it).
4. "Check it", then "Claim the organization" below. Nothing else is needed to bring the stack up: `runner` waits up to five minutes for
   AgentField, so the order of the deploys does not matter.

| Service | Root Directory | Dockerfile Path | Volume | Variables |
| --- | --- | --- | --- | --- |
| `agentfield` | `/deploy/org` | `agentfield.Dockerfile` | `/data` | `AGENTFIELD_API_KEY=${{secret(64, "abcdef0123456789")}}`, `AGENTFIELD_HOME=/data`, `AGENTFIELD_CONFIG_FILE=/dev/null`, `AGENTFIELD_PORT=8080`, `PORT=8080`, `AGENTFIELD_STORAGE_MODE=local`, `AGENTFIELD_STORAGE_LOCAL_DATABASE_PATH=/data/agentfield.db`, `AGENTFIELD_STORAGE_LOCAL_KV_STORE_PATH=/data/agentfield.bolt`, `AGENTFIELD_RUN_OWNER_REQUIRE_PROOF=true`, `AGENTFIELD_TELEMETRY_ENABLED=false`. Health check path `/api/v1/health`. |
| `codeaf` | `/` (the repository root) | `deploy/org/codeaf.Dockerfile` | `/data` | `CODEAF_BINARY_FROM=source`, `ORG_DIR=deploy/org`, `CODEAF_ROLE=server`, `PORT=8787`, `CODEAF_ORG_NAME=...`, `CODEAF_PUBLIC_DOMAIN=${{RAILWAY_PUBLIC_DOMAIN}}`, `CODEAF_TRUSTED_PROXIES=100.64.0.0/10`, `CODEAF_SERVER_KEY=${{secret(64, "abcdef0123456789")}}`. Hosted chats: `CODEAF_HOSTED_SECRET=${{secret(64, "abcdef0123456789")}}`, `CODEAF_HOSTED_URL=http://${{sessionhost.RAILWAY_PRIVATE_DOMAIN}}:8790`, `CODEAF_HOSTED_MODEL_URL=http://${{RAILWAY_PRIVATE_DOMAIN}}:8787/v1/model`. **The model door's key: `CODEAF_HOSTED_MODEL_KEY=<your OpenRouter key>`** (or an organization key an operator saves in Settings, Organizations); `CODEAF_HOSTED_MODEL_UPSTREAM` only for a provider other than OpenRouter. The door does not read `OPENROUTER_API_KEY`. Health check path `/v1/about` (answers 200 with no sign-in; `/` answers 401). Generate a public domain for this service only, target port 8787, **before its first deploy**. |
| `runner` | `/` | `deploy/org/codeaf.Dockerfile` | `/data` | `CODEAF_BINARY_FROM=source`, `ORG_DIR=deploy/org`, `CODEAF_ROLE=runner`, `AGENTFIELD_URL=http://${{agentfield.RAILWAY_PRIVATE_DOMAIN}}:8080`, `AGENTFIELD_API_KEY=${{agentfield.AGENTFIELD_API_KEY}}`, `ORG_MODEL=...`, `OPENROUTER_API_KEY=...`. No health check path: it serves no HTTP. |
| `sessionhost` | `/` | `deploy/org/codeaf.Dockerfile` | `/chats` | `CODEAF_BINARY_FROM=source`, `ORG_DIR=deploy/org`, `CODEAF_ROLE=sessionhost`, `CODEAF_SESSIONHOST_SECRET=${{codeaf.CODEAF_HOSTED_SECRET}}`, `CODEAF_SESSIONHOST_DATA=/chats`, `CODEAF_SESSIONHOST_MODEL=...` (the model chats use), and the limits you want (`CODEAF_SESSIONHOST_MAX_RUNNING`, `CODEAF_SESSIONHOST_MAX_PER_PERSON`). No public domain and no health check path (every request to it needs the shared secret). Give it resource limits (1 vCPU and 1 GB is plenty for a few chats). |
| `swe-af` | `/deploy/org` | `swe-af.Dockerfile` | `/workspaces` | `NODE_ID=swe-planner`, `AGENTFIELD_SERVER=http://${{agentfield.RAILWAY_PRIVATE_DOMAIN}}:8080`, `AGENTFIELD_API_KEY=${{agentfield.AGENTFIELD_API_KEY}}`, `SWE_HITL_NATIVE=1`, `SWE_FENCE=off`, `SWE_PRO_ENGINE=0`, `SWE_DEFAULT_MODEL=openrouter/...`, `OPENROUTER_API_KEY=...`. |
| `pr-af` | `/deploy/org` | `pr-af.Dockerfile` | `/workspaces` | `NODE_ID=pr-af`, `PORT=8007`, `AGENTFIELD_SERVER=http://${{agentfield.RAILWAY_PRIVATE_DOMAIN}}:8080`, `AGENTFIELD_API_KEY=${{agentfield.AGENTFIELD_API_KEY}}`, `AGENT_CALLBACK_URL=http://${{RAILWAY_PRIVATE_DOMAIN}}:8007`, `PR_AF_MODEL=...`, `OPENROUTER_API_KEY=...`. |
| `modelstub` (keyless only) | `/deploy/org` | `modelstub.Dockerfile` | none | `STUB_DELAY=2s`; then on `runner`, `swe-af`, `pr-af` and `codeaf` set `CODEAF_BASE_URL=http://${{modelstub.RAILWAY_PRIVATE_DOMAIN}}:8890/api/v1` and `OPENROUTER_API_KEY=stub-key`, on `runner`, `pr-af` and `sessionhost` the model variables to `stub/scripted` (`ORG_MODEL`, `PR_AF_MODEL`, `CODEAF_SESSIONHOST_MODEL`; SWE-AF: `SWE_DEFAULT_MODEL=openrouter/stub/scripted`), on `codeaf` also `CODEAF_HOSTED_MODEL_UPSTREAM=http://${{modelstub.RAILWAY_PRIVATE_DOMAIN}}:8890/api/v1` and `CODEAF_HOSTED_MODEL_KEY=stub-key`. For the self-test below, `swe-af` also takes `SMOKE_FIXTURE_REPO=/workspaces/smoke-fixture`. |

For real work, also set `GH_TOKEN=...` on `runner`, `swe-af` and `pr-af` (it lets the nodes clone private repositories and open pull
requests, as in "What you need"; the keyless test needs none). People's own GitHub access for hosted chats is not a variable: each
person stores theirs on the server (see "Keys, spend and git").

Volumes: Railway's default size is large (50 GB on the plan this was tried on), the dashboard and the CLI's `railway volume add` take the
default unless you size it, and **a volume can grow but never shrink**. Size it when you create it: 1 GB is plenty for `codeaf`, `agentfield`
and `sessionhost`, 512 MB for `runner` and `pr-af`. Reference variables (`${{agentfield.RAILWAY_PRIVATE_DOMAIN}}`) are Railway's way of
naming another service; keep them as written.

What a first install needs that the table does not show:

- **Why `codeaf`, `runner` and `sessionhost` build from the repository root.** They are one image, and the pinned release a plain
  build fetches has no `codeaf sessionhost`, so the image is built from this repository's source (`CODEAF_BINARY_FROM=source`, and
  `ORG_DIR` says where `deploy/org` is inside that context; Compose passes the same two by default, but the Dockerfile's own default
  stays `fetch`, so Railway has to set them). Railway hands every service variable to the build as a build argument,
  which is how those two reach it; they stay in the running service's environment too, where nothing reads them. Never use
  `CODEAF_BINARY_FROM=local`: it expects a binary beside the Dockerfile that a repository build does not have.
  `codeaf version` in the running service, and `/v1/about` on the public name, name the commit Railway built (the Dockerfile reads
  Railway's `RAILWAY_GIT_COMMIT_SHA`; a `SOURCE_COMMIT` variable is not needed).
- **The public domain first.** The server reads `CODEAF_PUBLIC_DOMAIN` only when it starts, and a link is accepted only with exactly the
  address the server gave itself. So generate the domain before `codeaf` first deploys; if you generate it later, restart `codeaf`
  (changing a variable does) and use the operator link its new start prints, not an older one.
- **Secrets are Railway's.** `${{secret(64, "abcdef0123456789")}}` works in a plain project (not only in a template): each is a fresh
  random value that lives in the service's variables, never in a file. Keep `CODEAF_SERVER_KEY` and a copy of the `codeaf` volume's
  contents safe: people's own keys are sealed with it, and a server that finds them sealed with another key refuses to start.
- **The model keys.** `OPENROUTER_API_KEY` goes on `runner`, `swe-af` and `pr-af`. The hosted chats' model door is a different path: it
  calls the provider with `CODEAF_HOSTED_MODEL_KEY` on `codeaf` (else an organization key saved in Settings, Organizations), so putting
  your real key only in `OPENROUTER_API_KEY` on `codeaf` leaves hosted chats without one.
- **Pin what you deploy.** A service connected to a branch with a commit pinned does not follow later pushes; connect it again with
  the new commit to move it. `railway redeploy` re-runs the old build, so it does not pick up a new commit or a changed Dockerfile
  path. Changing a variable or a setting redeploys the service with the new value, and restarting `codeaf` before the organization is
  claimed prints a new operator link and retires the old one.
- **Looking inside.** There is no `docker exec`. `railway ssh` (it needs an SSH key registered on your Railway account; pass it with
  `-i`) runs a command in a service, with stdin from `</dev/null` or the command hangs. It sometimes answers
  `{"status":"no_ssh_key"...}` or nothing on the first try: run it again. A Railway container has no `/sys/kernel/security/lsm`, so the
  sessionhost's own `codeaf sessionhost probe` is the check that the fence works. The same command shows what a service really
  received from a reference variable: `railway ssh -s swe-af -- printenv CODEAF_BASE_URL </dev/null` must print the stub's host name.
  A service deployed before the service it names existed can receive `http://:8890/api/v1` (empty host) while the variables page
  shows the right value, and every model call from it then fails with no useful message (in the self-test, SWE-AF's plan step ends
  with "the output file was NOT created"). Redeploy that service (`railway redeploy --service swe-af`) and check again.

Two things differ from Compose. Railway's edge is an HTTPS proxy in front of `codeaf`, so
`CODEAF_TRUSTED_PROXIES=100.64.0.0/10` (the edge's range) is required: without it every password sign-in is
refused as plain HTTP. `/v1/about` on the public name says `"scheme": "https"` when it is right. Railway's private network lets
services in the same environment connect to one another. A chat's commands can reach services on allowed TCP ports, and UDP is
unfiltered. See [Known limits](#known-limits) for how to isolate other services.

### Claim the organization

The operator link is printed once per start in the `codeaf` service's log, and it names the public HTTPS address, so it works from any
computer. From a terminal in a folder linked to the project (`railway link`), or with `RAILWAY_PROJECT` and `RAILWAY_ENVIRONMENT` set to
the two ids:

```sh
deploy/org/railway/operator-link.sh        # prints the operator link (a secret; it works once); needs the railway CLI
```

It reads `railway logs --service codeaf` and prints the newest `codeaf://factory/join?operator=true...` link; with no link in the log it
says why (claimed already, not started yet, or the line scrolled past `OPERATOR_LINK_LINES`, default 300). The dashboard's log view for the
`codeaf` service shows the same line (`operator access link: ...`). Open the link in the desktop app (or paste it into **Join an organization...**),
choose a username and password, and you are the operator. **Anyone who can read the project's logs can read the link**, so claim the
organization before you add people to the Railway project. A restart of an unclaimed `codeaf` prints a fresh link and the older one stops
working: always use the newest.

### Check it

Self-test, with the stub: set `RUNNER_SELFTEST=1` on `runner` (and `SMOKE_FIXTURE_REPO` on `swe-af`, in the table) to have it run the smoke test's
control and job sections on itself at start: `PASS` or `FAIL` lines with the prefix `selftest` in its log, and `selftest RESULT PASS` at the end of
each section (the job section takes a few minutes and prints when it ends). It makes no person, so it does not claim the organization.
`RUNNER_SELFTEST=after` runs the after-a-restart section instead, on the volumes the first run left. Remove both variables when you
are done. The sections that need a person signed in (`org`, `hosted`) are not run this way: they need the organization claimed first.

From a terminal with the project linked:

```sh
railway ssh -s runner -- 'curl -s -o /dev/null -w "%{http_code}\n" $AGENTFIELD_URL/api/v1/nodes' </dev/null   # 401: the control plane wants its key
railway ssh -s sessionhost -- codeaf sessionhost probe </dev/null                                            # "ok": true, "mode": "uid+fence"
curl -s -o /dev/null -w '%{http_code}\n' "https://<the codeaf domain>/access"                                # 200: the sign-in page, over HTTPS
curl -s "https://<the codeaf domain>/v1/about"                                                               # "scheme": "https", the commit it was built from
```

The first three answer for the whole stack's wiring: AgentField refuses an anonymous call, the sessionhost can keep chats apart on this
kernel, and the public name serves over HTTPS. None of them makes a model call or a chat; those start once the organization is claimed.

## Known limits

- The runner is a small Python host of the codeaf engine (`scripts/runner.py`), because `codeaf factory serve`
  does not itself register the factory on AgentField. When codeaf ships a headless runner this goes away.
- The organization keeps no canvases: the server needs a `canvas` program beside it, which this stack does not
  carry. It starts, says so in its log, and leaves the canvas routes off.
- The server names itself by its listen address, so without `CODEAF_PUBLIC_DOMAIN` its links say `http://[::]:8787`,
  which does not connect to the published port and which the server would not take a password over anyway. Even a trial needs
  a name and an HTTPS proxy ("Trying it on one machine"); a real deployment serves HTTPS on a name.
- GitHub sign-in (`CODEAF_GITHUB_CLIENT_ID`) and invitation mail (`CODEAF_SMTP_URL`) are optional and untested here;
  username and password is the default way in.
- `scripts/serve.sh` writes the organization server's host config from the environment on every start, so a setting with
  no variable here, such as `loopback_trust` (always off for this team server; see "Claim the organization"), cannot be
  changed in this stack, and a hand edit of the file is replaced.
- One replica of each service; no high availability.
- **The network fence filters TCP by port.** A chat's commands may connect only to ports 443, 80, 22 and 9418 by default
  (`CODEAF_SESSIONHOST_CONNECT_PORTS`) and the ports used for the organization server's own address. Landlock has no rule
  for a destination address. Ports 80 and 443 can reach any host accessible from the chats' machine, including the internet,
  internal services, the Docker host at the container network's gateway, cloud instance metadata at `169.254.169.254` and
  AWS ECS task credentials at `169.254.170.2`. UDP is unfiltered. A command that reaches cloud metadata can read that
  machine's cloud credentials. A chat's environment is built from a short fixed list, which leaves out the container's own
  cloud credential variables. Text in a repository or web page can make the chat's agent run a command, so access to the
  network is relevant to anyone who can put text in front of a chat. The port fence worked on a flat test network, where
  the metadata address did not answer. Whether it is reachable on a cloud machine remains untested. At start the sessionhost
  connects to `169.254.169.254:80`, `169.254.170.2:80` and `[fd00:ec2::254]:80`, sends nothing, and logs one `WARNING` line
  naming any that answer; it never refuses to start over it, because closing the gap is the network's job.

  The operator sets these guardrails outside codeaf.

  - Block the whole link-local range `169.254.0.0/16` for traffic from the machine that runs chats, at the host or VPC
    firewall. It includes metadata addresses on AWS, Google Cloud and Azure and the ECS task credentials address. On AWS
    also block the IPv6 metadata address `fd00:ec2::254`. On a Docker host, use the `DOCKER-USER` chain for traffic from
    containers. These firewall rules were not tested.
  - On AWS require IMDSv2 (`HttpTokens=required`) and set the hop limit to 1. The hop limit helps only when IMDSv2 is required.
    Google Cloud and Azure have no equivalent hop limit. Use the firewall rule and give the instance identity (a service
    account or managed identity) only permissions you would give a chat.
  - Keep other internal services off ports 80 and 443 on the chats' network or give each its own sign-in. The container
    network's gateway is the Docker host itself. A chat can reach a host service on an allowed port, so firewall the Docker
    bridge from the host's own services.
  - Give the stack a network of its own, using a dedicated VPC or subnet, a separate security group, or a Docker network
    with no route to other internal services.
  - Railway's documentation says services in one environment can reach each other on the private network and services in
    different projects or environments cannot. Run codeaf in a project of its own and keep all other services out of that
    environment. Railway's documentation does not say whether the metadata address answers from a service container, and
    this was not checked.
  - An egress proxy that refuses private and link-local addresses is a possible next step. Address filtering is absent today,
    and the sessionhost has no network capability.
- **There is no per-chat disk or memory quota.** Each chat's Linux user is limited to 512 processes, which stopped a fork
  burst in testing. Commands are limited to 4096 open files and 10 GiB per file, with no core dumps. A 200 MB write
  and a 256 MB allocation both succeeded. One chat can fill the sessions volume or the machine's memory and slow or stop
  other chats. `CODEAF_SESSIONHOST_MAX_RUNNING` and `CODEAF_SESSIONHOST_MAX_PER_PERSON` cap the number of running chats at
  20 and 3 by default. Bound the whole `sessionhost` service with Compose `mem_limit`, `pids_limit` and `cpus`, a fixed-size
  volume and a disk alert to limit the impact to that service.
- **Removing someone while the chat machine is down.** Removal succeeds, ends the person's chats' model and git access
  immediately and clears stored keys and GitHub access. The chats the machine could not delete are closed to everyone.
  Their Linux users and folders stay on the machine until it answers, and the server then deletes them by itself (it
  retries with a longer wait each time up to five minutes, and keeps the removal owed across a restart). Check the server's
  log for the line that says it finished.
- **The container runtime supplies the seccomp profile.** The sessionhost relies on the runtime's default profile to
  block user namespaces and calls such as `mount` and `chroot`. It sets no filter of its own for those calls. It sets
  no-new-privileges, drops every capability from the chat's processes and uses a separate Linux user for each chat.
  Never use `seccomp=unconfined` or a profile looser than Docker's default. Docker applies its default unless Compose or the
  platform specifies another profile. Check that your platform's profile applies equivalent restrictions. In testing
  with a profile that allowed everything but Landlock, `unshare` worked and chat commands could write a user ID map.
  That exposes more of the kernel to a chat's unprivileged user, increasing the risk of an escape. By design, Landlock and
  the chat's Linux user still apply inside those namespaces. That behavior was not tested.
- `local_sessions: refuse` is enforced on the server side only. No client acts on it yet.
- Request framing and WebSocket upgrades through a front proxy such as Railway's edge, nginx or a load balancer were untested
  end to end. The test stack used no proxy.
- The model door's reservation path for a priced model was read in the code and remains untested end to end. The test stub's
  model has no price.
