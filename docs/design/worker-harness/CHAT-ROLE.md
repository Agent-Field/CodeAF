# The role of the main chat once tasks are the main road

*2026-09-18, cell c244, design only. The owner's question: once `/task` runs on
the run engine, is the main chat a manager of the tasks, or a conversation the
management stays out of? Three candidates, one recommendation. Reads against
`docs/design/worker-harness/SURFACE.md` and `PR-BODY.md` ("Words on the
screen").*

## What the chat is now, mechanically

A `/task` typed in a conversation becomes the run's root; a second `/task`
while one runs is added as a **child of the live root** — no second store
(task_run_belt.go:160–200, `startTaskRun`). The run is driven beside the
conversation in a goroutine (`driveBeltRun`, task_run_belt.go:~288), and when
it lands the conversation is woken with one note — the outcome word, the root's
result, where the work went (`beltRunOutcomeNote`, :365;
`deliverBeltRunLanding`, :314). That note rides the machinery a landed task's
note rides (`postTaskMessage`, task_run.go:4054): priced as a wake, durable to
the person's record, and it starts a turn nobody typed (`startTurnLocked(userMessage{wake: true})`).

The cautionary number is pull request 1183's: before the present bound, one
landing woke a 28-minute turn of 49 tool-call rounds on the high tier over a
tree that was already clean. What bounds that turn today: run workers are
capped at `taskMaxSteps` = 200 (task_run.go:161, `RunSpec.StepsPerTask`), a
woken turn is metered by the loop's ceilings (`openCallWindow`,
callwindow.go:59), and wakecause.go's rule — a turn owes what arrived *in it*,
not the newest thing typed (wakecause.go:10–46). The wake is the one cost here
that compounds per landing; every candidate says what it does to it.

The store holds per task: status, parent, dependencies, `Waiting`, `Chat`,
`Project` (internal/plandb/model.go:122–148), notes, spend, a trajectory
(plandb_tasks.go:35, :84). It holds **no conversation node** the chat can
address and **no question row**. A question reaches a person today only
through the conversation's ask lanes (`asklane.go`), which a run worker has no
door into — it parks with `plandb wait` or the run ends `needed an answer`
(PR-BODY.md). The surface: a conversation is a **tab** on the strip (tui3
chattabs.go `tabBar`; app.go:1729), the **rail** holds the rows, and the
**landing card** is the transcript's object for work that landed.

## The three candidates

### A · The chat itself manages — prompt rules for routing

The conversation's prompt pages gain routing rules: on each message decide
*hand off* (a new `/task`), *add to the running task* (an `Amend` write), or
*keep in chat*. Splitting into children with depends-on is the chat's own
reading, expressed as `/task` briefs naming each other. While four tasks run
and one has just landed, the main tab holds a woken turn narrating the landing
beside the rail:

```
main tab
  rail · 4 tasks
   ◐ Widen the import…   step 3 · $ go test ./internal…
   ◐ Add the cache…      step 11 · $ git grep -n Cache
   ○ Fix the flake       waits: Widen the import
   ● Add rate limiter    done · 14 steps · $0.11

  the limiter task landed — merged · 14 steps · $0.11
  Ask me about it, or steer the rest here.
› you:
```

### B · A plan seat owns decomposition; the chat only converses

A second model — the plan seat — owns the plan: it decomposes, launches, and
reads landings in its own context, in a **second tab** holding that seat's
thread and the tree. The main chat only converses and receives one-line
outcomes:

```
main tab
  task landed: Add rate limiter · merged · $0.11
› you:
```

```
work tab · 4 tasks
   ◐ Widen the import…   step 3 · $ go test ./internal…
   ◐ Add the cache…      step 11 · $ git grep -n Cache
   ○ Fix the flake       waits: Widen the import
   ● Add rate limiter    done · 14 steps · $0.11
  note · you: keep the middleware order
  › a note for this task
```

### C · No second model — three verbs, and landings go to the work tab

The run engine and the store stay the scheduler; the chat's prompt pages gain
**three verbs**: *hand off*, *add to*, *ask about*. A landing does **not** wake
the chat — it goes to the work tab (the same tab shape as B, drawn by the
surface, not spoken by a model), and the chat gets a **one-line digest** with
no turn of its own, the same dim telemetry the rail already spends:

```
main tab
  task landed: Add rate limiter · merged · $0.11
› you:
```

```
work tab · 4 tasks
   ◐ Widen the import…   step 3 · $ go test ./internal…
   ◐ Add the cache…      step 11 · $ git grep -n Cache
   ○ Fix the flake       waits: Widen the import
   ● Add rate limiter    done · 14 steps · $0.11
  › a note for this task
```

## The six answers, per candidate

**What wakes the main conversation, and what one wake costs.** A: every
landing wakes a full turn (task_run_belt.go:303); four landings are four turns,
each a model pass over the whole conversation context — the exact shape of the
1183 turn, bounded only by the loop ceilings and wakecause.go's owed-ask rule;
at the high tier, dollars per landing, not cents. B: the wake lands on the
seat, not the chat — a shorter context, but a second standing model turn per
landing plus its decomposition turns. C: **no wake** — the landing is a surface
draw (the tab and rail already repaint on `Changed(since)`, store.go:864); the
digest line is written, not spoken. One landing costs a store read and a
repaint: zero model calls.

**Who answers a task's question.** A: nobody, structurally — a run worker has
no ask lane (asklane.go serves the conversation), so it parks with `plandb
wait` or ends `needed an answer`, discovered at the landing. B: the seat could
poll, but the store has no question row to poll; the seat would invent one.
C: the question becomes a store row the work tab draws (`your call` is already
the surface's word for a held task), answered by a note — the steering door
the task page already owns (`PlanNote`, plandb_steer.go:62).

**What the person sees while four tasks run.** A: the rail, and a chat that
narrates each landing in a full turn. B: a quiet main tab and a rich work tab,
at the cost of a second thread to read. C: the same rail and work tab as B,
and a main chat that stays a conversation.

**What breaks on resume.** A: nothing new — the wake is durable
(`durableDelivery`, task_run.go:4004–4014) and the store survives the closed
terminal. B: **the seat's context does not** — `codeaf resume` reopens the
store's tree, but the seat's reasoning about why it split the plan as it did is
gone unless re-derived from notes. C: nothing — tab, rail and store all
rehydrate from disk; the digest line is a journal line.

**What it needs from the store that is not there today.** A: nothing for routing. B: a conversation
node for the seat and a question row — neither exists (model.go:122). C: a
question row, drawn as `your call`; the digest is nearly `beltRunOutcomeNote`
already (task_run_belt.go:365).

**What it needs from the prompt pages that is not there today.** A: the whole
routing rule set — hand off / add to / keep, plus depends-on syntax in briefs;
the most prompt law of the three. B: a second persona's pages, plus steering
pages for the seat. C: three verbs stated once — the smallest addition, and
each names a thing the session already does.

## Recommendation

**C.** (1) It is the only candidate that answers the 1183 number with a shape,
not a bound: landings stop waking turns entirely, so four running tasks cannot
spend the conversation's money at all. (2) It keeps one brain — the run engine
and the store are already the scheduler (task_run_belt.go drives, `plandb`
enforces pause/cancel/dependencies), and B's seat re-solves decomposition the
store already holds, at model prices, with a resume story that does not
survive a closed terminal. (3) It keeps the main chat what the owner
half-wants — a conversation place: the digest is the rail's dim telemetry, one
line, and the three verbs live in the prompt pages where routing was always
going to live; B needs new persona pages, A needs the most prompt law of all.

**The one thing it gives up:** the chat is no longer the place a landing is
*understood*. A person who wants "what did the run conclude, in your words"
must open the work tab or ask; C's digest states the fact, not the reading. A
mitigates this by waking a turn — exactly the cost A pays.

**The smallest first cell that proves it** — one M cell, four items:

1. **Failing test first**: `internal/session` — `driveBeltRun` on a completed
   run starts no turn; `Wakes()` gets nothing, and the digest line is
   journal-authored (red against today's `deliverBeltRunLanding` wake,
   task_run_belt.go:303).
2. The digest line: `beltRunOutcomeNote` (task_run_belt.go:365) written to the
   conversation's journal as a dim line — no `wakeNote`, no
   `startTurnLocked(wake: true)`.
3. The work tab: the tab strip (chattabs.go) gains one tab of the run's rows —
   the tasks place's own rows (taskplan.go `planItem`), the store's tree, and
   the note composer at the foot.
4. Manual: the landing's one line in the main chat, and the work tab, in
   `internal/manual/chat/tasks.md` — existing words only.

*Words on every mockup are the surface's own — task, step, note, steer,
rail, page, landing card, tab — plus the telemetry the rail already draws
(`waits:`, `done`, `N steps · $`). Design only; nothing under `internal/` or
`cmd/` changes in this cell.*

## A landing speaks only when an answer is owed (approved 2026-09-18)

The owner approved candidate C with one amendment of his own. A fully silent
landing is wrong in the commonest case: the chat launched the task in a turn
that was answering something the person asked, so the person is owed a reply.
The rule is one sentence: **a landing speaks only when an answer is owed.**

1. **At hand-off the debt is recorded.** When the chat launches a task in a
   turn that answers the person's ask, the task is marked *answer owed* and
   carries the person's question with it, in the store, on the task. A
   `/task` the person typed themselves is not marked: they asked for work,
   not for an answer.
2. **An owed landing wakes one short turn**, fed only the question and the
   task's result note. Its prompt says: answer from the result, never redo
   the work; if the result is thin, say so and offer a follow-up task.
3. **An unowed landing is the dim digest line**, zero model calls, exactly as
   candidate C draws it.
4. **A family replies once**, when the root lands, never per child. A
   check's landing wakes nobody (already the law in `internal/run`).
5. **The reply turn is bounded by shape, not by a ceiling**: the cheap tier,
   a few tool rounds at most, a context of two things. The 28-minute turn of
   pull request 1183 cannot recur because the turn has nothing to walk.

This extends wakecause.go's rule, "a turn owes what arrived in it", across a
hand-off: the ask arrived in the turn that launched the task, so the landing
that answers it is that turn's debt, paid once.

What it needs from the store: one field on the task, the owed question (empty
when nothing is owed), written at hand-off and read at the root's landing.
What it needs from the prompt pages: the reply turn's own short page, and the
three verbs. Words on the screen do not change: the digest line, the landing
card and the reply are the conversation's own sentences.

## The cells that build it

- **no wake, digest line**: `driveBeltRun` on a landed run starts no turn;
  the digest is a journal line (`beltRunOutcomeNote`). Failing test first.
- **work tab**: the tab strip gains the run's tab: the run's rows as the
  tasks place draws them, the live lines, the note composer at the foot.
- **three verbs**: hand off, add to, ask about, stated once in the chat's
  prompt pages and the manual.
- **answer owed** (after *no wake*): the store field, hand-off marks it, an
  owed root landing wakes the bounded reply turn, a family replies once.
