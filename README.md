<div align="center">

<img src="assets/readme/hero.jpg" alt="codeaf, the open-source software factory. Direct the work from your terminal, servers or phone." width="100%">

<br>

<a href="LICENSE"><img alt="Apache 2.0" src="https://img.shields.io/badge/license-Apache%202.0-0A0B0D?style=flat&labelColor=1D2024&color=D4A24A"></a>
<a href="https://github.com/Agent-Field/codeaf/releases"><img alt="release" src="https://img.shields.io/github/v/release/Agent-Field/codeaf?style=flat&labelColor=1D2024&color=0A0B0D"></a>
<a href="https://github.com/Agent-Field/codeaf/stargazers"><img alt="stars" src="https://img.shields.io/github/stars/Agent-Field/codeaf?style=flat&labelColor=1D2024&color=0A0B0D"></a>
<img alt="one binary, darwin linux windows" src="https://img.shields.io/badge/one%20binary-darwin%20%7C%20linux%20%7C%20windows-0A0B0D?style=flat&labelColor=1D2024">

<p>
<a href="#install">Install</a> ·
<a href="#what-a-factory-is">What a factory is</a> ·
<a href="#benchmarks">Benchmarks</a> ·
<a href="#switching-from-opencode-pi-or-aider">Switching</a> ·
<a href="docs/GUIDE.md">Guide</a>
</p>

</div>

codeaf is an open-source software factory for your terminal. You describe the
work. It runs in its own worktree, on the model you choose, and comes back for
your review. One binary, no account, no service to run. Any model, open models
by default. Apache 2.0. By [AgentField AI](https://agentfield.ai).

## Install

```bash
curl -fsSL https://agentfield.ai/get/codeaf | bash
codeaf
```

The script fetches the release binary for your platform into `~/.codeaf/bin`.
Prefer to build it yourself: `git clone`, `make build`, `bin/codeaf`
([guide](docs/GUIDE.md#install)). Release assets, checksums and version
pinning are on the [releases page](https://github.com/Agent-Field/codeaf/releases).

On first start it asks for a key: OpenRouter, DeepSeek, GLM, Kimi, MiniMax or
Qwen. Point it at Ollama and it needs no key.

Everything is in the one binary: the tools, tasks and their worktrees,
standing orders, memory, spend, search, remote access, the headless commands
and a manual about itself.

<!-- TODO: binary size, cold start, idle memory. -->

## Hand it work

Start it inside a repository. Give it something that takes longer than you
want to watch.

```text
 › /task move the retry logic out of the three clients into one place, keep the
   per-client backoff numbers, and make the tests pass
```

The task exists when you press enter. It cuts a worktree, works there, and the
conversation is yours again. Describe the work without `/task` and codeaf
proposes one on a card when the job is bigger than a few edits, and waits for
your yes.

When it lands, `home` says so under `to check`, with the diff, the test run and
the cost. `a` accepts and merges, `l` sends it back with a note, `n` drops it.
Nothing merges without you.

<!-- TODO(G1): recording. A brief becomes a task, home shows it running, it
     lands, `a` accepts it. 160x45, about 20s, scripted with vhs. -->
<img src="assets/readme/screens/tasks.png" alt="a conversation with tasks running in the column beside it" width="100%">

Every turn ends with its price, so you always know what a thing cost.

```text
                                          · 19:31 · 3.2s · 1 tool call · $0.0008 · ❮
 $0.0008 · ⟲ saved $0.0005 · 45% cached   15.6k/1.3M · 1%                        idle
```

## What a factory is

If you have three terminals open with three agents, three worktrees and a merge
waiting, you already run a factory, by hand. The agents got fast. The
scheduling, the merging and the checking stayed with you.

<img src="assets/readme/how-work-changes.png" alt="one agent: you wait. several agents by hand: you are the scheduler. a factory: you assign and review." width="100%">

More work in flight means more to check, so a factory only helps if checking
gets cheaper. Two things do that here.

- **Work runs where it cannot collide.** Each task gets its own worktree and
  its own budget. It keeps a record you can read, and it comes back as one
  thing to accept or send back, not five terminals to reconcile.
- **Specialist tasks check themselves.** Work that comes round in the same
  shape (review this pull request, chase this flaky test, audit these
  dependencies) runs as a saved program with typed input and typed output.
  It plans, fans out, tests its own result, and either produces what it
  promised or is marked incomplete. Those are the tasks in the benchmark below.

<!-- TODO: settle the public name. The manual calls these subharnesses and the
     command is /subharness; this README says specialist task. -->

## Benchmarks

<!-- TODO(C1): chart, two panels: pass rate vs cost, pass rate vs time.
     Render from the launch run with bench/oneroad/plots/final_board.py. -->

We ran `[N]` held-out GitHub issues, `[K]` seeds each, through every harness
below on the same open model, `[MODEL]`. Versions and configs are in
[BENCHMARKS.md](BENCHMARKS.md), with every failure, timeout and unpriced call.

| harness | version | pass rate | cost per issue | time per issue |
| --- | --- | --- | --- | --- |
| codeaf | `[..]` | `[..]` | `[..]` | `[..]` |
| `[HARNESS]` | `[..]` | `[..]` | `[..]` | `[..]` |
| `[HARNESS]` | `[..]` | `[..]` | `[..]` | `[..]` |
| `[HARNESS]` | `[..]` | `[..]` | `[..]` | `[..]` |

codeaf is on the frontier: the harnesses that passed more cost more and took
longer, and the ones that cost less passed fewer. Rerun it on your own
repository with `bench/` and send us the numbers.

## What a copilot does, and what codeaf does

| | a copilot | codeaf |
| --- | --- | --- |
| where you work | one window, one repository | every project on the machine, from one home screen |
| what you do | watch it type | describe work, answer questions, review what lands |
| what it works in | your checkout | a worktree per task, merged when you accept |
| how long it lasts | one session | conversations, tasks and standing orders that outlive the window |
| what you pay | a seat | the tokens, priced on every turn, under a daily limit you set |

## Seven places, one machine

codeaf gives every machine seven full-screen places, reached with `alt+1` to
`alt+7` from anywhere. `esc` returns to the conversation you were in.

| place | what it shows |
| --- | --- |
| `home` | what needs you, what is running, what happened while you were away, what today cost |
| `tasks` | every task on the machine, by the conversation that started it, with state and cost |
| `spend` | cost by day, by model and by task |
| `settings` | models, the crew, connected accounts, limits |
| `standing` | the orders that keep running, when they last fired, what they cost |
| `memory` | what codeaf remembers about you and your projects, line by line, editable |
| `search` | every past conversation |

<img src="assets/readme/screens/home.png" alt="home: needs you, where you were, projects, running, since you left, spend, next up" width="100%">

<img src="assets/readme/screens/tasks-tree.png" alt="the tasks place: conversations, their tasks and sub-tasks, with state and cost" width="100%">

<img src="assets/readme/screens/review.png" alt="a task page: finished, but nobody has checked it. your call." width="100%">

## Standing orders

Say "always run the tests before you land" or "every morning, check the
release radar". codeaf asks whether you mean once or from now on, and it
becomes a standing order: a timer, a budget, and the same review as everything
else.

<img src="assets/readme/screens/standing.png" alt="the standing place: four orders, when they fired, what they cost" width="100%">

## From your terminal, your servers, or your phone

The conversation lives on the machine that owns the work. Your screen attaches
to it.

<img src="assets/readme/anywhere.png" alt="one conversation on devbox; a terminal, a laptop over ssh and a phone through the relay all attach to it" width="100%">

```bash
codeaf chat --host devbox     # the work runs on devbox, over ssh; codeaf on its PATH is all it needs
codeaf serve                  # no ssh in? hold a connection open through a relay
codeaf chat --at otter-lamp   # attach to a served machine by name, from any paired device
```

The relay puts two connections next to each other and nothing more. It sees a
machine name and byte counts; the conversation is encrypted end to end, and
pairing uses a password-authenticated key exchange, so the relay cannot read it
or sit in the middle. [Details](docs/REMOTE.md).

Close the laptop and the tasks keep running. Open the phone and `home` is
there at phone width: what needs you, a digit to answer, a key to accept.

<p>
<img src="assets/readme/screens/ssh.png" alt="codeaf chat --host devbox from a laptop" width="66%">
<img src="assets/readme/screens/phone.png" alt="home on a phone: needs you, approve" width="32%">
</p>

## What it is allowed to touch

A task writes in its own worktree and nowhere else. In the conversation, a
command the rules do not already allow is held up and shown to you first:
`1` allow once, `2` always this command, `3` deny. `--yolo` turns the asking
off for a session and says so on the status line. There is no telemetry.

<!-- TODO: confirm "no telemetry" against the code before publishing. -->

## Any model, open by default

Providers built in: OpenRouter, DeepSeek, GLM, Kimi, MiniMax, Qwen, Ollama and
any OpenAI-compatible endpoint. Each of the five crew seats has its own model.
Three presets ship, all on open weights:

| preset | worker | high | mastermind |
| --- | --- | --- | --- |
| frugal | deepseek-v4-flash | glm-5.3-flash | glm-5.3-flash |
| balanced | glm-5.3-flash | qwen3.8-27b | glm-5.3 |
| max | glm-5.3 | kimi-k3 | kimi-k3 |

<img src="assets/readme/screens/crew.png" alt="/crew: the chat model and five seats, frugal, balanced and max" width="100%">

Open source under Apache 2.0, all of it, including the runtime and the
specialist tasks. You are giving a program write access to your repositories.
You should be able to read every line of it.

## Switching from opencode, pi or aider

Your keys and providers carry over. The conversation works the same way.
Everything around it is new: a home screen across projects, tasks in their own
worktrees that wait for your call, standing orders, spend by model, and a
session you can reach over ssh or from a phone.

## Roadmap

v0.1.0 shipped on 2026-08-17. `[N]` changes since, each written up in
[docs/changes](docs/changes/).

| shipped | launch, `[DATE]` | next |
| --- | --- | --- |
| conversation, tasks, worktrees, review | binaries and installer for every platform | team server: one machine, every engineer's factory |
| seven places, standing orders, memory | benchmark results and chart | web view |
| open-model crew, spend limits | `[..]` | identity and a signed record per task, through the AgentField control plane, Apache 2.0 like the rest |
| ssh, relay, phone, headless | | |

Vote in [Discussions](https://github.com/Agent-Field/codeaf/discussions).
Three things help most: run the benchmark on your own repository and send the
run; add a provider that is missing; when a task goes wrong, `/why` and paste
what it says into an issue.

<!-- TODO: Discord link. -->

## Docs

- `codeaf manual`, or `alt+.` for the key map. The manual ships in the binary and the chat reads it too.
- [Guide](docs/GUIDE.md): every flag, key, slash command and exit code.
- [docs/](docs/README.md): architecture, headless, remote, limits.

Built by the team behind [AgentField](https://github.com/Agent-Field/agentfield),
the open-source AI backend, and Covalent.
