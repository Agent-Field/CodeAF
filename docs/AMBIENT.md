# The ambient side — what aforge does when you are not asking

*Design doc, 2026-08-20. Status: grooming. Decisions marked OPEN are not settled.
Nothing here is built. Companion to CHAT-V3.md (the session) and home-design.md (the
place a window opens onto). STANDING.md and ARCHITECTURE.md Decision 5 are the v1
ancestors; this document says what v3 takes from them and what it leaves.*

## The one-sentence design

A conversation can leave something behind that keeps working after the window is
closed — a reminder, a watch, a rule, an overnight job — and the machine keeps those
things honest, cheap, and quiet with no server, no daemon, and no new place to look.

## Part 1 — What a person gets

These are written as things you say. Every one of them is recognised from ordinary
words; none of them is a command. Every one of them shows you a card with what it will
do, when, and what it costs, and does nothing until you say yes.

### Reminders and routines

- "Remind me at 6 to leave."
- "Remind me every Sunday to water the plants."
- "Every Monday at 9, draft the weekly update from the git log and my calendar."
- "Every weekday at 8, read my inbox and list what needs a reply today."
- "At the end of each day, write two lines on what I committed, for my standup."
- "On the first of the month, pull the Hetzner and OpenRouter invoices into finance/."

A reminder is the smallest of these: it fires once, it says one line, it retires.

### Watches on the world

- "Tell me when CI on main goes red."
- "Tell me when a PR opens on this repo." / "…when someone reviews mine."
- "Watch this log for `OOM`." / "Tell me when port 5173 comes up."
- "Tell me when the Dyson drops under 500." / "…when the docs site returns anything but 200."
- "Tell me when a new release of `charm.land/bubbletea` ships."
- "Tell me when disk on this box goes under 10 GB."
- "Tell me when the TLS cert on api.example.com has under 14 days."
- "When an invoice from Hetzner lands in mail, file it under finance/."
- "When Priya replies on that thread, draft an answer for me."
- "Fifteen minutes before a meeting with the design team, brief me from our last thread."
- "When a Linear issue is labelled `bug`, try to reproduce it and tell me what you found."

A watch can look with a shell command, a URL, a file glob, or a tool on a connected
account (mail, calendar, Linear, Notion, Slack, anything a signed-in service brings).
v1 built its watches before aforge had accounts; v3 has them, and that is what makes
half of this list possible.

### Rules you stop thinking about

- "Keep main green." — on red, a task investigates in its own worktree and either lands
  the fix under the rules you have already banked, or stops as `needs your look`.
- "Whenever a PR opens, review it and leave a summary."
- "Whenever I push a branch, run the tests and tell me before I open the PR."
- "Keep the benchmark number from regressing more than 5%."
- "Keep the README's install command working — try it on a fresh container every week."
- "Keep the manual in step: whenever a slash command or tool is added, check the pages."
- "Keep dependencies current — open one PR a week with what moved and whether tests pass."
- "Track the flaky tests — tell me on Friday which ones failed more than twice."

A rule's firing is an ordinary v3 task: a brief, a worktree, a landing, a cost row, a
room you can walk into. Nothing new to learn.

### Overnight and long-horizon work

- "Tonight, run the full suite and the benchmark and have a report for me in the morning."
- "Try the migration overnight. Stop if tests fail. Spend at most $5."
- "Keep trying to get the port build green — up to four attempts, then tell me where it stands."
- "Every night, one line on what changed in this repo since yesterday."
- "Every Friday, a changelog draft from the week's merges."

Close the laptop lid. A one-shot with an expiry, or a rhythm. The result is waiting in
the conversation that asked for it.

### Follow-ups the work proposes by itself

A task finishing can offer, on a card, default declined:

- "re-run the suite when `schema.sql` changes?"
- "check the deploy in 20 minutes?"
- "wait for this PR to be reviewed, then address the comments?"
- "you have asked for this most mornings — make it a routine?"

Never armed by itself. Declining is remembered and not re-proposed.

### A watch that survives the window

Today's `watch` tool — a command on a timer that speaks only the delta — dies with the
terminal. With this, closing a window that still has one asks once: keep watching? Yes,
and tomorrow the same conversation hears the news when you resume it.

### Home becomes the inbox

One screen, every project: `▲` needs you, `◆` news since you left, a dim count of what is
being kept an eye on. Open a conversation that was busy while you were away and it opens
on one fold — `while you were away` — what fired, what it found, what needs you, what it
cost. Twenty minutes away: nothing, because you missed nothing.

### Nothing to run, nothing to expose

No server, no port, no account, no config file. An OS user timer you said yes to once;
a status you can read in `/status` that is derived from files, not asserted; a daily
rail that stops spending and asks. Over `--host` the watches live on the devbox, where
the work is — not on your laptop.

### What it will not do, said plainly

- It will not fire while the machine sleeps. launchd and systemd (`Persistent=true`)
  catch up when the machine wakes, and the brief says a check was late.
- It will not reach your phone. There is no outward lane (OPEN — see Part 5).
- It will not spend past the daily rail, or past a firing's own cap, without asking.
- It will not do anything unattended that you have not already allowed in a window.
  Nobody present means the consent gate denies, and the work stops as `needs your look`.
- It will not pretend a check happened. A watch that ran for thirty mornings and found
  nothing reads differently from one that never ran.

## Part 2 — What v3 takes from v1, and what it leaves

v1's mechanism is right and small; what it is wrapped in is not needed.

| Keep (≈2.7k LOC in v1) | Leave (≈40k LOC in v1) |
|---|---|
| OS user timer → `aforge wake` → bounded pass (120 s, 32 ticks, early stop). `internal/watchdog` reused nearly verbatim. | The SQLite event graph and the head/resident split. v3 is folders + JSONL. |
| A live window holds a lock; the timer's wake refuses if held. `internal/lease` shape. | Practice loop, belief aging, retrospectives, self-audit dials, competence map, surprise ledger, crafts, skills. v3 has routed memory and the fix store; none of this is the ambient side's job. |
| Charter = the person's verbatim words + watch (cron / file / poll) + sentinel + action + rails. Rails mandatory by construction. | `graph:` watches. There is no graph. |
| One cheap yes/no sentinel call carrying its own previous judgments, so a declined firing is not re-proposed forever. | The 1.7k LOC regex cadence recogniser. In v3 the model is the compiler. |
| Ratify once on a card; offer the timer once, ever; "yes" journaled before the host is touched; installed status derived from file bytes. | Probation → tenure counters. See "rules are tenure". |
| Ambient law: routine "checked, nothing" never enters the conversation. Arrival brief only after ≥4 h and only if something happened. | Services with health probes and restart policy. A background job with a keep flag covers it later if wanted. |

## Part 3 — The mechanism in v3 terms

1. **Recognition, not declaration.** The model reads standing words and calls one belt
   tool (working name `stand`; OPEN) with the person's words, a watch spec, an action,
   and rails. The surface draws the card. Nothing stands until yes.
2. **Storage is files.** `~/.aforge/v3/standing/<id>.json` — one document per charter,
   written temp + rename, flocked on mutate. `standing/ledger-YYYY-MM-DD.jsonl` —
   append-only, for the daily rail and max-per-day. `standing/<id>/runs/<n>/` — each
   firing is an ordinary session folder, so `/cost`, `/export`, the task index and the
   room all work unchanged. Runs live **outside** `projects/` so home never scans them,
   and "no news" runs are reaped after 7 days by the existing sweep law.
3. **Who ticks.** Any open window takes `v3/locks/standing.lock` and runs the pass every
   5 minutes. The OS timer is the backup for "no terminal open". Same binary, same pass,
   same lock.
4. **A firing is a session.** Sentinel says yes → a child session runs in the charter's
   project under the person's banked rules. Anything that would ask stops the run as
   `needs your look`; its presence file says so; home sorts it to the top.
   **Rules are tenure:** no counters. What you banked is what runs unattended.
5. **Where news lands.** In the planting conversation's journal — as a steering note if
   the window is live, as a queued note replayed under one `while you were away` fold
   if not — and as a glyph on home. A "no" leaves one line in the charter's own log.
6. **Presence, felt not seen.** One dim status-line segment only when charters exist,
   breathing only while a firing runs. The card is the editor: pause, retire, change the
   cadence in words, in the conversation or by clicking.
7. **Money.** Per-firing cap + daily rail, both quoted on the card, both read from the
   spend journal that already exists. The sentinel call is billed as an auxiliary line.
8. **Honesty.** `/status` prints keeping watch: installed / window-only / off, last
   wake, next check — all derived.

## Part 4 — Storage: the numbers, and the trajectory

Measured 2026-08-20 on the author's machine: 48 sessions, 39 projects, 21 MB, largest
transcript 1.4 MB, a full home read of every session in 50 ms.

Assume a heavy user — 30 charters, one poll every 2 minutes, 50 firings a day, a year:

| Load | Files + JSONL | Breaks at |
|---|---|---|
| Tick: read 30 docs of ~1 KB | <1 ms | never |
| "Checked, nothing" | rewrite `lastChecked` in the doc; never a line per tick (that would be 365 MB/yr) | — |
| Daily rail across firings | append-only daily ledger; `O_APPEND` lines <4 KB are atomic on a local fs; sum ≤ hundreds of lines | never |
| 18k runs a year | session folders outside `projects/`, TTL-reaped | only if put under `projects/` (home's refresh would reach ~1 s cold) |
| "Every firing of the PR watch in May, with cost" | scan an 18k-line JSONL ≈ 10 ms | ~1M rows, or several hosts on one store |

Writers to one charter doc: the elected ticker, the person's window, a finishing firing.
Three, rarely at once, one document each — temp + rename and a per-doc flock, the
pattern `presence.json` and `meta.json` already use. The one cross-document invariant
(the rail) rides the append-only ledger; the worst race is two firings passing the cap
in the same second, an overspend bounded by one firing's cap, which v1 accepted too.

Where files and SQLite fail alike: NFS and synced home directories. flock is unreliable
there and SQLite says the same of itself. `AFORGE_HOME` is the answer, and the page says
so.

**Decision: files.** The dependency cost of SQLite is zero (v1 links it in the same
binary), but it would be a second truth beside the folders that home, world, presence,
the sweep and `cat` already read. v1's 26k LOC of store was the event-sourced graph, not
SQLite — and that is the part v3 is not bringing.

**Trajectory.** One package, `internal/standing`, owns every path and every read. If a
real query need or a multi-host need appears, a SQLite **index derived from the files**
is added behind that package — rebuildable from the folders at any time, never the
truth, and no caller changes. That is the whole plan, and it is deliberately not built
now.

## Part 5 — Where you say it: home, errands, and the record (OPEN)

The problem in the person's words: "remind me at 6" or "tell me when CI goes red" is
something you say from anywhere, most naturally from home. It is not a project
conversation, and it is not a single question either — getting to the card can take a
tool call or two and a sentence back and forth. But every such exchange today becomes a
session row, and home's list would fill with one-off errands that are done.

The tempting answer — a chat that is not stored — is wrong twice: the name is scary, and
the record is the value. "Why did I get this reminder?" must open the conversation that
made it. Nothing that fires may lack provenance.

**The proposed rule, one sentence: home lists a conversation by what it came to.**

- Still talking → a session row, as today.
- Came to something that stands → shown as that thing, under its project, in a quiet
  band: `◦ every Monday 9am · weekly update · fired Mon`. Opening it opens the
  conversation behind it, with the card at the top and the firings under it. Saying
  "make it 8" or "stop this" there edits it. The session is not hidden; it is filed
  under the thing it became, the way a task's record row is a door to its transcript.
- Came to one answer and went quiet → it folds under `…3 more, quiet since Tue`, as
  every quiet session already does. A retired reminder is this case.

No new primitive: a charter carries the id of the session that made it, and home's
grouping reads that field. Search sees through it (a charter's words are matched the
way task outcomes are).

What is still open:

1. **The word.** "standing watch" is resident vocabulary and banned from the chat
   corpus. Candidates for the band and the tool: *watches*, *keeping an eye on*, *on its
   own*, *routines*. Pick one; use it everywhere.
2. **Which project owns an errand typed at home.** A reminder belongs to no project; a
   CI watch belongs to one. Proposal: the project under the cursor when you start
   typing, `~` when on none — and home's `elsewhere` limit means, today, only this
   window's project can take it. Lifting that limit is the same work home-design.md
   already names.
3. **Answering in place.** For a true one-liner, home's right pane could carry the card
   and a turn or two without opening a conversation on screen — still a session folder
   underneath, for the record. This is a phase-two nicety, not the mechanism.
4. **Outward lane.** Desktop notification for `needs your look` and reminders, off by
   default. v1 had none.
5. **Unattended scope.** Full task under banked rules (proposed), or read-and-report
   only until a person is back.
6. **Reuse of `internal/watchdog` and `internal/lease`** as-is (tested, ~1.4k LOC) versus
   a v3-styled rewrite.

## What this is not

- Not a server, a daemon, a port, or an account. The timer is the whole host footprint.
- Not a scheduler UI. Cadence is said in words and echoed in words.
- Not a rules engine. The sentinel is judgment: a broad watch with a smart sentinel beats
  a precise trigger with none.
- Not notification spam. Routine silence is the design; the rail bounds the worst day.
- Not a second memory system, a learning loop, or a self-improvement programme.
