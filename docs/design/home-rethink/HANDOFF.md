# Home Rethink — local copy of the Claude Design project

Source: https://claude.ai/design/p/0b9b85a6-e381-4265-a10d-16a19ab8a8b9
Project: "Aforge home redesign discovery" · pulled 2026-08-25

**Complete.** All 6 files the project holds are here, byte-for-byte.

## Files

| Path | Bytes | What it is |
| --- | --- | --- |
| `Home Rethink.dc.html` | 108,666 | **The design.** One long dark-mode page of TUI mockups: screens 1a–1g, 2a–2f, 3a–3e. Loads `./support.js` relatively, so open it from this directory. |
| `support.js` | 69,151 | Claude Design canvas runtime (`x-dc` element, `DCLogic` base class). Generated — do not edit. Pulls React 18.3.1, ReactDOM and Babel standalone from unpkg at runtime, so viewing needs network. |
| `github.md` | 2,181 | The project's own sync note: which repo file each screen was built from. **Read this first** — it is the map from mockup to Go source. |
| `uploads/home-concept-inventory.md` | 17,415 | The brief the design answers. Identical to `docs/home-concept-inventory.md` in the repo. |
| `uploads/Screenshot 2026-08-25 at 1.11.41 PM.png` | 376,210 | Reference shot of the current home screen, 2626×1838. |
| `.thumbnail` | 24,416 | The project's gallery preview card, WebP 593×390. Hidden file. |

## Viewing

```sh
cd "docs/design/home-rethink" && python3 -m http.server 8765
# http://localhost:8765/Home%20Rethink.dc.html
```

`file://` also works, though some browsers block the relative script tag.

## Note for the implementing session

`github.md` says `path: internal/tui2` and every screen maps to `internal/tui2/...`.
Per this repo's CLAUDE.md, **`internal/tui3` is the live surface** and `internal/tui2`
is the older one. Settled 2026-08-25: the build targets `internal/tui3`; the tui2 paths
are only where the palette and glyph tokens live, which tui3 imports. The plan and the
decisions are in `LANES.md`; `RECON.md`, `DATA-AUDIT.md` and `PAGES.md` are what it rests
on; `SCREENS.txt` is every artboard's text for a session without a browser.

## The demo home — `make demo-home`

The owner opened the standing page, the memory page and the spend page on their own
machine and said *"fill some standings, memories and spend — I don't see anything"*.
Those stores were empty, and every one of those pages was correctly drawing nothing —
the emptiness law, working, and indistinguishable from a page that is broken. Seeding
the real `~/.aforge` with invented rows would make a person's own record a lie, so the
answer is a **second home, somewhere else**, that the real binary can be pointed at.

```sh
make demo-home                                       # a fresh one, in a temp directory
make demo-home DEMO_HOME=/tmp/aforge-demo            # somewhere you can name
make demo-home DEMO_HOME=/tmp/aforge-demo KEEP=1     # open the one that is already there
```

It builds `bin/aforge`, seeds the directory, opens the surface with `HOME` pointed at it
and the working directory inside the demo's own `aforge-v2`, and prints the command that
comes back to the same home. It writes inside that directory and nowhere else — a test
(`TestTheDemoHomeSeedsNothingOutsideTheDirectoryItWasGiven`) holds it to that.

### What is in it

| Place | What it has |
| --- | --- |
| home | 3 projects — `~/aforge-v2` (a real git repository with two uncommitted files, so the repo band draws), `~/pricing-site`, `~/infra` |
| home | 12 conversations, one archived; one stopped on an answerable consent question, one holding a running task |
| tasks | 7 rows across the three buckets — one running, four landed (one of them a `saved shape`), one failed 8 days back |
| standing | 4 orders in `v3/standing/*.json` — one needing your look, one fired today with a check line and `earning trust 3/5`, one paused, one rule (`holds`) — plus day ledgers so cost per firing draws |
| memory | 12 memories over all three shelves in `graph.db`, with varied use and miss counts and one let go |
| spend | ~57 lines over 14 days across three models, bound to conversations, work and standing orders — every id joins to a row that is really there |
| search | every turn of every conversation in the message index |
| made for you | 3 deliverables, with the files behind them on the disk |

### How it is built, and the two rules it keeps

`cmd/aforge-demo-home` writes through **the engine's own writers** — `session.SaveMeta`,
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
