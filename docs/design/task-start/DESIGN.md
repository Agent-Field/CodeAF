# Task start: the worker's first request waits on nothing that can run beside it

*Opened 2026-09-11 by the task-start wave (integration branch `speed/task-start`).
Each lane appends its own section; this one is lane S2's. The measurement every
section starts from is node 5 of conversation `de9eabcb10cc1e45`, a round-10
carry-on: the worker's first request went out **4 minutes 18 seconds** after the
turn that handed the work over had ended.*

## S2 — memory and sizing run beside the worker, not in front of it

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
from `askTask` beside `awaitTaskAnswer`, with the tree removed on a decline.
