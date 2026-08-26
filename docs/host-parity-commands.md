# --host parity: the slash commands and the keys

WIP handoff from the laptop to spark. This is an AUDIT lane's findings, driven
live over an ssh shim (a script named `ssh` first on PATH that execs the far
command with a different `AFORGE_HOME` — a real second process over real pipes
on a genuinely separate disk). Nothing is fixed yet; every row below was
observed on a running `--host` surface, not read off the code alone.

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

`/harness` says `harnesses are unavailable here` — true, but it names no machine
and no reason. One sentence in the register of [connectRemoteWord] would finish
it.

## Stale in host.go's honesty table

The table says `the task rail  absent, and absent by construction`. It is NOT
absent: a hosted surface draws the rail (`+ /task`, `❯ ctrl+g hide`). The deck
belongs to lane fix/task-card-host; the TABLE belongs to this lane and is wrong.

## Not yet driven

The keys: ctrl+t, ctrl+g, ctrl+e, ctrl+l, ctrl+., ctrl+b, ctrl+s, ctrl+o,
ctrl+q, ctrl+,, esc esc, space space, alt+1…7, tab, ctrl+r, the steer chord.
Also `/task`, `/history`, `/resume`, `/new`, `/compact`, `/rewind`, `/attach`,
`/image`, `/select`, `/quit`, and `/files <path>`.
