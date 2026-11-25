# Organizations

An **organization** is a codeaf server that a company or team runs for its people: the people sign in to it, share
work in it, and run their chats on its machines instead of their own. This page is the entry point. It says what an
organization is, what runs where, who can see what, and how to stand one up and run a first chat in about ten
minutes. The operator's full walkthrough is [`deploy/org/README.md`](../deploy/org/README.md); what a person reads
from inside the running chat is the manual's `organization-*.md` pages (`internal/manual/chat/`).

Two readers:

- **The operator** is whoever hosts the organization (an IT person at a company). Read "What runs where", "The ten
  minutes" and "What the operator can and cannot see".
- **A person** is someone who joins one and uses it from the desktop app or the terminal. Read "Joining and working in
  one", "Sharing a chat" and "What the operator can and cannot see".

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
| **A chat's own commands** | Their own folder and the internet on four ports (443, 80, 22, 9418 unless changed). Not another chat's files, processes or sockets, and no key or token of anyone's. | Each chat is its own Linux user inside a Landlock fence. The model is reached only through the server's model door with a bearer minted for that one chat; git goes through the server's git door. |
| **Whoever is root on the host** | Everything on the disk, including every chat. | Nothing: that is the honest limit (below). |

Keys and git:

- **The organization's model key never enters a chat.** The chat calls the organization server's model door with a
  bearer minted for that chat (it ends when the chat is stopped or deleted, and after seven days). The door adds the real key
  itself and meters every call to the cent into the owner's account.
- **People may add their own OpenRouter key and GitHub access.** They are sealed on disk with a key per person, which is
  itself sealed by `CODEAF_SERVER_KEY`. Back that key up with `.env`.
- **Git goes through the server.** A chat's `git clone` and `git push` to an allowed host (GitHub unless changed) are
  rewritten to the organization server, which adds the owner's GitHub access to each request. No token is ever in the
  chat.

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
chat's files) is the owner's whatever the role.

**The owner owns the chat.** It runs on the owner's credentials, spending limits and GitHub identity. A person who may
send messages acts with the owner's GitHub identity, the owner's files and the owner's limits, and the server records
who asked next to each message, charge and use of the owner's GitHub access. Share at `view` or `comment` to let someone
read along without acting as you.

The server sends each person's verbs with every chat row, and the client only hides the controls it was not given.
Taking a share away ends the guest's open window within a few seconds (`CODEAF_HOSTED_HEARTBEAT`, five by default).

## Bots

A person can make a **bot**: a named identity with its own token (`cbt_...`, shown once, kept only as a hash) for a
script or integration. A bot is owned by its creator and never holds more than the creator does right now. What it
makes (a hosted chat, for example) belongs to the creator and is recorded as made by the bot. It cannot make bots, mint
tokens, use the operator's routes, or touch a person's stored keys.

## The hosting policy

The organization's policy (`GET` and `PUT /v1/org/policy`; only the operator changes it) has a `hosting` section:

- `local_sessions`: `allow`, `warn` or `refuse`. Empty means `refuse` for an organization that runs hosted chats and
  `allow` for one that does not. Today the server reports and stores it and the desktop app's organization window is always
  hosted: the way to work on your own computer is the Personal account.
- `max_session_cost_usd`: what one hosted chat may spend before its model calls are refused. Zero keeps
  `CODEAF_HOSTED_SESSION_CAP_USD` (five dollars). The monthly limit every person's spend is held to is set by the operator in Settings (twenty dollars by default).

The same policy holds the roles, whether new chats start private (the default for chats and hosted chats), who may share,
who may move a chat (`chats.move_by`: `owner`, the default, or `manage`) and what bots and agents may do.

A **team server does not trust its own computer.** A request from the server's own machine with no sign-in is refused
like any other (a solo server, a desktop window's own, still trusts it). `loopback_trust` is off for a team server; the
Compose stack cannot turn it on.

## The ten minutes

You need Docker with Compose v2.24 or newer, 4 GB of free memory, and a Linux host (Linux 6.12 or newer for the full
sandbox; see the sandbox note below). For real work add an OpenRouter key. For anyone who is not on the same
machine you need an HTTPS name in front of port 8787.

**On Railway instead of Docker?** Skip to "Railway" in the [operator README](../deploy/org/README.md#railway): it has the services
and variables to set, how to get the source onto Railway, the operator link and the checks. Railway serves the HTTPS name, so
steps 1 to 3 below are replaced by that section, and steps 4 and 5 are the same.

**1. Set it up.**

```sh
cd deploy/org
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

**2. Start it.** Compose builds the codeaf image from this checkout by default (`CODEAF_BINARY_FROM=source`), because no
published release has `codeaf sessionhost` yet. Do not set `CODEAF_BINARY_FROM=fetch` when hosted chats are on. Published
images work too, once they exist: see "Published images" in the operator README.

```sh
docker compose up -d --build
docker compose ps                                       # every service says "healthy"
docker compose exec sessionhost codeaf sessionhost probe  # "ok": true on a machine that can keep chats apart
```

The first build compiles codeaf and takes several minutes; later ones reuse Docker's cache. Set
`SOURCE_COMMIT=$(git rev-parse HEAD)` in the shell first if you want `codeaf version` inside the image to name the commit.

**Sandbox note:** the full fence needs Linux 6.12 or newer. On an older kernel
the sessionhost starts no chats and says why; setting `CODEAF_SESSIONHOST_REQUIRE=uid` lets it run on the user switch
alone, with no limit on what a chat's commands can reach, and it warns in every chat. That is a choice, not a default.

**3. Claim the organization.**

```sh
./operator-link.sh      # prints the operator link; it is a secret and works once
```

(On Railway the link is in the `codeaf` service's log; `deploy/org/railway/operator-link.sh` prints it.)

Open that `codeaf://factory/join?operator=true...` link in the desktop app (or paste it into **Join an organization...**)
and choose a username and password. Whoever uses it first is the operator, so keep it to yourself until you have. If it
is lost before anyone used it, `docker compose restart codeaf` prints a fresh one.

**4. Invite a person.** In the desktop app, Settings, Organizations, your organization, **People**: **Invite someone**.
Without `CODEAF_SMTP_URL` nothing is emailed; the button gives you a link to copy. It works for seven days and for one
person. They paste it into **Join an organization...** on their own computer and choose their own username and
password (a passkey or GitHub if the server takes them).

**5. Run a first hosted chat.** In the desktop app, switch from Personal to the organization and start a new chat there;
it runs on the sessionhost. From a terminal:

```sh
CODEAF_ORG_TOKEN=<your sign-in token> codeaf chat --org https://codeaf.example.com
```

`--org` takes `https://` for a real server (plain `http://` only for the computer you are on), opens your most recent
chat or makes your first, and a chat shared with you opens with `--session hs_...`. It needs the sign-in token your
organization gave you in `CODEAF_ORG_TOKEN`, or kept once with `codeaf attach <url> --token <token>`.

To check the whole stack without spending anything: `SMOKE_HOSTED=1 ./smoke.sh` (it runs against a scripted model and
needs an image that has `codeaf sessionhost`; the operator README says how to point it at yours).

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
  needs one waits for the owner.
- By default only the owner moves a chat between My space and a workstream; the policy's `chats.move_by` set to `manage` lets
  people with the `manage` verb on the chat move it too (never into their own My space, never a bot).
- `local_sessions` `allow` and `warn` are reported and stored; nothing in a client acts on them yet.
- The sandbox filters outbound network by port, not by address.
- The Railway template is made from the published images, which are not on Docker Hub yet, so the script that makes it has only
  been dry-run. The by-hand Railway install in the operator README is the tested configuration, and it includes the sessionhost.
- GitHub sign-in and invitation mail are optional and untested in the stack; a username and password is the default way in.
- One replica of each service; no high availability.

## Where to read next

- [`deploy/org/README.md`](../deploy/org/README.md): environment variables, HTTPS, backups, upgrades, images and the leak
  gate, Railway, and every limit in detail.
- `internal/manual/chat/organization-hosted-sessions.md` and `organization-sharing.md`: what a person is told inside the
  chat, sentence by sentence.
- [`docs/design/app-v3/PROTOCOL.md`](design/app-v3/PROTOCOL.md): the wire the desktop app and the server speak.
