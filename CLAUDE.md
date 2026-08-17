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

Build with `make build` → `bin/aforge`. Rebuild after every merge; the user runs that
binary.

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
| `internal/manual/chat_test.go` | 46 real questions, in a person's own words, each still reach the page that answers them |

The failure message names the exact missing string.

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
`TestSettingsSheetIsOneCalmColumnAtEveryWidth`, `internal/plan`, and four `internal/swepro`
packages. Confirm with a stash-and-rerun before chasing anything in that list.
