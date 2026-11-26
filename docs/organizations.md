# Organizations

An **organization** is a codeaf server that a company or team runs for its people: the people sign in to it, share
work in it, and run their chats on its machines instead of their own. This page is the entry point. It says what an
organization is, what runs where, who can see what, and how to stand one up and run a first chat in about ten
minutes. The operator's full walkthrough is [`deploy/org/README.md`](../deploy/org/README.md); what a person reads
from inside the running chat is the manual's `organization-*.md` pages (`internal/manual/chat/`).

Two readers:

- **The operator** is whoever hosts the organization (an IT person at a company). Read "What runs where", "The ten
  minutes", "From a terminal" and "What the operator can and cannot see".
- **A person** is someone who joins one and uses it from the desktop app or the terminal. Read "Joining and working in
  one", "From a terminal", "Sharing a chat" and "What the operator can and cannot see".

## What an organization is

- **A separate account.** In the desktop app a switch chooses between **Personal** and each organization you joined.
  Personal runs on your computer and never sees the organization's data. An organization is its own account: it owns
  what is made in it, and nothing in its window runs on your computer.
- **Hosted chats.** A hosted chat runs on a machine the organization keeps: its folder, its files, its shell, git and
  every command are there. Your computer shows the screen and sends what you type. Close the window and the chat keeps
  working; open it again and the conversation is there. Hosted chats are off until the operator turns them on.
- **Private by default.** A chat belongs to the person who made it and is invisible to everyone else (not in their
  lists, not in live updates, not even its title) until the owner shares it. Sharing is a grant of a role, and a role is
  a set of verbs.
- **One organization per deployment.** One deployment of the stack below is one organization.
- **No hosted account.** There is no codeaf-run service to sign up for. An organization is a server somebody runs.

## What runs where

```
 a person's computer                     the organization's machine(s)

 desktop app  --- HTTPS / WSS --->  organization server  (codeaf, team mode)
 or                                   people, roles, sharing, spend,
 codeaf chat --org <url>              the model door, the git door
                                        |                      |
                                        | shared secret        | node link
                                        v                      v
                                   sessionhost          AgentField control plane
                                   one chat = one         |-- factory runner
                                   Linux user in a        |-- SWE-AF   (issue -> pull request)
                                   Landlock fence         `-- PR-AF    (pull request review)
```

| Piece | What it does |
| --- | --- |
| organization server | `codeaf factory serve` in team mode. Signs people in, keeps the access rules, mints each chat's model access, meters spend, and adds a person's own GitHub access to a chat's git. |
| sessionhost | The machine hosted chats run on. Gives every chat its own Linux user and folder and fences its commands. Optional: only with hosted chats on. |
| AgentField control plane | Routes factory work to the nodes. |
| relay | The organization's own relay, `codeaf relay`: where members' computers pair, see each other, Move chats and open each other's chats live, so none of that needs the public one. It keeps each member's list of computers and no chat content. `codeaf pair --org <url>` pairs through it; an explicit `CODEAF_SYNC_URL` or `--via` still wins. |
| factory runner, SWE-AF, PR-AF | The factory's nodes: the runner hosts the codeaf factory, SWE-AF builds an issue into a pull request, PR-AF reviews one. |
| modelstub, gitstub | A scripted model and git host for the keyless smoke test only. |

`deploy/org/` holds one Docker Compose file that runs all of it, the Dockerfiles, and the scripts. The images are built
from pinned sources, scanned for leaked secrets before they are pushed, and a Railway project can run the same
Dockerfiles (see "Railway" in the operator README).

## The trust model

Who sees what, and what holds that to be true:

| Who | What they can reach | What enforces it |
| --- | --- | --- |
| **The owner of a chat** | The chat, its files, its history; it runs on the owner's model access, spending limits and GitHub identity. | The organization server's access rules, checked on every request. |
| **A person the chat is shared with** (a guest) | Only what the role they were given allows (verbs, below). | The server ends the guest's connection itself and checks each call against the guest's verbs. The app only hides controls; it enforces nothing. |
| **Everyone else** | Nothing: asking for a private chat answers `There is no such chat.`, the same as for a chat that never existed. | The same access rules; the chat is left out of their lists and live updates. |
| **The organization's operator** (the person who claimed the organization) | People, roles, policy, totals of spend. Not a private chat: no title, no history, no files. | `operator.sees_private` in the policy is off by default. |
| **A bot** | At most what the person who made it holds, narrowed by its scope, decided again on every request. | The server; a bot is default-deny and may only use the routes on its allow-list. |
| **A chat's own commands** | Their own folder, and any address the machine can reach on the allowed TCP ports (443, 80, 22 and 9418 by default, plus the ports of the organization server's own address); UDP is not filtered. Not another chat's files, processes or sockets, and no key or token of anyone's. | Each chat is its own Linux user inside a Landlock fence. The chat uses its own bearer for the server's model and git doors. |
| **Whoever is root on the host** | Everything on the disk, including every chat. | Nothing: that is the honest limit (below). |

Keys and git:

- **The organization's model key never enters a chat.** The chat calls the organization server's model door with a
  bearer minted for that chat. It ends after seven days or at once when the chat is stopped or deleted or the operator
  removes its owner. The door adds the real key and meters every call to the cent into the owner's account.
- **People may add their own OpenRouter key and GitHub access.** They are sealed on disk with a key per person, which is
  itself sealed by `CODEAF_SERVER_KEY`. Back that key up with `.env`.
- **Git goes through the server.** A chat's `git clone` and `git push` to an allowed host (GitHub unless changed) are
  rewritten to the organization server, which adds the owner's GitHub access to each request. No token is ever in the
  chat.
- **Browser chat connections.** A WebSocket upgrade with an `Origin` header is accepted only if the header names the host
  the request was sent to or the public name set with `CODEAF_PUBLIC_DOMAIN`. Otherwise the server returns `403` with
  `This page is from another site, so it cannot open a chat connection here.` Connections with no `Origin` header are accepted
  because the codeaf command line, the desktop app's engine and other non-browser clients send none. The sign-in cookie is
  `SameSite=Strict`. The origin check also protects against pages on sibling names of the same site. There is no setting to
  allow another origin because no client needs one.

## Sharing a chat

A grant gives a person, a team or the whole organization a **role** on one chat, and a role is a bundle of **verbs**.
The verbs are fixed: `view`, `comment`, `edit`, `run`, `steer`, `answer`, `share`, `manage`. The four roles every
organization starts with (the operator can change their bundles; the names stay):

| Role | Verbs |
| --- | --- |
| `view` | view |
| `comment` | view, comment |
| `edit` | view, comment, edit, run, steer, answer |
| `full` | all eight |

What a guest can do in a hosted chat follows from the verbs: `view` watches and reads; `steer` also interrupts a turn
and redirects it; `answer` also answers a question the chat is waiting on; `run` also sends messages, files and
commands. `manage` also stops or deletes the chat. Everything else (approving a tool use, settings, opening the
chat's files) is the owner's whatever the role, with one setting: `chats.approvals_by` set to `answer` lets a person who
holds `answer` allow one tool use once, or refuse it (never allow a tool for the rest of the chat).

**The owner owns the chat.** It runs on the owner's credentials, spending limits and GitHub identity. A person who may
send messages acts with the owner's GitHub identity, the owner's files and the owner's limits, and the server records
who asked next to each message, charge and use of the owner's GitHub access. Share at `view` or `comment` to let someone
read along without acting as you.

The server sends each person's verbs with every chat row, and the client only hides the controls it was not given.
Taking a share away ends the guest's open window within a few seconds (`CODEAF_HOSTED_HEARTBEAT`, five by default).
When the operator removes a person from the organization, every share they hold on anyone's chat ends with them, whichever way
they joined (a named invitation or an anonymous access link), and they leave People. If they join again later, by a new link or
the old one, none of those shares comes back.

**From a terminal.** The desktop app has no sharing buttons for chats yet, so the owner shares one with a request. The
chat's name, `hs_...`, is in the owner's list; the share names a person by username and a role:

```sh
curl -sS "$ORG/v1/hosted/sessions" -H "Authorization: Bearer $TOKEN"        # "sessions": [{"id":"hs_...", ...}]
curl -sS -X POST "$ORG/v1/resources/session/$CHAT/grants" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"subject":{"kind":"person","id":"owner"},"role":"view"}'              # 201
```

The person then sees the chat under `shared` in their own list, with the verbs they hold, and opens it with
`codeaf chat --org $ORG --session $CHAT`. At `view` they watch, and a message they send is refused in the chat's own words:
`You can watch this chat, but you were not given permission to send messages in it. Ask its owner.`

## Bots

A person can make a **bot**: a named identity with its own token (`cbt_...`, shown once, kept only as a hash) for a
script or integration. A bot is owned by its creator and never holds more than the creator does right now. What it
makes (a hosted chat, for example) belongs to the creator and is recorded as made by the bot. It cannot make bots, mint
tokens, use the operator's routes, or touch a person's stored keys.
Starting a hosted chat requires the organization's policy to allow agents to create things and the bot's scope to hold
`run`, which the default `edit` and `full` roles include and `view` and `comment` leave out.

## The hosting policy

The organization's policy (`GET` and `PUT /v1/org/policy`; only the operator changes it) has a `hosting` section:

- `local_sessions`: `allow`, `warn` or `refuse`. Empty means `refuse` for an organization that runs hosted chats and
  `allow` for one that does not. Today the server reports and stores it and the desktop app's organization window is always
  hosted: the way to work on your own computer is the Personal account.
- `max_session_cost_usd`: what one hosted chat may spend before its model calls are refused. Zero keeps
  `CODEAF_HOSTED_SESSION_CAP_USD` (five dollars). The monthly limit every person's spend is held to is set by the operator in Settings (twenty dollars by default).

The same policy holds the roles, whether new chats start private (the default for chats and hosted chats), who may share,
who may move a chat (`chats.move_by`: `owner`, the default, or `manage`), who approves a tool call in a shared chat
(`chats.approvals_by`: `owner`, the default, or `answer`), who may start a chat that is not running (`chats.wake_by`:
`view`, the default, `run` or `owner`) and what bots and agents may do.

A **team server does not trust its own computer.** A request from the server's own machine with no sign-in is refused
like any other (a solo server, a desktop window's own, still trusts it). `loopback_trust` is off for a team server; the
Compose stack cannot turn it on.

## The ten minutes

You need `git`, Docker with Compose v2.24 or newer, 4 GB of free memory, ports 8787, 8788 and 8080 free on the host, and a
Linux host (Linux 6.12 or newer for the full sandbox; see the sandbox note below). For real work add an OpenRouter key; to
try it first, the scripted model needs none. For anyone to sign in with a password, **including you on the same machine**,
you need an HTTPS name in front of port 8787: the server takes a password only over HTTPS, and Docker does not hand it a
request from the machine's own address ("Trying it on one machine" in the operator README is the one-computer version).

**On Railway instead of Docker?** Skip to "Railway" in the [operator README](../deploy/org/README.md#railway): it has the services
and variables to set, how to get the source onto Railway, the operator link and the checks. Railway serves the HTTPS name, so
steps 1 to 3 below are replaced by that section, and steps 4 and 5 are the same.

**1. Get the files and set it up.** The files are this repository, and the images are built from your checkout.

```sh
git clone https://github.com/Agent-Field/codeaf
cd codeaf/deploy/org
./setup.sh        # writes .env with fresh secrets; never prints one
$EDITOR .env      # set the lines below, then save a copy of .env somewhere safe
```

For hosted chats set these in `.env`:

```sh
COMPOSE_PROFILES=hosted
CODEAF_HOSTED_URL=http://sessionhost:8790
CODEAF_HOSTED_MODEL_KEY=<your OpenRouter key>      # the key the model door uses; it never reaches a chat
CODEAF_SESSIONHOST_MODEL=<the model chats use>
CODEAF_PUBLIC_DOMAIN=codeaf.example.com            # only when a proxy serves HTTPS on that name (no scheme)
CODEAF_TRUSTED_PROXIES=<the proxy's address or range>
```

No model key yet? "Without a model key" in the [operator README](../deploy/org/README.md#without-a-model-key) has the lines
that point every call at a scripted model, which answers `STUB noted: <your message>` and spends nothing.

**2. Start it.** Compose builds the codeaf image from this checkout by default (`CODEAF_BINARY_FROM=source`), because no
published release has `codeaf sessionhost` yet. Do not set `CODEAF_BINARY_FROM=fetch` when hosted chats are on. Published
images work too, once they exist: see "Published images" in the operator README.

```sh
docker compose up -d --build
docker compose ps                                       # every service says "healthy" (the scripted model says "Up")
docker compose exec sessionhost codeaf sessionhost probe  # "ok": true on a machine that can keep chats apart
```

The first build compiles codeaf and takes several minutes; later ones reuse Docker's cache. Set
`SOURCE_COMMIT=$(git rev-parse HEAD)` in the shell first if you want `codeaf version` inside the image to name the commit.

**Sandbox note:** the full fence needs Linux 6.12 or newer. On an older kernel
the sessionhost starts no chats and says why; setting `CODEAF_SESSIONHOST_REQUIRE=uid` lets it run on the user switch
alone, with no limit on what a chat's commands can reach, and it warns in every chat. That is a choice, not a default.

**3. Claim the organization.**

```sh
./operator-link.sh      # prints the operator link; it is a secret, works once and lasts 24 hours
```

(On Railway the link is in the `codeaf` service's log; `deploy/org/railway/operator-link.sh` prints it.)

The link names your HTTPS name, so set `CODEAF_PUBLIC_DOMAIN` before you ask for it. Without one it names `http://[::]:8787`, which
does not connect. Open that `codeaf://factory/join?operator=true...` link in the desktop app (or paste it into **Join an
organization...**) and choose a username and password; with no desktop app, "From a terminal" below does the same. Whoever
uses it first is the operator, so keep it to yourself until you have. `./operator-link.sh` prints nothing once the
organization is claimed. If the link is lost or too old before anyone used it, `docker compose restart codeaf` prints a
fresh one.

**4. Invite a person.** In the desktop app, Settings, Organizations, your organization, **People**: **Invite someone**.
Without `CODEAF_SMTP_URL` nothing is emailed; the button gives you a link to copy. It works for seven days and for one
person. They paste it into **Join an organization...** on their own computer and choose their own username and
password (a passkey or GitHub if the server takes them). From a terminal, see "From a terminal" below.

**5. Run a first hosted chat.** In the desktop app, switch from Personal to the organization and start a new chat there;
it runs on the sessionhost. From a terminal:

```sh
CODEAF_ORG_TOKEN=<your sign-in token> codeaf chat --org https://codeaf.example.com
```

`--org` takes `https://` for a real server (plain `http://` only for the computer you are on), opens your most recent
chat or makes your first, and a chat shared with you opens with `--session hs_...`. It needs your sign-in token in
`CODEAF_ORG_TOKEN`, or kept once with `codeaf attach <url> --token <token>` (that also makes this computer's new codeaf
windows use the organization's factory until `codeaf detach`). The token is the `access_token` that joining or signing in
answers with: "From a terminal" below shows both. The `codeaf` that runs it has to be new enough to have the flag
(`codeaf chat --help` lists `--org`); no stable release has it yet, so build `bin/codeaf` from the checkout with `make build`.

To check the whole stack without spending anything: `SMOKE_HOSTED=1 ./smoke.sh` (it runs against a scripted model and
needs an image that has `codeaf sessionhost`; the operator README says how to point it at yours). It claims the organization
itself, with throwaway people, so run it on a stack nobody has claimed, and `docker compose down -v` before the real set-up.

## From a terminal

Joining is one request to the server, the one the desktop app sends, so a person with no desktop app can do it with `curl`.
Set the address once, and add `--cacert ./caddy-root.crt` to each `curl` if your proxy's certificate is not from a public
authority ("Trying it on one machine" in the operator README):

```sh
ORG=https://codeaf.example.com
```

**Claim it, as the operator**, with the link `./operator-link.sh` printed:

```sh
LINK='codeaf://factory/join?operator=true&url=...#token=...'
curl -sS "$ORG/v1/factory-access" -H 'Content-Type: application/json' \
  -d "{\"verb\":\"sign_up\",\"link\":\"$LINK\",\"username\":\"owner\",\"password\":\"twelve or more characters\",\"name\":\"Your Name\"}"
```

The answer holds `"state":"joined"` and an `"access_token"`: your sign-in token. Keep it like a password; it ends after 30
days without use, or 90 days at most. A refusal is not an error code: the answer is `200` with `"state":"ready"` and a
`"message"` (`A password cannot contain your username.`), and a link already used says `"state":"used_up"`.

**Invite a person**, as the operator, with the token from above:

```sh
curl -sS -X POST "$ORG/v1/access/links" -H "Authorization: Bearer $OPERATOR_TOKEN" -H 'Content-Type: application/json' -d '{}'
```

`link.url` in the answer is the invitation: one person, seven days. The person joins with the same `sign_up` request, giving that
link and a username and password of their own, and keeps their `access_token` as `CODEAF_ORG_TOKEN`:

```sh
CODEAF_ORG_TOKEN=<their access_token> codeaf chat --org $ORG
```

**Sign in again** when a token has ended or on another computer, with no link:

```sh
curl -sS "$ORG/v1/auth/password" -H 'Content-Type: application/json' -d '{"username":"priya","password":"..."}'
```

It answers with an `access_token` the same way.

## Joining and working in one

1. Open the invite link you were sent in the desktop app, or paste it into **Join an organization...**, and sign in (a
   username and password, a passkey or GitHub, whichever the server takes).
2. Switch from **Personal** to the organization. Everything in its window is the organization's, and nothing of Personal's
   is shown there.
3. Start a chat in the organization's window. It runs on the organization's server, not on your computer; a server that
   is down is reported as `<host> is unreachable.` and the chat never falls back to running locally.
4. Your chats are private until you share them. The chats others shared with you are listed next to yours with what
   you may do in each.

## What the operator can and cannot see

- The operator cannot open, list or read a private chat; asking for it answers `There is no such chat.`. The operator sees
  people, roles, the policy and totals of spend, and the metadata of a chat only when the policy makes chats of that kind
  visible.
- **Whoever runs the machine can read everything on its disk**, the operator included in that role, and so can the hosting
  platform's administrators. A hosted chat is not private from them, and the stack does not encrypt the disk. If you do not
  want your code on a machine your organization runs, use the Personal account.
- A chat's commands can reach the internet and any service listening on an allowed port of the private network (the limit is
  by port, not by address), so each service on that network must keep its own sign-in.

## Known limits

- A guest's message or steer waits while the owner's window holds the keyboard: the chat itself refuses it in its own words
  about the keyboard. It goes through when the owner has no window open, or after the guest takes the keyboard (allowed
  with `run`). Interrupting and answering are not affected. A tool-use approval stays with the owner, so a command that
  needs one waits for the owner, unless the policy's `chats.approvals_by` is `answer`.
- A guest who runs `codeaf chat --org ... --session ... --once` on a chat that is not running starts it for its owner in that
  one-shot mode, even when the guest's own message is refused. The owner's window then reads `this conversation is already
  open in a different interactive or headless mode; close it before retrying` until the chat is stopped (the owner's stop, or
  `POST /v1/hosted/sessions/<chat>/stop`) or stops itself when idle. The policy's `chats.wake_by` set to `run` or `owner`
  keeps a person who cannot send messages in the chat (or anyone but its owner) from starting it at all: they are told the
  chat is not running and to ask its owner, and nothing starts. The default, `view`, is the behaviour described here.
- By default only the owner moves a chat between My space and a workstream; the policy's `chats.move_by` set to `manage` lets
  people with the `manage` verb on the chat move it too (never into their own My space, never a bot).
- `local_sessions` `allow` and `warn` are reported and stored; nothing in a client acts on them yet.
- The sandbox filters TCP by port. Through allowed ports, a chat can reach internal services, the Docker host gateway and
  cloud metadata such as `169.254.169.254` if those addresses answer. Cloud metadata can expose the machine's cloud
  credentials. UDP is unfiltered. The port fence worked on a flat test network, where the metadata address did not answer.
  Reachability on a cloud machine remains untested. The sessionhost logs a warning at start when this machine answers on one.
- A hosted chat has no total disk or memory quota and can fill the sessions volume or the machine's memory. The operator can
  bound the whole `sessionhost` service.
- Removing someone while the chat machine is down still ends the person's model and git access, clears their stored keys and
  GitHub access and closes their chats to everyone. The chats stay on the machine until it answers. The server keeps the
  deletion owed in its store and retries by itself, with a longer wait each time up to five minutes, also after a restart,
  and stops once the machine confirms. The operator does not repeat the removal.
- The sessionhost relies on the container runtime's default seccomp profile to block user namespaces and calls such as
  `mount` and `chroot`. Use Docker's default profile or one with equivalent restrictions.
- `local_sessions: refuse` is enforced on the server side only. No client acts on it yet.
- Request framing and WebSocket upgrades through a front proxy were untested end to end. The test stack used no proxy.
- The priced-model reservation path was read in the code and remains untested end to end. The test stub has no model price.
- The script that makes the Railway template (`deploy/org/railway/make-template.sh`) ran end to end against the published
  images on Railway CLI 5.71.0, except its last call, `railway templates create`, which makes a draft that the owner still has
  to finish in the dashboard and that was not looked at. Its volumes come out at Railway's default 50 GB because the CLI has no
  size option. The operator README, "Railway", has the resize step, the draft and a backup recipe for Railway that was run once
  and restored only into a scratch folder.
- A hosted chat's sandbox has `python3`, `git`, `curl` and `openssh-client`, and no `pip`, `pytest`, `go`, `node`, `make` or
  `gcc`; a chat's user is not root and cannot install them. A project that needs more than the standard library cannot run its
  tests in a hosted chat today, and there is no setting that changes it.
- SWE-AF, PR-AF and the runner call the model provider directly with their own copy of the key, not through the organization's model
  door. Their spend is not in the spend table, in anyone's My space, or in any chat's or person's limit, and those limits do
  not stop them. In one production-style run a factory job's own view said it cost `0.00027` while OpenRouter billed about $0.56 for
  it. Put a credit limit on the key at the provider.
- GitHub sign-in and invitation mail are optional and untested in the stack; a username and password is the default way in.
- One replica of each service; no high availability.

See the [operator guardrails in Known limits](../deploy/org/README.md#known-limits) for firewall and cloud identity rules,
resource limits, cleanup after removal and seccomp profiles.

## Where to read next

- [`deploy/org/README.md`](../deploy/org/README.md): environment variables, HTTPS, backups, upgrades, images and the leak
  gate, Railway, and every limit in detail.
- `internal/manual/chat/organization-hosted-sessions.md` and `organization-sharing.md`: what a person is told inside the
  chat, sentence by sentence.
- [`docs/design/app-v3/PROTOCOL.md`](design/app-v3/PROTOCOL.md): the wire the desktop app and the server speak.
