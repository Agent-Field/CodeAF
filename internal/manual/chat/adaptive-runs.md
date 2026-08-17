# Adaptive runs

## What an adaptive run is

An adaptive run is one big, many-part goal worked on by a planner and a fleet of small
workers at the same time, beside your conversation.

A planner model cuts the goal into small nodes — one question or one artifact each — and a
scheduler starts every node whose prerequisites are finished, immediately. Each node is a
child agent with a context of its own. Every time a node lands, the planner is asked again:
add work, drop work that is no longer needed, say one line about what it is doing, or
declare the goal answered. Nothing waits for it — execution never blocks on thinking.

**The graph is never designed; it appears.** Nobody writes the plan up front. What you
watch is the shape of the work crystallising as the planner learns what is there.

Two things bound it: **one fuel tank in dollars** for the whole run, and small nodes.
Parallelism comes from having many nodes, never from a big one.

## How I start one for you

Ask for the work. Deciding whether a request is an adaptive run is the model's judgement,
made with your whole conversation in view — there is no phrasing you have to learn and no
keyword that triggers it. When the goal genuinely has independent parts and no known shape
("audit every package for this pattern", "migrate us off the old client and fix what
breaks"), the run is started with the `run_adaptive` tool and you are told in one line.

There is also an older, narrower door: a message that BEGINS with `orchestrate …`,
`adaptively work on …`, `run an adaptive run on …` starts one directly, without asking the
model at all. Courtesy openers (`please `, `can you `, …) are stripped first. You can name
the tank in the same sentence — `with a $5 budget`, `on $2.50`, `$10 cap` — and the money
clause is taken out of the goal before the run reads it.

Both doors are off unless there is an interactive screen: a run that empties its tank has
to ask somebody, and a run nobody can answer is a run that stops halfway and stays there.
So `--once` has no runs, and neither does a session opened over `--host` — the fuel gate
travels on a lane a remote connection does not carry. Tasks and background nodes cannot
start one either: a node is handed no runner.

**The turn ends immediately, with no answer written.** The run is the answer and it has not
happened yet. Its write-up arrives later as a line in the conversation.

## What one costs and what happens when the money runs out

Every model call in the run bills against **one tank**: the nodes, and the planner's own
calls too.

The default tank is **$2.00** when nobody named a figure — deliberately small, because the
better moment to ask for more money is with the graph on screen rather than before anything
has run.

- At **80%** the run says so, once.
- At **100%** it **pauses**: whatever is in flight is allowed to finish, nothing new starts,
  and a question is raised on the run's page.

The question has exactly three answers:

```
? out of fuel · $2.00 of $2.00
  add $1
  finish with what we have
  stop
```

`add $1` raises the cap by a dollar and the run carries on. `finish with what we have` skips
to the write-up over the results that exist. `stop` settles the run and keeps what finished.
Each answers with a line saying what it did: `topped up; the run carries on`, `finishing on
what is already done`, `stopped; what finished is kept`.

A run is also bounded in time: **4 hours** covers every node, every planner call, and the
wait at the gate.

## Watching a run and steering it

A run is not a task: it has no row on the rail, because it is not a node. Press **→** over
an empty message box with no room open and the run's page opens — that is the only keyboard
door onto it, and a paused run brings its own page up when it asks its question.

The page is the graph: nodes as chips in layers, the planner's notes as thin lines between
them, and the fuel gauge pinned in the header (`$0.87 / $2.00`), which is never dropped at
any width. It re-reads the run four times a second. `esc` leaves; the conversation is
untouched.

**Type a sentence with the page open and it goes to the planner**, which sees it on its next
call. Steering outranks the plan. It is talk to the planner, not a new goal: what it cannot
do is change what a node already running was asked for.

When no page is open, the planner's notes land in the conversation as dim lines beginning
`run · `.

## What a run's workers may touch

Each node is a child agent working in your workspace, and two bounds are put on it.

- **A write scope.** The planner says which paths a node may write. That is enforced by the
  harness, not by asking politely in the brief: a write or an edit outside the scope is
  refused with a result the node reads, so it can pick another file and carry on. `bash` is
  deliberately not covered — a shell command's effects are whatever it did, and a guard that
  pattern-matched commands would be claiming a guarantee it cannot keep. A node with no
  scope is told it is read-only work.
- **A worktree, sometimes.** When the planner judges a node needs isolation, it runs in a
  worktree of its own under the session folder. If there is nowhere to put one, the node
  shares the workspace instead and is told so. It degrades; it never fails for this.

A node sees its prerequisites' **digests** — eight lines or so each, plus what they wrote —
and never their whole output. That is what keeps the run's context from growing with the
run. Each node's transcript is a real session journal under
`~/.aforge/v3/runs/<session>/<run>/<node>.jsonl`, so `read` opens it like any other.

Nodes run **4 at a time**, and one gives up after 60 steps or 6 steps with no progress.

## When a run is the wrong tool

- Work that can be done in the conversation is done in the conversation.
- One self-contained piece of work is a **task** (`propose_task`) — one worker, one brief,
  one branch.
- A shape of work that will recur is a **sub-harness**: built once, saved, and offered
  again. See *Saved shapes of work*.

A run is for the one-off goal with several genuinely independent parts and no shape known
in advance.
