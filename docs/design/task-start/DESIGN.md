# Task start — design

The task-start wave (2026-09-11) exists because a carried-on task on the owner's laptop
spent four minutes and eighteen seconds between the turn ending and the worker's first
request. Each lane of the wave appends the part of the start it changed, saying what was
true and what is true now. The quality law binds every section: the same models answer
the same questions with the same briefs; what changes is when things happen, what
overlaps, and what the person sees while they wait.

## Breadth and depth of a task tree

**What was true.** A task node could hand out at most five pieces (`taskFanLimit`) and
the tree stopped two tasks deep (`taskDepthLimit`): the conversation's task could split,
its pieces could not. Both numbers were guesses, and the comment beside them said so.
Five was a claim about decomposition ("more than that has not decomposed its work, it has
shredded it"), and two was a claim about where a third level stops paying, and neither
had been measured. The fan-out page also told a worker to split when a step had "two or
three parts", which anchored breadth at the size of the old cap whatever the work was.

**What is true now.** The cap is twenty pieces per node and the tree goes three deep.
Neither number is what decides how wide work goes, and neither is presented as if it were:

- **How much runs at once** is bounded by the person's `task.parallel` and by the admission
  governor (`task_pressure.go`), and parts that would write one file are refused or
  queued by their claims. The fan cap is a runaway stop: a node handing out more than
  twenty pieces has lost the plot.
- **Whether to split** is the worker's own reading of the material, taught by
  `prompts/fanout.md` and the picture paragraph in `beltfacts.go`, with the division
  road's evidence gate under it. Sequential work is never split.
- **Three levels** let a part of a wide job that opens its material and finds it wide in
  turn split its own share, instead of grinding through it.

**The one sentence about the goal** lives in the picture paragraph of `beltfacts.go`'s
`handoffFacts`. That paragraph is the one text every agent that can hand work out reads,
including the conversation, whose fan-out is uncapped:

> THE GOAL IS THE SHORTEST WALL TIME FOR THE WHOLE JOB: when what is ahead has parts that
> do not need each other, hand them all out at once, however many there are, before you
> open the first, the way one mind with a team of workers would, and keep one to begin
> yourself, rather than working through them in turn, the order you fall into unless you
> choose otherwise.

It tightened the paragraph's existing `HAND THEM OUT AND KEEP ONE` sentence and absorbed
two neighbours: "Working through them yourself is the slowest order there is" and "What
they wait on is wall time, not calls". Those two now say the same law. It was first
written onto `prompts/fanout.md`, which only a task node reads; review caught that the
agent that fans out most never saw it. The fan-out page now carries neither this sentence
nor its old `WEIGH THE CLOCK AT EVERY STEP` paragraph, which restated the picture's
arithmetic.

The law registry files it as `handoff.wall-time-goal` (`lawCore`). The registry now
searches the pages only a task node is handed as well (`worker.md`, `revise.md`,
`fanout.md`, `divide.md` and `quick.md`), so a second copy on any of them fails the build.

Against `dev`:

| page | before | after |
| --- | --- | --- |
| conversation fixed prefix | 38,742 | 38,843 (+101: +77 on the page, +24 in `propose_task`'s description naming both bounds from their constants) |
| depth-1 worker page | 28,397 | 28,231 (−166) |

**Depth is read once.** `fansOutAt(depth)` in `task.go` is the one reading of
`taskDepthLimit`. The belt asks it of the worker itself (`Config.mayFanOut`), and the
fan-out page asks it of the depth one below. So the page tells a depth-1 worker "a piece
you hand out may split its own share under the same cap", and tells a depth-2 worker "a
piece you hand out cannot hand out more — it does not have the tool", from the same line
that builds the pieces' belts. Before this change the page carried the second sentence
unconditionally. That was true only because the limit was two.

**A number derived without saying so.** The `tasks` tool lists a node's own family, and it
was clipped at the project search default of ten rows. That was safe only because a
family could never exceed five. A node that names no limit is now shown its whole family,
which is bounded by `taskFanLimit`. A test fails if the cap ever passes the listing's
ceiling.

**An accident that depth two hid.** A division's part opens on its parent's brief
composed around its own scope, and it inherits the person's sentence. `armDivision`
read both as the part's own words, so every part of an eleven-file division was armed
`counted` by its parent's eleven files. It was inert while parts stood on the floor with
no verb to use it. At depth three it would have handed each part the whole job's pile to
divide a second time.

`armDivision` now reads a piece's own words:

- The judge's yes arms only the root it was asked about.
- The count reads the scope under the part's `WHAT THIS PART WORKS ON` heading.
  `partOwnWords`, beside `partBrief`, reads that heading back, so a restored checkpoint
  answers the same way without new state.

A part whose own scope counts a pile of its own is still armed, and may divide it.

The rule a piece opens on (`briefPieceRule`) said handing work out "is not yours to do
again", which read as a ban once pieces carried the verb. It now declines "any handing out
it asks for": the message's instruction, not the piece's own reading of its share.

**What guards it.** In `task_nest_test.go`:

- `TestATreeGrowsToItsDepthLimitAndNoFurther` walks a real tree down through `propose_task`
  until the verb comes off. It must come off exactly at `taskDepthLimit`, and at every
  level the page's clause about its pieces must agree with the belt the next level was
  built with.
- `TestTheManualSpellsTheBoundsTheCodeEnforces` pins every manual sentence that states
  either number to the constants.
- `TestAParentListingItsPiecesSeesEveryOneOfThem` pins the family listing.
- `TestAPartIsArmedByItsOwnWordsAndNotItsFamilys` pins the arming.
- The fan-cap tests read `taskFanLimit` and hold at twenty unchanged.

**What twenty does to the runtime.** Measured by reading, not by a live run:

- **Nothing breaks.** The rail draws a family of twenty and a third level without a fixed
  row cap, and scrolls on a short terminal. The provider's limiter queues past sixty-four
  requests in flight, and worktree creation is serialised on the git-root lock.
- **The admission governor weighs the machine once per frontier pass.** A batch that
  becomes ready together is therefore admitted on one reading. At five pieces that was a
  small burst; at twenty it is twenty checkouts and twenty builds starting against a
  reading taken before any of them. That is the governor's shape, not the cap's, and it
  is not worked around in the cap. It is issue
  [#878](https://github.com/Agent-Field/aforge-v2/issues/878), and lane G owns the fix.

### The governor seam (#878)

**What is true today.**

- `runFrontier` (`task_run.go`) asks `g.governor.holds()` once, before it takes the
  graph's lock.
- It then walks every queued node, and `holdOnStartingLocked` answers each one from that
  same `busy` bool.
- The reading is load per core against `DefaultTaskMaxLoad` (1.5) and MemAvailable
  against `DefaultTaskMinFreeMB` (1536 MB), behind a one-second cache
  (`task_pressure.go`).
- Nothing in it accounts for a node the pass itself has just started. A node's memory
  arrives seconds after admission, when its checkout is carved and its first build runs,
  and load average is a one-minute decayed figure. So no reading taken inside the burst
  can see the burst. With `task.parallel` unset, every ready node in the pass starts.

**The shape the fix takes.** Two changes, one mechanism:

1. **The frontier asks the governor per admission, not once per pass.** Each node that
   would take a slot is put to the governor at the moment it would start, so the answer
   can differ between the first node of a batch and the twentieth. The reading itself
   stays behind its cache and outside the graph's lock. What moves is the question, which
   becomes "may THIS node start, given what has been admitted since the reading".
2. **A reservation per admitted node, counted against the memory floor until a reading
   taken after that node started.** The governor keeps the nodes it has admitted since its
   last fresh reading and subtracts a per-node footprint from MemAvailable for each of
   them.
   - A reservation is released by the first reading taken after the node started, which is
     the first reading that can see the node's own memory. It is never released by a timer.
   - The footprint is a belief measured from nodes that actually ran on this machine, not
     a constant somebody picked, for the same reason the count was never the resource
     (`task_pressure.go`'s header).
   - Until a node has been measured on this machine, there is no honest number to
     reserve. The design question #878 carries is what stands in until then: the peak of
     the last nodes seen, or one start per fresh reading. It also carries where that
     measurement lives.

**What does not change.**

- Nothing running is stopped.
- A machine this package cannot measure still never holds.
- A node that takes no slot is held by neither ceiling.
- The fan cap stays a runaway stop and is not lowered to stand in for any of the above.

## The ground a task stands on (lane S1)

### The fork nobody used

**What was true.** The ground ladder (`internal/session/groundladder.go`) walks three
rungs, highest first: a furrow fork of the whole folder, a git snapshot, a file-by-file
copy. Every way the top rung could fail answered a bare `false`, and the rung below
took over in silence. On the owner's laptop that was every node: ninety-three admitted
between 2026-09-08 and 2026-09-11, all ninety-three grounded on the snapshot rung, and
each of them had first attached, sealed and forked its workspace and thrown the fork
away. The measured node spent 15.1 seconds between being admitted and its worktree
existing, with no model call in it, and the snapshot that followed took 0.8 seconds.
Nothing said so anywhere, and nothing stopped the next node paying again. `Fork` was
also the one furrow call on that road with no bound at all.

The laptop's own cause is not reproducible on Linux: on the Spark the same furrow
0.1.0 forks the same shapes of repository and the rung succeeds (eight of eight
recorded nodes). One cause shape was proven there, though. The fork sealed its world
with `git checkout -b` and `git commit`, which run the repository's commit hooks and
honour its signing configuration. The snapshot rung seals with a private index and
`commit-tree`, which runs neither. With a commit-msg hook that refuses machine commits,
the old top rung fell on every node, and the child lost the `.env` the rung exists to
carry. The snapshot rung grounded the same repository without a word.

**What is true now.**

- **A rung answers two questions, in order.** `reach(order)` is asked before anything is
  touched. `carve(ctx, order)` makes the world, answers a `rungFell` carrying the
  reason, or answers an error that stops the ladder. The walk writes a `groundClimb`
  into the node's log: every rung that stood down or fell, with its reason and cost,
  then `its world was made in <time>`. The climb is the log's alone. The worker's brief
  is unchanged.
- **A fork that could not be made is remembered for its ground** in
  `~/.aforge/v3/universe-falls.json` (`groundfalls.go`). The next node on that ground
  skips the rung before touching furrow, and its log says
  `a fork of the whole folder was not tried`, with the reason and when it last failed.
  The memory holds only while the aforge build (`buildinfo.Identity`) and the furrow
  program (`furrow.Program`: path, size, mtime) both match the failure. A change to
  either end tries again, and a fork that succeeds forgets the fall. A task the person
  stopped mid-fork is not a fall.
- **One seal.** `sealForkWorld` is deleted. The fork is sealed by `sealGroundWork`, the
  snapshot rung's own door, and its branch is cut at the seal by `standOnSeal`
  (`git branch`, `symbolic-ref`, a mixed `reset`). That plumbing runs none of the
  person's hooks. A checkout could not have done it anyway: the fork's working copy
  already holds the seal's files, but its index holds HEAD's.
- **Every furrow call is bounded unless its clock is the person's.** `Fork` shares the
  attach's bound (`wholeWorkspaceTimeout`, one minute, since both read the whole
  folder). A merge preview takes the read bound. `WaitDelay` makes a bound end the wait
  as well as the process. `Attach` now answers why it could not, in furrow's words.
  `TestEveryCallIntoFurrowIsBounded` walks the package and fails on an unbounded call
  that is not listed as person-paced (only `RunInFork` is).

**The trade, stated.** A fall that was a coincidence stands the top rung down for that
ground until the next build or furrow change. Meanwhile, tasks there get git's world,
without the ignored files, and every such task's log says so. Retrying on every task is
the fifteen seconds this section was written to end, and retrying on a timer is a knob
with no measured value behind it.

Measured on the Spark (real furrow, four nodes per shape, per-node ground time):

| ground | before | after |
| --- | --- | --- |
| plain repository | universe · 0.44–0.49 s | universe · 0.46–0.47 s |
| commit-msg hook refuses | snapshot, no `.env` · 0.81–1.03 s every node | universe, `.env` · 0.46–0.48 s |
| furrow refuses the fork | snapshot · 0.80–1.02 s every node | snapshot · 0.98 s once, then 27–30 ms |

### The runner's reading of the tree

**What was true.** The runner (`task_child_run.go`) ran
`git status --untracked-files=all` synchronously for every finished tool call, to ask
whether that call moved the working copy. A batch's end events arrive together, after
the whole batch has run, so every one of those processes read the same tree. The first
call in call order took the credit for whatever the batch did, even when it was a
`read`. The readings held the node's room feed and its finish. They did not hold the
worker's next request, because `hub.send` never waits on the drain. A plain `git status`
also takes the index lock when it refreshes, which the worker's own `git add` needs.

**What is true now.** A `treeWatch` is read **at most once a batch**, by the first call
of the batch that could have changed the tree (`couldChangeTheTree`, keyed on the
belt's classes). Reading hands, harness refusals and saves that name their file ask
nothing. A batch whose only change WAS a save would leave the fingerprint a batch
behind the tree, and the next batch's first command would be handed the movement the
save made — so that batch spends its one reading at the end of itself
(`childRun.batchSettled`), for the fingerprint and for no verdict, at the moment the
batch is over and the next request has not gone. The reading is an `offpath.Reading`, the type lane L6 (#876) built for
looped.go's twin of this reading, settled within `lane.Hysteresis`. A tree git cannot
read that fast is credited a batch late and does not hold the drain. The watch belongs
to the run, and `runTaskChild` closes it on the way out, which kills a reading in flight
and waits for its goroutine. `worktreeDirt` runs with `--no-optional-locks`.
`TestTheRunnerReadsItsTreeOnlyThroughTheWatch` fails if anything in the run reads the
tree another way.

| per 8-call step | before | after, with an edit and two bash | after, read-only |
| --- | --- | --- | --- |
| aforge-v2 clone, 5,706 files | 61.6 ms | 8.3 ms | 0 |
| 50-file repository | 11.3 ms | 1.5 ms | 0 |

## S2 — memory and sizing run beside the worker, not in front of it

*The measurement this section starts from is node 5 of conversation
`de9eabcb10cc1e45`, a round-10 carry-on: the worker's first request went out
4 minutes 18 seconds after the turn that handed the work over had ended.*

### What was true

After the working copy existed, a task node made two model calls of its own before
its worker asked anything, and both stood **in front** of the work:

- **The memory router, as a constructor argument.** `newTaskAgentOn` built the
  worker's `Config` with `memoryBrief: a.memoryBlock(ctx, node.assembledBrief())`,
  so one reflex call (6,065 ms on node 5, mistral-nemo, failing open) ran serially
  every time a node built a worker: the first worker, a second one after a provider
  fault, every repair round, every merge resolver, quick and design nodes too.
- **The division reading, in front of the first request.** A node handed a drawing
  by the mark reader had it put to the division road by `divideFromSketch`, called
  after the worker was built and before `runTaskChild`. The reading
  (`reviewDivision` → `callRole(RoleDivision)`) was 219,105 ms on node 5: 5,652
  completion tokens, 4,465 of them reasoning, and the 60 s hazard ceiling never
  fired because a stream that produces tokens is never cut. The card read
  `sizing the work` the whole time and the room read `nothing on this page yet`.

### What is true now

Both calls are a **reading beside the work** (`internal/session/task_beside.go`'s
`besideWork`): started next to the work, bound to the node's own context, and
joined on every road out of the code that started it. The law, stated there once:
**no reading outlives the node that started it, and a reading never decides whether
the work may start.**

**Memory** (`memory.go`'s `nodeMemory`). `runTaskNode` puts one reading per node
run on the node's context (`withNodeMemory`) and joins it on its one road out;
`workTaskNode` begins it before the working copy is carved, so the router's call
overlaps the carve. `newTaskAgentOn` makes no model call: it hands the built worker
the node's block (`handTo`) — at once if the router has answered, the moment it
answers otherwise. The join point is the drain in front of every request
(`agent.go`'s `landVolatileLocked`): a task worker never clears what it was handed,
so a block that arrives after the first request rides the next one. One reading
serves every worker the node builds; the cue is the node's assembled brief, which
is settled at admission. A body that builds no worker (a saved program's run) makes
no reflex call, exactly as before.

**Sizing** (`task_divide_sketch.go`'s `sizingBeside`). The drawing is weighed beside
the node's FIRST worker, which starts at once on its unchanged brief. The division
body is split into its two moments — `weighDivision` (every gate and the reading)
and `admitDivision` (claim, freeze, admit) — with one record written once by
whoever ends the division (`recordDivision`). When the reading answers:

| Answer | What happens |
| --- | --- |
| parts | admitted through the same body `divide_work` uses; the receipt goes onto the worker's own queue and it reads it at its next step |
| a refusal, or nobody reachable | nothing: the worker is already doing the work as one worker |
| work no worker can do | the worker's run is cancelled and the node lands `your call` with the reader's sentence and whatever the worker wrote (`landNeedsPerson`) |
| the worker has said its last word, or handed work out itself | dropped, journalled as `dropped` |

**The one concurrency decision.** "Admit only while the worker is still reading" is
decided under `Agent.handover`, the lock the runner's tail reads "is any part out,
is any report owed" under (`taskNewsStanding`), gated on the worker still holding
its room (`taskRoom.speaker`). Either the tail sees the parts and folds them, or the
reading sees the worker withdrawn and drops — there is no instant in between. The
hold covers one admission per node (a commit of the worker's own files and a few
admits), so the tail's next read waits that long at most, once.

**Two choices inside that seam.** *Work no worker can do* cancels the run's own
context and lands through `landNeedsPerson`, not through the person's stop door
(`TaskGraph.stopFor`), because the two settle differently: the stop door ends a
node `TaskFailed` behind a `stopped` lead, which is the ending for work somebody
called off, while this node's work was never called off — it turned out to need a
person. So it takes the unverified ending (`landShifted`'s road): the branch is
committed and kept, the report leads with `yourCallLead` and carries the worker's
own account under the reader's sentence, and the row that asks the person —
including the bubbling of a child still undecided — is machinery that was already
there. And the receipt is
a `briefNote`, which is not `steered`: it owes no answer and wakes nothing, so it
cannot start a turn on a parked runner. It reaches the model only because
`room.speaker()` has just answered that the worker is mid-turn and will drain the
queue at its next step — the same fact `task_child_run.go`'s queued opening brief
relies on.

**The phase word.** `sizing the work` (`TaskPhaseSizing`) is drawn only where the
asker waits on the reading (`divisionAsker.waits`, read by `sizingWait`): a worker
inside its own `divide_work` call. The drawing weighed beside a working worker moves
no phase; lane V draws that call as a side fact.

### Why the parts are not handed to the worker to re-propose

The brief for this lane proposed sending the reviewed parts to the running worker as
a steer so that it hands them out with `propose_task`. That road has none of the
division's own gates (the scope refusal, the shared-check lift), no family composer,
no careful tier, and would have the worker re-transcribe a mastermind's briefs — and
`task_divide_sketch.go`'s header records that cheap workers were measured never
reaching for a verb to hand out a division already written for them. The quality law
of the wave rules it out. The harness still admits the parts through the one body;
only the moment moved.

### What this costs

On a drawing the reader approves, the worker spends the length of the reading on
work that is then handed to parts; they start from its copy as it stood, so what it
wrote is on their disk, and they start exactly when they did before (the reading was
always in front of them). On every drawing refused or unreachable, the worker has had
the whole reading to work in. A first worker that dies on the wire before the reading
answers takes the reading with it; the second worker is not re-divided (the drawing
is put once) and can still call `divide_work`.

### Measured (scripted, this repository, 2026-09-11)

Memory router 600 ms, division reading 2,000 ms, worker steps 1 s each; the same
test file run on `6a3b478cf` and on this branch:

| | before | after |
| --- | --- | --- |
| admitted → first worker request | 2,642 ms | 27 ms |
| admitted → division answer | 2,628 ms | 2,028 ms |
| memory rides | request 1 | request 2 |
| parts receipt rides | request 1 (held until reports) | request 3 (mid-run) |

### Not done here, and why

**Carving the ground under a proposal's countdown.** The forming card's 15 s
(`config.DefaultTaskAutoApprove`) could hide the carve, but on the worktree and
snapshot rungs carving writes a branch, a worktree registration and objects into
the person's repository before they said yes, which the consent law forbids; and
the carve is lane S1's (`openTaskWorld`, `prepareTaskTreeForNode`). With memory off
the constructor, consent → first request is the carve alone. The seam, if S1's
rungs can carve without touching the person's `.git` (the mirror rung can):
`prepareTaskTreeForNode` taking a reserved id and a spec rather than a node, called
from `askTask` beside `awaitTaskAnswer`, with the tree removed on a decline. Filed
as #893 with that shape and its acceptance.

## V — a request made on a node's behalf is seen while it runs

### What was true

A worker's request left three traces. `loop.go` took the node's pulse either side of it
(`tasks/<id>.beat.json`: `requests`, `request_started`, `request_finished`). Its stream
reached the room through the session's observer, so the room drew the thinking and the
token column as they arrived. And the journal got a `call` line when it was over.

A request made **on** the node's behalf, an errand through `Agent.callRole`, left one of
those three. The reading that sizes the work, the memory recall a node's context
assembly makes and the other side calls all run without their stream, because an errand
is nobody's answer and must not type itself into the room. So for 219 seconds the pulse
said `"requests": 0`, the journal held nothing, the rail said `sizing the work` and under
it `asking deepseek/deepseek-…`, and the room said `nothing on this page yet — it fills
in as the task works`. The ladder's rung was drawn (`sizingLine`, #101) and the call's
own life was not: when it started, whether anything had come back, how much, from which
machine.

### What is true now

**One watcher, bound to the node, on every request made under a context**
(`internal/session/task_calltrail.go`, `callTrail`). internal/provider already counts
each request's life where its stream is read, and hands it to whoever asks through the
context seam `provider.WithCallProgress` (#868). The trail is the node's side of that
seam, and it turns each moment into the three traces a worker's request already leaves:

- **The pulse.** The request going out and coming back are `taskBeat.began` and
  `taskBeat.ended`, the same two edges `loop.go` writes for a worker's request.
- **The journal.** A new evidence-only line kind, `flight` (`journalFlight`, in
  `sessionfile.go`), is written at both ends of every request on the node's own file.
  The start line carries the role, the model and the arm. The end line adds the machine,
  how the request ended, the milliseconds to the first token, the milliseconds overall,
  and the stream's own counts of answer and reasoning. Like `call`, it is never money,
  and the replay drops it.
- **The phase.** The live request rides the phase notice the node already sends
  (`TaskPhaseNotice.Call`, a `TaskCall`), beside the ladder's sentence in `Text`. The
  rail, the room and a hosted window already fold that event whole, so all three draw it
  from one event, and none of them can draw a request under a phase that has moved on.

**Concurrency.** The provider calls the watcher synchronously from its read loop, and the
watcher may do no work there. So `callTrail.heard` appends to the trail's own queue,
under a mutex nothing else takes, and nudges the one goroutine the trail owns. That
goroutine writes the pulse and the journal and sends the notices. The ladder's sentence
(`callTrail.say`) goes on the same queue, so the sentence and the request are said in
the order they happened. A climbing count replaces the queued count before it; an edge
(out, first token, phase turn, ending) is never folded. `callTrail.end` drains the
queue, writes a `cancelled` end for any arm still open (a rescue that lost its race and
has not said so), and joins the goroutine. The caller ends the trail before it moves the
node out of the phase. A report that arrives after the end is dropped at the door.

**One arm on the row.** A hedged question has several requests out at once. The notice
carries the one that has had the most back (`callTrail.leading`), because that is the
request the person is waiting on. The journal keeps every arm.

**The surface** (`internal/tui3/taskphase.go` `callFields`, `room.go` `roomCallRow`)
draws a request as ranked facts after whatever leads the row:

- **What it is doing**, in the provider's own words through one table
  (`callPhaseWords`): `first word`, `paced`, `thinking` or `writing`.
- **How long it has been out.** This is in tenths while nothing has come back and in
  whole seconds after, as `phaseFields` spells a clock.
- **↓**, thought and answer together (`TaskCall.Received`), through the column's own
  spelling (`tokenDownWord`, now shared with a step's caption).
- **The machine**, spelled by `phaseServing`.

On the rail the facts ride the phase word's own row: `sizing the work · thinking 41s · ↓
4,465 · deepinfra`, with the ladder sentence on the row under it. In the room the row is
the ladder sentence led by the thought mark (`tokens.GThought`, through `app.icon`) while
the model thinks, or the wait mark otherwise. It stands where the next thing will appear.
On a page with no transcript it takes the place of `nothing on this page yet`, which was
the page saying nothing was happening while something was.

**Which path each figure takes.**

| Where | Source |
| --- | --- |
| Local rail and room | `EventTaskPhase` on the standing task lane |
| Hosted (`--host`) rail and room | the same event, which `internal/remote`'s task lane carries whole as JSON (`TaskCall` carries no error, so it survives the wire) |
| Another conversation's work opened as a guest | Nothing. Its phase is not drawn today (`tookGuestNotice` ignores phases), and the journal's `flight` lines are the record it could read |
| An outside reader | the pulse file |
| An autopsy | the `flight` lines |

The beat is not read by the surface, and it did not need to be.

### What is not covered, and the seam each needs

- **The memory recall at a node's context assembly** (the six-second `reflex` of the
  measured start) is evaluated inside the child's constructor literal
  (`task_run.go:6893`, `memoryBrief: a.memoryBlock(ctx, node.assembledBrief())`). That
  is `newTaskAgentOn`, which lane S2 owns. The node's own agent, whose journal is the
  node's journal, does not exist yet at that line, so there is no journal for a trail
  to write to. Once the recall runs after the child exists (the shape S2 is changing),
  it gets the same trail. The node's agent wraps the context passed to `memoryBlock` in
  `trailCalls(node, TaskPhaseNotice{Phase: TaskPhaseWorking}, roles.RoleReflex).watching(ctx)`
  and ends the trail when the block returns. The room and the pulse then draw the
  recall. The rail draws a request only under a phase word, so it does not.
- **The handover's brief** (`checkpoint.go:4690`, `callRole(ctx, roles.RoleHandoff, …)`)
  happens before a node exists, so there is no node for a trail to bind to. It is drawn
  as `briefing a worker · 15s` on the conversation's own phase clock (#101). A live count
  there needs a conversation-side watcher that re-tells `PhaseBriefing` with the request's
  phase and machine. That seam is in `checkpoint.go`, which this wave does not own.
- **tok/s.** `provider.CallProgress` carries no rate. The rate the status line draws is
  the stream watch's own, on `PhaseNews.Rate`. When the seam carries it, `callFields`
  passes it to `phaseServing` and the machine reads `deepinfra 38 t/s`, as it does on the
  status line. Deriving a rate here from the counts would be a second estimator of one
  number.
