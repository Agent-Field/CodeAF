# Closing the window: work keeps going, or waits to be continued

SETTLED 2026-09-20. Proposed 2026-09-19; the two questions at the foot were
the owner's and he answered both. It builds on `DESIGN.md` (D4, one working
copy per run), `CHAT-ROLE.md` and `WORK-TAB.md`. Existing words only, plus ONE
new state word, `interrupted`.

## What the owner asked

"what happens once i ctrl c codeaf? does things run in bg? … can we give
options for things to just be running, or make sure we restart exactly from
where, not ending or cancelling tasks: it goes to interrupted state and we ask
when launching 'continue these tasks' and they press enter".

## The rule

Closing a window never ends work and never loses it. Work is in exactly one of
three conditions, and the screen always says which:

| Condition | It means | The person can |
| --- | --- | --- |
| running | something is driving it now | watch, steer, stop |
| interrupted | nothing is driving it; everything it did is kept | continue it with one key, or leave it |
| settled | done, incomplete, your call, stopped | read it |

Nothing restarts silently. Nothing is silently dropped. Nothing spends money
with nobody present unless the person turned that on.

## What is true today (read at `39ffbc548`)

1. `ctrl+c` at rest is the way out. A hosted conversation DETACHES: the engine
   keeps it, and a turn in flight finishes. `--no-host` interrupts and closes.
2. The engine retires a conversation idle for 30 minutes and exits 2 minutes
   after it holds nothing (`internal/enginehost/host.go`, `sessionIdle`,
   `hostIdle`).
3. `Agent.WorkingNow` (`internal/session/work_tree.go`) sums the task graph,
   the orchestrations and the background jobs. It never counts a live run. A
   conversation whose only live work is a run therefore reads idle and is
   retired at 30 minutes, mid-run.
4. A run's context is the proposing TURN's, detached from its cancellation
   (`task.go`, `context.WithoutCancel`). Nothing ties the run to the
   conversation, and nothing cancels it; it ends when the process ends.
5. On reopen, a run row that had not settled is stamped failed and stopped
   (`task_store.go`, `runRowNotice`). Seen on a real screen 2026-09-19:
   `stopped · it ended when codeaf closed; its journal is kept`, over a run
   whose store held every row, result and step.
6. Nothing re-enters a run's store, though the machinery exists: the
   supervisor releases claims whose owner is gone and re-reads the ready set
   on every pass (`internal/run/run.go`, `releaseStale`). Its only caller is
   the road that starts a run.
7. A task of the shipped road is set back to queued on reopen and restarts
   with no question asked.
8. The five-minute tick (`codeaf tick`) wakes standing orders only. It never
   touches a conversation's tasks.

## Defaults proposed

1. **The engine is alive when the window closes: the run keeps running.** It
   is what detaching already promises for a turn, and a run is the longer case
   of the same promise. **Two** plain fixes make it true: a live run counts as
   work in `WorkingNow`, and the run's context belongs to the conversation
   rather than to the turn that proposed it.

   This page first said three, the third being that the engine does not exit
   while a run is live. Building it showed that one is not a fix but a
   CONSEQUENCE of the first. The retirement of a conversation and the host's own
   quiet both hang off one reading — `WorkingNow`, through internal/remote's
   `workingNow` — so a run that appears there keeps its conversation, and a
   conversation that stays keeps the host. An engine-side rule of its own would
   be a second authority over one fact, and the first thing it would do is
   disagree with this one. Corrected after the cell landed, because a design
   page that stays wrong after the build teaches the next person the wrong
   shape.
2. **Nothing is driving it: the run is `interrupted`, never failed.** The
   engine died, the machine slept or restarted, or a `--no-host` window was
   closed. The store already holds the truth: tasks done, tasks ready, and
   claims whose owner is gone.
3. **On the next launch the person is asked, once, and `enter` continues.**
   Never an automatic restart: it spends money. The shipped road's silent
   requeue on reopen is the behaviour to retire, not the one to copy.
4. **The tick never continues a run.** Off, with no setting. If unattended
   continuation is wanted later it is one setting with a dollar cap, and its
   own small design.

## The one card

It sits where `needs you` already sits on home, and it is the first thing in a
conversation that is reopened holding interrupted work.

```
 2 tasks were interrupted when codeaf closed
 ▸ 1  continue them     recommended · picks up from the last finished step
   2  leave them        they stay interrupted; open one to continue it later
```

- An interrupted task wears the state word `interrupted` on the rail, the
  work tab and its page, and its dot row stands as it stood.
- `stopped` stays for work a PERSON stopped. `incomplete` stays for work that
  ended short. `interrupted` is only ever "nothing is driving this".
- Leaving them is a real answer. The card does not come back for the same
  tasks; each one's page carries `continue` as its own key.

## How, with what exists

- **Continue** is the conversation's engine opening the same store and
  starting the run's supervisor on it. Claims whose owner is gone are
  released, the ready set is re-read, and workers are seated as at any pass.
- A task that was claimed when the window closed is run again from its own
  recorded steps: the worker reads its trajectory and carries on after the
  last FINISHED step. A step that was mid-command is repeated. That is the
  honest limit, and the card's second line says it.
- The run's copy is the one it had (D4). Landing is unchanged: an explicit
  step whose note says what moved where.
- Reading an interrupted run costs nothing and needs no engine: the rows, the
  page and the `tasks` tool answer from the store.

## Order of work

Planned together with the cell that gives a hand-off its own copy, since both
change how a run is started.

| # | Cell | Size | New words |
| --- | --- | --- | --- |
| a | a live run counts as work, and the run's context is the conversation's; the engine staying while a run lives falls out of the first | S | none |
| b | on reopen, an unsettled run reads `interrupted` from the store's own state, never failed and stopped | S | `interrupted` |
| c | the card, and continue through the existing supervisor on the existing store | M | none beyond b |
| d | the tick continues a run | not built | a setting, its own design |

Every cell is accepted on the real binary in hosted mode, which is how a
person runs codeaf: close the window mid-run, reopen, read the screen.

Two parts of that are easy to drop and are part of each cell's definition of
done rather than a follow-up:

- **A cell that has to end an engine ends it by the process id recorded when
  it was launched**, confirmed by reading that process's own executable. Never
  by matching a pattern against a command line, which matches the wrapper
  around it, and never by signalling a group.
- **Cell b carries the manual in both directions.** The new word has to reach
  the pages, AND the page that today tells a person their run ended when
  codeaf closed has to be found and corrected in the same change. A page that
  names a word while denying the thing around it satisfies every gate and
  still lies.

## The owner's two questions, and his answers

He answered both on 2026-09-20, in his own words:

> 1. `interrupted` is the state word. Yes.
> 2. Continuing always asks first, and the five-minute tick never continues a
>    run on its own. Yes.
>
> The design is settled on those two points. Build it as written.

So `interrupted` joins running, finishing, done, incomplete and your call, and
it means only "nothing is driving this". Continuing is always asked for and
never assumed. Cell d stays unbuilt: the tick continuing a run is off, with no
setting, and would need its own design and its own cap before it came back.
