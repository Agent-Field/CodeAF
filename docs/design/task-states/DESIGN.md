# Task states — three tiers, one question

Owner ruling 2026-09-09. This is the contract every lane in the task-states wave
builds to, and this file is the copy of record: the engine lane landed the words
and the Go names below, and every surface lane reads them from here.

## The question the surface answers

Every task row, card, rail line and roster entry answers ONE question before it
says anything else: **do I need to do anything?** There are exactly three
answers, and each has one glyph and one word.

| Tier | Glyph (existing constant) | Word on the row | What it means |
| --- | --- | --- | --- |
| moving | `◌` (`glyphQueued`) still, `▸` (`glyphRunning`) running | queued · working · waiting on … · auto-starts in … · finishing | nothing for you |
| over | `✓` (`glyphDone`) · `⊘` (`glyphStopped`) · `✗` (`glyphBad`) | done · stopped · incomplete | nothing for you; a rerun may be offered |
| your call | `?` (`glyphAsk`), accent colour, always | your call | the machine has done what it can; the card carries the reason and the answers |

The fuel gate keeps its `⏸` prefix rule exactly as today (taskstrip.go): a
gated node wears `⏸` in front of the tier glyph.

Rules that hold everywhere:

- **The glyph is the tier and nothing else.** A person who learns three glyphs
  has learned the whole system.
- **The reason is on the row, in a plain sentence, never a machine word.** A row
  never reads a bare `waiting` or a bare `your call`: it reads `waiting on task
  4`, `your call · conflicts with your branch`.
- **One word per state across every surface.** Card head, rail, roster, home,
  the landing note the model reads, the `tasks` tool's reply. These words are
  DELETED as person-facing text: `awaiting review`, `unverified`, `needs your
  look`, `failed`, `delivery needs attention`, `what it produced was not taken
  as done`, `stopped — branch kept` (the branch is a fact line, below).
- **The emptiness law holds.** Unknown renders as nothing.
- **Banned machinery vocabulary stays banned:** auditor, verdict, verified,
  refuted, unverified, reaudit.

## The states

### moving

| Row reads | Backend |
| --- | --- |
| `queued` | TaskQueued, no wait |
| `waiting on task 4` | TaskQueued with Waits, or running with Hold |
| `auto-starts in 9s` | consent clock running (today's `taskAutoWord`) |
| `working · <phase caption>` | TaskRunning |
| `finishing · <gap>` | TaskRunning in checking/repairing |

### over

| Row reads | Backend | Optional verb |
| --- | --- | --- |
| `done` | TaskDone | none |
| `stopped` | ending stopped by the person | `rerun` if the branch has work |
| `incomplete · <reason>` | TaskFailed, any ending | `rerun from its branch` where a rerun can help |

`failed` is gone. The engine's TaskFailed keeps its name internally; the person
reads `incomplete` plus one of these reasons, spelled once in
`internal/session` and read by every surface:

| Ending | Reason sentence |
| --- | --- |
| wire | lost the connection |
| upstream | the model provider refused it |
| circling | went in circles |
| blocked | was blocked by another task |
| steps | ran out of steps |
| notes | would not write its notes down |
| stale | its brief went stale |
| refused | would not take a step it was asked to |
| error, or no reason | a fault: <first line of the error> |
| check named gaps (incomplete lead) | the check found gaps: <gaps> |

`Fault` (task_status.go) stays true only for error and unknown endings; a
faulted row may be coloured bad, every other incomplete row is dim.

A `done` row carries the merge as a FACT LINE, not a state: `merged`,
`branch kept` (only when keeping was asked for), or nothing for in-place work.

### your call

One word, one glyph, and the reason plus the answers on the card. The reasons
are a closed set, each with its yes verb and its no verb. The card's chips are
ALWAYS the same three columns in the same order with the same keys:

```
[a] <yes verb> · [n] <no verb> · [s] tell it
```

| Reason (row sentence) | Backend | `a` yes verb | `n` no verb |
| --- | --- | --- | --- |
| starts on your word | consent card before the run (`taskWaitingWord` today) | start | don't |
| design ready to approve | harness asking | approve | decline |
| conflicts with your branch: <files> | merge conflicted after the merge round failed | resolve it (spends one more merge round) | drop it (refute; branch kept) |
| nobody could check it | TaskUnverified, checker gave no answer after failover | accept | not right |
| the check did not pass it: <gaps> | ResultHeld / held landing | accept anyway | not right |
| paused at the <cap> cap | fuel gate | raise the cap | stop it |

`[s] tell it` is the third column on every card. It puts the composer into the
existing steer mode (steer.go, `glyphSteer`) addressed to that task, the
composer shows the address as a chip, and what is typed is sent as a steer. **A
steer never resolves a task by itself.** The model or a worker reads it and
spends the verb. "Looks good" typed on a card must not silently become accept.

`check again` / reaudit is no longer offered to the person. The engine retries
the check itself (below). The engine verb stays for the model.

`decide these for me` leaves the answer row. It was a persistent preference
disguised as an answer and it is why the owner found a card with no choices and
no explanation. In its place:

- a one-time `[d] let aforge decide this one` drawn dimmer than the three
  columns, which hands THIS card to the model and changes no setting;
- the standing `task.settle` setting lives in `/settings` only.

### the auto-settle floor

`task.settle = auto` stays. When it is the reason a card has no chips, the card
must say so on the reason line: `nobody could check it · aforge is deciding ·
[t] take it back`. Pressing `t` draws the chips and resolves nothing.

**A task never stays unowned past the end of a turn.** If the model's turn ends
and a task it was handed is still unsettled, the engine publishes that the
decision is back with the person and the card draws its chips. If the model's
last message asked the person a question about that task, the chips are the
answer surface for that question: model text above, chips below, one ask.

Conflicts are never handed to the model. It cannot merge by decree.

## What the engine tries before anything is your call

Each of these is one automatic attempt, logged in the task's journal, then the
ordinary landing:

1. **Checker failover.** A check that never answers or answers with neither
   word is retried once through the provider failover ladder on another model
   before the node lands as `nobody could check it`.
2. **The merge round.** A branch that conflicts gets one resolver round: merge
   the person's branch into the task branch in the task's worktree, a worker
   resolves the markers with the brief and both sides in front of it, the check
   runs again on the result, and the landing is retried. Only a round that fails
   reaches the card, with the files named. The original task branch ref is kept
   under `task/<slug>-before-merge` so nothing is rewritten in place.
3. **One rerun** from the branch for wire, upstream and stale endings, before
   the node lands `incomplete`.

## How the end shows in the chat

The landing card is three rows at most and every row has one job.

```
✓ ◆ Port the parser · done · 6m40s · 2 files · merged · task/parser
  "what it said about the work" · started 14:02 · ctrl+o output
```

```
? ◆ Port the parser · your call · 6m40s · 2 files · branch kept · task/parser
  nobody could check it — the checker never answered
  [a] accept · [n] not right · [s] tell it · [d] let aforge decide this one
```

```
? ◆ Port the parser · your call · 6m40s · 2 files · branch kept · task/parser
  conflicts with your branch: parser.go, parser_test.go
  [a] resolve it · [n] drop it · [s] tell it
```

```
✗ ◆ Port the parser · incomplete · 4m02s · 1 file · branch kept · task/parser
  ran out of steps · [r] rerun from its branch
```

- Row 1 is the head: tier glyph, kind glyph, title, tier word, then facts in a
  fixed order: span, files, merge fact, branch. Nothing else.
- Row 2 is the reason (accent for your call, dim otherwise) or the quoted
  report for done. Never both on one row; the report is behind `ctrl+o` when a
  reason is showing.
- Row 3 is the chips, only on your call, and only while it is unanswered. An
  answered card replaces row 3 with one dim receipt in the person's voice
  (`you took this as done`, `you said it is not finished`, `sent to be
  resolved`), which already exists in tasksettle.go.
- The head is never rewritten after landing. A later resolution lands as its
  own card, as today.
- No sentence on the card ends in `…` hiding the one instruction the person
  needs. If the reason must be cut, the files list is what gets cut, never the
  verb.

## The rail and the roster

- The rail row is `<tier glyph> <title> · <word or reason>`, cut from the right.
  Your-call rows are ordered first, then moving, then over, which is
  `railGlyphRank`'s order today with the `awaiting review` exception removed.
- A folded family wears its loudest child, as today.
- The roster (`/tasks`, tasksplace.go) and home use the same word constants;
  none of them spells a state locally.

## The model's side

- The landing note (`taskNote`) uses the same tier words and reason sentences.
  It says `task 7 your call: <title>` / `task 7 incomplete: <title>` / `task 7
  done: <title>`. `failed` and `needs your look` leave the note.
- `beltfacts.go` and `prompts/system.md` describe the verbs the model has:
  accept, not right, check again (the engine verb `reaudit`), and steer. They
  say a conflict is never the model's to accept.
- The `tasks` tool's replies use the same words.

## Contract: Go names every lane uses

Owned by the engine lane (E1), read by every surface lane:

```go
// internal/session/task_status.go
type TaskTier string
const (
    TaskTierMoving   TaskTier = "moving"
    TaskTierOver     TaskTier = "over"
    TaskTierYourCall TaskTier = "your-call"
)

type TaskAskKind string
const (
    TaskAskStart    TaskAskKind = "start"     // starts on your word
    TaskAskApprove  TaskAskKind = "approve"   // design ready to approve
    TaskAskConflict TaskAskKind = "conflict"  // conflicts with your branch
    TaskAskCheck    TaskAskKind = "check"     // nobody could check it
    TaskAskHeld     TaskAskKind = "held"      // the check did not pass it
    TaskAskCap      TaskAskKind = "cap"       // paused at the cap
)

// TaskAsk is the question on a your-call row: the reason sentence and the two
// closed answers. Yes and No are the person-facing verbs, spelled once here.
type TaskAsk struct {
    Kind   TaskAskKind
    Reason string   // the row sentence, complete, e.g. "conflicts with your branch: a.go, b.go"
    Yes    string   // "accept", "resolve it", "start", "approve", "accept anyway", "raise the cap"
    No     string   // "not right", "drop it", "don't", "decline", "stop it"
    Owner  TaskAskOwner // who holds the decision right now
}

type TaskAskOwner string
const (
    TaskAskOwnerPerson TaskAskOwner = "person"
    TaskAskOwnerModel  TaskAskOwner = "model"   // task.settle=auto handed it over; the floor hands it back
)

// TaskStatus gains:
//   Tier   TaskTier
//   Ask    TaskAsk        // zero unless Tier == TaskTierYourCall
//   Word   string         // the row word: "queued", "working", "done", "incomplete", "your call" …
//   Reason string         // already exists; for incomplete it is the reason sentence above
func TaskReasonOf(ending TaskEnding, report string) string   // the incomplete reason table
func (s TaskStatus) RowWord() string                          // Word, plus " · " + Reason where one exists
```

`ProjectTask` fills Tier, Word and Ask from the facts it already has. Surfaces
read Tier for the glyph, Word/Reason for the row, Ask for the card. No surface
computes a tier from State on its own.

The three resolutions the engine accepts stay `accept`, `reaudit`, `refute`;
the surface maps `a`→accept (or the conflict's merge round), `n`→refute. A new
engine door `ResolveConflict(id)` spends the merge round on demand.

## Gates that will name you

- `internal/e2e/tuiwords_test.go` pins person-facing strings; every respelled
  string must be updated there in the same change.
- `internal/manual/chat/` pages must describe the new words; the probes in
  `internal/manual/chat_test.go` must still reach their pages.
- `internal/tui3/manual_test.go` and `internal/session/manual_test.go` for any
  new key or tool verb.
- Comments are full-sentence prose with ALL-CAPS for a stated law. Match the
  density around you.
