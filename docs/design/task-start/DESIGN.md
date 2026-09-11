# Task start — design

The task-start wave (2026-09-11) exists because a carried-on task on the owner's laptop
spent four minutes and eighteen seconds between the turn ending and the worker's first
request. Each lane of the wave appends the part of the start it changed, saying what was
true and what is true now. The quality law binds every section: the same models answer
the same questions with the same briefs; what changes is when things happen, what
overlaps, and what the person sees while they wait.

## A proposal starts while the rest of its reply is still arriving (lane B)

**What was true.** A reply carrying several `propose_task` calls ran none of them until
the whole reply had streamed. The stream already surfaced each call the moment its
arguments closed (`StreamToolCallReady`), and the turn loop already started calls from
it, but only the four readers (`earlyTools`: read, grep, find, ls). The reason is loop.go's
safety law, and it is sound: a reply that fails is asked for again, and a mutating call
started early would then run twice. So the first proposal of a batch waited for the
second and third to finish streaming, and only then put its card up and started its
fifteen-second countdown.

**What was measured.** Every transcript on the Spark (5,562 files) holds ten replies with
two or more task calls, all `propose_task`. No transcript carries per-delta timestamps, so
the stream time after the first task call closes was estimated from the bytes that follow
it, two ways: the reply's own wall time times that share of its output, and those bytes
at 3.5 bytes per token over a throughput (the lane ledger puts deepseek-v4-flash lanes at
10 to 56 tokens a second; 40 and 100 bracket it).

| task calls | replies | bytes after the first closes | share of wall time | at 40 tok/s | at 100 tok/s |
|---|---|---|---|---|---|
| 2 | 8 | p50 1.8 KB, p90 4.4 KB | p50 11.3 s, p90 60.8 s | p50 11.6 s, p90 31.3 s | p50 4.6 s, p90 12.5 s |
| 3 | 1 | 2.1 KB | 13.2 s | 15.1 s | 6.1 s |
| 4 | 1 | 10.4 KB | 18.7 s | 74.1 s | 29.7 s |

Even on the fastest bracket, the median is several seconds a reply. For a proposal the
gain is capped by its countdown: the work starts at the later of the reply ending and the
countdown ending, where it used to start at the reply ending plus the countdown.

**What is true now.** A tool may be built in two halves, and `propose_task` is.
`internal/exec/bare`'s `StagedTool` builds a tool out of its first half, a function
that returns a `Staged` value. The `Staged` value's `Commit` is the half that cannot be
taken back, and its `Withdraw` undoes the first half. Execute is derived: the two halves
run back to back through `RunStaged`. Whether a tool may start early is `Tool.Stages()`,
and the field behind it is unexported, so the property is the tool's shape rather than a
claim it makes about itself. That is the difference from `earlyTools`, which stays
enumerated because read-only-ness cannot be checked.

For `propose_task` (task.go), the first half is `stageTask`: it reads the arguments,
asks the door refusals, settles the model, resolves the ground, takes a fan slot and an
id, runs the preflight, and puts the card up with its clock running (`openTask`). The
wait for the answer runs on its own goroutine from that moment (`taskWait.run`), so an
answer given and a clock expiring while the reply is still arriving are read in the order
they happened, exactly as before. The second half is `stagedProposal.Commit`: it reads
what the wait came to, applies the redirect and the model shortlist, compiles what was
said around the work (`admissionContext`), and admits.

**The quality law, applied.** The admission context moved from before the card to the
commit. The brief it produces quotes the transcript, including the words of the
assistant message carrying the call. That message is recorded only once it is whole, so
compiling it early would hand the node a brief missing the reply's own framing. At the
commit, the brief is byte-for-byte the brief the batch would have compiled. For a call
nobody started early it is compiled at the same moment in the turn as before, because
nothing between the old and new positions writes to the transcript.

**The hold.** An early start carries a `bare.Hold` on the call's context. `RunStaged`
waits at it between the halves. `Release` lets the commit run. `Withdraw`, or the turn's
context ending, runs the first half's `Withdraw` instead. The first decision wins. A
withdrawn proposal is gone everywhere:

- the question is withdrawn from every window with `the reply that proposed it did not
  go through`;
- the card settles as `withdrawn · its reply did not go through` (the new
  `TaskNotice.Withdrawn`, drawn by `internal/tui3`'s `proposeTask`);
- a late answer finds nothing to answer;
- the fan slot is handed back.

Two things stay. The id is spent, as a declined proposal's is. The place the ground
ladder resolved stays on the conversation, because it is a fact about the conversation
and not about the call.

**Consent.** The card counting down early is the point: consent is the commit, so
nothing starts before the person (or the clock) has answered *and* the reply is whole.
An answer given before the reply finishes is honoured when it does. An answer given to
a call that is then withdrawn is discarded with the call, and the replacement call is
put to the person afresh.

**The seam in loop.go.** loop.go belonged to another lane when this landed, so the lines
that switch the early start on sit on branch `speed/ts-B-seam` as one commit on top of
this change, with their tests (`stageearly_loop_test.go`) and the manual lines that
describe the behaviour. The manual lines travel with the seam because the pages must not
describe machinery that is not running yet. The seam is five moves, all in the warm
batch:

1. `consider` accepts a call whose tool `stagesEarly` (stageearly.go) and runs it on a
   context carrying a new hold.
2. `take` releases the hold when the batch claims the call, and withdraws it when the
   sighting disagrees with the response.
3. `keep(calls)` runs the moment a response is in hand and withdraws held calls it does
   not carry. The transport can switch requests mid-answer (hedge.go), and a response
   can end in words alone.
4. `reset` withdraws everything still held. It already runs on every retry, steer and
   refused reply.
5. `runTurn` defers one `reset` for every other road out of the turn.

Every withdrawal waits for the call to have finished withdrawing, so a turn cannot close
its lane before the card's settling is sent.

**What was deliberately not done.** `quick_task` is not a staged tool. Its commit is the
worker's first request, and that request's brief quotes the reply that asked for it
(`quickBrief` prints the admission context), so under the quality law nothing of it can
run before the reply is whole. What could run early is parsing and a slot, which takes
microseconds. The worktree and the other preparation after admission stay after
admission too. Starting them before consent would be starting work before the person has
said yes, and that preparation belongs to the lanes that own it (S1, S2).
