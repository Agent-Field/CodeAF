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
worker in its own copy of the repository, and the first worker stays and folds their reports
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

The default tank is **$10.00** when nobody named a figure. **You cannot name one from a
conversation**, because a conversation cannot start a run at all — a run's cap is settled by
whatever started it.

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
  you used, or the things it says are absent are in the text you are about to read. The
  review was checked against the world and lost, and the answer is handed over as
  finished: `a review raised this: … — I've delivered as it stands, because what it asked
  for is already on disk under the name the request used.`
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
exit **2**, not 0. That is the whole difference: a refusal that was checked against the
filesystem or against the delivered text overturns the finding, and a refusal about where
the review got its words does not, because no ruling about a quotation makes missing work
appear.

Watching a headless run, the line names the finding first and the reason second:

```
gate: refused — The deliverable does not contain the code that writes feature_schema.joblib — what the review asked for next is not in the request
```

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
the run goes on to deliver, be reviewed, and land like any other.

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

**When it does not happen.** A project that declares no way to check itself is not checked
— there is nothing to run. A piece of work whose whole time budget is too short to hold a
real reading takes none at all, rather than spending an eighth of its life on a command
that would be killed before it said anything: below about eight minutes of wall, nothing is
measured. A tree nothing changed is not read a second time. In every one of those cases the
run behaves exactly as it would have without this, and says nothing about checks it did not
run.

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
