# The chat is the manager: coordination over the plan store

The conversation is where a person thinks, changes their mind, and splits work
off. The plan store is where that work lives once split. Today the two are
joined in one direction: the chat can put work in and is woken when work lands.
This design closes the loop in the other three directions, and it does so by
adding readers to channels that already exist rather than by adding a mind.

Evidence base: [BELT-DOE.md](BELT-DOE.md). Everything here sits on the bash
road, because the plan store exists only there.

## The problem, stated by what a person sees

A person says "do A, B and C" and the chat hands out three tasks. Then:

1. Task A finds that C's premise is wrong and writes a note saying so. **Nobody
   reads it.** The note is on the person's task sheet if they open it; the chat
   never sees it; C's worker sees it only if it happens to run `plandb task
   notes`, and it has no reason to.
2. The person says "actually, skip the migration". The chat hears this. **Task B
   is doing the migration right now and the chat does not know that**, because
   nothing tells it what is in flight when the person speaks. It answers the
   person and B carries on.
3. All three land. **Nothing checks that they fit together.** Each was checked
   alone by a check task seated after it. The person finds out when the branch
   does not build.
4. The chat wants to add a small follow-up. A card appears and waits for
   consent, though the person already approved this run and its budget ten
   minutes ago.

Each of these is a missing reader on a channel that is already written to.

## What exists, verified on `santos/dev2` at `8a83f4132`

| channel | writer | reader today | file |
| --- | --- | --- | --- |
| plan rows | `propose_task`, `/task`, worker `plandb add/split` | chat's `tasks` tool (row, status, first line of result) | `internal/session/tools_tasks.go:221`, `planTasksText` |
| one task's page | store | chat's `tasks #N` (brief, result, checks, last steps) | `planTaskText` |
| **notes** | worker `plandb task note`, engine (`AddNote`) | **screen only** (`internal/tui3` via `PlanTaskPage.Notes`); `planTasksText` does not print them | `internal/session/plandb_tasks.go:672` |
| steer into a running worker | engine only: the same-step sentence, carried by `noteOwed` so a race with the turn's end cannot drop it | the worker's next round | `internal/run/bashworker.go:140-160`, `sameStepNote` |
| revise a task's brief | `revise_assignment`, written through with a version so a stale direction is refused. **A worker's verb only** (`Config.mayRevise` is `InTask && tasker != nil && taskID != 0`): the chat never carries it, so the chat has no door that revises a brief | store; worker on its next read | `planReviseThrough`, `internal/session/plandb_plan.go:842` |
| dependencies | `propose_task depends_on` | supervisor; the dependent's brief is given its dependencies' reports | `internal/run` |
| the run's money | `CostUSD` on the run spec; a dollar limit holds while a worker is working (#1268) | supervisor | `internal/session/task_run_belt.go:81` |
| what a check reads | `"Acceptance: " + leaf.Description` plus the task's declared `Checks` | the check worker | `internal/run/run.go:799`, #1220 |
| wake on landing | engine | chat, one turn, with the task's report | existing |

Two things do not exist: a per-turn view of live tasks for the chat, and any
push of a plan note into a running worker.

## The four changes, in order

### 1. Notes are a channel, not a log

**Mechanism.** A note addressed to a task travels to that task's running
worker on the road the engine already uses for its own sentences: read the
task's unread notes between steps, in the same place `sameStepNote` is composed,
and open the next round on them the way `noteOwed` opens a round. A note that
arrives as the turn ends is carried, not lost, by the same carry. The worker
marks what it has read so a note is delivered once.

The chat's `tasks` tool prints each task's notes: the last one on the row, all
of them (bounded) on the task's page. The data is already in `PlanTaskPage`.

**Who may write.** The person, from the task sheet (exists). The chat, through
a plain note for information that is not a redirection (new: a `note` field on
the unified door in change 4, or a small tool until then). *Correction,
2026-09-23:* this line first said the chat redirects through
`revise_assignment` "(exists)". It does not exist for the chat — the verb is a
worker's — and the chat's only answer to a row whose work is wrong is `tasks`
with `stop` and a fresh hand-off. Workers, through `plandb task note`
(exists). Sibling workers thereby reach each other, which is the case in
problem 1.

**Must not.** A note must not be able to change a task's brief; that is
`revise_assignment`'s job and it carries a version for a reason. A note is
information the worker weighs, and the prompt says so.

**Prompt.** One paragraph in `bashworker.md`: notes addressed to you arrive
between your steps; read them as a colleague's word, not an order; a direction
comes as a revised assignment and looks different.

### 2. The chat sees the plan when the person speaks

**Mechanism.** While a run is live, each of the person's turns opens with a
compact digest of the plan: for every task, one line of id, title, state, and
its newest note if any; then the person's message. Off when nothing is live.
Bounded to a handful of lines; a plan wider than that says how many more and
the chat asks `tasks` for the rest. This is the same information the `tasks`
tool answers, pushed rather than pulled, and it is pushed because the chat
cannot know to ask on the turn where the person changed direction.

**Prompt.** One sentence beside the hand-off facts: if what the person just
said changes what a running task should do, steer or stop that task before you
answer. That is the whole of the manager's job, and the chat is the only mind
that can do it because it is the only one holding the conversation.

**Must not.** The digest must not carry results, steps or transcripts. Those are
what `tasks #N` is for. The chat's context is the thing being protected; a
digest that grows with the plan defeats the purpose.

### 3. Consent is the run's budget, not each action

**Today.** Every `propose_task` raises a card. The person approves it or a
countdown runs out.

**Change.** A run has a dollar cap (`CostUSD`, already enforced). The first
task of a run raises a card as now, and the card names the cap. Inside a live
run and under its cap, the chat may add a task, revise one, note one, or stop
one without a card; the row appears on the task sheet and the digest, which is
where the person watches. A card is raised again only when a change would
exceed the cap, would start a new run, or the person has turned this off.

**Why not "the model decides whether to ask".** Santosh proposed a per-task
consent parameter the model sets. If the model decides whether the person is
asked, the person is asked exactly when the model is unsure and not when it is
confident, which is the wrong way round: the confident wrong plan is the
expensive one. A budget the person set is a decision the person made; a
parameter the model sets is not. This keeps the freedom he wants and leaves the
gate in his hands.

**Must not.** Never a task without a row on the sheet. Never spend past the cap
without a card. The `needs you` count and the cards' wording are unchanged.

### 4. One door for the chat, light doors for workers

**Today.** Three chat verbs over one store: `propose_task`, `tasks` (which reads,
stops and, on the plan road, notes a row) and cancel. `revise_assignment` is a
worker's verb and is never on the chat's belt, so the chat cannot revise a
brief today. Workers use `plandb add/split/note/done` through their shell.

**Change.** A `plan` tool for the chat with actions `add`, `steer`, `note`,
`stop`, `show`. `add` keeps `propose_task`'s five required fields (title,
summary, brief, deliverable, acceptance) and its optional ones; that schema is
what forces a good brief and it stays at the chat's door. `steer` would be
the chat's first door onto `revise_assignment`'s road — a new capability, not
a rename, since the chat holds no such verb today. `note` is change 1's writer. `show` is `tasks`. The old
names remain as aliases for one release and then go, with the manual and
`system.md` updated in the same change.

**Where acceptance lands.** Each acceptance condition from the chat becomes a
`Checks` entry on the row, because the check worker reads `Description` and
`Checks` and nothing else (`run.go:799`). Deliverable and summary fold into the
description. A worker's own `plandb add` stays title-plus-description; its
subtasks are judged against their description as today and carry no check
contract unless the worker declares one, because checks are the largest cost in
the runs measured (69% of one run's spend) and a rule forcing them on every
subtask would multiply that.

**This is last** because it is a rename of things that work, and because
changes 1-3 are worth having whether or not it lands.

## What is not built, and why

- **A sentinel.** miniplan's was advisory-only (`task note`), event-driven,
  and its own four-run comparison showed no accuracy effect and contradictory
  speed. Its job here is done by change 1 (workers write the notes it wrote)
  and change 2 (the chat reads them). A sentinel that could tell a task had
  gone stale would have to read the conversation, which makes it a second chat.
- **A manager seat.** Same reason. The person talks to the chat; a second mind
  deciding to add a review is a mind the person is not steering. Task health
  is already mechanical: the no-progress stop (#1266), step caps, per-leaf
  checks, the dollar limit.
- **The chat offloading its planning.** The arm that planned hardest
  (`crewplan`) produced 25-29 tasks per run and one pass in four at double the
  cost. Plans came out one level deep in 15 of 16 cells regardless. There is no
  evidence more planning machinery helps at this scale.
- **A coordinator for a plan subsection.** Already exists by construction: a
  task that splits is seated as the planner of its subtree. To get one, the
  chat proposes one task whose brief is "plan and coordinate X".

## Hazards

- **Notes as orders.** A worker that treats a sibling's note as a direction
  changes its brief without the version check. The prompt draws the line and a
  test asserts a note does not alter `Description`.
- **Digest bloat.** A plan with forty rows makes forty lines a turn. Bound it;
  the bound is a constant with a test.
- **Budget consent misread as no consent.** The manual must say, in the
  person's words, when a card appears and when it does not, and the `tasks`
  page must show the cap and what is left of it. `internal/manual/chat_test.go`
  gets probes: "why didn't it ask me", "how much can it spend without asking".
- **The node road.** None of this exists there. `system.md` and the manual must
  not promise it when `CODEAF_TASK_BELT` names the node belt; the hand-off
  facts already branch on `oneTaskRoad()` and these facts branch the same way.
- **Two readers of unread notes.** The worker marks read; the screen must not,
  or a note the person opened is one the worker never sees. The mark is
  per-reader or it is the worker's alone.

## Acceptance: the real binary, a real model, the real screen, on Spark

Unit tests prove the seams. They do not prove the thing. Each change is
accepted only when a person-shaped scenario passes against `bin/codeaf` built
from the branch, `CODEAF_TASK_BELT=bash`, a real provider key resolved the way
the product resolves one, in tmux, on Spark, with the screen captured to a file
and the asserted words quoted from the capture. `--no-host` and fakes prove
nothing here (docs/design/task-states, the hosted-surface rule). A drive
starts with `unset CODEAF_PROFILE_DIR`, uses a throwaway `CODEAF_HOME`, and on
every exit removes the key copy, ends the engine by its recorded pid, and
removes the home.

Scenarios, each in one sitting:

1. **A note reaches a worker.** Type `/task` with a brief that names two parts
   that do not depend on each other. When both rows show running, open one on
   the task sheet and leave a note naming a fact the other needs. Assert: the
   other worker's next step (its `plandb show` or its transcript) reflects the
   note; the note appears on the `tasks` output the chat produces when asked.
2. **The chat steers on a change of mind.** Start a run with three parts. While
   they run, type a message that makes one part wrong ("skip the X"). Assert:
   the chat's reply mentions the affected task by its row word and that task's
   row moves to stopped or its brief is revised, before or in the same turn as
   the chat's answer. Assert the other two rows are untouched.
3. **A cross-task review.** Start a run with two parts whose results must
   agree. When both land, ask the chat whether they fit. Assert: it proposes or
   adds a task whose brief names both and whose `depends_on` names both rows,
   and that task runs and its result is shown.
4. **Consent is the budget.** Approve a run's card with a small cap. Ask the
   chat to add a small follow-up. Assert: no card; a new row appears. Ask for
   something that would exceed the cap. Assert: a card appears and names the
   cap. `/cost` shows what was spent against it.
5. **Nothing changes on the node road.** Repeat scenario 2 with
   `CODEAF_TASK_BELT=node`. Assert: the screen and the chat's words are what
   they were before this branch (compare a capture from `santos/dev2`).

Every scenario's capture is kept beside the change entry's evidence, and the
PR body quotes the asserted lines from it. A scenario that cannot be made to
pass is reported as such with the capture, not softened into a unit test.

## Order and size

1 then 2 then 3, each its own PR against `santos/dev2` with its own scenarios
passed; 4 only after 1-3 have been used for a day. Rough size: 1 and 2 a day
each including the drives, 3 a day, 4 two days. Every PR carries its change
entry and updates `internal/manual/chat/` and `system.md` in the same change,
per the manual law; every new tool name must appear in the corpus or
`internal/session/manual_test.go` fails the build.
