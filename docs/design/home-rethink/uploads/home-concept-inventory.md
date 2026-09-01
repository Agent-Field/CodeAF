# aforge — the concept inventory behind home

A brief for rethinking the home screen. It lists every first-class concept the
program actually holds, what data exists for it, how far it reaches, and whether
home shows it today. Nothing here is aspirational — each entry is backed by a
struct, a file on disk, or a table.

---

## 0. The frame you need before reading the list

**aforge is a terminal program you sit in front of.** Home is what opens when you
start it with no arguments. It is not a web dashboard: it is a keyboard-first,
three-column text screen, redrawn every frame, on a terminal that may be 60
columns wide or 200.

**Four levels of reach.** Almost every concept below is stamped with one, and
this is the single most useful axis for organizing home:

| Reach | Means |
| --- | --- |
| **machine** | true across everything on this computer |
| **project** | scoped to one workspace/repo directory |
| **conversation** | scoped to one chat session |
| **task** | scoped to one unit of background work |

Home's current right-hand column already switches on this: put the cursor on
nothing and it draws *the machine*; on a project heading and it draws *the
project*; on a session row and it draws *the conversation*.

**Design laws already binding on this surface** (a redesign must keep these):

- **The emptiness law.** Unknown or zero renders as *nothing at all* — never
  `$0.00`, never `0 tasks`, never a placeholder. An empty list is the whole
  answer and costs no row.
- **No machinery vocabulary in anything a person reads.** Work is *running*,
  *finishing*, *done*, *incomplete*, or *needs your look*.
- **A capability that cannot work is absent, not broken.**
- **Nothing on the card column may grow with the data.** A list-shaped section
  draws a few rows then folds: `▸ …5 more tasks`.
- **One accent per screen**, and it always goes to the live thing — the row
  waiting on somebody, the work in flight.
- **Nothing blocks.** Anything reading a file or running a command answers from
  a cache keyed by subject.

---

## 1. What home shows today (the baseline)

**Top line — the pulse.** Product name, then machine vital signs: how many
standing orders are on watch, whether one is firing right now, what the day has
spent against its allowance, the clock.

**Left column — two strips.**
- `needs you` — everything across the machine stopped waiting on a person.
- `moving` — everything in flight anywhere. Exactly one spinner.

**Middle column — the world.** Every project on the machine, each with its
conversations, ordered by when somebody last spoke. Most projects collapsed.

**Right column — a card about whatever the cursor is on.** Registered bands,
drawn in this fixed reading order:

| Order | Band | Draws for |
| --- | --- | --- |
| 5 | folder is gone | project |
| 10 | what it is doing / stopped on | conversation |
| 15 | the question it is stopped on, answerable here | conversation |
| 25 | watchlist — everything keeping an eye on anything | machine |
| 28 | hands — what it has been doing lately | machine |
| 30 | news since you last looked | conversation |
| 35 | since you left, asked of every project | machine |
| 40 | the tasks it ran, and what they came to | conversation |
| 50 | deliverables — the files it produced | conversation |
| 60 | next up — standing items that will wake, and when | conversation |
| 70 | where the conversation left off | conversation |
| 80 | where the repository stands (branch, dirty files) | project |
| 90 | what it has cost | conversation |
| 93 | the rung it thinks at | machine, standing item |
| 95 | what the day came to, counted and priced | machine |
| 100 | what the keyboard does here | all |

**The machine card's computed facts:** items on watch, news across all projects,
conversations spoken in today, tasks run today, dollars spent today, the daily
ceiling, whether a pass is firing right now, and `hands` — how many things are
in flight across every project and every kind at this instant.

---

## 2. Concepts with a strong presence on home

### Project
One workspace directory and everything held in it.
**Data:** bucket (encoded dir name), full path, display name, ordered list of
sessions, count of sessions running, timestamp of last speech in any of them.
**Reach:** project. **On home:** yes — the middle column is a list of these.

### Conversation (session)
One chat. The unit a person actually sits in.
**Data:** id, transcript path, directory, project + project dir, title (self-named
by a model), workspace, owned flag, model, last-activity time, created time,
open flag, presence (who/what is attached), live flag, spend in dollars, token
count, a task rollup (running/done/needs-look counts), archived flag.
**Reach:** conversation. **On home:** yes — rows, plus a card of bands.
**Stored:** `~/.aforge/v3/projects/<encoded-dir>/<session-id>/` with
`meta.json`, `transcript.jsonl`, `presence.json`.

### Task (work that runs on its own)
Background work commissioned from a conversation, with its own room and record.
**Data:** state (proposed → running → finished / needs your look / incomplete),
brief (shaped, not verbatim), name, model, spend, parent conversation, sub-tasks
it handed out, tool calls, its final report, its room transcript.
**Reach:** task, rolled up to conversation and machine.
**On home:** partly — a per-conversation `work` band and the machine's `moving`
strip. The full task history lives behind `ctrl+.` / `/history`, not on home.

### Standing order (reminders, watches, routines, rules)
Something that stays true after the conversation that created it ends.
Four kinds: a **reminder** (fires once), a **watch** (fires when a condition
changes), a **routine** (every morning, every Monday), and a **rule** with
nothing to wake it (always/never/we-do-it-this-way).
**Data (rich — the best-instrumented concept in the program):** verbatim words
the person said, workspace, origin, when-clause, action, rails (daily allowance),
altitude (this chat / this project / everywhere), brief, grant (what it may do
without asking), exceptions, status, created, updated, why it retired,
lastChecked, lastCheckLine, nextDue, file fingerprint, last N sentinel lines with
outcomes, run count, lastFired, lastOutcome, lastRun, dollars spent, and
`needsPerson` — the one line it is stopped on.
**Reach:** machine, project, or conversation, by altitude.
**On home:** yes — `◦` rows, a watchlist band, a next-up band, ctrl+e/ctrl+x to
pause. **Stored:** `~/.aforge/v3/standing/*.json` + daily ledgers.

### Spend and the daily allowance
**Data:** dollars and tokens per conversation, per task, per standing item;
a machine-wide daily total; a machine-wide daily ceiling; the day's task count
and conversation count.
**Reach:** all four. **On home:** yes — pulse segment, `today` band, spend band.

### Deliverables (files it made for you)
**Data:** a machine-wide append-only ledger at `~/.aforge/v3/artifacts.jsonl`,
one line per artifact: absolute path, title, kind (`export`, image, audio,
video, document), created timestamp.
**Reach:** machine, though home only ever shows the per-conversation slice.
**On home:** per-conversation band only. **The machine-wide ledger is unshown.**

### Repository state
**Data:** branch, dirty file count, read live from the project directory.
**Reach:** project. **On home:** yes, one band.

---

## 3. Concepts that exist and are NOT on home

These are the interesting ones for a redesign. Each is real, each has data, and
none of them is currently reachable from the home screen.

### Memory — what aforge remembers about you
Lines kept across conversations, routed into the model's context per turn based
on what the message needs.
**Data per memory:** id, type (fact / preference / project-state / correction),
scope (**user / project / env**), title, text, tags, status (**active /
forgotten / superseded**), `UseCount` (times it actually *helped*), `MissCount`
(times it was injected and bore on nothing), updated-at, provenance (which
conversation wrote it). Plus machine-level counters and a tidy/merge record.
**Reach:** machine (user scope) and project (project scope).
**On home:** **nothing.** Only `/memory`, `/memories`, `/remember`, `/forget`.
*Design note: scope, status, and the help-vs-miss ratio are all natively
home-shaped facts — "what does this machine believe about me" is a machine-card
question that has no band.*

### Learned fixes — errors it has seen and repaired before
**Data:** `~/.aforge/v3/fixes.json` — per entry: the tool, the error signature,
the fix, and machine-wide counters `asked / found / worked / failed`, plus a
decay timestamp.
**Reach:** machine. **On home:** **nothing.** Surfaces only as a line mid-chat
("this error was fixed before").

### Saved programs (subharnesses)
Saved, versioned programs for a recurring shape of work. None ship with the
binary: one exists once a designer has written it.
**Data per program:** name, one-sentence description, the words a designer said
it answers to, matcher, version record (hash, parent, why), bundle (manifest,
program.js, prompts, seed memory), what it has learnt, how its last run went.
**Reach:** machine. **On home:** **nothing.** Only `/subharness`.
**Stored:** `~/.aforge/subharnesses/<name>/`.

### Saved shapes of work (harnesses)
The designed-on-demand cousin: aforge offers to build one, you approve it, it is
saved and re-runnable. Installed today: `social-marketing-images`,
`top-movers-research`.
**Data:** the design's own task and room, the draft, review findings, steps and
lanes, save-or-discard state, run history.
**Reach:** machine. **On home:** **nothing.** Only `/harness`.

### Adaptive runs
A planner model plus a frontier scheduler: the graph is never designed, it
crystallizes as the trace of what the planner did.
**Data:** planner model, node graph, per-node chat/thinking/tool calls, budget
and what happens when it runs out, repeat-guard state, `forming the work` phase.
**Reach:** task. **On home:** only as generic running work — the run as a *kind*
is invisible.

### The effort ladder (just landed)
Five named rungs — low, medium, high, xhigh, max — one resolver, and one chord
that moves the rung of whatever surface you are standing on. Scoped per process,
per conversation, per task, per standing item.
**Data:** current rung per scope, the resolution chain, the chip above the input
box.
**Reach:** all four. **On home:** a thinking band on the machine card and on a
standing item's card. Not on the pulse, not per-project, not per-conversation.

### The crew — models aforge uses on your own behalf
Five models total: the one you talk to, plus four crew seats bound to tiers.
Every auxiliary LLM call (session titles, compaction summaries, commit messages,
the advisor's aside) is a named **role** that inherits its tier's model, with
per-role pinning and a fall-through ladder when a model is down.
**Data:** the five bindings, per-role pins, presets (frugal / balanced / max),
which model answered which role, ladder fall-through events.
**Reach:** machine. **On home:** **nothing.** Only `/crew`, `/model`, `/status`.

### Model catalog
**Data:** `~/.aforge/v3/models.json` (89KB) and `model-catalog.json` (320KB) —
every OpenRouter model with modality flags (sees / draws / speaks / films /
hears), context window, price. Plus `model-quirks.json`.
**Reach:** machine. **On home:** nothing.

### Task-size calibration (the ruler)
Measured records of what tasks actually cost each model, used to recalibrate how
the planner sizes work. Six profile files on this machine today.
**Data:** `~/.aforge/profile-<model>-<kind>.json`, per model and per work kind
(`linear`).
**Reach:** machine. **On home:** nothing. Not person-facing anywhere.

### Permissions — what runs without asking
**Data:** banked rules with a width (once / this session / written down), the
approval mode, per-tool exceptions, shell-command allow/deny shapes, two floors
nothing can lift (dangerous shell commands; anything acting in your name),
`--yolo` state, and the guardian (a model answering the easy ones for you).
**Reach:** machine and conversation. **On home:** only indirectly — an
unanswered approval becomes a `needs you` row. The *standing posture* is unshown.

### Connected accounts
Keys that let aforge act on your own SaaS accounts, each with a per-use setting
of **yes / ask first / off** — where "off" means the tool is not on the belt at
all. Google is the only plug today; MCP servers are the general case.
**Data:** service registry, per-account auth state, per-capability setting, armed
MCP tool names and count.
**Reach:** machine. **On home:** **nothing.** Only `/connect`.

### Remote machines
`--host` runs the engine on another machine over ssh; pairing codes reach one
without ssh; attachments and file browsing cross the link.
**Data:** `~/.aforge/v3/hosts/<id>/` — per-host records; connection state;
what does and does not work over the link.
**Reach:** machine. **On home:** home renders *on* the far machine, but there is
no notion of "my machines" on home.

### Background jobs
Long-running commands — a server, a build, a watch — held beside tasks.
**Data:** the `jobs` tool; rows on the task column.
**Reach:** conversation. **On home:** nothing.

### Typed history
Every line you have typed, kept across sessions.
**Data:** `~/.aforge/v3/history.jsonl` (append-only JSONL).
**Reach:** machine. **On home:** the up-arrow only.

### Search across everything
Full-text and semantic search over old conversations and messages.
**Data:** message index over the transcripts; `search_conversations` tool;
`/history` for tasks.
**Reach:** machine. **On home:** yes, typing searches — but ranked worst-first
and with no facets.

### The shared build cache
**Data:** `~/.aforge/cache/toolchain` — size on disk, location, cleanable.
**Reach:** machine. **On home:** nothing. `/cache` only.

### Craft assets
**Data:** `~/.aforge/craft/{exemplars,skills,verifiers,workflows}` — empty on
this machine but a live directory contract.
**Reach:** machine. **On home:** nothing.

### The tool belt itself
What the model can actually do, which changes with what is connected.
Registered verbs: `bash`, `read`, `write`, `edit`, `find`, `research`,
`web_search`, `web_fetch`, `read_document`, `view_image`, `generate_image`,
`generate_music`, `generate_video`, `speak`, `remember`, `recall`, `stand`,
`watch`, `tasks`, `jobs`, `propose_task`, `divide_work`, `run_adaptive`,
`propose_subharness`, `list_subharnesses`, `build_harness`, `list_harnesses`,
`revise_design`, `manual`, `services`, `use_service`, `gmail_search`,
`gmail_read`, `gmail_send`, `calendar_create`, `calendar_list`,
`search_conversations`, `whoami`, `commit`, `track`.
**Reach:** machine, narrowed by account settings. **On home:** nothing.

### Context and compaction
**Data:** how full the window is, the fill percentage before compaction fires,
the completion reserve, what a compaction pass kept, the 256,000-token
per-request ceiling, whether auto-compaction is off.
**Reach:** conversation. **On home:** nothing. `/status`, `/cost` only.

### Presence — who else is here
**Data:** `presence.json` per session; the two-terminals-same-folder case; the
"another window is already in these files" warning.
**Reach:** conversation and project. **On home:** partly — live/open flags on a
row, but no notion of *other windows* as a thing on the machine.

---

## 4. A separate product in the same binary — do not mix these in

`internal/head` and `internal/resident` are the **resident**: an employee that
keeps working while the terminal is closed. It shares the repository and the
`graph.db` file with the chat and almost nothing else. Its concepts have real
tables and no path to home:

**charter, competence, growth, practice, taste, tenure, territory, traits,
trials, skills, notebook, receipts, retrospective, worker, product, role
bindings, lineage, activation, admission, gates.**

They are listed only so a designer does not discover them in the schema and
assume they belong on home. **They do not.** The chat's own manual is forbidden
from using resident vocabulary, and a test enforces it.

---

## 5. The gaps, stated plainly

Ranked by how machine-shaped the concept is and how absent it is from home:

1. **Memory** — a rich, scoped, self-scoring store of what aforge believes about
   you, with zero presence on the machine card.
2. **The crew and the effort ladder** — how this machine thinks and what it
   costs to think, configurable, currently only reachable by typing a command.
3. **Saved programs and saved harnesses** — the machine's accumulated
   capabilities. Two harnesses and two subharnesses exist on this machine and
   home never mentions them.
4. **Connected accounts** — what aforge can reach on your behalf, and what it
   will ask before doing.
5. **Permissions posture** — not the individual pending question (home has that)
   but the standing answer: what runs without asking, machine-wide.
6. **The machine-wide deliverables ledger** — every file aforge has ever made
   for you, in one append-only list, sliced per-conversation only.
7. **Learned fixes** — measurable, self-scoring, and completely invisible.
8. **Machines** — `--host` exists, hosts are recorded, and there is no "my
   machines" concept anywhere on the screen.
