# Home rethink — implementation and demo-home handoff

The generated Claude Design HTML/runtime, uploads, thumbnail, and captured artboard
text were removed during the public-repository cleanup. The retained files below
record the implementation decisions, data audit, fidelity rulings, architecture,
acceptance, and demo-home fixture.

## Implementation record

`internal/tui3` is the live surface; `internal/tui2` retains shared palette and
glyph components. Decisions are retained in `LANES.md`, the data inventory in
`DATA-AUDIT.md`, fidelity rulings in `FIDELITY.md`, architecture in
`ARCHITECTURE.md`, and acceptance in `ACCEPTANCE.md`.

## The demo home — `make demo-home`

The owner opened the standing page, the memory page and the spend page on their own
machine and said *"fill some standings, memories and spend — I don't see anything"*.
Those stores were empty, and every one of those pages was correctly drawing nothing —
the emptiness law, working, and indistinguishable from a page that is broken. Seeding
the real `~/.codeaf` with invented rows would make a person's own record a lie, so the
answer is a **second home, somewhere else**, that the real binary can be pointed at.

```sh
make demo-home                                       # a fresh one, in a temp directory
make demo-home DEMO_HOME=/tmp/codeaf-demo            # somewhere you can name
make demo-home DEMO_HOME=/tmp/codeaf-demo KEEP=1     # open the one that is already there
```

It builds `bin/codeaf`, seeds the directory, opens the surface with `HOME` pointed at it
and the working directory inside the demo's own `codeaf`, and prints the command that
comes back to the same home. It writes inside that directory and nowhere else — a test
(`TestTheDemoHomeSeedsNothingOutsideTheDirectoryItWasGiven`) holds it to that.

### What is in it

| Place | What it has |
| --- | --- |
| home | 3 projects — `~/codeaf` (a real git repository with two uncommitted files, so the repo band draws), `~/pricing-site`, `~/infra` |
| home | 14 conversations, one archived; one stopped on an answerable consent question, one holding a running task, one named in full-width CJK with an emoji and a combining accent |
| tasks | 13 rows across the three buckets — one running, one needing your look with a 101-character title, eight landed (one of them a `saved shape`), three failed |
| a conversation's own work | `Sweeping the Frame Budget` keeps a checkpointed task graph of 6 nodes and 3 background jobs in its session folder — see below |
| standing | 4 orders in `v3/standing/*.json` — one needing your look, one fired today with a check line and `earning trust 3/5`, one paused, one rule (`holds`) — plus day ledgers so cost per firing draws |
| memory | 12 memories over all three shelves in `graph.db`, with varied use and miss counts and one let go |
| spend | ~57 lines over 14 days across three models, bound to conversations, work and standing orders — every id joins to a row that is really there |
| search | every turn of every conversation in the message index |
| home's `since you left` | a `.last-look` stamp twelve hours old under `v3/projects`, which is the ORIGIN news is measured from — without it that panel whispers on a fixture full of landed work, because a machine home has never been closed on has no origin and "the first look marks NOTHING as news" (`internal/session/look.go`) |
| made for you | 3 deliverables, with the files behind them on the disk |

### The work one conversation still has out — the room, the jobs, the hostile names

`seed_room.go` writes what the project index cannot reach: the surfaces drawn from a
CONVERSATION's own folder rather than from a project's history. Before it, the jobs
section on the column, a job's page and a task ROOM had nothing on them at all, and
`docs/design/polish/audit-tasks.md` had to read three of its rows out of the source.

It all hangs off one conversation, `Sweeping the Frame Budget` in `~/codeaf`, and it
is written as the two files a live session writes: the session folder's `tasks.json`
(internal/session's `taskDocument`) and, beside it, `tasks/<stamp>_<id>.jsonl` per node
and `logs/jobs/<id>.log` per job.

| What | What it has |
| --- | --- |
| the task graph | 6 nodes, four handings deep — a plan that landed, a piece that landed under it, **one that finished and needs your look** with a 101-character title, one that stopped and said why, and two waiting behind the one that needs you |
| node journals | four of the six replay a real transcript in their room: the brief, assistant turns, reasoning, and expandable tool calls with their results |
| background jobs | three, with logs on disk — one that finished clean (`go test ./internal/tui3/`), one that failed and said why (`make check`, a vet error), and one that was still up when the window closed (`npm run dev`, 250+ log lines so the job page has something to scroll) |
| hostile names | a 101-character task title (`roomLongTitle`), a conversation named `国際化とレイアウト幅 🌏 the café pricing page` — full-width CJK, an emoji, and `e` + a COMBINING acute — and a family four handings deep |

**No running job and no running node is seeded, and there is no way to seed one.**
Execution does not survive a process and the records say so: a job's row is settled to
`stopped` on the way in (`task_store.go`'s `runRowNotice`) and a node the checkpoint calls
running comes back queued and paused (`interrupt`). Writing either would be a fixture
faking a shape the product cannot write, and every frame taken on it would be a frame of
something that cannot happen. A frame of work genuinely in flight needs a live engine —
`internal/e2e` is where that already happens.

**And nothing in the graph may re-enter the frontier.** A resumed session runs its queued
work (`recoverTasks` turns the frontier at the end of recovery), so a bare queued node
would spend real money on invented work the first time somebody opened the demo. Every
queued node waits — directly or through another queued node — on the one that needs a
person, and an unverified prerequisite parks its dependants until somebody resolves it
(`readinessLocked`). `TestTheFixtureOpensWithoutStartingAnyWork` holds that.

`TestTheFixtureHasATaskRoomWithAFamilyInIt`, `TestTheFixtureHasBackgroundJobsWithLogsToRead`
and `TestTheFixtureHasNamesThatDoNotFit` read all of it back by building a real
`session.Agent` on the seeded folder and subscribing its roster lane — the same door
`internal/tui3` opens — so a fixture only its own reader can read fails the build.

**What the first frames on it found** is `docs/design/polish/audit-jobs.md`. The headline:
the engine replays every restored job onto the lane the surface is listening on, and
`internal/tui3/task.go`'s `taskEvent` has no case for it — so the jobs section and the job
page draw nothing on a resumed conversation however much the fixture seeds, and that is a
surface defect rather than a hole in this file.

### How it is built, and the two rules it keeps

`cmd/codeaf-demo-home` writes through **the engine's own writers** — `session.SaveMeta`,
`session.RecordUsage`, `session.RecordArtifact`, `standing.Store`, `store.Store` — so what
the surface reads back is what the product itself produces. There is exactly one place
that spells a file shape for itself, and it says so: a session journal is written by a
live agent through an unexported type and there is no seam for "write me a conversation
that already happened" (`seed_talk.go`'s `journalLine`).

**The projects live under the demo HOME and not under `/tmp`.** A temp-rooted session is
litter to the launch sweep after a week (`internal/session/sweep.go`), so a fixture that
put its projects in a temporary directory would watch its own oldest conversations
disappear on the first launch.

**A presence file is believed for fifteen seconds.** The two clauses on the tab bar —
`1 want you`, `1 moving` — come from `presence.json`, which every reader gives up on after
three heartbeats. So `--launch` runs a heartbeat beside the surface that restamps the rows
the seeding wrote, for exactly as long as the demo is open (`beat.go`). Without it the two
clauses vanish before anybody has finished reading the screen.

**AND SO DOES HOME'S `running` PANEL, which is the reason this matters more than two
clauses.** That panel is `Presence.Fresh(now)` and nothing else (`homepanel_running.go`),
so a demo opened with a bare `HOME=<dir> bin/codeaf` shows it whispering however much work
the fixture seeded — the rows are on disk and every reader has given up on them. Open the
fixture through the seeder (`make demo-home DEMO_HOME=<dir> KEEP=1`, which is `--launch`)
and the panel fills. A screenshot of the demo with an empty `running` is a screenshot of
the missing heartbeat, not of the panel.

`TestTheDemoHomeFillsEveryPlace` reads all of it back through `session.ReadWorld`,
`session.Peek`, `session.ReadTaskIndex`, the standing store, `store.MemorySnapshot`,
`session.ReadUsage`, `Store.SearchConversations` and `session.ReadArtifacts`, and insists
every place has rows — so the fixture cannot rot in silence.

### Two things the demo makes visible that are not the fixture's fault

- Every memory reads `now` and `new today`, because the memory store stamps an event with
  its own clock and there is no door that backdates one. A demo built this morning
  genuinely did learn all of it this morning.
- The spend page's `what it was for` column names a task by its **slug** —
  `put-the-annual-toggle-on-the-pricing-page` — because `internal/tui3`'s `spendNames`
  joins on `TaskIndexEntry.Name`, which is the kebab-cased mention handle, rather than on
  `Label`, which is the title as a row draws it. Worth a one-line fix in a lane that owns
  that page.
