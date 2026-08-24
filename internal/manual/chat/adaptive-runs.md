# Adaptive runs

## What an adaptive run is

An adaptive run is a goal whose graph is **planned before the work starts** — a planner
model and a fleet of small workers, beside your conversation.

A planner model cuts the goal into small nodes — one question or one artifact each — and a
scheduler starts every node whose prerequisites are finished, immediately. Each node is a
child agent with a context of its own. Every time a node lands, the planner is asked again:
add work, drop work that is no longer needed, say one line about what it is doing, or
declare the goal answered. Nothing waits for it — execution never blocks on thinking.

**The graph is never designed; it appears.** Nobody writes the plan up front. What you
watch is the shape of the work crystallising as the planner learns what is there.

Two things bound it: **one fuel tank in dollars** for the whole run, and small nodes.
Inside a run, parallelism comes from having many nodes, never from a big one.

**A run is not how aforge works on wide things, and it is not something the conversation
decides on.** Ordinary work — however broad — takes one road: one task, which hands its own
parts out once it has opened the material. A run is reached only by asking for one by name,
and *How a run is started* below is the whole of how. See also *Should this be a run, or one
worker that splits itself*.

## Should this be a run, or one worker that splits itself — when I use a run instead of a task, why didn't you start an adaptive run for this

**I cannot start one, and that is the answer to why I did not.** The tool that opened a
planned run is off my belt entirely — not refused, not gated: I do not have the verb — so
whatever you ask for, work that leaves this conversation leaves it as a **task**.

Work that is simply wide — a sweep across many files, research across many sources, the same
change over many separate items — starts as **one task**, and the worker hands the parts out
itself once it has opened the material and can see how many there are. Each part becomes a
worker in its own copy of the repository, and the first worker stays and folds their reports
into one deliverable. That is *When a task turns out to be too wide for one worker*, in *work
that runs on its own*.

The reason is that nobody can see the parts from the request. A planner asked to cut up
"audit every package for this pattern" is guessing at how many packages there are and what
is in them; a worker that has just listed them is not guessing. Two roads meant the guessing
one kept being taken on width alone, so there is one road now.

**A run is still here, and you can still have one — you ask for it by name.** It is for the
goal whose structure has to be settled up front because its nodes are genuinely different
from each other and the later ones are aimed by what the earlier ones find, or for a plan
you want to watch and steer. *How a run is started* has the words that open one.

## How a run is started — the words that open one, and why asking me for work never does

**One door, and it is a sentence you type.** A message that BEGINS with `orchestrate …`,
`adaptively work on …`, `adaptively run …`, `adaptively do …`, or `run an adaptive run
on …` (`start` and `kick off` work in place of `run`) starts a run directly. Courtesy
openers (`please `, `can you `, `I'd like you to `, …) are stripped first, so `please
orchestrate the migration` is the same request as `orchestrate the migration`. You can
name the tank in the same sentence — `with a $5 budget`, `on $2.50`, `$10 cap` — and the money clause is taken out of the goal before the run reads it. You can
name the model the same way: `orchestrate the migration with opus`.

It is a **table lookup and never a judgement**: the sentence says so in those words or
nothing happens. Anything else you type is an ordinary turn.

**Nothing else opens one.**

- **I cannot.** The `run_adaptive` tool is off my belt — I do not have the verb, so there
  is no phrasing that gets me to reach for a planner, and no card where I offer you one.
- **`/task adaptive` is retired.** The word picks nothing; your brief is kept whole and one
  ordinary worker starts. *Work that runs on its own* has the line it prints.
- **No setting turns it on.** The `starting a task` row had an `adaptive` answer once, and
  it is gone; a profile still holding the word reads as the default.

**The machinery under a run is used elsewhere, and that is not a second door.** A saved
program you launch from `/subharness`, and a harness being designed, both ride this same
engine — but you reach those by naming the program or asking for a harness, and what you get
back is a subharness or a design page rather than a run with a fuel gate you steer. *Saved
programs* and *saved shapes of work* have them.

**The door needs an interactive screen**, because a run that empties its tank has to ask
somebody, and a run nobody can answer is a run that stops halfway and stays there. So
`--once` has no runs, and neither does a session opened over `--host` — the fuel gate
travels on a lane a remote connection does not carry. Tasks and background nodes cannot
start one either: a node is handed no runner. Where the door is off, the sentence simply
runs as an ordinary turn.

**The goal is your sentence, word for word.** Nothing shapes it first and nothing writes a
done-condition around it, so whatever the run has to hold — what must exist at the end, how
it is checked — you write into the sentence yourself. The typed brief for a *task* is
shaped; a run's is not.

**The turn ends immediately, with no answer written.** The run is the answer and it has not
happened yet. Its write-up arrives later as a line in the conversation.

## What one costs and what happens when the money runs out

Every model call in the run bills against **one tank**: the nodes, and the planner's own
calls too.

The default tank is **$10.00** when nobody named a figure. Say your own in the goal —
"with a $5 budget" — and that wins.

- At **80%** the run says so, once.
- At **100%** it **pauses**: whatever is in flight is allowed to finish, nothing new starts,
  and a question is raised on the run's page.

The question has exactly three answers, and it says how it is answered — walk the rows
with `↑`/`↓` (the bold row is the one enter takes), press enter, or click a row; typing
does not answer it, it steers the planner:

```
? out of fuel · $10.04 of $10.00
▌ add $5
  finish with what we have
  stop
  ↑ ↓ pick · enter answers · or keep typing to steer the planner
```

The top-up offer is **half the tank you already approved**, in whole dollars and never
less than one — a $10 run is offered $5 more, a $2 run $1 — so a big run is not begged a
dollar at a time. Taking it raises the cap by that amount and the run carries on. `finish
with what we have` skips to the write-up over the results that exist. `stop` settles the
run and keeps what finished. Each answers with a line saying what it did: `topped up; the
run carries on`, `finishing on what is already done`, `stopped; what finished is kept`.

A run is also bounded in time: **4 hours** covers every node, every planner call, and the
wait at the gate.

**And the whole session says so, to every other window.** While a run sits at its gate the
conversation reads `waiting on you` — on home, and on every other aforge open on the
machine — with `out of fuel · $10.00 of $10.00` beside it, the same line the run's own page
shows. It is the same word an approval question or a task proposal puts there, and it
sorts to the top of its project for the same reason: nothing is going to happen until you
answer. Answer the gate and the word goes away.

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

The page is sectioned, and the graph is a list. Under a dim `work` heading every node has
**one row of its own**: its state glyph, its id, its goal in words, and a dim tail on the
right carrying its dependencies (`needs rfcs client`), its spend, and a `new` mark for
the interval after it appeared. Rows are ordered by depth — a node is always drawn under
everything it waits on — so the shape of the run reads top to bottom, and rows that wait
on the same things are the work running in parallel. Under the graph, a dim `planner`
heading gathers the planner's narration in one place, each note wrapped whole under its
`· ` bullet — it is the planner's own sentence about what it just decided, and half of
one says nothing. Your own steers and gate answers follow (`you steered · …`), then the
`answer` section once there is one. The fuel gauge stays pinned in the header
(`$0.87 / $10.00`) and is never dropped at any width. The page re-reads the run four
times a second. `esc` leaves; the conversation is untouched.

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

**Each node row names the model it runs on, and it is not the planner's.** A run is
deliberately two classes of model: the root row carries the **mastermind** class that cuts
the goal, and every node row under it carries the **small work** class that does it — one
careful call deciding what happens, many cheap ones doing it. So opening a node and reading
`task <model>` at the foot of the frame is how you see your crew actually working; the run's
own row above it will be naming something else, and that is the arrangement rather than a
disagreement. The id is settled once, when the run starts, so a `/model` half way through
does not move it. A run whose classes resolved nothing says nothing rather than guessing.

## What the rows under a run are called — sub task names that were just the prompt, and workers all named "You are a"

**Each node is named by the planner, on the same call that adds it.** Every node it hands
out carries a `title` of its own: two or three lowercase words naming the role or the
slice — `traffic shapes`, `token bucket`, `pricing sheet` — and that is what the roster
row, the home card and the task list all draw. It costs nothing extra; it is one more
field in an answer aforge was already paying for.

It is a separate thing from the node's **goal**, which is the whole brief the worker
opens on. That brief is written to the worker in the second person and runs to a
paragraph, so a name cut out of its first words named every worker in a run after the way
its instructions began — nine rows all reading `You are a`, one reading `You are
assembling`, and no way to tell which was which. The goal is still read in full on the
run's own page and on the node's card; it is simply not what the row is called.

If a node ever arrives without a name — an older planner, a reply that lost the field —
the row is named from the node's **id** instead, when that id has words in it: a slug like
`token-bucket` reads as `token bucket`. It is never cut out of the goal again.

## Why a run's rows read `r1`, `r2`, `synth` — ids where names should be, and what happens now

**They do not any more, and an id is never what a row is called.** Half the ids a planner
mints are its own filing rather than language — `r1` through `r7` with `synth` at the foot
of them — and spelling one of those out leaves you with `r1`. So a node whose title is
missing, or is nothing but its own id over again, is sent to the same small naming model
every other piece of work in aforge is named by: two or three lowercase words, read off
what the node was actually asked to do.

That call is made the moment the node joins the run and **nothing waits for it** — the
work is already launchable, and the answer lands a second or two later. In that gap the
row is drawn as `task 19` — aforge's own word for work nobody has named yet — and renames
itself when the name arrives. You may see one flicker past on a fast node. What you will
not see is `r1`: a raw id on a row looks like an answer and is not one.

If the naming call cannot answer at all — the provider is having a bad minute, the reply
comes back as a path — the row falls back to the id, because a row with a poor name is
still better than a row missing from your project's history.

## Why nothing seems to happen for the first minute of a run — `forming the work`

A run's row appears on the roster the instant you ask for it, and the workers under it
cannot exist until the planner has answered — the one call in a run with no work to
overlap it, and the most consequential thinking it does. That is a real wait, and the row
says so rather than sitting blank:

```
 ⠋ audit the pricing code
   forming the work
```

The moment the first worker exists the line goes away, because the rows under the run are
the picture from then on. Nothing is animated and nothing is guessed: the line changes
when a worker actually exists and at no other moment, so a run whose planner is taking a
long time keeps saying `forming the work` for exactly as long as that is true.

There is no second thing to dismiss afterwards. The family's ordinary fold is the
collapsed view — `←` on the run's row folds its workers into one line with a `▸ +N` badge,
`→` opens them again, and that choice is remembered.

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

## When a turn should have been work — a run is never what starts

A turn answered in words when the honest answer was work does get caught: a second small
model reads it afterwards and can start the work for you, with one line on the transcript
saying it did. **What it starts is always a task and never a run** — this judge has no
planner to reach for any more, and no card either. It is *Why a task started on its own* in
*work that runs on its own*.

## What a run's planner and its nodes are told — does the run see what I said?

Yes, and every node of it does too.

**The planner** is asked once at the start and once per landing. What it reads, in order:
your own message under the heading `WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS` and the
line saying their words win where anything disagrees; then `THE GOAL:` — the goal the run
was started with; then anything you have steered it with since (which outranks its plan);
then what is done, the frontier, and the fuel gauge. The verbatim part is taken by aforge
from the conversation, not written by any model, so a planner cannot paraphrase away a
requirement it never had to copy.

**Every node** opens on one message, and it begins with the same two things — your words,
then the run's goal under `THE WORK` — followed by
`YOUR PART OF IT, and the whole of what you are answerable for:` and the node's own goal
from the planner. Then its prerequisites' digests, its write scope or the read-only line,
and the ask for a short report.

That is deliberate: the planner cuts each node's goal out of its own reading of the run, and
a node that could see only that reading has no way to notice a requirement the reading
dropped. Both copies are bounded so a run does not pay for them once per node — your message
at 6000 bytes, the run's goal at 2000, each cut marked with `…`.

A run started with no person behind it — one resumed, one a test scripted — simply has no
verbatim part, and no heading over nothing.

The goal itself carries the rest of the contract, because a run has no separate deliverable
or acceptance field: what must exist at the end and how it is checked are written into the
goal, whose first line is the run's own header on its page — and is what the roster calls the
run until a **short name** lands for it. A run's row is named the same way a task's is: a
cheap `taskname` call turns a goal that is still a sentence into two or three lowercase
words, a few seconds after the run has already started. *Work that runs on its own* has it
under *Why my task is called something I did not type*.

**A run's goal is never shaped.** The typed door onto a *task* has a model write a fuller
brief around your words with a `DONE WHEN` line under it (*work that runs on its own*, under
*Why my task's brief is longer than what I typed*); the run door has nothing of the kind,
because the door that did that was `/task adaptive` and it is retired. What the planner
reads is the sentence you typed, minus the money clause and the model clause. So write the
done-condition into the sentence yourself.

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
Reopening the conversation does not start it again, and there is no way to ask for that.

What survives is the **record** of it — and, on the task column, its **rows**, redrawn
settled when you reopen the conversation. See *Where my run's rows went* below for what
they say and what they can no longer do. Every run takes a row in the project's task list
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
is left alone. And you do not have to wait for that session to be reopened to know: on the
task column and on `/history`, a row saying `running` is **drawn** as running only while the
window that started it is open and still holds it — otherwise it reads `incomplete`
straight away, whatever the file still says. Each node's transcript stays on disk at
`~/.aforge/v3/runs/<session>/<run>/<node>.jsonl` whatever happened, so whatever the workers
did get done is still readable.

## Where did my run's rows go — the run disappeared from the task column when I switched away or reopened the conversation

**The rows come back. Both ways.**

**Switching away and coming back** — to home, to another conversation, and back again —
redraws a live run whole: the run's own row, every node under it, and each one in the state
it is in *right now*, including `forming the work` if the run is still in its opening
minute. Nothing is lost by looking somewhere else, and nothing has to be re-asked for.

**Reopening the conversation tomorrow** redraws the run's rows as **history**. The run
itself does not come back — see *What happens to a run when aforge closes or restarts* —
but the rows do, settled:

- a node that **finished** comes back done, with its digest and what it spent
- a node that **failed** comes back failed, with what it said
- the run's own row and anything still queued or still moving come back **stopped**, and
  the row reads:

```
it ended when aforge closed; its journal is kept
```

**These restored rows are history the column keeps, not work.** Nothing on them runs.
Nothing on them can be stopped — `x` and the header's ✕ are not offered over a run that
ended with the last process, because there is nothing left to stop. They are not counted
in the running total on the status line, and no queued row among them will start. Opening
one still opens the run's page, and the page says `no shape published yet`, because the
frontier it would draw died with the process. Each node's transcript is still on disk at
`~/.aforge/v3/runs/<session>/<run>/<node>.jsonl`.

A conversation you have reopened several times keeps every run it ever started, in the
order they were started, each still a family with its nodes hanging under it.

## When a run is the wrong tool

- Work that can be done in the conversation is done in the conversation.
- One self-contained piece of work is a **task** (`propose_task`) — one brief, one branch.
- **So is wide work**, and this is the one people expect to be a run. A broad sweep, an
  audit across many packages, research across many sources: all of them are one task. It
  usually runs as one worker, but it is not fixed to one — a task that opens the material
  and finds many separate items in it splits into parts and stays to fold them back
  together. See *Work that runs on its own*, under *When a task turns out to be too wide for
  one worker*.
- A shape of work that will recur is a **sub-harness**: built once, saved, and offered
  again. See *Saved shapes of work*.

A run is for the one-off goal whose graph has to be planned before the work starts, and for
the plan you asked to see and steer — and you reach it by asking for one in those words, not
by giving aforge work and hoping. Several parts on their own are not the reason: those are
one worker that splits itself.
