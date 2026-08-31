# Multi-attach: any chat reachable from any terminal — plan for #52

Written 2026-08-31 against `dev@d0ad600b`. This is the plan for
[issue #52](https://github.com/Agent-Field/aforge-v2/issues/52), and it changes one of
the issue's groomed decisions, for a reason the grooming did not have in front of it.

## What already exists (verified in code)

| Piece | Where | State |
| --- | --- | --- |
| Per-workspace session host on a unix socket, attach-or-spawn under a flock, retires when idle or when its binary was replaced | `internal/enginehost` | built, tested |
| The room's one keyboard: newest window drives, `MethodTake` moves it in one round trip, a detach hands it to the newest survivor, drafts kept | `internal/remote/driver.go` | built, tested |
| Watcher UX: dim `typing from <machine> now · enter takes it back`, box hidden not cleared | `internal/tui3/watching.go` | built, tested |
| `aforge engine` attaches to the host, spawns one, or serves on the pipe; `--no-host`, `--stop` | `cmd/aforge/engine.go` | built |
| Host `open` joins a hello to a session already running under that key — "THE WHOLE PRODUCT IS THIS LINE" | `internal/enginehost/host.go:277` | built |

The gap is exactly the issue's: nothing local ever dials the host. `aforge chat` opens the
session in-process; a second terminal meets the journal's flock and `openV3Agent`
(`cmd/aforge/chatv3.go:976`) **silently starts a new conversation**; home's held row is
refused with `open in another window — go there, or start a new conversation here`.

## The fact the grooming did not have: what a connection switches OFF today

Every capability below is on in a local in-process session and **off or degraded over
the wire**, at the engine (`cmd/aforge/engine.go:355-441`) or at the surface
(`internal/tui3/host.go:45-110`, the honesty table). Decision 1 as written — "local
`aforge chat` routes through the session host by default" — would turn all of it off for
every local session on the day it lands:

- **Adaptive runs**: off — the fuel gate rides a standing subscription the wire has no
  door for; the engine builds with `session.New`, so the model does not have the verb.
- **Building a harness** (the design lane) and **subharness intake cards**: off at the
  engine (`Config.HarnessStore = nil`, `HarnessCards` false) — same reason, no wire door
  for the standing lanes. `internal/remote`'s held room deliberately holds four kinds of
  question and not five.
- **Browser sign-ins** (`/connect`): the ask card offers only "not now" (the browser is
  here, the loopback port is there). Key sign-ins work.
- **`/settings`**: opens with a warning; the session half reads the other end's profile.
- **The consent card's "always"**: nothing is saved; it says `allowed`, not `saved`.
- **`--max-hours` / `--max-cost`** ceilings: refused ("cannot travel").
- **`/harness`** registry door: absent. The git branch probe: off. Changing a running
  task's model: absent.
- **The keeper** — several live conversations in one window, `tab` between them, home as a
  switcher rather than a swap (`internal/tui3/keeper.go`) — over a link `Options.Open` is
  nil, so home falls back to `Resume`, which SWAPS which session the connection holds.
- **Cross-project rows on home**: a host is one workspace by the chdir law
  (`bootEngine` chdirs; "the only chdir in the tree"). Enter on another project's row
  over a link opens it in the wrong host's process or not at all — not verified either
  way, but the law says it cannot be right.

`docs/REMOTE.md` already names the fix for the biggest of these: *"What would light this
up is a wire door for the standing lanes, which is a lane of its own."*

## Staging — the change to decision 1

**Decision 1 is kept as the end state and split into two releases.** Flipping the local
default before the wire carries the standing lanes is a regression a person would meet on
the first `/task` that divides its work, and "one code path" is not worth that. The
in-process door and the host door are both real today; R1 uses the host wherever it is
the only way to reach a chat, and R2 makes it the only door once nothing is lost by it.

### R1 — no chat unreadable from a terminal (this wave)

Acceptance 1–5 of #52, without regressing a single-window local session.

1. **Local dial.** `aforge chat` (and `aforge resume`) tries the workspace's host first:
   `enginehost.Attach(workspace, spawn "engine --daemon")` gives a `net.Conn`; the
   surface is opened by the same `openChatV3Host` road with a local dialer instead of an
   ssh child (`remote.Roam` already takes any `io.ReadWriteCloser`). **When to take
   it:** when a host for this workspace is already up (something is open there), or when
   the requested session's journal is locked. Otherwise the in-process door runs as
   today — every capability above intact. `--no-host` forces in-process; it exists
   already on `aforge engine` and gets the same meaning on `chat`.
2. **Silent new conversation is dead.** `openV3Agent`'s lock fallback becomes a refusal
   that names the road: the host attach above is tried *before* the in-process open, so
   the only way to reach this branch is a lock held by something that is not a host (a
   build older than this one). The sentence says so and points at `aforge engine --stop`.
3. **Home's held row attaches.** When the surface has a link, `homeHeldNow` does not
   refuse: enter opens the row through the link and the host joins it (watcher, or driver
   if it is going spare). With no link (a plain in-process window whose row is held by a
   *host* on this machine), enter dials that workspace's host and opens the row there —
   the same road as 1, from inside a running surface. The one row that stays refused is a
   journal locked by a non-host process, with the sentence from 2.
4. **The link's local spelling.** `hosted()` is `a.host != ""` today and gates far-machine
   behaviour (home reads the far world, paths carry the machine name, the honesty table
   fires). A local host is a link with no machine: `linked()` for the keyboard/watcher
   half, `hosted()` stays for the far-machine half. Status line: no `via` segment; the
   watcher line reads `typing from another window now` (already the same-machine
   spelling in `watching.go`).
5. **Drafts both ways** — already the driver's law; pinned by a local two-window test.
6. **Manual**: `home.md` §"Why can't I open a session from home" rewritten (it becomes
   a door), `keeping-an-eye.md`/`screen.md` for the watcher line locally, probes:
   "open the chat that is running in my other terminal", "two terminals same chat",
   "take control from the other window", "why did it start a new conversation".

What R1 deliberately leaves: the keeper over a link (home is a swap there, as it is over
`--host` today), cross-project rows over a link (refused with a sentence naming the
other project's own terminal — measured, not guessed), and every OFF row in the table
above *for the hosted window only*. A person who never opens a second terminal on a chat
never meets any of it.

### R2 — one door (next wave, gated on the wire)

- Wire doors for the standing lanes: harness designs, orchestrations (with the fuel gate
  through the held room), subharness cards, task model change. This is the lane
  `REMOTE.md` names; it is the bulk of R2.
- The keeper over a link: several sessions on one connection (protocol: a session key per
  frame) so home stays a switcher.
- One host per **state root** rather than per workspace, or a host that can hold engines
  for several workspaces — needs the chdir law rewritten (per-engine cwd), which is a
  session-package change.
- Then flip: `aforge chat` dials the host always; `--no-host` remains the escape hatch;
  the in-process door survives only as the host's own engine boot.

## Lanes for R1

Each lane is a worktree off `dev`, integrated in the order below; each carries its own
tests and manual pages.

| Lane | Scope | Files |
| --- | --- | --- |
| L1 local dial | `openChatV3Local` (host attach → `remote.Roam` over the socket), the when-to-take rule, `--no-host` on `chat`/`resume`, the lock refusal replacing the silent new | `cmd/aforge/chatv3.go`, new `cmd/aforge/chatv3_local.go`, `internal/enginehost` (a `Dialer` for a live host), tests in `cmd/aforge` and `internal/enginehost` |
| L2 surface | `linked()` vs `hosted()`, home's held row through the link and through a fresh local dial, the watcher line's local spelling, the two-window test with a lab host | `internal/tui3/home.go`, `hostlink.go`, `host.go`, `watching.go`, `welcome.go`, tests |
| L3 manual + e2e | pages and probes above; a tmux script under `docs/` that opens two terminals on one chat, checks the watcher line, `enter` in both directions, drafts kept — the `--host localhost` recipe in `docs/remote-access-testing.md §3d` with no ssh | `internal/manual/chat/*.md`, `internal/manual/chat_test.go`, `docs/multi-attach-testing.md` |

L1 and L2 can run in parallel against an agreed seam: `tui3.Options.Link` non-nil with
`Host == ""` means "linked, local". L3 starts when L1 lands (it needs a binary).

## Open rulings

1. **Accept the staging** (R1 attach-where-needed, R2 flip the default) — or flip now
   and accept the OFF table for every local session.
2. **Cross-project rows over a link in R1:** refuse with a sentence, or make the surface
   dial a second host and swap the client (a bigger L2).
3. **Should a host persist a local conversation past the terminal in R1** (it will, by
   construction — the host outlives the pipe) — or should a lone window's close retire
   its host at once, keeping today's "closing the terminal ends the conversation"?
