# Modules: where the responsibilities actually are, and where the seams should go

**Read as of `9daaa397a` plus this wave's status-projection change. It describes the code
that exists today; the PLAN/PRODUCT/COMMUNICATION documents describe intended behavior and
are not claims about what is shipped.**

The measurements below are line counts and call sites taken from the current tree. They are
here to locate responsibilities, not to argue that a long file is a bug: `task_run.go` is
7,249 lines and about half of that is prose explaining why a decision is the way it is,
which is the repository's own convention. **The argument in this document is never "this
file is long". It is "this file answers questions that belong to two different owners, and
the crossing is what makes a change in one break the other."**

## 1. What exists now

### 1.1 internal/session — one package, seven responsibilities

| Responsibility | Where it lives today | Who else reaches into it |
| --- | --- | --- |
| **Admission and context** — what work is allowed to start, where it will stand, what it is told | `task.go` (`proposeTask`, `ResolveTask`, `askTask`), `taskstands.go`, `groundladder.go`, `task_brief.go`, `task_divide*.go`, `taskmodel.go` | `task_run.go` calls admission's ladder at start; `task_contract.go` holds the frozen shape |
| **Lifecycle and scheduling** — the graph, the frontier, state transitions | `task_run.go` (`TaskGraph.admit/runFrontier/complete/resettle`, `TaskNode.end`) | everything; `orchestrate.go` and `harness_task.go` map their own runs onto the same states |
| **Steering and rooms** — the person's words reaching a running node | `task_room.go` (`SteerTask`, `taskRoom.steerIn`, `speaker`), `steer.go` (`Steer` for the main turn), `agent.go` (`enqueueNote`, `enqueueSteeredLine`, `steeringHeld`) | `task_run.go`'s `runTaskChild`, which must not close an agent with an unread line |
| **Worker** — one node's run inside its world | `task_child_run.go` (`childRun.open/drain/step/trip/park`), `loop.go` (the model loop both main and task threads use) | `task_run.go` owns the call and the landing around it |
| **Evidence and outcome** — did the work hold, and what is the record of it | `task_audit.go` (2,895 lines), `task_claims.go`, `taskgrade.go`, `task_checks.go` | `task_run.go` decides `TaskDone`/`TaskFailed`/`TaskUnverified` from the verdict |
| **Workspace and change disposition** — worktrees, branches, merges, holds | `taskgit.go`, `treehold.go`, `task_branch_protection.go`, `groundladder.go`, the `comeHome`/`abortedMerge` paths in `task_run.go` | `task_run.go`'s landings; the surfaces read `Merge`/`Branch` off the notice |
| **Storage** — what survives the process | `task_store.go` + `checkpoint.go` (the graph), `task_index.go` (the project's record), `sessionfile.go` (the transcript), `world.go` (`SessionRow.Runs`, the liveness ladder) | every surface; `task_run.go` calls `graph.checkpoint()` inline at ~20 sites |
| **Projection** — what a person is told | **new:** `task_status.go`; the surfaces in `internal/tui3` | `internal/tui3` only |

### 1.2 The crossings that actually hurt

These are ranked by how often a change to one side has broken the other, not by size.

1. **Lifecycle owns landing, delivery and evidence in one function body.**
   `task_run.go` holds `settleUnfinished`, `landUnchecked`, `landStopped`, `landShifted`,
   `landConflicted` and `landFinished` (in `task_ledger.go`). Each of them decides a state,
   asks git what happened to the branch, composes the note the model reads, and writes a
   checkpoint. A change to the *wording* of a landing is a change in the file that owns the
   scheduler. This is the crossing the status projection was written against: five
   different landings, five different tables of "what does this mean to a person".

2. **The surfaces re-derived meaning that the engine already knew.**
   Before this wave, `railGroupOf`, `railGlyphRank`, `taskStateMark`, `taskStateInk`,
   `roomMark`, `tasksGlyph`, `homeTaskGlyph`, `homeWorkGlyph`, `taskStatusGlyph` and
   `taskStateWord` each read `state`, `ending`, `stopped` and `merge` in their own order.
   They disagreed: the roster drew ⊘ for a person's stop and the record page drew ✗ for the
   same task, because the index carries no stop flag and each surface guessed differently.

3. **Liveness is one judgement with two readers and no shared vocabulary.**
   `SessionRow.Runs` is the ladder (a live conversation's own claim list, then the lock).
   Everything else treated "not in my map" as "dead", which is wrong the moment work
   outlives the terminal. The projection now takes liveness as a three-valued fact
   (`unknown` / `held` / `unclaimed`) so an absent answer stays absent.

4. **Delivery of *changes* was being read as delivery of *results*.**
   `taskUndelivered` (now `TaskStatus.ChangesUnlanded`) is a source-control fact: a branch
   nobody merged. A research or writing task can be completely answered with no branch at
   all. Any future "did the person get what they asked for" record must be its own field;
   the branch cannot stand in for it.

5. **Communication is delivery + wake + policy, fused per call site.**
   `enqueueNote` appends and then decides — from flags on `userMessage` (`wake`, `steered`,
   `ending`, `authored`) — whether to release a parked runner and whether to start a turn.
   `deliverTaskNote` picks the recipient (parent's agent, else the conversation), tags the
   message, and orders queue → fact → wake by hand with a long comment explaining the two
   interleavings that broke. That ordering is correct; it is also written once per caller.

## 2. What this wave changed

`internal/session/task_status.go` is one pure function, `ProjectTask(TaskFacts) TaskStatus`,
plus `TaskIndexEntry.StatusFacts`. It answers four questions that used to be one word:

- **presence** — `queued`, `working`, `waiting`, `finishing`, `done`, `incomplete`,
  `needs-look`, `stopped`, and the zero value for a state this build does not recognise.
- **who or what is being waited on** (`TaskWaitOn`), which is what makes "waiting" mean
  anything. A "whose move is next" enum was drafted and then removed: no production surface
  consumed it, and the runtime's actual continuation affordances — `SteerTask` for a running
  node, `ResolveUnverified` for a claim nobody could check, and `ContinueTask`
  (`task_continue.go`, which reopens a settled node onto the frontier under its own id) —
  already have their own doors. A projection that named a next action would be a second,
  weaker copy of those rules.
- **change disposition** (`TaskChangeDisposition`) — merged, in-place, kept, conflicted —
  which is source control and nothing else.
- **fault** — whether something broke, as distinct from work that did not finish.

`internal/tui3/taskstatus.go` holds the two adapters and the single table that spells a
reading in this program's words. Nine derivation sites now go through it.

Behavior that changed as a result, all in the direction of not claiming things:

- a task a person stopped is `stopped` everywhere, not `failed` on the record page;
- a run the wire, a provider, a threshold, a loop or a stale brief ended is `incomplete`,
  not `failed` — the cross is kept for actual faults;
- a **queued** node in a live session says `queued`, not `running`;
- the check and repair rounds read `finishing` (from the published lifecycle phase, not
  from whether a sentence happened to arrive with them);
- a node this window never watched keeps its recorded claim instead of being called dead.

Documented gaps, not fixed here: the project index carries no merge word, branch, hold, gap
or prerequisite, so a record row can never raise the unlanded-edits demand a live row
raises; harness phases are strings with no typed lifecycle, so exactly one of them
(`HarnessPhaseAsking`) is interpreted by name.

## 3. The communications boundary

The requirement is one delivery mechanism with different recipient policy per kind, built by
**extending the queue that exists** — not by adding a bus. Today's parts already form most
of it:

| Concept | Today | Gap |
| --- | --- | --- |
| Message | `userMessage{message, refs, replyTags, wake, said, authored, steered, ending}` | Flags, not a kind. Origin (person / agent / tool / runtime) is inferred from which flag is set. |
| Recipient | an `*Agent` pointer picked at the call site (`deliverTaskNote`) | Identity is a pointer. Nothing names "session S, node N". |
| Queue | `a.steering` (+ `a.ambient` for telemetry) | Fine. This is the mailbox; it should stay. |
| Wake | `wakeLocked` (main) / `releaseTaskWaitLocked` (a parked runner) | Correct and already one-step-with-the-append. It is re-implemented per caller. |
| Receipt | `enqueueNote` returns a bool; `SteerTask` returns `(waiting, error)` and `ErrNobodyToRead` | Accepted vs read vs superseded vs rejected are not distinguished. |
| Correlation | `replyTags []TaskReplyTag{ID, Title, Request}` | Only for landing notes; no reply-to for questions. |
| Revision | none | Nothing records which assignment revision a message or a result belongs to. |

### 3.1 The envelope to extract

One struct, in `internal/session`, carrying:

- **id** — stable per message, so a retry can be de-duplicated;
- **from / to** — `TaskAddress{Session, Node}`; a task number alone is not unique across
  sessions, and today's `*Agent` pointer cannot be persisted or routed later;
- **origin** — authenticated person, agent, tool, runtime. A quoted user line in a document
  is *not* a person's origin, and this is the field that has to make that impossible;
- **kind** — direction, work request, result, question, answer, progress, runtime notice;
- **body or content reference** — a hash identifies content, not an event: a person
  repeating an instruction after a correction is a new event with the same body;
- **correlation** — reply-to for question/answer pairs;
- **assignment revision** — which version of the assignment this message belongs to.

### 3.2 One mailbox, one wake, different recipient policy

Keep exactly the primitive `enqueueNote` already implements — *append and signal the
eligible reader in one locked step* — and let the recipient's policy differ by kind:

| Kind | Wake policy | Why |
| --- | --- | --- |
| User direction | release a parked runner; consider at the next legal model/tool boundary | somebody is standing there waiting; `enqueueSteeredLine` already does this half |
| Child result | mark owed, release the parent's wait, do **not** start a turn inside a task | `postTaskNews`'s existing law: a node's turns are its runner's to start |
| Question to a parent | same as a result — it must wake a parked owner, or the wait deadlocks | today a child's question has no route that is not a landing note |
| Answer to a child | release exactly the wait that named that question | scoped waiting: an unanswered question blocks only work that needs the answer |
| Progress | coalesce into the ambient queue; never a model wake, never an established fact | `enqueueAmbient` exists for exactly this and must not grow a wake |
| Runtime notice | typed state change; wakes only when it is actionable | `ending` already works this way for a parked job |

Two rules the current code makes easy to break and the extraction should make hard:
**progress must not become a paid wake**, and **data must not become authority** — an
origin field that cannot be forged by quoting.

### 3.3 What is explicitly not in scope

No global registry, no remote fan-out, no distributed event store, no event sourcing.
Cross-session is a *shape* requirement only: addresses instead of pointers, and a delivery
interface a future router could implement. Nothing in this wave routes across sessions, and
the extraction must not claim it does.

## 4. Staged extraction plan

Each stage is a seam, a mechanical move, and a test that would catch the move going wrong.
Stages are ordered by (risk of the current crossing) ÷ (cost of the move).

| # | Stage | Move | Boundary test |
| --- | --- | --- | --- |
| 0 | **Projection** *(done in this wave)* | `task_status.go`; surfaces adapt | table test over facts → reading; tui3 tests that every surface draws one node the same way |
| 1 | **Communication envelope + mailbox** | envelope type, `TaskAddress`, one `deliver(envelope)` used by `enqueueNote`/`enqueueSteeredLine`/`deliverTaskNote`/`postTaskNews` | duplicate delivery is de-duplicated; close/enqueue race yields an explicit rejection, never a silent drop; a parked parent wakes for a child result *and* for a child question; progress delivers with zero model calls |
| 2 | **Landing / change disposition** | the five `land*` functions keep the state decision and hand branch questions to a workspace seam; the note text moves next to the projection's vocabulary | a landing with each merge outcome produces the same state and the same person-facing reading; a protected checkout still keeps the branch |
| 3 | **Evidence and outcome** | `task_audit.go`'s verdict becomes a value the lifecycle consumes, rather than a call the lifecycle makes | a refused verdict, an absent verdict and an audit that never returns each produce their documented state without the scheduler knowing how the audit ran |
| 4 | **Admission and context** | one entry point returning a frozen assignment (brief, ground, mode, limits, model) | admission refusals are unchanged; a started node's contract is byte-identical to the one admission produced |
| 5 | **Storage** | checkpoint writes move behind one recorder instead of ~20 inline `graph.checkpoint()` calls | old checkpoints still load; a crash between any two transitions replays to the same graph |
| 6 | **Worker** | `childRun` becomes usable for main-thread work too (one loop, two threads) | the same conversation script produces the same transcript through either entry |

## 5. The next two or three surgical extractions

Concrete, small, and each with a place to stand:

1. **`deliverTaskNote` + `enqueueNote` + `enqueueSteeredLine` + `postTaskNews` → one
   `inbox` seam** (~250 lines moved, no behavior change intended). The ordering comment in
   `deliverTaskNote` — queue, then the fact, then the wake — becomes the seam's invariant
   instead of a paragraph each caller has to honour. *Integration test boundary:* a parent
   parked on two children, one of which reports while a person steers the parent; assert
   exactly one model turn carries both, and that neither wake is lost. This is the lane the
   root has said it will assign next; the envelope above is its shape.

2. **`roomStateWord` (room.go, ~110 lines of switch) → the projection.** It is the last
   large surface table and it is richer than the others: it prefers the kind's phase word,
   the lifecycle line, the gap, then the hold. Three of those four are now facts the
   projection already takes; the fourth (`stoppingWord` while a stop is in flight) is a
   presence the projection does not have and should gain. *Integration test boundary:* the
   header, the roster row and the composer's room segment say consistent things about one
   node through a whole run — queued, working, checking, stopping, stopped.

3. **`TaskIndexEntry` gains the facts the record cannot currently carry** (merge word,
   branch, hold), so that the history page can raise the unlanded-edits demand that only
   live rows raise today. It is a persisted-format change and therefore wants its own
   wave: additive fields, absence stays unknown, old rows keep their present reading.
   *Integration test boundary:* a session that lands a conflicted branch and is then
   reopened from the index alone shows the same demand it showed live.

## 6. Criticism of this plan

- **The projection could grow into a second engine.** It must stay a pure function over
  facts. The moment it starts asking the graph questions, it becomes a place where
  scheduling decisions hide behind a presentation name.
- **Extraction can be motion without value.** Stage 1 is worth doing because it removes a
  hand-repeated ordering invariant and a pointer-shaped recipient. Stages 4–6 are worth
  doing only if a real defect keeps crossing those lines; moving code into more files is not
  a result.
- **The envelope invites over-typing.** Six kinds is a small set answering real differences.
  A seventh should have to name a wake policy nothing else has.
- **`task_audit.go` is the biggest untouched risk** and it is not first on this list, because
  its interface to the lifecycle is narrow (a verdict) even though its internals are large.
  Size is not the ordering criterion; crossings are.
- **A record that cannot say whether the person got their answer is the real gap.** Both the
  index and the projection can describe a run and its branch. Neither can say "the draft was
  delivered". That is the field the delivery lane owns, and no amount of branch state infers
  it.
