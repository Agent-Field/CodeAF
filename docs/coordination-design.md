# Coordination — a noticeboard, not a postal service

*Design doc, 2026-08-20. Status: agreed direction, first two lanes in flight.
Nothing here is built except where noted.*

## The problem, in the user's words

"I have three windows open on this repo and four tasks out. I cannot tell what
any of them have already touched. So when one of them lands I don't trust it,
and when I want to fan out I don't — because the last time two of them worked
the same files I paid for both and threw one away."

The costs are concrete and all three are already being paid:

- **Rework.** A task reads a file, thinks for twenty minutes, and writes over
  the top of a change that landed while it was thinking. The work was correct
  against a world that stopped being true.
- **Collision.** Two lanes take the same files and one of them loses. Nobody
  finds out until land time, when it is most expensive.
- **Wasted paid runs.** Both of the above end with a real invoice for work that
  gets discarded.

And underneath them, the one that matters most: **the fear caps the product.**
Calm parallelism is the whole thesis of v3 — work fans out, you stay a person
sitting in front of one conversation. A person who will not start a fourth task
because they cannot see what the other three are doing has been sold the thesis
and handed a tool that makes them pay for believing it.

## The evidence is in this repo's own history

None of this is speculative. This codebase already coordinates by hand, badly,
and left the scars in writing:

| The workaround | What it is really compensating for |
| --- | --- |
| a hand-written `merge-coordination.md` under `/tmp`, that several sessions were told to read before merging — **kept outside the repo, because there was nowhere inside it to put such a thing** | there is no artifact any window can read that says what another window is holding, so one gets invented in a scratch directory and lost on reboot |
| **"Never `git add -A` or `git add .`"** in `CLAUDE.md` | a lane cannot tell which untracked files are its own, so the rule is "stage explicit paths and hope" |
| "Re-run `go build ./...` after fetching: another lane's half-finished file can break the tree for everyone" | the tree is shared and the collision is discovered by the build, after the fact |
| worktree lanes off `chat-v3-task`, one branch per wave | manual, human-enforced isolation, remembered or forgotten |
| `git worktree list` as the first move of most sessions | reading the machine's state through a tool that was never meant to answer "who is working on what" |

Every one of these is a person doing by hand what an artifact could do. The
design below is that artifact, and nothing more.

## The mental model

- **Nothing ever addresses an agent.** There are no messages, no recipients, no
  inboxes. Work leaves marks; other work reads the marks. This is the whole
  design in one line: **build a noticeboard, not a postal service.**
- **Two tenses, two files.** The **present** is a claim a live process holds up.
  The **past** is a row in an append-only record. They are different files
  because they are different kinds of truth, and the codebase already separates
  them exactly this way.
- **Relatedness is artifact overlap.** You never pick who should hear something.
  A reader discovers that something is relevant by intersecting file sets — my
  brief touches these paths, that live task claims those paths, they overlap.
  No topics, no tags, no subscriptions, no routing.
- **Claims are facts, not intents.** A claim says *this task has touched this
  file*, which is observed. It never says *this task plans to touch this file*,
  which is a promise nothing can keep and nothing can verify.
- **Crash-safe by construction.** Files and stamps. No queues, no delivery
  guarantees, no liveness protocol, no acknowledgements, no agent addressing.
  A window that is killed mid-write costs one refresh; a window that is killed
  outright goes stale and stops being believed. There is no cleanup daemon
  because there is nothing to clean up.

## The central decision — coordination through artifacts, not messages

Every message-shaped design imports four problems the moment it is drawn:
addressing (who gets this), liveness (are they still there), delivery (did it
arrive), and trust (do they have to act on it). An artifact-shaped design has
none of them, because a file that nobody reads is simply a file nobody read.

So:

| Tense | Where it lives | What it says |
| --- | --- | --- |
| **present** | the per-session presence file, TTL'd | which files the work this window has out right now has touched |
| **past** | the project's task index | which files a landed task cited, beside the cost and the transcript it already carries |

A reader asking "is anything else near this work" opens a directory, reads
small JSON, intersects two sets of paths, and draws a line. That is the entire
protocol.

### Entries are facts, never instructions

**A session renders what it reads to its person. It does not obey it.**

This is a law, not a preference. The moment one session's file can change
another session's behaviour, the noticeboard becomes an instruction channel and
every window on the machine becomes a way to steer every other one — including
whatever a model wrote into a title. A claim is evidence a surface draws and a
model may cite. It is never a command, never a lock, never a veto, and reading
one never blocks anything.

The refusals follow from that: nothing waits on a claim, nothing retries against
one, and no claim can stop a person's edit. The worst a claim can do is put a
warning in front of somebody.

## Where the value concentrates

The record is cheap to write and mostly unread. That is fine — its value is not
spread evenly across the day, it concentrates at **two moments where money and
correctness are both on the line.**

**1. Before spending — task preflight.** When a brief is about to become a paid
run, its likely files are checked against every live task's claims. Overlap gets
a warning, in front of the person, before the run starts. This is the cheapest
possible place to catch a collision: nothing has been paid for yet.

**2. Before merging — land time.** When work comes back, the files it claimed
are checked against what those files look like now. If the ground moved under
the run while it ran, the work is flagged into the state that already exists for
"finished, but somebody should look" — `needs your look` — instead of merging
silently. No new vocabulary, no new state, no new surface: an existing outcome
gains one more reason to fire.

Two more pieces earn their place beside these, for different customers:

**The delta, for the model.** At session-open and at turn boundaries, a terse
line goes into the model's context: what changed in this project since this
session last looked. Its customer is **the agent, not the person** — it is what
stops a model editing against a stale read and what stops it re-discovering, at
full token price, something a sibling task established an hour ago. It is a
context feature that happens to be built out of coordination data.

**Ask-the-record, for retrieval.** "Why did that task do X" is answered by
reading the task's own transcript. This mostly exists already — every landed row
carries a `TranscriptURI` pointing at a real journal file the read tool can
open. What is missing is the question reaching the answer.

## What already exists (don't rebuild)

The single most important fact about this design is how little of it is new.
The present tense, its TTL, its crash-safety and its cross-window reader all
shipped already, for the home page and the roster.

| Need | Existing primitive |
| --- | --- |
| a live claim about right now | `presence.json` per session — `SessionPresence`, `PresenceTask` (`internal/session/taskpresence.go`) |
| freshness with no liveness protocol | `presenceHeartbeat` (5s) / `presenceWindow` (three heartbeats) and `SessionPresence.Fresh` — age is the only rule, and staleness is the cleanup |
| crash-safe writes | temp-and-rename in the session folder; a failed write is dropped in silence |
| version safety | `presenceSchema`, and a reader that refuses a number it does not know rather than guessing |
| the join between a live claim and a record row | `SessionPresence.Holds`, written once, on `(SessionID, ID)` |
| every other window on this project | `internal/session/taskelsewhere.go` — `Elsewhere.Runs`, `Elsewhere.Tasks` |
| the past tense, per project | `tasks.jsonl` + `Agent.TaskIndex()` / `ReadTaskIndex` (`internal/session/task_index.go`) |
| where the story of a task is | `TaskIndexEntry.TranscriptURI` — a real session file the `read` tool opens |
| where the work of a task is | `TaskIndexEntry.ArtifactURI` — worktree, else branch |
| how many files a task wrote | `TaskIndexEntry.FilesChanged` (a count, today) |
| **which files a task wrote** | already computed and already carried: `changedPath` over `savingTools` (`edit`, `write`, `generate_image`, …) accumulates `n.changed`, read back by `leavings()` (`internal/session/task_run.go`). It reaches the checkpoint, the settle question and the digests — and then collapses to `len(n.changed)` at the index. **The list exists; it is thrown away one line before it would be durable.** |
| which files a turn wrote in the main session | `fileLedger` / `changeLedger` over `mutatingTools` (`internal/session/recovery.go`) — in memory, dies with the turn, exists only for revert |
| "somebody should look at this" | engine state `session.TaskUnverified` (`internal/session/task_contract.go`); surface words `taskUnverifiedWord` (`internal/tui3/task.go`), `needsLookLead` (`internal/session/task_audit.go`); rail placement `railAttention` via `railGroupOf` |
| a "since you last looked" stamp | `internal/session/look.go` — `.last-look`, one RFC3339Nano instant, `LastLook` / `NoteLook`; home already draws `since you last looked` |
| everything on the machine, one read | `internal/session/world.go` |

## What is genuinely new

Less than it looks like, and that is the argument for the design rather than an
accident of it.

1. **File citations on index rows.** A bounded list of paths beside the count
   that is there now — and the list is *already in hand* at the moment the row
   is written. This is the one place the design pushes against a law the index
   states out loud; see the open questions.
2. **File claims on presence.** `PresenceTask` grows the paths this running node
   has touched. Same file, same heartbeat, same TTL, same schema gate, same
   temp-and-rename. Nothing about the present tense is invented.
3. **The preflight overlap check** — a brief's files against live claims, before
   the money.
4. **The land-time ground-shift check** — claimed files against what those files
   look like now, routed into `needs your look`.
5. **The delta line** injected for the model at session-open and turn
   boundaries. Note what this crosses: `internal/session/tools_tasks.go` today
   has no reference to `Elsewhere` at all. **Other windows are visible to the
   screen and invisible to the model.** The delta is the first thing that
   changes that, which is why its customer is the agent.
6. **Ask-the-record** as a question a person can actually ask, over transcripts
   that already exist and are already reachable — `TranscriptURI` is followed by
   `PeekReport` for the card's tail today, and handed to the model as
   `transcript <uri>` by the `tasks` tool for it to open with `read`.

## Decisions taken

- Coordination is **artifacts, not messages**. Nothing addresses an agent, ever.
- Claims record **what was touched**, never **what is intended**. A promise is
  not a fact.
- **Relatedness is computed, not declared.** Overlap of file sets, discovered by
  the reader.
- Entries are **facts a surface renders**, never instructions a session obeys.
- The two checks reuse **existing states and existing words** — `needs your
  look` gains a reason; it does not gain a sibling.
- The delta's customer is the **model**. If a person wants to know what changed,
  that is `/history` and git, and both already work.
- **No new substrate.** The task index, the presence file and a last-looked
  stamp are the log. Nothing is built underneath them.

## The cut list — rejected on purpose

Recorded so that a future wave does not spend a week re-proposing them.

| Rejected | Why |
| --- | --- |
| **`found` entries — a second institutional memory** ("things learned about this project") | it rots, it duplicates `CLAUDE.md`, and nothing curates it. An uncurated memory that the model reads is worse than no memory, because the wrong entry is indistinguishable from the right one. |
| **A file-story provenance view** ("show me everything that ever happened to this file") | `git log` answers it for content and ask-the-record answers it for reasoning. Keep only the invisible edge — file → last task that touched it — which is what the two checks actually need. A surface for it is a screen nobody opens. |
| **Handoff entries** ("task A hands its context to task B") | the user is one person handing off to themselves. The transcript *is* the handoff, and it is already written. |
| **Attention claims / watch-this-path** ("tell me when anything touches this") | a configuration surface plus a noise source. Home's marks already put the things that need a person in front of them, without anybody having to have predicted which path would matter. |
| **Live session-to-session notes** | re-imports every problem artifacts were chosen to avoid: addressing, liveness, delivery and trust, all four at once. |
| **The grand unified event log as a new substrate** | the task index plus presence plus a look stamp already *are* the log. Ship citations and two checks; do not ship a platform and then look for its first customer. |
| **Autonomous traffic control — queueing an overlapping task behind the one already running** | **deferred, not cut.** Ship the preflight warning first and let its firing rate say whether auto-queueing earns a scheduler. Building the scheduler before the warning has ever fired is guessing about a collision rate nobody has measured. |

### The boundary — the record warns, it never acts

Every cut above has the same shape underneath it, and it is worth stating
plainly: **the log records and it warns. It never acts.**

Autonomous reaction to what other work is doing — noticing, deciding, and moving
without a person in the loop — is the resident's territory. That is a different
product in the same binary, with a different contract about what happens while
the terminal is closed. v3 chat is a session you sit in front of; the most it
ever does with a collision is put it where you will see it before you spend.

## Open questions

- **Exact paths or directories?** Path-level overlap is precise and misses
  neighbours that fight anyway (two files in one package). Directory-level
  catches those and fires constantly on a flat repo. Probably path-level with a
  directory-level second tier, but this is untested.
- **How does preflight know a brief's files?** A brief says what to do, and only
  sometimes says where. Candidates: paths named literally in the brief, paths a
  cheap pre-read resolves, or the previous run's citations for a similar brief.
  Whatever is chosen must fail quiet — a preflight that cannot guess must say
  nothing rather than guess wrong.
- **Hot files.** `go.mod`, `CLAUDE.md`, the manual corpus — everything touches
  them, so every check fires and the warning becomes wallpaper within a day.
  Needs a cap, a suppression list, a frequency-based decay, or all three. This
  is the question most likely to decide whether the preflight is loved or
  turned off.
- **The index's own law.** `TaskIndexEntry.FilesChanged` is a count on purpose,
  and the comment beside it says why: "the list is in the transcript, and a row
  that carried forty paths would be the thing this index refuses to be."
  Citations have to arrive as a **bounded** list — capped, with the count still
  authoritative — or the law has to be amended deliberately, in writing, in the
  same change. It must not be quietly broken.
- **Does presence cover the session's own edits?** A claim today is about a
  *task*. But a person typing in one window edits files too, and that is
  invisible to every other window. The machinery for it half-exists —
  `recovery.go`'s `fileLedger` already collects the paths a turn wrote, for
  revert — but it dies with the turn and would have to outlive it. Extending
  claims to the session's own writes makes the picture complete and makes the
  writer considerably chattier.
- **Whose stamp does the delta read from?** `.last-look` exists, but it is one
  instant at the places root, for the whole machine, written on the way *out* of
  home. The delta needs "since **this session** last looked at **this
  project**", which is a different stamp with a different scope and a different
  write moment. Either `look.go` grows a scoped form or the delta keeps its own,
  and a second file that means almost the same thing is exactly the drift this
  codebase legislates against.
- **Claims are about writes, not reads.** Nothing accumulates the paths a task
  *read*, anywhere. That is the cheap and probably correct choice — reads are
  numerous and mostly uninteresting — but it means the preflight cannot warn
  about the case that started this doc, a task that read a file and thought for
  twenty minutes. Land time catches that one instead. Whether the earlier
  warning is worth a read ledger is unanswered.

## Sequencing

1. **Now (in flight):** citations on index rows + file claims on presence
   (`fix/coord-citations`). Everything else reads what this writes.
2. **Now (in flight):** ask-the-record (`fix/ask-the-record`) — the transcripts
   are already on disk; this is the question finding them.
3. **The preflight overlap warning.** First chokepoint, and the first thing that
   saves money.
4. **Land-time ground-shift → `needs your look`.** Second chokepoint, reusing
   the state that already exists.
5. **Delta injection for the model** at session-open and turn boundaries.
6. **(Deferred) queueing**, judged on measured collision frequency — not before.

Every lane from 3 onward introduces a person-facing warning, which means every
lane from 3 onward owes manual pages in its own change. There is good news
there: `what-i-can-do.md`'s "What aforge cannot do" carries **no** denial about
seeing other windows' work, so nothing has to be un-said. `tasks.md` already
documents rows marked `another window` and already tells a person to "go to that
window to act on it" — the page that grows is that one, and what it gains is the
warning, not the visibility.

### The two numbers that decide step 6

- **Preflight fire rate** — how often a brief's files overlap live claims. Low,
  and the whole class of problem was imagined. High, and queueing has a case.
- **Ground-shift flag rate** — how often work comes back onto files that moved
  under it. This one measures the collisions the preflight *missed*, which is
  the number that says whether warning early is enough.

Neither number exists yet, which is exactly why step 6 is deferred rather than
designed.
