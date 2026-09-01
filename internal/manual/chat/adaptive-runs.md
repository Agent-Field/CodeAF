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

**A run is not how aforge works on wide things, and it is not something a conversation can
open at all.** Ordinary work — however broad — takes one road: one task, which hands its own
parts out once it has opened the material. There is no word, no command, no setting and no
tool that starts a run from a conversation; *How do I start an adaptive run* below is the
whole of that answer. What the rest of this page describes — the fuel tank, the roster tree,
the run's own page — is how a run behaves where one exists, and is left here because the
machinery is still in aforge. See also *Should this be a run, or one worker that splits
itself*.

## Should this be a run, or one worker that splits itself — when I use a run instead of a task, why didn't you start an adaptive run for this

**I cannot start one, and that is the answer to why I did not.** The tool that opened a
planned run is off my belt entirely — not refused, not gated: I do not have the verb — so
whatever you ask for, work that leaves this conversation leaves it as a **task**.

Work that is simply wide — a sweep across many files, research across many sources, the same
change over many separate items — starts as **one task**, and the worker hands the parts out
itself once it has opened the material and can see how many there are. Each part becomes a
worker in a working copy of its own, and the first worker stays and folds their reports
into one deliverable. That is *When a task turns out to be too wide for one worker*, in *work
that runs on its own*.

The reason is that nobody can see the parts from the request. A planner asked to cut up
"audit every package for this pattern" is guessing at how many packages there are and what
is in them; a worker that has just listed them is not guessing. Two roads meant the guessing
one kept being taken on width alone, so there is one road now.

**And there is no phrasing that gets you one either.** The sentence that used to open a run
outright — a message beginning `orchestrate …` — is an ordinary turn now, so asking for a
planner in so many words gets you the same thing asking for the work gets you: an answer,
and a task if what you asked for is work. *How do I start an adaptive run* has the whole of
it.

## How do I start an adaptive run — the words that used to open one, and why nothing opens one now

**You cannot start one from a conversation. There is no door left.** Not a sentence, not a
slash command, not a setting, and nothing I can reach for. This is not a refusal you can
argue with or a switch somebody turned off: there is no code that reads what you type and
starts a planned run, so there is nothing to say no to you either.

What closed, in order:

- **`run_adaptive` is off my belt.** I do not have the verb, so there is no phrasing that
  gets me to reach for a planner and no card where I offer you one.
- **`/task adaptive` is retired.** The word picks nothing; your brief is kept whole and one
  ordinary worker starts. *Work that runs on its own* has the line it prints.
- **No setting turns it on.** The `starting a task` row had an `adaptive` answer once, and
  it is gone; a profile still holding the word reads as the default.
- **And the typed sentence is gone too.** A message beginning `orchestrate …`, `adaptively
  work on …` or `run an adaptive run on …` opened a run directly until this build. It does
  not now. Those words are an **ordinary turn**: I answer them like any other message, and
  if what you asked for reads as work, a **task** starts the way it would for any other
  turn. Nothing is special-cased about them, nothing is stripped out of them, and no note
  says a door used to be there.

**So what happens to `orchestrate the migration off the old client`?** I read it, I answer
it, and the migration goes out as one task that hands its own parts out once it has opened
the material. If you named money or a model in the sentence — `with a $5 budget`, `with
opus` — those words are simply part of what you said; nothing reads a tank out of them any
more.

**What is left of a run, and where.** The planner, the fuel tank, the run's page and the
node tree are all still in aforge, and the rest of this page describes them, because work
that arrives as a planned graph still behaves exactly this way. What no longer exists is a
way into it from here. For a shape of work that recurs, the thing to reach for is a
**sub-harness** — built once, saved, offered again (*Saved shapes of work*) — and for one
job that leaves the conversation, a **task**.

## What one costs and what happens when the money runs out

Every model call in the run bills against **one tank**: the nodes, and the planner's own
calls too.

The default tank is **$100.00** when nobody named a figure. **You cannot name one from a
conversation**, because a conversation cannot start a run at all — a run's cap is settled by
whatever started it.

- At **80%** the run says so, once.
- At **100%** it **pauses**: whatever is in flight is allowed to finish, nothing new starts,
  and a question is raised on the run's page.

The question has exactly three answers, and it says how it is answered — walk the rows
with `↑`/`↓` (the bold row is the one enter takes), press enter, or click a row; typing
does not answer it, it steers the planner:

```
? out of fuel · $100.40 of $100.00
▌ add $5
  finish with what we have
  stop
  ↑ ↓ pick · enter answers · or keep typing to steer the planner
```

The top-up offer is **half the tank you already approved**, in whole dollars and never
less than one — a $100 run is offered $50 more, a $2 run $1 — so a big run is not begged a
dollar at a time. Taking it raises the cap by that amount and the run carries on. `finish
with what we have` skips to the write-up over the results that exist. `stop` settles the
run and keeps what finished. Each answers with a line saying what it did: `topped up; the
run carries on`, `finishing on what is already done`, `stopped; what finished is kept`.

A run is also bounded in time: **4 hours** covers every node, every planner call, and the
wait at the gate.

**And the whole session says so, to every other window.** While a run sits at its gate the
conversation reads `waiting on you` — on home, and on every other aforge open on the
machine — with `out of fuel · $100.00 of $100.00` beside it, the same line the run's own page
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
(`$0.87 / $100.00`) and is never dropped at any width. The page re-reads the run four
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
the goal, and every node row under it carries the **worker** class that does it — one
careful call deciding what happens, many cheaper ones doing it. It is the same class a task
handed off in conversation runs on. So opening a node and reading
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

## Why nothing seems to happen for the first minute of a run — nothing happens after a run starts, nothing happens for a minute, and what `forming the work` means

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

## it could not be read on that machine — a run's transcript over --host

A run's rows on the task page each name a transcript, and over `--host` that name is drawn
with the far machine in front of it — `spark:/home/you/.aforge/v3/runs/<session>/<run>/n3.jsonl`.
The card then peeks the end of that file over the connection.

Two things used to go wrong here and both are fixed:

- **Every adaptive transcript was refused.** The engine only opens a record under the roots
  it answers for, and it was holding one of the two: a run's node journals live under
  `~/.aforge/v3/runs` while conversations live under `~/.aforge/v3/projects`. So a card that
  named a perfectly readable `n3.jsonl` was answered `it could not be read on <machine>` —
  the file was there, and the boundary was wrong. The engine now answers for both roots.
- **A run's own row named a folder.** The root row of a run pointed at the run's *directory*,
  which is not a transcript and can never be opened, so the card drew a `transcript ·` line
  for it and then said the file was gone. It now points at the run's closing node,
  `…/<run>/synthesis.jsonl` — the call that reads what every worker produced and writes the
  run's report. A run still in flight has not written that file yet, and the card says so
  rather than pretending.

`it could not be read on <machine>` still has an honest use: the machine could not be
asked. It is not the same claim as the file being gone — every other fact on the card came
over on the world walk and is still true, and only that one read failed.

## When a turn should have been work — a run is never what starts

A turn answered in words when the honest answer was work does get caught: a second small
model reads it afterwards and can start the work for you, with one line on the transcript
saying it did. **What it starts is always a task and never a run** — this judge has no
planner to reach for any more, and no card either. It is *Why a task started on its own* in
*work that runs on its own*.

This is also what happens to a message that begins `orchestrate …`: it is read by the model
like any other sentence, and the judge behind it starts a **task** if there is work in it.

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
*Why my task's brief is longer than what I typed*); a run has nothing of the kind — the door
that did that was `/task adaptive`, retired, and the last door of any kind onto a run is
closed too (*How do I start an adaptive run*). What the planner reads is the goal it was
handed, word for word.

## What a run's workers may touch

Each node is a child agent working in your workspace, and two bounds are put on it.

- **A write scope.** The planner says which paths a node may write. That is enforced by the
  harness, not by asking politely in the brief: a `write`, an `edit`, or an `edit_video`
  cut, frame or score aimed outside the scope is refused with a result the node reads, so
  it can pick another file and carry on. Two things are deliberately not covered. `bash`,
  because a shell command's effects are whatever it did and a guard that pattern-matched
  commands would be claiming a guarantee it cannot keep. And a saving call that names no
  path at all — `edit_video` or `generate_image` with no `path` lands under a name of its
  own in this session's picture or video folder, which is nowhere the scope is about. A
  node with no scope is told it is read-only work.
- **A working copy of its own, sometimes.** When the planner judges a node needs isolation,
  it runs in a copy of its own under the session folder. If there is nowhere to put one, the node
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

## When a review says something is missing — why it says partial, not finished

Before a run's answer is handed over, one review reads it against your own request and
asks the only question that matters: would you accept this as done? Its default is to
pass. When it fails, it has to name the missing thing concretely enough that a worker
could close it from those words alone.

A named gap buys **one revision round** — the same work again with the review's words in
front of it — and, if the gap survives that, the work that closes it, planned and queued
like any other piece. You see one line: `a review found this still missing: … — finishing
that before delivering`.

**Three things can stop that round being bought, and only one of them means the review was
wrong.**

- **It is already there.** The file the review says is missing is on disk under the name
  you used. The review was checked against the world and lost, and the answer is handed
  over as finished: `a review raised this: … — I've delivered as it stands, because what
  it asked for is already on disk under the name the request used.` Where a run wrote no
  files at all — a question answered in prose — the answer itself is what it left behind,
  and things named in the text you are about to read count the same way. **Where the run
  did leave files behind, what the answer SAYS about them is never evidence:** a summary
  claiming work was done is the thing being checked, not the world it is checked against,
  and a run once shipped "all three files are implemented and committed" over a file that
  had never been written.
- **The review asked for something you never asked for.** Nothing is redone, because a
  round bought against a standard aforge set for itself cannot converge on anything. You
  are told, in the delivery: `— I've delivered as it stands, because what the review asked
  for next is not in the request, and I don't redo work over a standard the request never
  set. Say the word and I will.`
- **Nothing more could be started.** No money, no rounds, no room, or not enough time left
  on the run to finish a repair: `I'm handing this over with a reservation — a review found
  this still missing: … I've taken it as far as repair takes it: there is not enough time
  left on the run to finish it.`

**Only the first of those is a finished run.** The other two hand over something the run
itself says is short, so the work lands as **partial** — headless, `aforge do` leaves with
**exit 2: the run handed over less than it promised, and the finding it is short of is
named on the last line.** That is the whole difference: a refusal that was checked against
the world overturns the finding, and a refusal about where the review got its words does
not, because no ruling about a quotation makes missing work appear.

Watching a headless run, the line names the finding first and the reason second:

```
gate: refused — The deliverable does not contain the code that writes feature_schema.joblib — what the review asked for next is not in the request
```

## The last line of a headless run — `partial`, and why the run says it is short

An exit code is what a script reads and it is the one thing you cannot see in a terminal.
So a run that ends short says it out loud, once, last, under the ✓ rows:

```
partial — gate: The deliverable does not contain the code that writes feature_schema.joblib (not repaired: the same words were already worked on once)
```

The finding comes first because the finding is the news; the reason is why nothing further
ran — a refusal in the review's own words, or `nothing further was started`. A run that
prints no such line finished whole, and `aforge do` left with 0.

One shortfall has no repair to explain, and drops the `gate:` and the reason with it: a run
whose project declares a suite that could not be read. Nothing was refused and nothing was
short — the check itself never happened, which is why the line names the measurement rather
than a finding:

```
partial — nothing in this project's verification could be read: `npx ava --tap` was killed at its ceiling of 1m53s without finishing
```

A gap that a repair round closed is **not** short. You see `gate: pass — <what the first
reading said was missing> — closed by the repair`: the work was redone and re-read, checks
that were passing before were checked again, and the exit code is 0. Only a finding that
was still standing when the run stopped makes it partial.

A run that stopped because it had stopped getting anywhere says that instead, and it says
it in front of any review finding — the review says what is missing, this says why nothing
more was bought to go and get it:

```
partial — no relevant progress in 3 rounds; last change: src/grid.ts
```

`last change` is the newest file the run changed that the request was actually about; when
there has never been one it says `nothing this job is about has changed`. A run stopped
because its wall could not hold another round of work says `partial — no time left for
another round of work` — which is a run choosing to stop while there is still time to
check what it did, not a run that ran out of time.

## When aforge decides there is nothing left to do — and when it may not

Before buying more work, aforge asks whether everything the request is judged on is
already covered by work that has landed or is running. When the answer is yes, it stops
and says `everything this job is judged on is already covered by work that has landed or
is already running — handing over what's done`.

**That answer never wins over something measured.** It is a reading of the plan, and a plan
can look complete over a tree that is not: checks can be failing, a behaviour the request
asks for can have no check at all, a change can have deleted public names or left the
callers of something it rewrote behind. While any of those stands on the job, the work
carries on regardless of what the coverage reading said — and it makes no difference
whether the round is a repair, a continuation of work that ran out, or a reaction to a
result that contradicted the plan.

What still stops the work is the rest of it: the round cap, a job that has stopped changing
anything, one finding worked on twice, and the wall.

## When one thing is worked on twice and still stands

A review finding buys work. **Each thing it names buys at most two rounds.** If the first
round ends and that thing is still missing, a second round is bought; if that one ends and
it is STILL missing, nothing more is bought for it — it is handed over named instead of
repaired, and the last line counts and names what stood:

```
partial — 1 behaviour stood through 2 rounds of repair: Count a circuit failure for body-read/stream-consumption errors
```

**It is counted thing by thing, not review by review.** A review usually names several
things at once and the list it names changes from one reading to the next — two behaviours
this time, five the next, with one of them in both. What matters is each behaviour, each
check, each deleted name on its own: it is the same one when it is the same KIND of
finding — a check that used to pass and now fails, a check that was removed, a public name
the change deleted, a behaviour nothing exercises, callers a rewritten definition left
behind — spelled the same way. A round is bought as long as the review still names
something that has not already had its two rounds; the ones that have ride along without
buying anything.

The wording you see on the work's own record when nothing in the review can buy a round is
`everything still missing here has already had two rounds of work aimed straight at it, so
this is handed over with it named rather than repaired`.

## When the wall gets close — the work is checked before the clock stops

A job that has not finished when its wall arrives is a job nothing ever judged: the review
happens when a job finishes, and a job cut off mid-step never finishes. So aforge stops
buying new work while there is still room to check what it did. Two things do it, and they
answer the same question from opposite ends:

- Work that would need another round is not bought. The line is `partial — no time left
  for another round of work`.
- A job that has never been reviewed at all is wound up: the parts that have not begun are
  retired, the parts in flight are asked to stop, and the job is handed over to be checked
  as it stands. You see `handing this over to be checked while there is still time`, and
  the parts that were retired say `there was not enough time left on this run to finish
  this, so it was handed over to be checked as it stands`.

How close is "close" is measured on the errand itself — the pace this job's own rounds have
kept, plus what this project's own checks cost to read on this machine. It is not a fixed
number of seconds, and a job that has not yet shown a pace is never wound up early.

## When the repair only rewrote the summary — a round that changed nothing on disk

Sometimes the work has already landed and what is wrong is the account of it, so instead
of running the worker again aforge rewrites the summary over the change that is already
there. Nothing runs; only the words are new. **That kind of round can close a review's
finding about the summary — a wrong description, a missing explanation — and it is not
allowed to close one about the work itself.**

The files on disk are stamped before the round and again after it, and if nothing moved,
a finding about a file, about a check the run ran, or about a change no test has been run
against still stands. The run ends partial and the last line says which round it was:

```
partial — gate: The missing element is the substance of the work (not repaired: the repair rewrote the account and changed nothing on disk)
```

A finding that only ever concerned the wording still closes this way, and so does anything
where the project's own checks ran on the change and came back green.

## When the review itself could not be read — unchecked, and said so

The review is a model call like any other, and a model can answer with something that is
not an answer: prose where an object was asked for, or a reply cut off before it finished.
When that happens the answer is asked for again, once, with the format stated and the
offending reply quoted back. If the second attempt is unreadable too, **the review did not
happen** — and a run whose own check did not happen is not a run that passed its check.

The work is still handed over: it was done, and it is yours. What it carries is a
reservation naming what is missing, which is the check and not the work:

```
I'm handing this over unchecked: the review of it could not be read, so nothing has confirmed this is what you asked for.
```

Watching a headless run you see it as the review's own line:

```
gate: fail — the review could not be read, so this delivery was never checked
```

**The run lands partial.** `aforge do` leaves with exit **2**, not 0. This used to be exit
0 with the work reported as done — the review was treated as having no opinion rather than
as having failed to give one — and that is the single difference. Nothing is being said
about the work; nobody looked at it.

## When a model's answer is cut off or comes back as prose — the ↻ lines

Every place aforge asks a model for a structured answer — planning a job, compiling your
request, reviewing a delivery — the reply is given room sized to what was asked for, and
repaired when it does not fit. Two things go wrong and both are said out loud:

```
  ↻ plan: answer cut at the ceiling — continued
  ↻ gate: the answer was not readable — asked again
  ↻ compile: the answer was still not readable — giving up on it
```

- **cut at the ceiling** means the model ran out of output room mid-answer. The half that
  arrived is kept and the rest is asked for, joined onto it. It is not bought a second
  time; a re-ask spends the same tokens to hit the same wall.
- **not readable** means nothing usable came back at all. The model is asked once more with
  the shape stated and its own reply quoted, so it can correct rather than start over.
- **giving up on it** is the third line and the only one that changes the outcome. What
  happens next depends on which call it was: a review that gives up hands the work over
  unchecked, above; planning that gives up runs the job as one piece of work, below.

The word before the colon is the call: `plan`, `gate`, or `compile`. Every one of these
lines is also kept with the job, so a run can be read back afterwards and its repairs
counted.

## When the planner cannot lay a job out — one worker instead of nothing

Planning is itself a model call, and it can fail before any piece of work exists. It used
to end the run there: no tasks, nothing attempted, and an error where an answer should be.

It now falls back to **the smallest plan there is** — one worker, given the whole compiled
goal — which is the same shape every single-step ask already gets. You see:

```
  the planner could not lay this out — running it as one piece of work
```

One worker on the whole job is worse than a plan and enormously better than nothing, and
the run goes on to deliver, be reviewed, and land like any other — **with the acceptance
checklist it already read off your request**, so a job that fell back here is still held to
the behaviours you stated rather than only to how its answer reads.

## What the review is allowed to hold you to — the request, the method, and what was promised

A review may only ask for things that were promised **before the work started**: your own
request, the working method that kind of work was held to, and what the plan itself said it
would produce. Those three cannot have moved in response to what the work turned out to be,
which is what makes them safe to buy work against.

A review's finding is accepted when it quotes one of those — **including a quotation that
skips a middle**, with `...` between two parts of your sentence, which is how anybody
quotes a long request — or when it names a file one of them names, under either spelling,
or when every distinctively spelled name in it is a name one of them uses. So "the code
that writes feature_schema.joblib is not there" is held to, even though it quotes no whole
clause of the request, because every name in it is yours.

An ordinary word is not a name. A finding built out of your vocabulary but asking for
something you never asked for — "March refers to any calendar year present in the data" —
names nothing distinctive, so it is refused, and the run tells you so rather than quietly
redoing work against it.

## When the work broke something that was working — checks that passed before and fail after

A run working in a repository reads that repository's **own** way of checking itself — the
verification command declared in its `package.json` scripts, its `Makefile` targets, its CI
workflow, `go.mod`, `Cargo.toml`, or a Python project with a suite — and takes two readings
of it: one before it touches anything, and one after, when the tree actually changed.

**It reads the runner, not the script around it.** A lifecycle script that lints and
typechecks on its way to the suite is followed into its own body — through a Makefile's own
variables and past `npx`, `pnpm exec` or `poetry run` — and the runner found there is asked
for its own machine-readable account: JSON from vitest, jest and `go test`, the
line-per-check summary from pytest, TAP from mocha. Your flags on it are kept. This is why a
formatting complaint no longer reads as a broken suite; it used to exit first, and the
report was "`pnpm test` exited 1 and named 0 checks" of a suite that was green. Where that
invocation names nothing — a flag aimed at a plugin this machine lacks — the runner is asked
again in its plainest form, inside the same budget. A runner aforge has not met is invoked
as your project declares it and read as plain text, as everything was before.

**It reads the package your work is in, not the whole workspace.** A repository that
declares what it is made of — `workspaces` in a `package.json`, `packages:` in
`pnpm-workspace.yaml`, `lerna.json`, `[workspace] members` in `Cargo.toml`, `use` in a
`go.work`, or a manifest in each package under a turbo/nx/rush root — is read in the package
the work touched, found by the nearest manifest above the files that changed. A root command
whose whole job is to fan out over packages says nothing about any of them: measured at one
project's own base commit, the declared root command exited 1 in five seconds naming no
check, and the same reading taken inside the package named 173.

**And it reads the checks next to your change before it reads the whole suite.** Two things
count as next to it, and both are relationships rather than resemblances: the test file
**named after** what you touched, by the runner's own convention — `test_thing.py`,
`thing.test.ts`, `thing_test.go` — together with the test files sitting beside it; and the
test files whose **import lines** name it, including through a package's own front door,
because `from textual.widgets import RichLog` is an import of `_rich_log.py` and the
package's `__init__.py` is the only thing that says so. Only import lines are read and only
whole names match, so `Log` is never `Logger` and `log` is never `dialog`. **Your request
does not have to spell a path**: a name it uses that this repository has a file for — an
`IntersectionObserver`, a `RichLog` — is resolved to that file, whole, and that is what
decides which package is read. It resolves to **every** file of that name rather than the
first one found, so a repository that keeps a documented example beside the real widget does
not send the reading to the example. And when your request writes a name both ways — `Log`
and `RichLog` in one sentence — the short one counts as a name too, so a change to both is
read on both sides. A short word you only ever write on its own is not treated as a name.

If the selection still comes to more than an eighth of the suite it is not a scope any more,
so it is cut back to the checks your change is actually in. The whole suite is what is tried
if that names nothing. A reading is scoped before it
is bounded, because a budget cannot rescue a measurement of the wrong size: one project's
whole suite is 3,422 checks and takes over thirteen minutes, against the five and a half
minutes its run could afford, so the only reading it had was one that could never finish.
The line in the record says which it was — `scope: touched packages (3 files)` or
`whole` — and the two are never compared with each other, because a whole reading minus a
scoped one would report every check that was not selected as one that had disappeared.

**And the checks beside what you changed are read on the second pass.** The scope of the
first reading comes from your request, because when it is taken nothing has changed yet — and
a request is not a change. One run asked for two widgets by name, only one of the two names
matched a file, and the test file sitting next to the other one was never read on either
side. So the second reading takes the checks next to your change as well: the files the run
actually touched, put through the same beside-and-imports rule. If your request matched
nothing at all and the whole suite ran out of time, the second reading is aimed at your
change instead of at the same ceiling — you get the checks for the work that was just done
rather than nothing.

**And the checks the run wrote itself are always read.** A scope is worked out from your
request, before any work exists, so it can never contain a test file the run goes on to
create — one run wrote about forty checks into a new file and every reading it took named
the same two checks the old file already had, so nothing it had just written was ever
measured and nothing it had just covered stopped being reported as covered by nothing. The
second reading therefore takes the checks next to your change plus **every test file the run
left behind**, read off the tree rather than off the run's own account of itself. It costs
no extra reading, and a new test that is red is not reported as something the work broke —
a check that did not exist before cannot have been passing before. It is still a leaf that
has not finished, and the acceptance line is where you see that.

**And the record always says what was read, whoever read it.** The two readings above
are taken by the worker that does the work; when the work went to a worker that does not
photograph, the check at the end of the job takes its own reading of the tree it is judging.
Either way there is a row in the run's record saying what ran, what it named — or, when
there was no reading, which of the reasons it was: your project declares no way of checking
itself, there was no time left to size a reading against, or there was nothing to read.
Silence used to mean all of those at once.

**A suite that never got as far as running a check is not a suite with a failing check in
it.** When your project's runner is asked for a machine-readable report and prints none — an
import that will not resolve, a config that will not load — the record says its suite failed
to collect and quotes the runner's own words, rather than inventing a result out of the
error text. Nothing is compared against a reading like that in either direction, so a broken
import can never be reported as work you broke.

**A reading that is cut keeps what it named, on both sides.** A command killed at its ceiling having
already printed some of its checks is a partial reading: it can say a check for something
exists, and it is never used to say the work broke something, because the checks it never
reached are missing from it for a reason that has nothing to do with your change. That holds
for the reading taken after the work as much as for the one before it — and when a command
ran but printed nothing a check could be read out of, the record says that, rather than the
same words it would use for a project it could not read at all.

**And a reading cut at its ceiling is taken again, smaller.** Every other reason there is no
reading is a fact about your project or about the time available, and a later round inherits
it rather than paying to learn it twice. A scoped reading that ran out of time is not one of
those: it is a fact about a size aforge chose, and the run now knows how fast this project's
checks go — so the next reading is the checks your change is in, rather than the same
ceiling again.

**Repair rounds are measured against the tree the job started with.** The first reading
belongs to the whole job, not to one attempt: a second or third round inherits it rather
than photographing a tree its own earlier round has already changed. Without that, a check
broken in round one is red in round two's baseline and is never reported again. It also
means a repair round runs the suite once rather than twice.

**A behaviour you asked for that no check covers keeps the run from ending clean.** The
check at the end of a job maps every behaviour your request states against the checks that
exist, and it holds that mapping to the same standard it holds a quotation to: a check
covers a behaviour only when the check itself — its name, or the file it lives in — spells
the names your sentence spelled. A judge saying so is not enough. What is left over is
named, it buys a repair round, and while any of it is still open the run ends `partial`
with the count in the last line, whatever else was settled or overturned along the way.

**And a public name your code had before the work and does not have after it is a finding
at every check of the job, not only the one that lost it.** A suite only covers what somebody wrote a check for, so it can come back greener on a
change that broke every caller outside it: one job deleted eight public attributes off a
class, the project's own checks went from two passing to fourteen, and all twenty-four of
the hidden ones it was really measured on failed on their first line. So the names are read
as well as the checks — what your source files publish before the work and what they publish
after, for the files the job actually changed. Python, TypeScript, JavaScript, Go and Rust, each by its own
idea of public: an underscore, `private`, an unexported name and a non-`pub` item all mean
the author called it internal. It is read conservatively and says nothing it is not sure of.
A rename reads as the old name going, which is what everyone calling it sees.

**And a name your code kept, whose definition the work rewrote, is read against everything
that still uses it.** Deleting a name is only half of what breaks callers; the other half is
keeping the name and changing what stands behind it, and no suite and no list of names can
see that. One job rebound a module's `configs` from a dictionary to an object of its own
with no item assignment on it — same name, checks still green, every one of the twenty-four
hidden tests failing on the first line that wrote into it. So the same two readings that
say which names went also say which names STAYED and are no longer the same thing — a
definition whose own text your project spelled one way before the job and another way
after — and every place in your project that still uses those names is found and counted
by what it DOES with them: calls it with so many
arguments, indexes it, assigns into an index, reaches a member off it, iterates it. That
goes in front of the check at the end of the job with the file, the line number and the line
itself, and it may refuse the work on one of those lines — `configs changed and its 14
consumers still use it as subscript-assign: tests/test_igel.py:92, …`. It is measured
against the tree as your job FOUND it, not as the last attempt left it, so it is the same
finding at every check of that job until the work puts things back. It is read
conservatively, from the shape of the code and never from what anything means: nothing here
resolves a type or follows an import, a name it cannot look for whole has no consumers
rather than the wrong ones, and prose in a document that happens to spell the name is not a
usage site.

**And a name your code READS that nothing in your project defines is caught before anything
runs it.** The other two readings both ask about a name that used to be there. This one asks
whether a name is there at all: an attribute a class reaches for on itself that no code
anywhere assigns, and an import that asks one of your own modules for a binding that module
does not define. One job rewrote a module so that three paths became local variables inside a
function, left an `import` of one of them standing in another file, and every one of the
twenty-four checks it was really measured on failed on that single line — while the only
thing the run could say was that its own tests were red, never which name they were red
about. So it says the name, the file and the line, and where it looked: `this work reads
names nothing defines: temp_post_req_data_path (igel/servers/fastapi_server.py:1,
igel/configs.py binds no top-level temp_post_req_data_path)`. Python, TypeScript and
JavaScript; Go and Rust are left to their own compilers, which answer this better. It is
read from your files with no type checker and nothing that resolves a type, and it is
deliberately timid: a class that sets its attributes through `setattr`, a class built on a
base your project does not itself declare, an import from a package outside your tree, a
module that re-exports with `*` — each of those makes the question unanswerable rather than
answered, and it says nothing at all. A name assigned ANYWHERE in your project counts as
bound, including by a test. It buys a repair round, it stops the work landing clean, and the
next check of the same job re-reads the tree — so binding the name closes it.

**A check the run wrote itself and did not get passing is a different finding from a check
it broke.** Only a check that was in your project's roster before the work, and green there,
can be reported as broken by it. A red check that first appears after the work is the run's
own unfinished business, and it says so — `the checks this work wrote fail: …` — because a
run told it damaged your repository and a run told its new tests do not pass will do two
different things about it. Both stop the work landing clean and both buy a repair round.

A check that passed before and fails after is a finding the run raises about itself, and it
is a blocker. Nothing is weighed about where it came from: you never have to ask for your
repository to keep working. It reads:

```
This work broke checks that were passing before it: tests/test_igel/test_feature_schema.py::TestFeatureSchema::test_fit_writes_schema, … They were measured twice with the project's own command, before the work and after it.
```

That buys the repair round like any other finding, and if nothing closes it the run lands
partial rather than finished. Checks that were **already** red before the work began are
named separately and are never held against the change.

**Why this exists.** A worker's own new checks are the one signal that structurally cannot
see a breakage, because the worker wrote them. Three measured runs shipped a change that
deleted an attribute the repository already had, watched their own narrow checks stay
green, and reported success while every one of the repository's own checks failed on setup.

**When there is no reading — and how you find out.** A project that declares no way to check
itself has nothing there to read. Work whose whole time budget is too short to hold a real
reading takes none, rather than spending an eighth of its life on something cut off before
it says anything: below about eight minutes of wall, nothing is measured. A suite too large
for that eighth is begun and cut off at its ceiling. A tree the job has not changed is not
read twice.

**Each of those is recorded with its reason and the exact invocation**, so work that
measured nothing is legible apart from work whose project had nothing to measure — they used
to be the same silence, and one of them costs an eighth of the wall. The reason is settled
once per job, so a repair round inherits it rather than paying again. In every one of these
cases the behaviour is what it would have been without any of this, and nothing is claimed
about checks nobody read.

## What "acceptance" means — the checklist read off your request before the work starts

Before anything runs, your request is read once for the **behaviours it states** — a rule
the result must follow, a case it must handle, a transition it must make, an outcome it
must produce. Each one carries the words of yours it came from. That list is the acceptance
checklist, and if a headless run has one you see it once, near the start:

```
  acceptance — 6 points from the request  14s
```

The list is held by the review and **never shown to the worker**. A worker handed the list
of things it will be checked on writes checks for the list and nothing else, which is the
whole problem this exists to fix.

Nothing can get onto the list that is not yours. Every point has to quote your request, by
the same rule a review's finding does — a verbatim span, a quotation that skips a middle, a
file you named, or the distinctively spelled names you used. A point this program wrote for
itself is dropped before the review ever sees it. And the list can never be longer than
your request has clauses — text either side of a full stop, semicolon, colon, question or
exclamation mark: past that it has stopped describing what you asked for. It counts clauses
rather than lines because one line of yours often states several things at once, and a
checklist that read "defaults are threshold = 5, cooldown = 30000, halfOpenMaxRequests = 1"
as one behaviour had nothing fine enough to match a check to.

**When there is no list.** A request that states no checkable behaviour — a question, a
lookup, a piece of writing — has no checklist, no line is printed, and the run behaves
exactly as it would have without any of this.

## When nothing checks what you asked for — "no check exercises …"

At the end, the review asks one more question of every verdict it reaches — whether it was
about to accept the work or has already found something else missing: for each behaviour on
the checklist, **is there a check that would fail if this were absent or wrong?** Where the
review has already named a gap, both gaps travel together, so the repair round is told about
each of them.

It answers that from two places, and the worker's own account of its checking is not one of
them. It reads the check declarations in the change itself, and it reads the identities the
project's **own** verification command printed when it ran. A sentence in the answer
claiming every check passes is weighed as prose: those are checks the same worker wrote,
and counting them proves nothing about what you asked for.

A behaviour nothing exercises is a finding, and it reads:

```
The request asks for behaviours that no check exercises. Nothing in this project's own verification would fail if each of these were absent or wrong, so nothing that has been run says whether the work does them:
no check exercises: A half-open probe holds its slot across internal retries
no check exercises: A parse or hook failure is not retried by status-based retry logic
Write the check for each, and make it pass.
```

Like a broken check, nothing is weighed about where it came from — it is a measurement of
the repository rather than a reading of your words — so it buys the repair round straight
away, and the round is asked to write the check and make it pass. It buys that round **even
when the review's own finding is refused**: a ruling about where a review got its words says
nothing about a behaviour nothing exercises. If nothing closes it the run lands partial
rather than finished.

Watching a headless run, it is one line under the verdict — the first behaviour named, the
rest counted:

```
  gate: fail — The deliverable is a report about the work rather than the work.  14m39s
  no check exercises — A half-open probe holds its slot across internal retries — and 2 more  14m39s
```

## When a check names what you asked for and asserts nothing about it

A check can run a behaviour and weigh nothing about it. One run was asked that
`RichLog.write(expand=True)` keep its full-width rendering; it wrote a check that called
`write("short", expand=True)` and then asserted only that the widget had more than zero
lines. The check was real, it ran, it passed, and it would have passed just as happily with
the behaviour broken.

So there is a second question, asked of every pairing the first one accepts: **do the
check's own assertions name any of the identifiers your request spelled?** Those
identifiers are read out of your words by shape — a name with a dot, an underscore or an
interior capital in it (`is_following_end`, `RichLog.write`, `max_scroll_y`), and a name
you bound to something (`expand=True`, `follow_end(animate: bool = False)`). The
assertions are read out of the file the same way: `assert` and `pytest.raises` in Python,
`expect(...)` and `assert.*` in JavaScript, the `if` beside a `t.Fatalf` in Go, `assert!`
in Rust. Setup is not an assertion, so a name that appears only in the call is a name the
check mentions rather than one it checks.

Where no assertion names any of them, the behaviour stays open and reads:

```
1 behaviour the request states is asserted by no check. A check names each of these and runs it, and then asserts nothing about the identifiers the request spelled — so it would pass whether the behaviour is right or wrong:
asserted by no check: RichLog.write(expand=True) no longer preserves full-width justified rendering — observables never asserted: richlog.write, full-width, expand
Write the check for each, and make it pass.
```

Under a headless run it is its own line, `asserted by no check — …`, and it buys the
repair round exactly as an unexercised behaviour does.

**Your own words count as identifiers where the tree agrees.** "the vertical scrollbar
position" names `ScrollBar.position` — the project's own public names are read as a
vocabulary, and consecutive words of yours that spell the words a name is built out of name
that name. Two words at least, and only names that have an owner: one word is a word, and
"after users scroll up" is not a reference to a `ScrollUp` class that happens to exist.

**Every one of them has to be asserted, not just one.** A check that weighs the scroll
position and never reads the scrollbar has answered half of what you asked, and the line
names the half it missed: `observables never asserted: ScrollBar.position`. Where a name
you wrote is qualified, a check satisfies it through the member — you write
`ScrollBar.position`, the check writes `bar.position`.

**Every silence here favours the check.** A behaviour of yours with no identifier in it —
"it must post only when the boolean actually changes" — is asked nothing; only checks
the run itself wrote are read; a file in a language this program has no reader for, or a
check whose declaration it cannot find, is left alone. Hyphenated English — "full-width",
"half-open" — is not a name unless you wrote it as a selector like `#follow-log` or the
project declares it, and a bare class name is what a check builds rather than what it
weighs.

**The question is asked again on every round, and the answer is the job's.** The checklist
was read off your request, and every round of the job has the same request — so a repair
round whose own plan carries no checklist inherits it rather than asking nothing. What the
last measurement found nothing exercising is carried the same way: **a behaviour measured
once as unexercised stays that way until a measurement says otherwise**, so a round whose
worker took no reading cannot quietly drop it. The set only shrinks on evidence — a round
that wrote the missing checks grows the project's own roster, and the next mapping is what
notices. The finding's last line is the score, over both halves: `3 of the 17 behaviours
this request states still have no check that asserts them.`

**Why this exists.** Two measured runs handed over wrong answers at exit 0 because the
review believed the worker's own sentence about its own checks. One claimed all 56 of them
passed and failed 6 of 47 hidden ones; the other claimed 31 of 31 and was one hidden check
short of a complete solve. In both, the behaviours that failed were stated
in the request and exercised by nothing the worker wrote.

A reading that **ran and named nothing** is still a reading: the project was asked how it
checks itself, it answered, and nothing it printed exercises anything you asked for. That is
the finding above, for every behaviour on the list.

**When it does not happen.** If the project declares no way to check itself and the change
produced no readable diff, nothing can be matched, and the answer is handed over as
finished rather than failed — nobody looked is not the same as something is wrong; the run
says so rather than passing in silence. Behaviours you stated on one line are gathered into
one thing to go and check, and eight of those are named at most; the rest are counted.

**But a project that HAS a suite nobody could read is a different answer.** Where the
project declares a way of checking itself and this run could not read it — the command was
killed at its ceiling, or the run's time was too short to hold a reading — the review has
been left with nothing but the answer's own words, and a run does not call that finished. It
lands partial, exit 2, and the last line says so:

```
partial — nothing in this project's verification could be read: `npx ava --tap` was killed at its ceiling of 1m53s without finishing
```

The two silences are told apart deliberately. A project with nothing to read leaves the
question unanswerable and finishes; a project whose suite could not be read leaves it
unanswered, and that is a fact about the run rather than about your request. Where no worker
took a reading at all, the review takes one itself before it decides, on the same share of
the time everything else here is held to, and remembers it for the rest of the job.

## When a check the work deleted stops existing

Taking out the check that is failing is the cheapest way there is to make a suite green, so
that is checked too. A check declaration the change **removes**, and a check the project's
own command reported before the work and did not report after it, are both findings:

```
This work removed checks that existed before it: keeps the half-open slot across internal retries. A check that was there and is not was deleted, renamed or skipped; whatever it was holding is now held by nothing.
```

A check that merely moved between files, or was re-indented, is not this: it is added and
removed in the same change and cancels. And a run that reported no check identities on
either reading raises nothing — plenty of verification commands say only `ok`, and a quiet one is
not a suite that lost everything.

**A check you REWROTE is not a check you removed.** Most runners spell a check's name as a
path — a describe chain, `file.py::Class::test_thing`, `TestThing/subtest` — and that path,
plus the code names the title opens with, is what the check is ABOUT. Replacing
`IntersectionObserver > observe() > Does nothing` with real checks under the same
`observe()` leaves nothing uncovered, so it raises nothing: the run reports it as replaced
and moves on. Only a check whose subject has no check at all after it is called removed.
Whether the checks that replaced it PASS is a separate question, and it has its own words —
`the checks this work wrote fail: …`. A runner whose names carry no path at all, such as
TAP's plain sentences, has nothing here to read, and its checks are compared by name exactly
as before.

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

A run is for the one-off goal whose graph has to be planned before the work starts — and
from a conversation you cannot reach one at all, by any words (*How do I start an adaptive
run*). So every one of the rows above is the answer here, and several parts on their own are
never the reason to look for a planner: those are one worker that splits itself.
