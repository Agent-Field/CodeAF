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
conversation. The fold takes the piece's news into the parent's own report, so the
landing already on its way carries it — one account of what this node's work came to,
in the family it belongs to. The report is composed from two halves under the graph's
lock (`TaskNode.composeReportLocked`): what the landing wrote (`landed`) and every piece
folded since (`late`). Either half may move without erasing the other, so a landing
cannot overwrite a folded piece and a fold cannot rewrite a landing. The fold refuses on
exactly the fact the seat refuses on — the node has settled — so **only a parent that
has already landed** sends its pieces to the person, which is the fallback as designed.
A folded piece is marked reported like any other, so nothing is delivered twice.

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
  with no account of what it was a copy of) and `taskCopyFor`'s own copy of the mode
  switch. `TaskNode.report` stopped being a field two writers could overwrite: it is
  now composed, in one function, from the halves that own it.
- **The law tests.** `TestADeclaredCheckIsBoundToTheTaskOwnCopy` (the door's check
  answers green on what would ship, red on the base, and the command as written still
  fails — the defect itself), `TestChecksBindOnlyWhatTheGroundHolds` (outside the
  ground, relative, sibling tree, and the identity for a checker standing on the
  ground), `TestAGroundCheckThatNamesItsOwnScriptOpensTheDoor`,
  `TestAChildLandingAfterItsParentStoppedReadingIsFoldedIntoItsReport` and
  `TestAChildLandingAfterItsParentSettledReachesTheConversation`.
- **What a reviewer might call a band-aid.** Spelling the copy as `.`. It is not a
  trick for one call site: it is the only spelling of "the copy this is being run in"
  that is true in all four places a task's check is run, and it is produced by the same
  `bind` as every other address, from a map built out of the real directories. The
  alternative — binding to one named directory — is correct for the checker and wrong
  for the before-reading, which is how the "red before this work" softening got there.
