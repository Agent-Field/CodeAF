# Task start — design

The task-start wave (2026-09-11) exists because a carried-on task on the owner's laptop
spent four minutes and eighteen seconds between the turn ending and the worker's first
request. Each lane of the wave appends the part of the start it changed, saying what was
true and what is true now. The quality law binds every section: the same models answer
the same questions with the same briefs; what changes is when things happen, what
overlaps, and what the person sees while they wait.

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
nothing. The reading is an `offpath.Reading`, the type lane L6 (#876) built for
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
