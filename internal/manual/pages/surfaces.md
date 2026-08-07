# Where you look: places, panels, and commands

## Three places

Aforge has three, and only three, top-level places. Switch with the header, or:

- **thread** — `alt+1` — the conversation, receipts, and results
- **board** — `alt+2` — live jobs and running services
- **self** — `alt+3` — the employee file: today, competence, beliefs, standing

`‹` and `esc` walk back out of whatever is layered on top.

## The task rail and drilling into a worker

`ctrl+t` (or `alt+g`) toggles the task rail beside the thread. `[` and `]` nudge
the split. `↑/↓` or `j/k` move the selection, enter opens what is selected, and
`tab` cycles the zones: input, questions, thread, rail, header.

Opening a task turns the input into a **steer** box — the placeholder changes to
*"steer this worker — lands before its next turn"*. Type there and your line
reaches that worker between turns, without re-planning anything. Steered lines
appear in the feed prefixed `steered:`. With that box empty, `c` cancels the
worker you are looking at. Close it and your chat draft comes back exactly as it
was.

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
| `?` | this guide's surface-level twin, when the draft is empty |
| `esc` | back out one layer at a time, then quit |
| `ctrl+c` | quit immediately |

## Slash commands

Type `/` and filter. The full set:

`/graph` `/self` `/tasks` `/node` `/notebook` `/history` `/budget` `/standing`
`/settings` `/help` `/model` `/memory` `/session` `/new` `/cancel` `/quit`

`?` opens the same catalogue as a scrollable overlay, generated from that same
list, so the two can never disagree.

## From a shell, without the chat

- `aforge` — open the resident chat. A second instance opens as a read-only
  visitor rather than fighting over the same brain.
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
- `aforge plan "<goal>"`, `aforge run <graph.json>`, `aforge revise`,
  `aforge show` — the headless path: build a graph, execute it, re-plan it from
  what happened.

## There is no web surface

Everything is the terminal chat and these commands. If you want aforge running
somewhere you are not sitting, the shape is the standing watch — an OS timer
running `aforge wake` against the same durable brain — not a server.
