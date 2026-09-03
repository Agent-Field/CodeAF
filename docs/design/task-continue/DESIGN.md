# Continue: a task is continued from any ending, and nothing spent is waste

*2026-09-03, written against `dev @ 7ea37cab2`. Status: design → build. Lives at
`docs/design/task-continue/DESIGN.md`. Every `file.go:line` was verified at that
commit. `internal/tui3/place_tasks.go:1064-1079` is the sentence this page
replaces.*

## The one sentence

A task that has ended — however it ended, including by finishing — is continued
by one verb, and the continuation is the original assignment plus one finding,
run in the tree the ending left, carrying the record the ending kept.

## The problem, in the owner's framing

Work stops for eight or nine different reasons and there is exactly one thing a
person can do about any of them: type the whole request again. The surface says
so in its own comment, and states it as a law:

> `run it again` has **NO SEAM**. Nothing on this machine re-runs a finished
> task: a record row is an account of work that happened, and starting the same
> brief again is `/task <brief>`, which is a new piece of work with a new id
> rather than a repeat of an old one.
> — `internal/tui3/place_tasks.go:1073-1079`

The engine says the same thing to the model, in the `tasks` tool's own refusal:

> `Task %s ran in an earlier conversation, so there is nobody left to say it to.
> Propose the work again if it needs doing differently.`
> — `internal/session/tools_tasks.go:466`

and in the schema for its steer verb: *"Brief and acceptance never change;
propose the work again if the objective was wrong"* (`tools_tasks.go:81`).

**And the manual already promises the verb.** `internal/manual/chat/how-tasks-run.md:2033`,
under `## The three ways a task can land`, tells a person what to do about a halted
task in these words:

> The work can go on from its branch: say `continue task 7` or start a task that
> builds on that branch.

`grep -rn 'continue task'` finds that line and nothing else in the repository. The
`tasks` tool takes `query, limit, id, lines, scope, say, resolve`
(`internal/session/tools_tasks.go:80-83`) and `resolve`'s enum is `accept | reaudit
| refute`. A person who says `continue task 7` today is answered by a model
improvising, because the page is the only place the verb exists. **A CAPABILITY
THAT CANNOT WORK IS ABSENT, NOT BROKEN** is being broken in the corpus right now,
and this design is the way to settle it in the direction the page already chose.

**So a person who wants twenty more minutes of work on a task that ran for forty
must pay for the forty again.** The branch is still there. The working copy is
still there. Forty turns of the worker's own record are still there. None of it
is reachable from the verb the person actually has.

The machine already knows how to do the thing. It does it **once**, for **one**
ending, **automatically**, and a person cannot ask for it:

- `internal/session/task_store.go:1251` — `interrupt` turns a node that was
  running when aforge closed back into `TaskQueued` and hands it its own working
  copy again (`resumeTree`, `task_run.go:4134`); the person reads *"paused — it
  resumes; branch `task/fix-it-9c1a2f` kept"*.
- `cmd/aforge/chat.go:334` — `ReleaseOrphans` does the same on the resident side,
  and says *"picked up %d piece(s) of work that were interrupted — each continues
  from what it had already reached, with the files it had already written still
  where it left them"* (`pickedUpMessage`, `chat.go:2246`).
- `internal/resident/overrun.go:174` — a leaf that ran out of room is handed the
  original assignment, its criterion, its partial, its files, its own turns and
  the gap a checker named, and carries on.

Three working continuations, none of which a person can ask for, on any of the
other eight endings.

---

## A. The endings a task can have, and what is durable at each

Read off the code, not off intent. `TaskState` is `task_contract.go:219`,
`TaskEnding` is `:175`, the merge marks are `task_run.go:204-207`, and the
per-ending durability is `task_run.go`'s five landing roads plus
`task_land_unsaved.go`.

| what the person reads | state · ending | what is durable the moment it ends | what is lost |
| --- | --- | --- | --- |
| `finished` | `done` · — | the commits, merged into the person's branch; the index row; the node journal; the family ledger | **the working copy and the branch** — `releaseLanded` removes the worktree and runs `git branch -d` (`groundladder.go:971-979`) |
| `needs your look` | `unverified` · — | branch committed and kept; **working copy kept** for a re-audit; the report; the journal | nothing |
| `stopped` | `failed` · `stopped` | branch committed and kept; working copy released but its files remembered (`leftBehind`, `task_run.go:4262`); journal | the turn in flight |
| `lost the connection` | `failed` · `wire` | same as `stopped`; the worker's own turns to the last flush | the model's last message (`treeStandsWords`, `chat.go:2874`) |
| `ran out of steps` | `failed` · `steps` | same, plus the bound that fired (`RanOutSubject`, `internal/exec/ranoutwords.go:16`) | nothing |
| `went in circles` / `was blocked by another task` | `failed` · `circling` / `blocked` | same | nothing |
| `would not write its notes down` | `failed` · `notes` | same; the work is on the branch like any other halted node's | what it worked out in silence |
| the check did not accept it | `failed` · `refused` | branch kept; **the gaps the checker named** | nothing |
| its world did not match | `failed` · `stale` | the brief and the failed assumptions; nothing ran and nothing was spent | — |
| could not be saved | `unverified` · merge `aborted` | **the working copy is the only copy there is** (`task_run.go:4237-4243`, #277) | the branch holds nothing |
| the process died | stays `running` in `tasks.json` | working copy with uncommitted work; the journal; transcript rows to the last batch of 64 (`internal/store/transcript.go:134`) | the node's own settle; **its landing time (#530)** |
| a headless run, cleanly | exit 0 | the files, edited in place in the working directory | **the whole record** — `keepPrivateStore` deletes the store on a clean run alone (`cmd/aforge/do.go:696-698`) |
| a headless run, any other way | exit 1 or 2 | `graph.db` with claims, transcript, usage and node rows, at the path the run printed as `record kept at %s` (`do.go:375`) | nothing |

Two readings fall straight out of this table, and they are the whole design:

**Almost everything survives almost every ending.** The two exceptions are
narrow, and both are about *success*: a finished chat task loses its tree because
its work came home, and a clean headless run loses its record because isolation
was the point of a one-shot. Everything else — nine endings out of eleven — ends
with the branch, the working copy, the journal and the findings all sitting on
disk with no verb pointed at them.

**Two stores, and they are not the same store.** The chat engine's durable record
is a JSON checkpoint per conversation (`~/.aforge/v3/projects/<project>/<session>/tasks.json`,
`task_store.go:111`), an append-only project index
(`~/.aforge/v3/projects/<project>/tasks.jsonl`, `task_index.go:359`) and one JSONL
journal per node (`task_run.go:5460`). The resident and the headless door use
`graph.db` (`internal/store`). A continuation must be composed from whichever one
the ending wrote, and the composer must not know which.

---

## B. What already exists that this stands on (verified)

| piece | where |
| --- | --- |
| the remainder wrapper, and its exact-match peel | `internal/resident/overrun.go:56-142` — `OverrunPreamble`, `OriginalAssignmentHeader`, `remainderSections`, `originalAssignment`, `assignmentEnd` |
| the twin wrapper for a job that divided itself | `internal/resident/cooperative.go:84-96` — `CooperativePreamble` |
| what an ending hands to the attempt after it | `internal/resident/bank.go:41-59` — `ContinuationPartialHeader`, `ContinuationFilesHeader`, `ContinuationStateHeader`, `ContinuationTranscriptHeader`; the `Bank` struct at `:81` |
| a whole job's record read back, newest first, bounded once | `internal/resident/bank.go:701` — `LineageBank` |
| one attempt's own turns read back from the store | `internal/resident/bank.go:389` — `BankedRun` |
| findings a checker already wrote down | `internal/resident/findings.go:98` — `OpenFindings`, `OpenFindingsHeader` |
| an ending turned back into queued work, with its own tree | `internal/session/task_store.go:1251` — `interrupt`; `task_run.go:4134` — `resumeTree` |
| the same, on the resident side | `internal/store/lifecycle.go:554` — `ReleaseOrphans`; `cmd/aforge/chat.go:807-820` — the `node.Attempt > 0` bank read |
| a person settling a node the machine could not | `internal/session/task_audit.go:1932` — `ResolveUnverified`, and `TaskResolution` at `task_contract.go:256` |
| a person moving one node's model without moving the conversation's | `internal/session/task_room.go` — `RetargetTask` |
| the verb strip on a task row, and its build guard | `internal/tui3/place_tasks.go:1081-1102` — `s stop it` |
| the settle keychip grammar | `internal/tui3/tasksettle.go:447` — `a accept · l look again · n not right` |
| the words for every ending | `internal/session/task_run.go:2998` — `taskNote`; `haltedVerb` above it |
| the words for the bound that fired | `internal/exec/ranoutwords.go:16` — `RanOutSubject` |
| the grammar for a forecast price | `cmd/aforge/do.go:919` — *"about $%.2f at what work like this has cost here"* |
| a landing that is idempotent, so a second one lays the whole family | `internal/session/task_ledger.go:54` — `absorbedLedger` |
| a node journal that already tolerates several runs under one id | `internal/session/task_run.go:5528` — `findTaskJournal` |

---

## C. Rulings already made that this design treats as law

1. **A REMAINDER IS THE SAME PIECE WITH A FINDING, AND ITS SIZE IS THE SIZE OF
   THE FINDING RATHER THAN OF THE GOAL.** (#544,
   `docs/changes/unreleased/544-remainder-divides-on-size.md`; the peel is
   `overrun.go:108`.) A continuation is a remainder with a person behind it, so
   the law is inherited whole, nesting included.
2. **A LANDING THAT CANNOT SAVE THE WORK KEEPS IT AND ASKS FOR YOUR LOOK, RATHER
   THAN DELETING IT.** (#277.) The working copy is then the only copy, which is
   what makes it a continuation's tree.
3. **A NODE THAT RAN OUT WITH WORK RECORDED IS OFFERED AGAIN; ONE THAT RECORDED
   NOTHING IS NOT RESUMABLE.** (#327.) A continuation with an empty bank is still
   a continuation — it has the assignment and the tree — but it must not claim to
   carry anything it does not have.
4. **A LIVE PATH AND A RESTORED PATH ARE TWO SEPARATE SWITCHES AND BOTH MUST
   CARRY THE KIND.** (#529.) Anything drawn for a continuation is drawn twice, or
   it is only ever right inside one window.
5. **A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN.** (`CLAUDE.md`.) `c
   continue` is not drawn on a task there is nothing to continue from, and the
   manual page that says a task cannot be re-run is deleted in the same change
   that makes it possible.
6. **UNKNOWN OR ZERO RENDERS AS NOTHING.** (The emptiness law.) A continuation
   with no prior spend to report says nothing about money.
7. **THE SETTLED VERDICT IS ONE FIELD-READING, AND EVERY READER USES IT.**
   (`docs/design/gate/SETTLEMENT.md:387`.) A reading the continuation inherits is
   the same reading, not a copy of one.

---

## D. The mechanism

**Continue is one verb, and there is only one of it.**

A person standing at a task that has ended does one of three things, and all
three enter the same door:

- says **continue** and nothing else;
- says **continue** and adds words — a correction, a fact, *"also do X"*;
- picks one of the lines under **still open**, which are findings a checker already
  wrote down and never guesses.

The engine then composes exactly one thing:

```
ContinuePreamble                     — a constant, exactly matchable
  originalAssignment(node.brief)     — the innermost assignment, peeled (#544)
  SpecUnchangedNotice + criterion    — the acceptance, unchanged
  ContinueEndingHeader + ending      — how it ended, in the product's own words
  ContinueAskedHeader + words        — the person's words, verbatim, if any
  ContinuationPartialHeader  + …     — what it had produced
  ContinuationStateHeader    + …     — what it actually did
  ContinuationFilesHeader    + …     — files already on disk, to reuse
  OpenFindingsHeader         + …     — what a checker named as missing
  ContinuationTranscriptHeader + …   — its own turns, oldest first, last and longest
```

and admits it as **the next attempt of the same node**, in the tree the ending
left.

Three things about that composition are load-bearing.

**It is the shape that already exists.** Eight of the ten blocks are constants
already written, already exported, already pinned by
`TestEverySectionOfARemainderGoalEndsTheAssignment`. Two are new, and both are
new **blocks after the assignment** rather than new preambles, so the peel stays
one exact-prefix match against a small set of constants. A continuation of a
continuation therefore cannot nest, for the same reason and by the same code
that a remainder of a remainder cannot.

**The assignment never changes.** The person's words arrive as *this round's
finding*, under `ContinueAskedHeader`, and the peel gives the next round back the
assignment they first wrote. This is what makes *"also do X"* safe: it does not
edit the objective, so a fifth continuation is still aimed at the first request.

**The criterion travels; it is not re-derived.** `overrun.go:194-202` already
says why, in a sentence this design does not improve on: *"a new one written from
a partial result is a criterion aimed at the work that happened rather than at
the work that was asked for."*

### Decision 1 — a continuation is an attempt on the same task, never a new one

**Decision.** The continuation reuses the node's id. It is attempt *n+1* of node
`id` in session `s`, and every durable thing that already keys off `(SessionID,
ID)` — the index row, the branch, the node journal, the family ledger, the spend
— keeps working with nothing added. `findTaskJournal` (`task_run.go:5528`) already
documents the multi-run case: *"several mains under one id are a node that ran
more than once — an interrupt resumed."*

**Rejected: a new task that quotes the old one.** That is today's answer, and it
is the defect. A new id orphans the branch, re-plans the issue from the whole
brief, re-runs every check, files a second index row for one piece of work, and
prices the second attempt as though the first had not happened.

### Decision 2 — the engine seam is `interrupt`, generalised

**Decision.** `taskStore.interrupt` (`task_store.go:1251`) already performs the
whole transformation: it takes a settled-or-running record, resettles it to
`TaskQueued`, keeps the brief, the ground quartet, the branch, the worktree, the
`wrote` list and the cost, and lets `runFrontier` pick it up into its own
directory. It applies to exactly one ending because it is called from exactly one
place. Continue is **the same function reached by a person, for any ending**, with
the composed brief written in. One new exported door,
`(*Agent).ContinueTask(id, words)`, and one new caller of a function that is
already the most-tested path in the file.

**Rejected: a second re-entry path beside it.** Two paths that both mean "run
this node again" is how a node came to be judged done on one side of a seam and
cut off on the other (`executor.go:605-607`). There is one.

### Decision 3 — the continuation runs in the tree the ending left

**Decision.** Where a working copy still stands — nine endings out of eleven — the
continuation takes it, exactly as `resumeTree` already does. Where the work
already came home and `releaseLanded` removed the worktree and deleted the branch
(`groundladder.go:971-979`), the continuation cuts a fresh working copy **from the
ground's current tip**, which is the commit the finished work landed. Where the
work could not be saved and the directory is the only copy (#277), the
continuation takes that directory and the first thing it is told is that its
predecessor's work could not be committed.

**Rejected: composing a tree from the record.** A tree built from a summary is a
tree that has lost every file the summary did not mention. THE TREE IS THE
RECORD.

### Decision 4 — a continuation is undivided until its own size says otherwise

**Decision.** #544's ruling, unchanged and inherited: the continuation is planned
as one node unless the ruler puts it past one worker, and never because its own
words happen to list several things. `admitsEnumeratedPieces`
(`internal/plan/enumerated.go`) refuses `Undivided`, and a person typing *"also
fix the tests and the docs and the changelog"* is naming a finding, not a plan.

**Rejected: re-planning the continuation as a fresh issue.** Measured: it doubled
the do-door's spend across nine canary cells with quality flat (#544).

### Decision 5 — a reading that still holds is not taken again

**Decision.** The continuation inherits the previous attempt's settled acceptance
readings as its baseline. The gate re-reads a check when the continuation's own
changes could have moved its subject, and carries it forward settled otherwise.
This is `docs/design/gate/SETTLEMENT.md`'s field-reading law applied across an
attempt boundary rather than within one, and it is what stops a twenty-minute
follow-up paying for a forty-minute check suite.

**Rejected: re-running everything, on the grounds that it is safer.** It is the
same reasoning that produced invented verification rounds
(`overrun.go:160-167`), and it is what "nothing spent is waste" means in the one
place where the spending is largest.

### Decision 6 — what is still open is a finding somebody already wrote down

**Decision.** A finished task's page draws a `still open` block of at most three
lines, and every one of them is an `OpenFindings` entry
(`internal/resident/findings.go`) or an acceptance item the checker recorded as
unexercised. Pressing one continues the task with that line as the finding. No
model call is made to invent them, and a task with no such findings shows
**nothing** — the emptiness law, and the honest answer.

The word is `still open` and not `follow-up` because **`follow-up` is already
taken twice**: `internal/tui3/followup.go`, and the home exchange pane's own
`enter sends a follow-up · tab or esc back to`
(`internal/manual/chat/asking-from-home.md:57`). A third meaning would make all
three harder to search for.

**Rejected: asking a model what to do next.** It costs money on a page nobody
asked a question on, and it produces work the person did not want with a
confidence the record does not support.

---

## E. The same door, headless

The vocabulary is settled by what the two words already mean in this product:
`/resume` and `aforge resume` open an earlier **conversation**
(`internal/manual/chat/hints-and-tips.md:49`, `staying-on-that-machine.md:97`).
So:

> **CONTINUE IS FOR WORK; RESUME IS FOR CONVERSATIONS.** No door spells one of
> them with the other's word.

```sh
aforge do --continue <record>              # carry on, with nothing added
aforge do --continue <record> "also do X"  # carry on, with a finding
```

`<record>` is the path the run already printed as `record kept at %s`
(`do.go:375`), or a `--db` store. The flag implies `--db <record>/graph.db`, so the
continuation opens the store the first run wrote, `ReleaseOrphans` puts its
unfinished claims back, and the composed continuation enters as one
`CommandSplice` against the same root — the existing chain at
`cmd/aforge/chat.go:334` and `:807-820`, reached deliberately instead of by
accident.

Two honest limits, stated rather than engineered around:

- **A clean run deletes its record**, and `--continue` on a path that is gone
  says so and continues from what is left — the assignment and the working
  directory as it stands — rather than pretending to carry a bank it has not got.
  The closing line of every `do` run therefore names the record and the command:
  `record kept at <path> · continue it with: aforge do --continue <path>`, and on
  a clean run, `record deleted · pass --keep to be able to continue this run`.
- **A headless run edits the working directory in place** (`do.go:659-685`) and
  never made a worktree, so its tree is the person's own directory and is always
  still there. THE TREE IS THE RECORD holds most simply here.

---

## F. What the person sees

The words are the product's own. Nothing below is a new coinage where an existing
sentence fits.

**On a task that has ended**, wherever the strip is drawn, one more verb beside
the one that is there:

```
→  c continue
```

drawn only where there is something to continue — the build guard the strip
already asks before naming `s stop it` (`place_tasks.go:1095-1101`).

**On the task's page**, above the verb, one line saying what carrying on keeps.
It is `pickedUpMessage`'s clause, singular:

```
it carries on from what it had already reached, with the 6 files it had
already written still where it left them
```

and where the previous attempt left no record at all, the honest half instead:
`it starts again from the assignment; nothing of the last attempt was recorded`
(#327's second sentence, in the person's words).

**What is left** is the ending, in the words the landing note already uses
(`how-tasks-run.md:1452-1470`, and `taskNote` at `task_run.go:2998`): `lost the
connection`, `went in circles`, `was blocked by another task`, `ran out of steps`,
`would not write its notes down`, `stopped — branch kept`, `needs your look`. On a
refused task it is the gap the checker named. On a finished one it is the line the
person picked or typed.

**On a finished task**, and only where the record holds any, a block of at most
three:

```
still open
  the --verbose flag is not exercised by any check
  the sample tool's README still documents the old flag
```

Pressing a line continues the task with that line as the finding. With none
recorded, the block is absent entirely.

**What it will cost** uses the door's existing grammar and the emptiness law:

```
$0.41 so far · about $0.20 more, at what work like this has cost here
```

With no prior reading of work like this, the second clause is absent. With no
prior spend, the whole line is absent.

**The composer's chat command**, for a task not under the cursor:

```
/continue <task> [words]
```

and the `tasks` tool grows a fifth verb beside `search`, `read`, `say` and
`resolve`: `continue`, whose description says what `say` does not — that it works
on work that is over, that it never changes the assignment, and that the words
arrive as this round's finding.

---

## G. The laws, held by the build

> **NOTHING SPENT IS WASTE.** Every ending keeps its tree, its record and its
> findings, and a continuation is composed from what is actually there. A
> continuation never re-derives a fact the record already holds, and never claims
> to carry one it does not.

> **A CONTINUATION NEVER RE-PLANS THE ISSUE.** It is the original assignment plus
> one finding, admitted undivided, and it divides only when the ruler puts its
> own size past one worker — never because its words name several things.

> **THE TREE IS THE RECORD.** A continuation runs in the working copy the ending
> left, or in a fresh one cut from the tip the finished work landed at. A tree
> composed from a summary is not a continuation of anything.

> **THE ASSIGNMENT NEVER CHANGES; ONLY THE FINDING IS ADDED.** The person's own
> words arrive under their own header as this round's finding, and the peel
> returns the assignment they first wrote, however many rounds later.

> **A READING THAT STILL HOLDS IS NOT TAKEN AGAIN.** A check whose subject the
> continuation did not touch is carried forward settled, in one field-reading, and
> is not re-run.

> **CONTINUE IS ONE VERB FOR EVERY ENDING.** There is no retry, no restart, no
> rerun and no resume for work. What differs between a task that lost its
> connection and a task that finished is what the finding says, and nothing else.

> **WHAT IS STILL OPEN IS A FINDING SOMEBODY ALREADY WROTE DOWN, NEVER A GUESS.**
> No page spends a model call to invent work the person did not ask for.

> **CONTINUE IS FOR WORK; RESUME IS FOR CONVERSATIONS.**

### Where they are held

Two files, each law a named sentence failing with a file and a line — the
convention `internal/lane/structure_test.go` set.

| file | holds |
| --- | --- |
| `internal/carry/law_test.go` | the peel is exact-prefix and never substring; every block a composer writes has a header in the one section table; a continuation of a continuation does not nest; a wrapper around nothing unwraps to nothing; a person's words never enter the assignment; the criterion travels unchanged |
| `internal/session/continue_law_test.go` | one re-entry path (a structural test over `go/ast`: every assignment of `TaskQueued` on a settled node goes through `interrupt`); a continuation reuses the node id; `c continue` is not offered where there is no tree and no record; `taskFileVersion` does not move; every ending in `TaskEnding` has a continuation word |

`internal/e2e/tuiwords_test.go` gains the new person-facing strings, which puts
them on the untagged gate the day they land.

---

## H. The seams, file by file, smallest general change at each

**New: `internal/carry`** — the one place the continuation grammar is spelled.
It holds the preambles, `OriginalAssignmentHeader`, `remainderSections`,
`originalAssignment`, `assignmentEnd` and the four `Continuation*Header`
constants, moved verbatim out of `internal/resident/{overrun,bank}.go`, which
keep their exported names as aliases so no caller changes. It exists because
`internal/session` does not import `internal/resident` and must not start to:
both doors need one grammar, and ONE SOURCE OF TRUTH is the reason the peel works
at all.

| file | change |
| --- | --- |
| `internal/carry/carry.go` | the moved constants and the peel; two new constants, `ContinuePreamble` and the section headers `ContinueEndingHeader` / `ContinueAskedHeader`, both added to `remainderSections` in the same commit |
| `internal/carry/goal.go` | `Goal(assignment, criterion, ending, asked string, bank Bank, findings OpenFindings) string` — the one composer both doors call; `resident.overrunGoal` becomes a caller of it |
| `internal/resident/{overrun,bank,cooperative}.go` | aliases to `internal/carry`; no behaviour change; `TestEverySectionOfARemainderGoalEndsTheAssignment` moves with the table |
| `internal/session/task_contract.go` | `TaskEnding` gains no member; a `ContinuationWord(TaskEnding) string` beside it, so the finding's first line is one spelling |
| `internal/session/task_store.go` | `interrupt` splits into the reconciliation (unchanged) and `reopen(record, brief)` — the transformation — so a person can reach it; `taskRecord` gains `attempt` and `endedAt`, both `omitempty`, `taskFileVersion` unmoved (#530's discipline) |
| `internal/session/task_continue.go` *(new)* | `(*Agent).ContinueTask(id uint64, words string) error`: read the node, refuse a running one with `settledAlready`'s shape, build the bank, compose through `carry.Goal`, `reopen`, checkpoint, announce |
| `internal/session/task_bank.go` *(new)* | the chat engine's `Bank`, read from `tasks.json` + the node journal + `leftBehind`, answering the same struct `internal/resident` fills from `graph.db` |
| `internal/session/tools_tasks.go` | the fifth verb; the refusal at `:466` and the schema line at `:81` are rewritten, because both currently say the thing that stops being true |
| `internal/session/beltfacts.go:148` | *"offer a rerun"* becomes the real verb |
| `internal/tui3/place_tasks.go` | `c continue` in `verbs`; the comment at `:1064-1079` deleted, since it is a statement that this is impossible |
| `internal/tui3/place_tasks_test.go:130-153` | `TestTheTasksPlaceNeverNamesRunItAgain` retired in the same commit — it asserts the feature is absent |
| `internal/tui3/tasksettle.go` | `c continue` in the keychip grammar for an ended task |
| `internal/tui3/taskcommand.go`, `commands.go` | `/continue`, and its row in the command table |
| `cmd/aforge/do.go` | `--continue <record>`; the closing line names the record and the command; `keepPrivateStore` unchanged, and the clean-run line says what `--keep` buys |
| `internal/session/prompts/system.md` | the model is told the verb exists, since it will otherwise deny having it |
| `internal/manual/chat/` | §J |

---

## I. Milestones

Each ships alone, behind no flag, with its own change entry and its own manual
edit. The order is by value, and the first is the smallest.

| # | ships | why first / depends on |
| --- | --- | --- |
| **M1** | **continue after an ending nobody chose** — `wire`, `steps`, `circling`, `blocked`, `notes`, `error`, and a process that died. `internal/carry`, `ContinueTask`, `c continue` on the strip and the page, the kept/left line. | The tree, the branch and the record are all still there and the only door today is to pay for the whole task again. It touches no landing, no gate and no headless door. |
| **M2** | **continue with your own words** — `/continue <task> [words]`, `ContinueAskedHeader`, the `tasks` tool's fifth verb, the two rewritten refusals. | Turns *"also do X"* from a re-typed brief into a finding. Depends on M1's composer only. |
| **M3** | **continue a task that finished, and a task that was refused** — the fresh working copy from the ground's tip; the `still open` block from `OpenFindings`; the checker's gap as the finding. | The follow-up case the owner named. Depends on M1; wants #530's landing stamp for the readings, and says nothing about age until it lands. |
| **M4** | **the same door headless** — `aforge do --continue <record>`, and the closing line that names it. | Independent of M1-M3's surface; depends on M1's `internal/carry`. Wants #502 (a finished leaf's outcome is never journaled) so the continuation reads results rather than the plan as planned. |
| **M5** | **a reading that still holds is not taken again** — the inherited acceptance baseline at the gate. | The largest saving and the largest blast radius; last on purpose. Depends on `docs/design/gate/SETTLEMENT.md`'s field-reading being one value, which it is. |

---

## J. The manual learns it

The corpus is 918 probes over BM25, the assertion is *the page is in the top four*
(`DefaultResults = 4`), and **the corpus size is the section count** — so cutting
or minting a `## ` heading moves every term's IDF a little and puts one more
candidate in every race (`internal/manual/corpus_test.go:31`). The order below is
that constraint, not a preference: **every correction happens in a section that
already exists, and the one new heading is minted last.**

There is also a collision to respect. The adaptive-run family already owns this
vocabulary for the machine's own carry-on — `internal/manual/chat/adaptive-runs.md:762`,
`## When a worker runs out of its tokens mid-work — ↻, and the work is picked up
again`, with its live probe `why was my work picked up again`. Those pages are
about a node the runner reclaimed with nobody asking; these are about a person
asking. **The two must stay tellable apart**, and the way they are told apart is
that the person's verb is `continue` and the machine's is `picked up again`.

| # | page and section | change |
| --- | --- | --- |
| 1 | `how-tasks-run.md:2033` (`## The three ways a task can land`) | **rewrite in place.** It already says `continue task 7`; make it true, and say what the continuation carries. |
| 2 | `tasks.md:3609-3612` | **rewrite in place.** It currently reads *"There is no `run it again`, and no key is bound to one… the verb is named nowhere."* That is the denial `CLAUDE.md` says to hunt down. |
| 3 | `how-tasks-run.md:1626` (`## Nothing is thrown away`) | extend. It already enumerates every ending that keeps a branch; it gains the sentence that this is what a continuation runs in. |
| 4 | `task-rooms-after-restart.md` (`## See what a task did after restarting`) | extend: a task read back this way can be continued. |
| 5 | `when-the-connection-drops.md` (`## Did I lose my work`) | extend: name the verb. |
| 6 | `how-tasks-run.md` (`## My task could not save what it wrote`, `## My task's branch would not merge`) | extend: what continuing does with that directory. |
| 7 | `adaptive-runs.md:762` | one sentence distinguishing `picked up again` (the machine's) from `continue` (yours). |
| 8 | `tasks.md` | **the one new `## ` heading**, minted last, for the verb itself — and `go test ./internal/manual/` run whole afterwards to see which of the 918 probes moved. |

Retire in the same change, or the build says the feature does not exist:

- `internal/tui3/place_tasks.go:1064-1079` — the comment stating `run it again` has
  no seam.
- `internal/tui3/place_tasks_test.go:130-153` —
  `TestTheTasksPlaceNeverNamesRunItAgain`, which asserts the verb is drawn nowhere.
- `internal/session/beltfacts.go:148` — *"offer a rerun"*, which is what the model
  is told to do instead.
- `internal/session/tools_tasks.go:81` and `:466` — both sentences saying to propose
  the work again.

Probe sentences for `internal/manual/chat_test.go`, in the asker's own words:

- `my task ran out of steps, can I just carry on from where it got to`
- `it lost the connection halfway through — do I have to start over`
- `the task finished but I want one more small thing done to it`
- `how do I continue a task instead of running it again`
- `what happens to the files a stopped task already wrote`
- `does continuing a task cost me the whole thing again`
- `aforge do finished and I want to add one more thing`
- `is continue the same as resume`

---

## K. Replication, for the lost-connection case

**Deterministic (no model, no key that is ever used). Everything is in the
repository.** A stub provider that serves two turns and then answers every
request with a transport error; a task started through the real `Agent` door
against a temporary `AFORGE_HOME`; the node settles `failed` · `wire` with its
branch committed and its report saying `lost the connection`.

Expected today: `internal/session` has no exported door whose name contains
`continue`, `resume`, `restart`, `retry` or `rerun`
(`grep -rn "^func (a \*Agent) [A-Z]" internal/session/*.go | grep -iE
"restart|resume|requeue|retry|rerun|reopen|continue"` returns nothing), and the
only way to run the work again is `StartTask` with the brief typed out, which
mints a new id, a second `tasks.jsonl` row and a fresh working copy.

Expected after: `ContinueTask(id, "")` puts the same id back on the frontier in
the same working copy; `carry.OriginalAssignment(node.brief)` equals the brief the
person first typed; the composed goal contains the two files the first attempt
wrote and its own turns; a second continuation of the same node does not nest the
first one's wrapper.

**Field (real, needs `OPENROUTER_API_KEY`, a stranger can run it).**

```sh
make build
export AFORGE_HOME=$(mktemp -d)
# point the provider at a port nothing is listening on, mid-run
bin/aforge do "add a --verbose flag to the sample tool and a test for it" \
  --model deepseek/deepseek-v4-flash --timeout 10m -w "$(mktemp -d)" &
sleep 90 && OPENROUTER_BASE_URL=http://127.0.0.1:1 ...   # or drop the route
```

The run ends non-zero, keeps its store (`keepPrivateStore`, `do.go:696`) and
prints `record kept at <path>`. Then:

```sh
bin/aforge do --continue <path>
```

Expected after M4: the second run reports the files the first left, does not
re-plan the issue, and its own record shows it carried on rather than started.

*Owner's forensics, may be gone by the time you read this:* none. This
replication is deliberately self-contained, because #185's was not.

---

## L. Acceptance

- **e2e (tmux, real model):** through the real binary on
  `deepseek/deepseek-v4-flash`, a task is driven to a `lost the connection`
  ending, the person presses `c`, and the screen carries `it carries on from what
  it had already reached` and then the task's own second run. The task's id on
  the rail is unchanged, and `tasks.jsonl` holds one row for it, not two.

  ```sh
  go test -tags e2e -run 'TestTUIE2E/continue_after_a_lost_connection' \
    -count=1 -timeout 40m -v ./internal/e2e/ 2>&1 | tee /tmp/continue-e2e.log
  ```

- **e2e, the control:** the same run **without** pressing `c` settles exactly as
  it does today — `failed`, `lost the connection`, branch kept — and no
  continuation string appears anywhere on the screen.

- **e2e, headless:** `aforge do --continue <record>` against a kept record from a
  killed run exits 0, its `--json` object names the same root, and the deliverable
  does not restate the first run's work as new.

- **Unit:** the peel is exact-prefix (a preamble occurring inside a person's own
  assignment is not stripped); a third continuation carries one wrapper; the
  criterion is byte-identical across three rounds; a bank with nothing in it
  composes no continuation blocks at all.

- **Structural (`make test-laws`, `go/ast`):** every write of `TaskQueued` onto a
  settled node goes through the one reopen; every block written by any composer
  has a header in `remainderSections`.

- The manual pages of §J quote the new sentences, the denial at
  `place_tasks.go:1073-1079` is gone, and the change entry's `invalidates` names
  what people believed: that nothing on this machine re-runs a task.

---

## What this deliberately does not do

- **It does not restore a transcript.** The previous attempt arrives as an
  input, under `ContinuationTranscriptHeader`, exactly as it does for a requeued
  leaf today (`cmd/aforge/chat.go:326-334`). Rebuilding a message list across a
  model change, a compaction and a tool-belt change is a different problem, and
  the bank is the answer this codebase already measured.
- **It does not resume a harness design or a subharness run.** `spec.design` and
  `spec.run` are not on `taskRecord` at all (`task_store.go:142`), so those two
  kinds settle failed on a restart today and continue is honestly absent for them
  until the spec's variant payload is checkpointed the way `Offer` already is.
- **It does not give a task a second landing.** A continuation lands the way any
  attempt lands, through the same five roads; `absorbedLedger` is already
  idempotent, and nothing here adds a second way home.
- **It does not un-delete a clean headless run's record.** It says so, and it
  says what `--keep` buys.
- **It does not add a scheduler, a queue or a retry budget.** A continuation is a
  person's decision, taken one at a time. The machine's own automatic carry-on —
  `interrupt`, `ReleaseOrphans`, the overrun rounds — is unchanged and stays
  bounded by `MaxOverrunRounds`.
