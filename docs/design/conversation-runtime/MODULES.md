# Modules: where the responsibilities are, and where the seams are

**Read as of the integration line at `15ebb5d61`.** Every row marked *implemented* names
code in this tree. Every row marked *proposed* is a plan and nothing more. `PLAN.md`,
`PRODUCT.md` and `COMMUNICATION.md` describe intended behavior and are not claims about
what ships.

Two words this document uses precisely:

- a **seam** is a named boundary *inside* `internal/session` — one file that owns a
  question, with the rest of the package calling it instead of re-deriving it;
- an **extracted package** is a separate Go package with its own import boundary.

**No part of the conversation runtime has been extracted into a standalone package.**
`internal/session` is still one package. The seams below are real files with real owners;
they are not module boundaries the compiler enforces. The only enforced boundaries in this
area are the pre-existing package lines: `internal/session` (engine), `internal/remote`
(client/server protocol), `internal/tui3` (terminal surface), `cmd/aforge` (doors).

## 1. Seams that exist

| Seam | File(s) | What it owns | Status |
| --- | --- | --- | --- |
| **Delivery** | `mailbox.go` | who is addressed (`conversationID{session, task}`), who is speaking (`messageOrigin`), what kind of message it is (`messageKind`), one hand-over (`deliverTo`) and one answer (`deliveryReceipt`) | implemented |
| **Durable acknowledgement** | `mailbox.go` (`durableDelivery`, `deliveryID`, `Agent.hasRecorded`), `task_store.go` | the difference between a message a live reader accepted and one the recipient's own record holds | implemented |
| **Assignment and revision** | `assignment.go`, `assignment_tool.go` | the frozen admitted spec plus a revision overlay; which origins may move the done-condition | implemented |
| **Admission context** | `admission.go`, `admission_compile.go` | one bounded, attributed selection of conversation excerpts and tool-output handles per admission door | implemented |
| **Result** | `task_result.go` | the full answer kept apart from the compact card, with a retrievable overflow reference | implemented |
| **Context window** | `toolcompact.go`, `stub.go`, `turnfold.go` | the bounded *view* of frozen history sent to the provider: reduced tool results with retrievable pointers, a fold to a headroom target, a linear budget walk | implemented |
| **End-of-turn handoff** | `turnhandoff.go` | whether the current request's work was handed to a task that is still live, so the turn can end without polling | implemented |
| **Projection** | `task_status.go`; adapters in `internal/tui3/taskstatus.go` | one pure `ProjectTask(TaskFacts) TaskStatus`: presence, wait-on, change disposition, fault, attention | implemented |
| **Lifetime of a view** | `internal/remote/client.go` (`WorkOutlivesExit`, `Detach`), `internal/tui3/keeper.go` | whether closing a window ends the conversation, asked as a capability rather than guessed from a hostname | implemented |
| **Lifecycle and scheduling** | `task_run.go`, `task_ledger.go` | the graph, the frontier, state transitions, the five landings | no seam; owns several questions at once |
| **Evidence and outcome** | `task_audit.go`, `task_claims.go`, `taskgrade.go`, `task_checks.go` | did the work hold, and what is the record of it | no seam; reached through calls, not a value |
| **Workspace and change disposition** | `taskgit.go`, `treehold.go`, `task_branch_protection.go` | worktrees, branches, merges, holds | no seam; the landings call it inline |
| **Storage** | `task_store.go`, `checkpoint.go`, `task_index.go`, `sessionfile.go`, `world.go` | what survives the process | no seam; `graph.checkpoint()` is called inline at many sites |

## 2. What each implemented seam actually guarantees, and what it does not

### 2.1 Delivery (`mailbox.go`)

A message is addressed to a `conversationID` — a session plus a task number, where task 0 is
the main chat. `deliverTo` walks the caller's ordered mailboxes and returns one receipt:
`nobody`, `closed`, or `accepted`. The two implementations are an `*Agent` and a `roomSeat`,
which resolves the live reader and appends under one hold of the room's lock, so a runner
withdrawing its seat cannot interleave into a swallowed message.

Origin is carried separately from kind. `fromRuntime`, `fromPerson` and `fromAgent` all
arrive on one queue as user-role text, and the label is what keeps a descendant from raising
its own authority by writing a sentence that sounds like the person. Kind chooses the queue:
`msgProgress` goes to the ambient queue that no step drain reads, so telemetry cannot start
a paid turn however it was addressed.

**Not guaranteed:** accepted is not read. A receipt says a live reader has the words on its
queue and nothing else.

### 2.2 Durable acknowledgement

For news whose sender is owed an answer about the *recipient's record* rather than its queue,
`durableDelivery` carries an id composed from checkpoint facts and a `settled` callback that
fires when the recipient's own journal holds the line. Only that second fact is written down
as announced, and a resume asks the journal (`sessionFile.recorded`) before re-telling a
landing.

**Not guaranteed — stated in the code and repeated here because it is the most likely thing
to be misread as shipped:** this is **not exactly-once**. Dedupe covers what the journal
holds; a session with no journal, or a journal line that never reached disk, is told again on
resume, which is the direction it is designed to fail in. **Nothing here makes an external
effect idempotent.** A task that already sent an email has sent it, and no acknowledgement in
this runtime changes that.

### 2.3 Assignment and revision (`assignment.go`)

The admitted specification is frozen; corrections land as an overlay of `assignmentRevision`
values, and `effective` is what the work is judged by. `directionOf(messageOrigin)` is the
single mapping from origin to authority: **only a person's direction may move the
done-condition.** An agent's coordination line, however it is phrased, is recorded and read
but can never re-aim the work. A landing may not publish while a person's direction is
unread, which is what closes the race between a correction and a worker's finalization —
that boundary is on the node, not in the mailbox.

**Not guaranteed:** discussion is not direction. There is no classifier model call deciding
whether a sentence was "really" an instruction; the rule is structural (which door the words
came through, and which origin they carry). An ordinary question asked in a room does not
rewrite the acceptance condition, and a genuine correction typed anywhere *other* than into
the task is not automatically routed to it — see §4.

### 2.4 Admission context (`admission.go`)

Each admission door compiles a bounded context: attributed quotes (`AdmissionQuote` carries
speaker and source) and tool handles (`AdmissionHandle` carries call, tool, input and
outcome, where the outcome may be `unknown`). Records are versioned and survive checkpoints.

**Not guaranteed:** the selection is deliberately partial, and the partial-selection rule is
stated in the file rather than hidden. This is **not a durable universal constraint ledger**:
there is no promise that every relevant earlier statement a person made is present in the
context a task receives. A handle is useful only because the tools available to the recipient
can open it; an identifier alone would establish nothing.

### 2.5 Result (`task_result.go`)

The full answer is kept apart from the compact card, with a bounded excerpt and a retrievable
overflow reference, and the outcome qualification is retained rather than flattened into
"done". Delivery of a result is recorded separately from whether the work merged.

### 2.6 Context window (`toolcompact.go`, `stub.go`, `turnfold.go`)

Compaction rewrites the snapshot leaving for the provider, never the transcript. The system
prompt and the newest frozen batch stay verbatim; everything earlier is replaced by a reduced
view of itself — the head that says what ran, the tail a checkpoint digest already proved
keeps a verdict, the exact count of bytes cut, and a pointer to where the whole of it can be
read back (`Agent.fullResultPointer`), which falls back to the journal and then to saying
plainly that it cannot be read. Nothing reads a result and decides what it meant; head, tail
and counts are mechanical. A result may be shortened and is never dropped, because the
provider pairs every call with its result. A pass folds to a headroom target below the
threshold rather than re-firing at each step, and the budget walk carries a running total
instead of recomputing one.

**Not guaranteed:** the model sees less than the whole history by construction, and a pointer
is only useful because the read tool can open it. This bounds resend cost; it is not a claim
about answer quality.

### 2.7 End-of-turn handoff (`turnhandoff.go`)

The measured failure this answers: a turn that correctly *handed work to a live task* was
re-opened by an end-of-turn reader asking whether the outcome had arrived, and the model,
with nothing to do, polled its own task. The seam asks the narrower question — did this turn
put the current request's work into a task that is still live — and lets the node's own
landing start the next turn, at wake prices, with the report in front of it. It is
request-scoped and does not poll.

### 2.8 Projection (`task_status.go`)

One pure function over facts. It separates presence (`queued`, `working`, `waiting`,
`finishing`, `done`, `incomplete`, `needs-look`, `stopped`, and a zero value for a state this
build does not recognise) from what is being waited on, from source-control disposition, from
fault, from attention. `Settled()` reads the lifecycle state, not the presentation word.
Liveness is three-valued (`unknown` / `held` / `unclaimed`) so an absent answer stays absent
instead of becoming "dead". Wire enums and checkpoint formats are unchanged.

**Not guaranteed:** the project index still carries no merge word, branch or hold, so a
record row cannot raise the unlanded-edits demand a live row raises. Harness phases are
strings with no typed lifecycle; exactly one (`HarnessPhaseAsking`) is interpreted by name.

### 2.9 Lifetime of a view (`internal/remote`, `internal/tui3`)

`Welcome.Persistent` is the engine's own statement about whether it outlives the connection.
`Agent.WorkOutlivesExit()` reports that fact; `Agent.Detach()` sends `MethodDetach` when it is
true and falls back to interrupt-and-close when it is false. A terminal exit — including
SIGHUP — detaches instead of ending the conversation, so running work, standing questions,
drafts and parked messages survive. `/close` and an explicit stop are still endings, and the
quit hint promises "keeps running" only for conversations that will actually keep running.

## 3. UX first principles this architecture is answering

1. **The main chat is for thinking, not for waiting.** A person describes what they want and
   keeps talking. The turn ends when the request has been handed off, not when the outcome
   exists (§2.7).
2. **Workers own their results.** A task holds its own assignment, evidence and answer, and
   reports when it has one. The main chat does not poll it, and a progress tick is not a
   reason to pay for a turn (§2.1).
3. **A person can steer a room.** Words said into a task room are recorded on that task as a
   direction with an origin, and a person's direction moves what the work is judged by while
   an agent's does not (§2.3).
4. **The surface says what is true and no more.** A stop is not a failure, a queued node is
   not running, an unknown liveness is not death, and a window closing is not an ending
   (§2.8, §2.9).

## 4. What is NOT achieved

Stated plainly, because each of these is easy to read into the sections above.

- **No distributed runtime and no exactly-once external effects.** Everything here is local
  and in-process. The durable acknowledgement bounds *re-telling*, not *re-doing* (§2.2).
- **No main-chat correction addressed at a task.** Steering *inside a task room* works
  (§2.3). A correction typed in the main chat is not routed or broadcast to running tasks,
  because nothing decides which tasks such a line concerns. This is the live UX gap.
- **No universal durable constraint ledger.** Admission context is a bounded selection, not a
  record of every constraint a person has ever stated (§2.4).
- **No general Pareto superiority.** Specific costs are measurably reduced and covered by
  tests — a long frozen tool history compacts to a sub-linear prompt, a fold reaches its
  headroom target, the budget walk is linear (§2.6). That is not the same as being faster or
  cheaper than another harness overall, which the available comparisons do not establish; see
  `IMPLEMENTATION.md`.
- **No extracted packages.** §1's seams are files, not import boundaries.

## 5. How addresses, origins and messages extend later without cross-session infrastructure

The shape is already the extension point, and it costs nothing today:

- **Address, not pointer.** `conversationID` carries a session id because a task number is
  minted per session and repeats across them. A later router would resolve a remote address to
  a mailbox exactly the way `deliverTo` resolves local ones, and the callers would not change.
- **`mailbox` is a two-method interface.** `address()` and `accept(delivery) deliveryReceipt`
  are satisfiable by something that speaks to another process. The receipt already
  distinguishes nobody / closed / accepted, which is the vocabulary a remote delivery needs.
- **Origin is a field, not an inference.** Adding `fromRemotePerson` or an authenticated
  peer origin is an enum case plus a policy decision in `directionOf`, not a rewrite of
  authority checks scattered across call sites.
- **Durable ids already exist.** `deliveryID` is composed from checkpoint facts, so replay
  protection across a restart and replay protection across a peer are the same mechanism.

What such a step would additionally require, and what deliberately does not exist now:
authenticated authority (an origin claim from another process must be verified, not
believed), admission policy (who may address this session at all), and replay protection at
the boundary. **There is no discovery service, no registry, no global bus, and no
cross-session permission model in this tree**, and nothing above should be read as a partial
implementation of one.

## 6. Proposed next work

Ordered by (how often the current crossing breaks something) ÷ (cost of the move). None of
this is started.

1. **Landing / change disposition.** `task_run.go` and `task_ledger.go` hold five landings
   that each decide a state, ask git about the branch, compose the note the model reads and
   write a checkpoint. The wording of a landing lives in the file that owns the scheduler.
   Move the branch questions behind the workspace seam and the note text next to the
   projection's vocabulary. *Boundary test:* each merge outcome produces the same state and
   the same person-facing reading; a protected checkout still keeps its branch.
2. **`roomStateWord` into the projection.** The last large surface table. It prefers the
   kind's phase word, then the lifecycle line, then the gap, then the hold; three of those
   four are facts the projection already takes. The fourth — a stop in flight — is a presence
   the projection should gain. *Boundary test:* header, roster row and composer segment agree
   about one node through queued → working → checking → stopping → stopped.
3. **Evidence as a value.** `task_audit.go`'s verdict becomes something the lifecycle
   consumes rather than a call it makes. *Boundary test:* a refused verdict, an absent verdict
   and an audit that never returns each produce their documented state without the scheduler
   knowing how the audit ran.
4. **Storage recorder**, then **one worker loop for main and task threads**. Both are worth
   doing only if a real defect keeps crossing those lines. Moving code into more files is not
   a result.

## 7. Criticism of this plan

- **The projection can grow into a second engine.** It must stay a pure function over facts.
  The moment it asks the graph a question, scheduling hides behind a presentation name.
- **The delivery seam can be mistaken for a transport.** It is a resolution-and-append rule
  with an honest receipt. Every sentence about cross-session use in §5 is about *shape*.
- **Kinds invite over-typing.** Four kinds answer real differences in queue and wake policy.
  A fifth should have to name a policy none of the others has.
- **`task_audit.go` remains the largest untouched risk** and is still not first, because its
  interface to the lifecycle is narrow even though its internals are large. Size is not the
  ordering criterion; crossings are.
- **The record still cannot say whether the person got their answer.** The index and the
  projection describe a run and its branch. Neither states "the draft was delivered", and no
  branch state infers it.
