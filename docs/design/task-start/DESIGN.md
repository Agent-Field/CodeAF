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
