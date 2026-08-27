# --host parity: the slash commands and the keys

Audit continued from the laptop on spark. The findings were driven
live over an ssh shim (a script named `ssh` first on PATH that execs the far
command with a different `AFORGE_HOME` — a real second process over real pipes
on a genuinely separate disk). Nothing is fixed yet; every row below was
observed on a running `--host` surface, not read off the code alone. The wrong-disk
commands below now stop before reading or writing this machine and name the connected
machine; the remaining keys and commands are recorded below as they are driven.

The rig, for whoever picks this up:

    /tmp/af-shim/ssh          the shim; far state root /tmp/af-far-home/.aforge
    /tmp/af-far-home          seeded by `bin/aforge-demo-home --into /tmp/af-far-home`
    /tmp/af-near-home/.aforge the SURFACE's own state root, so the two disks differ
    tmux -L parity            a private tmux server; the shared one gets killed
    aforge chat --host farbox launched with PATH=/tmp/af-shim:$PATH

## What works, unchanged

`/help` (the whole key sheet, session path prefixed `farbox:`), `/status`,
`/cost`, `/home` (the FAR machine's twelve chats), `/standing`, `/model`,
`/settings` (opens, and says [settingsRemoteWord] on the way in), `/connect`
(refuses with the whole reason), `/files` bare (opens the far workspace's browse
page), `/export` (writes here and says so), `/memory` bare (the place, saying
[memoryRemoteWord]).

## BROKEN — the surface answers for the wrong machine

These five findings are fixed on this branch with an honest refusal. No new wire method
was added, so protocol version remains 5.

1. **`/cache` and `/cache clean`** (`internal/tui3/cachecmd.go`). Reads and
   DELETES `~/.aforge/cache` on the machine the SURFACE runs on. Observed:
   `the cache is empty · /t/a/.a/cache` — the laptop's — while the far machine's
   cache held 800KB. The cache is filled by task workers, and over `--host`
   those run on the engine. A destructive verb pointed at the wrong disk.
   Fix (small): gate over `--host` with one sentence naming the machine, and do
   nothing. A wire method for size/clean is the larger version.

2. **`/permissions`** (`internal/tui3/permissions.go:288`). Lists
   `config.ParseBashApprovals(config.BashApprovalsAt(a.profileDir))` — the
   SURFACE's profile. Observed: the panel drew the laptop's `read:allow` while
   the engine's profile said `write:allow, bash:prompt`. `d` then drops a rule
   on the laptop. The gate that decides is the engine's (Decision 6), and the
   consent card's "always" already writes nothing over a connection.

3. **`/crew <preset>`** (`internal/tui3/crew.go:58`). WRITES the four tier models
   into the SURFACE's `config.json` and reports success. Observed: `/crew frugal`
   put `models.tiers.*` into `/tmp/af-near-home/.aforge/config.json` and left the
   engine's untouched, then said `crew → frugal · brain qwen3.8-27b …`. The crew
   the session resolves through is the engine's.

4. **`/memories`, `/memory <query>`, `/remember`, `/forget`**
   (`internal/tui3/memory.go`). The remote agent is not a `memoryAgent`, so all
   four say `memory is off for this session · turn it on under /settings` — a
   claim about the FAR machine's settings this surface never asked, pointing at
   a row that writes to the wrong machine. The place already has the honest
   sentence ([memoryRemoteWord]); the four commands must say it in one voice.

5. **`/subharness`** (`internal/tui3/subharness.go:867`). Says
   `no subharnesses here yet — a subharness is a saved program for work that
   comes round again.` The surface cannot ask the engine at all, so this is the
   tasks-place fault walked backwards: an emptiness asserted about somebody
   else's disk.

## Weak but honest

`/harness` now names the connected machine and says to change it there.

## Stale in host.go's honesty table

The table now records the hosted task rail drawn from `Places.Task`. Live driving also
confirmed that `/task` itself has no remote task door and refuses honestly; the visible
rail is a reading of work in the far world, not a task-creation capability.

## Remaining command census

| Door | Result | Classification |
| --- | --- | --- |
| `/task` | The far world draws the rail, but creation says `this session has no task door`. | HONEST |
| `/history` | Opens the history held by the attached conversation. | WORKS |
| `/resume`, `/new` | Ask the engine to replace the attached conversation; paths remain prefixed by the machine. | WORKS |
| `/compact`, `/rewind` | Act through the remote agent; an empty rewind says `nothing to rewind`. | WORKS |
| `/attach` | Resolves the source here and carries its bytes to the far session. Bare form refuses with its ordinary usage line. | WORKS |
| `/image` | Deliberately resolves pictures here and carries the bytes with the message. | WORKS |
| `/select` | Local terminal selection mode; it describes and selects the rows already on screen. | WORKS |
| `/files`, `/files <path>` | Browse or fetch from the far workspace through the file seam. | WORKS |
| `/quit` | Closes this surface and detaches from the far conversation. | WORKS |

## Key census

| Door | Result | Classification |
| --- | --- | --- |
| `ctrl+t`, `ctrl+g`, `ctrl+.` | Open/hide the roster and open history from the far world already carried to the surface. | WORKS |
| `ctrl+e`, `ctrl+l`, `ctrl+o` | Reveal thinking, return to latest, and fold tool output in the transcript already here. | WORKS |
| `ctrl+b`, `ctrl+s` | Copy and pointer-selection modes are wholly local to the terminal. | WORKS |
| `ctrl+q`, the steer chord | Follow-up and steer intent travel through the remote agent. | WORKS |
| `ctrl+,` | Opens settings and immediately says which rows are local and which belong to the other machine. | HONEST |
| `esc esc` | Rewinds through the remote agent; with no point to take back it says so. | WORKS |
| `space space` | Opens home from the far world. | WORKS |
| `alt+1`…`alt+7`, `tab` | Move among the seven places. Home, tasks and standing use far data; spend, search and memory draw their explicit other-machine sentence; settings states its split ownership. | HONEST |
| `ctrl+r` | Changes the conversation's reasoning rung through the remote agent and its facts replica. | WORKS |

No key in this census reads or writes an undisclosed local substitute. The three place
keys whose stores have no wire door are honest rather than functional; estimates for
their missing doors are in the final gap table.
