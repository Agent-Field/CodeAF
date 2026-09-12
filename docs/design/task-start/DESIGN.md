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
  is not worked around in the cap. It was issue
  [#878](https://github.com/Agent-Field/aforge-v2/issues/878), fixed in the governor by
  the last section of this document.

### The governor seam (#878)

This subsection carried the seam while the fix was owed: the frontier asked the governor
once per pass, every ready node started on that one answer, and the design question was
what footprint to reserve before any node had been measured. It landed as the section
**The admission governor: a reservation, and one question per admission** at the end of
this document, which says what was true, what is true now, and what stands in for a
footprint before one is measured.

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

## The admission governor: a reservation, and one question per admission (#878)

**What was true.** `runFrontier` asked the machine once — `busy :=
g.governor.holds()`, taken before the graph's lock, behind a one-second cache —
and then marked *every* ready node running against that one bool. It was the
right shape for one node and the wrong shape for a fan. Twenty parts handed out
in one breath were twenty agents, twenty worktrees and, minutes later, twenty
builds, and all twenty were admitted against a reading taken before any of them
existed. Neither half of the reading could have caught it: load average is a
one-minute decayed figure, and a node's memory arrives with its first build, so
the reading that finally shows the burst is taken long after the burst was
admitted. The shape was reachable before from uncapped conversation roots; with
the fan raised to twenty it became the ordinary path at four times the width.

**What is true now.** The governor keeps a **reservation** for the work it has
let in, and the frontier consults it **once per admission** rather than once per
pass.

- Every running node is expected to need one node's **footprint** of memory.
- What the running nodes already hold is **visible** in the reading: the
  resident memory of this process and everything it started, above what that
  tree held at a reading taken while this graph ran nothing.
- The part of their footprints not yet visible is taken off `MemAvailable`
  before the floor is compared:

      projected = MemAvailable − max(0, running × footprint − visible)

  and one more node starts only while `projected` is at or above
  `task.min_free_mb`.

The count passed in is the graph's own `running`, which already includes every
node the same pass has started, so the second node of a fan is judged against
the machine the first one leaves behind. Nothing is counted twice: as a node's
memory becomes visible, `MemAvailable` falls by what `visible` rises by, so the
projection stands still while the reading catches up and moves only when a node
settles or the machine frees memory of its own.

**The footprint is measured, and the prior is the machine's own shape.** It is
the larger of:

- `peakShareMB` — the most visible memory per running node any reading of this
  session has seen. It only rises, for the same reason the kernel's own
  `ru_maxrss` is a high-water mark: a figure that fell whenever the nodes
  happened to be between builds would hand the room back just before the next
  build needs it.
- one core's share of this machine's memory, `MemTotal ÷ cores`. A node is
  local work whose heaviest act is a compiler or a test run, which takes a core
  and memory in the proportion the machine was built in. It is what a node is
  assumed to need before anything has been measured — and a footprint of zero
  there would admit a whole first fan against a reading that cannot see it,
  which is the burst this exists to stop.

A consequence worth stating plainly: a quiet machine starts about **one node per
core's share of the memory above the floor** — four on a 16 GiB eight-core
laptop, sixteen on this 122 GiB twenty-core box — and holds the rest with
`waitingMachineBusy`, which the rail draws as `waiting · machine busy`. Held
nodes start as earlier ones settle, or when a later reading shows room; the
five-second poll is still the only clock, because a machine getting quieter is
not an event this process can hear.

**Why the reservation is memory and not load.** Memory is the resource that
fails rather than slows: a machine short of cores runs every build slower and
finishes all of them, and a machine short of memory kills one or swaps until
nothing finishes. And bounding admitted nodes by memory already bounds the CPU
burst, because a footprint is at least one core's share — so a quiet machine
admits at most about one node per core, which is one machine's worth of
compilers rather than twenty. The load half stays exactly what it was: the
reading of a machine busy with somebody else's work.

**What the visible half reads.** `treeResidentMB` walks this process and its
descendants through each thread's `/proc/<pid>/task/<tid>/children` file and
sums `statm`'s resident pages. It follows the tree down rather than reading
every process on the box and sorting out whose is whose, so what it costs scales
with what this process started: a full `/proc` scan measured 13 ms on a machine
running 1,600 processes, and this runs on the frontier's own goroutine. The
kernel promises the children file exactly only for a stopped tree, so a process
born or reaped as it is read can be missed — the right precision for a figure
re-read every second.

**Boundaries, stated rather than hidden.** The reading is `/proc`'s, so on macOS
and Windows the governor still says "cannot say" and never holds, exactly as
before.

**One account for the whole process (#907).** Each conversation keeps its own
graph and its own governor, but the count those governors divide the reading by
is **the process's**, not the graph's. It has to be: the visible half is a
reading of the whole process tree, and /proc cannot say which conversation
started which compiler. Divided by one graph's own lanes, a second
conversation's build read here as that build over *this* conversation's node
count — and the measured footprint only rises, so one such reading narrowed
every later fan in this conversation for the rest of the session. The same gap
ran the other way on the reservation, each graph reading the other's visible
memory as covering part of its own. Both close with `session.TaskLanes`: one mutex and one int,
written by every graph through the one door every `TaskGraph.running` mutation
now goes through (`takeLaneLocked`/`giveLaneLocked`), read back by
`TaskGraph.lanesTaken` for `observe` and by `holdOnStartingLocked` for `admits`.
No clamp to a fraction of `MemTotal`, no timer decay and no per-graph
correction.

**Which graphs share one is said, not assumed.** `cmd/aforge`'s `v3OpenSession`
— the one door every conversation is built through, launch, relaunch, `/new`
and `/resume` — builds one account for the life of the process and puts it on
every `session.Config` it hands out; `Agent.graph` and `standingWideWork` read
it off the config. A graph handed none is **alone in its process** and keeps an
account of its own (`newTaskGraph`), which is the truth about an embedder with
one conversation and about every scripted graph in the tests. A package-level
variable would have been the same claim made silently, and it is a claim a
library cannot make for its caller: a test binary is one process and a hundred
unrelated machines, and a lane one scene left running would have narrowed the
next scene's fan.
`TestEveryLaneMovesThroughTheOneDoor` (`go/ast`, so it is on the pull-request
gate) is what keeps there being one door, and the two graphs of
`task_pressure_account_test.go` are the scene itself.

**Deleted.** `admissionGovernor.holds`, the once-per-pass `busy` bool and the
`busy` parameter of `holdOnStartingLocked`; `TaskGraph.machineBusy`, which asked
the governor a second time from the division's receipt — a question about a node
that did not exist yet. The receipt now reads what the frontier decided
(`TaskGraph.machineHolds`), and says *some* of the parts are waiting, because a
fan wider than the machine is half started and half held.

**The law.** `TestEveryStartIsJudgedByTheGovernor` (`go/ast`, so it is on the
laws gate) fails if a node is moved to `TaskRunning` anywhere but `runFrontier`,
if `runFrontier` stops asking `holdOnStartingLocked` before that move, or if
`admits`/`observe` gain a second caller. A node *born* running is named in
`bornRunning` with its reason.

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
  how the request ended, how many machines the request walked to, the milliseconds to
  the first token, the milliseconds overall, and the stream's own counts of answer and
  reasoning. Like `call`, it is never money, and the replay drops it.

  **A question is more than one request, and the seam says so.** internal/provider
  reports `CallEnded` once per question, naming the request that ended it; a request
  that is refused and walks to another machine reports `CallStarted` again under the
  same attempt, with a later moment and no ending in between. The trail follows that
  shape: a walk keeps one pair of lines and one pulse edge (as a worker's own retry
  ladder does, `loop.go` taking the pulse either side of it and not inside it) and is
  recorded as `hops`; the question's ending closes every request it had out, the named
  one as it ended and the rest as left. The row's clock restarts on a walk, because the
  wait really did begin again, while the end line's length is the question's own, from
  its first attempt.

  **The only reading of the world's clock** in the trail is the moment a request came
  back, and it goes through the agent's one clock door (`Agent.now`, `Config.clock` —
  which is what the checking window has always been measured against), so a test can pin
  a 219-second reading without waiting for one.
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
| Local rail and room | `EventTaskPhase` on the standing task lane. The rail draws against `app.taskNow`, which freezes while somebody is in the room; the room draws against `app.now` |
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

## A task and its own work agree (lane L)

Two beliefs held by the code were wrong in the same way: a task's copy of the world
was treated as a detail of where the *worker* stands, rather than as the world the
task's whole life happens in. A check ran against the person's folder, and a piece of
the task's own work that came home a minute late was handed to the person instead of to
the task.

### A declared check is run against the task's own copy (#886)

**What was true.** `taskCopy.bind` bound `work`, `deliverable`, `acceptance` and
`expects` to the copy the worker was given (`composeBrief`, #566) and bound nothing
else. `checks` went from `spec.checks` onto `TaskNode.Checks` untouched and reached
`runOneCheck` as written. `runOneCheck` sets the command's working directory to the
copy — and an absolute argument is not a working-directory question. So
`grep -q rewritten /person/folder/report.txt`, which is exactly what
`prompts/system.md` asks a parent standing in that folder to write, read the untouched
original: it answered red, the checker spent minutes hunting for files its own check
named (one call ran 2m29s and was abandoned), and correct work landed
`your call · nobody could check it`. The same address made the **before**-reading read
the person's folder too, so a check this work really had broken came back "red before
this work and remains red" — a finding softened by an address. And a check whose first
word was an absolute path into that folder named no file under the checker's feet, so
`runnableHere` dropped it from the door in silence.

**What is true now.** A check is bound onto **the copy it is run in**, through the same
`taskCopy.bind` the brief's four fields go through, at the one place a check is turned
into a door (`auditDoorFor` → `runnableChecks`). The copy is spelled as the directory
the command will be run in — `.` — because a task's check is run in *several* copies of
one ground: the clean restore of what would ship, the commit the task was cut from, and
the worker's own tree for the progress reader. All of them stand at the root of a copy
and all of them run the command there, so one spelling is true in all of them, and the
before-reading, the landing reading and the checker's own shell can no longer disagree
about which tree a check is about. The order in `runnableChecks` is shape, then bind,
then "could this run here", which is what admits the ground's own script instead of
dropping it. `copyOnto` is now the one reading of "is this directory a copy of that
ground", shared by the worker's map (`taskCopyFor`) and the check's (`Agent.checkCopy`,
read off the node's own record of where its work stands). A checker standing on the
**ground itself** — the session's own reading after a task has landed — carries
`standingOn`, the identity: the work is home, so the address the contract wrote names
the place that now holds it. An address outside the ground is left as written, as
before.

### A piece that comes home late is folded into its parent's report

**What was true.** The runner withdrew the parent's seat the instant the worker's
reading was over (`childRun.foldParts`, and `runTaskChild`'s `defer room.speaking(nil)`
for every other road out) — it must, or a line said into that room would be taken by
somebody who will never read it (#273). But the node stays open through its check, its
repair round and its landing, "which on a checked node is minutes away". A piece landing
in that window found an empty seat and fell through to the **person's conversation**,
the fallback written for a parent that has already landed. Nothing was lost from the
person's screen and everything was lost from the family: the piece's result never
reached the deliverable it was cut out of, and the parent's report said nothing about
it. The dominant trigger is not a race — a parent stopped at its threshold leaves its
pieces running, `stopChildren` cuts them, and every one of their landings arrives while
the parent is still being checked. At twenty pieces over three levels (#874) that is
ordinary work.

**What is true now.** `taskNoteReaders` asks three readers in order: the parent's
worker, then **the parent itself** (`landingFold`, `task_latefold.go`), then the
conversation. The fold takes the news into the parent's own report, so the landing
already on its way carries it — one account of what this node's work came to, in the
family it belongs to. The fold refuses on exactly the fact the seat refuses on — the node
has settled — so **only a parent that has already landed** sends its pieces to the
person, which is the fallback as designed. Routing stays one ordered question
(`deliverTo`); nothing branches on which road a message came by. **The fold is only
for news that would otherwise leave the family:** when the delivering agent is itself
the parent's reader (`Agent.standsIn` — a standing firing reads its root node's pieces
as the graph's home, never from a seat), the list is the seat and that agent, with no
fold between them. Folding there took the report from the one reader waiting on it and
parked the firing forever (`TestADivisionUnderAFiringIsWaitedForAndBilledToTheRun`).

Three properties make the fold a delivery rather than a string append, and each has a
test (`task_latefold_test.go`):

- **The words are the sender's.** A landing note is written for a model — it opens by
  telling its reader which word to say back (`landingNoteLead`) and may close on how to
  settle — and a report a person reads must carry neither, nor may the next model be
  handed an order about somebody else's word. So a delivery carries, beside its note, the
  same message **as a record keeps it** (`delivery.record`): `deliverTaskNote` composes it
  as the landing's head line, report and changed files (`landingRecord`, which shares
  `taskNoteHead` with `taskNote` so the two cannot spell the head two ways), and a
  message that says only what happened — `bubbleUnverifiedChildren`'s re-addressed
  sentence — is its own record. The fold writes no sentence of its own.
- **The fold is the acknowledgement.** A delivery is written down as announced only when
  the recipient's record holds it (`durableDelivery`). For a fold the record is the
  parent's report, checkpointed by `foldLatePart` before the receipt returns, so
  `postTaskMessage` makes the mark and settles every durable delivery at once. Without
  that, a restart restored the folded piece as unannounced and told its landing again —
  to the person, once the parent had settled.
- **The report has two halves and one author.** `TaskNode.report` is composed under the
  graph's lock (`composeReportLocked`) from what the landing wrote (`landed`, written by
  every road through `landLocked`) and every folded message (`late`). Both halves are on
  the checkpoint (`taskRecord.Late`, and `taskRecord.Landed` beside them when there is a
  folded half), so a restored node composes from the halves a live one does, and a second
  fold after a restore adds its piece and nothing else.

**What the fold does not do, stated rather than hidden.** It is not a turn: the worker's
reading is over by definition, and starting a second one for a node whose check is
running would pay a model to read a piece into a tree the checker is holding still. And
the check does not see it — the checker is handed the worker's own last words
(`checkerConclusion`), written before the piece came home, and is not asked again. A
second audit of the same tree is the person paying twice for one question, and the
piece's own check already answered for the piece. The manual says the same sentence.

### The bar's four questions

- **The one abstraction.** `taskCopy` as the map from the folder the work is *about*
  onto a copy of it — now reached through one reading (`copyOnto`) by both the worker's
  brief and the checker's door, with `bindCommand` for the one thing a command needs
  that a document does not: to be true in whichever copy it is run in. Anything that
  later has to run something declared in one world inside another world can use it.
- **What was deleted.** `auditDoorFor`'s bare `ground string` parameter (a directory
  with no account of what it was a copy of), `taskCopyFor`'s own copy of the mode
  switch, and `taskNote`'s private spelling of its head line (now `taskNoteHead`, shared
  with the record). `TaskNode.report` stopped being a field any road could overwrite:
  every writer goes through `landLocked`, and the field is composed in one function
  from the halves that own it.
- **The law tests.** `TestADeclaredCheckIsBoundToTheTaskOwnCopy` (the door's check
  answers green on what would ship, red on the base, and the command as written still
  fails — the defect itself), `TestChecksBindOnlyWhatTheGroundHolds` (outside the
  ground, relative, sibling tree, and the identity for a checker standing on the
  ground), `TestAGroundCheckThatNamesItsOwnScriptOpensTheDoor`,
  `TestAChildLandingAfterItsParentStoppedReadingIsFoldedIntoItsReport`,
  `TestAChildLandingAfterItsParentSettledReachesTheConversation`,
  `TestAFoldedPieceIsNotToldAgainAfterARestart`, `TestTwoFoldsAcrossARestoreKeepOneOfEach`
  and `TestTheFoldKeepsTheSendersRecordAndNothingElse`. The structural law is
  `TestEveryCheckDoorIsBuiltFromAMap` (`go/ast`, on the laws gate): every call to
  `auditDoorFor` or `runnableChecks` is handed `checkCopy(...)`, `standingOn(...)` or
  the map its own caller was handed, and a copy's fields are spelled only in
  `task_brief.go`.
- **What a reviewer might call a band-aid.** Spelling the copy as `.`. It is not a
  trick for one call site: it is the only spelling of "the copy this is being run in"
  that is true in all four places a task's check is run, and it is produced by the same
  `bind` as every other address, from a map built out of the real directories. The
  alternative — binding to one named directory — is correct for the checker and wrong
  for the before-reading, which is how the "red before this work" softening got there.

## The typed `/task` door stops waiting (#936)

*The measurement this section starts from: every `/task <brief>` stood in a forming
block for twenty-eight seconds on a thinking model — `sizing it up…` for the sizing
judge's three-second window, then `shaping the brief…` for the shaper's twenty-five —
and both windows ran out and returned nothing, so the work then started on the person's
sentence anyway.*

### What was true

The typed door waited on two model calls in series before the node existed:

- **The sizing judge, in the surface.** tui3 called `JudgeDecomposable` (remote:
  `Task.Judge`) and held the command under `sizing it up…` for its answer. A yes
  armed the node to divide (`rememberDivisible`); a timeout was a silent no.
- **The shaper, in the door.** `StartTask` called `shapeBrief` under
  `taskShapeWindow`, streamed the brief into a preview line under the forming block
  (`WithBriefWatch`), named and placed the node from the shaper's `title` and
  `where`, and on a timeout admitted the person's words with the note
  `brief kept as you wrote it`.

S2 had already moved memory and the drawing's division reading beside the worker. The
person's own door was the one road still in front of it.

### What is true now

`StartTask` asks no model. It admits the node at once on the person's sentence and the
canned done-condition, with two flags on the spec saying what is still to be read
(`taskSpec.unshaped`, `taskSpec.unsized`), and returns in milliseconds. Both readings
run **beside the node's first worker**, through the same `besideWork` S2 introduced, and
are started in `workTaskNode` next to `sizeBeside`, after the worker's opening is
composed — so the first request always goes out on the person's words and waits on
neither reading.

**Width** is one more source for the division road that S2 built, not a second road.
`proposalBeside` has two sources: the drawing (as before) and, for an unsized node, the
judge's parts (`judgedDivision`), asked with `askedByJudge`. That asker carries
`breadth`, which lets the parts past the floor gate the way an armed node's own
division gets past it; the parts are then weighed by `weighDivision` and handed to the
worker by `deliverBeside` as the same receipt #883 delivers. **Arming is not touched**:
`spec.armed` stays frozen at admission, because the belt and the system prompt read it
when the worker is built, and arming a running worker would make the prompt and the
belt disagree. `solo` (typed, or the standing `single`) is the one thing that turns the
judge off.

**The brief** arrives as a steer. `shapeBeside` asks the shaper beside the worker and
hands the written brief to the worker's room as a note through the mailbox
(`deliverTo`, `roomSeat`), opening `YOUR BRIEF IS WRITTEN OUT NOW`. The node's contract
(`spec.brief`, `spec.acceptance`, the assembled brief) is written only when the note is
settled into the worker's record (`durableDelivery.settled` → `writeBrief`), so the
checker is never judged against a contract the worker did not see. A brief that lands
after the worker's last step is not written, and the work stands on the person's words.
There is still one brief format: the shaped brief replaces the person's sentence in the
same assembled shape every node has.

**The name** is the namer's, asked at admission the way it is for every unnamed node;
the mechanical cut of the person's opening words stands until it answers. **The ground**
is the ladder's, which reads the paths in the person's own sentence.

### What was deleted

`JudgeDecomposable`, `Task.Judge` and its call class, `taskCallDeadline`,
`rememberDivisible` at the door, `TaskShapeFallbackNote`, `BriefWatch` /
`WithBriefWatch` and the forming block's preview, window and walk, the surface's
`sizing it up…` phase and `the work looks wide` note, the shaper's `title` and `where`
fields (in `prompts/shape.md` too), and `taskName`. The remote wire moves to version 16.

### The bar's four questions

- **The one abstraction.** `besideWork`, reused: every reading that must not stand in
  front of a worker is started beside it, bound to its context and joined. The typed
  door is its third user after memory and the drawing. `divisionAsker.breadth` is the
  one new field, and any future source of parts that already judged width can use it.
- **What was deleted.** The list above: one surface wait, one remote door, one
  streaming preview and its machinery, and two ignored prompt fields.
- **The law tests.** `TestThePersonsTaskDoorAsksNoModel` (`go/ast`: `StartTask`'s body
  calls no model-asking function); `TestTheWorkersFirstRequestGoesOutBeforeTheShaperOrTheJudgeAnswers`
  (the judge never answers, the shaper is held, the worker's first request lands first);
  `TestAWideTaskStartsItsWorkerFirstAndTheJudgesPartsArriveAsTheReceipt` (the control);
  `TestABriefIsWrittenOnlyWhenTheWorkerReadsIt`; and the existing
  `TestEveryReadingBesideTheWorkHasAJoin`.
- **What it costs, on one branch.** A judged-wide task whose division the reviewer then
  refuses carries on with no `divide_work` verb at all, where dev's armed worker could
  still divide later. Arming cannot move without unfreezing the belt and the prompt
  together, which is a change of its own: issue #958 holds it, with the fix stated.
- **What a reviewer might call a band-aid.** Starting the task on the person's raw
  sentence. It is not a fallback: it is the only brief that exists at the moment the
  work can begin, it is the authority the shaped brief quotes verbatim, and the shaped
  contract replaces it through the same assembled shape rather than a second format.
