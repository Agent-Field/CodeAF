# Working in this repo

## Which surface is which

Three chat surfaces live here. Getting this wrong wastes a whole recon pass, so check
before you read.

| Path | What it is |
| --- | --- |
| `internal/tui3` | **v3 — the live surface.** Entry `cmd/aforge/chatv3.go`. Bare `aforge` and `aforge chat` both open it. |
| `internal/session` | **the v3 engine** — the agent, the turn loop, the toolbelt, tasks. |
| `internal/tui2` | the older v2 surface. Not the live one. |
| `internal/tui` | v1, and the visual north star: restrained, dim telemetry, no borders. |
| `internal/head`, `internal/resident` | the v1 **resident** — a different product in the same binary. |

v3 is a **session you sit in front of**. The resident is an employee that keeps working
while the terminal is closed. They share a repository and almost nothing else — do not
carry vocabulary or assumptions between them.

## Build and ship — the owner's standing orders

- **Always build with `make build`**, which writes `bin/aforge`. Never a bare
  `go build -o` to some other path: `bin/aforge` is the ONE binary the owner
  runs, and every stray copy becomes a shadow that rolls them back silently
  (the root `./aforge` did it once, `~/.agentfield/bin/aforge` did it again on
  2026-08-24 — if a shipped feature "stopped working", run `which -a aforge`
  and `shasum` before debugging anything).
- Rebuild after every merge. Never `cp` over a binary that may be running —
  `rm` first, then install — or the next launch dies with `Killed: 9`.
- **Finished work is committed and pushed to `origin chat-v3-task`** in the
  same wave — never left sitting on a local branch or an unpushed worktree. If
  the shared checkout is dirty with another session's work, merge and push
  through a temporary detached worktree (`git worktree add --detach … origin/chat-v3-task`)
  rather than touching their tree.

`make check` is vet, the tests, the build, and the binary-size ratchet in `SIZE-BUDGET`.
The performance laws it and the suite enforce — and the rule that changing any cap
changes the doc in the same commit — are in [PERF.md](PERF.md).

`make demo-home` builds a **throwaway home with something on every place** — three
projects, twelve conversations, standing orders, memories, a fourteen-day spending
ledger — and opens `bin/aforge` against it with `HOME` pointed there. Use it when you
want to SEE a page full: on a machine that has just started using aforge the standing,
memory and spend pages correctly draw nothing, which is indistinguishable from a page
that is broken. It never touches `~/.aforge`. The seeder is `cmd/aforge-demo-home` and
`docs/design/home-rethink/HANDOFF.md` says what is in the fixture and how to add to it.

```sh
make demo-home                                       # a fresh one, in a temp directory
make demo-home DEMO_HOME=/tmp/aforge-demo            # somewhere you can name
make demo-home DEMO_HOME=/tmp/aforge-demo KEEP=1     # open the one that is already there
```

## THE MANUAL LAW — a feature is not done until the manual knows about it

`internal/manual/chat/` is v3's own account of itself, compiled into the binary. The
running chat reads it with the `manual` tool to answer "what can you do?", "what does this
key do?", "why did you just do that?". It is the **only** authoritative source about
aforge for the model: its training data does not contain this program, so anything not in
those pages is something the chat will either improvise or deny having.

**So: if you add, change, or remove a feature, you update the pages in the same change.**
That means a new slash command, a new key, a new tool on the belt, a changed default, a
new limit, a refusal whose wording changed. Not a follow-up task — the same change.

Three gates fail the build if you forget:

| Gate | What it demands |
| --- | --- |
| `internal/tui3/manual_test.go` | every slash command **and every alias**, spelled with its leading slash, appears somewhere in the corpus |
| `internal/session/manual_test.go` | every tool on the belt appears by its exact registered name |
| `internal/manual/chat_test.go` | every probe in its table — real questions in a person's own words (over a hundred by now) — still reaches the page that answers it |

The failure message names the exact missing string.

The corpus ships **packed** — `internal/manual/{pages,chat}.pack.gz`, generated from the
folders by `make build` (see `internal/packed`). Edit the Markdown and never the archive;
`internal/manual/packed_test.go` fails when the two disagree, so a page changed without a
build is a page the binary has not learned.

**When a question reaches the wrong page, fix the page, never the test.** Write the
asker's vocabulary into a `## ` heading — people search for "saved" where a writer wrote
"kept", for "delete a file" where a writer wrote "write". Retrieval is the feature; a page
that is complete, correct and unreachable is a page the chat talks over the top of.

Rules for the pages themselves:

- `# Title` once at the top; every topic gets its own `## ` heading, because headings are
  the search index. Keep each section under ~2000 characters and **self-contained** — it
  gets retrieved and read with none of its neighbours.
- Write what the code does, not what a design doc intends. Quote person-facing strings
  exactly as the code spells them.
- **State limits and refusals.** Someone asking the manual usually wants to know whether
  something is possible; "no, and here is what it says instead" is the most useful answer
  on the page. Never describe half-built machinery as though it worked.
- **When you make something possible, hunt down the page that says it isn't.** The gates
  check that a name is *mentioned*, never that the claim around it is true — a tool named
  in a "what aforge cannot do" section satisfies them perfectly while lying. So a lane that
  lands a capability greps the corpus for the old denial and removes it in the same change.
  `generate_image` was documented as impossible right up until the wave that shipped it.
- The chat's corpus may not use resident vocabulary (`alt+1`, the board, the self page,
  standing watches, front desk). A test enforces this.

The resident's own pages are `internal/manual/pages/` and are a **separate corpus** — the
two cannot reach each other, and a page name may not exist in both.

Also true and easy to forget: `internal/session/prompts/system.md` is what the model is
told it can do. If a tool is conditional or absent, the prompt must not promise it. It has
been wrong before — it advertised `note`/`forget` (which need a `Config.MemoryFile` that
no door sets) and claimed `read` lists directories (it is `os.ReadFile`; a directory is an
error).

## Design laws the codebase enforces

Violations get rejected in review, and some are pinned by tests.

- **The emptiness law.** Unknown or zero renders as *nothing* — never `$0.00`, never
  `0 tok`. (One deliberate exception: the live status line keeps `$0.00` so its segments
  do not jump sideways. `/status` and `/cost` drop the line.)
- **No machinery vocabulary in anything a person reads.** `auditor`, `verdict`,
  `verified`, `refuted` are banned. Work is *running*, *finishing*, *done*, *incomplete*,
  or *needs your look*.
- **A capability that cannot work is absent, not broken.** A tool with nothing behind it
  is left off the belt entirely, so the model does not have the verb — rather than
  present and failing every time it is called. `memoryTools` and the remote harness
  designer are both written this way.
- **Comments are full-sentence prose** stating the *why*, with ALL-CAPS for a stated law.
  Match the surrounding density; this codebase comments heavily and deliberately.
- **One source of truth.** A number that appears in two places will drift — interpolate it
  from the constant. `propose_task`'s schema said the step default was 40 while the
  executor applied 200, and every model that read it reasoned from the wrong figure.

## Working alongside other sessions

Several Claude sessions often work this repo at once, in the same working tree.

- **Never `git add -A` or `git add .`** — stage your own explicit paths. It is easy to
  sweep another lane's in-flight untracked files into your commit.
- Run `ListAgents` before assuming whose work something is.
- Re-run `go build ./...` after fetching: another lane's half-finished file can break the
  tree for everyone.
- Feature waves are built in git worktrees off `chat-v3-task` (`git worktree add
  ~/af-<name> -b <branch>`), merged back, then the worktrees and branches are removed.

## Tests

`go test ./internal/tui3/` takes ~150s; budget for it. These fail on a clean tree and are
**not** yours: `cmd/aforge TestHarnessEntriesFromStore`, `internal/tui`
`TestSettingsSheetIsOneCalmColumnAtEveryWidth`, `internal/plan`, four `internal/swepro`
packages, `internal/session TestTheLegacyWorktreeStaysUnderTheRepository`, four
`cmd/harness-design` tests, `internal/config TestRegistryCoversEveryUserFacingEnvironmentPin`
(`AFORGE_RELAY` in `internal/pair/service.go` is not in the registry), two `internal/guard`
tests (`TestEveryGoroutineInTheGuardedTreeIsGuarded`, `TestEveryLockInTheGuardedTreeUnlocksFromADefer`),
`internal/thread TestEveryMessageWriteUsesThreadPost` (`chatlog.go` posts directly), and two more
`internal/tui` settings tests (`TestSettingsNavigatesAndEditsEveryKindAndPersists`,
`TestSettingsRefusesToFightTheEnvironment`), and on macOS `internal/enginehost
TestTheSocketMovesWithTheStateRoot` (the `t.TempDir()` path is too long for a unix socket;
green with `TMPDIR=/tmp/eh`) — all verified failing at `origin/chat-v3-task`
on 2026-08-26. Two more FLAKE under full-suite load on a clean tree and pass
alone: `internal/session TestOnlyADesignsOwnThreadCarriesTheReviseVerb` and
`TestInterruptedTurnDoesNotWakeOnTheNoteItDrained` — rerun them in isolation before
believing a failure. Confirm anything else with a stash-and-rerun before chasing it.

**Remote access** (`--host`, `--at`, attachments) has three layers, and they are cheap:

```sh
go test ./internal/remote/ ./internal/enginehost/ ./internal/pair/... ./internal/relay/ ./internal/furrow/
make test-remote          # three containers, no API key, ~50s; SKIPS GREEN with no docker
```

`make test-remote` builds its container binaries for **this machine's** architecture; an
`Exec format error` from `modelstub` means that pin was reintroduced. With no second
machine, `--host localhost` is a real connection over a real ssh pipe and exercises
everything except the shared-disk law — `docs/remote-access-testing.md` §3.0 has the tmux
recipe for driving the surface and killing the link on purpose.

