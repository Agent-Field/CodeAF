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

**The question waits for you, and leaving the page does not lose it.** `esc` out and back
in — or open the run again hours later — and a run still sitting on its cap asks again, with
the same three answers, because the pause is a fact about the run rather than a notice that
went past. Once you have answered, it is not asked twice while the run catches up.

## Watching a run and steering it

A run takes a root row on the roster, with its nodes drawn under it as a tree — see *A
run's nodes on the roster* below. On narrow frames its live members take ordinary chips in
the strip's single flat row; the strip does not draw the tree. Those rows and chips are a
picture of the run, not tasks: the run's own **page** is where a node is read and where the run is steered. Press
**→** over an empty message box with no room open and the page opens, or press the run's
row — or any of its nodes' rows — on the roster; a paused run brings its own page up when
it asks its question.

The page is the graph: nodes as chips in layers, the planner's notes as thin lines between
them, and the fuel gauge pinned in the header (`$0.87 / $2.00`), which is never dropped at
any width. **A planner's note is shown whole**, wrapped across as many lines as it takes
and hanging under its `· ` bullet — it is the planner's own sentence about what it just
decided, and half of one says nothing. What gets cut at a narrow width is the picture: a
chip, a glyph, the goal in the header. The page re-reads the run four times a second. `esc`
leaves; the conversation is untouched.

**Type a sentence with the page open and it goes to the planner**, which sees it on its next
call. Steering outranks the plan. It is talk to the planner, not a new goal: what it cannot
do is change what a node already running was asked for.

When no page is open, the planner's notes land in the conversation as dim lines beginning
`run · `.

## A run's nodes on the roster

A run registers itself with the same machinery tasks use, so the work shows up where you
already look for work: **one root row for the run**, and **one row per node** hanging under
it. The roster keeps the family whole and draws three-cell tree connectors:

```
 ⠋ audit the pricing code
├─ ✓ read the tariff table
├─ ⠋ read the invoice writer
└─ ◌ write up what disagrees
```

The family's fold behaves like every other roster family: live families start open;
settled or parked-only families start folded with an aggregate glyph and `▸ +N` badge on
the root. The fullscreen roster below the column's width floor draws this same tree.

The states are the ones every other row on the roster speaks: a node waiting on its
prerequisites or on a lane is **queued**, a node working is **running**, and a node that
finished is **done** or **failed**. A node the planner took back — work it decided against
before anything started — settles as **stopped**, because nothing went wrong with it and
nobody made a finding about it. The row carries the node's own spend, and a landed node's
row carries its digest.

**These rows write nothing into the conversation.** A landing card is how work *you*
decided on reports back; a run's nodes are cut by its planner and there can be a dozen of
them, so a card each would bury the answer under the workings of it. The run's own write-up
is the one thing that lands in the conversation.

**They are doors, and they open onto the run rather than onto a task.** There is no task
behind a node, so pressing one never walks into a task's room. Press a node's row — `enter`
with the roster focused, or click it — and the run's own page opens with that node's card
already up. The run's root row opens the same page at the graph.

## Opening a node — seeing its chat, its thinking and its tool calls

A node's card is one press from the graph: move to its chip and `enter`, or press the
node's row on the roster. The card carries the node's goal, what it needed, its digest when
it has landed, and its error when it failed.

The last line on the card is `transcript · enter opens it`, and that is the node's own
conversation — the same rendering a task's room uses. You get the instruction the node was
given, what it said back, what it thought, and every tool call it made, with a call's
arguments and its output opening under it. A call the node has not got an answer to yet is
drawn as still running, because the journal records the asking before the result.

A node that is still working streams into that view: the page re-reads its journal on the
same four-times-a-second poll that redraws the graph, and reads once more after the node
stops, so the closing lines land whichever way the two race. `esc` walks back to the card,
`esc` again to the graph, `esc` again out of the run.

A node that has not started has no journal yet, and the page says `no transcript yet`
rather than showing an error. Only a long transcript is trimmed, and it says how much:
`… N earlier lines` sits at the top of what is kept.

The file itself is a real session journal at
`~/.aforge/v3/runs/<session>/<run>/<node>.jsonl`, so `read` opens it like any other.

## Being asked whether that should have been work

Sometimes an answer arrives in words when the honest answer was work. After a turn that
called no tools and answered a substantial message, a second small model reads what you
asked and the first two lines of what came back, and decides one thing: should that have
been work? A yes raises **one card**, on the same row a harness offer uses:

```
? run harness "adaptive run"? · research across every package · [enter] run · [esc] no
```

The name in quotes is the **shape** being offered — `adaptive run` or `task` — and the dim
line beside it is the judge's own sentence about why. The row says `run harness` because it
is the harness offer's row, reused; nothing about a saved harness is involved.

- `enter` or `y` starts it: an adaptive run on the default **$2.00** tank, or one task,
  admitted straight away from the goal the judge wrote.
- `esc` or `n` drops it. Nothing started, nothing was written down, and the answer you
  already have is untouched.

**It never starts anything on its own** — the card is the action. It is asked at most once
every three turns, so two cards can never arrive back to back, and it is quiet on short
messages, on turns that called tools, and in any session with no screen to answer it
(`--once`, a task node). If the judge cannot be reached, or answers with anything that is
not the small JSON object it was asked for, nothing is said at all.

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

## What happens when a run node's reply is cut off at the output limit

A text-only reply is normally how an adaptive-run node finishes. If the provider says
that reply was cut off at its output-token limit, aforge does **not** accept the fragment
as finished work. It tells the node that the reply was cut off and asks it to continue in
smaller parts, using tool calls to save a large deliverable when writing is in scope and
keeping its final report short.

That continuation is bounded: the node gets two continuation attempts. If all three
replies end at the output limit, the node stops instead of spending forever. Its digest
begins `INCOMPLETE: the node's final reply was cut off at the output limit after two
continuation attempts.` The planner reads that warning with the fragment, so it can treat
the node as unfinished rather than mistaking the prose for a completed deliverable.

## When a run's worker says it wrote a file and did not

A node that was given a write scope and **changed no file** does not land as done, however
its last sentence reads. Models end turns on lines like "Now I have both files. Let me
write the synthesized report." — which reads exactly like success and is not — so the last
word is never what decides it. What decides it is whether anything was written.

Such a node comes back **failed**, and its digest begins:

```
INCOMPLETE: nothing was written — this was scoped to write research/report.md and it ended without writing anything.
```

Its own last words are kept underneath, so nothing is thrown away. The planner reads that
as unfinished work and can send something different; it can no longer mistake the
narration for a deliverable.

A node with **no** write scope is read-only work — its brief says `THIS IS READ-ONLY WORK:
find out, do not change anything` — and writing nothing is the whole of what it was asked
for. It is never held to this.

## Why it kept spawning the same worker over and over — the repeat guard

If a run seemed to run the same brief again and again — worker after worker sent at one
file, each one coming back looking fine, the file never appearing — that is the shape this
guard exists to end.

A run will not hand the same file to worker after worker forever. Once **two** nodes aimed
at one path have come back without writing it, the run refuses to take a third node for
that path, stops asking the planner for more, lets whatever is in flight finish, and goes
straight to its write-up. You see one line saying so:

```
research/report.md has been handed out 2 times and nothing was written to it; this run stops asking for it and reports what is actually there
```

The write-up is then asked to be honest rather than tidy: it must say which parts of the
goal were answered, say that this part is **incomplete**, and not describe the unwritten
file as finished. This is not a stop — a stop is your decision and skips the write-up
entirely. The run ends **done**, with an answer that admits what is missing.

A file two nodes wrote *successfully* is not this case: the guard counts nodes that failed
at a path, never nodes that touched it, so ordinary multi-step work on one file is
unaffected.

## What happens to a run when aforge closes or restarts

**A run does not survive the process.** It has no checkpoint and nothing resumes it: its
planner, its nodes and its fuel tank all live in memory, and closing aforge ends them.

What survives is the **record** of it. Every run takes a row in the project's task list
(the `@` list and the `tasks` tool) the moment it starts, saying **running** — that is how
you can see a run that is still going. When the run ends, a second row closes it with its
final state, its answer and what it spent.

If the process went away before the run could close its own row — a crash, a kill, a
laptop that slept — the row is closed the next time you open that session, and it reads:

```
incomplete — aforge closed while this was still running
```

So a run can never sit in the list saying "running" hours after anything was running it.
Only *this* session's rows are closed that way; a run in another window you still have open
is left alone. Each node's transcript stays on disk at
`~/.aforge/v3/runs/<session>/<run>/<node>.jsonl` whatever happened, so whatever the workers
did get done is still readable.

## When a run is the wrong tool

- Work that can be done in the conversation is done in the conversation.
- One self-contained piece of work is a **task** (`propose_task`) — one worker, one brief,
  one branch.
- A shape of work that will recur is a **sub-harness**: built once, saved, and offered
  again. See *Saved shapes of work*.

A run is for the one-off goal with several genuinely independent parts and no shape known
in advance.
