<div align="center">

# codeaf guide

**Talk through the work, change the files, and hand off tasks you can walk away from.**

*One terminal session gives a model tools, memory, background work, and a manual about itself.*

<img alt="Go 1.26.5" src="https://img.shields.io/badge/Go-1.26.5-0c0b09?style=flat&labelColor=8b7355"> <img alt="darwin, linux, windows" src="https://img.shields.io/badge/targets-darwin%20%7C%20linux%20%7C%20windows-0c0b09?style=flat&labelColor=8b7355">

<p><a href="#install">Install</a> • <a href="#talk-to-codeaf">Chat</a> • <a href="#tools-in-the-conversation">Tools</a> • <a href="#hand-work-to-tasks">Tasks</a> • <a href="#models-keys-and-spending">Models</a> • <a href="#the-manual-ships-with-the-binary">Manual</a> • <a href="#headless-work">Headless</a> • <a href="#work-on-another-machine">Remote</a> • <a href="#contributing">Contributing</a> • <a href="#documentation">Docs</a></p>

</div>

Run `codeaf` inside a project. It opens the conversation that directory was last
having. Ask in ordinary language; the model can read and edit files, run commands,
search the web, remember useful context, and bring separate tasks back when they land.

This is the chat flow in a real terminal:

```text
 codeaf                                                                 $0.0010 / $500 · sun 7:32pm
   Home      [key lookup] ×   +
────────────────────────────────────────────────────────────────────────────────────────────────────
    · esc interrupts · ctrl+c twice quits · ? for help

  › read ./apikey.go and tell me in two lines where codeaf looks for an API key

    ▾ worked 3.2s · thought 0.6s · 1 tool call · ctrl+e
    ▸ read apikey.go                                                                        1 call

  In Load's order (from `apiKeyFrom`/`APIKeyAt`): `$OPENROUTER_API_KEY`, then
  `$OPENAI_API_KEY`, then the `api_key` field in the profile config file written with
  `WriteAPIKey`. First non-empty wins.

    · ⟲ 14.1k cached · saved $0.0005
                                                          · 19:31 · 3.2s · 1 tool call · $0.0008 · ❮
─ where codeaf finds its a… · ~deepseek/deepseek-v4-flash-latest ─── space space home · / commands ─
 ›
 $0.0008 · ⟲ saved $0.0005 · 45% cached   15.6k/1.3M · 1%                                       idle
```

That is a real session on the default model, `~deepseek/deepseek-v4-flash-latest`:
one question, one `read`, one answer, and what it cost on the last line.

### Which folder it works in

"Inside a project" is decided at launch, in this order
(`cmd/codeaf/chatv3_layout.go:84`): `--workspace <path>` if you gave one, else the root
of the git repository you are standing in, else the directory itself. The exception is
a directory nobody chose — your home directory, or anything under a temporary directory
— where codeaf keeps a folder of its own and says so on the welcome screen:

```text
in a folder codeaf keeps for this conversation · /workspace picks another
```

Your files are not in that folder, so asking it to read one gets you an empty
directory. `/workspace <path>` anchors a conversation to a project at any point; the
shorter road is to start codeaf inside the project.

Once this machine has conversations on it, bare `codeaf` opens **home** rather than a
conversation — projects, running work, questions waiting on you, what you have spent.
Type into the box to start one, or press enter on a row to reopen one. `space space`
goes to home from a conversation and `esc` comes back untouched.

## Install

### The installer

```bash
curl -fsSL https://agentfield.ai/get/codeaf | bash                                                    # the supported line
curl -fsSL https://raw.githubusercontent.com/Agent-Field/codeaf/main/scripts/install.sh | bash     # the script under it
```

The first proxies the second: `agentfield.ai/get/codeaf` serves `scripts/install.sh` from
this repository's `main` branch, byte for byte, and a channel on the path — `/dev`,
`/staging`, `/rc` — rewrites the one line that sets the default channel. Pipe it to
`bash`, not `sh`; the script uses `pipefail` and `[[ ]]`, which dash rejects.

### Build from the repository

The module needs Go 1.26.5, and `make build` is the one supported build command; it
writes `bin/codeaf`.

```bash
git clone https://github.com/Agent-Field/codeaf.git
cd codeaf
make build   # fetches the pinned Furrow artifact, so it needs the network;
             # FURROW_ARTIFACT=/path/to/furrow supplies it offline
bin/codeaf
```

<details>
<summary>Installer channels, flags, and checks</summary>

A push to `dev`, `staging` or `main` publishes that channel's build, marked as a
prerelease; stable is published only when a person dispatches `Release` on `main`. A
channel with nothing published stops with `no <channel> build has been published yet`.
Recognizing a channel does not mean a matching release exists.

```bash
curl -fsSL https://agentfield.ai/get/codeaf/dev | bash
curl -fsSL https://agentfield.ai/get/devaf | bash
curl -fsSL https://agentfield.ai/get/stageaf | bash
curl -fsSL https://agentfield.ai/get/codeaf/staging | bash
curl -fsSL https://agentfield.ai/get/codeaf/rc | bash
curl -fsSL https://agentfield.ai/get/codeaf | VERSION=<tag> bash
```

| Installer input | Behaviour |
| --- | --- |
| `--stable` | Select the latest stable release; this is the default. |
| `--dev` | Select the latest `dev-*` release. |
| `--rc`, `--staging` | Select a matching channel build or stop if none has been published. |
| `--version TAG` or `VERSION=<tag>` | Pin one release tag. |
| `--name WORD` or `CODEAF_INSTALL_NAME=WORD` | Choose the installed binary's file name. |
| `--dir PATH` | Install somewhere other than `~/.codeaf/bin`. |
| `--no-modify-path` | Print the PATH line without editing a shell file. |
| `--verbose` | Print each GET. |
| `GITHUB_TOKEN` or `GH_TOKEN` | Raise GitHub's anonymous API limit. |

The script needs `curl` or `wget`, plus `sha256sum` or `shasum`. It downloads
`checksums.txt` and refuses a sha256 mismatch. Unless `--no-modify-path` is set, it
appends one `export PATH=… # codeaf installer` line to the applicable shell file. On a
normal run it prints three things and nothing else: `installed codeaf v… built … ·
go… os/arch` (the installed file naming itself), the three-line telemetry notice, and,
when the folder is not yet on `PATH`, the bare `export PATH=…` line to paste into the
current shell, bold green on a terminal, last, with a blank line above and below.
`--verbose` also reports the channel, the tag and the install path on stderr. The
`/get/devaf` line selects the dev channel and names the file `devaf`, installing it
beside codeaf. The `/get/stageaf` line selects staging and names the file
`stageaf`, also beside codeaf. Release builds cover darwin, linux, and windows
on amd64 and arm64.

</details>

## Talk to codeaf

Bare `codeaf` and `codeaf chat` open the same conversation surface. `codeaf resume`
opens a picker for an earlier conversation.

```bash
codeaf
codeaf chat --model <slug>
codeaf chat --once "summarize the changes"  # print one reply and exit
codeaf resume
```

`enter` sends a message or steers a running answer. `ctrl+enter` makes a standing
order. `esc` interrupts or, pressed twice at rest, opens rewind. `ctrl+c` interrupts
mid-turn; twice within 1.5 seconds while idle quits.

<details>
<summary>Chat launch flags and terminal keys</summary>

| Flag | Meaning |
| --- | --- |
| `--model <slug>` | Choose the model for this session. |
| `--once "text"` | Run one message non-interactively, print the reply, and exit. |
| `--session <path>` | Resume the transcript at that path. |
| `--yolo` | Run tools without asking; the approval default becomes allow. |
| `--reasoning auto\|low\|medium\|high\|xhigh\|max` | Override reasoning for the session. |
| `--no-compact` | Never compact automatically. |
| `--one-model` | Put every text call on the session model. |
| `--host`, `--at`, `--no-host` | Choose where the session runs and how this surface attaches. |
| `--debug` | Keep the full run record. |
| `--max-hours`, `--max-cost` | Bound an unattended `--yolo` session. |

| Key | Action |
| --- | --- |
| `ctrl+o` | Expand the selected call or task instruction. |
| `ctrl+.` | Open task history. |
| `space space` | Open home from an empty message box. |
| `alt+1` … `alt+7` | Select one of the seven places. |
| `ctrl+,` | Open settings. |
| `ctrl+g` | Send a running command to the background; otherwise close the task column, or bring it back. |

</details>

Once the conversation has begun, a column stands down the right-hand side of a wide
terminal — the tasks and standing orders over this conversation, or, while there are
none, `+ /task` and `+ /standing` with `❯ ctrl+g hide` under them.

Slash commands cover models, conversations, work, memory, permissions, and spending.
Start with `/help`. Common doors are `/model`, `/new`, `/resume`, `/workspace`, `/task`,
`/history`, `/standing`, `/memory`, `/remember`, `/permissions`, `/status`, `/cost`,
`/spend`, `/budget`, `/compact`, `/rewind`, `/manual`, `/help`, and `/quit`. `/drafts`
keeps the last ten cleared drafts; `enter` restores one and `d` lets one go.

<details>
<summary>Slash-command reference</summary>

| Command | Registered meaning |
| --- | --- |
| `/model` | `pick a model · or press its name above the message box`; with a slug, `switch the model for the conversation or open task`. |
| `/new` | `start another conversation in this project` |
| `/resume` | `open an earlier conversation` |
| `/workspace <path>` | `anchor this conversation to a project` |
| `/task <brief>` | `start work you can walk away from` |
| `/history` | `every task this project has run · ctrl+.` |
| `/standing <words>` | `keep this true · a card, never work done once` |
| `/memory` | `inspect and change what is remembered` |
| `/remember <text>` | `keep one thing across conversations` |
| `/permissions` | `what runs without asking · drop one with d` |
| `/status` | `everything the status line knows, one fact per line` |
| `/cost` | `what this conversation has spent · /spend is the whole machine` |
| `/spend` | `what this machine has cost, by the day · alt+3` |
| `/budget` | `what codeaf may spend · every limit on one tab` |
| `/compact` | `summarize the conversation now` |
| `/rewind` | `go back to an earlier point · esc esc takes back the last` |
| `/manual` | `asks the model what codeaf can do, from its own manual` |
| `/help` | `this list` |
| `/quit` | `close this conversation` |
| `/drafts` | `cleared-but-kept drafts · enter restores one, d lets one go` |

</details>

A call that needs permission pauses on a block. Its header counts ten seconds down and
then stops and keeps waiting, rather than answering for you:

```text
╭─  needs your ok to run bash ─────────────────────────────────────────────────────── bash · 10s ─╮
│ echo hi · default                                                                                │
│                                                                                                  │
│ ▸ 1  allow once                                                                                  │
│   2  always                                                                                      │
│   3  deny                                                                           safe answer  │
│                                                                                                  │
╰─ ↑↓ choose · enter take it · esc later ──────────────────────────────────────────────────────────╯
  c change · ? ask back · 1–3 jump
```

`↑↓` and `enter` take an answer, the digits jump straight to one, and `esc` leaves it
for later. `c` is `change: say what you want different, then enter`; `?` is
`ask back: type your question, then enter`. The answer leaves a row you can reopen —
`needs your ok to run bash → allow once · you · 20:21 · c change`. A call whose outcome
cannot be taken back drops choice 2, the widening one. `--yolo` makes allow the default,
and the status line then says `YOLO`.

## Tools in the conversation

The model works through named tools rather than by suggestion. `read`, `write`,
`edit` and `bash` act on the folder this conversation is anchored to; `grep`, `find`
and `ls` look around it; `web_search` and `web_fetch` go out; `remember` keeps something for later;
`propose_task` hands work off. The belt is assembled for each session, and a
capability that cannot work is left off it rather than offered and failing.

<details>
<summary>Tool-name reference</summary>

| Work | Exact tool names |
| --- | --- |
| Files and shell | `read`, `write`, `edit`, `bash`, `grep`, `find`, `ls` |
| Documents and jobs | `read_document`, `jobs`, `manual` |
| Interaction and work | `ask`, `watch`, `tasks`, `propose_task`, `quick_task`, `items`, `divide_work`, `stand` |
| Memory and workspace | `remember`, `recall`, `track`, `commit`, `workspace`, `workspace_snapshots`, `workspace_restore`, `workspace_fork`, `workspace_merge`, `search_conversations` |
| Web | `web_search`, `web_fetch` |
| Settings and accounts | `settings`, `change_setting`, `services`, `use_service`, `gmail_search`, `gmail_read`, `gmail_send`, `calendar_list`, `calendar_create`, `slack_search`, `slack_read_thread`, `slack_list_channels`, `slack_send` |
| Media | `generate_image`, `speak`, `generate_music`, `generate_video`, `view_image`, `edit_video`, `load_capability` |

`remember` needs memory switched on. The four `workspace_*` tools need a folder under
Furrow watch. The five `gmail_*` and `calendar_*` tools arrive only with a connected
Google account and the four `slack_*` only with Slack; `/connect` — `your connected
accounts · connect another` — is the door, and any other keyed account brings one
`<service-id>_request` instead, plus whatever the account names for itself.
`view_image` needs a vision model. `edit_video` needs its local video binaries. Each
media-generation tool needs both a media client and a resolved model for its modality.

</details>

## Hand work to tasks

`propose_task` gives self-contained work to a separate task, returns its id immediately,
and opens a short countdown. Redirect or cancel during the countdown; silence starts it.
Keep talking while the task runs. Its report starts a turn in the conversation when it
lands.

A task page shows the instruction, folded work and calls, steering, the report, and a
pinned line with activity, duration, cost and call count. `/task <brief>` is the direct
door; `/history` opens the project's task history.

Standing orders use `/standing <words>` or `ctrl+enter`. They become cards and stay true
after the conversation. Memory keeps person-, project-, or machine-scoped records in
`graph.db`; `/memory`, `/memories`, `/remember` and `/forget` change them.

## Models, keys, and spending

Key resolution for the default provider is `OPENROUTER_API_KEY`, then
`OPENAI_API_KEY`, then `api_key` in the profile's `config.json`. With no credential,
an interactive local launch opens a two-page setup that offers to connect OpenRouter
in a browser or take a pasted key. First run is unchanged and does not offer Codex.
A non-interactive chat starts when the default provider has a key or any connected
provider holds its credential; a call to an account without one still fails when it is
made. With no credential anywhere it stops with `codeaf chat needs a model to talk with.`

<details>
<summary>The two first-run screens, word for word</summary>

Where a browser is reachable the first page is headed `connect openrouter`:

```text
sign in once in your browser. openrouter makes the default provider's key for this profile; codeaf stores it on this machine. no prompt is sent and no model is called.
```

Where it is not, the same page is headed `your openrouter key` and reads `codeaf talks
to models on its default provider through openrouter, on your key and your card. nothing
is sent until you do.` Either way the foot takes a pasted key and `esc` skips setup.
The second page is `Daily limit` and `Chat model`.

</details>

The chat model resolves from `--model`, then saved `model.talk`, then `CODEAF_MODEL`,
then `~deepseek/deepseek-v4-flash-latest`. The last value is a floating alias. Besides
OpenRouter, the connection screen supports DeepSeek, Z.ai, Moonshot, MiniMax, Alibaba
Qwen, Codex through a ChatGPT plan, Ollama, and a custom OpenAI-compatible provider.
The same supported providers can be managed without opening the chat with `codeaf
connect` and `codeaf disconnect`; a qualified slug such as
`qwen/<model>` selects its provider.

Host routing defaults to `simple`: an unpinned OpenRouter call carries no host
object, while a pinned call asks for exactly that host. `latency` and `price` remain
opt-in settings.

The default daily rail is `$500`; setting that row to `0` removes it. First run asks for
`Daily limit` and `Chat model`; a task's crew is picked per task, and `/crew` shows it.

State lives under `$CODEAF_HOME`, or `~/.codeaf` when it is unset or empty: settings and credentials
in `config.json`, memory in `graph.db`, and project sessions under `v3/projects/`.

## The manual ships with the binary

The Markdown under `internal/manual/chat/` is compiled into codeaf, and the conversation
reads it with the `manual` tool to answer questions about its own behaviour. From a
terminal `codeaf manual` lists every page and `codeaf manual "<question>"` returns the
sections that answer it; inside the chat `/manual` puts the question to the model with
the manual open, and the answer arrives as a turn. Build gates require every
slash command and alias, every tool name, and more than a hundred questions in ordinary
language to reach an answering page.

## Headless work

- `codeaf do "<task>"` does one task and exits: the same living agent as chat, with nobody watching.
- `codeaf exec ["<prompt>"]` runs one worker for one pass, with no planning.
- `codeaf run <program> --input <file.json|->` runs one saved typed program and writes typed output to stdout.

None takes `--yolo`, and all three end the same way — `--json` carries the reason
in its `stop` field.

<details>
<summary>Exit codes, and the read-only commands</summary>

| Code | Meaning |
| --- | --- |
| `0` | Done. |
| `1` | Could not be run at all. |
| `2` | Ran and did not finish. |
| `3` | A limit you set stopped it. |
| `4` | Needed an answer and nobody was there. |

These need no model key and spend nothing: `codeaf why self`, `codeaf why <task-id>`,
`codeaf logs [--tail 40] [--follow] [--json]`, `codeaf doctor`,
`codeaf manual [page | "question"]`, and `codeaf version`. `codeaf help env` prints the
environment table.

`codeaf models [--refresh]` is the exception in that group: it spends nothing of yours,
but it reaches the network for the catalog and stops with
`codeaf needs a model to work with.` if no key is resolvable.

</details>

See [the headless contract](docs/HEADLESS.md) and [ambient work](docs/AMBIENT.md).

## Work on another machine

The conversation and its work stay on the machine that owns the workspace; the local
surface attaches to it over a byte stream.

```bash
codeaf chat --host devbox            # also me@devbox, or devbox:code/app
```

`--host` runs the far machine's `codeaf engine` over `ssh -T`: it needs `ssh` here and
codeaf installed there, and it cannot be combined with `--one-model`. For a machine
without ssh, `codeaf serve [--workspace path] [--relay url]` holds an outbound relay
connection and prints a pairing name such as `otter-lamp-42` — without a configured
relay it stops — and `codeaf chat --at <name>` connects to it. `codeaf devices` lists
paired devices; `codeaf devices revoke <name> [--all]` revokes access.

Current boundary: spend, search and memory pages still read local files, and inside a
remote task room steering, stopping and model changes are absent.

Read [remote access](docs/REMOTE.md) and its [testing guide](docs/remote-access-testing.md).

## Contributing

Branch from `dev` and open the pull request against `dev`; `main` is the release pointer.
Every pull request carries a change entry under `docs/changes/unreleased/`.

```bash
make build        # bin/codeaf
make pr-ready     # the bar a pull request into dev has to clear
make demo-home    # a throwaway home with something on every page
make changelog-new PR=<n> KIND=<kind> SLUG=<slug>
```

Read the [branching rules](docs/rules/branching.md), the [change-entry rules](docs/rules/changelog.md)
and [AGENTS.md](AGENTS.md) before contributing.

## Documentation

Start with [the documentation map](docs/README.md); the enduring references are the
[design language](docs/DESIGN-LANGUAGE.md), [remote access](docs/REMOTE.md), the
[headless contract](docs/HEADLESS.md) and [ambient work](docs/AMBIENT.md).
