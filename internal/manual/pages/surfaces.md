# Where you look: places, panels, and commands

## Three places

Aforge has three, and only three, top-level places. Switch with the header, or:

- **thread** — `alt+1` — the conversation, receipts, and results
- **board** — `alt+2` — live jobs and running services
- **self** — `alt+3` — everything aforge does when you are not asking

`‹` and `esc` walk back out of whatever is layered on top.

## Self

Self opens as one calm list, the way a settings app does. The top line is
today — what it cost, what it learned, how long it practised — and under it one
row per thing aforge does on its own. Each row says how many there are and, in
its own words, what they are:

| row | what it holds |
| --- | --- |
| **Crafts** | job-shapes it learned — versioned, measured, reusable |
| **Competence** | where it is strong and where it is at its frontier, measured |
| **Beliefs** | the notebook: what it holds true about you and this machine |
| **Skills** | procedures it forged and verified; they ride every worker's PATH |
| **Watches** | standing goals checking on their own schedule |
| **Services** | processes it keeps alive for you |
| **Practice** | what it did with idle time, and what that taught it |
| **Dials** | how it balances demand against curiosity — read-only, edit in `⚙` |

A row with nothing in it still explains itself, so you learn what would go
there before anything does.

`↑/↓` or `j/k` move, enter (or a click) drills in, `esc` walks back out one
rung at a time — filter, then item, then list, then the place. Inside a
drill-in the letters belong to the filter: **type to search**. Every list shows
a window rather than everything — `214 · showing 20` — with `show 20 more`
under it, and anything older than a week folds behind `N older · type to
search` so a brain that has been running for months still opens instantly.

Opening a craft shows its steps, the bounds a run will obey, and its version
history — the same commits `git log` shows in the craft directory. Opening a
watch or a service hands you to its card on the board, which is the one place
that can act on them.

**Practice** groups repeated attempts at the same goal into one row —
`the goal, clipped · ×6 · $0.67 · nothing yet` — with the count and the cost in
their own aligned columns. Open a row to see the whole goal and every attempt
behind it: when it ran, what it cost, and what it came back with.

## The task rail and drilling into a worker

`ctrl+t` (or `alt+g`) toggles the task rail beside the thread. `[` and `]` nudge
the split. `↑/↓` or `j/k` move the selection, enter opens what is selected, and
`tab` cycles the zones: input, questions, thread, rail, header.

Opening a task turns the input into a **steer** box — the placeholder changes to
*"steer this worker — lands before its next turn"*. Type there and your line
reaches that worker between turns, without re-planning anything. Steered lines
appear in the feed prefixed `steered:`. With the keyboard on the feed, `c` cancels the
worker you are looking at — `tab` moves the keyboard between that box and the
activity feed, and the single-key actions belong to the feed, so nothing you
type into a steer line can act on the job. Close it and your chat draft comes
back exactly as it was.

## Voice

`ctrl+v` (or `alt+v`), or click the mic. Speak; a live transcript appears while
you talk, and a full pass runs when you stop. The result is merged into your
draft — it is never sent for you. `esc` discards it. If the microphone is not
available you are told so, and your draft is kept.

## Answering questions

A pending question sits above the input. With an empty draft, `1`–`9` or the
arrow keys choose an option and enter sends it. You can always type a free-text
answer instead of picking. `esc` dismisses it.

## Settings

Everything you can tune lives in one sheet. Open it with `/settings`, the
`⚙` in the header, or `alt+,` — a bare `,` works too whenever the
cursor is not in the input. It is a single column of grouped rows: **models**
(all eight slots), **money & limits** (the daily budget, the practice
carve-out, the quiet period before practice), **rhythm** (how long an absence
earns an arrival brief, how many clean firings earn a charter tenure),
**learning** (how much practice follows measured demand rather than curiosity,
and whether aforge may propose new skills), **documents & vision** (the reading
rung and the model that looks at images), **sharing** (attribution — whether
aforge signs the commits and pull requests it writes for you), and
**appearance** (the chat/rail split).

`↑/↓` or `j/k` move, enter changes the focused row, `esc` closes an open editor
and then the sheet. Only the focused row explains itself, so the page never
becomes a wall. A model row opens the same capability-filtered picker the
models door opens. Nothing is posted to the thread when you change something —
the row showing its new value is the receipt.

Anything pinned in your environment stays pinned: that row reads dim, says
`pinned by AFORGE_…`, and refuses to be edited rather than writing a value the
shell would keep overriding. The footer lists the operator plumbing that is
set — base URL, profile directory, panel, reasoning — read-only, because those
are the machine's settings, not yours.

## Keys worth knowing

| key | what it does |
| --- | --- |
| `ctrl+j` | newline without sending |
| `ctrl+b` / `alt+b` | cycle boost: armed → pinned → off |
| `ctrl+v` / `alt+v` | voice input |
| `ctrl+t` / `alt+g` | toggle the task rail |
| `alt+1` / `alt+2` / `alt+3` | thread / board / self |
| `alt+,` / `,` | open the settings sheet |
| `v` | expand or collapse reading receipts |
| `y` / `Y` | copy the focused answer / the file it produced |
| `?` | this guide's surface-level twin, when the draft is empty |
| `esc` | back out one layer at a time, then quit |
| `ctrl+c` | quit immediately |

## Slash commands

Type `/` and filter. The full set:

`/graph` `/self` `/tasks` `/node` `/open` `/notebook` `/history` `/budget`
`/standing` `/settings` `/help` `/model` `/memory` `/session` `/new` `/cancel`
`/quit`

`?` opens the same catalogue as a scrollable overlay, generated from that same
list, so the two can never disagree.

## Getting work out

A deliverable's path is printed in full and rendered as a link your terminal can
open. Beyond that: `y` copies the focused answer, `Y` copies the file it
produced, and `/open` hands that file to your machine's own opener. The copy
goes through the terminal's clipboard, so it works the same over ssh as it does
locally.

## From a shell, without the chat

- `aforge` — open the resident chat. A second instance opens as a read-only
  visitor rather than fighting over the same brain.
- `aforge do "<task>"` — one errand, start to finish, with nobody watching. It
  is the same brain the chat runs with the conversation removed: the same
  planning, the same contracts, the same delivery gate, the same repair when a
  gap is found. It works in the directory you are standing in and edits what is
  there. The exit code is the verdict — 0 worked, 1 did not, 2 hit the wall with
  partial work — so a script can believe it. What it prints is the answer, then
  a short footer: the files the run wrote, one absolute path per line under
  `files:`, anything workers told each other under `learned:`, and the elapsed
  time, node count and cost. `--json` prints that same outcome as one object on
  stdout instead, with the files under `artifacts`. Both lists are the files the
  run actually produced, not every path it mentioned. It is a one-shot and
  schedules nothing for later: an errand never practices, whatever store it is
  pointed at with `--db`.
- `aforge wake` — run one bounded pass and exit. This is what the standing watch
  timer runs; you can run it by hand too.
- `aforge doctor` — the brain's path and size, whether a resident is alive, the
  standing watch, today's spend against the rail, active charters, and pending
  questions.
- `aforge notebook` / `notebook retract <seq>` / `notebook restore <seq>`
- `aforge competence` — the measured competence map
- `aforge services` / `services stop <name>`
- `aforge models` — the router ledger: ratings and how many observations back
  each one
- `aforge why self` — today's self-spend, itemized
- `aforge why <node-id>` — what one piece of work actually did: its turns, the
  tools it called with what arguments, what came back, how long each took, and
  how it ended. It answers from the record the worker wrote while it ran, so it
  still answers after the job's working directory is gone. A node whose worker
  keeps no record says so rather than printing nothing.
- `aforge plan "<goal>"`, `aforge run <graph.json>`, `aforge revise`,
  `aforge show` — the static pipeline: build a graph to a file, execute exactly
  what the file says, re-plan it from what happened. Reach for these to read or
  hand-edit a plan. To *do* a job, `aforge do` is the one that thinks while it
  works.

## There is no web surface

Everything is the terminal chat and these commands. If you want aforge running
somewhere you are not sitting, the shape is the standing watch — an OS timer
running `aforge wake` against the same durable brain — not a server.
