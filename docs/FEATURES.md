# aforge — everything it can do

This is the master feature catalog: one entry per capability, each with
where it lives, how it's used, and the tip-worthy phrasing. It exists to
be *mined* — a trivial LLM call over this document (plus the user's usage
profile from the notebook/journal) can generate the one contextual hint
worth showing while work runs: surface features they haven't touched,
deepen ones they use shallowly. Keep entries self-contained; a tip
generator should be able to lift any single entry without context.

Format per entry — **What** · **Where** · **How** · **Tip seed** (a
one-line hint a user would actually thank you for).

---

## 1. Talking to it

### Just talk — the compiler classifies, you don't
**What**: There are no mode commands. Plain language becomes an answer, a
reflex, a task, a project, or a standing goal based on the *shape* of the
ask — scale is read from structure, duration from temporal language.
**Where**: the chat input, always.
**How**: "open that file" → instant reflex; "fix the flaky test" → task;
"whenever a PR opens, review it" → standing-goal ratification card.
**Tip seed**: You never need to tell aforge how big a job is — say what
you want; it decides ceremony, and misreads are corrected by just saying so.

### The reflex ladder — trivial asks stay trivial
**What**: Four ceremony rungs: answer (no work), reflex (one journaled
micro-leaf, no planner, 1/8 budget), task (compiled), project (subtree).
Reversibility, not size, licenses a reflex; overruns promote upward,
never silently down.
**Where**: automatic in chat.
**Tip seed**: Small asks ("rename this", "open that") don't build task
ceremony — they run as reflexes and still leave a journal trail.

### Ask it to change work in flight
**What**: Cancel, amend, pause, or redirect running/planned nodes
conversationally; the head resolves which node you mean, asks back with
options only when ambiguous, and gates consequential surgery behind a
confirm question.
**Where**: chat, any time work is on the graph.
**How**: "cancel the audio job", "make the PR watcher hourly", "stop
watching PRs".
**Tip seed**: You can redirect running work by talking about it — no need
to find an id or a kill switch.

### Questions come with numbers
**What**: When aforge needs you, it asks with structured components:
choose (vertical `▸ 1 …` rows with dim hints), confirm (`▸ 1 yes · ▸ 2
no`, enter accepts the marked default), text (a dim `answering: <prompt>`
line above the input). Number keys, arrows+enter, click, or free text all
answer; typing your own words always wins.
**Where**: in the thread and on job cards.
**Tip seed**: Press the option's number — or ignore the options entirely
and type what you actually want; free text always beats the menu.

### A stuck question finds you
**What**: A job waiting >2 minutes on an answer promotes its card to the
dock's top slot with a violet `?`, and the header tasks button carries a
violet dot while anything anywhere is waiting.
**Where**: dock strip + header, even with the rail closed.
**Tip seed**: A violet dot on ⟨tasks⟩ means something is waiting on you —
click it to jump straight to the question.

## 2. Voice and media

### Speak instead of typing
**What**: Mic in the input bar (alt+v or click). While you talk: breathing
glyph, live 8-bar waveform, elapsed time, and your words appearing as
faint provisional text — cut at natural pauses, transcribed while you
keep speaking. Stop → one full-clip pass replaces it with final, editable
text, cursor at the end.
**Where**: right edge of the input bar; alt+v toggles, esc discards.
**How**: whatever you had typed stays — voice *appends* to your draft, in
every path including cancel and failure.
**Tip seed**: alt+v talks; your typed draft is never lost — voice adds to
it, and esc discards only the voice.

### Show it images
**What**: Drag an image file into the terminal — the path becomes a dim
`⌾ name.png ⟨×⟩` chip above the input and rides to the model as a real
image when the talk model has vision (a calm hint tells you when it
doesn't). Attachments persist durably on the thread and flow into
spliced tasks.
**Where**: input bar; ⟨×⟩ or backspace-on-empty removes.
**Tip seed**: Drag a screenshot straight into the chat — if your talk
model has vision, aforge sees it.

### Tasks can generate media — any task, any time
**What**: `generate_image`, `speak` (TTS), `generate_music`,
`generate_video`, and `view_image` are graph tools available to every
leaf — chat reflexes and headless runs alike. Artifacts land in the task
workspace under `media/` with readable names and render as one clickable
line (`⌾` image, `♪` audio, `▶` video) that opens in your OS viewer.
**Where**: any task; just ask ("make a diagram of this", "generate an
intro jingle").
**Tip seed**: Ask any task for a diagram, a voiceover, a jingle, or a
clip — media generation is a tool every job already has.

### Model slots — the right model per modality
**What**: Standing slots: talk, work, voice (transcription), image,
speech, music, video. Defaults resolve against a live catalog
(krea-2-medium-turbo images, kokoro-82m speech, qwen3-asr-flash
transcription, lyria-3-clip music, seedance-1-5-pro video) and degrade
gracefully if a slug disappears. Each slot's picker only lists models
that can actually do that job.
**Where**: click any `⌄` model name in the header → type-to-search
dropdown; env overrides (`AFORGE_IMAGE_MODEL`, …) for headless.
**Tip seed**: Click a model name in the header to change it — the list is
pre-filtered to models capable of that slot's job, so you can't pick wrong.

## 3. Standing goals — it acts without being asked

### Say it once, it stands forever
**What**: Durable language ("whenever…", "every morning…", "keep the
suite green", "remind me tomorrow at 9") is *recognized*, never declared —
no /goal command exists. What comes back is the one ceremony in the
system: a ratification card quoting the watch, cost (from measured
history when available), and rails, with `▸ 1 yes · ▸ 2 change cadence ·
▸ 3 once, not standing`.
**Where**: chat; the card appears before anything stands.
**Tip seed**: "Remind me Friday at 3" or "watch this folder" just works —
you'll approve the standing cost once, then it's furniture.

### The standing rail — felt, not seen
**What**: One dim line per charter above tasks (`⏱ pr-watch · last fired
2h · 3 today`), breathing only while a sentinel evaluates or a firing
runs. Routine "checked, nothing to do" firings never touch the thread —
only questions, deliveries, and failures earn a card.
**Where**: the rail (alt+g or header ⟨tasks⟩).
**Tip seed**: Quiet standing lines are good news — a goal only speaks
when it has something worth saying.

### Zoom from goal to everything it ever did
**What**: Click a standing line → the charter card: invariant in your
words, watch, rails, and firing history (when, outcome, cost). Click a
firing → its full job graph, the same flight recorder every task has.
Esc walks back up: graph → card → rail.
**Where**: rail → charter card → firing drill-in.
**Tip seed**: Wondering what a goal has been doing? Click its line — the
history with costs is one click deep, the full graphs two.

### Manage goals by talking (or clicking)
**What**: "pause the PR watcher", "make it hourly", "stop watching PRs" —
references resolve by searching the invariants; ambiguity asks back with
options. The charter card offers pause/retire/edit-cadence as clicks.
`/standing` lists all charters.
**Tip seed**: There's no goals settings page — say "pause it" or click
the goal's line; both end in the same journaled event.

### It may propose — you always dispose
**What**: When the retrospective notices the same-shaped ask ≥3 times, it
may propose one charter per reflection — the same ratification card,
marked `proposed · noticed you ask this most mornings`, default-declined.
Declining is remembered and never re-asked.
**Tip seed**: If aforge notices you asking for the same thing most
mornings, it will offer — once — to just do it every morning.

## 4. Money, not tokens

### The dollar rail
**What**: The only budget is dollars — $20/day default. No token caps, no
turn caps that kill work: a task stops for done, or for your word. At
the ceiling, work pauses and asks; a raise is a journaled event.
**Where**: `/budget` shows today (`spent $3.40 of $20 · resets
midnight`); `/budget 50` raises today; `/budget default 35` persists;
`/budget unlimited today` uncaps the day. Header shows the running cost.
Headless: `AFORGE_DAILY_BUDGET`, `--yes-spend` to preauthorize.
**Tip seed**: Nothing ever dies from a token limit — if the daily
dollars run out, work pauses and asks; `/budget 50` resumes it.

### Every modality is railed
**What**: Image/speech/music/video generation, charter firings,
transcription — all draw admission from the same daily rail and record
real cost from the provider's usage data. Charter firings carry
per-firing quotes and daily caps on top.
**Tip seed**: One number governs everything aforge spends today — check
it any time with /budget.

## 5. It gets better as you use it

### The notebook — beliefs, not logs
**What**: Preferences, quirks, lessons, and facts are captured from every
stage of work, consolidated with evidence links, superseded on
contradiction, aged, and quarantined when poisoned (with injection
attribution). Wrong beliefs are correctable: "that's wrong, retract it."
**Where**: automatic; `aforge notebook` inspects; retract works in chat.
**Tip seed**: If aforge keeps repeating a wrong assumption, tell it to
retract that — beliefs are journaled and die on command.

### The skill forge — tools it builds itself
**What**: When work reveals a repeatable procedure, it becomes a
candidate skill; promotion requires the skill's check script to actually
pass (execution-gated), then it's delivered to future leaves via PATH +
retrieval. Nothing is hard-coded — capability emerges from use.
**Tip seed**: Scripts aforge writes twice tend to become tools it owns —
your recurring workflows are being quietly compiled into capability.

### Experiments over faith
**What**: When two approaches compete and evidence is thin, the
disagreement is stored as an *unsettled pair*; future work runs trials
against it and settles it mechanically, with provenance.
**Tip seed**: aforge doesn't argue with itself twice — unresolved
approach debates become experiments the next relevant job runs.

### Playbooks per scope
**What**: Each work scope accumulates a delta-updated playbook (never
wholesale rewritten) of what works there, composed into contracts for
future jobs in that scope.
**Tip seed**: The tenth job in a repo starts smarter than the first —
scopes carry earned playbooks.

### The router learns who's good at what
**What**: Model capability is measured per work-shape (Rasch ratings)
from verified outcomes only; planning cascades cheap→capable; leaf
execution pins openers by measured fit. Mispredictions feed a surprise
ledger the retrospective attends to first.
**Tip seed**: You don't pick models per task — measured history routes
each job shape to the cheapest model that verifiably handles it.

### Recall — it remembers everything it did
**What**: Full-text recall over verbatim intents, summaries, and fold
digests; executors can pull relevant history mid-task; the head answers
"have we done this before?" from the journal.
**Tip seed**: Ask "how did we solve this last time?" — the graph is
permanent and searchable, including packed history.

### The retrospective — territories, proposals, self-knowledge
**What**: A background reflection packs settled jobs into territories
(folds of folds), audits beliefs, proposes charters, and updates
per-model self-knowledge — attending first to whatever surprised it.
**Tip seed**: Old jobs get packed into territories — the rail stays calm
at any history size, and nothing is deleted, only folded.

## 6. The surface

### Living job cards
**What**: Work lives as cards with four states (compiling, working,
question, settled); active cards dock above the input, settle at their
birth position in the thread, and expand through a disclosure ladder
into the rail's flight recorder.
**Tip seed**: A settled card sits where you asked for it — scroll back to
where the conversation was, not to a log.

### Everything clicks; everything has a key
**What**: Every affordance is explicit (no hover exists): `▸`/`▾` expand,
`⋯` more, `⟨×⟩` dismiss, `⌄` opens a picker. Focus zones cycle
input→dock→thread→rail; arrows+enter act; esc backs out one rung; alt+g
or header ⟨tasks ▸⟩ toggles the rail; slash commands complete inline.
**Tip seed**: If you see ▸ ▾ ⋯ ⟨×⟩ ⌄ — it's clickable, and the keyboard
path always exists (arrows, enter, esc, alt+g, alt+v).

### Honest motion
**What**: Replies stream token-by-token; running work shimmers
left-to-right; plans stream while forming; file paths are clickable
(OSC8); tool calls render with kind glyph + accent name, failures rose
`✗`, outputs guttered.
**Tip seed**: The shimmer isn't decoration — something breathing means
something is actually running; click ⟨tasks⟩ to see exactly what.

### Interruption survival
**What**: Close the terminal mid-run and nothing haunts: on reopen,
orphaned claims release, recovered work resumes with a one-line notice,
and the head's snapshot always shows live work first.
**Tip seed**: Killing the terminal never strands a task — reopen and it
resumes where it left off, saying so once.

## 7. Headless — the same power, scripted

### `aforge plan` / `aforge run`
**What**: The benchmarked, byte-stable CLI path: compile a goal to a
plan, run it with the atomic linear harness. Learning surfaces
(anchors, playbooks, router) feed it without changing its contract.
**Tip seed**: CI and scripts use aforge plan/run — same tools, same
rails, no chat needed.

### `aforge wake`
**What**: One watch pass over due charters — evaluate sentinels, fire
what's due, exit. Lets cron or launchd drive standing goals without a
resident process.
**Tip seed**: No terminal open? A crontab line running aforge wake keeps
your standing goals firing.

### `aforge notebook`
**What**: Inspect, search, and retract beliefs from the CLI.
**Tip seed**: aforge notebook shows what it believes — and what evidence
each belief stands on.

### Environment
**What**: `AFORGE_DAILY_BUDGET`, `AFORGE_PREAUTHORIZE_SPEND`/`--yes-spend`,
`AFORGE_VOICE_MODEL`, `AFORGE_IMAGE_MODEL`, `AFORGE_SPEECH_MODEL`,
`AFORGE_MUSIC_MODEL`, `AFORGE_VIDEO_MODEL`, profile dir config at
`~/.aforge/config.json`.

---

## Using this document for hints

The intended pipeline: `(this file + user's recent usage signals) → one
cheap LLM call → one hint`. Selection guidance for that call:
- Prefer entries whose surface the user has *never* touched (journal
  shows no alt+v? offer the voice tip).
- Prefer deepenings of shallow use (uses /budget to check but never set
  a default? offer `/budget default`).
- Never more than one hint at a time; render it as one dim line in the
  help position, dismissible, never modal — the same Apple-calm rules as
  everything else.
- Tip seeds are starting points; regenerate phrasing in the system's
  voice, don't quote verbatim.
