# Commands you type in a terminal

## codeaf cluster status when the sync service cannot be reached

If the service cannot be reached, `codeaf cluster status` exits with a failure
status and uses the same sentence as `codeaf devices`:
`the sync service at <address> cannot be reached from here — check this computer's network`.
The address is your configured sync service. Check this computer's network
and that the service is running, then try the command again.

## Is there a newer version — update codeaf — update a dev build — keep a dev build up to date — latest dev — /update — why does it say this every time I start

Every launch of a release build checks whether there is a newer version: the
newest release of the channel it follows. What happens next depends on the build:

- A released codeaf — a stable tag, in a file named `codeaf` — with `automatic
  updates` on (the default) installs the newest stable release BEFORE the chat
  opens and starts on it. The terminal says `updating codeaf <running> to
  <newest> · ctrl+c opens <running> now`, then `codeaf <newest> installed ·
  opening it`, and the chat's first line says `codeaf updated from <old> to
  <new> as it opened · automatic updates in /settings turns this off`.
- Every other build is told and never replaced unasked: dev and staging builds,
  release candidates, `devaf`, `stageaf`, any other file name, a pinned file,
  and any codeaf with `automatic updates` off. Before the chat opens the
  terminal prints `codeaf <newest> is out · you have <running> · codeaf update
  installs it` (with the file's own name: `devaf update`), and the chat says
  `codeaf <newest> is out · you have <running> · /update installs it for the
  next launch`. Nothing is downloaded until you ask.

A release candidate is told when its stable line is published. An equal or
ahead build gets nothing. Source and unstamped builds — `make build`, `go run`
— make no launch request at all, and their `/update` answers `this codeaf was
built from source · rebuild with make build, or install a release: curl -fsSL
https://agentfield.ai/get/codeaf | bash`. A codeaf owned by Homebrew, Nix or the
system package manager, or one in a folder this account cannot write, is never
replaced in place: the launch names the manager and the curl line instead.

## Does it update itself automatically — automatic updates — auto update on launch — restart on the new version — stop updating itself

Only a released codeaf moving to a newer stable release is updated without
being asked, and only where a person is waiting: standard input and output are
a terminal, `CI` is unset, and the launch is not `--once`. The look is cached
for 24 hours and otherwise waits at most 1.5 seconds; a look that got no answer
is not waited on again for an hour. The install waits at most 20 seconds and
then hands the terminal to the new build with the same arguments.

Every way it stops short opens the build already on disk. ctrl+c says `update
stopped · opening <running>`, and that window does not count down to the same
release again. A slow download says `codeaf <newest> is still downloading ·
opening <running> now · the chat installs it in the background`, and for a day
that release is left to the chat rather than waited on at launch. A failure says
`codeaf <newest> could not be installed · <reason> · opening <running>` and
counts: a release backs off for hours after a failure and stops being tried on
its own after three. A release you skipped, or one backing off, prints nothing
at launch.

When the launch could not finish, the chat's offer takes over: `codeaf <newest>
is out · installs in 10s · <skip chord> skip · /update` waits in the keys line,
counted down from about ten seconds, then installs in the background and says
`codeaf <newest> installed · restart to use the new version`. It never blocks a
keystroke or restarts the conversation. `/update` while it is up
installs at once; the skip chord, or `/update skip`, answers that one release
and it is never offered again.

To stop all of it, turn off `automatic updates` on `/settings`' **General**
tab or type `/update never`; launches still say what is out.
`CODEAF_NO_UPDATE_CHECK=1` makes no request at all. Turning the row off does not
cancel an install already downloading.

## Pin a version — stay on an older version — roll back to an earlier release

`codeaf update --version <tag>` or `/update <tag>` installs that exact release
and PINS it: ``codeaf: <tag> is pinned · it is not updated on its own until
`codeaf update` runs without --version`` (the chat says `… until /update runs
without a version`). Naming the release already installed pins it in place
without a download. Naming a tag is also the only way to roll back.

A pinned file is never updated on its own. Its launches say `codeaf <newest> is
out · you have <running>, pinned · codeaf update installs it and unpins`, and an
automatic install that races a pin is refused with `codeaf <tag> is pinned on
this machine · /update or codeaf update installs <newest> and unpins it`.

A plain `codeaf update`, `codeaf update --stable`, or `/update` with no tag
installs the newest release and unpins. The pin belongs to the file, not to a
profile: it is kept beside it in `<file>.install.lock`, every profile running
that file sees it, and a file reinstalled with the curl line is no longer
pinned.

## /update in the chat — install in the background — skip — never — channel — rollback

`/update` downloads the release, checks its sha256 and replaces this executable
IN THE BACKGROUND; `/upgrade` is its alias. It never quits, never restarts the
conversation and never stops a running turn or task — the session keeps the build
it started on, and the new one is what opens the next time codeaf is started.
`/update` [skip|never] answers an open offer. With no channel word, a dev or
staging build selects its own channel; stable and rc select stable. An explicit
channel or tag wins. An ahead dev build refuses with `this codeaf is <running>,
ahead of the newest dev <newest> — /update <newest> installs it anyway`; naming
the tag is the deliberate downgrade, and it is the ONLY road that rolls a file
back — a bare `/update` you type while an offer is up installs what the offer is
about and never steps the file backwards. An automatic or channel update never installs an older build than the one
on disk, and a release already on disk is a quiet no-op. If the file is owned by
Homebrew, Nix or the system package manager, or is in a folder this account
cannot write, neither road replaces it in place. One install runs at a time per executable:
two terminals, or two profiles sharing one codeaf, cannot replace it at once, and
a window that resolves a release another terminal has already superseded installs
nothing.

## Skip the update check — CODEAF_NO_UPDATE_CHECK — update-check.json — which curl line reinstalls this file

Set `CODEAF_NO_UPDATE_CHECK=1` to skip the launch check and the update before
the chat opens. Dev and staging
answers are cached beside `config.json` for one hour in `update-check.dev.json`
and `update-check.staging.json`; stable and rc use `update-check.json` for 24
hours. A cached newer release is still shown. The curl line reinstalls this file
from the channel the build follows — dev and staging follow themselves, and a
stable, rc or source build follows stable. So `devaf` gets `curl -fsSL
https://agentfield.ai/get/devaf | bash`, `stageaf` gets `curl -fsSL
https://agentfield.ai/get/stageaf | bash`, a dev build named `codeaf` gets
`/get/codeaf/dev`, a release candidate gets the plain `/get/codeaf`, the same
stable release its launch line just named, and a file under any other name gets
`| CODEAF_INSTALL_NAME=<name> bash` on the end of its channel's line.

## codeaf update from a shell — --check — default channel — ahead of newest

`codeaf update` and `codeaf update --check` default to dev on a `dev-*` build,
staging on a `staging-*` build, and stable on stable or rc. `--stable`, `--rc`,
`--dev`, `--staging`, or `--version <tag>` overrides that choice. `--version
<tag>` also pins the file at that release, and any install without it unpins.

`--check` exits 3 when the selected release is newer, and also whenever a dev or
staging tag is selected from a build of another channel, because a channel tag
cannot be ordered against a build from another channel; that answer reads
`codeaf <tag> is available · you have <running>`. It exits 0 when the
selection is the tag already running, when this build is ahead of its own
channel, and when the two cannot be ordered at all — which is what a dev build
asking `--stable` gets. It exits 1 when it could not check. `--version <tag>`
exits 0 only on that tag and 3 otherwise. Every answer names the selected tag.
A build ahead of its own channel says `the newest dev codeaf is <newest> · this
codeaf is <running>`, with `staging` in place of `dev` on that channel.
Installing without `--check` refuses an implicit downgrade the same way:
`this codeaf is <running>, ahead of the newest dev <newest> — pass --version
<newest> to install it anyway`; naming that tag installs it.

A source build refuses and names its path: rebuild with `make build`, or install
a release with `curl -fsSL https://agentfield.ai/get/codeaf | bash`. An unwritable
target, failed download or bad checksum leaves the original in place and offers
the curl line for this executable and channel; codeaf never tries sudo.

## How do I install codeaf — the curl line, agentfield.ai/get/codeaf, dev, staging, rc and stable channels

The install line is one curl, the same one the README prints:

```sh
curl -fsSL https://agentfield.ai/get/codeaf | bash
```

`https://agentfield.ai/get/codeaf` serves `scripts/install.sh` from the `main` branch of
the public repository, byte for byte. The script under it can be run directly:

```sh
curl -fsSL https://raw.githubusercontent.com/Agent-Field/codeaf/main/scripts/install.sh | bash
```

Pipe it to `bash`, not `sh`: the script uses `set -o pipefail` and `[[ ]]`, and `sh` is
dash on Debian and Ubuntu, which rejects both.

A push to `dev` publishes a `dev-*` build, a push to `staging` publishes a
`staging-*` build, and a push to `main` publishes an rc. Each is marked as a
prerelease. Stable is published only when a person dispatches `Release` on `main`.
`--stable` is the default and reads GitHub's `releases/latest`, which excludes
prereleases. The `/get/devaf` and `/get/stageaf` lines install the dev and staging
channels beside codeaf under those file names. To take another channel, put it on the path —
`https://agentfield.ai/get/codeaf/dev`, `/staging` or `/rc` — or pass `--dev`,
`--staging` or `--rc` after `bash -s --`. A channel with nothing published stops with
`no <channel> build has been published yet`. To pin one complete tag, replace the
final pipe with `| VERSION=<tag> bash`; for example:

```sh
curl -fsSL https://agentfield.ai/get/codeaf | VERSION=v0.2.0 bash
```

For the bare address the proxy hands the script out unchanged; for a channel path
it rewrites the one line that sets the default channel. If it cannot find that line
exactly once, or what it fetched is not a shell script, it answers 502.

Building from source needs nothing published: clone the repository, run `make build`,
then run `bin/codeaf` from the checkout.

The installer writes `~/.codeaf/bin/codeaf`, adds it to your shell profile, and
when a folder already on `PATH` is writable (`~/.local/bin`, `~/bin`, or
`/usr/local/bin`) it links `codeaf` there too, so the command works in the same
terminal with nothing to paste. It never replaces anything there but its own link:
a file, or a link to another build, stays, and you get the line to paste instead. It prints a few checked steps (`Downloaded`, `Installed codeaf <tag>`, `PATH`,
`Linked`), then a short *Get started* guide: the `export PATH=…` line to paste when
the command is not reachable yet, `cd your-project` and `codeaf`, and how to connect a
model. On a terminal it ends by asking `Start codeaf in <folder> now? [Y/n]`; `enter`
starts it there, and the first run connects a model. `--no-start` or
`CODEAF_NO_START=1` skips the question, and nothing is asked when the output is not a
terminal, when `CI` is set, or when the install runs in your home folder or `/` —
`cd` into a project and type `codeaf` there instead. It prints nothing about telemetry, and
neither does codeaf: what the anonymous usage counts carry is written in `docs/TELEMETRY.md`
in the repository, and the `telemetry` switch in `/settings` turns them off.
`/update` in the chat or `codeaf update` in a terminal replaces it in place;
running the install line again works too. With `automatic updates` on, a codeaf
installed this way also installs each new stable release itself as it opens.

## The installer stopped — GitHub API rate limit used up, curl install fails with 403, 401 Bad credentials, GitHub rejected GITHUB_TOKEN, token refused, repository you cannot read

The installer names GitHub's refusal. These are example messages; `HH:MM ZONE`
stands for the reset time GitHub supplies:

- `GitHub's API rate limit for this address is used up until HH:MM ZONE; export GITHUB_TOKEN to use your own, or pin VERSION=<tag>`
- `GitHub rejected GITHUB_TOKEN (401: Bad credentials), and the request without it failed too (401: Bad credentials); replace or unset GITHUB_TOKEN, or pin VERSION=<tag>`
- `GitHub refused GITHUB_TOKEN access to Agent-Field/codeaf (403: Resource not accessible by personal access token), and the request without it failed too (403: Resource not accessible by personal access token); check what GITHUB_TOKEN may read, or pin VERSION=<tag>`

A token GitHub refuses with 401, 403 or 429 is set aside for the rest of the run,
and the same lookup is asked once without it. If that works, installation
continues with a note: `GitHub refused GITHUB_TOKEN (its rate limit is used up until HH:MM ZONE); continuing without it`.
Other token failures get no retry. `GITHUB_TOKEN` supplies the token when
non-empty; otherwise `GH_TOKEN` does, and the message names `GH_TOKEN` instead.
Downloads never carry the token.

The reset time comes from GitHub's header in this machine's local time, with
no zone suffix if the machine supplies none. Without a renderable reset, a
numeric Retry-After header gives `for about a minute` or `for about M minutes`;
with neither it says `for up to an hour`. A 403 without quota evidence means
access was refused. A 404 can also mean a repository you cannot read. Some wget
builds discard error bodies or headers, so a refusal can lack its message or
quota details.

Pinning a complete release tag bypasses the API lookup for any of these failures:
`curl -fsSL https://agentfield.ai/get/codeaf | VERSION=<tag> bash`.

## What is devaf — dev build beside codeaf — side by side — two versions — install under a different file name — --name

`devaf` is the file name for a codeaf dev-channel build, not another product.
Install the newest dev build beside codeaf with:

```sh
curl -fsSL https://agentfield.ai/get/devaf | bash
```

That proxy serves the installer from the `dev` branch and rewrites exactly two
default lines: the channel becomes dev and the installed name becomes devaf. It
writes `~/.codeaf/bin/devaf` and leaves `~/.codeaf/bin/codeaf` untouched. On
Windows the file is `devaf.exe`. `devaf version` still starts with `codeaf`.
The installer's receipt names the command to type first:
`installed devaf · codeaf dev-<date>-<commit> built …`. Any `--name` install
reads the same way, with its own word first.

The general spelling is `--name WORD` or `CODEAF_INSTALL_NAME=WORD`; the name
may contain ASCII letters, digits, `.`, `_`, and `-`, and must begin with a
letter or digit. codeaf and devaf share `~/.codeaf`, including keys and
conversations, and one engine per workspace; opening a workspace with the other
build retires an idle host or joins a busy compatible one. A devaf launch checks
the dev channel hourly, and bare `/update` keeps following dev. A devaf is never
updated on its own: a launch behind the newest dev build says `codeaf <newest>
is out · you have <running> · devaf update installs it` and opens as it is.

## What is stageaf — staging build beside codeaf — try the next release early — install staging — how often does staging update

`stageaf` is the file name for a codeaf staging-channel build, not another product.
Install it beside codeaf with:

```sh
curl -fsSL https://agentfield.ai/get/stageaf | bash
```

That proxy serves the installer from the `staging` branch and rewrites only the
channel and name lines. It installs `~/.codeaf/bin/stageaf` (`stageaf.exe` on
Windows) beside an untouched codeaf. `stageaf version` still starts with `codeaf`.
It shares `~/.codeaf` with codeaf and devaf, including keys and conversations.
A stageaf launch checks the staging channel hourly; bare `/update` follows staging.
Like devaf it is never updated on its own; `stageaf update` installs the newest
staging build when you ask.

Promotion runs when dispatched and moves staging to the selected dev commit
after the full check passes. When the check fails or there is nothing new,
staging stays where it was. Moving main is a separate release decision.

## Why codeaf do may download rtk — compressed shell output and how to turn it off

When `codeaf do` starts local workers, it starts one background attempt to find or
fetch rtk v0.45.0. rtk is a third party's program from `github.com/rtk-ai/rtk`; it
compresses the output of its named read and check filters before the work model pays
to read it. Anything that writes runs plain, and a wrapped result codeaf cannot trust
is run again without rtk.

If no rtk is already resolvable and `CODEAF_RTK` is unset, codeaf downloads
`rtk-<target>.tar.gz` and `checksums.txt` from that repository's release. The whole
fetch has two minutes and every downloaded response is capped at 64 MB. Its sha256
must match the published checksums; a mismatch or missing entry is refused. The
managed fetch supports only macOS arm64, macOS amd64, Linux amd64, and Linux arm64;
on any other platform it fetches nothing. A copy you name or put on `PATH` can still
be used there.

The archive contributes only its file named `rtk`. It is installed atomically with
mode `0700` at `$CODEAF_HOME/bin/rtk`, or `~/.codeaf/bin/rtk` when `CODEAF_HOME` is
unset or empty. Nothing waits for it: commands run plain until it lands. Success logs
once per process as `note: installed rtk v0.45.0 for compressed shell output`; failure
logs once as `note: shell output will not be compressed — <err>`.

`CODEAF_RTK=off` disables rtk everywhere. Set `CODEAF_RTK` to an executable path to
use your own; naming a path that is not executable also prevents fallback and fetch.
Without that variable, codeaf looks for `rtk` on `PATH`, then its managed copy. Calls
to rtk carry `RTK_TELEMETRY_DISABLED=1` and `RTK_NO_TOML=1`: codeaf does not opt you
into a third party's collection.

## Running codeaf from the terminal — can I run this without the chat, every verb a terminal takes

Typing `codeaf` with no arguments opens the conversation. Everything else is a verb after
it, and there are five kinds:

```
talk to it              chat · resume
hand it work            do "<task>" · exec "<prompt>" · run <program>
look at what happened   why self · why <task-id> · notebook · competence · services ·
                        logs · models · doctor · manual · version
housekeeping            connect · disconnect · cache · cache clean · rebuild ·
                        serve · pair · devices · help env
plan work by hand       plan new "<goal>" · plan show <plan.json> ·
                        plan revise <plan.json> "…" · plan run <plan.json>
```

One more verb is the plan store the bash-belt task worker coordinates through, exposed
to a person: **`codeaf plandb`** answers the same commands the worker runs in bash —
`codeaf plandb list --status ready`, `codeaf plandb task overview`, `codeaf plandb
critical-path` — against the run's own `plandb.db`, so a plan a worker is driving can be
read the way the worker reads it.

Five more exist and are deliberately kept out of the help text, because nothing types
them by hand: **`codeaf app`** is the backend the codeaf desktop app starts and talks to
over its own pipes, holding every agent the app shows in that one process;
**`codeaf engine`** is the far half of `chat --host`, started by ssh, and
**`codeaf clock`** is the one process that runs your automations while a codeaf window is
open — a window starts it, and it leaves by itself half a minute after the last window
closes. Neither draws anything or reads a key. **`codeaf tick`** and **`codeaf wake`** are
kept out of it as well, because they do nothing any more (see *codeaf wake and codeaf
tick* below).

## The desktop app's command became a background process — its work log link

When a command becomes a job, the desktop backend names that process on the
command's finished work-log row and on the same row read back from history.
This includes `background:true`, a foreground command kept past its session
clock or timeout, a command kept by a steer, and a declined `keep_running`
request run as a job for that turn. Waiting for approval does not break the link.
A kept service is linked only where the backend publishes its process row.
Each command keeps its own link; starting another command does not carry an
earlier command's link.

The link comes from the engine's job or service registry. A normal foreground
completion, failed call, refusal, argument rejection or interrupted command
gets no process link, however often its output mentions a job. Older records
without the fact leave the link absent; the backend does not guess from words
or from the call's clock.

## The desktop app's keys, day cap and budget asks — what codeaf app keeps

Keys pasted into the codeaf desktop app are kept in the Mac's keychain, not by codeaf.
The app hands them to `codeaf app` at every launch and that process holds them in memory
only: while it runs, a key never reaches the profile's `config.json`, and a provider the
app disconnected is not quietly reconnected from a key in the profile or the shell. A
rejected key is reported in the provider's own words and status code. Which providers the
app connects, and how, is *Connecting a provider from the app* below. The GitHub token
pasted while setting up the app's factory is kept the same way and handed back at every
launch, so the factory stays connected after a restart; choosing gh's own login keeps only
that choice, and gh keeps its token.

The app's day cap is the profile's daily limit, the same one `/budget day` sets: setup,
Settings and the terminal all read and change that one figure, and a cap an older app
kept for itself is adopted into it once, keeping the tighter positive limit. A message sent once today's spending has reached the day cap is not sent at
all; the agent asks instead, `Day cap reached · $40.00 of $40.00`, with *Raise for today*
(back to the set figure at midnight) or *Wait until tomorrow*. An agent with a budget asks
once it has spent four fifths of it — `It's close to its $5.00 budget` — and again when it
reaches it, offering *Raise budget*, *Stop* or *Tell it something*. Raising either limit
runs again the message it refused. Neither limit stops a turn halfway: a turn that crosses
one finishes, and the next is the one that waits.

The app's own settings (new agents' folder, the 12-second proposed-task clock, 7 idle
days before an agent reads closed) and its setup progress live in `app/state.json` under
codeaf's home folder, so quitting setup halfway resumes there; the day cap is not among
them, because it is the profile's.

## Connecting a provider from the app — Ollama, a custom endpoint, Codex, regions

The app's setup and its Settings offer every provider `/connect` offers, in the
same order, and ask each for what `/connect` asks: OpenRouter a key or a browser
sign-in; DeepSeek, MiniMax and ai& a key; Z.ai, Moonshot and Alibaba Qwen a region
(International or China) and then a key; Ollama and a custom OpenAI-compatible endpoint
an address, with a key only if they want one; Codex the ChatGPT plan sign-in in the
browser and nothing else. Setup connects as many of them as you like, one after another,
or none at all, and Settings connects one later; with none connected, setup says there
are no models yet instead of offering a default.

OpenRouter's key is checked with one request and ten seconds to answer. Every other
provider is checked the way `/connect` checks it: its model list, a one-token call where
it has none, and for a provider with a coding plan each way of paying in turn, plan
first. What the check found besides the key — the region, the plan the key opened, the
address — is kept in the profile where the terminal reads it; the key is not. A provider
that lists no models offers the model `/connect` would move a conversation onto.

The ChatGPT sign-in is the one credential the app leaves in the profile, exactly where
the terminal keeps it, because its tokens change on every refresh; the app keeps no key
for it. One sign-in waits for the browser at a time, for up to ten minutes, and it can be
cancelled while it waits without signing anything out. Removing it in the app signs the
account out of that profile, as `codeaf disconnect codex` does — a sign-in still waiting
is cancelled first, and one already being saved has to finish before it can be removed.
Removing Ollama or a custom endpoint disconnects it.

## The desktop app's processes — stop, rerun, and what a Mac going to sleep does to them

The app lists each conversation's background processes, running and ended, for
seven days, and shows a process's output by line or follows it live. Stop
stops it; Rerun starts its command again as a new process. When the Mac goes
to sleep the app stops every running process and the agent reads asleep; on
waking, each stopped process leaves one line, such as `link checker stopped
while this Mac slept · Rerun`, and nothing is restarted for you.

## The desktop app's work log — what each step says, and a command's whole output

Each step in the app's work log says its kind (read, search, command, edit,
web, an account, a task, or a step that asked you first), when it started and
ended, which steps the agent started together, a command's exit code, and
whether it repeats an earlier command of the same reply. An edit says the
lines it added and removed. A command still going shows its last three lines,
and twelve once it ends. A command's whole output is kept on this Mac for a
week under the app's folder in codeaf's home, so it can be opened after the
reply ends, or after the app restarts.

## The desktop app's conversation — steer or queue, the decision dock, and notes to a task

In the desktop app a message typed while the agent works can steer or queue.
A steer reaches the agent after its current step, like a correction typed in
the terminal; if the reply ends first, the words are sent as the next message.
Queued messages wait in order until the reply ends; they can be edited,
reordered or taken out until then. Thinking shows while it happens and is not
kept. Words the agent says before a step show as an update.

The dock above the message box shows one question at a time: why it is asked,
what it shows, the agent's suggested answer, the tasks waiting on it, and a
clock only when the question has one. Later puts it aside; You decide takes
the agent's suggestion. Commands asked about together are answered together
or one by one. A note to a task says when a step of the task read it, and a
task can be paused, resumed or tried again from its page.

## The desktop app's Inbox — every open question in one list, Undo, and Later

The app's Inbox lists every question any agent the app holds is waiting on,
each with the two turns before it and where it comes from. An answer given
there waits five seconds before it is sent, and Undo takes it back. Later puts
a question aside without answering it. A question answered anywhere else, in
the conversation, on a task's page or in the terminal, leaves the Inbox at
once with its receipt. An agent with a question open is kept open until the
question is answered.

## The desktop app's crews — they are your teams

A crew in the desktop app is a codeaf team: the same teams, members, handles,
manager, sub-teams, daily caps, team log and decisions you see in the Teams
page, under another name. A crew made in the app is a team in the terminal,
and the other way round. While the app is open it does for a crew what a
terminal window does for a team it holds: it opens the member a manager starts
with `team_start` and stops the one a manager stops. Members with live work,
questions, running factory stages, processes or work awaiting recovery are opened
at startup. Once opened, a crew member stays held across its engine closing or a
factory stage resuming, so its questions still reach you. Idle
members stay as saved records until you open their chat, send them a message,
or crew traffic wakes them; a member woken by its manager is seen working.
When the app and a terminal both hold one manager, the first to claim a start
opens the member and the other does nothing, so one `team_start` makes one
member. Each member's row shows the step it is on, or the question it waits on.

A question that climbs to you, a cap reached and a closing report arrive in the
app's dock and Inbox as decisions; your answer decides them for the team.
Closing a crew does not stop its conversations.

A few things exist only in the app and the terminal never shows them: a crew's
goal and end date, linked jobs, issues and pull requests, roles (instructions
and a model sent to a member as a message), a member's own budget, pausing a
member (its own turns are stopped until you resume it), and an agent's
proposal to bring in helpers.

## Idle desktop chats, recovery checks and crew traffic

While the desktop app is open, startup checks every saved candidate for work
awaiting recovery. After startup, chats with live factory stages, kept or
starting services, crew holds or due retries are checked every two seconds.
Chats seen live in the last 10 minutes also keep these prompt checks after
closing. Crew traffic and team changes retain their immediate file-watch checks.

Other closed chats are checked in small batches, so every quiet chat's presence,
tasks, team and process files are checked within a minute, allowing one extra
two-second tick at the boundary. New work wakes the chat on its next check.
These checks also find missed changes when file watching is unavailable or a
work directory disappears and is re-created. Watcher errors still trigger an
immediate crew check. The app watches shared crew directories; saved chats do
not each add file watches.

## Closing, archiving and renaming agents in the desktop app — and finding one

In the desktop app, Move to Closed lets an agent's conversation go and takes it
off the rail; nothing in it is deleted, and opening it or sending to it opens
it again. Archive closes it and puts it away with the same archive mark the
terminal's conversation list uses, so it is listed only when you ask for
archived agents; Reopen brings either back exactly as it was. Select idle picks
the open agents nobody has spoken in for the idle days in Settings (seven
unless you change it). Rename gives an agent the name you type, and codeaf never
renames it after that. Search (⌘K) finds agents and crews by name, tasks by
title, and the words of every conversation.

## The desktop app's Settings window — what each row changes

The desktop app's Settings window reads and writes the same places the
terminal does wherever there is one. The default model is the profile's chat
model; the Checks and Planner models are the crew's high and planner rows (the
ones /crew changes), and leaving them empty follows the default; the fallback
model is the profile's fallback list. Day cap, per-agent budget and task cap
are the spending limits; the proposed-task clock also times a choice with a
suggested answer. Appearance, text size, open at login, the dock badge and the
three pinned models (⌥⌘1–3) are the app's own. Permissions lists every
always-allow rule, a tool's or a command pattern's, and removing one takes it
out of the profile; Memory lists what codeaf remembers and forgets a line on
request; Export writes a CSV of every billed call between two days into the
app's exports folder. Signing in to GitHub is not in the app yet.

## Quitting the desktop app — agents pause and resume, processes stop

Quitting the desktop app does not fail the work in it. An agent in the middle
of a reply is stopped where it stands and carries on by itself the next time
the app opens, with "Resumed after quit" in front of it; a task it had running
is picked up where its record stands. A process the model started is sent
SIGTERM, then SIGKILL five seconds later, and the Processes list says "stopped
by quit". Automations are not touched by a quit and fire again once the app is
open. A conversation the app joined in a terminal window is that window's, and
quitting the app only lets go of it.

The app waits for local saving, publishes the newest saved turn and releases its
hold so another computer can continue. Each saving boundary waits up to 30 seconds.
A successful save is quiet; a definite final-save failure is kept in the engine
log and leaves unsaved turns on this computer.

Crew members follow the same quit and resume rules. A member still opening
does not hold up quitting for optional file cleanup; its files are kept.
Closing the desktop's input pipe starts the same bounded shutdown as a first
SIGTERM, including while the engine is waiting for a lock. A second SIGTERM
ends the process immediately.

## When the desktop app closes unexpectedly — a reply stuck working, carrying on after it closed

If the desktop app closes unexpectedly during a reply, opening it again carries
the reply and its running tasks on from where their records stand.
"Resumed after the app closed" appears before that reply. Finished steps are
not repeated; the agent checks the result of an unfinished step before relying
on it. Another window still working on the conversation is left alone. An idle
conversation without a promised reply stays idle.

If the app closes again before its promised reply begins, opening it again
tries that reply with the same notice. Newer task activity keeps that promise's
original notice and age. Another reply starting does not cancel
that promise. A message, correction, queued message or answer from you cancels
that promise, including a message sent in another window while the app was
closed. A deleted conversation never carries on.

A conversation carries on automatically at most twice in a row. A reply that
finishes normally, or a direction from you, resets that limit; the agent's own
notes do not. Compacting or rewinding the conversation does not reset it.
Retrying a promised reply uses an attempt too. On the third
opening within ten minutes that finds unfinished replies, none carry on
automatically. A reply last active more than an hour before opening is left
for you to choose. For a promised reply that never began, the hour starts when
that promise was first saved, even if its tasks worked again afterwards.

When work is left for you, its chat says "The app closed while this was working · send a message to carry on".
Send a message to continue. These lines, and "Resumed after quit" after a normal
quit, stay in the conversation when you close and reopen the app.

## Reopening the app while the last one is still leaving — your factory isn't answering, another codeaf is still running this factory

When the app closes, or its window is killed, the codeaf behind it leaves by
itself: it never keeps working without the app. It lets go of this machine's
factory first (a running job stops the way a quit stops it and goes back to the
queue), then stops its replies and processes, which carry on when the app opens
again. Leaving takes at most two minutes, including the final saving waits.
A second SIGTERM or SIGINT ends it at once.

If the app is opened again before the last one has let go, the factory on this
machine is listed as not answering with the words "Another codeaf on this
machine is still running this factory. It comes back here once that one
stops." The next look at the factory list after that serves it here again,
with nothing to set up. The same words appear while a terminal's
`codeaf factory serve` holds this machine's factory; stop that one, or start
the factory in the app to use it through that codeaf.

## The desktop app's clocks — a proposed task, and a choice with a suggestion

In the desktop app a proposed task waits on a clock (twelve seconds unless you
change it in Settings) and starts on its own when it runs out; its receipt
says how long the clock was, as in "Started on its own after 12s". A choice the
agent asks with a suggested answer runs on the same clock and takes the
suggestion when it runs out ("Picks Suggested in 12s"); holding the card or
typing in it stops the clock. A choice with no suggestion, anything
irreversible, and a project that set its own rule for choices never run on it.
In the terminal neither clock applies unless you set one with /autonomy.

## Pausing a handed-over task in the desktop app

A reply you moved to a task, and any task codeaf handed over to run on its own,
can be paused from the app like any other task: nothing new starts under it and
the worker stops at its next step until you resume it. Its page shows the
worker's steps as its log, and a note you leave shows when the worker read it.
A task is never renamed after a single file it touches.

## A conversation open in a terminal and in the desktop app at once

When the app opens an agent a terminal already has open, it joins that
terminal's conversation instead of opening a second copy, even when the
conversation lives in another folder's terminal (a crew member a manager
started). If it cannot reach that terminal it says the conversation is open in
another codeaf window rather than opening it twice.

If that window accepts a connection but does not answer, the app waits up to
five seconds across all candidate windows, then shows “Couldn’t open chat”:
“This chat didn’t answer from the other codeaf window. Try again, or continue
there.” Choose “Try again” when that window answers again. A failed open sends
no message, takes over no work and leaves the terminal's conversation alone.
The five-second limit ends with the handshake; joined replies can run longer.

A joined conversation keeps the rules of the window that opened it, because
they are how it behaves for everyone: a proposed task's clock, a choice's
clock, moving a running reply to a task, naming, and saving proposed reminders
as standing orders follow the terminal. One thing changes while the app is
joined: a command's output streams as it arrives, in the app and in the
terminal's row for that command. The app still says the agent is working
whenever a turn runs, whoever started it.

## Standing orders in the desktop app — agree to a reminder or routine before it runs, and how it earns trust

In the desktop app, a reminder or a piece of scheduled work an agent proposes
is saved as proposed the moment its card appears, and NOTHING RUNS until you
agree: Save on the card, or Ratify in the orders list, makes it active; Don't
save or Not now takes it away; a card left unanswered keeps it proposed. These
are the same automations `/automations` lists in the terminal.

Scheduled work proposed this way asks before each run: when it comes due it
waits for your Go, Skip or Not right (showing its budget per run), and later
times are passed over while it waits. Five Go's make it trusted, and it runs
alone after that; one Not right puts it back to asking first. Run now counts as
a Go. Reminders and watches never ask. Pause keeps the trust; Stop ends it for
good and keeps its history. Each order says where its results go: this chat,
the crew, everywhere, or not here.

The app keeps the clock running while it is open. Nothing runs while it is
closed or the Mac sleeps; a time missed then runs once, late, and the app says
it was missed. A run waiting for your Go shows as queued in the terminal's
list, and only the desktop app can answer it.
A third, **`codeaf cell`**, is typed by hand but is not in the help text: it works
by default on Linux and macOS on every build. Native Windows is supported too; a compatible furrow engine is required. Its verbs are described under "The cell verbs" below.

## codeaf pair — show a code, or use one, from the terminal

**`codeaf pair approve <link>`** compares the four-digit check number and asks `y / n`: only `y` or `yes` approves, and `d`, Esc, Enter or anything else decline. Code pairing keeps its `y / n` question.

**`codeaf pair`** on a new computer asks to join your devices and shows a link to approve from a computer that is already in (`codeaf pair approve <link>`); **`codeaf pair --code`** shows a code that gives your chats to another computer. **`codeaf pair <code>`** uses a code that another computer showed, for example `codeaf pair 42-715-302`. `CODEAF_SYNC_URL=<url>` or `--via <url>` keeps the invitation on that service's own pairing page and includes `--via` in the token command; it never prints the hosted page link for a self-hosted invitation. `--via <url>` names the sync address to go through, and ordinary pairing keeps and shares a solo computer's own chats, history, saved keys, memory and preferences. Terminal pairing prints “Sharing finishes the next time codeaf runs.” Start codeaf again to share existing chats, saved keys, memory and preferences in the background; reopen after an interruption to resume. A used computer already sharing another set refuses and changes nothing. Only explicit `--replace` chooses a clean slate, with its existing confirmation. The page *Pairing your chats with a second computer* has the whole story.

## The architecture map — codeaf atlas [map], /atlas [map], what talks to what, how two computers pair and continue a chat

The atlas draws a picture of the parts behind the two-computer work — the
pairing, the devices, the chats that move, the sync and the furrow engine — as
boxes and arrows, with flows you can step through. It opens no conversation, makes no
model request and spends nothing; it is a map of the code, not a control. The old
Bun + OpenTUI version that lived in `tools/atlas` is gone: everything goes
through Go, from the one model and one registry of maps inside the binary.

Maps are named. **`codeaf atlas pairing`** opens the pairing map — codeaf on
two computers, pairing with a link and continuing a chat on the other — and
**`/atlas pairing`** opens the same map inside the chat, over the conversation
you are typing in, on the same terms: no model request is made, nothing is spent, and
`esc` or `q` closes it and gives the conversation back exactly as it was, with
the transcript, the draft and everything you had open untouched. A box you
dragged stays where you put it the next time you open the map.

Bare `codeaf atlas` and bare `/atlas` open a **picker** instead: one row per
registered map, its name and its one-line description. `enter` opens the row
the cursor is on; `esc` or `q` leaves without opening anything — back to the
shell, or back to the chat. `tab` completes map names after `/atlas`, the way
other commands with arguments complete theirs. A name that is not a map exits
the CLI non-zero with `no map "<name>" — available maps: …`; in the chat the
same line lands in the transcript like any other slash error.

## Architecture map keys — moving boxes and stepping through a flow

Once a map is open:

- Drag a box with the mouse to move it; the arrows follow.
- Click a box, or `tab` then `enter`, to open what it does, its files and what
  it talks to; click again (or `enter`) to close it.
- `1` `2` `3` or `f` open the flows; `←` `→` (or `h` `l`) step through one,
  and `p` plays it. `0` goes back to the overview.
- `?` shows the keys and the colour legend.
- In the chat, `esc` or `q` closes the map and returns to the conversation; in
  the standalone command, `q` or `ctrl+c` quits and gives the terminal back.

It is meant for a terminal of 120×35 or larger and still works at 80×24.
`codeaf atlas --help` answers with its usage like every other command.

The map uses the same Display `step icons` choice as the chat. Rich, plain and
linear glyphs are resolved from one shared vocabulary; changing that choice changes
the map's marks too.

## Where the architecture map's engine source lives

The engine's authoritative source is Agent-Field/furrow. The Rust engine boxes
point to this build's local pin and Go callers rather than a vendored engine tree.
Other boxes name files inside this repository. The pin is the source of the engine
version; the map does not keep a second version number.

## The cell verbs — list a cell's turns and rewind it

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first. They need a compatible furrow engine. `codeaf cell` is hidden from the
help text. **`codeaf cell log [<cell>]`** lists a cell's
turns, newest first, one line each (turn, parent, time, trigger, tools, receipt; a setup turn says `Setup` and its calls are marked `[external]`), and
**`codeaf cell rewind <turn> [<cell>]`** puts the cell's files and transcript back to that
turn. A rewind adds a new turn on top of the newest one and deletes nothing, so it can be
rewound too. It refuses while a tool call began and never finished. With no cell named,
both use the cell the current folder is in.

## Do moving chats restrict commands, network access or installs

New chats declare `host-bound`: shell commands run on this computer with the network and the same access as any program you run. Moving chats does not change that access. `/setup` compares the tools a chat used with this machine and can install missing tools or rebuild folders left behind by a move; it is not needed for ordinary network access. A cell already declared sandboxed by an earlier build keeps that class.

## Your identity — codeaf identity show, export and import

Your identity is the one root secret codeaf keeps for you on a machine: a signing key, a
secret for deduplicating file content, and the key your saved secrets are sealed under. It
lives in `identity.json` in the codeaf home, readable by you only. There is no account.
These verbs are hidden from the help text and are available by default on Linux, macOS and native Windows with a compatible furrow engine. An older engine says `moving chats needs a newer furrow engine than this build carries`.

**`codeaf identity show`** prints your public id, a short fingerprint you can compare by
eye between two machines, the cell key id, and this machine's device id. It makes the identity the first time it runs.

**`codeaf identity export [<file>]`** asks for a passphrase (typed twice at a prompt, or one line
piped in) and writes your identity wrapped under it, to the file or to the
screen. The blob is safe to copy; without the passphrase it opens to nothing. Keep the
passphrase: losing it and every machine loses the identity.

**`codeaf identity import [--replace] <file>`** asks for the passphrase and makes that
identity this machine's. Afterwards `show` prints the same id and cell key id on both
machines. It refuses to overwrite a different identity already here unless you pass
`--replace`, and after a replace the secrets sealed under the old identity can no longer
be read. A wrong passphrase and a damaged blob get the same refusal.

## Replacing an identity — rotate, device keys and an older vault

**`codeaf identity rotate [--grace 7d] [--yes] [--abandon]`** replaces your identity on every
computer you keep: a new root, every chat and your saved keys sealed again under it, and the
old identity deleted by the relay after the grace period. It is how a lost computer is locked
out for good. It asks first and says what it costs; `--abandon` stops a rotation that has not
switched yet. Read *Locking out a lost computer for good*.

Each machine also has its own device key, made the first time and never exported. Your
identity signs a small certificate for it, so two machines share one id but keep separate
device ids, and one machine can be told apart from another.

A vault made before identities existed keeps working: its old key becomes the identity's
key the first time the identity is made on that machine.

## List your chats on every machine — codeaf cell list --all, and what its lines mean

**`codeaf cell list --all`** prints every chat you have on any of your machines, one line
each: id, title, device, where it runs (`held on <device>`, `<device> offline`,
`<K> turns from <device>: discard`, or `-` for one held here or let go), and how
long ago its last saved turn was. It needs no terminal UI, so it works over ssh. Without
`--all` it prints the usage line. With sync off (`CODEAF_SYNC_URL=off`, or no relay to sync through) it prints
`sync is off: set CODEAF_SYNC_URL to your sync service's address` and lists nothing. With a relay
set but no identity on this machine it prints
`this machine has no identity yet: codeaf identity import` and lists nothing, and when the
other machines cannot be reached it starts with `the sync service is not answering`.

## A different sync setting on an existing engine

The engine keeps its sync setting from startup. Changing `CODEAF_SYNC_URL` to another address or `off` retires an idle engine before opening a chat and says `the engine restarted to pick up this terminal's sync setting`. If another window or work still uses it, the new window refuses to attach: `the background engine for this folder started with a different environment than this terminal and is still in use — codeaf chat --no-host opens this window on its own and leaves the other one running; or, after work finishes, close every window for this workspace and reopen. To stop now (interrupts active turns and tasks), run codeaf engine --stop --workspace <folder>`. The command names the exact folder.

To use the new setting at once without touching the other window, run `codeaf chat --no-host`, with `--session <path>` to open a particular chat; for example `CODEAF_SYNC_URL=off codeaf chat --no-host`. That window runs its own engine in this terminal and ends with it.

Only a terminal that sets `CODEAF_SYNC_URL` itself is asking for a different relay. A window with the variable unset — `codeaf chat --host` reaching this computer over ssh, whose shell usually does not carry what you exported, or a terminal you never set it in — joins the running engine and its relay as it is. It restarts the engine only when the relay a pairing saved on this computer has changed since the engine started.

## Sync your chats between machines — CODEAF_SYNC_URL and CODEAF_SYNC_INTERVAL_MS

Moving chats and pairing are available by default on Linux, macOS and native Windows on every build with a compatible engine. Pair both computers first.

Older chats move through an independent copy. Once paired, codeaf prepares chats active in the last 30 days in the background, one at a time; opening another older chat prepares its copy then. This build shows and writes to the copy. The original files stay untouched and remain usable in older codeaf builds.

These environment settings are not rows in `/settings`. `CODEAF_SYNC_URL=off` stops relay
traffic; new chats still keep their local cells.

**`CODEAF_SYNC_URL`** is the relay's address, like `http://host:8787`, or the word `off`.
Plain `http` is for trying it out inside an ssh tunnel; later the relay needs TLS. Unset, it
is the relay a pairing saved on this computer, and with none saved it is codeaf's hosted
relay when this build has one (see "Where your chats are stored" in use-this-on-another-computer); a build with no
hosted relay keeps sync off until you name one. `off` turns sync off. Set to something that is not a web address, it stops with
`CODEAF_SYNC_URL is not a web address like http://host:8787`. Set on a machine with no
identity, a headless sync command stops with `this machine has no identity yet: codeaf identity import`.
Starting the app creates a local solo identity when a relay is configured. It sends nothing
to the relay until pairing; that identity alone does not connect your other computers.

## How often chat turns sync — CODEAF_SYNC_INTERVAL_MS

After pairing, codeaf sends sealed chat turns to the relay configured by `CODEAF_SYNC_URL`.

**`CODEAF_SYNC_INTERVAL_MS`** is how often unsaved turns are sent to the relay, in
milliseconds. The default is `5000`. A longer interval means fewer requests and more turns
that only this machine has if it goes to sleep. It must be a positive number.

Everything sent is sealed under your identity first; the relay holds no names, no titles and
no file content it can read. Requests are signed by this machine's own device key, so the
relay can tell your machines apart. `codeaf cell list --all` and the sessions panel on home
show your chats on the other machines once both machines share an identity and this setting.

## Why did my tool call not run after I continued the chat on my laptop — the view-only window, kept branch and clock warning

With sync on, the chat you are typing in sends each saved turn to your relay, and holds the chat
for this machine while it is open. Closing the chat sends what is left and lets go of it, so your
other machine can pick it up at once. If this machine sleeps or loses its connection, presence stops answering; the other computers mark it offline after 25 seconds of silence, within 30 seconds. The lease itself expires after about ninety seconds, and an explicit continue can take it sooner.
You do not have to wait for that: press enter on a chat that says `held on <device>` on another
machine and continue it there, and it is taken at once (see "Continue a chat here that another
machine left" in home).

When another machine does, this window says `<device> continued this chat; this window now only shows it`,
with the name of the machine that took over (or the first digits of its id when it has no name). From then on
this window only shows the conversation: a tool call the model tries here answers with that same line and does
not run, so the two machines never both change the chat. Nothing on the other machine is overwritten.

Turns you saved here that had not reached the relay yet are not lost. They are kept as a separate chat
that starts from the last turn both machines share, and `codeaf cell list --all` shows it as
`<K> turns from <device>: discard`. Open this chat again and it carries on as that separate chat.

**`codeaf cell discard <branch> [<cell>]`** sets such a branch aside: it is archived, not deleted, and it leaves
every machine's list. It runs on the machine that holds the chat and says `discarded <branch>`; on any other
machine it refuses, naming the chat whose hold is missing. (The branch row on home discards from any of your
machines.) **There is no `codeaf cell merge` in this version:** folding a branch back into its chat is not
offered anywhere, so a branch row says `discard` alone.

If this computer's clock is more than five minutes off, sync shows `this computer's clock is off by more than 5 minutes`
once and tries again on the next round; fix the clock and it carries on. With sync off none of
this exists and the chat behaves as it always did.

## What syncing a conversation cost — how much data it uploaded, codeaf cell report, and sending the numbers only if you choose

Each time a conversation is synced to another machine, codeaf adds one line of counts to a file on this
machine, `v3/sync/stats/<cell>.jsonl` in the codeaf home: how many turns, frames and objects that flush
carried, how many bytes went up and came down, and how many put, get and has requests it made. Requests that
no flush carries are counted too, in a line with a dash for its turn: what it cost to fetch a conversation
when you continue it here, and what a publish that failed had already sent. Sending your secrets along with
a conversation is counted apart, in `v3/sync/stats/vault.jsonl`, because it serves every conversation. A line holds
counts only: no time, no file names, no titles and no content. The file never leaves the machine on its own.

**`codeaf cell report [<cell>]`** (when cells are available) prints one row per flush and a total row for the
cell, or for the cell the current folder is in, then a `vault` row for what carrying your secrets cost on this
machine. The vault row is never added into the cell's total. Every request this machine made to the relay is
in exactly one row, so the rows of all conversations plus the vault add up to what the relay counted for
this machine. A cell that has never been synced has no rows, and
with no vault row either the report prints nothing at all rather than a table of zeros.

**`codeaf cell report --export <file>`** also writes those same lines to the file you name, and says so:
"counts only: bytes, objects and requests per turn; no content, no paths". Nothing is exported unless you
type `--export`, so a report on its own writes no file and sends nothing.

## Free disk from old conversations — codeaf cell gc and the cell disk budget

**`codeaf cell gc [--dry-run]`** (when cells are available) frees disk from finished
conversations. A conversation that codeaf keeps its own working folder for has files that
can always be written back from what codeaf sealed. `gc` removes those working files of
finished conversations, least recently opened first, toward the disk budget:
the `cell disk budget` row in settings, or `CODEAF_CELL_BUDGET_GB` for one launch, in
gigabytes (a fraction such as 0.5 works), 20 when unset, 0 for no limit. The settings row is absent when cells are unavailable. `codeaf doctor`
shows a `cells` row with what they hold against that budget. Only a conversation nobody is using and that is
fully sealed is touched. One that is open in any window, one opened in the last ten
minutes, and one with a call still in flight or not yet sealed are passed over, and `gc`
says why beside each. Your own project folders are never removed: a conversation
working in a folder of yours is passed over with "workspace is yours", however much disk
you are short of. What stays is the conversation itself (its transcript and history),
so it still lists and opens, and opening one writes its files back, byte for byte. Nothing is removed automatically when opening a chat, while idle or on a timer. The budget is the default for explicit gc only. A fresh snapshot is checked by restoring it into a temporary folder. Only top-level entries that match that restored copy byte for byte are removed. Withheld secrets, `.furrowpolicy` exclusions, rebuildable dependency folders and any directory containing excluded files stay in place. The policy file and the lockfiles (or their containing folders) stay too, so reopening preserves those exclusions. `--dry-run`
removes nothing and prints the exact top-level paths that would go (it still checks a fresh capture):

```
cells hold 24576.0 MiB, budget 20480.0 MiB
  01M3NZ9GCNB38JGMWYVG7V6WWC  6144.0 MiB  would evict
    captured.txt
    src
```

## Connect from the terminal without opening the chat — codeaf connect and codeaf disconnect

`codeaf connect` lists every model provider this profile knows, whether it is connected,
and whether its door is a browser or a key. `codeaf connect codex` signs a ChatGPT plan
in through the browser; `codeaf connect openrouter` uses OpenRouter's existing browser
road. Add `--no-browser` to print the address without opening it. For Codex on another
machine, the next line gives the tunnel to run before opening that address here:

```
ssh -L 1455:localhost:1455 <that machine>
```

If that sign-in chose port 1457 instead, the printed command uses 1457. DeepSeek and
MiniMax take a key through the same checked connection as `/connect`; Ollama takes none.
Z.ai, Moonshot and Qwen take a key and also need `--region intl` or `--region cn`. A key
is read from stdin when it is piped, or asked for without echo on a terminal. A new custom
provider is created only in the chat: an unknown custom name says it is not a provider this
profile knows. Once the chat has created one, `codeaf connect <its name>` can reconnect
that instance with a key.

`codeaf disconnect <provider>` says `forget a provider and the key or sign-in behind it`
in its help. The command forgets that provider and its key or sign-in. Neither
command sends a prompt, calls a model or adds model spend. They do not print keys or
tokens.

## The belt's hands from a shell — codeaf patch, codeaf doc, codeaf web fetch, codeaf web search, codeaf image

Four verbs reach, from a terminal, the same hands the conversation's model uses — each
through the same code path the tool on the belt runs, so the two cannot drift:

- **`codeaf patch FILE --old TEXT --new TEXT`** is the edit hand's exact-match
  replacement: the old text must match exactly one region of the file, the file is
  rewritten with that one region replaced, and a refusal names how many regions matched
  and leaves with 1 without writing anything. `--old-file` and `--new-file` read the two
  texts from files when they are awkward to quote.
- **`codeaf doc PATH [--pages A-B]`** reads a document the way the conversation's
  `read_document` does: locally and free when the file has a text layer, otherwise
  through the billed parser rungs on your profile's key. A plain text file is printed as
  it is, with no call at all. `--pages` names pages of a PDF the local rung reads; billed
  text arrives with no page boundaries, so a range on a scan is refused.
  `codeaf doc --help` prints `codeaf doc PATH [--pages A-B]` and the flags without
  reading a file or making a model call.
- **`codeaf web fetch URL`** and **`codeaf web search QUERY`** are the belt's web verbs:
  one page fetched with the markup stripped and bounded the way `web_fetch` bounds it,
  or one search rendered as the numbered list `web_search` renders, on whatever provider
  the settings name.
- **`codeaf image PROMPT --out PATH [--model M]`** generates one picture where the
  `generate_image` tool does and writes it at PATH, printing the path. The spend is
  recorded the way the tool records it — one row in the usage ledger — so a run's books
  see it.

`patch` spends nothing, and so does `doc` on a plain file; `doc` on a scan, a search or a
fetch on a keyed provider, and every `image` call the model and are billed like any
other call.

## What codeaf --help prints — the six groups, and where the environment table went

`codeaf help`, `--help` and `-h` all print the same thing: every command under six
headings, in this order — **Talk to it**, **Hand it work**, **Hand it a whole task**,
**Look at what happened**, **Housekeeping** and **Plan work by hand** — then five worked
examples. **Hand it a whole task** lists the programs this build carries, such as
`codeaf senior-dev`, `codeaf sec` and `codeaf review`; a build that carries none
prints the other five.

**The environment table is not on that page**: it is `codeaf help env`, because it is a
reference somebody consults and it used to be more than half of what `--help` printed.

Every verb also answers `<verb> --help` with its own line and its flags, and `codeaf plan
--help` answers with all four of its subcommands.

The top-level synopsis includes `do` worker limits and model choices (`--slots`,
`--best`, `--cheap`, `--pin`, and `--check-model`). It also names chat
`--max-cost` and `--max-hours`; those unattended-work limits require `--yolo`.

## Conversation time limit — --max-hours

When the conversation reaches `--max-hours`, its delegated runs, background jobs,
and automatic memory or title work stop before the terminal error appears:
`conversation time limit reached · start a new conversation or relaunch with a larger --max-hours`.
The stopped run records `a time limit you set stopped it`. Start a new conversation
or relaunch with a larger limit to do more work. Closing a window is different: it
detaches the window and does not itself stop delegated work.

## What $? means after a headless one-shot — the codes it leaves with

**One table, and `codeaf do`, `codeaf exec` and `codeaf run` all leave on it.**
This is what `$?` holds after a one-shot, and it is the thing a script should branch on:

| `$?` | what it means |
| --- | --- |
| 0 | it is done, and what is on stdout is the answer |
| 1 | it could not be run at all — no key, bad arguments, the store would not open, the name is not a program |
| 2 | it ran and did not finish: part of the work does not stand |
| 3 | a limit you set stopped it — the wall, the token budget, the turn cap, the price |
| 4 | it needs an answer from you and nobody was there |

The three commands used to have three tables, and two of them meant opposite things by the
same number: `do` exit 1 was "nothing usable came back" and `codeaf run` exit 1 was "it
could not be run at all", while `exec` returned 2, 3, 4, 5 and 6 and never returned 1. If
you have a script written against the old numbers, `CODEAF_EXIT_CODES=legacy` puts `exec`'s
back for one release — see below — and the rest is in
`docs/design/polish/envelope-and-exits.md`, which sets old and new side by side.

**A wall on the default run road leaves with 124.** When `--timeout` ends a `codeaf do`
on the worker harness, `$?` is 124 — the number `timeout(1)` uses — rather than 3, and
`stop` still says `deadline`. The run engine reports a wall and a failed part with the
same word, so the number is where the two are told apart.

**Exit 3 and exit 2 are different questions.** 3 says the work was going when something you
set cut it off, so raising `--timeout`, `--token-budget` or `--max-turns` and running it
again is the remedy. 2 says it got to the end and part of it does not stand, so what came
back is worth reading before anything is re-run.

**A delivery nothing checked is exit 2 too, and it says so.** `codeaf do` puts what a run
delivered to a final check; when that check cannot be reached — a dead route, a refused
account, a service that is down — the work still ships (holding a finished deliverable
hostage to the weather helps nobody), and the run ends `unchecked` rather than `ok`. The
gate is asked once more first, inside the wall, unless there is no time left for a call or
the refusal was about the request itself and every endpoint would say the same. The last
line on stderr is

```
delivered without a check: the gate could not be reached
```

with how it was asked and the provider's own sentence after a `·`. `$?` is 2, `stop` is
`unchecked`, `ok` is false, and `--json` carries the whole reason in `unjudged`. **Read the
answer — it may be perfectly good — but nothing has vouched for it.** Two graded runs
ended `ok` at exit 0 over exactly this before the ending had a name.

**Exit 1 means nothing ran, and only that.** It is the rung for a refusal *before* the work
starts — no key, a flag it could not parse, a store that would not open, a saved program by
that name that does not exist. Nothing was attempted, so nothing was spent and there is
nothing on stdout worth keeping. **A run that started and then failed leaves with 2, not
1**, however early it broke: a provider that gave up at turn nine has already cost you
money and is usually holding part of an answer, and the reason it stopped is in
`incomplete` rather than in `error`. So a script may retry exit 1 blind, and must not do
that with exit 2 — read what came back first. `codeaf exec` published mid-run provider
failures as exit 1 for a while; if a wrapper of yours treats 1 as "provider flaked, try
again", that is the line to revisit.

**Exit 4 is the one nobody can fix by retrying.** A headless run has no keyboard, so a
question ends it. The question is printed on stderr verbatim, under `it stopped to ask:`,
and it is in the `blocked_on` field of `--json`. Answer it inside the ask itself and run it
again, or bring the work to `codeaf` where it can be answered.

Asking for help is never a failure: `--help` on any verb exits 0.

## The --json result object — one shape, three commands

`--json` on `codeaf do`, `codeaf exec` and `codeaf run` prints **one object on
stdout, always parseable, printed even when the run failed**. On `do`, that also
includes a refusal after its flags parse, such as a blank brief or conflicting
`--best` and `--cheap`: `ok` is false, `stop` is `error`, and `error` carries
the same refusal stderr prints. An unrecognized flag is refused with usage on
stderr before the command can enter this envelope path:

```json
{
  "ok": true,
  "stop": "done",
  "answer": "…",
  "files": ["notes.md"],
  "error": "",
  "spend_usd": 0.0213,
  "tokens": {"in": 18422, "out": 1130},
  "seconds": 91.4,
  "model": "anthropic/claude-opus-4",
  "steps": 3,
  "run": "0123456789abcdef",
  "calls": 47,
  "rounds": 2,
  "redispatches": 1
}
```

| field | what it holds |
| --- | --- |
| `ok` | the work stands. True on exactly the runs that exit 0 |
| `stop` | why it ended: `done`, `error`, `incomplete`, `unchecked`, `budget`, `turn-cap`, `deadline`, `price`, `question` |
| `answer` | what was produced, in prose. Empty when nothing was |
| `files` | the paths it wrote. Never null — a run that wrote nothing carries `[]` |
| `error` | why it could not be run, including a failed model call on `do`'s default run road. The same reason appears on stderr; a limit that stopped the work is named by `stop` and `blocked_on` |
| `spend_usd` | what it cost, whole, in dollars |
| `tokens` | `{"in": …, "out": …}` — the input and output totals across the run's model calls |
| `seconds` | wall clock |
| `core_done_seconds` | when the requested work was first found done, in seconds from the start; absent when the gate never said so |
| `model` | the model the work ran on |
| `steps` | how many pieces of work ran — `do`'s nodes, `exec`'s turns. A saved program does not measure it: the key is still there, holding `0`, and that `0` is a measurement nobody took rather than a count of none |
| `run` | this invocation's id. It names the folder `--debug` writes into, and every row this run wrote into `~/.codeaf/logs/calls.jsonl` carries it too — so `codeaf logs --run <that id>` is how you get from this object to the calls behind it. Empty on a verb that opened no run of its own |
| `calls` | how many model calls the run made, counted whether or not the call log is switched on. It is the figure you would otherwise count by hand in `calls.jsonl` |
| `rounds` | how many times the run went back for **more work** after looking at what it had. One is the ordinary shape; eight is a run that kept finding more to do, and it is the number that explains a bill nothing else here accounts for. `exec` does not plan and a saved program does not grow, so both hold `0` — a measurement nobody took, the way `steps` does |
| `redispatches` | how many times one of the run's nodes was sent round again **in place** after running out of the room it was granted — back on the queue to carry on from what it had already banked, rather than grown around. Present only when it happened: a run that never re-dispatched a node carries no key at all, and neither does a verb with no plan nodes |

**Within a release a field is never removed and never changes meaning; new fields may
appear.** `stop` is the field to read for *why*; the exit code only says how much is wrong.

**None of the three takes `--yolo`.** That is the conversation's flag, and it means "stop
asking me before each tool call" — these three have nobody watching in the first place, so
nothing in them stops to ask. Typing it is refused by name:

```
error: codeaf do has no --yolo flag — nothing here stops to ask, and --yes-spend answers the one question a run can still stop on
```

`--yes-spend` is the nearest thing to an equivalent on `do` and `run`: the one thing they
still refuse is a plan whose price crosses your limit. `exec` has no such flag — what bounds
one pass there is `--token-budget` and `--timeout`.

`--json` on `codeaf plan new` and `codeaf plan revise` is a different thing: it is the plan
itself, the same bytes `--out` would write. `codeaf logs --json` is a third: one JSON object per line,
byte-for-byte what is on disk.

## Which models a headless run uses — the crew, --best, --cheap, --pin and the daily cap

A headless run has the same crew a conversation's task has: a **worker**, a **planner** and
a **checker**, and every seat nobody pinned is picked for this task from what kind of work
it is. The run says its crew on stderr before anything is spent — every seat, and which
rung answered it — and under it the class the task was read as and the estimate:

```
models: worker z-ai/glm-5.3-flash (routed) · planner z-ai/glm-5.3-flash (routed) · checker moonshotai/kimi-k3 (pinned)
crew: bugfix · worker glm-5.3-flash (openrouter) · checker kimi-k3 (pinned) · est $0.023
```

A seat you pinned reads `checker kimi-k3 (pinned)` on the headless crew line.
When the run ends, the `crew:` line is said again with what it actually cost beside the
estimate: `crew: bugfix · worker glm-5.3-flash (openrouter) · checker kimi-k3 (pinned) · $0.021 (est $0.023)`.

**Every model flag is a one-task pin.** `--model`, `--plan-model` and `--check-model` pin the
worker, planner and checker for this run and no other, and `CODEAF_MODEL`,
`CODEAF_PLAN_MODEL` and `CODEAF_CHECK_MODEL` do the same from the environment. Each seat
climbs its own ladder — the flag, then its variable, then a `/crew pin` in the profile,
then the crew picked for this task — and the rung that answered is named beside the seat
(`--model`, `CODEAF_MODEL`, `pinned`, `routed`). **The checker never inherits the planner:**
a `--plan-model` says who plans and nothing about who checks.

`codeaf chat --one-model` and `codeaf resume --one-model` keep text calls on the
session model; with that flag, an explicit media-model pin that cannot handle
the operation leaves it unavailable rather than switching models.

`codeaf do` takes three more, all for this one task:

- **`--best`** — the strongest crew your allowed models make.
- **`--cheap`** — the cheapest crew your allowed models make.
- **`--pin seat=model[@provider]`** — pin one seat, `worker=`, `planner=` or `checker=`; say
  it once per seat. `--pin checker=moonshotai/kimi-k3@openrouter` sends the checker through
  that connection. A seat that is not one of the three, or a pin with no model, is refused.

Nothing a headless run is told is written to the profile: the pins and the allowed models
stay what `/crew` last set.

**At the daily cap, `codeaf do` refuses.** When today's crew spend has reached the cap
`/crew cap` set, it starts nothing and says so:

```
today's crew spend has reached the daily cap of $5.00 · raise it or turn it off with `/crew cap`, pass -yes-spend, or wait until midnight
```

`-yes-spend` is the one way past it for this run — past the daily cap, never past the
per-task limit. `--cheap` is refused at the cap like any other run. The day turns over at
midnight on this machine's clock.
**Every run is held to the per-task limit**, $5 unless `/crew cap task` set another: a call
that would take the run past it is not made, and the run stops on
`this task reached its $5 limit · raise it in /crew`. The other headless doors — `exec`, `run`, `plan run` —
are a person at a terminal running one thing, so they **warn and go on**:
`note: today's crew spend has reached the daily cap · this run goes ahead; `codeaf do` would have stopped`.

With `--json`, `codeaf do` carries the crew too: `class` (the kind of work the task was read
as), `crew` (each seat's `model`, `provider`, `kind`, `pinned` and `est_usd`; `crew.<seat>.pinned` is true for a pin), `est_usd` for
the whole crew beside `spend_usd`, `effort` when `--best` or `--cheap` was given, and
`check_model` with `check_model_source` beside the worker's `model_source` and the planner's
`plan_model_source`.

## What checked my unattended or headless run — what judged the delivery, and why task.audit is not the answer

A `codeaf do` errand's delivery is read at the end by the **delivery gate**. It takes a
reading of the project's own checks before the work and another at the end, maps what you
asked for onto the checks that exercise it, and answers whether the delivery holds.

With `--json`, `judged_by` names that reader when the settled root has a gate row that
is not marked unreachable. An unreadable response still names the reader; read `ok` and
`stop` to learn the outcome. `unjudged` instead names an unreachable gate's reason. A run
with no gate row can omit both keys, so absence alone does not prove a check happened.

`task.audit` is a different road's row and does not reach `codeaf do`. It governs work the
conversation hands out with `/task`: a separate, fresh, read-only checker is put in a clean
restore of what the task wrote. Turning that row off produces the report line `nothing
checked this work: the task.audit setting is off`. A headless errand never prints that line,
because the session task engine is not the engine running it; its delivery gate is the
check.

One limitation remains: when a job is broken into several pieces, its gate is journaled
against the piece that delivered rather than the whole that settles them, so `judged_by` is
absent there.

## The old --json field names — deliverable, text, elapsed_ms, settled

**The old names still work, for one release, and then go away.** They are printed beside
the new ones, so nothing that reads them breaks today and nothing has to be rewritten in a
hurry:

| old name | read this instead |
| --- | --- |
| `deliverable` (`do`), `text` (`exec`) | `answer` |
| `artifacts` | `files` |
| `spend` (`do`) | `spend_usd` |
| `nodes` (`do`), `turns` (`exec`) | `steps` |
| `elapsed_ms` (`exec`) | `seconds` |
| `usage` (`exec`) | `tokens` and `spend_usd` |

**`settled` is not the old name of `ok`, and it is not going away.** It means "nothing this
run is waiting for can still move", which is true of a run that asked a question and did
nothing: `settled: true` with `ok: false` and exit 4. Reading the one as the other would
record every refusal as a success. A broken finished tree is the one ending that answers
that sentence false; *Why settled can be false* below gives its exact shape.

## Why settled can be false — the tree does not build or the run left code broken

A run that hands back a tree its own check could not collect is not settled: the tree does
not build, it left the code broken, and repairing it is still work waiting to move. That
run says `settled: false`, `ok: false`, `stop: "incomplete"`, and leaves with exit 2. Its
answer includes the check's own sentence about what could not be read.

## Fields that belong only to one command

Some fields belong to one command and stay. `codeaf do` carries `spend_work` and
`spend_overhead` — what the work cost against what it cost to decide what the work should
be. On the default run road, every call belongs to a run node, including structuring and
checks: `spend_work` equals `spend_usd`, and `spend_overhead` is zero. It also carries
`blocked_on`, `learned`, `plan_model`, `model_source`, `plan_model_source`,
`check_model`, `check_model_source`, `class`, `crew`, `est_usd`, `effort` and `subharness`. It also carries `judged_by` when the settled root records an answered gate
attempt and `unjudged` when that gate could not be reached. Both keys can be absent when
no root gate row is available; neither key replaces `ok` and `stop`. `codeaf run` carries `output`, which is
the typed answer whole, and `report`.

`incomplete` is on `codeaf run` and `codeaf exec` both, and it is why it did not finish, in
the same words stderr carried — a token budget that ran out with half an answer already
written, a wall that arrived, a provider that gave up at turn nine. It is **not** `error`:
`error` means the run could not be started at all, and every one of those started. A run
that was cut short leaves `error` empty, puts what it managed in `answer`, names the reason
in `stop`, and says the sentence in `incomplete`.

## Keeping exec's old numbers for one release — the legacy switch

`codeaf exec` used to leave with 2 for the token budget, 3 for the turn cap, 4 for the wall,
5 for an error and 6 for a run that finished with nothing to show, and it never returned 1.
Those five numbers all moved when the three commands were put on one table.

Setting `CODEAF_EXIT_CODES=legacy` puts them back:

```
CODEAF_EXIT_CODES=legacy codeaf exec "…"
```

**It changes nothing else.** Not `codeaf do`, not `codeaf run`, not one field of `--json`,
not one word on stderr. It is an escape hatch for scripts already written, it applies to
`codeaf exec` and to nothing else, and it goes away after one release. The thing to change
the script to is `stop` in `--json`, which names why a run ended in a word rather than a
number and is the same word on all three commands.

## codeaf wake and codeaf tick — the old background passes

`codeaf wake` and `codeaf tick` were background passes an operating-system timer used to
run; they now do nothing. Nothing is scheduled on the machine any more: automations run
only while a window is open.

Both still answer, quietly and with exit status **0**, because a timer an older version
installed goes on running them until this version's first start removes it — and an
unknown command would exit 1, which the timer reports as a failure every five minutes.
Neither is listed in `--help`, and neither takes, checks or refuses a flag.

## What did that task actually do — see one piece of work's turn-by-turn record, with codeaf why

This prints one piece of work's whole record: every turn, what it said, every tool it
called with its arguments, what came back, and how it ended.

```
codeaf why <node-id> [--db path]
```

Each entry is a headline with its body indented four spaces under it:

```
turn 1 · said
turn 2 · read ←
turn 2 · read → 12ms
turn 3 · bash → 1.4s · error
turn 4 · codeaf
turn 5 · stopped
turn 6 · the record stops here
```

`←` is the call going out; `→` is what came back, with how long it took — milliseconds
under a second, tenths of a second above. `· error` marks a tool that failed. A turn signed with the product's own name is a note
about the run rather than something the model said, and `the record stops here` means the
record was trimmed.

**When there is nothing to print it says which nothing it is**, because those are two very
different situations:

```
build has no transcript: either nothing has run it yet, or the worker that ran it keeps no record.
```

**And it leaves with 1**, not 0. The sentence is for you; the exit code is for the script
that asked, which would otherwise read a success and conclude the id exists and has nothing
in it. `codeaf logs --run <id>` answers a miss the same way.

The id is the one the plan gave that step — the same id `logs --node <id>` filters on, and
the value of the `node` field in `logs --json`.

## The run finished and my directory is empty — where did the work go after codeaf do spent money and wrote no files

`codeaf do` edits the directory you point it at in place; it files nothing anywhere else.
When a run did work but ended without delivering or creating or changing a file, its answer
ends with:

```
No created or changed files were recorded: this run worked in /srv/project, editing it in place.
```

A run that did write instead names every file by its absolute path.

The run's own record is separate. On a run that does not finish cleanly, the
`record kept at <path>` line on stderr names the private store holding its traces and job
logs. The empty file record concerns the project directory. It does not prove the directory is
unchanged: deletions and files outside the bounded workspace scan may be absent. Under
`codeaf do --json`, `workspace` carries the same absolute project directory. It is empty
when an existing resident did the work and this invocation cannot establish its directory.

## Where is the record of my headless run — reading a kept one-shot's store

`why` reads a store, and by default that store is `~/.codeaf/graph.db`. A headless
`codeaf do` run does **not** work there: it uses a separate store of its own, kept only when
the run failed or you asked for it with `--keep`, and the last line on the error stream
says where, and the line above it says how to carry the run on:

```
continue it with: codeaf do --continue 3f81c2
record kept at ~/.codeaf/runs/codeaf-do-3f81c2
```


That directory holds a `graph.db`, and that is what to point the reader at:

```
codeaf why <node-id> --db ~/.codeaf/runs/codeaf-do-3f81c2/graph.db
codeaf why self      --db ~/.codeaf/runs/codeaf-do-3f81c2/graph.db
```

`--db` means the same thing on `why`, `notebook`, `competence`, `services`, `doctor`,
`rebuild` and `do`. **None of them will create a store**: a `--db` that is not a regular
file is refused, each in its own words — `open receipts: <path> is not a regular database
file` from `why`, `open notebook: …`, `open competence map: …`, `open store: …` from
`rebuild`.

**A conversation's tasks are somewhere else entirely.** A task commissioned in the chat
writes its transcript to a file beside the conversation, not into this store, and its room
in the chat replays it. `why` is the reader for headless work.

## Continue a kept headless run — codeaf do --continue

`codeaf do --continue <id>` carries a kept run on: the id is the part of the folder name after
`codeaf-do-`, an unambiguous start of it is enough, and the folder's own path works too. Words
after it are this round's finding (`codeaf do --continue 3f81c2 "also add a test"`). The new run
is handed the original assignment unchanged, how the last run ended and what its plan reached,
and it works in `--dir` (the current directory unless you say otherwise), where the last run's
edits already are. It is a new run with its own record and its own id, and continuing a
continuation still names the first request once. Only a kept record can be continued: a run that
finished cleanly keeps none unless you passed `--keep`. An id nothing matches, or matches twice,
is refused in words. Only the run engine keeps this record, so `--continue` is refused when
`CODEAF_TASK_BELT` selects the older engine.

## Continuing a sealed headless run — its cell and incomplete calls

Cells are available by default on Linux, macOS and native Windows on every build with a compatible engine.
Native Windows is supported too, and a compatible furrow engine is required. `codeaf do`
runs inside a cell, the same kind of folder a chat is, and
the tool calls the run's workers make are sealed as turns, one turn for each set of calls the
model asked for together, taken after the calls' results are written down, and the end of
a worker's turn is sealed after its answer: `codeaf cell log` lists the turns,
and the kept record holds a `cell` file naming the cell. `codeaf do --continue <id>` then goes
on in that same cell from its last sealed turn: its files stand as that turn left them, and the
new run is told how many turns were sealed. A call that began and never finished (the run was
killed in the middle of it) is listed in the new run's brief and is never run again, so the new
run looks at what it left before relying on it. A run's cell is host-bound, so every call it
seals counts as external. A record from a run made without cells has no cell and continues
from the record alone, as before. Without a compatible engine, `do` continues from its kept record as before.


## What have I spent today — the receipts, with codeaf why self

`codeaf why self` prints today's receipts: what was attempted, what it cost, and what was
learned from it. The day is local midnight to now.

```
codeaf why self [--db path]

TRIED                          COST     LEARNED
Practice parser recovery       $0.31    facts #14,#15; surprise down 12%
Summarise the changelog        $0.0042  nothing
```

- **TRIED** is the intent the work was started with, folded onto one line.
- **COST** is dollars, to cents, and to four decimals when it is under a cent.
- **LEARNED** lists `facts #…` and `skills #…` by their notebook numbers, then
  `surprise down N%` when the work became more predictable. When nothing came of it the
  column says `nothing`, and work that became *less* predictable adds `surprise up N%`
  after it.

**A day with nothing on it says `nothing was tried on its own account today.`** It used to
print the `TRIED  COST  LEARNED` header with no rows under it, which reads as a table whose
rows failed to arrive rather than as a quiet day.

## What has it learned — reading and retracting beliefs with codeaf notebook

`codeaf notebook` prints every belief in the store, newest last, with the evidence for it:

```
SEQ  SCOPE      KIND        AGE     USES  RIDES  BAD  STATUS       BELIEF
#14  tool:git   lesson      2d ago  6     4      0    active       always verify changes
#15  user       preference  5h ago  1     0      0    quarantined  prefer compact tables
```

`USES` is how often it was retrieved, `RIDES` how often it rode along into a piece of work,
`BAD` how often that work then failed. `STATUS` is one of `candidate`, `active`,
`superseded`, `quarantined`. Under the table is the day's spending line — one of
`daily rail: unlimited`, `daily rail: $500.00`, `today's spend: $1.23 of $500.00 daily rail`
or `today's spend: $1.23; daily rail unlimited`. A day that has spent nothing prints no
figure for it. Scope renamings follow under `aliases`, as `from → to`.

**To take one back:**

```
codeaf notebook retract 15      quarantined #15: prefer compact tables
codeaf notebook restore 15      restored #15: prefer compact tables
```

Retracting hides a belief from everything that would otherwise reach for it; restoring puts
it back. Nothing is deleted and the number never changes. A number that is not there is
refused with `notebook fact #15 not found`, and anything that is not a positive number with
`invalid notebook fact sequence "x"`. The `#` is optional. Any other word gets
`unknown notebook command "forget"`.

## What is it actually good at — the measured evidence, with codeaf competence

`codeaf competence` sorts what this machine has evidence about into four groups, printed in
this order: `strong`, `frontier`, `weak`, `stale`.

```
strong
  repo:/work/parser — 24 runs · 4% failed · surprise flat · 2 installed skills
frontier
  linear work — 6 runs · 33% failed
stale
  repo:/work/old-api — last touched 3w ago
```

Each row names what the evidence is about, then the evidence: how many runs, what share of
them failed, which way surprise is trending, and how many skills are installed there. A
scope with skills that has never been exercised says `installed, not yet exercised` instead
of a failure rate. A `stale` row says only when it was last touched, because an old
measurement is not a measurement.

`--model <slug>` picks whose measured behaviour to fold in; left alone it is the model this
machine would run work on. **On a machine with no evidence the whole answer is one line:**

```
No competence evidence yet.
```

## What has each model proven — checked results, with codeaf competence --json

`codeaf competence --json` prints the desktop Competence map: **which model passes checks,
and where**, from the checked-results ledger beside spending.

A checked result is one finished task whose declared checks — the tests, lint or build its
proposal named — actually ran against the work and came back with a real exit status. Every
check exiting zero is a pass; any failing is a failure. It is credited to the one model that
answered the task's work, after any fallback, even a free one. A task answered by two models,
that another task joined, or with a check that could not start, is not counted at all. A
checker's opinion, a task that finished with no check, and anything a model says about its
own work are never counted; a check that was killed or cut off counts as failed. Undo does not
appear yet: the only undo there is takes back quick tasks, which run no checks.

Results are grouped by place (the repository or folder) and kind of work (the words of the
task's title) into `strong`, `frontier`, `weak` and `stale`. A scope with no result in the last
30 days is `stale`. Otherwise four in five passing, with at least three results, is `strong`;
two in five or fewer is `weak`; everything between is `frontier`.

`--model <id>` maps that exact model; left alone it is the model with the newest result.
`--scope <id>` narrows to one place, kind of work or scope from the map's own `filters`.

Deleting a source keeps its results. **Earlier work is not inferred**: a new ledger starts
with `"model": ""` and four empty groups. No model is called and routing never changes.
A complete read is bounded to 32 MiB and 10 seconds. Oversized or unreadable history is
unavailable, never classified from a partial subset.

## The Model Pool — what this machine reads from it, with codeaf pool

The public Model Pool is a signed index of measured models. It does not seat
the task crew: codeaf routes that crew from its built-in prior and this install's
task outcomes, and all that routing reads of the pool is the model-name aliases in
the copy built into the binary. Your runs can improve the shared measurements. Nothing about your code ever leaves
the machine — what is shared is a measurement of the run, not the work. One
setting answers for all of it, `model_pool` in `/settings`, with three
values: `on` reads and sends, `read` uses the pool and sends nothing, `off`
does neither. It defaults to `on`. `CODEAF_MODEL_POOL` pins the same word
from the shell, and on a CI machine with neither set codeaf reads but does
not send. **The telemetry off switch stops the pool sending too**:
`CODEAF_TELEMETRY=off`, `DO_NOT_TRACK=1` or the `telemetry` switch in `/settings` caps the
pool at `read` — it wins over an explicit `on` — and `codeaf pool status`
then says `mode read · telemetry`.

```
codeaf pool [show|status|verify] [--json] [--key key]
```

`show` — also what bare `codeaf pool` prints — is the reading form: the mode
and the addresses in force with the word saying where each came from
(`default`, `setting`, `env`, `ci`, or `telemetry` when the telemetry off switch capped
sending), then what index is cached, how old
it is and how many cells it holds, or `no index cached yet · built-in
seed of <date>`. The binary carries a seed index of scored runs, shown until a fresher
signed one is cached. `--cells` lists the held
index's cells, one per line — the role, the model, the dims the cell
spells, the measurement and the installs behind it — and `--json
--cells` carries them as an array. Your install also keeps
the scores its judge gave in `own.json`
under the pool directory — `show` and `status` say what that sheet holds.
Only `codeaf pool` reads that sheet; it does not pick the next crew.

## What codeaf pool status shows — waiting rows, pending judge, last sweep

`codeaf pool status` reports how many outbox rows wait to be sent and whether the mode
allows sending and reading. Its `pending` line can also say `dropped N` and `identity set`;
it counts waiting rows but does not list them. Status also says whether the relay and mirror answered, what the last
judge did, how many runs are `pending judge:`, and what happened in the `last sweep:`.
`--json` prints the same answer as one object; `show` reads nothing off the network.

## What the own sheet stores — scores from this install

**The scores start here.** In a conversation, after a task lands, a model
outside the crew is asked to score each seat the work ran on — the worker that
carried it, and the seat that checked it when there was one. The scores stay
in your install's own sheet (`own.json`). `codeaf pool` reads this sheet for its display;
crew routing does not read it. With `model_pool` set to `on` the same scores also
wait in `outbox.jsonl` beside the sheet, to leave with the pool's other
measurements; `read` keeps them local, and `off` asks no judge at all and
writes nothing. The call itself is billed to the `judge` seat, so it shows up
in the spend pages beside the crew seats rather than inside a task's own
cost. The judge asks with the key the install holds at that moment, so a task
that lands just after you paste a key into first-run setup is scored with that key.
A task that lands while there is no key at all is not judged. It is not marked
judged either, so the next start scores it once a key exists.

With `model_pool` on, the rows leave for the relay after each judged run and
once more at start-up, under this install's own nonce and nothing else. The
index is fetched once a day, checked against the key built into the binary —
or the key in `models.pool.public_key` when one is set — and a changed
document is read at the next start.

## Verify the Model Pool signature — codeaf pool verify

`verify` fetches a fresh index and checks its detached ed25519 signature,
then prints the version, generated date and metric names. For example:

```
signature good: version 1790468332, generated 2026-09-27, metrics acceptable, role_quality
```

It wants a public key: `--key <base64 ed25519 public key>`, repeatable, or
one built into the build. The build carries the index signer's key, so
`verify` works as it stands; `--key` checks under that key alone — the
build's key is not consulted beside it, so a document signed under any
other key does not verify. A fetch or a signature that fails is exit 1; a
`verify` on a machine whose setting is `off`, or whose
`models.pool.public_key` does not decode, is refused with exit 2 and
fetches nothing.

A private relay is a copy of `relay/` deployed to your own account with your
own keypair, and `CODEAF_MODEL_POOL_RELAY_URL` with `models.pool.public_key`
(or `CODEAF_MODEL_POOL_PUBLIC_KEY`) point an install at it.

## What is still running in the background — codeaf services, and stopping one

`codeaf services` lists the long-running processes started here and not yet stopped — a dev
server, a watcher — one per line, tab separated: the name, the status, how long it has been
up, what is watched for health, and where its log is.

A shell command that has already finished keeps any background server or watcher
it left running when the turn ends. Interrupting a command that is still running
stops the command and its child processes.

```
dev-server	running	2h	port:5173	/tmp/dev.log
```

**With nothing running it says `nothing is being kept running.`** It used to print
absolutely nothing and exit 0, which is indistinguishable from a command that broke — so it
answers in a sentence now, the way `codeaf cache` always has.

To stop one, name it:

```
codeaf services stop dev-server

dev-server	stopped
```

A name nothing answers to is refused — `service "dev-server" is not running` — and anything
other than `stop <name>` gets the usage line
`usage: codeaf services [--db path] | codeaf services stop <name> [--db path]`.

## codeaf rebuild — throwing away everything worked out from the journal and replaying it

Everything in the store except the journal was worked out from the journal, and can be
thrown away and worked out again. `codeaf rebuild` is that, and it is the recovery path
when something in the store looks wrong.

```
codeaf rebuild [--db path] [--yes]
```

**It asks first**, on two lines, and only `y` or `yes` proceeds — anything else, an empty
line included, says `cancelled` and changes nothing:

```
Rebuild everything codeaf worked out from the journal in /home/you/.codeaf/graph.db?
The journal itself is untouched; everything worked out from it is discarded and replayed. [y/N]
```

**The question is on the error stream, and so is `cancelled`.** Only the line saying what
was replayed goes to stdout — so `codeaf rebuild | tee log` still shows you the question
and still lets you answer it, and the file gets the result and not the prompt.

`--yes` skips the question for a script. When it is done it says what it replayed:

```
rebuilt 128 steps from 4173 journaled events
```

**It refuses while another process holds that store**, because the rebuild is one
transaction but the process on the other side would be reasoning about a graph that moved
underneath it:

```
a resident is running (pid 41207) — close it before rebuilding
```

Conversations, settings and credentials were not worked out from the journal and are not
touched.

## codeaf do is waiting and nothing happens — machine busy on a headless run

**A `codeaf do` the machine is holding says so on stderr, once.** The same two limits
that hold a task in the chat hold its workers here: `task.max_load` (load per core)
and `task.min_free_mb` (available memory). The chat draws `waiting · machine busy` on
the run's rail; a headless run has no rail, so the first time a worker is held,
stderr gets one line in the same words and the limit that held it:

```
waiting · machine busy · load per core at or above task.max_load 1.5
waiting · machine busy · available memory under task.min_free_mb 1536 MiB
```

Those figures are the defaults; the line prints your own. It is not repeated while
the machine stays busy. When a held worker starts, stderr says `starting · the machine has room again`,
once, and the run goes on as usual.

**If `--timeout` arrives first, nothing started.** The run leaves with 124 and `stop`
says `deadline`, as every wall does; `--json` also carries `blocked_on`, the held line
followed by ` · nothing started before --timeout`, so a script can tell a run the
machine held from one that was slow. Nothing was spent.

To let it start, wait for the machine to clear, raise `--timeout`, or set that row
to `0` in `/settings`, which turns that half of the check off. On a machine with no
`/proc` (macOS, Windows) the limits never hold, and this line never appears.

## What goes to stdout and what goes to stderr — piping a headless command

**stdout is the answer. Everything else is on stderr.**

The answer is the thing you would capture: the deliverable, the `--json` object, the rows of
`codeaf logs`, the plan `codeaf plan show` prints. Everything a person reads *about* the
run is on stderr: the `goal:` and `models:` preamble, the progress lines, warnings, the
path a record was kept at, the receipt saying a file was written, and any question the
command asks you. For `codeaf chat --once`, this includes every handover and ending line:
`finishing here · what was asked is done`, `stopping here · ` and the line that says a
reply's work was moved. Landing-woken replies remain on stdout with the first reply.

So these do what you would expect, and nothing has to be filtered out of them:

```
codeaf do "summarise CHANGELOG.md" --json | jq -r .answer
codeaf plan new "ship the endpoint" --json > plan.json
codeaf plan run plan.json > result.txt          # the preamble stays on your terminal
codeaf logs --tail 20 | wc -l                   # 20, not 21
codeaf cache clean | tee clean.log              # you can still see the question
```

Three of those used to be wrong. `codeaf plan new` and `codeaf plan run` printed their
`goal:`/`workspace:`/`models:` preamble into the stream; `codeaf logs` printed the log's
path as a first line, so every count was one too many; and `codeaf cache clean` printed its
**question** to stdout, which put the question in the file and left you looking at a blank
terminal waiting for a word you could not see.

`2>/dev/null` silences the commentary and keeps the answer. To keep both separately, redirect
them separately: `codeaf do "…" >answer.txt 2>notes.txt`.

**A command with nothing to show says so in one short sentence, and never prints a column
header with no row under it.** `the cache is empty · <path>`, `nothing is being kept
running.`, `nothing was tried on its own account today.` Silence and a bare header both read
as a command that broke.

## Which of these cost money, and which need no API key

**These read, need no key and spend nothing**: `why`, `notebook`, `competence`, `services`
(listing), `doctor`, `logs`, `cache`, `show`, `manual`, `version` and `--help`. They are
safe in a shell prompt, a CI step or a bug report.

`models` reads the measured ratings on this machine and also reaches the network for the
model catalog, but spends nothing of yours.

**These spend**, because all of them call a model: `chat`, `do`, `exec`, `run`,
`plan new`, `plan revise` and `plan run`. The door opens when the default
service has a key or any connected service holds its credential — a Codex sign-in, an
Ollama connection, a vendor key. So with no OpenRouter key a headless command still
starts once another service is connected; point it at that service's model with
`--model` or the profile's `model.talk`, because a call to a service that has no
credential still fails when it is made, with `no API key: this session has not been
given one yet`. With no credential anywhere, each fails at the door with the same two
lines:

```
codeaf needs a model to work with.
export OPENROUTER_API_KEY and run it again.
```

**For the default service, `OPENROUTER_API_KEY` is not required** — it is the first of three places its key is looked
for. On OpenRouter the order is that variable, the default-service key kept in your
profile, then `OPENAI_API_KEY` only if it starts with `sk-or-`. An OpenAI key such as
`sk-proj-…` is ignored. With a custom `CODEAF_BASE_URL`, the order is
`OPENROUTER_API_KEY`, `OPENAI_API_KEY` of any shape, then the profile key. The profile is
where the one connected in the browser or typed into `/settings` lives, so a machine
set up in the chat runs `codeaf do` with no variable set at all.
Each directly connected service may instead name its own environment variable, which is
stored with that service. `codeaf doctor`'s first row still reports only which default-service key answered — `key set · OPENROUTER_API_KEY`, or
`key set · /home/you/.codeaf/config.json`, or `key none ·` and the remedy above.
When `OPENAI_API_KEY` is set but unused, doctor explains whether it was outranked or
was not an OpenRouter key, without printing its value. `codeaf help env` explains
which variables the folder engine picks up when it restarts.

**These key variables are removed from a model's shell environment.** `OPENROUTER_API_KEY`,
`OPENAI_API_KEY` and every connected service's own key variable are stripped before a
bash call or background job starts, the same way `TMUX` is. This does not stop a shell
with file access from reading a key saved in the profile. To let a command use an
already exported provider key, set `CODEAF_ALLOW_PROVIDER_KEYS_IN_SHELL=1` in the shell
that starts codeaf. This setting does not export a key stored only in the profile.

If the default provider answers with an authentication refusal (`401` or `403`), codeaf stops
without retrying it as a provider failure. The ending names the safe source of the key it used:
`your key was not accepted for this model — the shell's OPENROUTER_API_KEY`,
`the shell's OPENAI_API_KEY`, or `the key saved in your profile`. It never prints the key itself.
`codeaf do` prints that sentence on stderr; `codeaf do --json` carries it in `error`,
with `stop:"incomplete"` and exit 2. A directly connected provider uses its own key,
so its refusal does not name the default provider's key source. This also applies to
bash workers in `do` runs: they use the profile admitted by `CODEAF_PROFILE_DIR`,
including its saved key and connected providers. A chat task keeps the provider,
model, and credential chosen for its crew seat; its key-source explanation cannot
reroute that call through the default provider. Delegated programs resolve the key
explanation for the model actually served, including a fallback onto another model.

**These change state without model spending**: `connect`, `disconnect`, `cache clean`,
`rebuild`, `notebook retract|restore`, `services stop` and `devices revoke`. A browser
connection may make authentication and model-list network requests, but sends no prompt.
The two that destroy something ask
first — `cache clean` wants the word `now` typed out, the same word `/cache clean now`
wants in the chat, and `rebuild` wants `y` — and `--yes` skips the question on both. The other three act at once, and all three can be undone: a
retracted belief restores, a stopped service starts again, a revoked device pairs again.

## Does codeaf collect data about me — telemetry, the anonymous usage counts

codeaf sends anonymous usage counts to AgentField, **on by default**: version, OS,
mode (chat or task), session counts and errors in bands, and the tokens each provider
call used. Never prompts, code, file names, paths, repo names, keys, email, IP or
machine name, and no model names. Each completed provider call queues a `usage_delta`
with provider-reported token counts when available and separate estimates for missing
text counts. A hashed receipt key pairs an estimate with a later provider receipt,
so analytics select reported counts per component instead of adding both. A recovered
receipt with just one token count leaves the other component's estimate available;
a cost-only receipt adds no token measurement. No text is sent. The queue is sent every 30 seconds while
the session stays open and once more when it ends; `session_ended` carries the outcome
and bucketed counts without repeating those tokens.

The relay is `https://agentfield.ai/api/oss/codeaf/telemetry`;
`CODEAF_TELEMETRY_ENDPOINT` moves it. The full disclosure, field by field, is
`docs/TELEMETRY.md` in the repository, linked from the README's *Telemetry* section.
The product prints no notice. With `model_pool` on, one scored row per crew seat also
leaves for the Model Pool after a task lands (*Model Pool* above).

## Missing streaming token counts — connected services and refused usage options

Connected services ask for streaming token counts with `stream_options.include_usage`.
If a service explicitly refuses that option with HTTP 400 or 422, codeaf retries
once without it and omits it on later calls to that endpoint. An unrelated error
does not turn usage reporting off. The default router uses its existing usage
request without the extra option.
The usage-option repair can follow or precede an independent reasoning repair;
each distinct repair is used once in that chain.

Missing text counts may have separately labelled estimates in anonymous usage
events. Estimates never change spending, prices or token limits. An interrupted
answer estimates only what arrived. Provider-reported counts, including explicit
zeroes, remain measurements. Separate answers keep separate hashed receipts even
if a connected service repeats its own answer id.
Opaque reasoning replay in the input leaves its input estimate unknown. Role or
metadata frames alone do not establish output on a cut stream; a normally
finished empty answer can honestly estimate zero output.

## How to turn telemetry off now — /settings stops sending immediately

On the *display* tab of `/settings`, turn the `telemetry` switch off. It writes
`telemetry = off` to your profile and stops sending immediately: no new event is
spooled and later periodic and exit flushes send nothing. Turning it back on takes
effect the next time codeaf starts. A request already on the wire may complete;
counts already queued locally stay unsent while it is off.

Other ways to turn the counts off: `CODEAF_TELEMETRY=off`, `DO_NOT_TRACK=1`,
`telemetry = off` in the project's `.codeaf/config.json` (a project may only turn it
off), or an empty `CODEAF_TELEMETRY_ENDPOINT`. Every switch also caps the Model Pool
at `read`: its index is still fetched and nothing is sent. Dirty or unstamped builds
and test binaries never report.

`codeaf telemetry` is no longer a command. It exits with an error naming the off
switches and `docs/TELEMETRY.md`, which lists what is sent. The former `status`, `info`
and `show` verbs were removed. Unsent events can be read directly in
`~/.codeaf/telemetry/spool.jsonl`.

## Reading a plan by hand — codeaf plan new, show, revise and run

`codeaf plan` writes a plan to a file you can read, edit and diff, then runs it exactly as
written. It is a real feature and it is not what most people want, because nothing codeaf
learns mid-flight can change a plan that is already frozen.

```
codeaf plan new "<goal>" [--out plan.json] [--dir dir] [--json] [--instructions]
                         [--passes auto|off|N] [--model slug] [--plan-model slug]
codeaf plan show <plan.json>
codeaf plan revise <plan.json> "<what happened>" [--done 1,2,3] [--out plan.json]
codeaf plan run <plan.json> [--dir dir] [--parallel 8] [--out done.json] [--yes-spend]
                            [--max-turns N] [--token-budget N] [--total-token-budget N]
                            [--no-method]
```

`codeaf plan show` is the reader. It takes a plan file and prints `goal:` and then the plan
as a table: the commitments it settled on, the stages, one row per step with what it waits
for, and the instructions under them. It reads a file and nothing else, so it needs no key
and spends nothing. With no file it says `usage: codeaf plan show <plan.json>`.

`codeaf plan revise` takes the same file and an account of what happened, and writes back a
plan changed in the light of it; `--done` names the steps that already landed.

`codeaf plan run` executes the file. `--parallel` is how many steps run at once,
`--max-turns` a backstop per step, `--token-budget` a token wall per step and
`--total-token-budget` one for the whole run, and `--no-method` skips writing a working
method for each step before it runs.

This is a different thing from `codeaf run <program>`, which runs a saved program on typed
input. The two used to share the word `run` and share nothing else.

## The old spellings — what happened to plan, show, revise and run

**`run` used to mean two unrelated commands.** `run graph.json` executed a static plan and
`run subharness <name>` ran a saved program. It means the saved program now, matching
`/subharness <name>` in the chat, and the pipeline moved under the one noun its four verbs
all act on:

| what you used to type | what it is called now |
| --- | --- |
| `codeaf run subharness <name>` | `codeaf run <name>` |
| `codeaf run <plan.json>` | `codeaf plan run <plan.json>` |
| `codeaf plan "<goal>"` | `codeaf plan new "<goal>"` |
| `codeaf show <plan.json>` | `codeaf plan show <plan.json>` |
| `codeaf revise <plan.json> "…"` | `codeaf plan revise <plan.json> "…"` |

**Every old spelling still works for one release.** It is absent from `--help`, it does
exactly what it always did, and it prints one line on stderr the first time it is used:

```
note: `codeaf run subharness <name>` is now `codeaf run <name>` — the old spelling works for one more release.
```

**That line is on stderr and never on stdout**, so `run <name> --json | jq` keeps parsing.
The two are told apart by the SHAPE of what you named, never by the directory you stand in:
a first argument spelled as a path is the old pipeline spelling, and a bare word is a
program. A separator anywhere in it, a leading `./`, `../` or `~`, or a file extension on
the end: any of those is a path, and where both readings would work the path wins, because
that is the one you spelled on purpose. `run formatter` is the saved program from every
folder; `run ./formatter`, `run plans/formatter` and `run plan.json` are plan files from
every folder; a saved program whose name carries a dot, `tidy.up`, is read as a file,
because saved-program names are bare words. It used to be decided by whether the file
existed, which made one command mean two things in two folders.

## Which flags moved — budget, turns, brief, contracts, ensemble

Some flag names moved in the same change, for the same reason: one concept, one spelling,
on every command.

| what you used to type | what it is called now | why |
| --- | --- | --- |
| `--budget N` | `--token-budget N` | *budget* is a word about **money** everywhere else here — `CODEAF_DAILY_BUDGET`, `/budget`, `--max-cost` — so `--budget 150000` read as $150,000 |
| `--run-budget N` | `--total-token-budget N` | the same, for the whole-run wall |
| `--turns N` | `--max-turns N` | it is a limit, and every other limit says so |
| `--max-seconds N` | `--timeout 2m` | one duration flag, one spelling, on `do`, `exec` and `plan run` |
| `--brief` | `--instructions` | it writes a self-contained instruction for every step |
| `--contracts=false` | `--no-method` | a boolean that defaults on needs a negative spelling, and what it turns off is a **working method** |
| `--ensemble 0\|-1\|N` | `--passes auto\|off\|N` | a tri-state is words, not magic integers |
| `-w`, `-o`, `-j` | `--dir`, `--out`, `--parallel` | the single letters are shorthands and **keep working forever**, silently; the long names are what is printed |

Each renamed flag says the same one line on stderr the first time it is used, and each old
spelling goes away after one release. `--plan-model` on `exec` is the odd one: that command
plans nothing, so the flag is still accepted and now says
`note: exec does not plan — --plan-model has no effect here.` rather than quietly doing
nothing.

**A flag that does not exist, and a number that will not read, are both refused in this
surface's own words** — spelled with the two dashes you typed, never the one dash Go's flag
package writes:

```
error: codeaf do has no --nosuchflag flag
error: invalid value "notanumber" for flag --max-turns: a whole number of turns to allow, such as 200
```

`--max-turns`, `--turns`, `--token-budget`, `--budget` and `logs --tail` all answer that way;
a negative count is refused too. The command's own usage follows the line, and the exit is 1
— nothing was attempted.

## Is this install healthy — codeaf doctor, and where it keeps things

`codeaf doctor` is the page to open when nothing works. It reads this machine and prints a
short block of labelled rows. It needs no key and spends nothing.

```
codeaf doctor [--db path] [--fix]
```

Every row is labelled in the words a developer would search for:

```
key              set · OPENROUTER_API_KEY
store            /home/you/.codeaf/graph.db · 496 KiB
resident         this terminal while open
spend            rail $20.00
model calls      /home/you/.codeaf/logs/calls.jsonl · 26 KiB
```

`key` comes first: whether this machine can reach a model at all, and where the key came
from — never the key itself. `store` names the file the journal and every derived table
live in, and how big it is — `· not created` on a machine that has not made one yet. `spend`
is the day against its limit, and `model calls` is where the call log is and what it
weighs. `store` used to read `brain` — nobody looking for where their data lives searches
for a brain.

**There is no background timer row any more.** Earlier versions printed one about a
five-minute pass an operating-system timer ran, and a `standing` row counting what that
pass kept; codeaf no longer installs anything on the machine, so there is nothing for
either row to read.

**It leaves out what it has not measured.** A machine that has spent nothing today prints
the limit and no figure beside it, rather than `$0.00` — a zero nobody measured reads as a
machine that counted, which is the opposite of the truth.

That command is about this *install*. `/status` in the chat is about the conversation you
are in. They are related and they are not the same reading.

## I deleted my codeaf home while the engine was stopping

A running process remembers its original home directory. If you remove it or
replace it with a different directory, attempts to make an identity or device
refuse with `codeaf home was removed or replaced; restart codeaf to use the current folder`.
Late saving work does not make a new identity there. If the engine is already
stopping and still closing chats, it exits promptly when its original home is
removed. With the home intact, stopping keeps its usual saving order and limits.
A new process can still create a home on first use.

## Repair a busy engine store or sync state — codeaf doctor --fix

When an engine store lock or cross-computer sync issue exists on this computer, `codeaf doctor` adds `engine store` and `sync` rows naming a busy store holder (`busy · held by <command>, pid <pid>`), an `unfinished continue · chat <id>`, `duplicate rows · chat <id>`, or a `lapsed hold on this computer · chat <id>`. Healthy stores and clean sync state print nothing.

Without `--fix`, `codeaf doctor` changes nothing. `codeaf doctor --fix` releases unfinished continues started by this computer, and retires duplicate branch copies whose turns are all already in the main chat, printing one `fixed:` line for each repair. A store lock whose process has ended is already free, so doctor neither reports nor removes it, and a busy store waits for its holder to finish or quit. It never touches another computer's live hold and never deletes a turn that exists only in the copy or scratch folder it inspects.

## codeaf help, and the verbs that take no flags at all

`codeaf help` is a third spelling of `codeaf --help`, beside `-h`, and all three print the
same list of commands. Every verb answers for itself the same way — `<verb> --help`, on
stdout, exiting **0**, because asking for help is not a failure — and the fuller account of
that, and of what a mistyped command is answered with, is on the *commands* page.

Two things that account does not cover:

- **The doors that parse no flags at all answer the gesture too.** `codeaf plan show` and
  `codeaf cache` take a positional or nothing, and each reads `--help` as the question
  rather than as an argument. (`codeaf models` has one flag, `--refresh`, which fetches
  today's model list first — what `ctrl+r` does in `/model`.) `codeaf plan show --help` used to answer
  `open --help: no such file or directory` — a filesystem error about a flag.
- **`help env` is the environment table.** It moved off `--help` when that page was 127
  lines and more than half of them were this table, so the last thing on the screen after
  asking what the commands are was `CODEAF_CALL_LOG_BODIES`.
- **`codeaf manual --help` prints its usage, then the list of pages.** The list is what
  that command can be asked for, so it is still there; it used to be *all* that was there,
  which made one verb in the binary answer `--help` differently from the other twenty-two.

## Why does an empty task brief say no goal was given?

`codeaf do`, `codeaf exec`, and `codeaf plan new` require a nonblank brief.
An empty quoted argument, whitespace-only arguments, or empty piped input are
rejected before planning or model work starts. Supply the goal as command
arguments or pipe it through standard input; a single `-` explicitly selects
standard input. A missing goal is not a request for the model to invent work.

## A brief containing literal -- or words that look like flags

For delegated programs such as `codeaf senior-dev`, the first standalone `--`
ends flag parsing. Every argument after it belongs to the brief, including
another literal `--`. For example, `codeaf senior-dev -- --` supplies the brief
`--`; `codeaf senior-dev -- --max-cost 2` supplies the words `--max-cost 2`
as the brief rather than setting a cost ceiling. Put ceilings before the first
`--` when using this form.

## Do receipt tokens and spending after an incomplete turn

On `codeaf do`'s default run road, paid calls remain in `tokens.in`, `tokens.out`
and the spend total when a later call fails in the same turn. Auxiliary summary
calls count too. A worker settles outstanding provider receipts before reporting
its final totals. With no receipt outstanding, it adds no receipt wait to the
ending. Otherwise it waits at most the provider's existing receipt deadline and keeps
the figures already received when a receipt cannot be settled. An incomplete
receipt therefore reports the work's paid calls as well as its failure reason.

## Desktop messages with an invalid client id

The desktop app refuses a message with an invalid `client_id` as `bad_params`
before opening its conversation or starting a turn. This applies to creating an
agent, sending, queueing and steering. An id must be valid UTF-8, at most 256
bytes, with no control characters. Client ids correlate entries with messages;
they are not idempotency keys, and repeated ids are allowed.
## What background processes are still running

Run `codeaf services` in the terminal to list long-running services started here
and not yet stopped, with each name, status, age, health check and log path.
`codeaf services stop <name>` stops a named service. If none are running, the
command says `nothing is being kept running.`

For the current chat's background commands, watches or renders, use `/processes`
in that chat. Click a process to open its log. The sidebar shows only running
processes; finished logs remain available on demand without an ended count.

## Crew proposals in the desktop app stream beside the chat

**Bring in helpers** asks the conversation's own model about the work without
starting a turn or changing the chat's history or model context. The proposal
arrives in complete lines, with its size and name before its helpers. Proposal
generation asks for reasoning to be off, or the lowest level the provider supports.
Your chat's reasoning setting stays the same. The default
is the smallest useful crew, usually one to three helpers. Separate teams are
proposed only for separate streams of work that each need multiple people.

The final **Bring in helpers** ask still offers **Make the crew**, **Edit**, and
**Keep it one agent**. If the stream stops after at least one helper, the ask
keeps those helpers and says "Stopped after 2 helpers", with the actual count.
An answer with no usable helpers leaves one quiet notice. Handles are limited to
12 characters. Longer model-proposed handles are shortened at the last hyphen
that fits, or to the first 12 characters, with numbering for clashes.

Accepting a proposal keeps this conversation as manager and creates one flat
crew. All helpers are seated under their approved handles before creation returns.
Duplicate helper handles are refused before a crew is made. An automatic manager
handle that clashes with a helper gets a free handle; the manager remains the same
conversation. If setup fails after the crew is made, accepting the same proposal
continues seating its missing helpers and reuses their original starts. Repeating
a completed acceptance returns the same crew. Proposed reporting handles are recorded on members. Team groups do not
create nested crews yet.
