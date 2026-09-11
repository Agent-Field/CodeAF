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
before. And each conversation's graph keeps its own governor and so its own
reservation, while the visible half is this whole process: two conversations in
one process fanning out at the same moment each read the other's visible work as
covering part of its own reservation. The floor on the reading itself still
holds for both.

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
