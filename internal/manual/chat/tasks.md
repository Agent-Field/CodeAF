# Work that runs on its own — tasks, and finding out what one actually did

## What a task is

A task is one self-contained piece of work handed off to run on its own while the
conversation carries on. It works in a copy of its own — a working copy of your repository, or
a copy of the folder when the work is about a folder — and reports back when it lands. It
never inherits the conversation: what it reads is one written brief — your own message, word
for word, then the work, what to produce and what done means, and a pointer at the
journal path and line of your original turn so it can read those words in full when the
restatement was cut. How that is assembled is on the *how tasks run* page, under *What the
task actually reads* and *Can the task see the original request*. The one exception is a
**quick task started with `inherit`**, which opens holding the conversation itself — see
*Can it keep what it read* below.

You can ask for the work in words, and the model grooms it and calls its `propose_task`
tool. You then get a card asking whether the work should go. The
model's window onto work that is running or already landed is its `tasks` tool; that is
also the door it uses to steer a task or to settle one, when you say so in conversation.

You can also start one directly with `/task <brief>`. **You are never asked a question by
that form, and nothing is waited for in front of it**: the task exists the moment you press
enter and one worker starts on your own words. A small judge reads those words for width
**beside** that worker. If it finds more than one job in them, the parts are weighed the
way any division is and, where they hold up, handed to other workers while the first keeps
going — they appear on the roster as rows of their own, and the worker is told what was
handed out. That is *When a task turns out to be too wide for one worker*, below. A no, a
timeout or an unreadable answer changes nothing: one worker is what is already running.

**A yes counts for the whole run, even where those first parts do not hold up.** The parts
are read together by a second model before anybody is given one, and it can say they are
really one job. That is a finding about those parts — drawn out of your sentence by a
reader that had not opened anything — and not about the work: nothing is handed out, the
one worker carries on, and it keeps `divide_work` for the rest of its run. So it can still
hand out the real parts later, once it has seen the material and can say what they are.

`/task solo <brief>` starts one worker and asks the judge nothing.

**There is no `/task adaptive` any more.** `/task` cannot open an adaptive run, and the
word picks nothing. Type it and your brief is kept exactly as you typed it — the word is
left where you put it rather than cut out of your sentence — the work starts as one
ordinary worker, and one dim line says so:

```
/task adaptive retired · the word stays in your brief, and the work starts as one worker that can split as it goes
```

**Every `/task` has its brief written beside its worker.** Your words are kept word for word
and a fuller brief is written around them — the constraints this kind of work needs, what
was decided on your behalf, and what done means — and it reaches the worker a few steps in.
It is the next section.

Every task carries a title, a short summary, the brief, and a done-condition — the command
that must pass, the behaviour that must hold, the output that must appear. The original brief and
done-condition stay on the record. A correction from you can change the effective
assignment through `revise_assignment`; the task keeps both your words and its revision.

You can keep working while a task runs. codeaf tells you not to wait for it: its report
arrives in the conversation when it lands.

**One other thing on the roster is a task, and it is not work in a copy of its own.** A sub-harness
being designed runs as a task too — same row, same room, same `x` — with its own phases
(`designing`, then `awaiting your look`) in place of the states below, and no branch, no
changed files and no merge, because it writes none. It is admitted without a countdown,
because the question about a design is the card at the end of it. The page on saved shapes
of work has it in full.

A task can also break its own brief into smaller tasks when it finds independent parts in
it, and those are rows of their own on the column, each under what it is doing (see *When a
task splits its own work*).

**And a copy of its own is the ordinary task, not every task.** The other kind is the
**quick task**: no copy of the folder, no branch, no check and no merge — it works in the
folder you are already in, and its last message is its answer. It is the next several
sections, starting with *What a quick task is*. Everything below about briefs, cards,
branches, checks and landings is about the ordinary kind unless it says otherwise.

## What a quick task is — a small job done here in the folder you are in, instead of on a branch, with no check and no merge

**A quick task is a task with everything optional taken off.** It is the other kind of
handed-off work, and it differs from an ordinary task in four ways:

- **It works where you work.** No working copy of your repository, no branch, no copy of
  the folder — the folder this conversation is standing in, the one you are looking at.
- **It starts the instant it is asked for.** No sizing call, no shaped brief, no card
  asking whether to run it, no countdown, nothing to accept: the row is simply there,
  already running.
- **Nothing checks it, and nothing lands.** There is no branch to merge and no check over
  the work. **Its last message is its answer**, and it arrives as the ordinary note a
  finished task sends back — into the conversation, or into the task that started it.
- **It carries a checklist.** One line saying what to do, and an ordered list of items it
  works through, ticking each as it goes.

Everything else is the task machinery you already know: a row on the column with a state
and a clock, a room you can walk into and read, `enter` to steer it, `x` to stop it — straight
away when it is the only running row, with `alt+t` first when it is not — its own spend, its own
model. The word for what it is, on the row and on the card, is `quick`.

Because it wrote in your own folder, its card carries no branch and no merge word — only
the files, if it changed any.

## How a quick task starts — there is no /quick command, codeaf starts one itself

**You cannot type a quick task into being.** There is no `/quick` command and `/task`
never makes one: `/task` is the ordinary road, with its width read and its brief written
beside its worker.
A quick task is started by the model, with its `quick_task` tool, when it judges that the
work in front of it is that shape — most often when you have asked for several small
things that can go at once.

So you ask in words. *"Read those four config files and tell me which one sets the
timeout"* becomes four quick tasks and a reply that reads their answers. Nothing asks you
to confirm: the rows appear, and the answers come back into the reply you are waiting on.

When it starts one, the model may hand the tool a title, the files it means to write, ids
it must wait for, and a model to run on. What it gets back is one line, at once:

```
quick task 7 started: compare the four configs · 4 items
```

That is the whole of the ceremony. If you want the other road instead, say so — *"do that
as a proper task"* — and the model proposes an ordinary task, card, branch, check and all.
*"Just do it quickly"* goes the other way.

## What quick means next to a task on the row — `quick · 2/4 · reading server.go`

Where an ordinary task's row shows the state it is in, a running quick task's row shows
what it is doing:

```
quick · 2/4 · read the timeout out of server.go
```

Three parts, joined the way every row joins its facts. `quick` says what kind of work this
is. `2/4` is how many of its items are ticked off out of how many it has. The rest is the
item it is on now. A quick task with no items at all — a one-line job — reads just
`quick`.

The counter moves when the worker ticks an item off, not on a clock, so a row sitting on
`1/3` for a while is a worker still on item 2 rather than a stalled one. When it ends, the
row settles like any other: `done` with the answer under it, or the reason it did not
finish.

## Steering a quick task — adding an item, changing one, telling it to skip the rest

The same as steering any other task: `enter` on its row opens its room, you type, and your
line reaches the worker at its next step. Nothing about a quick task's box is different.

What you are usually changing is the checklist, because that is a quick task's whole plan.
*"Also check the staging config"* adds an item; *"skip the third one and tell me what you
have"* takes one off the end. The worker keeps the list itself, with its `items` tool — it
ticks an item as it finishes it and appends the ones you ask for — and the row's counter
and next item move as it does.

**You cannot edit the list yourself.** There is no checklist to click, and no key that
ticks a box. The items belong to the worker; the words belong to you.

## Why it said waits for task 5 — two quick tasks that would write the same file

A quick task may name the files it intends to write. When it does, and another quick task
that is running or queued in the same folder has already claimed one of those paths, the
second one **waits for the first to finish** instead of both editing at once. The line the
model reads says which and over what:

```
quick task 7 started: rewrite the retry helper · 3 items · waits for task 5 (both claim internal/net/retry.go)
```

Nothing is refused and nothing is lost. The row is on the column from the start, waiting
with the task it is waiting for named on it, and it begins by itself the moment task 5 is
done. Quick tasks that claim different paths — or claim nothing at all — run at the same
time.

This is a promise made in advance, and naming nothing does not make a free-for-all: a
quick task that named no files may write anywhere in your folder, but **the first one to
write a file owns that file until it finishes**. Anything else aiming at the same path — a
second quick task, a task's worker, the conversation's own `edit` or `write` — is refused
with the holder named:

```
notes.md is held by task 7 (draft the note), so nothing was written.
```

The hold is on files it has **written**, not on files it only named. What naming files up
front buys is the *waiting* — the two never start together at all, so neither one has to
find out halfway through.

## Stopping a quick task — where the half-made work goes, and why there is no branch to go back to

`x` stops it, from its row or inside its room, and it asks before it does, the way
stopping any task does. What is different is what is left behind.

**There is no branch, so nothing is kept anywhere else.** An ordinary task you stop leaves
its work committed on its own branch for you to look at. A quick task was writing in your
folder the whole time, so what you are left with is your folder: the changes it had
already made, exactly as it left them, **possibly half made**. A file it was in the middle
of is as far as it got. Your own git is the undo — `git diff` shows the whole of what it
did, and `git checkout -- <file>` throws it away.

The same is true of a quick task that runs out of its rounds rather than being stopped by
you. Its report leads with `out of rounds — ` and quotes its own last sentence back,
because that sentence is the only account in existence of the change it was halfway
through.

## What a quick task cannot do — no check of its own, nothing to inspect, and it goes when the window goes

Six limits, and they are the price of there being no ceremony:

- **Nothing checks the work.** No check reads what it did against what was asked. A quick
  task is never `your call` and never waits for your approval — what you get is what it
  says it did, in its own last message.
- **There is nothing to inspect afterwards.** No branch, no copy of the folder, no diff of
  its own. The only record of what it changed is your folder and its room.
- **It does not outlive this window.** Work that has to keep going while the terminal is
  closed is an ordinary task. Its **row** does come back — a quick task you ran last week
  is on the column with its answer when you reopen that conversation, see *A quick task
  after a restart* below — but the working stops when the window does.
- **It cannot be continued.** `continue task 7` on a quick task is refused —
  `task 7 is quick, not a run that can be continued` — because there is no copy to pick up
  from. Instead, use **enter retry** on its task page when it is stopped or incomplete;
  it keeps the same task and checklist and works in the same folder.
- **It cannot be divided.** A quick task never splits itself into parts. Work too wide for
  one worker was never quick.
- **It cannot land anything.** No merge, no branch kept, no conflict to resolve — those
  words never appear on a quick task's card, because there was never a second copy of
  anything.

None of these are settings. A quick task that turns out to need any of them is a sign the
work wanted an ordinary task, and you can say so mid-flight: stop it and ask for a task.

## A quick task after a restart — the rows came back empty, my whole task column vanished after running quick tasks, does a quick task resume

**A quick task's row comes back like every other row, and so does everything beside it.**
Reopen the conversation and a quick task that finished is on the column `done`, with
`quick` on its row, its checklist ticked as it left it, and its answer on its card.

What each one does depends on where it had got to:

- **Finished** — it comes back exactly as it landed. Nothing runs again.
- **Running when the window closed** — it comes back `incomplete`, saying
  `the quick task did not finish before codeaf closed; whatever it wrote is in your folder`.
  It is **not** started again, and that is deliberate: it was writing in your own folder
  rather than a copy, so a second worker walking a half-done checklist over the top of the
  first one's edits would not be a resume. Under that sentence its card lists the checklist
  as it left it — `ticked 2 of 4: …` and `not ticked: …` — and the files it wrote are its
  changed list; `git diff` has the rest.
- **Still waiting its turn** — behind another quick task that claimed the same file, say —
  it never started, and it does not start now: the turn that asked for it is over, and work
  arriving on its own in a conversation that has moved on is not what anybody asked for.
  The row says so:
  `the quick task never started before codeaf closed, and it does not resume — ask for it again`.
  Asking again costs a sentence.

The line you read on reopening counts them under their own clause —
`recovered task graph: 3 done · 2 quick tasks did not finish` — never as `interrupted`,
because nothing about them resumes.

**If you remember a conversation reopening with an EMPTY column after quick tasks had run
in it, that was a fault and it is fixed.** A quick task is checked by nobody, so it is
written down with no `DONE WHEN` clause of its own — and the check that reads the record
back refused any row without one, which threw the whole conversation's work away together:
finished rows, running tasks, families and all. The rule still holds for every ordinary
task; a quick task is now allowed the blank it is supposed to have.

## Quick task or a proper task — why it went quick instead of a real task, why a survey did not get a branch, and how codeaf decides which road your work takes

The rule is written once, in the words the model itself reads:

> A task gets its own copy of the folder, is checked, and lands. A quick task works where
> you are and its last message is its answer. If you will read the result and carry on,
> it is quick. If it must be checked and merged on its own, or survive the window
> closing, it is a task. One edit, one read, one command is a step: do it yourself.
> Related steps that share what they learn are one quick task's items, not several quick
> tasks. Keep one small — a few files and a few minutes: reading is not progress, so
> six steps that only read end it.

**What decides is what happens to the answer, never how wide the work is.** Work whose
result comes back for the conversation to read and carry on with is quick tasks, one per
independent part, however many parts there are — a survey of four packages is four of
them, not one task with a branch. Work that has to be *checked and landed on its own*, or
to outlive the window you are looking at, is a task. Width only decides the shape of the
second one: a wide **change** is one task whose worker hands the real parts out from
inside once it has opened the material, and a wide **read** never comes down that road.

**And the model is told what each one costs**, rather than given a list of which kinds of
work go where. Its own instructions describe a quick task as a copy of its abilities
working where it stands and a task as a worker in a copy of the folder that is checked and
merged, and then the arithmetic: pieces it keeps cost their sum, independent pieces handed
out in one breath cost the longest of them. Everything on this page follows from that, and
so does anything this page did not think to list.

Three consequences worth knowing. **Small things still do not become work at all** — one
edit, one read, one command is done in the reply, and it was never a candidate for either
road. **Related steps are one quick task's items, not several quick tasks**: reading
four files to answer one question about them is one quick task with four items, because
the fourth read is worth more to somebody who has seen the first three. And **one quick
task is a few files and a few minutes** — see *How big one quick task should be* below.

You can overrule it either way in words, and the model follows.

## How big one quick task should be — a quick task that ran out of rounds, one that read twenty files and cost a dollar, why a big package became several

**A few files and a few minutes.** A quick task is one worker with one context and no
grooming, so the thing that ends it early is not a wall clock, it is running out of room:
a worker that only reads is not making progress by codeaf's own measure, and after a few
such steps in a row it is stopped and its row lands saying `out of rounds`.

That is a real failure and not a hypothetical. A quick task told to survey twenty files
and 8,600 lines read them whole into a context with no space for them, stopped on
`out of rounds — stopped: 6 steps without progress` after eight minutes, and cost over
half a dollar for an answer nobody got.

So a large package is **several** quick tasks of a few files each, or one quick task with
one item per small group — never one over all of it. And a quick task that starts quick
tasks of its own cuts them smaller still, because its own room is already spent.

## Why several things started at once — three quick tasks in one message, why it did not do them one at a time

**Because they did not need each other, and one after another is the slowest order.**
When a turn has independent pieces in front of it, codeaf starts them in the same breath
rather than in turn, keeps one piece for itself and gets on with it. You wait for the
longest piece instead of the sum of them, and the rail shows every one of them running.
That is the whole aim when work splits: the shortest wall time for the whole job, the way
a team of workers would take it, so however many independent pieces there are, they all
start together.

**How many of them run at once is decided by memory, not by a number here.** Each piece
that begins sets aside a footprint — one core's share of memory, or more where this
session's pieces were seen to need more — so a wide hand-out runs as many pieces as the
memory above `task.min_free_mb` can hold and leaves the rest queued, each row reading
`waiting · machine busy`. Those begin by themselves as earlier pieces finish; there is
nothing to do about it and nothing to come back for. how-tasks-run has the arithmetic.

What it will *not* do is watch them. Each landing writes one dim line in the
conversation and does not start a turn, so once nothing is left that is independent of the
work it handed out, the turn simply ends — a reply that sat there polling would have spent
your money to learn what it was going to be told anyway. If the pieces share what they
learn, they are one quick task's items instead, and they stay in order.
## Can a quick task start more work — quick tasks inside quick tasks, and the two bounds

**Yes, under exactly the bounds every task is under.** A quick task's worker carries
`quick_task` and `propose_task` on the same terms as any other worker.

**Depth is 3 levels.** The conversation starts work; that work may start more; what it
started may start more once again; the level below that may not. A worker at the floor
has neither tool on its belt — `quick_task` is withheld there the same way `propose_task`
is — so a child saying it cannot hand work out is describing a limit and not a choice.

**Fan-out is 20 pieces per parent**, counting quick tasks and ordinary tasks together.
That number stops a runaway; it does not ration breadth. How many actually run at once is
`task.parallel` and how busy this machine is. A worker asking for one more is told
`no: you have already handed out 20 pieces of this work, which is as many as one task
may.` and to do the rest in its own hands.

And a quick task takes a slot like anything else: if you have set `task.parallel`, quick
tasks queue behind it with everything else.

## A quick task started inside a task — a quick row appeared under my task, and who reads its answer

A quick task started by a task is a row of its own on the column, under what it is doing,
beside the task that started it; its room names that task in its breadcrumb trail. Point at
its row and the hint line reads the same `quick · 2/4 · …` it would read anywhere.

**Its answer goes to the worker that started it, not to you.** The note with its last
message in it is delivered to its parent, which reads it and carries on — the same road a
part's report takes. You see the row and can open its room, but the conversation is not
handed the answer; what reaches the conversation is what the parent task says when *it*
lands.

The other direction is the same shape: a quick task started by the conversation reports
into the conversation, and its note is what you read.

## Why my task's brief is longer than what I typed — the brief is shaped

A task you start with `/task` does not stay the sentence you typed. **Beside its worker** —
never in front of it — one model call reads your words and writes the brief the work is
held to: your request quoted word for word, then the things a worker alone with the job
needs settled — what kind of work this is, who the output is for and what makes it good to
them, the ways this particular kind of work goes wrong and the conditions that forbid them,
anything ambiguous decided one way with the assumption stated. It also writes a separate
done-condition that somebody other than the worker could check.

**The worker starts on your own sentence**, and the written brief reaches it a few steps in,
as one message opening `YOUR BRIEF IS WRITTEN OUT NOW` that carries the whole document in
the same layout it opened on. From the moment the worker reads that message, the brief and
its done-condition are what the work is judged by, and the task's room shows them. Nothing
shorter was sent and nothing was kept back.

**Is it stuck on shaping the brief?** A typed `/task` no longer shows `shaping the brief…`
at all: nothing waits for the brief, so there is no wait to watch. It used to — up to 25
seconds with a spinner and a clock, before the task even existed. If you see
`shaping the brief…` now, it is on a task you approved from a proposal card, in the moment
before that task appears on the roster.

**If the brief cannot be written, your words go as they are.** No model resolved for it,
no answer inside the 25 seconds the call is given, an answer that was not readable, or a brief that arrived
after the worker had already finished — the work stands on exactly your sentence and the
plain done-condition `Complete the brief and report the result and checks run.`, and
nothing is printed about it. It is never a reason for your task to be refused, held up,
or lost.

The call is billed the way codeaf's other calls-you-did-not-type are: to the session, not to
a turn. It runs on the `shaper` role, which follows the careful-work model.

## Why my task says brief kept as you wrote it — the line under a started task, my brief was not shaped

**It does not any more.** `brief kept as you wrote it` was one dim line an older codeaf
printed under `single task 12 started · …` when the shaping call, which then ran before the
task, was cut. Nothing waits on that call now, so there is nothing for such a line to
report at the moment the task starts.

A brief that is never written leaves the work on your own sentence with the plain
done-condition, and the task runs, is named and sits on the roster like every other one.
The task's room shows which it has: the longer document, or your sentence alone. If you
were relying on that pass to spell out the format or the done-condition, say it yourself —
in the task's room, while it runs, or in the brief you start it with.

## Does codeaf change my task, or rewrite what I asked for?

No. The shaping pass adds around your words; it never replaces them.

Your sentence is carried separately from anything a model wrote, under the heading
`WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS`, followed by the line *“This is the message
this work came out of. Where anything below reads differently from it, their words are what
was asked for.”* That is a rule the worker reads: where the shaped brief and your sentence
disagree, yours wins. So a shaper that overreached is overruled by the document itself.

**The short summary beside the row is the first line of what you typed**, word for word.
The title above it is not — see *Why my task is called something I did not type*.

What shaping is allowed to do is settle what you left open — which file, which format, how
long, which of two readings — and it must say in the brief that it decided, so you can see
it in the room. What it is told not to do is invent scope you did not ask for.

The same guidance reaches briefs the conversation model writes with `propose_task`, but as
part of that tool rather than as a second call: it already has the whole conversation, so
nothing needs to be re-read for it.

## Why my task is called something I did not type — who names a task, why a row is named after a folder path or the first few words I typed, and can I rename it

The name on the roster is written by a model, not cut out of your sentence.

The roster draws **three words**, and the first three words of a typed sentence are almost
never the useful ones — "can you have…", "please look into…", "read /Users/…". Every task
would be named after the way you cleared your throat, or after a path you pasted, and a
column of them would be unreadable.

**Where nothing named it, a small call does.** Some work reaches the roster with no name at
all — only the sentence it was started from: every `/task`, work that started on its own
after a words-only turn, an adaptive run's own row. That title is
handed to the cheap `taskname` role, which reads the work and answers with two or three lowercase words.
Empty replies, instruction echoes and placeholders such as `nothing to name` are refused. An answer
that is a sentence about what the model is about to do — `I'll start by …`, `Let me first …`,
`First, I will …` — is refused too: a name is a label for the work, not the front of a plan, so
the row keeps the fallback instead of being titled after somebody's opening words.
The next configured naming model may answer within the same time limit; if it cannot,
the existing fallback stays. A previously saved placeholder such as `nothing to name`
is named again from its saved brief when you reopen the conversation; the work itself
is not rerun.

**Work a model already named is left alone.** A task the conversation proposed with
`propose_task` carries the name the model wrote as an argument to that tool, and it is not
renamed — a second call to disagree with a name codeaf itself just wrote would be a bill
for nothing. The model that writes a `/task`'s brief does not name it: the task already has
its name by then, a few seconds after it started. A title that is already two or three
words with no file path in it is left alone for the same reason.

**The work does not wait for the name.** The task is admitted, checkpointed and started
before that call is made. Until the name lands — usually a few seconds — the roster, the
home card, the tasks list and the task's card all show the fallback they showed before: your
own words, cut to the first eight of the first line. When the name arrives the row simply
changes to it. If it never arrives — no model on the class, a timeout, an answer that was
itself a path — the fallback stays, nothing is reported, and nothing about the work is
affected.

**Work codeaf starts on its own is usually named before you hear about it.** When a turn is
judged to be work, or a running turn is handed over at its ceiling, the name is asked for at
that moment — beside the brief being written, not after the task exists — so by the time the
`this looked like work, so task N started:` line is drawn the name is normally in hand and
the line carries it. If the namer is still answering when the task starts, the line carries
your own words and the row changes when the name lands; the task waits for that answer
rather than asking a second time. A task that fails before its name arrives is reported under
your words.

**The name is kept with the task**, so a task that is still running when you quit comes back
under the same name after a restart.

**There is no command to rename a task.** Once a task has its name it does not change again.
What you can always see is the summary underneath it, which is the first line of what you
typed, word for word — and the room holds your whole sentence under `WHAT THE PERSON ASKED
FOR, IN THEIR OWN WORDS`. If a name is wrong, nothing about the work is wrong with it: the
worker read the brief, not the name.

**A sub-harness being designed keeps its own title** — `harness · <what you asked for>` —
because its row is read as a design and not as a task.

## Stopping the sizing call — the starting a task setting, making one worker the default

`/task <brief>` asks you nothing, and every answer here starts **one worker**. What the row
decides is only what is paid to find out how wide the work is. `/settings` → Session →
**starting a task**, or the `task.start` row:

- **sized** — the default. One worker starts at once and the sizing call reads your brief
  **beside** it; where it finds independent parts in your words, they are weighed and
  handed to other workers while the first keeps going. Nothing waits for the call.
- **single** — one worker, and the sizing call is not made at all. Nothing is spent reading
  your brief for width, and nothing is said about it.

`/task solo <brief>` always means what it says, whatever the row is set to.

**Two answers have been retired, and neither retirement is felt.** `ask` went first: a yes
from the sizing call used to open a two-row list reading “this parallelizes — how should it
run?”, and the question was being put to the one person in the room who had not read the
material yet. `adaptive` went with it: it started a planner and a fleet without asking, and
a chat turn may no longer open a planned run at all. A profile still holding either word
reads as **sized**, silently — nothing errors, nothing is said, and you are not told that a
preference you set months ago has gone.

**Choosing `single` closes nothing off.** A single worker can still break its own brief into
smaller tasks when it finds genuinely independent parts in it — see *When a task splits its
own work* — and it can still split itself once it has opened the material and found the work
is wider than one worker's share, which is *When a task turns out to be too wide for one
worker*. What `single` costs you is only the reading: nothing is judged up front, so the
split has to come off what your brief already spelled out.

## The card that asks whether to run the work — what happened to the proposal card

While the model is still writing the proposal, a grey block opens in the transcript and
grows: a still `○`, the title (or just the word `task` until the title arrives), and one
row such as `⠙ forming… · 6s`. The mark spins and the clock climbs on the same grid as
the `/task` block and a running tool row. Under a second the clock is not shown. In the
plain-text tier the mark is a still `*` and only the clock climbs. It is not a question
yet — there are no options. If the turn ends before the proposal finishes arriving, the
block settles as `cancelled · the proposal never arrived`. If the call is refused before
there is a proposal to ask about, it settles as `not started · the call was refused`.

When the proposal is complete, that same block becomes the ASSIGNMENT, and the question
about it is asked above the message box with every other question codeaf puts to you. The
block in the conversation shows:

- a head with the task's own identity mark and a two-or-three-word name;
- one dim sentence under it — the first sentence of the summary, capped at 90 cells, and
  left out entirely when it would only repeat the name;
- the facts about the work: which other window is already in these files, `where:` it will
  run, and `from your folder as it stands — unsaved edits included`;
- a dim meta line reading `model <full id> · ctrl+e for the brief`. The model id leads
  because it is the one fact nothing else on screen will say again; on a narrow frame the
  hint is dropped and the model kept.

**There is no row of answers on it and no meter.** Those were a decision drawn in a place
no other decision on this screen is drawn. The question is above the box:

```
? wants to start a task: Fix the nil-map crash
  The parser drops a key on an empty map. · codeaf
  ▸ 1  start it
    2  no
  enter take it · esc later · c change · start it in 9s
```

`▸` marks the answer the clock is about to take. The question is **not modal**: the message
box stays live, and what you type into it is the correction.

Only one proposal is a live question at a time. If a second one arrives while the first is
unanswered, the older block settles as `expired · the turn ended`, because a question that
can no longer be answered must stop looking like one.

## The forming card is not moving — proposal card frozen

While a proposal is arriving, the card's middle row reads like
`⠙ forming… · 6s`: the braille mark turns and the elapsed clock climbs. The `○` in the
head is intentionally still — it is the empty identity slot, not a second animation. Under
one second there is no number. In the plain-text tier the row uses a still `*`, so only the
clock moves.

The moving row is the display's local clock. It says the proposal call remains open; it
does not prove that network fragments are arriving, so it can keep moving while a provider
is slow or stalled. When the proposal lands, the forming row stops and the same block
becomes the question with its options and countdown. If the stream or turn ends first, it
settles as `cancelled · the proposal never arrived`. If the request is cut and retried, the
partial block disappears because that attempt's half-arrived call was discarded; a new
proposal fragment starts a new block. If the call is refused before it ever becomes a
question, it settles as `not started · the call was refused` instead of moving forever.

## Why a task started on its own — codeaf started work I did not ask for, this looked like work, so task N started

**Sometimes work starts without you asking for it, and you are told after.** After a turn
that answered a substantial message in **words alone** — no tool call — a cheap model reads
what you asked and the first two lines of the reply, and decides one thing: should that have
been work? The same judge also starts reading your message **the moment you send it**,
alongside the reply rather than in front of it, which is its own section below.

**A yes is asked twice.** The cheap model only screens — it reads every substantial message
and every wordy turn, which is why it is cheap — and it cannot start anything on its own. Before a task exists, the same
question is put once more to your **mastermind** model, in the same words, with none of the
first answer in front of it. Only if both say yes does the work start, and then one dim line
goes into the transcript:

```
this looked like work, so task 4 started: audit the pricing code
```

The number is the task's own, and the words after the colon are what it was started on. If
the machine was already full the line reads `queued` instead of `started`, and the task
begins when a lane frees up.

**The judge writes the done-condition too.** The same answer that carries the goal carries
what finished looks like — "every package under internal/ has been read and the report names
each pricing bug with its file and line" — and that sentence is what is checked against when
the work says it is done. It is read **on its own**, without the goal beside it, which is why
it has to name something somebody could go and look at. If the judge writes none, the task
falls back to `the work named at the top is actually done, and the report says what was done
and how it was checked`. Nothing else about the check differs from any other task on this
page.

**There is no card and no key to press.** There used to be a row above the message box
offering to run it, and it is gone: the question "shall I?" was being asked about work
nobody had seen yet, and the task itself answers it better by existing — it is on the
roster, it says what it is doing, and `x` stops it like any other task. Nothing else is
different about it: a row, a room, a report, and everything else on this page.

**What keeps it from becoming a nuisance:**

- **At most one start every three turns.** Two can never arrive back to back, so a
  conversation you have just taken back is not interrupted again on the next line. It is
  one allowance for both moments the judge looks, not one each.
- **Nothing after a turn that used tools.** A turn that called tools was already work, and
  asking whether work should have been work has no useful answer.
- **Nothing on a short message.** Under six words it is not read at all — "thanks", "run
  the tests", "what does this key do" are answered in words by construction.
- **Nothing on a one-command ask.** A commit, an undo, a one-line or one-file edit, a
  single read: those stay in the conversation, whatever a judge would have said. See
  *A commit or an undo is never a task*.
- **Nothing where there is no screen.** `--once`, a task's own worker, and a session with
  no router model to ask are all silent.
- **Two models have to agree.** The cheap one can only screen; the thinking model confirms
  the yes before anything is started. A confirmed no starts nothing and says nothing at all
  — and it does not spend the one-every-three-turns allowance either, because nothing was
  interrupted, so the next turn is read exactly as this one was.
- **Silence when it cannot answer.** Either model failing is the same silence: one that
  cannot be reached, or that replies with anything but the small JSON object it was asked
  for, starts nothing and says nothing. A yes nobody could confirm is not a start.

**Stopping one you did not want** is the ordinary stop: `x` over an empty message box —
which works at once when it is the only running task, and needs `alt+t` to walk to its row
first when there are others — or `Stop` on the pointer. Nothing about it is special from the
moment it exists.

**There is no setting that turns this off.** No `/settings` row switches it, and nothing you
type disarms it for the session. What bounds it is the list above, and the stop.

Nodes cut by an adaptive run also appear as rows under the run's own row — see *Adaptive
runs*, which explains what those rows can and cannot do.

## A message that reads like work makes codeaf look sooner — nothing else happens

**The judge that starts work on its own looks at two moments.** One is after a turn that
answered in words alone, above. The other starts **the moment you press enter**: your
message itself is read, with no reply to read instead, because there is not one yet.

**That read happens beside your answer, not in front of it.** Nothing waits for it. The
reply starts arriving exactly as fast as it always did, and the question is being answered
somewhere else while you watch it. So a no costs you nothing at all — which is most
messages, and it is why the read can afford to take its time.

**If both models say yes, you see nothing.** No line, no task, no interruption. The one
thing that changes is *when codeaf says something about the work*: instead of waiting until
the answer has run ten rounds of tool calls, the first note lands at the very next break
between rounds — and then at the ordinary point after that. What that note says, and what it
can and cannot do, is *An answer that runs long is told, and decides for itself* below.

**It used to hand the reply over on the spot, and it does not any more.** That was measured
against real transcripts and it took work out of the conversation that the conversation
would have finished faster: a message that *sounds* like four jobs is not the same fact as
four jobs, and reading the request cannot tell them apart. The answer doing the work can. So
this moment now only decides **how soon the answer is told what it has run up**, and the
answer decides for itself from there.

**What the judge is looking for is a request whose fastest correct answer is not a
conversation:** several independent deliverables in one message, a sweep over many files or
many sources, or an answer you would otherwise sit and watch a spinner for. A question, a
discussion, one obvious edit and a few tool calls are all answered here, however large the
subject sounds.

**Everything that bounds the auto-start above bounds this too** — one start every three
turns across both moments, six words, two models agreeing, silence when either cannot
answer — with three more that belong to this moment alone:

- **Nothing you can feel.** The read never stands in front of your turn, so there is no
  wait to notice, whatever the models do. It used to get about three seconds and to hold
  the turn for them, and three seconds was less time than the model needed to answer at
  all, so it answered nothing and charged every message the wait.
- **Nothing after you stop it.** Pressing stop ends the question with the turn. Nothing
  moves onto the rail out of an answer you interrupted.
- **Nothing on a message with pictures in it.** A task is given words, so a message
  carrying images is always answered here, where they can be looked at.

## Handing work over in the middle of an answer — this one wants more hands, why it handed my answer to a team of workers

**Work can leave an answer that already started it.** A turn begins as an ordinary reply,
a few tool calls go by, and the material turns out to be wider or longer than one answer.
At that point the model can hand it over rather than grind through it in the conversation,
and one dim line goes into the transcript above the proposal card:

```
this one wants more hands · handing it over with everything found so far
```

That line appears **only** when the handoff came out of work already done — a proposal
made on the first step of a turn, before anything has been read or run, does not draw it,
because nothing has been found yet to hand over.

**What it hands over is the findings, not just the goal.** The brief of a mid-answer
proposal is written to carry what the turn already learned: what was found, the shape of
the material, what has been ruled out and why, what would have been done next. Nothing
trims it — a long brief reaches the worker whole — so the work starts knowing what the
conversation knew instead of reading it all again.

**The proposal still appears, and the countdown still runs.** This is not the same road as
a task that started on its own (above): this work was groomed by the model, so it is
offered the way every other proposal is offered — `1 start it`, `2 no`, and a countdown
whose silence starts it. You are told, and it opens; what the card gives you on top of that is
the window to redirect it before it spends anything.

## An answer that runs long is told, and decides for itself — a reply that stops halfway to become a task, my answer was moved, this is running long, why did it not become a task

**When one answer keeps going, codeaf says so — and the model writing it decides what to do
about that.** After ten finished rounds of tool calls, and again after twenty, a short note
arrives inside the reply. It carries three facts codeaf already has — how many rounds have
gone, how many files have been opened, how many kilobytes of results are being held — and the
three roads on: answer now from what is already there; carry the rest on in a room, with
`quick_task` and `inherit` set, which opens on this conversation exactly as it stands; or
hand out the parts that have not been opened yet.

**Nothing is asked of a second model at those two points, and nothing is moved.** No task
starts, no line is added to your transcript, and the note costs no extra call — it rides into
the next request the reply was going to make anyway. The count of finished rounds is the
whole of the trigger, and **nothing about what you asked for is read** to decide when to say
it. Rounds spent only watching work already handed out do not count towards it.

**Why the model decides and codeaf does not.** The question is not "are there parts" — it is
"is this worth carrying somewhere else", and only the model holding what has been read knows
what re-reading it would cost. A second model used to be shown a summary of the work and
decide for it: on a one-line design question it read ten files in 48 seconds, a reader said
`split`, and the worker that started re-read every one of them — 551,000 tokens and six and a
half minutes, with not one item ticked. The model that had done the reading answered the
question out of what it already held, the moment somebody asked it.

**So a long answer is no longer moved for being long.** Ten rounds, twenty rounds, forty —
a reply that is working is left to work. What takes a reply out of your hands is the section
below, and it is not a count.

## When a reply is taken out of your hands — this is running long, my answer was moved, the ceiling, why a reply stopped halfway

**A reply is moved only when it can no longer work where it is.** codeaf reads that three
ways, and none of them is a number of rounds:

- **Its own context is full.** The conversation has grown until this model's window no longer
  has room for one more tool result. A reply that cannot hold another result cannot take
  another step.
- **It is going in circles.** The loop watch has said so twice, and a third note would be
  codeaf talking to itself — see *Why does it say carry on*.
- **It said it was finished and then carried on.** A reply claiming nothing is left is
  believed once per request, and ten more rounds of real tool work disprove it.

A session running unattended with a wall has a fourth: one turn may spend only a share of the
wall inline, so that what it hands over can still be checked before the wall comes down.

**At that point, one second model is asked one thing: draw what is left.** Not whether to
move the work — that is already decided — but the shape of it, as parts and arrows:
`A | B | C` means three pieces that do not wait on each other, `A > B > C` one job in three
steps, and a sentence under it saying what the letters are. That drawing becomes the
**checklist** the work carries on with. If nobody can be reached — no second model
configured, a reader that faults or takes too long — the reply still moves, with no checklist.

**Then the reply stops where it is, and what takes the work opens on the work.** See *A quick
task took over my answer* for the one-line case and *It made a task out of work that was
already done* for the reply that was finishing anyway.

## A quick task took over my answer · this is running long, carrying on here in this folder · this has parts, a quick task is taking them here · why was there no copy of the folder · the moved work carried on in my own folder

**When the answer being moved has changed nothing on disk, a quick task takes it instead of a
full one.** That is the whole of what codeaf reads here — not what the work was about, but
whether the reply had written or edited anything under the folder you are in. A reply that
only read files, ran searches and looked things up has nothing to isolate and nothing to
merge, so it is not given a copy of the folder.

**You read one line instead of two:**

```
this is running long · carrying on here, in this folder, with everything already read: audit the pricing code
```

**What that means.** The work carries on **in the folder you are standing in** — no branch,
no copy, no merge. The parts the drawing named become the quick task's **items**, in the
order they were drawn, and it works through them in that order. Nothing checks it and there
is nothing to land: its last message is the answer, and it reaches you as the ordinary note
when the row goes `done`. It is on the rail like any other task, so it can be opened, steered
and stopped from there.

**If the reply had written anything at all, none of this applies.** One edit is enough: the
answer takes the ordinary road above — a task in its own copy of the folder, briefed, checked
and landed — and you read the two lines that road writes.

## Can it keep what it read — does the moved work start over, why did it stop reading and start a task, inherit

**A moved reply is PROMOTED, not restarted.** The quick task that takes it opens holding
**this conversation's whole transcript** — every file that was read, every result that came
back, word for word — with the drawing's parts as its checklist on the end. It re-reads
nothing, and the provider bills most of it as already cached.

**That is what the `with everything already read` in the line means.** Before this, the work
was handed a brief plus a list of **pointers** to the calls the reply had already made, and a
pointer is an instruction to go and read it again: measured once at 551,000 tokens and six and
a half minutes, with nothing ticked.

**The model can do the same thing itself.** `quick_task` has an `inherit` field, off by
default. With it set, the quick task it starts opens on this conversation as it stands
instead of on a sentence about it.

**And what cannot be inherited is still not re-read.** `inherit` is refused when the
conversation will not fit the worker's window with room left to work in — the model is told
roughly how big the conversation is and how much room the worker would have, and can start
the work without it. A reply that could not be promoted for that reason, or because it had
written files, goes the briefed road, and **the brief carries the account of the turn**:
what was called and the end of what came back, newest first, cut, under the heading
`WHAT THIS WORK ALREADY FOUND OUT`. Not pointers.

## It made a task out of work that was already done · why did it hand over when everything was written · the task redid what the answer had already written · it started again from my first message

**When codeaf stops to look at a long answer, it asks the model writing that answer whether
anything is left.** If the answer is that nothing is — everything you asked for is already
written — then **no task starts**, no line is added to your transcript, nothing is marked
done and nothing is stopped. The reply you were already getting simply finishes.

**That needs nothing to agree with it. It needs nothing to contradict it.** The second
reader's sketch stops the drop only where the sketch names **independent parts still to
do** — a reader saying positively that work is left. A reader that could not be reached, one
that faulted, one that ran out of time, one that drew a single job, and one that drew
`(waiting)` all say nothing about whether you are finished, and none of them is read as
though it had said you were not. Measured before this: a reply wrote the CSV that was asked
for, deleted the file it replaced and said it was done — and because the sketch at that
moment read `(waiting)` for a build still running in the background, the whole request was
handed to a fresh worker that started again from the first message and ran the clock out.

**If something of your own is still running, this is not the road you are on.** A reply whose
only remainder is a command it started waits for it here instead, and no task is made of the
wait — see *Waiting for a command you asked for does not become a task*. If the reply instead
says it is finished outright while that command is still going, the drop above applies and
the command is untouched by it: nothing is killed, nothing is marked done, and its ending
comes back to this conversation as the note it always would have.

## It said it was done and then carried on · it started work after saying nothing was left · I typed something more and it made a task of it · I pressed escape and a task started anyway

**A reply that says nothing is left is believed once per request.** If it then does ten more
rounds of real tool work, codeaf looks again, does not believe it a second time, and the work
moves on in the ordinary way. Rounds spent only watching pieces you already handed out do not
count towards those ten — see *Watching the pieces you handed out does not move your answer*.

**Once, whether or not the second reader agreed.** A sketch reading `(done)` at the same
moment is better evidence than the reply alone and still not proof — both can be wrong
together — so agreement buys no extra drop.

**Typing something new gives you a fresh one.** The count runs against the request, so a
direction you add while the reply is running is a different request: it arrives with nothing
spent, and a reply that discharges *that* is dropped on its own terms.

**Interrupting a reply stops its handover.** Press escape, or close the session out from
under a running reply, and it cannot start a task. This also applies if the interruption
arrives while the handover is preparing its brief: a canceled model call does not fall
back to starting a worker from your original message.

**Both writers of the handover are told that finished work is not what is left.** The
second reader's question ends with it in as many words: work already handed out is not a
part, and work that is already done is not a part either — what the account it was shown
says is finished is not what remains, and drawing it sends somebody to do it a second time.
The model that writes the brief underneath is told the same thing about its prose: nothing
already done goes under *what is left to do*, because what has been read, run or found out
is what the worker **already knows**. Before that clause existed, a sketch naming reading
the answer had already finished put it at the top of the brief under **WHAT IS LEFT, AS
PARTS**, the worker obeyed the loudest and earliest line in its document, and it spent its
first minutes re-reading what the conversation above it had read.

**And the worker is told which half of its brief wins.** The list of calls that had already
run is authoritative about what has happened: where the parts or the brief read as though
one of those calls were still to be made, it has been made already, and the worker is told
to read it through the pointer on its line rather than run it again — running it again only
where the line says it failed or where what it reads disagrees with the brief.

**What that still does not cover: the seconds after the account was taken.** The reading is
one line drawn from a snapshot, so a part that lands while the reader is answering is still
drawn as remaining and the work still moves. What was closed separately is the opposite
mistake: silence, a fault, or a shape with no parts in it are no longer read as a reader
saying work remains.

## Can I give a task a short name?

**What the task is given.** Your own message rides it **word for word** — that is true of
every task on this page and it is never rewritten. On top of it comes the brief: an
instruction for whoever picks the work up, saying what is left, what was already found out
that they would otherwise have to find again, what was ruled out, and how anybody could tell
when it is done. **Those first two are not the same list**: anything already done belongs
under what is already known, never under what is left — see *It made a task out of work that
was already done*. Where the work was handed over because it had parts, the sketch and its
sentence sit at the top of that brief, so the worker starts with the pieces already named.
Its name is cut from your own message too, and a short name replaces that a second later.

**The brief is drafted by one model and written by another.** The model that wrote your
answer is asked first, because it is the only one that knows what the answer found out — but
by then it is a tired model at the end of a long turn, and asking it to be its own editor was
measured producing 5,882 characters in which the same six sentences went round and round. So
its answer is a **draft**, and a second model on the thinking tier writes the real one: it is
handed your message word for word, what the conversation already knew before this turn, the
account of the work with what came back in it, and the draft. If what comes back is not prose,
or is a document that has stopped saying new things, it is asked once more and then given up
on — and then the draft stands, and if there is no draft either, your own sentence alone. A
task always starts; the only question is how much it starts knowing.

**And when the brief cannot be written at all, you are told so on the line.** The second
model can fault, be out of capacity, or simply not answer inside the minute and a half it is
given — and the draft can come back as nothing usable at the same time, which is exactly what
happened on a measured run. The move still goes ahead: a task that starts knowing only what
you typed is better than an answer left grinding where nobody is watching it. But it is a
different event and it reads as one, with the reason on the end of the same dim line:

```
this is running long · moving it to a task that is watched and can split · carrying the ask only — the brief could not be written: the second model did not answer in time
```

The reasons you can see there are **did not answer in time**, **could not be reached**, **had
nothing new to say** — a document that went round in circles twice — **there was nothing
to write it from**, and **no second model is set** — which is not a wire failure at all, and
has a section of its own below. When either the written brief or the draft survives, the line says nothing
of the kind and the extra sentence is simply absent: your work went with everything the turn
found out. Every rung of that ladder is also written into the session's own record, with what
it produced or what the model that refused actually said, so a run where a worker started
blind can never again read the same as one where it started knowing everything.

**And the task is finished against your question, not against the brief.** The brief says
what is left of the work right now; the thing the independent reader at the end checks
against is **your own message**, in full — "everything asked for below is actually done — all
of it, not the part that was easiest to reach". A task handed over halfway used to be
finished against whatever the brief happened to be holding, which on a long piece of work
meant a ten-hour request being accepted as met the moment the code compiled.

**And it can still come to nothing — when both readers agree.** That last question also asks
what still remains, and if the model answers that everything you asked for is already done,
**and the second reader's sketch at that same point said `(done)` too**, the move is
**dropped**. No task, no lines. The answer carries on to its own end and stands, which is the
right outcome for a turn that was finishing anyway: what this whole mechanism is for is an
answer that is grinding, and one that is about to stop is not.

**One reader saying so is not enough**, and that is deliberate: a model in the middle of a
long answer saying "everything is done" is that model marking its own work at the moment it
has a reason to. So if the second reader still sees work left, the move happens anyway — on
your own message, since a reply that answered "nothing left" wrote no brief to hand anybody.

**When the second reader names parts, the task is split up
while its worker is already at work.** The sketch is not only a paragraph at the top of
the brief: it is put to the task's own splitting road beside the worker, which starts on
the whole brief at once, and each part named in the sketch becomes a worker of its own
with its own copy of your folder, taken as the task's copy stands when they are handed out.
The task they came out of stays open to gather their reports into one answer. Nothing about
that road is skipped: the parts are read together by a second model that can sharpen their
instructions, merge two that overlap, or say this is one job after all — and if it says
that, or if there is no free lane to run them in, the task simply goes on as **one
worker**, which is what it was already doing. Where the sketch drew a final step behind the
parts — `(A | B | C) > D` — the parts are handed out and `D` stays with the task itself,
to do once their reports are in.

**Whether the sketch is read as parts.** What counts is what could be started **now**.
`A | B | C` is three. `A > B > C` is one job in three steps. `A > (B | C)` is one job too —
the fork is behind a step nobody has taken yet. `(A | B | C) > D` is three, with a fourth
step waiting on all of them. `A > B | C > D` is two chains that wait on nothing but
themselves.

**At the ceiling the task is allowed to split but nothing is handed out.** There the
answer outran one pair of hands by measurement and no parts were drawn, so the worker is
merely allowed to hand parts out once it has opened the material. Whether it does is its own
decision, and it still has to justify them inside the task; the roster says so if it happens —
see *When a task turns out to be too wide for one worker*.

**What it costs.** Nothing at all until a reply is actually taken out of your hands: the two
notes buy no model call, and the drawing is one call at the ceiling, which most answers never
reach. A reply that is promoted stops there — no brief is written, because the worker is
handed the conversation itself. A reply that has to be briefed instead adds two: the draft,
and the model that writes the brief out of it. One further call goes at the
**end** of any answer that touched a tool at all, asking whether your question is finished —
see the section below. An answer that called no tools costs none of this. The read at the
front of your turn is one cheap call and now starts nothing by itself.

**This applies to replies codeaf started by itself, too.** When a task lands, the chat
answers it without you typing anything (see *Why did the chat reply on its own* in *how tasks
run*). That reply is priced exactly like one you asked for: same two notes, same ceiling,
same handover. It used to be exempt, on the grounds that a reply about a task already had a
budget somewhere — it does not, and a measured run had one such reply make 127 tool calls
over 46 minutes with nobody watching, and then the session sat idle for seven and a half
hours. A line codeaf writes to itself that nobody owes an answer for is still left alone.

**There is no setting that turns this off, and no number you can raise.** What bounds it is
the list above.

## No second model is set — the brief said that instead of a reason, nothing is configured for the reader or the writer, no thinking tier

**`no second model is set` means a row nobody wrote, not a provider that went quiet.** It
appears on the end of the line that tells you your answer was moved:

```
this is running long · moving it to a task that is watched and can split · carrying the ask only — the brief could not be written: no second model is set
```

Two calls on that road do not run on the model you are talking to: the **reader** that decides
whether the answer is moved, and the **writer** of the brief the task opens on. Both are on the
thinking tier, and both are deliberately crew-only — with no crew they are skipped rather than
handed to the model that has just written the answer and would be editing itself. So if nothing
is set for them, they have no model at all, and this is the sentence that says so.

**What to do about it.** Set a thinking-tier model with `/crew`, or pin the two roles on the
**pinned roles** row in `/settings` → Providers — they are called `markreader` and `handoff`,
so the row reads `markreader:openai/gpt-5, handoff:openai/gpt-5`. Or run with `--one-model`,
which settles them on the model you are talking to along with everything else. Either way the
move still happens — a task that starts knowing only what you typed is better than an answer
left grinding — and the session's own record keeps the exact reason, `roles: no model for
role`, beside the rung that had nowhere to call.

**It is one of four different facts, and they read differently on purpose.** `no second model
is set` is nothing configured; **could not be reached** is the wire; **did not answer in time**
is the minute and a half running out; **had nothing new to say** is a document that went round
in circles. A run told the wrong one of those sends you to look at the wrong thing, which is
exactly what happened before this sentence existed.

## Watching the pieces you handed out does not move your answer — I was only waiting on the other tasks and it made a task out of that, does watching a running task count

**Rounds spent looking at work you already have out do not count.** A step that only reads
the task rail or a background job's output is not a step of work, so a turn spent watching
four running pieces never reaches a look at all, and the points stand exactly as far ahead
as they did before it.

**And the sketch has a word for it.** If the only thing left is waiting on work you already
handed out — waiting for it, reading what comes back, accepting it — that line is
`(waiting)`, and nothing is moved: a piece already running cannot be handed out a second
time, and a drawing whose every part is a wait or a bare `accept`/`review` is read the same
way even when it is not written as that word.

## When only part of a handover can move while other tasks are still out

When only half of what is left is a wait, the other half still moves when its whole instruction
stands alone. If a reply crosses one of the lines above while pieces you handed out are still
running, what is left is divided first. The self-contained work becomes the task; everything
about the pieces already out — waiting for them, integrating their branches, reviewing them,
opening the pull request over them — stays with this conversation, and the line announcing the
move ends ` · the rest stays here for when the pieces already out land`.
The check covers the brief, the request above it and the done-condition below it, after the
reader's drawing has been added. If any of those still names a piece already out, nothing
moves: your original words are kept here rather than rewritten into a different request.

What stayed is written into the conversation under `WHAT STAYS HERE, FOR WHEN THE PIECES
ALREADY OUT LAND:`, so the turn that wakes when those pieces land opens on the integration,
the review and the pull request it still owes you. Nothing is dropped; it is done a little
later, here.

**And when all of what is left is about them, nothing moves at all** — even where the
sentence you typed was itself the coordination, and equally where the brief could not be
written and only your sentence was left, because a sentence typed before any of this was
divided cannot stand in for the half that could have gone. No task is started, your words
are not rewritten, and the line reads `this is all about the pieces already out · keeping it
here until they land`.

**Why none of it can go with the work.** A task sees only the pieces it created itself, so a
worker told to wait for your task 4 and task 8 would ask for them and be told `No tasks have
run in this project yet.` — a duty it can never see the object of.

## Waiting for a command you asked for does not become a task — my build was still running and it made a task out of waiting for it, it started a task just to check a file it had already written

**A command you asked for is already yours, and waiting for it is not work anybody else can
take.** When a reply crosses one of the lines above while a background command **this
conversation started** is still running, the model writing the answer is shown that command
by its number and its name — `job 1 (./slow-build.sh)` — and asked one extra question along
with the brief: is everything else you asked for already done, so that all that is left is
that ending? If it says yes, and names the number, **nothing is started**. No task, no lines,
nothing added to your reply.

**Nothing is finished and nothing is stopped either.** The command keeps running, your
request stays open, and the ending comes back the way it always did: the moment the command
exits, that ending starts a turn here on its own, and the reply you get is written with the
result in front of it. This is the difference from the *both readers agree* drop above — that
one is your request being **done**; this one is your request being **unfinished, in this
conversation's own hands**.

**It has to be verifiable or it does not happen.** The number must be one you were actually
shown, the command must still be running when the answer comes back, and you must not have
typed anything in between. If the command finished while the answer was being written, if a
number was invented, or if you changed direction mid-reply, the work moves onto a task exactly
as it would have — the direction this errs in is never to drop work on a doubt.

**And a running command on its own changes nothing.** If there is real work left — three call
sites still to rename, a suite never run — the reply is handed over as usual, with your build
still running beside it. What holds the answer here is the remainder being only that ending,
never the fact that something is running.

**Watches are not this.** A watch is a command re-run on a timer with no ending of its own to
wait for; a reply that stops "until the watch fires" is the other question — see *Waiting on
something, and the limit on carrying on*.

## A reply that starts changing files becomes a task — why did my edit become a task, it started a task instead of just editing, how many files can a reply change, how many edits can a reply make, small edits inline

**Reading is free. Writing is not.** The two notes above count tool ROUNDS, which is the
right unit for a reply that is looking things up and the wrong one for a reply that is
changing your files: forty rounds of reading cost you a wait, and forty rounds of editing are
unreviewed changes in the folder you are sitting in. So there is a second, much shorter
count, and it counts only the calls that CHANGE something under the folder this conversation
is open on.

**The allowance is five write calls.** A reply may make a small, obvious edit inline — fix
the typo, change the one line, write the note beside it and the file it needs — and that is
the whole point of leaving one at all. What is counted is HOW MANY TIMES the reply reaches
for the disk, never how many different files it touched: a reply that writes a short script
and then the file the script produces has done one small thing, and it stays here. The write
that would cross the allowance is not made in the reply. The answer ends where it is, what
is left moves onto one task, and the same two dim lines go into the transcript as at the
third point above:

```
this is changing more than a quick edit · moving it to a task that is watched and can split
this looked like work, so task 4 started: rename the parser
```

**What counts as a write.** An `edit` or a `write` call naming a path under this folder, a
saved cut from `edit_video`, and a shell command that names what it would change — `sed -i`,
`patch`, `mv`, `rm`, `cp`, `mkdir`, `touch`, a `>` redirection, a git command that is not
just looking. A `cd` inside the command is followed, so a write into somewhere else is
somewhere else. **Reads are never counted**, in any number: `read`, `grep`, `ls`, `git log`,
`git diff`, running your tests. Neither is a write that FAILED, and neither is anything
outside this folder — a scratch file in `/tmp` is not your work.

**It can still decide not to move.** The move goes through the same road as the third point,
which means it can be dropped when the model writing your answer says nothing is left AND the
second reader agrees — which is exactly the reply that made its one edit and was finishing.
And it happens **once** in a reply: past it, the notes and the ceiling govern it again.

**Why it is there.** A message reading "implement this issue" was answered as an ordinary
reply for seven minutes and forty-six seconds — forty-eight tool calls, `sed -i` edits in
somebody's live checkout — before the round ceiling finally moved it. Nothing in between was
watching what those rounds did to the disk.

**A commit, an undo, a one-line edit or a single read is never moved**, whatever the write
count. Those stay in the conversation — see *A commit or an undo is never a task*. Neither is
a reply that is delivering a task's own finished result — see *Finishing a task's work stays
in this conversation*.

## Finishing a task's work stays in this conversation — it started a second task while integrating, my cherry-pick became a task, the commit after the task finished never happened, why did merging the branch start more work

**A landing speaks only when an answer is owed.** When the chat launches a task in a turn
that answers something you asked, the task carries your question with it. Its landing wakes
one short reply, fed only the question and the task’s result note. That reply answers from
the result and never redoes the work; if the result is thin, it says so and offers a
follow-up task.

A task you start yourself with `/task` carries no question, so its landing only writes the
dim line and starts no reply. A family replies once, when the root lands, never for each
child. A check’s landing starts no reply. The reply that does start is where the rest of
what you asked for happens: cherry-picking the branch across, staging the files, the commit,
the pull request. However many files it touches, it finishes here.

**Why.** A four-module repair was asked for on a branch with a final commit. The task did the
work — 29 independent checks passed, the protected files were untouched, the branch was
there — and the reply that read its report began the cherry-pick. The write count moved that
integration to a second task, which got a **fresh working copy with none of the staged
index** and a description written from a reply that was integrating rather than working. It
ended in a cancelled stream, and the commit you asked for never happened.

**What has to be true, exactly.** The reply is answering the report of a task **this
conversation started** — checked against the engine's record of which conversation started
it, never against any wording — and **you have not typed anything in that reply**. Both, or
the write count applies as usual.

**Your next message closes it.** A new broad request is moved exactly as it was before, and
so is one you type into the delivery reply while it is running: your words are the request
again, and this is not a licence over everything that follows.

**And it has to be provable.** After a restart, a task restored from a previous run has no
record of who started it, so its delivery is moved like any other writing reply.

**Nothing is said and nothing is shown for this** — the reply simply carries on and finishes.
The round ceiling and the time share above still govern that reply exactly as they govern
every other one; it is only the count of files changed that stands down.

## A commit or an undo is never a task — commit became a task, undo started a task, why did a small ask become a task, fix this one line, commit everything

**A one-command ask is never handed to a task.** "commit everything", "undo that", "fix this
one line", a single file read: those are answered here, in this conversation. They are not
converted mid-reply, they are not proposed with `propose_task`, and they do not start a task
on their own — even if the reply has already staged several files, even if a judge said the
message looked like work.

**Why.** "commit everything with a sensible message" was measured becoming task 5, and the
commit then never happened. Handing a one-command ask to a worktree is how the deliverable
gets dropped. The floor is the words you typed, not how much the reply has already touched.

**What still becomes a task.** Several independent pieces in one message, a sweep across
many files, a rewrite you would sit and watch: those can still be handed over, proposed, or
started with `/task`. Typing `/task commit everything` still starts a task, because you
asked for one.

## An answer that stops before your question is finished is carried on — my reply stopped halfway, it said it would do the rest and then stopped, codeaf kept going without me

**A reply ends when the model stops calling tools, and that happens for two different
reasons.** One is that the work is done. The other is that it reached a comfortable place to
stop — "I've finished the parser, next I'll wire the handlers" ends a reply exactly as firmly
as a finished job does. Until this, nothing checked which of the two it was, and a measured
ten-hour request ended with hours of it never touched.

**So at the end of a reply that has already run long enough to be looked at once — the first
of the moments above — the same second reader is asked one question**: is what you asked
for finished? It is shown the same short account of the work —
your message, the steps, what came back — and it answers either the single line
`NOTHING LEFT TO DO`, or one line saying what of your request is still not done.

- **Finished** — the reply ends, exactly as it always did. Nothing is said and nothing is
  added.
- **Not finished** — the reply **carries on**. One dim line goes on the screen:

  ```
  the ask is not finished · carrying on rather than stopping here
  ```

  and the model is handed the reader's one line as the thing still to do. It picks up from
  where it stopped rather than starting again.

**A reply that ends by asking you something is never carried on.** If the last thing it said
finishes with a question mark, it is waiting on you, and carrying it on would be codeaf
answering a question that was addressed to you. That is the whole of the test — the mark
itself, so it works whatever language you are talking in.

**A short reply that only LOOKED at things is not read at all.** If the reply ended before it
reached the first of those notes — a couple of reads and an answer, or no tools at all
— it is never read for what remains. There was not enough work in it to leave half done, and
reading every small reply cost a thinking-tier call on every message you sent: measured, that
was a third to a half of a small question's whole bill, and it almost never found anything
left to do.

**A finished task starts no reply on its own.** Its landing writes one dim line in the
conversation and does not start a turn. You can open its room and read the task's own report;
what you happened to ask about in between is not part of it, and a slow task landing after you
have said something else does not overrule what you said.

**But a reply that CHANGED a file and then stopped is read however short it was.** The gate is
what the reply left behind, not what it cost. If the last thing a reply did was save or edit
something — and it then stopped in words, with nothing run over the top of it — it is read for
what remains whatever its length. A short reply cannot leave a job half done; it can very
easily leave an **unbuilt edit**, and that is exactly what happened on a measured run: a reply
woke up, made six calls, overwrote an 18,000-byte source file, said "now let me build and run
the full test suite", and ended without running anything. The file it had just written was the
six compile errors that shipped, and three hours of the request went unspent.

**Running anything after the save takes it back off that gate.** A build, a test, a re-read of
the file it just wrote — anything at all after the last save is the reply having checked
itself, and it is then priced like any other short reply. codeaf does not try to tell a build
from a test from a read; it only asks whether the reply stopped on the change or looked at it.

**What bounds it is the same meter as everything else on this page, and two rules of its own.**
Carrying on counts as a round, so it climbs the same ladder, and a carried-on reply that
reaches the third one is handed to a task in the ordinary way. On top of that: a reading that
says what the last one said stops the reply on the spot, and a reader that keeps finding new
things is believed at most three times — see *Waiting on something, and the limit on carrying
on* below.

**A reply that BROKE is never carried on.** Carrying on is for a reply that stopped early,
and a reply that ended on a **failed request** did not stop early — it broke. A provider
that says it stopped on an error, and a reply that comes back completely empty, both count:
neither is read for what remains, because the failure is what remains and the retry ladder
already owns it. A measured run read a broken reply three times in fifteen seconds, paid a
thinking-tier call each time, and re-opened a reply that could not move. See *Models,
crews and what things cost* for what the error line says now.

**With no second model set, nobody is asked at all.** The reader is a crew job, so an install
with no thinking-tier model configured has none — and rather than call, fail in two
milliseconds and write a failed reading into the session file on every round, codeaf does not
ask, and notes the absence once. A reader that faults or takes too long is different: the call
was made and it came back with nothing. In a session you are watching, the reply still ends as
it would have ended before any of this existed. On an unattended run, if the session made work
inline and that missing answer is the only gap, codeaf actually runs the declared checks over
the tree; a green check can stand in for the reader, while a red one is carried on by command.
Having no configured reader is an absence, not a failed call, and never runs checks on its
own — see *Leaving it running on its own* in *starting codeaf*.

## Waiting on something is not carried on — it kept polling while it waited, it turned my wait into a task, why does it say carry on, why does it say "carried on 3 times", why does it keep asking about a task that is still running

**A reply that ends while something IT started is still running is never carried on.** A
background command, a watch, a video or music render — while any of those is still going,
the reply is waiting on it exactly the way a reply that ends on a question is waiting on
you, and pushing it on would only make it poll.

**The ending comes back and starts a new reply by itself.** A background command exiting, a
render landing, **a watch firing**: each of those wakes codeaf and you get
the sentence about it without typing anything. So you can start something, close the laptop
lid on the conversation, and come back to the answer rather than to a card and silence.
On a headless `--once --yolo` run with a budget, the command stays alive for those replies
until no work is moving and no reply is in flight; without a budget, `--once` still exits
after its one reply.
`jobs list` shows what is still running, and `jobs output <id>` shows what it has said so far.

**A watch is two kinds of news and only one of them wakes you.** Its ordinary updates — the
new lines in a log, the number that moved — are quiet: they wait for the next thing you say,
because a reply every time a log grows by a line would be a ticker tape. But the tick that
**ends** the watch is the answer you started it for, and that one wakes the conversation: the
line `until` was waiting for appeared, the output went quiet for as long as you asked, or the
command failed three ticks in a row and the watch gave up. Nothing else will ever come from
that watch, which is why it is the one that gets said out loud.

**What that fixes, measured.** A conversation waiting for GitHub's checks on two pull requests
had a watch of its own running over `gh pr checks`, and said so at the end of every reply.
Nothing knew that "waiting on the world" was an answer, so the reply was read, found unfinished
— it *was* unfinished — and carried on. Twenty times in five minutes, each one another poll of
the very command that was going to report, for about a third of a dollar and no progress, until
the running-long point moved the wait into a task whose done-condition nobody could ever fail.

**A task started for the message you just sent ends the reply, and nothing else does.** If the
reply handed *this* request's work to a task and that task is queued or running, the reply
stops there and is not read: the outcome is the task's to deliver. When it lands, one dim
line is written in the conversation and no turn starts. Anything you say after the handoff,
including a correction typed into the running reply, puts the reading back.


## Why does it say carry on — what carry on means, carried on, why does it say "carried on 3 times", the reply was pushed on, it argued with itself about something it had already answered, why does it say "saying it again would not change it"

**Carry on is the reply being pushed on past its own ending.** When a reply stops, its
ending is read against what you asked. If something you asked for is still missing, the reply
is not left there: it is carried on, with the missing piece as its brief, and the row says so.
`carried on 3 times` is the count, and three is the limit — after the third the reply stops
where it is and tells you, rather than being pushed on again over the same gap. **A reading
that simply repeats the one before it never gets that far**: the second identical look ends
the reply straight away, because a thing said twice is not a second piece of evidence.

**It is never carried on over its own running work.** A reply waiting on a task, a quick
task, a background command or a watch it started is waiting, not unfinished. A landing
writes one dim line and does not start a reply here on its own.

## A settled task with green checks is not carried on as unfinished — it kept saying the ask was not finished over a done task, carried on 3 times then said unfinished

**When the task that was this request comes home done and its own checks have passed, one dim
line reports the landing.** The card already shows it done. No reply starts.

**A piece of a larger ask still writes one dim line.** If the landing is one finished part and
what you asked for is bigger, or the landing is incomplete, or nobody ran the task's own
checks, no reply starts.

**It is the other half of handing the work out.** While the task is queued or running, the
reply that started it is not read. When it lands, one dim line is written and no turn starts.


**And a reader that keeps finding NEW things is carried on at most three times.** Three
different readings are three pieces of evidence, so each one is believed; the fourth time the
reply ends instead, and one dim line goes on the screen with what was actually read in the
middle of it:

```
carried on 3 times · the last reading showed: the checks have not landed · stopping here rather than carrying on again
```

**Either line quotes a reading and never asserts a conclusion.** What sits after `the last
reading showed:` is what the reader said, or — in an unattended run — the list of things that
came home unfinished and the checks that did not pass. There is no number to raise and no
setting that turns either of them off.

**Three carry-ons can never reach the running-long point by themselves.** That point stands at
forty rounds and carrying on can add three, so a reply that gets handed to a task got there on
rounds of its own work, which is exactly the reply that point was written for.

## Every key a task proposal takes — how to decline a task

The proposal is answered on the question block above the message box, in the ONE key
grammar every question on this screen takes:

| key | when | what it does |
| --- | --- | --- |
| `1` | box empty | **start it** — admits the work exactly as briefed |
| `2` | box empty | **no** — declines it |
| `enter` | box has words | sends what you typed as a correction, and starts the corrected work |
| `enter` | box empty | takes the answer marked `▸`, which is the one the clock would take |
| `esc` | always | **later** — folds the question to the chip and answers nothing |
| `c` | box empty | answer in words: the same thing as typing and pressing `enter` |
| `ctrl+e` | box empty | opens or closes the brief in the conversation |

**Bare letters are ordinary text.** The question is not modal: the moment there is anything
in the message box every printable key belongs to that box, and the only key still the
question's is `esc`. This is why `run tests first` can be typed into an empty box without
losing its first letter.

**Any key you press stops the countdown**, whether or not it answers anything, and tells
the engine so. Deleting your draft does not restart it.

**A key pressed in the first quarter-second is dropped**, so a proposal landing under a
moving hand is not answered by a keystroke aimed at your sentence.

You can also click an answer: each answer's row is pressable along its whole width.

**Honest limit:** there is no longer any way to pick the model from the proposal. When a
word matched more than one model the card used to offer them on a row of chips answered by
`1`–`4`, and those digits are the question's answers now. The work runs on the closest
match — the one the chips opened on and the one the clock would have taken — and it is
named on the block's meta line. To ask for a different one, say so in words.

While the question is up, a decision the session is blocked on outranks the roster, any
open room, every overlay and the draft.

Expanding the brief: `ctrl+e` with an empty box, or `ctrl+o` on a block you selected with
`↑`/`↓`. It shows the whole summary, then the whole brief, then `done when: <acceptance>`
on its own labelled line. Clicking the block's body does not open the brief — it opens the
task's room.

## The countdown on a task proposal — start it in 9s

The clock is the last thing on the question's own answers row, and it says which answer is
about to be taken and when: `start it in 9s`. It is rounded up, so the last second you have
is drawn as a second; above a minute it reads `2m 13s`.

**The clock runs toward yes.** Silence approves the work as briefed, with no correction
appended, and the block settles as `approved · the clock`. This is the opposite of the
permission question's countdown, which never answers at all: a task proposal is not a
permission gate — it is your window to correct the work or wave it off before it starts.

While that countdown runs, the main footer says `starting task`. You do not have to answer.
Holding the proposal removes the countdown; the footer then says `waiting · your call`, and
other windows report `waiting on you` too. An automatic proposal does not hide a separate
question that really needs an answer.

The default window is 15 seconds. **Where is the setting for how long a proposal waits?** It
is `task.autoapprove_seconds`, and it lives on the **`Safety`** tab of the settings panel —
open that with `ctrl+,` or `/settings` — where it is the row labelled `task countdown`. It
is not on the `Spending` tab; it sits with the consent rows because it answers their
question in the other currency.

**This is one of the rows codeaf will not change for you.** It decides how long you get
before work starts on its own, so `change_setting` refuses it and points you back at
`/settings`. Same for `task.parallel` below, and for the whole approval and spending
family — the permissions page lists them.

Set that window to 0 and there is no clock at all: the answers row ends in `waiting`, and
the question sits there until you answer it, however long that takes.

Pressing any key the question reads also stops a running clock. The tail stops counting
immediately and the task cannot start while you finish your answer. Deleting everything you
typed does not restart it.

## What start it and no each do

**`1` start it** admits the work exactly as briefed.

**`2` no** declines. Nothing is spawned, no row appears on the roster, and no room exists.
This is a normal answer, not an error.

**Anything you type is a correction**, and a correction is a yes to the corrected version.
Your words travel verbatim and are appended to the brief; this is the last moment the brief
may change. Only `enter` sends them. The box is cleared on an answer, so your next `enter`
does not send the correction to the model as a message.

**There are no hidden word answers.** Typing `no` into the box and pressing `enter` does
NOT decline — it starts the work with the word "no" appended to its brief. A bare `no`,
`nope`, `n`, `stop`, `cancel`, `don't`, `yes`, `y`, `ok`, `okay`, `go` and `sure` used to
be thirteen secret answers, none of them drawn anywhere; the answers are on the row with
their keys now, and the box is words. **To decline, press `2`.**

**`esc` does not decline either.** It is *later*: the rows fold to the chip
`? 1 question · alt+y`, the proposal stays open, the engine stays waiting, and nothing is
decided. `alt+y` brings it back.

Once answered, the block collapses to its head and one foot line that keeps both halves —
what you reached for and what it came to, joined by ` · `:

| what you did | the foot line |
| --- | --- |
| pressed `1` | `start it · approved` |
| typed a correction and pressed `enter` | `change · approved · you redirected it` |
| pressed `2` | `no · declined` |
| let the clock run out | `approved · the clock` |
| the turn ended under the question | `expired · the turn ended` |

And one dim receipt is left above the message box, in the same words the answer is written
into `decisions.jsonl` with:

```
  ✓ wants to start a task: Fix the nil-map crash → start it · you · 14:02 · c change · ◐ working
```

## The task started before I could say no

A proposal starts on silence only while its countdown is still moving. The default window
is 15 seconds. Pressing any key the question reads stops that clock immediately, and
deleting what you typed does not restart it. Press `2` to decline.

If nothing was pressed before the clock reached zero, the work was already admitted and a
later answer cannot pull it back. Use `task.autoapprove_seconds` in the Safety settings to
give yourself a longer window, or set it to 0 so every watched proposal waits for you.

## I typed no and it started anyway

**Typing `no` into the message box does not decline a proposal.** It is a correction, so
the work starts with the word "no" appended to its brief. This changed: `no`, `nope`, `n`,
`stop`, `cancel`, `don't` and `dont` were once complete answers you could type, and they
were the only answers on this screen that nothing on screen named.

**Press `2`.** It is drawn on the question's own row as `2  no`, and it is the only thing
that declines. `esc` folds the question away without answering it, and the clock will
still start the work when it runs out — so a proposal you `esc` and forget is a proposal
that starts.

## How do I stop a proposed task from starting?

Press `2`, or click the `no` row. Pressing any key the question reads stops a running
countdown; erasing your draft does not restart it. Set `task.autoapprove_seconds` to 0 in
Safety settings if every proposal on a watched session should wait until you answer.

## Why it warned me another window is already in these files — two windows working on the same files

Before a brief becomes a paid run, codeaf compares the paths that brief **spells out**
against what every other codeaf window open on this directory has already written. Where
they overlap, one dim line appears — on the proposal card, between the brief and the
answers, and as a note when you start work yourself with `/task`:

```
another window is already in internal/tui3/home.go · Port the picker
```

The path is the file both pieces of work are in; the name after the `·` is what that other
window called its task. At most three paths and two names are spelled, and the rest are
counted: `+2 more`.

**It is a fact, not a gate.** Nothing is blocked, nothing is queued, no clock changes and
no option disappears. The countdown still runs toward yes, and `yes` starts the work. The
line is there so you can `redirect` it or say `no` in the seconds before the money is
spent, rather than finding out at merge time. The model that proposed the task reads the
same line on the end of its result, so it can sequence its next proposal around it.

When the brief **names no files**, there is nothing to intersect, so the line says only
what is still true — and where those windows have been so far:

```
another window has work out in this project · internal/tui3/home.go, internal/session/task.go
```

When another window's work **has not written anything yet**, nobody can say whether it is
in your files, and the line says exactly that instead of guessing:

```
another window has work out in this project · it has not said which files yet
```

## Can I keep editing while a task is running — who may write while work is out

**Yes, except the files a running task has already written, and except a task running in
place.** A task gets its own checkout of the repository on its own branch, so you and it
are in different directories. A file it has not touched yet is still yours: keep editing,
keep saving. A file it has already written is **held by that task** until it lands. A chat
`edit` or `write` of that file is refused with the task named:

```
cart.py is held by task 2 (discount code entry), so nothing was written.
```

The hold is that one file, not the directory. The write is not routed into the task. You
are not stopped from editing the file yourself in your own editor or shell — this is a
rule about what the chat's own tools will do on your behalf.

**A task running in place is the other exception.** When the directory is not a git
repository — or is one with nothing to branch from — there is no second checkout to give
the task, so it works in your directory. While that is happening the chat becomes a
**reader** of that directory: it can read anything, and a `write` or an `edit` there is
refused with the task named:

```
src/analysis.rs is in the working copy task 4 (repair the parser) is using right now, so
nothing was written.
```

**A quick task is in your folder and does not hold it.** It has no copy of its own, so it
writes where you are — but the folder stays yours: keep editing, and the chat's `edit` and
`write` go on working everywhere else in it. Its claim is **files, not the directory**: a
file it has written is held by it and refused with its name on it, the first case above. A
file it only named at the start is not held against you; what naming does is make a second
quick task that names the same file wait (*Why it said waits for task 5*).

Either hold ends when the task lands, fails or is stopped. *How work runs* has the whole of
it, under *A task that has written a file holds that file* and *A task working in place
holds the directory*.

## What the other-window warning can and cannot see — and why it stayed quiet

**codeaf never tells you that a file is yours alone.** No line means nothing was found, not
that the files are clear: the brief may have named no paths, or the other window may simply
not have written anything yet. Silence here is never an all-clear.

The rest of the limits, plainly:

- **Only paths spelled with a directory on them count.** `internal/tui3/home.go` is a
  place; `home.go` on its own is not, because it cannot be matched against a claim that
  spells its directory. A brief that describes the work without naming a file gets the
  general line above and no guess about where it will land.
- **Claims are about files already written, never files intended.** A task ten minutes in
  that has not saved anything yet is invisible to this check — nothing anywhere records
  what work *means* to touch.
- **Reads are not claims.** A task that read your file and has been thinking about it for
  twenty minutes raises no line. Only writes are recorded.
- **The files everything touches raise nothing.** An overlap that is only in `go.mod`,
  `go.sum`, a lockfile or `CLAUDE.md` is not drawn, because it would be drawn on nearly
  every proposal and the line would become furniture within a day.
- **Closed windows stop warning anybody.** A claim goes stale within a few seconds of the
  window that made it going away, and a stale claim is not believed.

The check runs at the moment work is proposed and never again. Nothing re-checks a task
while it runs, and nothing waits: two windows that decide to work the same file both work
it, and the merge is still yours.

## The states a task passes through — the tiers, what the question mark on a task means, what the word on a task row means, finished but needs your look

Every task row, card, rail line and roster entry answers **one question before it says
anything else: do I need to do anything?** There are exactly three answers, each with its
own glyph and its own word, and the glyph is the tier and nothing else. Learn three glyphs
and you have learned the whole system.

| Tier | Glyph | The word on the row | What it means |
| --- | --- | --- | --- |
| **moving** | `○` queued, `⚑` waiting on something, `◐` working | `queued` · `working` · `waiting on …` · `auto-starts in …` · `finishing` | nothing for you |
| **over** | `✓` · `■` · `✕` | `done` · `stopped` · `incomplete` | nothing for you |
| **your call** | `?`, in the accent colour, always | `your call` | the machine has done what it can, and the card carries the reason and the answers |

**A row never reads a bare `waiting` or a bare `your call`.** The reason travels with the
word, because the reason is the half you can act on: `waiting on task 4`,
`your call · conflicts with your branch`, `incomplete · ran out of steps`.

A run that is standing at its spend gate wears `=` in front of its tier glyph.

**On a terminal with a patched font you get icons rather than shapes.** Every mark above
is a slot in one vocabulary with three spellings — a Font Awesome icon, the geometric
character shown here, and one ASCII letter for a screen reader — and codeaf picks the tier
for your terminal. `/settings` → **step icons** is where you choose `plain` if the icons
draw badly in your font. The state a mark means never changes with the tier.

These are the exact words on screen.

| what is happening | the word you see |
| --- | --- |
| the proposal is still arriving | `⠙ forming… · 6s` after six seconds; under one second there is no clock |
| the proposal is waiting, with no clock | `your call · starts on your word` on the roster and the rail, and `starts on your word` where the card's own meter would be |
| the proposal is waiting, with a clock | `auto-starts in <time>` |
| queued behind something | `queued`, with the reason after it |
| queued behind named work | `waiting on <title of the work it needs>` |
| running | `working`, a turning spinner, and what it is doing this second |
| running, but its calls are being paced | `waiting · <what is holding it>` — and the flag `⚑`, because nothing is happening this instant |
| running and closing a gap | `finishing · <what it is closing>` |
| stopped by you | `stopped`, with `■` on the roster |
| stopped before it ever ran | `stopped before it started` |
| stopped, with work on a branch | `stopped`, and `branch kept` as a fact beside it |
| landed clean | `done` |
| ended without finishing | `incomplete · <the reason, in one plain sentence>` |
| the machine took it as far as it could | `your call · <what it is asking>` |
| cut off while it was being checked | `your call`, over `incomplete — it was stopped while its work was being checked` |

**There is no `failed` and no `needs your look`.** Both were the machine's own words for
things that now have plainer ones: work that ended without finishing says `incomplete` and
why, and work waiting on a decision says `your call` and what it is asking. `awaiting
review` and `unverified` are gone the same way. The engine still calls one of its states
`failed` inside itself; you never read that word.

`finishing` is not a separate state — the work is still running, and the sentence after the
word names the gap it is tying off.

The three reasons a queued task gives for waiting are `slot`, `machine busy` and
`rate limited`. A named prerequisite outranks any of them, because a name is something you
can act on and a queue clears itself.

How a branch came home is a **fact**, not a state: `merged`, `branch kept · <branch>`,
`in your own folder` (there was no branch to bring home — the work edited your own files),
or `conflicted · <branch>`. It rides beside the word, never in place of it.

## Why does it say incomplete — what incomplete means, what incomplete means vs stopped, and the reason beside it

`incomplete` is work that **ended without finishing**, and it never stands alone: one plain
sentence says why. The sentences are a closed set, written down once and read the same on
the card, the rail, the roster and in the chat:

| What happened | What you read |
| --- | --- |
| the connection to the model dropped | `incomplete · lost the connection` |
| the provider would not serve the request | `incomplete · the model provider refused it` |
| the worker repeated itself and its loop guard ended the turn | `incomplete · went in circles` |
| the work it depended on did not land | `incomplete · was blocked by another task` |
| it used up the steps it was given | `incomplete · ran out of steps` |
| it would not write its notes down | `incomplete · would not write its notes down` |
| its brief no longer described the world | `incomplete · its brief went stale` |
| it would not take a step it was asked to | `incomplete · would not take a step it was asked to` |
| a check looked and named what is missing | `incomplete · the check found gaps: <the gaps>` |
| something broke | `incomplete · a fault: <the first line of the error>` |

**`incomplete` is not `stopped`.** `stopped` is *you* ending the work and means nothing else
— no threshold, no loop guard, no rule a worker would not follow is drawn as a stop, and a
task you stopped is never coloured as something having broken. Only the last row of that
table — a fault — is coloured bad; every other `incomplete` row is dim, because running out
of steps is not a thing going wrong.

**`incomplete` is not `your call` either.** `incomplete` is over and asks nothing of you;
the branch it wrote is kept and `continue task N` is the door onto carrying it on.
`your call` is a question with two answers on it.

## A task that was cut off while its work was being checked — interrupted work, killed mid-check, why did my task fail when nothing was wrong with it

A worker's deadline is watched even while a command, backgrounded check or model
request produces no new events. At the deadline, the existing progress check
decides whether to renew the allowance or stop and preserve partial work. Waiting
on the worker's own command counts toward that allowance; waiting for delegated
parts keeps the existing pause rule. An unfinished check is never a passing check.

**A cancel is an interruption, never a finding about the work.** When codeaf quits, a
deadline on the whole session fires, or something outside the task ends it while the check
is running, nobody has looked at the deliverable and nobody has said anything about it. So
the task does **not** land as broken. It lands as **`your call`**, with the plain
sentence:

```
incomplete — it was stopped while its work was being checked, so nothing finished
checking it — what it wrote is on its branch
```

and, underneath it, **the task's own last words about what it did**, quoted. Whatever the
check had already said before it was cut off stands under that. Nothing is merged into your
tree, and the branch is kept, so the work is still there to read, finish or throw away.

**This is not the same as a task that was checked and came back short.** That one *is* a
finding — somebody looked and said what is missing — and it lands `incomplete` with the
gaps in front of it. The difference is whether anybody actually looked: `your call ·
nobody could check it` means nobody could judge it; `incomplete · the check found gaps: …`
means the check did judge it and named the next work.

**And it is not the same as a task you stopped yourself.** Stopping a task from `ctrl+c`,
the roster or `jobs kill` is your decision and is drawn as `stopped`, with the branch kept.

## How work lands — what the card means by merged, branch kept, in your own folder, or conflicted

Every landing writes a card into the conversation, with a blank row on each side, and moves
the task's row on the roster.

```
✓ ◆ Fix nil-map crash · done · 4m12s · 3 files
  "the guard is in and the regression test passes" · started 14:02 · ctrl+o output
```

The head is what happened. The muted line under it is what came of it, in the task's own
first sentence, quoted because they are its words and not codeaf's.

**The quoted sentence is the first line of the report that says something**, not its
literal first line. Work that ran a command, produced a diff or answered in JSON often
opens its report with the code fence around that — the fence is how the answer is spelled,
not a sentence — so a fence marker, three backticks or three tildes with or without a
language word after it, is passed over the way a blank line is. The first line that is
neither is what the card quotes, which for a report that is nothing but a fenced block is
the first line inside it. A report with no line to quote draws no quotation marks at all,
only the start stamp.

**The conversation replies only when its task was started while answering your question.**
That task carries the question until its family lands, then one short reply answers from the
root task’s report. A part does not reply when it finishes, and neither does a check; the
family replies once, when the root lands. If you typed `/task` yourself, you asked for work
rather than an answer, so finishing only shows the landing line and makes no model call.
The same is true when a task started by the conversation carries no question: the landing
line is the whole arrival.

**Every fact on the head is joined by ` · `, the state word included.** It used to read
`done 4m12s`, with the state and the clock fused into one phrase while `3 files` beside
them was properly separated — so on a card asking for a hand, the word and the clock ran
together and the reason it was asking read as part of a duration.

**`started 14:02` is when the work began**, and the word is `started` — it said `spawned`
until 2026-09-03, which is the machinery's own verb for launching a process and not a word
anybody reads on a screen here. A task replayed out of a checkpoint carries the instant it
began, so it says the same `started 14:02` after a restart that it said before one. Where
nothing knows when the work started — a checkpoint written before the record carried the
instant — the stamp is **absent** rather than invented.

- **`done`** — a tick, `✓`, muted. It is settled work on the roster.
- **`stopped`** — `■`. You ended it, and that is all it means.
- **`incomplete`** — `✕`, with the reason beside it: `incomplete · ran out of steps`,
  `incomplete · the check found gaps: …`. Its branch is kept. The row is dim unless
  something actually broke, in which case it reads `incomplete · a fault: <the error>` and
  is coloured bad. **There is no `failed` on any card** — that word is the engine's own.
- **`your call`**: a `?` in the accent colour. On the column it is in the needs-you band
  above all the work, in amber. The `?` is deliberately neither a tick nor a cross: it claims neither a finding
  nor a judgement nobody made. The card carries the reason it is asking on its own row and
  the answers under that — unless you have set `task.settle` to `auto`, in which case the
  reason row says `codeaf is deciding` and the answers are drawn beside it.

After the name the card carries the span, the file count, and how the branch came home:
`merged`, `in your own folder`, `conflicted · <branch>`, or `branch kept · <branch>` —
each its own fact, so a task you ended reads `stopped · branch kept · <branch>`.

`branch kept · <branch>` on a **done** task means the work finished but your checkout was
on a protected branch, was on a different branch than when the work was cut, moved
to a different commit by your own work after the cut, or was detached. The branch
named there holds the finished work; the how-tasks-run page explains the exact reason.
Inspect that branch and keep the delivery workflow you requested. A task finishing
does not by itself request a merge or a checkout change.

Click anywhere on the card, or press `ctrl+o` with it selected, to expand it. `enter` on the
selected card opens the task's room instead. What the expansion holds, and in what order, is
under *What an expanded landing card shows* below. Each long field caps at 20 rows.

**A quick task always comes home `in your own folder`**, with no branch row at all, because
it never had a branch or a copy to bring back. Its card is the state, the clock, the files
if it wrote any, and its own last message as the answer.

The branch row is labelled with **where the work was done**, in plain words rather than in
git's: `a branch of your repository`, `its own copy of the folder`, or `your own folder` —
the same labels the settled card uses, listed under *Does a task touch my working copy?* in
*how tasks run*. A landing whose copy codeaf has no record of falls back to `branch`.

More than two landings in a row become one rollup — `✓ 3 tasks done · 9m14s` with a compact
row per task under it. Any failure in the batch swaps the header to `✕ N tasks landed`; any
`your call` swaps it to `? N tasks landed`. A task you answered straight after it landed is
counted once, by what became of it — the `?` goes with the answer. A delivery that did not land also keeps a
warning on the batch and its individual row. The header's span is wall-clock, first
spawn to last landing, not the sum of the parts, because tasks run at the same time.

## What an expanded landing card shows — why is the task answer in asterisks, Markdown, the delivery warning, the facts

Click a landing card, or press `ctrl+o` with it selected, to expand it. The order is
**delivery status**, **the answer**, then **supporting details**.

A save or integration that did not come off leads with a warning behind `!`, and
diagnostics follow in quieter text, once. It no longer carries a headline of its own:
`delivery needs attention` was a second account of a state the card's own word already
gives, and it is gone. A branch deliberately kept separate is described without a warning
at all; keeping a branch can be the requested outcome.

The answer uses the normal reply renderer: headings, lists, bold, and fenced code
appear as formatted Markdown in body ink. A shortened result names where the rest
can be read: `… the whole of it is at <path>`, or `… the rest of it was not kept`.
If a check turned the result back, the card's reason row already says so — `the check did
not pass it: <the gaps>` — so this block says only the useful half, which is where the
answer can be read. The sentence it used to lead with, `what it produced was not taken as
done`, was a second spelling of the same state and is gone.

Supporting details follow: `changed · <files>`, the branch, then
`model · <model> · $<cost> · ran · 14:02 → 14:14`. Whole facts wrap onto another
row when needed. An unknown price is omitted. With no end time, the card says
`started 14:02`. The acceptance criteria and original brief follow when available.

An expanded card omits the quoted preview and repeated branch from its heading.
Closing it restores the compact summary. Each long field is capped at 20 rows;
open the task's conversation for the full record.

## where do I watch a task while I keep chatting?

A live task has a tab after the conversation tab, named with the task title. Open
that tab to watch its rows while you keep chatting in the conversation. The tab
closes when the task lands; the landing card stays in the conversation.

At the foot of the task tab, type a note and press `enter` to steer the task. The
page says the note was left when it lands. Press `esc` to return to the
conversation.

## Watching work: the strip along the top

The strip is one row under the pinned header — a tab bar of doors into live work:

```
⠙ Fix nil-map · ◆ Auth tests · +2
```

It appears only while something is running, and goes away the moment nothing is. It needs a
frame at least 24 columns wide and 6 rows tall. It is the narrow-frame door: wherever the
roster stands — as the right column or open over the whole frame — the strip stands down.
A column you closed with `alt+l` is a roster standing down, so the strip comes back and
running work stays reachable. The one exception is a running sub-harness: its chip raises
the strip even beside a standing roster, because the roster's rows are tasks and a harness
run is not one — the chip is the only place on the screen that run exists.

A blank line sits under the chips, separating them from the first line of conversation.
It is part of the strip and leaves with it.

Order: running first, then work that needs you, then queued. Waiting and finished work never
appear on it — the strip is the live set, the roster is this session's whole record, and
`/history` is the project's, across every session.

A chip carries one glyph and the name cut to 18 cells, and nothing else: no clock, no
spend, no tool name, no tree connector, no cursor mark, and no stop button. The room you
are standing in takes a colour band. The strip is one flat row even when a task has
children, and so is every row of the roster; a task's own page has its family.

The strip is pointer-only and adds no keys or cursor of its own.

- Click a chip to walk into that task's room. Click the chip of the room you are already in
  to close it.
- Click the `+N` overflow mark to open the whole roster.
- A press anywhere on the row belongs to the row, even in the gaps, so a miss never falls
  through to the transcript.

On a frame too narrow for one whole chip plus its `+N`, the first chip is drawn cut and the
`+N` is dropped: a count of things you cannot identify is worth less than one name.

**Under 60 columns the strip is not chips at all.** It becomes one full-width door —
`▸ 3 tasks · 1 running` — that a tap anywhere opens into the roster page. A thumb cannot
land between chips a few cells apart, so the phone tier trades the tab row for one door. See
*Tasks on a phone*.

## The roster: the column of all the work

The roster is the **Tasks view of the column on the right**. The column is one column in
every chat, with one header, and it reads like this:

```
Tasks 14 · Traffic 3 new                  alt+l
? Port the parser · your call · nobody co…
✕ parser bench incomplete
──────────────────────────────────────────
Running 4
  ⠙ rebase the parser onto dev          2m
  ⠙ port the key table                 40s
Queued 3 ▸
Done 6 ▸
+ /task
```

**The header is the column's two words.** `Tasks 14` is this conversation's work, counted
whole. `Traffic 3 new` is what passes in the team this chat is in; a chat in no team has
only the first word. The word in front is bold ink and the other is dim, and the other one
keeps its count, so traffic that arrives while you read the tasks still says `3 new` on the
header. Each word is a button with a hover ground and a hint (`Show this chat's tasks ·
click`); press the other word to bring it to the front. A count of `0` is dim. At the right
of the header is the column's own key, `alt+l` (`opt+l` on a Mac), and it is a button too:
press it and the column goes away (*Hiding the task column* below).

**Which word is in front is remembered for the session, per kind of chat.** A manager's
chat opens on the Traffic and every other chat opens on the Tasks. Change it in a member's
chat and every member's chat you visit this session opens the same way; the manager's chat
keeps its own answer. The team manager page has the Traffic view.

**Under the header is the needs-you band**, when anything needs you, and nothing at all
when nothing does:

- a task whose next step is yours (a landing nobody could judge, a branch that
  conflicted), led by its own state mark, `?` for your call, in the needs-you amber;
- in a chat in a team, a member's question to you, or a decision put to you, led by `?` in
  amber;
- a task that failed while this window watched it and that you have not opened yet, led by
  its `✕` in ordinary ink. A failure is news, not a demand, so it is never amber.

The band draws at most three rows. A fourth item is drawn as a fourth row, and five or
more show the first three and a `+2 more` row; press it to show them all, and `fewer` to go
back to three. A thin rule closes the band. **An item in the band is not drawn again in
the list under it**, and opening a failure is looking at it: it leaves the band and takes
its place under Done. Each band row opens its thing: a task's room, the member's chat at the
question, or the teams page for a decision.

**Under the band the work is grouped by what it is doing:** `Running`, `Queued` (nothing in
its way but a slot), `Waiting` (behind other work) and `Done`. Running work is always open
and its heading wears no mark, because it does not fold. The other three start folded to
one line, `Done 6 ▸`; press the heading, or `enter` on it, to open the group (`Done 6 ▾`)
and again to fold it. What you open stays open for the rest of the session, through new
work, new landings and switching chats. A group with nothing in it is not drawn at all;
there are no empty headings and no `none` rows.

**Every task is one line**: its state glyph two cells in, its name cut with `…` where the
column is too narrow, and how long it has been at it (or how long it took) in the muted ink
at the right, `40s`, `2m`, `1h`. The time gives way before the name does: it is left off
rather than cut a name that would fit whole without it. Inside each group the newest work
is first. Point at a row and the hint line says the whole of it: the whole name, then what
the row has no room for, the way it used to be written under it (what it is doing, what it
waits on, how its branch came home, what it cost), then `click opens it`.

The roster holds every task **this conversation** has admitted, not just the live ones.
Tasks *other* sessions ran are not on it; the page `/history` opens is the one that has
them, and one dim line at the foot of the column, `ctrl+. earlier`, is the door onto it.
Work finishing never puts the column away, and neither does `/new`: that takes this
session's tasks with it and leaves the column standing.

Under the task list the same column carries `+ /task`, then a section labelled `standing`
(the orders standing over this conversation) and, when this conversation has started any, a
third labelled `jobs`. The standing orders page has that half; *Background jobs on the
column* below has the jobs section. The header and the band stand still at the top while
the list under them scrolls.

**The column is permanent** from 100 columns up: it stands from the session's first
keystroke, before any task exists, and work fills it rather than raising it. It is a
quarter of the frame, never under 28 columns or over 40: 28 at 110, 30 at 120, 40 at 160.
It is the same width in every chat and whichever word is in front, so switching words,
opening a group, a new row and the band appearing never move the conversation beside it.
From 120 columns up `alt+w` widens it by 16 columns, when the conversation keeps at least
56. The one frame without it is the untouched empty conversation, which opens on a centred
greeting and no column at all until you type, a task lands, or a standing order reaches it
(*The empty screen* page). Under 100 columns there is no column and no edge, and `alt+t` (or
`alt+l`) lays the same column over the body instead.

**Workers under a task are rows of their own.** A task that split itself into parts, and an
adaptive run and its workers, each announce themselves as tasks, so each one is an ordinary
row of the list under the group it is in: its own state glyph, its own name, reachable with
`↑`/`↓` and opened with `enter`. The column draws what each piece of work is doing, not a
tree of who started whom; the task's own page (its kin line) and the task page have the
family.

**A run's rows are on this column too.** Under the conversation that started a run, the
column draws that run's tree out of the tasks place's reading: one line per task (the
connector, the state mark and the fitted title) with `waits: <that task>` at the end of a
line held behind named work, and the run's own row ending in the dot row (*what are the
dots next to a task?* has the cells). While a task's worker is on a step, its row spends one
line under it: the running glyph `◐`, the shell lead `$` and the command its worker is on
right now. That page (the page `/history`, `ctrl+.` and `alt+4` open) has the run whole.

## What is the diamond symbol next to each task? — why the sidebar has no diamond, the mark on the cards

**On the column at the right there is no diamond.** A task's row there opens with one cell,
the **state** glyph, which changes as the work does. Then the name, then how long it has been
at it, muted, at the far end. There is no `#7` on the row: the hint line names the task
whole when you point at it. Every row of that column leads the same way, so it reads
downward as one column of states.

The `◆` is still on the surfaces that hold more than tasks: the proposal card, the card
that lands, the notes in the conversation, and a queued task's chip on the strip above the
conversation. It is one marker, the same on every task, saying only that this row is a
piece of work, which is worth a cell where tasks sit among other things and worth nothing
in a column that is only tasks. On a terminal with no colours or no unicode the marker is
`#`.

The marker used to be one of eight shapes in one of six colours, picked from the task's id,
so that a given task wore `◆` teal everywhere. That is gone. It had to be learned, it was
relearned every session because ids start again in each conversation, and it told you
nothing you could act on: the state glyph and its word already say what the task is doing.

**The name is the task's own title, cut to its first three words** (`Fix the nil-map`,
`Collect the sources`), and that is the name it wears everywhere: the column, the strip
above the conversation, its room's header, the card that lands, the home card and the task
page. Three words is also what the `taskname` call is asked for, so a named task fits the
column whole rather than being cut to fit it. A row reading **`task 19`** means one thing
only: nothing has told codeaf what that task is called yet. It is a name you can still say
out loud, and the row takes the real one the moment the title arrives, including a room you
already have open on it. **Nothing is drawn under a row.** What it is doing, what is holding
it, what it waits on, or how its branch came home (`conflicted · task/fix-nil`) is on the
hint line while the pointer is on the row, and a branch that needs you is in the band at the
top of the column.

**The row of the room you are standing in is picked out.** Walk into a task (from the
roster, a strip chip, a spawn card or a `task 7` link) and that task's row in the column
takes a colour band across its whole width, with its title in the accent and bold. It is the
same mark the strip puts on the chip of the room you are in, so the two lists of the work
never disagree about which door you went through. It follows you: opening another task's
room moves it, and `esc` back to the conversation clears it. With no room open no row is
marked at all. On a terminal with no background colours the accent title is what is left
of it.

There is no subtitle here. The column is a presence list; the proposal card and the landing
card both carry the sentence.

Under the task rows, one dim `+ /task` row closes the list; press it and `/task ` is typed
into your message box. Then a blank line, then the column's `standing` section. There are
no state totals at the foot: the header counts the tasks and each group's heading counts its
own. The session's spend and tokens are on the status row. When anything stands over this
project, a `◦ 2 standing orders` line follows and opens `/standing`.

Below those are up to two door lines: `ctrl+. earlier` when the full-screen page holds work
this column does not, and, while the column holds the keyboard or the pointer is on it,
`alt+w widen · click seam` from 120 columns up. The column's own way out is the `alt+l` at
the right of its header.

**Non-running rows are drawn quieter.** A running task's name is in the ordinary text
colour; queued, waiting and finished names are muted, headings are muted with dim counts,
and the room you are standing in is the one row in the accent. A glance at the column lands
on what is moving.

## Background jobs on the column — the jobs list on the right, what is running in the background, why a long command shows on the right, the row for a server, build or watch

**A background job is not a row among the tasks.** It used to sit in the roster with the
tasks. It is now a **third section** on the same column, under the tasks and `standing`,
labelled `jobs`. This covers everything the `jobs` tool can list except a task's own
worker, which already has a roster row of its own:

- a command run with `bash background:true` — a server, a long build, a sweep
- a foreground command the `background after` clock kept running as a job
  (`still running as job 3`), or one that reached its own timeout sooner,
  including one you sent there yourself with `ctrl+g`
- a `watch`, whose row reads `watch <name>`
- a video or music render, which is a job while it renders

**Collapsed is the default, and collapsed it is one line.** A fold mark leads it — `▸`
closed, `▾` open; on a terminal that cannot draw those, `>` and `v`. Enter or a click on
the label toggles it. The shapes:

```
jobs · 2 running
jobs · 1 running · 4m12s
jobs · 2 running · 6 ran
jobs · 6 ran
```

The clock appears **only when exactly one job is running**, because then there is one
duration to name. Past that the count is the news.

**Expanded**, every running job draws (oldest first), then finished ones fill whatever
room is left, newest first, and the remainder is counted on an `N earlier` line rather
than dropped. That remainder line is a count, not a door: it opens nothing, and it wears
**no fold mark** for exactly that reason — it used to read `▸ 4 earlier` under a section
whose head is also a `▸`, which invited a keypress that did nothing. It sits at the rows'
own indent, under the rows it is counting.

**Zero jobs draws nothing at all** — no label, no empty row, no `0 jobs`. A conversation
that has started none looks as it did before jobs had a section.

**`/new` and switching conversations drop the section.** A job belongs to the conversation
that started it. Coming back, or reopening it, redraws the jobs it ran, settled: a job
still going when codeaf closed comes back `stopped`. Nothing is restarted. The log stays
under this conversation's folder, in `logs/jobs/` — never in your project.

The `jobs` tool, `jobs output N`, `jobs kill N`, `ctrl+g` promotion and the
`still running as job 3` sentence are all unchanged. To kill a server you started,
open its row's page and press `x`.

## Why is my job called that — a job is named in three or four words, not the command cut to three words

**The name is three or four words from a cheap model**, not the command cut to three
words. Until the name arrives the row wears the command itself — `npm run dev` — and then
**renames in place**. The name is a display name; `job 3` is still the handle
`jobs output 3` and `jobs kill 3` take.

The namer is a small, cheap call (the low tier, the same kind of errand that names a
session or a task). It never blocks the work: the process is already running. A name that
never arrives costs a good name and nothing else — the command stays on the row.

**No call is made** when the job already has a label of its own, or when the command is
already short and readable:

- a watch or a render is named the moment it starts (`watch app`, the render's
  title) — a second call would disagree with `jobs list`
- a command with no shell metacharacters and no more than four words is left alone —
  `npm run dev` is what a person would call that job

A command that is a script — a pipe, a quote, a dollar, or more than four words — is the
one the namer is asked about. Four words is a cap, not a target: a two-word name is left
at two.

## What a job's row shows — exited 1, stopped, done, how a job ended, the clock while it runs

**A job's row shows its name on the left and, on the right, its number and either
the clock or how it ended.** The number is the handle — the same `3` in `job 3`,
`jobs kill 3` and `job:3`. The shapes:

```
3 · 4m12s
3 · done
3 · exited 1
3 · stopped
```

A running job's figure is the clock (`4m12s`). A clean finish reads `done` — how
long it ran is on the page, not restated here, because a frozen clock and a
ticking one are the same shape at a glance. A non-zero exit reads `exited 1`. A
job somebody ended reads `stopped`.

**The page says the same word the row does.** Open the row and its first line reads
`job 3 · exited 1 · ran 49s`, `job 3 · done · ran 12s`, `job 3 · stopped · ran 51m 12s` —
one function behind both, so the section you pressed enter in and the page that opened
cannot say two things about one job. The page used to print the engine's own name for the
state (`failed`) and then repeat the code behind it (`exit 1`); it does neither now. Under a second there is no clock on a
running row — `0s` on a row that has just begun would be a figure that has to be
read to learn nothing — and the number still stands alone.

A running name is drawn in the column's working ink; a settled one is muted. That
is the same brightness the roster already uses for live work versus history.

**The absolute log path is off the row.** It used to sit under the name as
`job 3 · log /…/3.log`. It is on the job's page now. Open the row (enter or a click) to
see it.

**A running job still counts as working** in the live-work tree. The jobs section's own
label is the count of *jobs*: `jobs · 2 running`.

**What a job's row does not have**, because a job has none of them:

- no branch, no changed-files list and no price — a job runs in the workspace itself, and
  nothing measures what it costs, because it costs nothing but time
- no agent inside it and no transcript — enter opens a **page**, not a chat
- no `✕` on the row itself — stop it from the **page** (`x`), or ask codeaf to run
  `jobs kill`

**No card is written into the conversation** when a job ends, because a job has no report
anybody wrote — what it left is its log. codeaf itself is still told, on its own side, at
the next step boundary (`job 3 exited 1: make: *** [build] Error 1`).

## How do I stop a background job — stop a job from its page, can I stop a background job from the sidebar

**Yes. Open the job's page and press `x`.** Kill a server you started the same
way. A job used to have no stop on the surface, and the only way was to ask
codeaf to run `jobs kill`. The row itself still has no `✕` — walk into the jobs
section, enter on the job, then `x`. That is how you stop it from the sidebar:
the section is the door onto the page, and the page is the door onto the stop.

`x` raises the same confirmation every other stop on this surface raises, with the cursor
on the safe answer. **The page steps aside for it** — the card is drawn above the message
box, so the log page closes and the question comes up in the conversation, where the
engine's own sentence about what stopped lands right under it. The job's row in the column
opens the page again:

```
?  Stop this job?
     The process is ended; its log is kept.
     1  stop it
     2  keep going
   enter take it · esc keep going · ←→ choose
```

`1` and `2` move the cursor onto the answer they name; `enter` is what decides.

`enter` on `stop it` ends the process. The engine's door is `job:3` — the same number the
handle shows. The line it answers with is `stopped job 3 (the name) — its log is kept`.
The model is told `job 3 was stopped` so it does not keep reasoning about work that is no
longer running. Pressing `x` on a job that has already ended does nothing: the settled
page does not offer the key.

**You can still ask codeaf to run `jobs kill N`.** That is the model's tool and it still
works. Every running job is also killed when the conversation closes. `esc` in the
conversation interrupts the turn and does **not** stop background jobs.

## Opening a job's page — where is a job's log path, a job is not a chat, copy the log path

**Opening a job opens a page, not a chat.** It is a full-frame card: the name (or
`job 3` until the name arrives), the handle and clock or ending on the next line, the
command in full, the log tail, and a foot. There is no composer on the page at all, so
there is nothing to refuse. The title is never the raw command — that lives once, in the
body — so the number is always visible in the head. The old feet
`this log grows as the job works — say it to main` and
`a background job keeps a log, not a transcript` are gone.

The keys, quoted:

- running: `x stop it · c copy path · m puts it in your message · ↑↓ scroll`
- settled: `c copy path · m puts it in your message · ↑↓ scroll`

**The way out is not on those feet, because the head is already saying it.** `esc back`
sits in the head's right corner, and a page that named the same instruction twice was
spending two of its words repeating itself. Where a long name takes the whole head line
there is no corner left, and the foot takes the way out back — last, so a narrow frame
spends it last: `x stop it · ↑↓ scroll · c copy path · m puts it in your message · esc back`.

`c` copies the log path; the confirmation begins `copied `. `m` drops the name, the
handle, the ending, and the last few log lines into your message box underneath, then
closes the page. Over `--host` the body says `its log is on ` plus the host name, because
the file is on the engine's machine; `jobs output N` is how you read it there.

The log tail is the last **200** lines, re-read four times a second while the job runs,
and one last reading after it ends. Colour codes and control characters are stripped. A
job that has written nothing yet draws no tail and no error.

## Stray lines painted over the conversation, the screen glitching while a job runs — a job cannot draw on your chat

**A job cannot draw on your screen.** It runs in its own terminal session, away from the
window you are looking at, so a command that tries to open the terminal directly — a CLI
that is itself a full-screen program, a prompt that insists on the keyboard — is refused
by the system rather than painting its output across your conversation. Everything a job
says goes to its log and nowhere else; if the chat's own frame ever glitches or shows
stray lines, it is not a job doing it.

## A task started from the composer carries a cap — how much a task may spend before it asks, how do I set a spend limit on a task before I send it

A task started with **`alt+enter`** from the composer on home or on any other place goes out
with a **spend cap** on it. The composer layer's third line is where you read it and where
you change it:

```
 · it may spend up to $100.00 before it asks                                    type a number
```

**The default is $100.00**, which is the tank codeaf applies to work nobody put a figure on.
Type digits while the layer is up and the figure is whatever you typed.

**It is a real limit and not a caption.** The figure becomes that errand's own spend rail:

- The errand **stops before its next turn** once its own accumulated spend reaches the
  figure. It never cuts a turn in half — a turn with tool calls in flight finishes — and it
  says which figure it stopped at.
- Any **adaptive run** the errand starts is held to a tank no bigger than the cap, even if
  something asks for more. When that tank empties the run **finishes what is in flight,
  starts nothing new, and asks you** — top it up, finish on what is done, or stop. That gate
  is the *asks* in the sentence on the line.

**A task started any other way carries whatever this window carries.** `/task <brief>`, the
proposal card and the model's own proposals run under the conversation's own limit — the
`per conversation` row on the **Spending** tab of `/settings`, which reads `no limit` until
you set it — and under the day's limit above it. An adaptive run they start opens on the
$100.00 default.

**A task has no dollar limit of its own**, which the Spending tab says on its `per task`
row in those words: `no limit of its own · it spends against the day and this conversation`.
Its own bounds are steps and time. The composer layer's third line is the one place a
figure is put on a single piece of work, and there is no per-task money row to edit
anywhere in settings.

Changing the engine's default changes the figure the composer layer opens on; the two are
meant to be one number and are stated in both places on purpose.

## The + /task row at the foot of the column — starting a task from the side

Under this conversation's task rows the column on the right carries one dim row:

```
+ /task
```

**Pressing it types `/task ` into your message box** — the word and a trailing space — and
hands the keyboard straight back to the box. It starts nothing, opens nothing and arms
nothing: what lands is ordinary text you can edit or delete, with no mode and no form
around it.

- **The word goes at the head of the line and keeps what was already typed there.** A box
  holding `fix the flaky test` becomes `/task fix the flaky test`, which is the line you
  were about to type anyway.
- **Pressing it twice does nothing the second time** — the word is already at the front.
- It is drawn whether or not this conversation has run anything, under the Tasks view's
  last row, and it is the **pointer's** row: the roster's keyboard cursor (`alt+t`) walks
  the band, the headings and the task rows and skips it. From the keyboard you type the command, which is what the
  row is teaching.

Finish the sentence and send it and it is `/task <brief>` like any other: codeaf sizes the
work, shapes the brief and starts it. The column's other section, `standing`, ends in a
`+ /standing` row that works the same way.

## What a bare /task does — /task with nothing after it opens the task page

**`/task` typed on its own opens the full-screen task page** — the same page `/history` and
`ctrl+.` open, holding every task this project has ever run. It used to print a one-line
usage instead. It does not any more.

The reason is the `+ /task` row at the foot of the task column: that row puts `/task ` in
your box before you have said what the work is, so a `/task` sent as it stands is asking
the only question the word can answer with no brief behind it — *what work is there.*

**On a project that has never run a task it opens the page anyway**, headed `sessions` over one
line — `work you send off with /task lands here, and its record stays` — which is exactly
what `/history` and `ctrl+.` do there too.

**The forms that start work are unchanged.** `/task <brief>` and `/task solo <brief>`
still size, shape and start the work directly, with no proposal card in between and no
extra question. There is no third form: `/task adaptive` is retired.

**There is still no `/tasks` command**, though `sessions` is the name of the PLACE `/history`
opens, `alt+4` and `tab` get there without typing anything. As a slash word the plural is not one this surface answers to;
the two things a bare `/task` and a `/task <brief>` do are the pair of errands a person has
about tasks — go and look at the work, or give codeaf some.

## Old tasks from previous sessions are not on the column — the `ctrl+. earlier` door

**The column is this conversation's work and nothing else.** No rows of earlier sessions'
tasks are drawn under it. What sits at the foot of the column instead, whenever the project
has a record this conversation never ran, is one dim line:

```
ctrl+. earlier
```

- **It is a door and not a note.** Press `ctrl+.`, or click that line, and the full-screen
  sessions place opens with every task this machine has run on it, grouped by what you do
  next: `running` and `completed`. `/history` is the same page.
- **It is drawn only when there is something behind it**, and never as `0 earlier` or any
  other count of nothing. There is only ever one such line.
- **A task of your own is not behind it twice.** A task this conversation ran, running or
  landed, is on the column above under its group; the door is offered for work this session
  never ran. A group folded to its heading is not a reason for the line: its heading opens it
  in place.
- It is dim, and it is a button as well as a key.

**The column used to footnote the record**: up to six dulled `earlier` rows under this
session's work, walkable with `↓`, each opening a card. They are gone. Six rows out of a
record that runs to two thousand is a sample, and the cursor walked out of this
conversation's work into another one's without the column ever saying it had. Everything
they offered is on the other side of the door, whole: every row, the filter, the cards, and
`m` for the mention.

**Where old work is listed now:** the task page (`ctrl+.`, `/history`, or that line), and
home (`/home`, or Escape from the conversation). The chat can also read the whole project
record for you with its `tasks` tool; just ask.

**Running work in another codeaf window** is on no surface but the task page. An ordinary
task writes nothing into the project's file until it lands, so the window next door is the
only place that work can be read from, and `/history` is the page that reads it.

## The column comes back after a restart — tasks reappear on resume or a switch back

**Reopening a conversation re-draws its own tasks, and the jobs it ran.** `/resume`,
`codeaf resume`, and switching back behind home all rebuild the column from the record:
every task admitted here comes back as a row under its group (finished work included, and
work that was interrupted comes back saying so on its card), and the `jobs` section
redraws the jobs this conversation started, settled. Tasks and jobs are not lost when the
terminal closes; the column is rebuilt, not carried. **Quick tasks are in that too** — a
finished one comes back `done` with its answer, and a conversation that ran them is never
left with an empty column, which it was until this was fixed (*A quick task after a
restart* has the whole of it).

A failure that comes back this way is under `Done` and not in the needs-you band: it is
news from before you left, and the band carries only what failed while this window watched.

The rebuilt row reads its start and landing times from that same record. Work that landed
in a previous session therefore keeps the time it actually landed instead of taking the
time you reopened the conversation. A record made before those times were kept still
reopens; its row leaves the age absent and its completion card omits the entire
`started 14:02` segment.

**Only this window's own work comes back.** Everything else stays behind the
`ctrl+. earlier` door, exactly as the section above says, and a task is never drawn
twice — a row on the column is not also an `earlier` row.

## Hiding the task column: closing the right sidebar, panel or task bar

`alt+l` (`opt+l` on a Mac) closes the column on the right and gives its columns back to the
conversation. Press it again and the column comes back with the current state of the work
in it, including anything that started, finished or arrived on the Traffic while it was
gone: nothing here is a snapshot; the column is redrawn every frame. It is the same key in
every chat, whichever word is in front. `ctrl+g` does the same when no foreground command
can be kept; while one can, `ctrl+g` keeps the command instead and the column stays where it
was.

**The pointer can do the whole cycle on its own.** The column's header ends in the key
itself, `alt+l`, dim, at its right; it takes a ground under the pointer, the hint line says
`Hide this column · alt+l`, and a press closes the column. What is left behind is a thin
edge carrying `❮`: click that and the column comes back. So a closed column is never a thing
you need to know a chord to recover. See *The task bar disappeared* below.

The choice is remembered. It is written to your profile the moment the column moves, as
the `ui.task_column` setting, which also appears in the settings panel (`ctrl+,`) on the
Display tab as **task column**. A change made in the panel lands the next time codeaf
starts; `alt+l` acts immediately and wins for this session. On the teams page the column
starts folded, because that page has a rail of its own on the left, and `alt+l` there opens
it for that visit only.

With the column closed, work is still visible:

- Anything **running** draws the task strip along the top (`⠙ Fix nil-map · ◆ Auth tests
  · +2`), because the strip stands up wherever the roster stands down. Click a chip for
  that task's room, or the `+N` for the whole roster.
- The legend's hint slot under the message box carries `alt+l tasks` (`alt+l traffic` in a
  manager's chat) for as long as this session has any tasks at all. A session that has run
  nothing says nothing there; the column you closed was empty, and `alt+l` still brings it
  back.
- `alt+t` still works: asking for the roster brings the column back and gives it the
  keyboard in one press.

`alt+l` works whether or not the session has tasks: the column stands empty, so an empty
column is still a column to close. Under 100 columns, where there is no column beside the
conversation, it lays the column over the body instead, and a second press takes it off.

## The task bar disappeared — how do I get the task column back

A closed column does not vanish without a trace. It leaves a **thin edge two columns wide
down the right of the frame**, with a `❮` handle at the middle of it, drawn in ordinary ink
rather than dim so the eye can find it:

```
 …and the parser suite passes now.                                       ❮
```

**Click anywhere on that edge and the column comes back**: the whole strip is the door, not
just the handle, so there is nothing to aim at. `alt+l` does the same from the keyboard.

- Under the pointer the handle brightens further and the whole two-cell strip takes a
  background, which is how everything pressable on this screen says so. The hint line says
  `Show this column · alt+l`.
- **One cell above the handle says what the work is doing**, while there is anything worth
  saying: `?` in the needs-you amber when a task or a team member is waiting on you, `◐` in
  the accent when something is running. Nothing at all otherwise: a session with nothing
  running and nothing waiting leaves the edge silent.
- **In a chat in a team, two cells below the handle count what arrived on the Traffic**
  since you last had it in front of you, dim, up to `99`. The hint says it too, `3 new in the
  traffic`.
- The edge costs the conversation two columns, exactly as the column it stands for costs it
  its own width. The text re-wraps; nothing is ever drawn underneath it.
- **On a frame narrower than 100 columns there is no edge**, because there is no column at
  that width to bring back. `alt+t` or `alt+l` lays the column over the whole frame.
- On a terminal that cannot draw `❮` it is `<`.

The legend above the message box also carries `alt+l tasks` while the column is away and
this session has run something.

## Using the roster from the keyboard

`alt+t` (`opt+t`) hands the keyboard to the column. It is asked for, never taken: the draft
is the rest state, so a person who starts typing is typing, not navigating. The cursor starts
on the first row that is work: a band row if there is one, else the first task.

| key | what it does |
| --- | --- |
| `↑` `↓` | walk the rows: the band, the group headings, the tasks, then the jobs |
| `enter` | on a task, open its room; on a folded or open heading, fold or open that group; on a band row, open its thing; on a job, its page; on the `jobs` label, toggle the section |
| `←` `→` | in a chat in a team, switch between Tasks and Traffic; the cursor goes to the first row of the other view |
| `x` | stop the task under the cursor, where it can be stopped |
| `alt+w` | toggle the column 16 columns wider (bare `w` only over the full frame) |
| `esc` or `alt+t` | give the keyboard back |
| `alt+l` | close the column altogether, or bring it back; this one works whether or not the column holds the keyboard |

The legend hint while it holds the keyboard is `↑↓ move · enter open · x stop · alt+w wide ·
esc`, led by `←→ tasks/traffic · ` in a chat in a team. On a row waiting on your look the
slot says that row's answers instead. The footer adds `alt+w widen · click seam` (or
`alt+w narrow · click seam` once it is wide); that hint is clickable as well as available
from the keyboard. **Both the hint and the seam appear only from 120 columns up**, which is
the only width that lends a wider column; on a 100-to-119 column frame the footer says
nothing about widening and the column's left edge is part of the row, not a handle.

Every other key is given back. The column cannot take the keyboard while the exit
confirmation, a permission question, a task proposal, or any overlay is up, and with
nothing on it to put a cursor on, `alt+t` falls through rather than being swallowed,
including in a directory whose earlier sessions ran plenty. The door `ctrl+. earlier` at
the foot of the column is how that work is reached. A session that has only started a
server still has the jobs section to put a cursor on, so `alt+t` takes it.

**The walk stops at this conversation's last job, after its last task.** `↓` walks the band,
the groups and then the jobs section under them, and clamps there rather than carrying on
into the project's record. Old work is walked on the task page (`ctrl+.`), where `enter`
goes inside its card.

The cursor follows the task, not the row, when a task moves from one group to another. If
its group is folded, the cursor rests on that group's heading.

With the pointer, a row takes the hover background step. The next section has the whole of
what a click on the column does.

## Clicking the tasks on the right — I cannot open a task from the sidebar, clicking a row does nothing, the list jumps instead of opening

**Anywhere on a task's row opens that task's room.** The state glyph, the name, the time,
the blank cells between them: the whole visible row is that one door, at every width the
column stands at, and the hover ground covers exactly that row. A click moves the column's
cursor to what was clicked but does not hand the column the keyboard; the draft is still
where you type.

The other things on the column that answer a press say so by lighting under the pointer:

- **A group's heading** (`Queued 3 ▸`, `Done 6 ▾`) opens or folds that group. The hint line
  says `Show what is done · click` or `Fold what is done · click`. The `Running` heading does
  not fold, wears no mark and does not light.
- **A band row** opens its thing: a task's room, the member's chat at the question, or the
  teams page for a decision put to you. `+2 more` shows the whole band and `fewer` folds it.
- **The header's words.** In a chat in a team, the word that is not in front brings its view
  to the front; the `alt+l` at the right closes the column. The space between them lights
  nothing and does nothing.

There is no disclosure triangle on a task row and no `▸ +N` badge: a task row is never a
fold. The folds are the group headings, and a press on a task's state glyph opens the task.

**Two other cells used to swallow presses and no longer do.** The column's two leftmost
cells are the resize handle only from 120 columns up; on a narrower frame there is no wider
column to pull to, so those cells are part of the row like any other. And the column answers
only for the rows it actually draws: a press below its last row belongs to the message box,
the legend or the status line under it, not to the column.

A click inside the column that lands on no row at all still belongs to the column and does
nothing, rather than closing the task page you are reading.

**A second click on the selected row keeps its task open**, with the same draft and
reading position. Press `esc` or the back control to return to the conversation.

## What did we do last week — why is my old task not on the tasks page, an old task says now, and a task from a previous session is missing from the tasks page

This is where **task history** lives — every **old and past task**, and **work from other
sessions**, on one page.

`/history`, or `ctrl+.`, opens the machine-wide **sessions** place holding work from every project
run** — this conversation's and every conversation's before it. It is the answer the roster
cannot give: the column beside the conversation is built from *this session's* work and
nothing else, so a task you ran last week, in a session you have closed, is nowhere on the
screen until you open this.

**There is no `/tasks` command** — though `sessions` is what the PLACE this opens is called on
the tab bar, reached with `alt+4` or `tab`. `/task <brief>` starts work; `/history` opens the same sessions place
started — and so does a **bare `/task`**, which opens this very page rather than printing a
usage line. The page is also reached from the one dim door line at the bottom of the task
column, `ctrl+. earlier`, whenever the project has work this conversation never ran.

The page takes the whole frame, the way the settings panel does. `esc` closes it. Three
pages here take the frame — the settings panel, this one, and `/home` — and **only one of
them is ever up**: opening any one closes the other two.

It opens on one heading saying what it is holding. A page holding conversations reads, for
example, `15 chats · 13 subtasks · $2.98`: chats first, then the work under them. A
page whose conversation metadata is unavailable still groups its tasks under a conversation
row and reads, for example,
`1 chat · 148 subtasks since aug 11 · $34.10`. The count is every row the time window
holds, the date is the far edge of that window, and the money is what those rows are known
to have cost. A window with no known start drops the `since`, and rows nobody priced drop
the money: zero means "nobody published a price", never "free". **A frame too narrow for
the whole line drops the money clause** rather than cutting it, because half a figure is a
wrong number.

It used to be a paragraph — `work codeaf ran on its own. 148 pieces of work since aug 11,
$34.10 between them.` — which was the widest thing on the page, taught the machinery's own
idea of itself before naming anything, and pushed the rows a person came for further down.
The place's name, its count and its window are what the line is for.

**The count is a claim about the PLACE and never about what you have typed.** With a filter
on, the sentence goes on counting every row the window holds — a word that matches nothing
does not make the machine's history empty. What matched is said on the line under the
list instead: `nothing matches`. The words you typed are on the control row at the top of
the list, where you typed them.

**When the time window holds none of it, that line says `nothing since jul 29`** —
in words, because a `0` there is the figure the emptiness law forbids, and with the date
still on it because the date is what says the window is the reason. The sentence stays on the frame in that state: it is the only thing naming the
window the four shift-arrows move, so a page that replaced it with the teaching prose
would have swallowed the way back. The teaching prose is for a machine that has run
nothing IN ANY WINDOW, which is a different screen.

Under it are only **running** and **completed**, with a conversation at the top of every
tree. A chat belongs to running while it is answering or has running, queued, waiting or
unanswered work. Otherwise it belongs to completed. Its entire tree moves together as
work starts and finishes; completed children stay beside their active siblings.

Both sections default to **newest activity first**; click `age ↓` to reverse them, using the newest recorded conversation
or task timestamp. Nested siblings follow the same direction. The time window selects whole
conversations, retaining older parents and children instead of splitting their trees.
Filtering never moves a conversation into a different section.

Conversation names use the full title shown on Home and truncate only to fit. Their
bullets also match Home: dim at rest, working while answering, bright for unread replies,
and a question mark when an answer is needed. A missing transcript still leaves its tasks
under an identified conversation row; nothing is promoted into a top-level task.

Each row is one line: a state glyph, then **the name, whole**, and then a dim tail of facts
joined by ` · ` — how long ago, the conversation or project it came out of, what it is doing
or what it came to, and last what it cost.

**The tail is ordered by what you would act on**, and it is spent from the right, so the
cost is the first thing a narrow frame gives up. It used to sit second, in front of the
conversation and in front of the state, and it is the only fact on the row with a colour of
its own — so `$3.10` was the loudest thing on a row whose state word had been cut off to
make room for it. The row's **kind** is no longer drawn at all: `adaptive` beside `18 of 40`
was an implementation word competing with the progress you were reading, about a choice made
before the work started that changes nothing you can do now.

**The name keeps every cell it asks for before a fact gets one**, and is shortened only
where the frame cannot hold it alone; the facts behind it are dropped from the end as the
frame narrows, and a long sentence of detail is said shorter (`2 files`) before it is
dropped. So a narrow terminal shows the same facts a wide one does, with the end missing —
never a different tail, and never a name you cannot match.

A row another window is running says `another window`, with that window's own name after it
when it has settled on one. A row that still claims `running` with **no** window behind it
says `incomplete` and **carries no age at all** — nobody judged the work, the window simply
went, and nothing in the record dates a row that never landed. Work that did not come off
says the same word its own page says, with the reason after it: `incomplete · a fault: the
package manager refused the archive` when the run actually broke, `incomplete · <the
reason>` when a check named what is missing or the wire, a limit or a stale brief ended it,
and `stopped` when you ended it yourself. All of them wear `✕` except the stop, which wears
`■`; **only the fault is coloured bad**, because nothing was found wrong with the rest.

**How long ago a settled row landed is what that row's own record says.** Work reopened
from a previous session is dated when it LANDED, not when you sat down and reopened it. It
therefore stays in the time window it belongs to instead of being filed in the future and
going missing from the page, and it does not say `now` merely because this is your first
look at it today.

A row whose older record never recorded a landing time **carries no age at all**. The row
is still on the tasks page: for deciding whether to include it, the page files that undated
row at the moment of the reading. For the words you see, it draws nothing where the age
would go. It never turns an unknown time into `now`.

**The sections are in time order, newest first**, and a family stays whole: the root is
placed by its own stamp and the workers stand under it in theirs.

**A section with nothing in it is not drawn at all**, heading and all. **The sections are
separated by a blank line** and by nothing else — no rule, no dashes, no alternating
background.

**One piece of work is drawn once**, however many places know about it: the project's file,
this window's own live work, and the window next door are read together and joined on the
conversation and the id, with the freshest of them winning.

**A family can be folded away.** Where it is, the section's own heading says so —
`completed · 3 folded away` — and `→` opens it. Everything else the window holds has a
line, and the page scrolls —
`↑`/`↓`, `pgup`/`pgdown`, `home`/`end` and the wheel all walk it. **The last rows fade** when
the list runs on below the bottom of the window: three rows, each a step fainter, saying
there is more under them. The row the cursor is on never fades wherever it sits, and a list
short enough to fit fades nothing at all — see *Why the bottom rows of a long list look
dimmer* on the screen page.

## Why completed tasks can appear in the running section

Sections describe whole conversations, not individual tasks. A finished child stays under
its conversation in running while any sibling is still active or awaiting an answer. Each
task keeps its own state word. When the last active task settles, the complete conversation
tree moves into completed. New work can move it back again.

The count above the filter is the number of chats and subtasks in the selected time window;
it does not shrink as you type a filter. All levels start expanded. Folding a branch by
hand adds the number of hidden rows to the section heading as `folded away`.

**Every door onto this place opens it, on a machine that has run nothing too.** `/history`,
a bare `/task`, `ctrl+.`, `alt+4` and `tab` all reach the same page, and with nothing on it
the page is its heading and one line naming what arrives there, instead of counts:

```
tasks
  work you send off with /task lands here, and its record stays
```

`/history` used to say one sentence **instead** of opening, with `ctrl+.` doing
nothing at all rather than raising an empty page. On a machine codeaf was installed on an
hour ago that was every door onto the page, so the first thing anybody tried appeared not to
work. The sentence stayed and moved onto the page it is about. A session that has run
nothing itself still shows work another window on the same directory is running — that is
the one fact it was opened to report.

The list is as long as the record is — internal to codeaf each project's record keeps the
most recent 2000 tasks — and the page scrolls rather than cutting it.

## Work running in another codeaf window — a task started in my other terminal, I cannot click into a running task, open a task another window is running, why is that task page read only, the task page says it cannot ask what the work is doing, can I read another conversation's task over ssh

Two codeaf windows open on one directory can see each other's running work, and `/history`
is where they see it.

**A conversation *this* terminal is holding is never one of them.** One terminal can have
several conversations open at once with `codeaf chat --no-host`, in this project or others,
and every one of them writes
the same file the rows below are read from — so they are filtered out by name before the
page is drawn. Telling you to go to a window that is two keystrokes away in the terminal
you are already sitting in would be the same wrong refusal home used to make about another
project. Those conversations are reached with `tab` or from home; the count of them is on
the status line as `2 open`. Under the place's own `running` word the page draws **one row for
every task each other window has out right now**, beside this window's own:

```
running
◐ Sweep the call sites                              another window · Fix the nil-map crash
○ Port the parser                                   another window
```

- The right-hand note is dim and says `another window`, followed by that window's own name
  when it has settled on one. A window nothing has named says only `another window`. A row
  whose conversation **this terminal** is holding says `open in this terminal` instead.
- The row's glyph is the task's own state, so `◐` is running and `⚑` is waiting behind
  something.
- **These rows are pressed, and every one of them opens something.** The cursor stands on
  them like any other row of work, `enter` and a click are the same door, and which door it
  is depends on where the work actually is. The foot always names the one you have.

**`enter go to that conversation`** — the work belongs to another conversation **this
terminal** is holding. Pressing it switches to that conversation, standing in that task's
own room. With `--no-host`, the conversation you were in goes on running, exactly as it does for any other
switch, and `tab` comes back.

**`enter read it as it runs`** — the work belongs to a conversation the engine is running
that this window can join. Pressing it opens **that task's own transcript**, live, updating
as the work goes. The trail at the top reads `reading in <that conversation>` so nothing on
the page can be mistaken for this conversation's own work. `esc` returns.

This page is **read-only**. The keyboard for that task belongs to the window that owns it,
so the message box says `Reading this task… (esc: main)` and sending anything answers
`this window is reading this task — go to the conversation that owns it to steer or stop
it`, with your words still in the box. There is no stop, no model change and no effort
change on it either: those act on work, and this window is looking at somebody else's.
Nothing about opening it takes control away from the window that owns the work, and closing
the page gives the connection back.

**When the work on a reading page lands, the foot names the owner and never main.** It
says `this task has finished — say it in docs pass` — the owner's own name, the same one
the trail carries — and `this task has finished — say it in the conversation that owns it`
where nothing has named that conversation. `say it to main` is what an ordinary finished
task's page says, and it would be the wrong sentence here: main is this window's own
conversation, whose task of the same number is different work. The box goes on saying
`Reading this task… (esc: main)`, because reading is what the page still is.

**What the page says about the work is what that conversation says about it.** The reading
opens a standing subscription to the owner's own task list — the same one every window
attached to a conversation gets — so the state on the page is the owner's, live: work that
lands while you are reading it stops saying it is running, and the page stops asking for more
of a journal that has ended. Nothing is inferred from files on this machine. If the door
that opened the page cannot offer that subscription, the page still shows the transcript and
adds one line saying so: `this window cannot ask that conversation what its work is doing
now — the state above is what the list last said`.

**If that window opens something else while your page is opening, the page does not open.**
The engine refuses rather than handing you whatever is open there now, and the tasks page
says so — nothing is drawn under the wrong name, and nothing is taken from the window that
owns the work:

```
could not open that conversation · engine: that conversation is not open here any more
```

**And it works where the engine is local.** `--host` and `--at` do not draw rows for other
conversations at all — the presence those rows are minted from is on the engine's machine and
is not carried across the connection — so over a remote session there is no such row and no
such page. What you get there is the ordinary record card.

**`enter where it is running`** — nothing on this machine can reach that conversation. What
opens is a card saying where the work is and the one thing to do about it:

  ```
  this is running in another window on this project: docs pass.
  go to that window to read it, steer it or stop it.
  ```

  A window that has not settled on a name says only `this is running in another window on
  this project.` `esc` backs out to the list.

- **That card offers no mention.** A `@` name resolves against work that has **landed** and
  this has not, so the card's foot is `↑↓ scroll · esc back` with no `m puts it in your
  message` on it, and `m` does nothing there.
- **A task nothing has named is left off**, because a row with no words on it says nothing
  anybody can act on.
- They **leave on their own** when that window closes or finishes the work. Nothing
  announces it; the row simply stops being drawn within a few seconds.
- The **column** never carries these. The roster beside the conversation is this session's
  own work, and a tree with another window's tasks hanging off it would be claiming a
  parentage that does not exist.
- The page re-reads what the other windows are doing every few seconds while it or the
  column is on screen, and on the way in.

If a task you started in another terminal is on **no** row here, that window has closed.
Its work stopped with it, and the project's record will say `incomplete` against whatever
it had started.

## Does it know what my other windows are doing — will it notice work from another terminal

Yes, and it is told rather than having to go and ask. When another codeaf window on this
same directory lands a task, or has one running, codeaf puts a small block into the chat's
own context — never on your screen — that reads:

```
<elsewhere>
Work on this project from outside this conversation. Facts, not requests.
recently landed in other windows:
- Fix the nil-map crash · done · internal/reconciler/state.go, internal/reconciler/state_test.go
  Added the guard and the regression test; the parser suite passes.
running in another window now:
- Sweep the call sites · window "docs pass" · internal/session/agent.go
- Survey the config loaders · window "docs pass" · 3 quick parts running · internal/config/load.go
- Compare the two lockfiles · quick · window "release"
</elsewhere>
```

- **`recently landed in other windows:`** is tasks that finished **in another window**. Your
  own conversation's tasks are never repeated there — their reports already arrived here in
  full. Each row names the task, how it ended, and the files it wrote.
- **`running in another window now:`** is what those windows have out at this moment, with
  the files each run has already written. Written, not planned: nothing is reserved and
  nothing is locked by it. **A task's parts ride on its row** (`3 quick parts running`,
  with the family's files), and `quick` on a row means that window's quick task is writing
  in its folder right now.
- It is **facts, never instructions.** Nothing another window writes can tell this
  conversation what to do; the chat reads it to you or works around it, and that is all.
- It is **silent when there is nothing to say** — no block at all, never a line saying
  "nothing new" — and it is not sent again while nothing changes. At most **6** landings
  and **6** running rows, each naming at most **5** files with `and N more` after them.
- The **first** time a conversation is told, it looks back **24 hours** and no further, so
  opening a window does not tip a project's whole history into it.
- What counts as "already told" is kept in `told.json` inside that conversation's own
  folder. It is per conversation, and it is not `/home`'s **since you last looked** — that
  one is about **you** having looked at the dashboard, this one about the chat having been
  told.
- A **task** is never given this block. A task's brief is its whole world.

## Asking the chat what else is running on this project right now

Ask it in words. It reads the other windows itself rather than guessing from the project's
record — which cannot answer, because an ordinary task writes no row there until it lands.
What it gets back looks like this:

```
running in other codeaf windows on this project:
another window · Sweep the call sites · running · running for 4m 12s
  in the window called "docs pass"
  files so far: internal/session/agent.go, internal/session/task.go
```

- Every row is marked `another window`, and `in the window called …` follows with that
  window's own name when it has settled on one.
- `files so far` is what that run has **already written**, in the order it wrote them.
- The rows carry **no id**, and the chat is told exactly why: `These have no id in this
  conversation: work running in another window cannot be read, steered or resolved from
  here, and it lands in that window rather than this one.` So it cannot stop, steer or
  accept another window's work on your behalf — go to that window, the same answer the
  `/history` page gives.
- A window that has closed contributes no rows at all: its work stopped with it, and the
  project's record says `incomplete` against whatever it had started.
- Inside a task this is absent too — a task is shown the pieces it handed out itself and
  nothing wider.

## What is running in my other projects — ask, and the tasks tool answers everywhere

The section above is about **this** project. Ask about the rest of the machine — "what
tasks are running outside this chat", "what is running in my other projects", "is anything
going anywhere else" — and the chat widens the same tool: it calls `tasks` with
`scope: "everywhere"`. That word is the whole of the feature. `scope` takes `project`,
which is the default and exactly what a search has always done, or `everywhere`.

`everywhere` keeps this project's rows and this project's other windows, then adds under
them **one group for each other project that has live work**: the project's name, its path,
and one row for each task running there — the title, the state word, how long it has been
going, and the files that run has already written where it says.

```
running in other projects on this machine:
wisp · /Users/ada/code/wisp
  another window · Port the parser · running · running for 2m 3s
    in the window called "parser work"
    files so far: internal/parse/lex.go
  open here · Rewrite the docs · queued
```

- **`another window`** is a terminal somewhere else. **`open here`** is a conversation
  *this* terminal is already holding behind this one, in that other project — reached with
  `tab` or from `/home`, not by going and finding a window. When any row says it, one more
  line spells that out.
- **A project with nothing running is not listed at all** — no heading, no zero, no line.
  Only live work is here, so a quiet machine answers with this section missing entirely.
- A window that has closed contributes nothing: rows are read from what each conversation
  says about itself every few seconds, and a claim nobody has refreshed is not believed.
- **These rows carry no id**, and the chat is told why in one line: `These have no id in
  this conversation: work running in another project cannot be read, steered or resolved
  from here, and it lands where it is running rather than in this conversation.` So it
  cannot stop, steer or accept another project's work for you — go to that project.
- The whole reading is only taken when `everywhere` is asked for. An ordinary turn, and an
  ordinary search, never look outside this project at all.
- Inside a task this is absent, like the rest of it: a task sees the pieces it handed out
  itself and nothing wider.

## Searching the task page: type to filter, find an old task by name, where the words I type appear, my cursor jumped to another task while I was reading

**Just type.** On the task page every printable key — letters, the space, and digits
everywhere they are not an answer — builds a filter, and every section narrows against it as
you go. The two exceptions are `1` and `2` over a row the record pane beside the list is
drawing answers for, which answer it: see *Answer a task from the list*.

```
⌕ parser                                            state               age ↓
```

is the **control row**, the first line of the list, and your letters land there in the
reading ink with the dim `type to filter` standing in the box until you type. It used to be
an echo on a note line UNDER the rows your keystrokes had just changed; it is at the top of
the list now, where the typing goes.

- It matches a task's **title**, its **id** (typed exactly: `7` finds task 7 and nothing
  else), its **name** as the `@` list spells it, and its **outcome**. Letters in order are
  enough — `prsr` finds `Port the parser`.
- **Every section is filtered at once**, another window's rows included — those match on
  their **title only**, never on an id, because ids restart with every conversation and `7`
  typed here is a number you read in *this* window. A section with no match is not drawn at
  all, heading and all, so a filter that only matches old work leaves the `earlier` list
  alone on the page.
- It also matches the **conversation or project** a row came out of, because that is drawn
  on the row and anything on screen is something you can search for.
- `backspace` deletes a character, `ctrl+w` a word, `ctrl+u` all of it.
- **`esc` clears the filter first and closes the page on the second press** — the same
  layering the settings panel's search has. `ctrl+.` closes the page from anywhere.
- With nothing matching, the line under the list reads `nothing matches`, and the page keeps
  its own heading and count — there is work here and your words are hiding it.
- **A filter opens every main chat it found something in**, because a row that matched and is
  sitting behind a shut fold is a row the query appears to have missed. Clearing the filter
  gives you your own folds back.
- `↑`/`↓` and `enter` keep working over exactly the rows the filter left.

## A task I just started is not on the task page — the page while it is open

The page re-files itself on its own beat while it is up, so work that starts *after* you
opened it grows a row under `running` without you closing and reopening the page. That is
true whether the task was proposed by the model or typed as `/task`, and it is true over
`--host`.

It used to be true only at home. The page re-filed itself when the reading of *other
windows on this machine* changed, and nothing over a connection answers that question — so
away from home the reading stood still for ever and a page opened before the work started
never grew the row. It now also watches this window's own roster, which is the only
authority a hosted page has for work that has not landed anywhere yet.

**A task that has not landed exists nowhere but this window.** No file on any machine has a
row for it until it finishes, so it reaches the page through the roster beside the
conversation and through nothing else. If the roster is empty, the page will be missing it
too — see *I started a task over ssh and the sidebar stayed empty* above.

## Keys and clicks on the task page

| key | what it does |
| --- | --- |
| `↑` `↓` (or `ctrl+p` / `ctrl+n`) | move, stepping over the head sentence, blank lines and section words |
| `pgup` `pgdown` | move twelve rows |
| `home` `end` | first row, last row |
| `enter` | open the main chat, task room, or record card named by this row |
| `→` | open the family under this row, where it has one; a second `→` on an open family opens the row's verbs |
| `←` | fold that conversation or family back up |
| any printable key | type into the filter — except `1` and `2` over a row the pane is offering those two answers for, which answer it |
| `backspace` `ctrl+w` `ctrl+u` | edit the filter |
| `esc` | clear the filter, or close the page when there is none |
| `ctrl+.` | close the page |

`ctrl+c` still works and still means what it always means: mid-turn it interrupts,
and at rest it quits codeaf on the press that lands — with this page still up, because
leaving is never modal and nothing on the way out asks about the tasks on it.

**What `enter` opens depends on the row**, and the last line of the page says which you are
going to get:

- A task **this session is holding** — wherever on the page it is filed — opens its room,
  exactly as `enter` on the roster does. The foot reads `enter open its room`.
- A task **another conversation ran** has no room to open: a room is a live lane onto a task
  in this session's work, and that session is closed. `enter` **goes inside it** instead —
  the card of everything the record wrote down about that piece of work, over the same
  page, with this list still underneath. The foot reads `enter go inside it`. See *Going
  inside an old task* below.
- A task **running in another window right now** is selectable too. The page opens its
  owning conversation when this terminal holds it, attaches a read-only view when the
  engine offers one, or opens a card explaining which window holds it. The foot names
  the available door.

Clicking a row's title puts the cursor on it and the record pane beside the list becomes that
row; clicking the **same row again** opens it — the page opens things, it does not change them.
On a frame too narrow for the pane there is nothing for a first click to show, so one click
opens as it always did. A click on one of the pane's own answers presses that answer and opens
nothing. The row under the pointer takes the hover step. The wheel walks the cursor.

## Main chats and their subtasks — the conversation tree, folds, holds 3 more, what the +3 under a row means

The **main chat is the parent** of the work it requested. Tasks hang beneath their
conversation; a task's children hang beneath that task, including deeper levels.
Chats in the selected time window appear even before they delegate any work.

**Everything starts expanded**, including every nested task. `←` folds a branch and
`→` opens it again. Those explicit choices survive refreshes; a filter temporarily opens
the ancestors of its matches and restores your folds when cleared.

```
running
  ▾ ? Repair the parser                  codeaf
      ▾ Update the parser
        ├ Port the lexer
        └ Port the tests
      Update the documentation
```

Conversations stay together under running while any work is active or unanswered, and
move to completed when it settles. Each child
keeps its own state: a finished sibling does not become a decision because another
child needs you. The top count includes every chat and subtask in the window.

**Click a conversation title or press `enter` to open its main chat.** A task title
opens that task's room or record through the existing owner-aware navigation.
Conversation rows offer no worker stop or mention action.

Conversations start expanded. Families beneath a task start folded. `→` opens a fold;
`←` closes it. Clicking the visible disclosure arrow also toggles it; clicking the
name opens the chat or task. A shut row says `holds N more`. Open folds and the cursor
stay attached to their conversation and task identities when the page refreshes.

**A conversation that has said nothing yet is called `new conversation`** on this page —
the same word the home column spells for a chat nothing has named — never the session's
own id. A launch's first session exists before the title role has anything to name, and
the row it gets is the word, with no age beside it because nothing has happened in it
yet. Typing the word in the filter finds it; typing the id never did, because an id was
never on the page to match.

Search keeps the ancestors of a matching task and opens its path. Clearing the search
restores your folds. Opening a record from Home also reveals its ancestor path in the
list, so returning from the record lands on that task. A missing parent leaves its
child visible, and a damaged parent cycle cannot hang the page.

On narrow terminals indentation gives space back to the names. The underlying tree
keeps every level even when there is not room to draw every indentation step.

## Tasks on a phone — the ▸ tasks door, the list, and the way back

Under 60 columns the whole task flow is a thumb's, with no keyboard anywhere in it. None of
the surfaces is new — the strip, the roster page and the record card all exist and all
answer a key — but each is reshaped so a finger can do the flow end to end:

1. **The strip is one door, and it looks like one.** Instead of a row of tiny chips a few
   cells apart, the strip at phone width is a single full-width row that says what is there
   and that it opens: `▸ 3 tasks · 1 running`. The `▸` is the same fold glyph the rest of
   the phone screen folds with, the count is how many tasks are live, and the tail is the
   most urgent of them — `running`, or `needs you`, or `queued`. One task still draws the
   door: `▸ 1 task · running`. A tap **anywhere** on the row opens the roster page.
2. **The roster is a list of cards.** Each task is a two-line card a thumb goes into — its
   name and state on top, and what it came to with how long ago under it. The list
   **scrolls**: walk it with a swipe or the arrows and the card you reach is drawn whole,
   never clipped at the fold.
3. **Tap a card to go inside.** A task this session ran opens its room; a task from a
   conversation that is closed opens its record card.
4. **Two backs, both bands.** The record card's foot is `‹ back` and `m puts it in your
   message`; `‹ back` returns to the list. The list's own foot is a `‹ back` bar too, and
   it drops you back to the conversation. So the way out is `‹ back`, then `‹ back` — a
   tap each, no `esc` needed.

The door opens the **roster page** — the machine's whole record, grouped by what you do
next — not the overlay column a keyboard drives. Mouse motion is ignored on the glass: a tap opens in one
gesture, and no row lights up under a finger that is only resting on it.

## Open a task by tapping — the tap targets under 60 columns

Every door onto a piece of work is a tap target at phone width, and none of them is a new
door — they are the ones above, reshaped:

- **The strip along the top** is one full-width door — `▸ 3 tasks · 1 running` — and a tap
  anywhere on it opens the roster page. It is not a row of chips at this width.
- **The roster's rows** are two-line cards, each a full-width target, and one press opens it
  — a room, or the record card — which is what a click already did at every width.
- **The roster's foot** is a `‹ back` band in place of the key legend
  `enter open its room`. Tap it to go back to the conversation.
- **The record card's foot.** `m puts it in your message · ↑↓ scroll` is a
  sentence about keys; under 60 columns the two things a thumb can do become bands instead —
  `‹ back` and `m puts it in your message`. Tap either, or press the key it names. The
  scroll is the screen itself.
- **A task row on home's sheet.** Tapping a task on the work band of a conversation's card
  opens **that task's record** — the same card `enter` opens on the task page.

## Open a past task

Open `/history` or press `ctrl+.` to see earlier work. Select a past task and press
`enter`, or click its row once, to open its saved record. The card shows the result,
originating conversation and available evidence. Press `esc` to return to the list.

## Going inside an old task — see what a past task did, read a finished task's report, where is the story my task wrote

`enter` on any row of the task page (`ctrl+.`, `/history`) that this conversation did not
run **goes inside that task**. A click does the same on the first press. The task column carries no rows of old
work — its `ctrl+. earlier` line is the door onto this page — so the page is where every
old task is opened.
What opens is a full-screen card over the same page, with the list still underneath:

```
 Fix the nil-map crash                                              esc back
 ─────────────────────────────────────────────────────────────────────────────
 done · landed 3h ago · ran 4m 12s

 Added the guard and the regression test; the parser suite passes.

 out of Fixing the importer
 anthropic/claude-sonnet-4.5 · $0.42 · 12k tok
 3 files changed

 a branch of your repository · ~/.codeaf/v3/projects/-tmp-alpha/trees/fix-the-nil-map-crash
 transcript · ~/.codeaf/v3/projects/-tmp-alpha/aaaa…/tasks/20260819-120133_7.jsonl

 what it said at the end
 Added a nil check in parseRow before the map write, and a regression test that
 fails without it. The parser suite passes: 84 tests, 0 failures.
 ─────────────────────────────────────────────────────────────────────────────
 m puts it in your message · ↑↓ scroll
```

**The rule and the keys sit under the last row the card drew**, not at the bottom of the
terminal, and `esc back` is said once, in the head's right corner. This page has no
composer under it, so a foot pinned to the bottom of a fifty-row frame under a six-line
card was a foot pinned for nobody. A card long enough to scroll fills the frame and its
foot is where it always was.

Top to bottom: the title; the state it came home in, when it landed and how long it ran;
the outcome sentence; the conversation it came out of; what it ran on and what it spent;
how many files it changed; where it left the work and where the story is; and then **the
last thing the task itself said** — the whole report, read off that task's own journal, of
which the outcome above is the first sentence.

- **`out of <conversation>` names where the work came from**, spelled the way the row on the
  list spells it, so the page is never a smaller answer than the row that opened it. It is
  the project's name where nothing has titled the conversation yet, and it is left off
  where nothing knows either — including where the row belongs to a conversation this
  machine's scan has never met. It is never filled in with the conversation you are
  sitting in: that is the one answer that is certainly wrong.
- **Work that failed did not land.** The clock clause follows the state in front of it:
  `done · landed 3h ago`, and `failed · stopped 8d ago` — `landed` is this program's word
  for work that arrived, and it was drawn over a run nothing came of.

- **Anything codeaf does not know is not drawn at all.** A task that spent nothing has no
  money line, one that wrote nothing has no file count, one still claiming to be running
  has no clock. That includes a run that plans itself: while it is running its card carries
  its state word and no `landed` clause at all; the clock appears when the run ends. Nothing
  here appears as a zero.
- **The first address is labelled with what that directory was**, in plain words and never
  in git's: `a branch of your repository`, `its own copy of the folder`, `your own folder`,
  or `where` when codeaf's record does not say. *Does a task touch my working copy?* in
  *how tasks run* says what each one means.
- **The two addresses are clickable where they still exist.** The transcript is a real file
  on this disk and opens in your editor on a click; a working copy that has since been
  merged and swept away is printed as plain text, because a link that opens nothing is worse
  than no link. A task whose working copy is gone says `branch` and the branch name instead
  — that is a name inside your repository, not a place on the disk, so it is never a link.
- **`esc` backs out to the list**, one layer at a time, with the cursor still on the row you
  came in on. A second `esc` closes the page. `ctrl+.` closes the whole page from inside.
- **`↑`/`↓` scroll the card**, `pgup`/`pgdown` a screenful, `home`/`end` the ends. A long
  report is read down rather than cut.
- **`m` puts the task in your message** — `@its-name`, appended to whatever you had
  half-written — and closes the page. That is where the mention gesture lives now: `enter`
  used to write it, and `enter` goes inside instead.
- If the row names a transcript that is **not on this disk any more** — a session folder you
  deleted, work that happened on another machine — the card says
  `its transcript is not on this disk any more` where the report would have been.

A task **this** session ran opens its room instead, which is the live thing: the roster's
`enter`, a strip chip and a `task 7` link all land there. Only work from a conversation that
is closed opens the card.

## The one door line at the bottom of the task column: `ctrl+. earlier`, and where `view more` went

When the project's record holds work this column is not showing, the roster's footer grows
one more dim line:

```
ctrl+. earlier
```

Click it, or press `ctrl+.`, and the full-screen task page opens. The column is left exactly
as it was: the page is somewhere you go and come back from, not a state the column enters.

**There is exactly one such line, never two.** It is drawn when the record holds tasks
**this session never ran**: work from an earlier conversation, or from a window still open
beside this one. This is the common case: any directory you have worked in before has one.

**`ctrl+. view more` is gone.** It stood for a family the column had folded away. The column
folds its own groups now (`Done 6 ▸`), and the heading is the door: press it and the rows
open in place.

A landed task of this session's, already on the column, does not earn the line: it would be
offering to show you what you are looking at. So a first-ever session in a fresh directory,
with no record behind it, has no door line at all, and that is not a bug. It is never drawn
as a count of nothing.

## Which task a row opens — two tasks numbered 7, a row opened a different task, the wrong transcript

**A task is named by two things: the conversation that ran it and its number inside that
conversation.** Numbers start again at 1 in every conversation, so `7` names a different
piece of work in each of them, and the tasks page can be holding several rows numbered 7 at
once — one this window is running, one an earlier conversation ran, one a window next door
has out.

Pressing a row opens **that row's** task, and the owner is what decides which door it gets:

- a row this conversation is holding opens its **room** — the live page, with the box
  talking to it;
- a row another conversation ran opens its **card** — the record, and the last thing that
  task said;
- a row another codeaf **window** is running opens the card that says which window has it
  (above).

A row's title is not what identifies it. Two conversations that ran a task numbered 7 with
the same words are two pieces of work, and the page opens the one you pressed. It did not
always: the number and the title were the whole of the match, so a landed row from a
conversation that closed months ago could open this window's live task 7 — a room that drew
a real transcript belonging to a different task. If you are on a build that does that, the
tell is the room's header naming work you did not press.

`enter` and a click are the same door on every one of these rows, at every width.

## Walking into a task's room

A task is a place you can go. Opening its room makes the body stop being the conversation
and become that task's own transcript — its history off disk, then its present, live — and
the input box stops talking to the model and starts talking to the task.

A room is a view, not a second app. Nothing under it stops: the conversation keeps
streaming, the roster keeps ticking, landing cards keep landing. The conversation's scroll
is never touched, which is why leaving restores it exactly.

Ways in:

- click a roster row, a strip chip, a proposal card, or a landed card;
- click an inline reference in the model's prose — `task 7`, `task #7`, `tasks id 7`,
  `task id #7` become underlined links when the id names a task this session has seen;
- `enter` on a proposal card or a landed card selected with `↑`/`↓` over an empty box;
- `→` over an empty box walks into the next running task's room, wrapping at the end;
- `enter` on a row while the roster holds the keyboard.

Pressing the same door again is always the way back out.

Ways out: `esc` leaves and restores the conversation's scroll exactly. `←` over an empty box
steps back one level. `←` twice within 600 ms goes home — out of everything, at the live
edge, nothing selected. `/new` closes any open room, because a task dies with its session.
With the pointer, click the root breadcrumb or the padded `esc/← main` control.
Breadcrumb separators and blank header space do nothing. **Clicking inside the page does not leave it** — a press
on a blank row, or on prose with nothing behind it, does nothing at all, the same as it
does in the conversation.

What refuses to open: a proposal whose task has had no update yet (the id is real, but a
room on it would be an empty page with nothing coming), and a queued task by way of `→`
(it has no worker yet, so there is nothing to talk to). A local agent with no room door
writes one note in the conversation: `room unavailable — this session has no task rooms`.
When the rail over `--host` already draws the far task, that refusal is never used: the
far task id is itself the room door, including while the task is running.

## Task roster and rooms while running on another machine

Over `--host`, the roster beside the conversation lists that far conversation's tasks
from the far machine's record, under the same header, band and groups. `alt+l` closes or
restores it exactly as it does for a local conversation; the column reads only what this
window already holds, so drawing it never waits on the connection, and it it never falls back to tasks on the machine holding the screen.

The far engine **pushes** every task update to every window attached to that
conversation, so a row appears when the work is admitted, moves when it starts running,
and stays when it lands — with nothing on your screen asking for it. When a window
attaches, the engine replays the whole roster onto it first, so a terminal you opened an
hour into the work still draws every row rather than only the ones that moved after you
arrived. This is the same subscription a local surface holds; the only difference is that
it crosses a connection.

## I started a task over ssh and the sidebar stayed empty — my task ran on the remote machine but there is no row for it

This was a real defect and it is fixed. Before it was, `/task solo <brief>` over `--host`
answered `single task 1 started · …`, the far worker ran and finished — and the column
beside the conversation stayed empty with only `+ /task` on it, so the person who started
the work could not watch it, could not click into its room, and could not stop it. Tasks
the model proposed inside a turn did appear, which made it look like the roster worked.

The cause was two things in the same seam. A typed `/task` is a **call** and not a turn, so
a task node's life goes out on the session's standing subscription — which nothing carried
across the connection. And the connection was missing the door a proposal's `y` goes back
through, which mattered more than it sounds: the surface asks for the whole task seam in one
question, so one missing door left the rail not subscribed at all rather than partly
working. Both halves cross now, and answering a proposal card over `--host` works.

If you are on a build where it is still empty, the two halves are speaking different
protocols. The engine refuses a mismatch at the door with a sentence naming both numbers;
if you get that instead, run `codeaf engine --stop` on the far machine so the older
process holding your session retires, and connect again.

Opening a queued, running, or landed row is asynchronous. The room opens at once — with
the instruction the task was given, the sentence naming what the work is doing where the
engine has published one, and `loading this task's conversation…` while the read is on
the wire — then replaces the whole of that with the bounded end of the task's transcript
when it arrives. Where there is no read on the wire the loading line is not drawn at all;
the page says `nothing on this page yet — it fills in as the task works` instead.
Waiting reasons use the roster’s wording, such as `waiting · its parts`. A read that failed says `couldn't read this task's conversation · retrying` and keeps
beating. While work runs, the room reads that bounded tail on its own beat and the
`nothing on this page yet` line lasts only until the first block arrives. The calls, results, reasoning, and
messages use the ordinary room renderer. `enter` steers the far worker; `x` raises the
ordinary confirmation and stopping uses the far engine's own sentence. Changing the
task's model remains absent over this connection. Leaving with `esc` or `←` works normally.

A background job still has no transcript, and over this connection its page draws no log
either: the path belongs to the other machine, and reading it here would open a file on
yours. The body says `its log is on ` plus that machine's name; the foot prefixes the far
log path with the same name and never offers the same spelling as a local path. `c`
still copies that far spelling; `jobs output 3` is how you read a far job's output. On a
local conversation the same page tails that log live.

## What is different inside a room

| | the main thread | inside a room |
| --- | --- | --- |
| what the body draws | the conversation | that task's transcript |
| its row on the roster | nothing is marked | that task's row wears a colour band and an accent title |
| clicking empty space | nothing | nothing — leaving is `esc`, `←`, or the pinned header |
| what `enter` does | sends to the model, or holds the message above the box while a turn is running | **steers the task** — never held |
| what `↑`/`↓` do | walk your history, then select a tool row, then scroll | the same walk through **the same history** — steered lines are in it — then scroll the page |
| what `esc` does | backs out to Home, preserving work | leaves the room. It never interrupts and never stops work |
| how you stop the work | `esc` | `x` over an empty box, which raises the confirmation card |
| the box's own line | the bare `› ` | a tinted segment naming the task, in its state's hue, then `› ` |
| box placeholder | the draft prompt | `Steer this task… (esc: main)`, or `Steer <title>… (esc: main)` where the frame is too narrow for the segment |
| pinned top rows | the pulse line, then the tab strip, one thin rule and a blank. That strip is the chat's own row, so a place draws three rows (pulse, rule, blank) and the page starts one higher. A dim `+N` at the strip's right end counts the tabs it could not spell, and `alt+k` opens the chats card | the same four rows as the conversation: pulse, tab strip, rule, blank. Then a breadcrumb row (conversation, ancestor tasks, current task) and a quiet facts row under it |
| legend word | the model, effort and approvals, with the remote machine when connected | `room · esc/←← main`, and `room · esc your line back` while a history walk is on |
| legend hint | `ctrl+c interrupt` while a turn runs | `x stop` while there is work to stop, `↑↓ history` mid-walk, nothing otherwise |
| the model on the status row | the conversation's model | `task <the task's model>` |
| clicking that model | opens the picker and switches the conversation | opens the picker and switches **that task**, from its next request — and does nothing at all once the task has landed |
| `ctrl+b` | freezes the transcript | freezes the room's own rows |
| scroll position | the conversation's | the room's own, kept separately |
| attachments | the tray sends pictures | a room's box sends words only |
| proposals | drawn as cards | never — a task's own pieces start without asking you |

The focus header is **two pinned rows**, under the tab strip:

```
  │ main ×│ the tree walk    │
  ─ main ▸ Ship the port ▸ Fix the nil-map crash ───────────── esc/← main ─
  ─ ⠙ working · 2m 12s · $0.04 · 6 tool calls ───────────────────── Stop ───
```

**The trail row is ancestry and nothing else** — the conversation, the actual ancestor
tasks, and the task you are on — with the way out at its right end. No state glyph, no
model, no money, no call count: a path with figures threaded through it is a path nobody
reads as a path.

**The facts row under it is what the work is doing.** The state leads it, wearing the same
hue the roster paints that task's glyph with, and the elapsed time, the spend, the call
count, the live line and the model follow in the quiet grey every other figure on this
surface wears. `Stop` is at its right end, and the row closes in the rule that separates
the header from the page.

**They degrade on their own rows**, which is what "navigation first when narrow" actually
means: the trail folds its middle to `…` because the *chain* did not fit, never because a
figure wanted the space, and the facts drop off the end in rank order whatever the trail
did.

**The name on it is the task's whole name, and the facts behind it are what a narrow
frame gives up.** Until 2026-09-03 a task's name was cut to three words the moment it
arrived, before any width was known, so a room opened at a hundred and sixty columns
named the work no better than the twenty-four-cell column did: a family of six pieces
that all began with a verb and a plural noun came out as `Cut every list`, `Fold the
settled`, `Move the tab`. The name now reaches every row whole and each row decides what
it can afford — the header keeps the name first and drops facts off the end as the
terminal narrows, and only a name that cannot fit the trail row **alone** is cut, in which
case it takes that whole row — the facts are on the row underneath either way.

**And the hint slot under a room asking for your call shortens rather than vanishing.**
At sixty columns the whole answers row is a few cells too long for what the foot has left
beside `room · esc/←← main`, and the slot used to go empty — so the narrowest terminal was
the one that named none of the keys answering the question it was standing on. It now drops
chips off the end and says how many went, `a accept · n not right · +1` and then
`a accept · +2`: the same answers in the same order, with a count of the ones that did not
fit. Every letter keeps working whether or not it is printed.

**`alt+e` inside a room moves that task's thinking rung**, one step up each press and back
round to `low` from `max`. It is the same chord home uses on the machine's own default and
on a standing item, bound here to the task whose page you are standing in; the keys page
has the whole of it. A worker already running keeps the rung it started with, so the line
codeaf writes says `task 7 · thinking · high · its next call takes it`.

**The header has separate click targets.** Ancestor crumbs open their exact task;
the root and the `esc/← main` end return to the conversation. The current crumb does
nothing when pressed — so clicking its name cannot accidentally leave it — though it
still lights under the pointer, so the row never looks broken where you are standing.
`Stop` is on the row underneath and retains its confirmation. Empty header space retains
the shortcut back to main. Below 12 columns,
or on very short terminals, the bar gives its row back to the transcript; `esc` still
leaves.

Actionable waiting work retains its answer row. The parent sentence no longer repeats
the breadcrumb ancestry; a compact `handed out:` row still names children and stays within
the task column, clear of the roster.

## Typing in a task's room — the up arrow, editing what you sent, and escape

The box in a room is the same box as the one in the main thread, and it behaves the same
way. There is no separate "steer widget" with rules of its own.

**The same box, and not the same words.** Each room keeps its own unsent line — its
text, its caret, its `[paste 1 · 42 lines]` chips and its tray — and so does the main
thread. Opening a room does not carry your half-written message for the model into it,
`esc` gives that message back exactly as you left it, and going straight from one room
to another keeps each line where it was typed. `enter` sends the box you are looking at
and clears only that one. What a room's box holds is written down with the thread's own
draft and comes back after a crash or a restart — into that room, never into the thread
and never into another conversation's task of the same number (see the keys page).

**Files on a room's tray are never sent.** `enter` in a room sends words: a correction
is a sentence, so anything you attached there stays on that room's tray and the room
says `attached files do not go with a correction · they stay on this page · esc, then
attach them in the conversation to send them`. Nothing is dropped — the files are still
there, on that page, when you come back. Compact `[paste 1 · 42 lines]` blocks *do* go:
the worker reads the document behind the tag, and the row keeps the tag.

**`↑` brings back what you typed, so you can edit it and send it again.** Over an empty
box, or with the caret on the first line of what you are writing, `↑` walks your own
history newest first — this directory's prompts before everything else — and `↓` walks
forward again until your own half-written draft comes back untouched. It is one list,
shared with the main thread: **a line you steered into a task joins your history**, so
`↑` in the room brings back the last thing you said to the task, and `↑` in the thread
reaches it too. A line the task refused (see *the steer guard*) never joins it — those
words went nowhere, and they are still sitting in your box.

Inside a multi-line message `↑` and `↓` move the caret between lines first, exactly as
they do in the main thread. With **no history at all** — a fresh machine, or input
history switched off in the settings panel — `↑` and `↓` fall through to scrolling the
page one row, which is what they used to do always.

**Scrolling the page** is `pgup`/`pgdown` and the mouse wheel, and those are never taken
by anything else.

**`esc` in a room is the way out and nothing else.** It leaves the page and puts the
thread back exactly as it was. It does **not** interrupt the running turn the way `esc`
does out in the thread — leaving is the first press, and the `esc` after that one
interrupts. And it never stops the task: stopping is `x`, which raises a card you have to
answer, because a stopped task cannot be un-stopped. The legend at the bottom of the
frame always says which of these the next `esc` is: `room · esc/←← main` normally, and
`room · esc your line back` for as long as a history walk is on, because during a walk
`esc` gives your own draft back before the room's own `esc` gets the key.

`enter` steers. Nothing is ever held above the box inside a room — the waiting-message
machinery belongs to the main thread, since a task reads what you send at its next step.
A task that is waiting on pieces it handed out has no next step coming, and your line is
what wakes it (*Steering a task that is waiting on its pieces*).

## I sent a correction and the window closed — what survives, and what is never sent twice

**A correction is written down before it is sent.** `enter` in a room saves it beside
that room's own unsent line first — the words, the caret, the compact paste blocks, and
the name this correction was sent under — and only then hands it to the task. Nothing
reaches the task before that write has landed, so a window that dies mid-send leaves the
correction on disk rather than leaving you with a worker that may or may not have been
corrected.

- **The next launch puts it back on that task's page, under the same name.** Asking
  again is a repeat of the same correction rather than a second one: a task that keeps
  names answers it with the receipt already on its record, so pressing it twice cannot
  make the worker read the words twice.
- **If it cannot be written, it is not sent.** You are told
  `task 7 was not corrected — the draft could not be saved first, and your words are back
  on its page`, and the whole line is back in that room's box. A correction this machine
  cannot give a name to — the random source that names one having failed — is refused the
  same way, because a correction with no name could never be asked about again.
- **A correction nobody answered stays a correction.** The row says
  `no answer — it is not known whether this arrived` and offers to ask again. It is never
  quietly handed back to the box, because the next `enter` would give it a new name and
  the task could then read it twice.
- **Closing a conversation does not settle a correction still crossing or unanswered.**
  Its words, compact paste blocks and original message name remain on disk. If the
  answer is lost after the close, the notice says `task 7 gave no answer — delivery is
  unknown; reopen its conversation to ask again`. Reopening that conversation and its
  task page restores the unresolved row, even after restarting the window. Nothing is
  retried automatically or placed in another conversation's composer. An explicit retry
  uses the original name; an engine that keeps names answers an already accepted
  correction without delivering it twice. Ordinary unsent drafts are discarded on close.
- **Words the task definitely refused are yours again**, back in that room's box with the
  caret where you left it and the pasted blocks behind them. If you have started a new
  sentence there since, that one is kept and the refused one waits beside it until the
  box is empty.
- **A conversation with no transcript of its own cannot steer.** There is nowhere to
  write the correction down first, so the room refuses — `this conversation has no
  transcript of its own yet, so a correction cannot be kept — and one that cannot be
  kept is not sent` — instead of sending something it could never ask about again.

## Steering a task that is waiting on its pieces — I typed into a task that split its work and nothing happened

A task that handed pieces of its work out stops talking and waits. It has said everything it
had to say, and the only thing that was ever going to move it again is one of its pieces
finishing (*When a task splits its own work*). The column still draws it as working, because
it is: waiting on its own pieces is the work.

**Type into its room anyway — your line is what wakes it.** The task reads your words as your
words, answers them, and goes back to waiting for its pieces. Because the page has been
still, your line says what the sending just did — as a short clause on the line itself,
which is drawn as a `└ ` elbow where you said it:

```
└ check the staging bucket first · it was waiting on its pieces — your line wakes it
```

The clause is there for a few seconds and then fades off the row, leaving the elbow. Every
steer into a task carries one: this sentence when the task was parked on its pieces, and
`· delivered` when it was taking steps and your line will land at the next one
(*Reading a task page*).

Your correction can change what the task is for through `revise_assignment`. The
original assignment remains history; the current condition and your words are kept
together (*When you change what a task is for while it is running*).

**A task whose worker has just finished is refused out loud, never swallowed.** If the last
piece reported in the instant before you pressed enter, there is nobody left in there to
read your sentence, and you are told so — `task 3 has just finished, so there is nobody left
to say it to` — rather than watching a room that says your words arrived. Your words stay in
the box.

## Reading a task's room, and its frozen clock

Inside a task's room the page is built from the same blocks the conversation is made of, so
a tool call expands to its diff or output, a reply renders as markdown, and anything you
steered wears your own hue. History comes off the task's journal, capped at the last 120
blocks; a missing or unreadable journal is not an error — the room opens on the live edge
instead. When the task has landed, a foot line reads `this task has finished — say it to main` —
and `this task has finished — say it to main, or open its parent, Ship the port` where the
task was spawned under another one — and a landed room with no journal to read says
`this task's transcript is not here any more` above it. The foot names a door rather than a
key: `esc` is already on the legend and the header, and what a person whose steering was
just refused needs to know is where the words can go instead. A finished task's room replays its whole transcript after a restart as well —
see *A task's room after a restart*.

`pgup`/`pgdown` scroll a page, the mouse wheel scrolls, and reaching the bottom re-sticks
to the live edge. `↑`/`↓` walk your history first and only scroll a line when there is no
history to walk — see *Typing in a task's room*. `ctrl+b` freezes the room's rows for
copying — one known wrinkle: leaving copy mode rejoins the conversation's live edge, so
freezing a room while the conversation was scrolled up loses that scroll.

The task's elapsed clock freezes while you stand in its room. That number exists to ask
whether you should go and look; being there is the answer. Nothing is stopped, only
unreported, and it thaws at the value it would have had when you leave.

## Seeing the whole conversation inside a task — what folds and what opens it

**A finished task reads like a conversation: its request, a collapsed work chip,
and its final answer.** This also applies to nested tasks. Intermediate narration
and tool calls stay inside the work chip; click it or press `ctrl+e` to inspect them.
Scrolling a finished task, including at the top of its page, keeps those details
collapsed. Open work shows `▾ worked`; closed work shows `▸ worked`. Click the chip
or press `ctrl+e` again to collapse the whole outline, including open tool steps.
Your later messages and corrections remain visible at their original boundaries.
A stretch with no final reply keeps its available work visible, so an interrupted
or tool-only record does not pretend to have an answer.

While a task is running, settled phases keep their individual chips. Current work
uses the conversation’s compact step display: recent step headings, with a moving
heading for the active call or `Working` between calls. Click it or press `ctrl+e`
to open the details, and use the same control to close them. Questions, failures,
your corrections, and the final reply remain visible outside this display.
When the task finishes, live expansion choices reset so they do not accidentally
expand the entire finished task. `ui.work = open` still opens details by default.

Inside expanded work, captions group the steps. Click a caption to open its tool
rows. Nothing is removed from the journal. The page retains a bounded recent view;
on very long records it keeps the opening request and explicitly marks where earlier
work lies outside that view. Expanding the work reveals the retained entries.

Within expanded work, the existing disclosure controls still apply:

- the **instruction at the very top** — the brief the task was given — shows its first
  three lines above a line reading `▸ …14 more lines · ctrl+o` when it is longer than
  that. It is the only message on this surface that folds; see "The long brief at the top
  of a task's page" below;
- a **thinking block** shows three lines until you press `ctrl+e` or click it —
  `⠿ thought for 6s · 148 tok · ctrl+e`;
- a live run with **no caption yet and more tool calls than fit your window** shows a
  screenful of the newest ones above the fallback
  `9 earlier tool calls · scroll up or ctrl+o`; scrolling up at the top of the page,
  `ctrl+o`, or a click on that line unfolds the run. (The conversation keeps three in
  this fallback and its line reads `· ctrl+o`; a task's page keeps as many as the window
  is tall — see "Reading a task's page".)

## Why a task’s progress paragraph changes to a step caption

A paragraph followed by more work in the same stretch becomes progress narration,
even if it previously ended a settled phase. It uses the quieter work styling and
can supply the next step’s caption. The trailing reply keeps answer styling; a
message or correction from you preserves the reply before that boundary.

The caption is a short summary. Open it to read any narration left out of the
heading alongside that step’s calls; shortening a heading does not discard text.

## The long brief at the top of a task's page — `▸ …N more lines`, view more, expanding and collapsing the instruction, a task description that fills the whole screen

**A long task description no longer takes over the page. It shows its first three lines,
then a line you can click or press to see the rest.**

The first block on a task's page is the instruction the task was given — the words you
typed after `/task`, or the brief codeaf shaped from them, or a spec you pasted in. On a
long one that used to be the whole screen: you walked into a task to watch it work and
were shown the assignment, with the first tool call somewhere below the fold.

Generated assignments use readable section labels: Task request, Original request,
Deliverable, Completion criteria, Workspace, and context. A child task puts its own
assignment first, ahead of the inherited project request. Expanding shows the full
section bodies and the rules sent to the model. The model input and stored original
payload stay unchanged; this is a reading layout only. Ordinary runtime wake and
checkpoint notes do not become requests.

So the block folds:

```
› Port the key table and the escape table out of the old parser, keeping the
  behaviour identical. The tests in internal/parse must pass unchanged, and
  the public function names must not move — anything that imports them is
  ▸ …14 more lines · ctrl+o
```

- **Three lines are always shown**, and they are the first three, so you can tell what the
  work was asked for without opening anything.
- **The number is real.** `…14 more lines` is fourteen more lines *as drawn at your
  current width* — resize the window and the number changes with it.
- **Click the `▸ …14 more lines · ctrl+o` line to open it**, or press `ctrl+o`. Opening
  shows the whole instruction, however long it is. There is no second cap behind it.
- **The line stays after you open it**, reading `▾ …14 fewer · ctrl+o`. Click it or press
  `ctrl+o` again to fold it back.
- **A short instruction has no such line at all** and is simply drawn whole. Nothing is
  hidden and there is nothing to press.

**Only the instruction folds.** Anything you steer into a running task afterwards, and
every message you send out in the main thread, is drawn in full and has no fold line —
your own words in a conversation are the one thing this surface will not hide.

**Reading choices stay with the task during this session.** Returning to a task restores
its request expansion and reading position. Expanded live work returns only if that
same work is still current; it does not open a new step or the finished task’s whole-work
chip. Nothing is written to the journal.

`ctrl+o` is a chord, so it costs you no character — you can press it with a half-typed
sentence in the box and carry on. The three visible lines are drawn exactly as they would
be if nothing were folded.

Inside a room `ctrl+e` over an empty box opens the newest work chip onto its caption
outline. Only when there is no work chip does it fall through to the newest thinking
block.

## Who started this task, and what it handed out

Inside a task, the breadcrumb bar shows its owning conversation and every known
ancestor, ending at the current task. For example:

`Shipping the parser ▸ Fix validation ▸ Add boundary checks`

Click an ancestor to open its page. Narrow frames fold the middle into `…`, which opens
the nearest ancestor it hides. The current task is inert. Unknown parents are omitted;
the UI never substitutes a bare task id for a name. Guest ancestry remains visible but
inert because this window cannot use another conversation's local task ids.

The roster shows each child as its own row under what it is doing. The breadcrumbs use
the parent links; the separate `part of:` sentence no longer repeats
the parent. A compact `handed out:` row still names children and their state. Prerequisites that hold work back are still named in the
header's state. A task awaiting an actionable decision keeps its answer row.

## Opening a task in the middle of its work — what the room shows

A room opens **at the bottom, on the newest thing**, never at the top: it is showing you
where the work is now, not replaying it from the start. Leaving with `esc` and coming back,
or walking from one task to another and back, lands on the same place — each room reads its
own journal fresh and each keeps its own scroll.

Two lanes fill the page and they meet at one instant. The **journal** is every message the
task has finished writing. The **live stream** is what happens from the moment you walk in;
none of the task's history is re-narrated onto it, because a stream that replayed half an
hour of somebody else's greps before reaching the present would make walking into a task
mean reading it slowly.

Between those two sits the step the task is **in the middle of**, and you are handed that
once, on the way in: the reasoning it is spilling right now, the reply it has written so
far, and any call it has finished asking for and not yet started. Live continues from
there. So a task caught mid-sentence shows the sentence, rather than the last thing that
finished and then nothing until the next word lands.

A call the task is **still running** is drawn as running — an unfinished row with no
duration on it, because nobody has measured one yet — and the row settles in place when the
call comes back. It carries no clock: the room learns of that call from the file, which
does not say when it started. If the task ends while a call is still open, the row stops
animating and keeps the dim mark for something nothing more is coming for; it is not
marked failed, because nobody watched what became of it.

## Mentioning a task in the conversation

Type `@` in the draft and a list drops up. Teams and conversations come first, then
task rows above the file rows. The task sections
are `running`, `recent` (ended inside 24 hours) and `older`, in that order; the `older`
heading carries `older · N more` when the list was cut. At most 8 task rows are drawn,
though the search itself goes 40 deep so a match three sections down is still counted. A
running task is the top row whatever it scored — ranking decides order inside a section,
not between them.

A row reads `▸ ⧉ Sweep the deprecated call sites            4m`: a state glyph, the mention
mark, the title, and the age on the right — how long a live task has been going, or how
long ago a landed one landed. `↑`/`↓` move, `enter` takes the row, `esc` closes, and typing
keeps filtering.

Choosing a row types `@<slug>` — the title, lowercased and kebab-cased — and nothing else.
It is derived from the title, so it is a name you can type from memory without ever opening
the list.

## What mentioning a task sends

At `enter`, every `@<slug>` in your message that names a task grows a pointer block after
your sentence.
Your token stays exactly where you typed it; the block is the footnote under it:

```
[Task reference: Fix the nil-map crash — id 7 · done · ended 3h ago
 Outcome: "Added the guard and the regression test; the parser suite passes."
 Output: git:task/fix-the-nil-map-crash-9c1a2f · Transcript: file:///…/7.jsonl]
```

A running task instead carries its age, a `Live:` clause saying what it is doing, and a
`Steer:` clause. Any clause with nothing behind it is dropped rather than written empty.

The expansion happens before the message is sent and before it lands in the transcript, so
what you see on screen is exactly what went on the wire. It never inlines the work: what
travels is six facts and two addresses, and the model follows either address if it needs
more.

Unknown tokens are left alone in silence — `@santosh` is a person, `@internal/x.go` is a
path. A task mentioned twice gets one block. A slug pasted whole and submitted in the same
beat resolves against what is already in memory, so it may stay the plain word you typed.

## Why did a task do that — asking about old work, what exactly it changed, what it decided

**Just ask, in the chat.** "Why did the auth task pin the clock?", "why did that task do
that?", "what exactly did that task change?", "what did it try first?", "why did it make
that decision?" — codeaf answers all of these by going and reading, not by remembering. It
was never in the room while the task worked, and neither were you.

What happens is two steps and you do not have to ask for either.

1. **It finds the task.** codeaf searches the project's whole record — every task this
   project has ever run, this conversation's and every closed conversation's — against the
   words you used, matching titles, ids and outcomes. You need no id and no `@`.
2. **It reads that task's own transcript** — what the task actually did, in its own words.
   Every task writes a journal as it works: a real file on this disk, one line per thing it
   said, called and got back, at a path like
   `~/.codeaf/v3/projects/<workspace>/<session id>/tasks/20260819-120133_7.jsonl`. The
   answer comes out of that — the decision the task made and its reasoning for it, the
   commands it ran, the files it touched by full path — and not out of the one-line outcome
   the record keeps.

Pointing at the task with `@its-name` is faster and never required: the pointer block
already carries the transcript address, so codeaf follows it instead of searching.

**When the transcript is gone, it says so.** A session folder you deleted, or work that
happened on another machine, leaves the record's row with nothing behind it. Then you get
that plainly — the outcome line and the fact that there is no journal to read — and not a
confident story reconstructed from one sentence. The task card shows the same thing its own
way: `its transcript is not on this disk any more`.

You can read it yourself too. The card behind `enter` on any `earlier` row of the task page
(`ctrl+.`) prints a `transcript · …` line, and it opens in your editor on a click.

## When a task splits its own work — sub-tasks, nested tasks, children

A task can hand pieces of its own work further out, and there are two moments it does it.
This section is the first: parts the task can see **from its brief**. The second is parts it
only finds **after opening the material**, which is *When a task turns out to be too wide for
one worker*, below. Both land in the same place — pieces under the parent, in the column and
in the room — and both count against the same five.

If its brief turns out to hold two or
three parts that do not need each other — different files, different subsystems, nothing
half-finished passing between them — it proposes each part as a task of its own and keeps
the coordination for itself. The parts run at the same time instead of one after another.

**A task coordinates its own children and nothing else.** `tasks` inside a task lists the
pieces that task started — never the other tasks running beside it under your conversation.
That is why coordination over pieces YOU handed out is not moved into a task (*Watching the
pieces you handed out does not move your answer*).

Nothing asks you about those. **A sub-task starts without a card:** the countdown card is
how a person redirects work, and there is nobody inside a task's own copy to show one to, so a
task's own proposals begin the moment they are made. What you see instead is the tree.

Where they show up:

- **the strip along the top** keeps one flat row of live chips on narrow frames.
- **the roster** draws each piece as a row of its own under what it is doing (`Running`,
  `Queued`, `Waiting`, `Done`), with its own state; it does not draw a tree. The hint line
  over a piece's row names it whole.
- **the parent's room** shows the `propose_task` calls as they are made, and the parent's
  own words when the reports come back; its header lists its children and their state.
- **the piece's own room** names the parent in its breadcrumb trail, so a task
  you walked into knows it is a piece of something.

Each piece works in a copy of its **parent's** own working copy, and its branch merges back
into the parent's — so a family's work comes home as the parent's work, in one merge, not as
three branches racing for yours. **A piece's copy is cut when that piece starts**, not when
it was proposed: work the parent had not committed yet goes with it, and so does work the
parent did after proposing it if the piece began later. A task that *divides itself* is the
other road and does freeze one world for all its parts at the split — *What the parts start
with* below says how that is written down. That is true whether the family is working on a
repository or on a plain folder: a family on a folder gets a private copy of it to work in,
and the pieces branch off that copy and merge back into it, so two pieces writing different
files never touch each other's directory. The person's own folder is written once, at the
end, when the whole family lands.

A parent never lands while a piece of it is still running. Its own turn may end long
before; the task stays open, each report is put in front of it as it arrives, and only then
is the parent's work checked and merged. While it waits it is not taking steps — so a line
you type into its room is what wakes it, and it goes back to waiting afterwards (*Steering a
task that is waiting on its pieces*). If you stop a parent, its unfinished pieces are
stopped with it and their branches are kept.

## When you change what a task is for while it is running — corrections that move the done-condition, `revise_assignment`

Say "CSV instead of JSON" into a running task and two things happen. The worker reads your
line in its next turn, as your own words; and, because that line changes what the job *is*,
it can fold it into the task's own **done-condition** with its tool for exactly that,
`revise_assignment`, naming the line you sent. From then on the work is done against what
you last said, and so is the check: the person who reads the finished work is judging
CSV, not the JSON you called off.

**The old goal's checks go with the old goal.** A task can declare the commands that
re-establish its result (`checks` on `propose_task`), and such a command is an assertion
about the goal it was declared for. A revision therefore clears them — the task's
own and any it was holding for parts it handed out — in the same instant the version moves,
so nothing that passed about JSON can be quoted about CSV. The worker may declare the new
goal's checks in the same call; when it does not, the corrected work is judged by reading
it and by what the work's own receipts show, and what was required before stays on the
task's record as history.

**Your words are kept beside the new condition.** The task's page and the checker's packet
both carry what you actually typed and what the worker made of it, so a restatement that
has drifted from your sentence is a thing you can see rather than the only account left.
What the task was first given stays on its record too — it is history, and history is not
edited.

**A correction sent while the work is being checked is kept, and the check cannot
land the task as done over it.** The task takes another round with your words instead.
There is a bound on that — a run may be sent round for corrections three times — and when
it runs out the task **stops rather than merging**: nothing goes to your checkout, the work
stays on the task's branch, and the card says what you said that it never took up.
`continue` is how you take it further.

**Most corrections change nothing about the contract, and that is the ordinary case.** "The
config lives under etc/" is a fact the work needs. "Why did you do it that way?" is a
question to answer. Neither moves the done-condition, and nothing in codeaf guesses: the
worker moves it only by making that one call, and only for a line **you** sent. What the
model says into a task with `tasks id N say` is one piece of work talking to another and
can never do it.

## When a task turns out to be too wide for one worker — a task that splits itself, dividing work, parts of a task

A task is usually one worker. It is not fixed to one.

The section above is about parts a task can see from its brief. This is the other moment:
the worker has **opened the material** and there is more of it than anybody knew when the
work was written. The directory holds eleven adapters. The search matched forty call sites.
The report needs a section per region and there are nine regions. Nobody could have known
that from the sentence you typed.

So the worker can say so. Its tool for it is `divide_work` — it names the parts it found and
what it actually saw that revealed them — and **the work splits**: each part becomes a worker of its own under the task, in its
own copy of the parent's working copy as it stood at the moment of the split — the parent's
unfinished work included, so a part can run the failing test rather than be told about it
(*What the parts start with*, above) — with its own branch coming home into the parent's.
Your transcript says it in plain words — `split into 3 parts:` and then each part by number and
name.

**The worker does not go away.** It keeps whatever part it decided to keep, every part's
report reaches it as that part lands, and the one thing it owes you at the end is a single
deliverable made out of all of it. A task never finishes while a part of it is still running.

**Not every task carries `divide_work`.** A worker has it where somebody read the work as
wide: the sizing judge at the `/task` door, the model's own reading when it proposed the
work, or a brief that already names enough separate items to be worth splitting. Work
nobody read that way runs as one worker and stays one — the verb is absent rather than
present and refusing, so nothing is said about it either way.

## What the parts start with — do the parts see the parent's unfinished work, when is the parent's work frozen for its parts, the wip commit before a split

**They start with the parent's files already on disk.** A task that has opened the material
has usually *written* something by the time it decides the work is too wide — a repro, a
failing test, scraped material, a half-drafted section. All of it is there for every part,
without the parent having to describe it in a brief.

That is not automatic; it is a commit. Before the first part is handed out, codeaf stages
what the task has written so far and commits it to **the family's own branch**, worded
`the work so far on <the task's title>, before its parts were handed out`. The parts branch
from that commit. So does the second part, and the fifth: **the world is frozen once**, at
the split, and every part gets the same one. Anything the parent writes *after* the split
reaches none of them — which is why a brief that needs a file has to be written before the
call, not after it.

Three things follow, and they are the ones worth knowing:

- **It arrives inside the merge, not on its own.** It is an ordinary commit on the family's
  branch, so when the whole family lands it comes home inside **one merge** along with
  everything else the family did — and on a repository `git log` shows it there afterwards.
  On a plain folder it stays in the family's private copy and never reaches you.
- **It is codeaf committing, never the worker.** Tasks are told they never run `git add`,
  and that is still true.
- **Nothing is committed in your own folder.** A task working *in place* — in the directory
  you are sitting in — has no branch of its own, so there is nothing to commit to and
  codeaf does not make one. Its parts share the directory, which is what "here" means.

If the family's branch is there and **cannot take that commit** — a disk gone read-only, a
repository somebody broke — **the split is refused** rather than taken on a world the parts
do not have. The worker is told in one line and carries on with the work in its own hands.
A family whose private copy could not be made **at all** is the other case and is not
refused: the split runs, the parts share your folder instead of each getting a copy, and the
task says so in one line.

**The worker is not the only one who can ask.** When a long answer of mine was handed over
because a second model read it and drew its parts, that drawing is put to this same road
beside the new task's worker, which starts at once rather than waiting for the reading — so
the parts somebody already named are handed out without the worker having to find them
again. Everything below applies to it without exception: the same tests, the same reading
by the mastermind, the same refusals. The receipt reads the same too, and reaches the worker
while it works, telling it the parts are now somebody else's so it does not do them again;
an answer that arrives after the worker has finished is dropped. *When a reply is taken out of your hands* is where that happens.

## Why it refused to split the work — it would not break the job into pieces, and the tests a division has to pass

**One test always decides, and it is not the worker's confidence.**

- **There has to be a lane free for the parts.** This is your own `task.parallel` cap and
  nothing else — the worker asking does not count, because it hands its lane back the
  moment it starts waiting on its parts. With every lane busy the parts would be done one
  at a time anyway and each would still cost a working copy, so the split is not
  taken and the worker is told to ask again once something finishes. With `task.parallel`
  set to exactly **1** there is no second pair of hands at all, and the worker is told
  plainly that asking again will not change it.

**The other test — enough separate items — is off unless you turn it on.** Until September
2026 a division also had to name at least six separate things, on a measurement that below
six, one worker doing them in order beats paying for a working copy, a check and a wait for
each part. Then the question was measured properly, four ways of planning against four
readings of that floor over 273 plans drawn and judged, and the floor lost: it folded real
divisions more often than it saved you a pointless one. Three lanes written out by hand over
one file name no pile of things at all, so the count read them as nothing and refused them.
It no longer decides anything unless you ask for it — *Can I make it always split the work*
is how you ask, and what the count reads when you do.

If a test says no, **nothing happens** — nothing is cancelled, nothing extra is spent, and
the worker carries straight on as one worker. Finding out that a split will not happen is
free, and always was.

**And what did not change is which work is offered the split at all.** A worker is only
handed the verb when something already read the work as wide: I marked it wide, the sizing
call at `/task` said so, or its own brief names six or more separate things. That last
reading is the same counting described below and it is always on. So turning the floor off
did not make everything divide — it stopped a second reading of the same evidence refusing
what the first reading had already invited.

## Can I make it always split the work — turning the width floor back on, why did it split my task into parts, CODEAF_SPLITGATE

**Why did it split my task into parts?** Because the work was read as wide, a worker asked
to hand its parts out, a lane was free, and — by default — nothing else stood in the way.
The parts are listed in the task column under their parent and each one says what it owns.

**The width floor is off by default. `CODEAF_SPLITGATE` in the environment turns it back
on**, and it is the only way to; there is no setting for it, because it picks how the
machine decides rather than anything you have a preference about.

- **unset** — off. Every division that is asked for is kept. This is what you have.
- **`CODEAF_SPLITGATE=1`** — the floor as it worked before September 2026: the work has to
  name at least six separate things or the split is refused, free, on the spot.
- **`CODEAF_SPLITGATE=judgment`** — asks the plan instead of your words. If every part is
  already the size of one sitting and none of them waits on another, the parts stand
  whatever your brief counted; where the plan has no opinion, the six-item count decides.
- **`CODEAF_SPLITGATE=0`** — off, spelled out. The same as leaving it alone.
- **anything else** — off, because off is what you get by default and a typo must not put a
  floor back under your work without your knowing.

**What the count reads, when you have turned it on.** It reads what the worker says it saw,
and counts a number standing beside a pile of things — "11 adapter files", "nine sections",
"34 people" — whatever the domain calls its things. What never counts is a number that
measures or budgets one thing: "250 words", "90 seconds", "3 retries" and "status 500" are
parameters, not piles, and evidence that names no pile at all counts zero.

**With the count on there is one exception to it.** If the work was started because a model
read your request and judged it broad — the wide line before a `/task`, a proposal I marked
wide, a message the harness moved to a task — and the count then says the evidence names too
few items, those are two readings of the same work disagreeing. The count is not the last
word there: the division goes to the mastermind, which decides it on the parts themselves.
That is the whole of the exception, it exists only while the count is on, and the free-lane
test is never waived by anything.

## What each part is told — the brief a part opens on, and how it knows what its siblings own

**What each part is told is composed, not copied.** A part opens on the same document
every task opens on: your own message word for word, then the work being divided as the
task itself was given it, then one line — `THE OTHER PARTS ARE IN SOMEBODY ELSE'S HANDS
RIGHT NOW:` — naming what each of its siblings owns and telling it to leave them alone.
Last comes `WHAT THIS PART WORKS ON`, and that part alone is what the splitting worker wrote.
What the part OWNS is under `DONE WHEN`, which is the done-condition its author gave it.
**The harness writes everything but the scope**, and it writes the same thing for a part a
worker split out and a part a second model drew — so a part never depends on the model
doing the splitting remembering to restate the job once per part. Your own sentence still
appears exactly once, at the top, where it appears on every task.

**And then the plan itself is read once, by your `mastermind` model.** The tests above are
about whether a split is worth it; neither of them reads the parts. But what a part owns is
everything that worker will act on — it is not handed your conversation and cannot
ask anybody anything, though the brief names the journal path and line of your original
words so it can read them if the restatement was cut — and the scopes were written by whatever model the task itself runs on.
So once those have passed, the whole division goes to the mastermind at once: the
evidence, the work it came out of, and every part beside its siblings. It can sharpen a
scope, fix a boundary two parts share, fold two parts into one, or say the parts are really
stages of one procedure and not a division at all — in which case nothing is split and the
worker carries on, exactly as a no from a test above. So the parts you see may be fewer
than the worker asked for, and what they own may not be word for word what it wrote.

**And it has one more answer, which is not about the split at all.** The mastermind may
read the work and find that what is left of it **cannot be done by a worker** — an approving
review only a named person may give, a credential or an account nobody here holds, a
decision that is yours to make, or a step that is somebody else's system doing something by
itself. When it says that, the task does **not** start a worker: it lands straight away
needing your look, with the mastermind's own sentence as its report, and the only thing
spent on it is that one reading. The next section, *A task that landed needing your look
without doing anything*, is what you see. This is a deliberate word the mastermind has to
reach for; a mastermind that merely thinks the split unwise, or would rather one worker did
this, has refused a division and the worker carries on with the work exactly as above.

## Two parts cannot own the same file — a division refused over an overlap

**No two parts may own the same file, and that one is not a judgement — it is
enforced.** What a part owns is what its **done-condition** names — the sentence that says
what must be true once that part is finished — so those are the files that are checked
against each other, and if the same file is named by more than one part's done-condition
the division is **refused before anything is handed out**: no part starts and no working
copy is made. A part's brief is read for none of this: it names the material that part
works on, which includes everything it only reads, and **a file two briefs both name is
not an overlap** — the plan they all start from, the notes they all draw on, the sibling's
file one brief mentions so that its worker leaves it alone.
The worker is told which file — "`report.md` is claimed by more than one part" — and can
redraw the boundary — give each part a file of its own and say so in its done-condition —
and ask again. The reason is that everything the parts write goes
into **one deliverable**: a file two parts wrote is kept once, and the other part's
version of it would simply be gone, with no conflict for anybody to notice.

**And no two parts may be told to run the same check.** Each part is finished against the
check that proves **its own slice** — the files that part produces and no others — and one
check that *every* part was told to run is the **family's**, not any part's, so that
division is refused too, before anything is handed out, and the command is named. The
reason is the same shape as the one above: the parts work side by side in working copies of
their own, so a check written into three done-conditions runs three times, and every one of
those runs judges a tree that does not hold the other parts' files yet. **The whole run is
the parent's to make once, after the parts' work is home.** So the road out is to give each
part a check over the files it produces, keep the family-wide run in the parent's own
done-condition, and ask again — it is not a finding that the work cannot be split. Two
checks that name different things — a package each, a file each — are two checks and are
admitted; nothing here reads which program is being run or how long it takes.

**And you are only told once.** If the same task asks again with the same shared check still in
every part — which is what a worker does when it cannot rewrite three done-conditions — the
division is **taken** rather than refused a second time, with that check **removed from every
part** and given to the parent instead. It is never left on the first part: the whole finding
is that it belongs to the family. The parent is then told, under its own done-condition, that
these checks are the family's and are to be run **once, after every part's work has come
home**, and its own checking is allowed to run them. Nothing rewrites what the parent was
admitted with; the checks are something the task now carries alongside it. A refusal you
cannot act on is worse than a wasteful split, and a worker that spends its steps asking the
same question is a task that finishes nothing.

**Shared material is read, never written — and that half is not enforced.** A file every
part reads is nobody's to change: everything the parts write goes into one deliverable, so
a shared file two of them edited is kept once and the other's edit is simply gone, which is
the same loss the rule above exists to stop. A part that finds something wrong in shared
material says so in its report and leaves the file as it stands. Nothing checks this: the
reading compares the parts **with each other**, never a part against the work it started
from. What holds it is what the parts are told — the splitting worker is asked to say it in
the brief of any part it hands shared material to, and the mastermind that reads the plan is
asked to write the file a part produces into that part's done-condition where the brief names
files and the done-condition names none.

**It is checked twice, and the first one is free.** The parts as the worker wrote them are
read before the mastermind is, so the commonest case — a worker that drew its own
boundaries badly — is refused for **nothing at all**, and the worker is told so. The parts
the mastermind settled are read again afterwards, because it can sharpen a part onto a
file its sibling already owns; a refusal there has cost that one reading and nothing else,
and its wording does not pretend otherwise. Only the **same file** counts either time. Two
parts working in one directory on different files are independent and always were, and so
is one part owning a folder while another owns a file inside it.

**This reading can only ever improve a split; it cannot lose you one.** If the mastermind
cannot be reached, times out, or answers something unusable, the division goes ahead **as the
worker wrote it**. It had already passed everything that was going to refuse it, and a second
opinion that cannot be had is not a reason to throw work away.

**Except on the one division it is deciding rather than sharpening** — the exception above,
where the count said too few items and a model's reading of your request said broad. There
the mastermind is the only thing that has said yes to those parts, so if it cannot be
reached nothing is admitted — and the worker is told exactly that: nothing was decided, ask
once more. The unanswered ask costs nothing and is not held against the work; only a
mastermind that actually answers settles the question, and its no is then final for that
task. Nothing is lost either way: an unreachable mastermind cannot admit a split, and it
cannot cancel any work.

## Which model each part runs on — ordinary parts, careful parts, and the grade the worker sets

**Some parts are done with more thinking than others.** Each part carries a grade the worker
sets. Most parts are ordinary work — the failure mode is simply not being done yet, and you
can see whether it happened — and those run on the same model the task itself is on. A part
graded **careful** is one whose failure mode is subtle wrongness: a design decision, a tricky
piece of debugging, a judgement about somebody else's code, where the work can look finished
and be quietly wrong. Those run on your **careful work** model instead — the same class
the check at the end of a task uses. The mastermind that reads the plan can promote a part to
careful too. If you have not set the four class rows at all, every part runs where its task
runs and the grade costs you nothing; *Models and cost* has the rows and the `/crew` word
that writes all four.

**And the grade is not the last word — what has actually happened here is.** Every task
that settles writes down what the check said about it, against the model it ran on and
the name the work was given: `codeaf models` is where those rows show up. When a part is
about to be handed out as ordinary work, codeaf looks that record up first. If work
named like this one has been turned down by the check **twice or more** on the model the
task is on, and the balance of those answers is against it, the part is minted on your
**careful work** model instead — even though the worker called it ordinary. Two names
count as the same kind of work when they share half their words or more, so *tests for
the rail* and *tests for the composer* are one thing and *the eleven adapters* is not.
Nothing is spent to work any of this out: the check had already read the work and said
so, and no extra model call is made to grade it. A part the worker itself graded careful
is never moved back down, and an install with no class rows set never moves anything,
because there is nowhere dearer to move it to.

**A busy machine is not one of these tests.** `task.max_load` and `task.min_free_mb` never
refuse a split. If the machine is over one of them when the work divides, the split happens
and the parts simply **wait** — the same wait any queued task does, drawn as
`waiting · machine busy` — and they start themselves as soon as the machine clears. The
worker is told so in its receipt and has nothing to come back for.

**A `/task` can be split for its worker, too.** The sizing call reads a `/task <brief>` for
width beside its first worker, and where it finds more than one job in your words, those
parts are put through the same tests below — the worker does not have to find them again.
Nothing is printed before the work starts; the parts arriving as rows of their own, and the
worker's transcript saying what was handed out, is how you learn it happened. A no from the
call changes nothing.

**And this is what I do with a wide change too.** When I hand work off myself rather than
you typing `/task`, `propose_task` carries a `wide` flag, and I set it whenever the work
that must be checked and landed is broad — the same edit over many separate items, a sweep
that writes across many files. It still starts **one** task, armed to split itself; it is
not a planner and not three tasks. A wide **read** never comes down this road at all: a
survey or a comparison across many packages is quick tasks in your own folder, one per
part, and *Quick task or a proper task* is where that is decided. **There is no planner on my belt at all any more**, and there
is no sentence you can type that reaches one either, so width has nowhere else to go —
*adaptive runs*, under *How do I start an adaptive run*, is the whole of that answer.

A task that was never read for width at all (`/task solo`, the `single` row, a proposal I
did not mark wide) says nothing up front and can still split, off the items its own brief
already names.

**And so can work that runs while you are asleep.** A standing order that fires and starts
work is on this road too, armed the same last way — off the items its own brief names,
with no sizing call, because a firing runs on a rhythm you set once and a model call every
night to re-read the same sentence is a bill nobody agreed to. The tests still decide and
the plan is still read once before the parts exist, and the machine is still respected: a
division at 3am on a loaded box is admitted and the parts wait for it. The firing stays open until its parts are home and their spend is on its
own cost row. The standing orders page has the rest of what an unattended run is.

## Where the parts show up on the screen, and how to stop them

**Where you see it:** the parts appear in the task column as rows of their own, each under
what it is doing, with their own state, exactly as pieces handed out from the brief do. Walk into the parent's room and its header lists each part by name with the state it is
in; walk into a part and its breadcrumb trail names the parent.

**Stopping.** Stop the parent and its unfinished parts stop with it, their branches kept.

It is on by default and there is no setting for it. `CODEAF_SWARM=0` in the environment
turns the whole road off — no task splits at all — and `CODEAF_SPLITGATE` decides whether a
width floor stands under the splits that do happen, which is off unless you set it
(*Can I make it always split the work*). Both are environment pins rather than preferences,
which is why neither is in the settings panel.

## How deep tasks nest, and how many pieces one task may hand out

Two hard bounds, and they behave differently on purpose.

**Both bounds count quick tasks and ordinary ones together**, and `quick_task` is withheld
at the floor exactly as `propose_task` is.

**Depth: 3 levels.** The conversation proposes a task; that task may propose pieces; a
piece may propose pieces of its own share; a piece of a piece may not. Neither
`propose_task` nor `tasks` is on a third-level task's belt. `propose_task` creates
children; `tasks` lets a task inspect and manage only its own children, not its parent,
siblings, or unrelated tasks. Both tools share the depth gate. A child saying the `tasks`
tool is unavailable is therefore describing a capability limit; it does not mean the
model chose to avoid delegation.

**Fan-out: 20 pieces per task**, counting both ways a task hands work out — parts it saw in
its brief and parts it found once it opened the material. A task that asks for one more
gets its call answered with:

> no: you have already handed out 20 pieces of this work, which is as many as one task may.
> Do the rest in your own hands, or finish these and report what is left undone.

It reads that as an instruction and does the rest itself.

Neither bound is a setting, and neither decides how wide work goes. Twenty is there to stop
a task that has lost the plot, not to ration breadth: when a job's parts are independent,
the aim is the shortest wall time for the whole of it, so they are all handed out at once.
Whether a task splits at all is its own reading of the material, and sequential work never
splits.

`task.parallel` still applies to the whole session: pieces queue behind it exactly as
top-level tasks do.

## How many tasks run at once — can I have it do two things at the same time, can you work on several parts of my answer at once

**There is no limit by default.** codeaf does not cap the number of tasks running at the
same time.

The setting `task.parallel` exists for anyone who wants a number anyway — settings panel
(`ctrl+,` or `/settings`), category "spending". Blank means no limit. A cap is a queue and
never a refusal: work past the cap waits and starts when a slot frees, and while it waits
its roster row says `waiting · slot` on its hint line, and it sits under `Queued`, which
starts folded to its heading.

What actually runs out is the machine, not a count of tasks. Two real ceilings hold new
starts instead:

- `task.max_load` — the one-minute load average divided by core count, default **1.5** per
  core. At or above it, nothing new starts and a held task's row reads
  `waiting · machine busy`.
- `task.min_free_mb` — a floor under available memory, default **1536** MiB. Below it,
  nothing new starts — and each running piece sets aside a footprint of memory against
  that floor until a reading shows it, so a wide hand-out runs what the memory can hold
  and queues the rest on `machine busy` (how-tasks-run has the arithmetic).

Both gate starts only. Nothing already running is ever touched; the pressure drains as
running work finishes, and the check is re-asked every 5 seconds.

**The honest caveat:** these two governors read `/proc/loadavg` and `/proc/meminfo`, so
they only apply on a machine that has them. Where there is no `/proc` — macOS, Windows —
the governor cannot say anything and therefore never holds. On those machines
`task.max_load` and `task.min_free_mb` do nothing at all.

Separately, a task that is already running can be held by the provider's own pacing. Its
row reads `waiting · rate limited` until the calls get through — or until the task's
patience runs out, which is **four and a half minutes** of trying, machine after machine;
your own turn gives up sooner, at **90 seconds**. Neither is a number of attempts: it is
how long, and nothing counts tries. See how-tasks-run.

The frontier used to hold two tasks at once. Two was a guess standing in for a resource
nobody had measured: idle on a sixteen-core box, one too many on a laptop already compiling.

## Naming a model for one task

You ask in words — "let opus do this one", "run that on gpt-5". There is no key, command or
field for it: the model that grooms the work sets the model on the proposal. The word may
be a whole catalog id (`anthropic/claude-opus-5`), the tail after the vendor
(`claude-opus-5`), or any tokens that appear in one id (`opus 5`). Case, stray spaces and a
leading `~` are ignored. Three things can happen.

**One match — it is used and nobody is asked.** The card's meta line names the full id,
and the model's receipt reads `task 7 started on anthropic/claude-opus-5: <title>`.

**A few matches — a hole in the question.** Two to four candidates put one line on the
proposal, above its answers:

```
     run it on [ anthropic/claude-opus-5 ▾ ]
```

`←` and `→` walk the shortlist. It is a correction, not a gate: the countdown is already
running on the closest match, which is what the hole opens on and what silence takes.
**Moving it answers nothing** — the question is still whether the work goes at all, and the
clock keeps running while you look. Whichever model is in the hole when you press `1` is
the model the work starts on. Only a member of the shortlist can win, and a shortlist with
one member draws no hole at all: there is nothing to ask.

The digits are the question's answers and never the models — `1` is `start it`, `2` is
`no`. That is the same grammar on every question codeaf asks you, which is why the models
moved off the digits and onto the arrows.

Name nothing and the task runs on `task.model` if you have set it, otherwise on your crew's
**worker** class (`hands` in the `/crew` line — `z-ai/glm-5.3-flash` on the shipped
`balanced` crew), and only when that row is blank on the model the conversation was on
**when the task was admitted**. The id is settled at that
moment and remembered for the task's whole life — it survives a restart, and switching the
conversation's model afterwards does not move work that was already handed over. This
holds for `/task` and for a task the model proposed alike. What *can* move it afterwards is
you, from inside that task's own room — see the next section.

**A model you named is kept even when the work is sent back.** If a check finds gaps, the
worker that closes them normally runs on your crew's careful model rather than the task's
(see *What happens when the work is not right yet*) — but only where nobody named a model.
Name one, here or in the task's room, and every round of that task runs on it.

## Changing the model for one task while it is running — switch, change or swap a task's model

**Walk into the task's room and press the model's name at the bottom of the screen.**

While you are in a room the status line names that node: `<mark> <task name> · task
<model>`. Press the `task <model>` part and the ordinary model picker opens, aimed at that
task. Choose a row and that task moves onto it.

The footer's state also belongs to the open task: working, queued, awaiting your
look, or its recorded outcome. It does not borrow the main conversation's idle
state or running clock. Leaving the task restores the conversation's status.

**On a narrow window that line gives way in one order.** The task's *name* is drawn whole
for as long as the row can hold it; then `task <model>` is dropped **whole** rather than
shortened, because a bare model id in the one spot that has only ever held the
conversation's would read as the conversation switching models; and only after that is the
name itself cut with a `…`. So a window too narrow for both says where you are rather than
what is answering — and a model that is not drawn cannot be pressed. Widen the window, or
read the model on the task's own card.

What that does, exactly:

- **It takes effect at the task's next request, not its next turn.** A task step is one
  turn and can run for twenty minutes, so "next turn" would mean your pick does nothing
  today. Which of two things happens depends only on whether the request the step is
  inside has given you anything yet:
  - **Nothing has come back** — it is still reaching a machine, waiting out a pace, or
    walking away from a refusal — and that request is **let go of at once** and asked
    again on the model you chose. The room says `switching now`. Nothing is lost,
    because nothing had arrived.
  - **Something has already come back**, including thinking you can see. It finishes on
    the model it started on — killing a reply you are reading would throw away work you
    have paid and waited for — and everything the step asks for after it is on the new
    model. The room says `the next request takes it`.
  Your word reaches the work within a second either way. A step already moving down its
  own rescue chain starts that chain again from the model you named, so it never keeps
  walking away from your choice.

- **And nothing quietly takes it back.** When a task's model stops answering, codeaf moves
  the work to another one rather than failing it — but if you have picked a model in this
  room, that pick is where it moves to, not the next name in codeaf's own fallback list,
  and the run's log says `moving to <model>, which you chose`. That includes a step
  grinding on a machine that keeps saying `temporarily rate-limited upstream`, which used
  to be the one failure that moved nothing at all: codeaf stayed on the machine pacing it
  and said `staying on`, for the whole four and a half minutes a task's call is given.
  Until 2026-09-11 the fallback list won, so a task could finish on a model nobody had
  chosen while the room showed the one you did.
- **Unless the work is already being checked, in which case the pick is saved for the next
  run.** A running task is running across three lives: its own worker, the gate reading
  what that worker left, and any repair round. Once the gate is reading, the worker has
  stopped, so there is no request left for the pick to reach. It is kept the way a finished
  task's pick is kept — the room reads `next model <id>`, the sidebar heads itself `Next
  run setup` — and it applies if you continue the work. The model on the row does not move,
  because that model is the one the work actually ran on.
- **It moves that task and nothing else.** The conversation stays on its own model, and so
  does every other task. Walk back out with `esc` and the status line is the
  conversation's model again.
- **A note is written in the conversation**, reading `task 7 · model · <the model you
  chose>`, so the change is on the record where every other model change is.
- **New tasks are unaffected.** Work admitted after this still follows the ordinary
  ladder: `task.model` from settings if you have set one, otherwise the crew's worker
  class, otherwise the model the conversation is on. A pick made inside one room is not a
  preference the session learns.
- **The row, the roster and the finished card all say the new model** from that moment on,
  and the change survives a restart. A pick that was saved for the next run instead leaves
  all three naming the model the work ran on, which is what a bill can be reconciled
  against.

The picker offers the same rows `/model` offers, and it opens with the cursor on the model
the task is already running — so `enter` confirms rather than changes. `esc` leaves
everything as it was.

There is still no command, key or setting for this: the model's name in the room is the
only door. `/model` always means the conversation.

## I typed continue into a running task and nothing happened — does typing into a running task reach it straight away, my correction into a task's page was ignored, telling a task to carry on

**It reaches it straight away, on the same clock as a model pick.** Anything you type
into a running task's page — `continue`, a correction, a fact it is missing — lands by
the same rule:

- If the request the step is inside has **given you nothing** — still reaching a
  machine, waiting out a pace, walking away from a refusal — that request is **let go of
  at once** and the step's very next one carries your words. Within a second.
- If **something has already come back**, including thinking you can see, that request
  finishes first and your words ride the step after it. Nothing you were reading is
  taken away to hear you sooner.

**It did not use to.** Until 2026-09-11 a line typed into a room was written down
perfectly and only read when the request it interrupted ended **on its own** — so
somebody watching a step sit on `waiting · rate limited · 13m 37s` could type `continue`
and be answered thirteen minutes later. The words were never lost; they were just not
heard. If you typed `continue` more than once while that was happening, each line is a
line and each one arrives.

**What it is not.** It does not stop the task, and it does not restart it: the step
carries on with your words added to what it knows. To stop the work, use `/stop` or the
Stop task button — *Stopping a task* below.

## Why can't I change the model here — the model's name is not pressable

**Because the task is not running any more.** A finished, failed, stopped or
needs-your-look task's model is a fact about what already happened, so the name is drawn
for you to read and there is nothing to press. The same is true of a task that is still
queued, of an adaptive run's page — a run is a fleet of nodes rather than one — and of any
node inside a run.

If a task lands in the instant between your reading the name and pressing it, the refusal
is said out loud rather than swallowed:

```
task 7 is done, not running
```

A stopped or failed task says the same thing with its own word in place of `done`.

Two more places the name is not a door. At phone width the status line becomes a two-row
deck and the task's model is a chip on the second row: tapping it opens the status sheet,
which names the conversation's model and the task's on two labelled lines, and only the
conversation's line is a door. And if you have turned the mouse off (`ui.mouse`) there is
no way in at all — the model's name is a pointer target and has no key.

## When no model matches the word you used

If you name a model for one task and nothing in your catalog answers to that word, the
attempt comes back as a refusal the model can correct in one round trip. With near misses:

```
no model here is called "opos-5" — did you mean anthropic/claude-opus-5, anthropic/claude-opus-5-thinking? Name one of those, or leave model out to run on <default id>.
```

With nothing in common at all (a word like "fast" or "cheap"):

```
no model here is called "fast". Name a model id the person has, or leave model out to run on <default id>.
```

A word matching more than four ids is refused the same way, because that is a list and
not a shortlist: `"claude" matches several models — say which: a, b, c, d.`

No proposal reaches you until that is settled. The model can name one of the ids the
refusal offers, or leave the model out so the work runs on the default.

## Stopping a task — how to cancel or kill running work

**`x` stops it, and it asks first.** Press `x` over an empty message box: with the roster's
cursor on the task, or inside the task's room, or — when the roster does not hold the
keyboard — with exactly one stoppable task visible, where the key needs nothing first.
One card comes up:

```
? Stop this task? Its work halts; the branch it wrote on is kept.
  [stop it]   [keep going]
```

The cursor opens on `keep going` — the destructive answer is never under the key you
press to dismiss a question. `left`/`right` move, `enter` takes, `esc` is `keep going`.
**`x` never bypasses it: the card is always asked**, because `x` is one bare keystroke over
a list and the work behind it may be an hour old.

With a pointer, the `Stop` at the right end of a room's facts row — the second row of its
header, under the breadcrumbs — raises the same card.
Strip chips do not carry a stop button.

**There is one other way to stop a task, and it asks no card.** On the **sessions** place
(`ctrl+.`, `/history`), `→` on a task this conversation is holding opens the row's verbs and
draws `s stop it`; `s` then ends it. That is two deliberate presses with the word on screen
for the second of them, which is what the card protects `x` from being without — and the
card cannot be drawn over a full-screen place anyway, so it would be a question nobody could
see. It uses the same door in the engine and answers with the same sentence.

**What stopping does.** A task that is RUNNING has its worker cut off where it stands: the
turn it was in the middle of ends, and the task settles as `stopped`. A task still QUEUED
is dropped instantly, reads `stopped before it started`, and anything waiting on it is
told its prerequisite will never finish. Either way:

- **its branch is kept, with its work on it.** Nothing it wrote is thrown away: whatever
  reached disk is committed onto the branch, and the landing card names the branch and the
  files, exactly as it does for every other early ending. **A quick task has no branch**,
  so there is nothing to commit anywhere: what it had written is in your folder as it left
  it, possibly half made (*Stopping a quick task*).
- **what it spent is what it spent.** The figure freezes where it was.
- **it is not a failure.** The roster draws `■` rather than the failure cross, the room's
  header reads `stopped`, and the model is told the task was *stopped* — so nobody goes
  looking for a fault that is not there.

**Between the card and the landing the header reads `stopping`.** A running task is not
stopped the instant you answer the card: its context is cut and its worker takes a moment
to wind up, so for those seconds the task is genuinely still running, and both the line
you are shown — `stopping task 7 (Fix the parser) — its branch is kept` — and the header
say the same present-tense thing. The word becomes `stopped` when the task actually lands.
The spinner beside it deliberately keeps turning, and that is not a contradiction: a task
is work happening somewhere else that really is still happening, unlike a turn in the
conversation, which is work you are sitting in front of and which stills the moment you
press `esc`.

Pressing `x` twice, or on work that has already landed, does nothing but say so — the
second press answers `task 7 (Fix the parser) is already stopping`.

**You can still ask in words instead** — "stop task 7", "cancel task 7" — and the model has
a real stop of its own: it calls `tasks` with that id and `stop`, which is this same door,
so the ending is identical. It asks no confirmation card, because the sentence you typed is
already the answer to that question, and your reason goes onto the task's record beside the
word `stopped`. The key is faster and does not spend a turn. **A line sent INTO a task is
never a stop** — the model's `say`, or your own words in its room, are messages the worker
may ignore or answer while carrying on, and a worker that then delivers nothing is read as
unfinished work and handed back for a round of `closing gaps` while it goes on spending.
`Asking the chat to stop a task` on the task-controls page has the whole of it.

What else you can do yourself, on a task that is running:

| what | how |
| --- | --- |
| see it | its roster row, its room, an inline `task 7` link, or its strip chip on a narrow frame |
| see what it is doing this second | the roster row's tool line, or its room, live |
| see what it is costing | the roster's telemetry row, the room's focus header, the `Σ` |
| walk into it | click it, `enter` on it, or `→` over an empty box |
| talk to it | `enter` on a sentence in its room |
| read its whole transcript | its room |
| copy text out of it | `ctrl+b` in its room |
| refer to it in conversation | `@<slug>` |
| leave it | `esc`, `←`, or `←←` — the work keeps running |
| stop it | `x`, or `Stop` on its room's facts row — one confirmation card, always |
| change its brief or its done-condition | send your correction to the running task; its worker uses `revise_assignment` and keeps your words beside the new condition |
| continue a failed or finished one | say `continue task 7`, or `tasks` with `id` and `continue` — same node, same copy |

Steering sends your words into the task's own loop verbatim, and they land in its room as
your own line. If nobody is listening any more — it landed, it was stopped, its worker is
gone — the room asks rather than dropping the sentence or quietly sending it to the main
model: `<title> is parked — [r] revive and send · [m] send to main · [esc] cancel`, with
the engine's own reason on a dim second row. `r` leaves the room and asks the model to start
the work again with your instruction; `m` leaves the room and sends your words to the model
unwrapped; `esc` cancels and leaves your words exactly where they are in the box.

The same question also rises on a task that is **still running** but momentarily has
nobody inside to read a line — while it says `checking what it left`, or in the seconds
its work is landing. That guard reads `<title> cannot read this right now — [m] send to
main · esc cancel`: no revive, because the work is not over and starting it again would
make a duplicate. Wait for the check to land, or send the thought to main.

Whenever a task stops for any reason it wears its own word — `stopped` when you ended it,
`incomplete · <the reason>` otherwise — with `branch kept` and the branch name beside it.
Nothing is thrown away: on every ending except a clean merge the branch is kept and named,
and what the task made is committed onto that branch before it lands — so the files it
produced are listed under `changed:` and `git merge task/…` brings them over. The merge is
never done for you, because only work that was checked reaches your branch.

## Continue task N — keep going on a failed or finished task, No task 1 in this project

When you say `continue task 7` or `keep going on task 7`, the model calls `tasks` with that
id and `continue`. That re-arms the **same** task — same id, same brief, same working copy
and journal, the last report as this round's finding — rather than proposing a new one.
The finding carries what the last attempt actually produced, not only the three lines of
its card, so the second attempt does not have to work the answer out again.

It only works for a task **this conversation** still holds. A task from another
conversation or another window, an unknown id (`No task 1 in this project` on a read), or a
task that is still running cannot be continued here. The tool then says there is no graph
left to continue it in, and names where the work is — its branch or working copy — so it
can be read. The model should relay that, not narrate progress it did not make.

Starting the same brief again with `/task` or `propose_task` is new work with a new id, and
it is the wrong door when you mean keep going.

## Why is the task waiting for me — what does your call mean, a sub-task needs my look, a nested task waiting on me

`your call` is the one tier that wants something from you: **the machine has done what it
can, and the rest is a decision only you can make.** It is neither done nor incomplete.
Nothing has merged, the branch is kept, and anything waiting on that task stays waiting
until somebody answers.

The word never stands on its own. The reason is on the row beside it, in a plain sentence —
`your call · nobody could check it`, `your call · conflicts with your branch: parser.go` —
and the card under it carries the same sentence and the two answers.

Read it first. Its room holds the whole of it, and its landing card expands to the changed
files, the branch, the model, the cost, the done-condition and the report.

**A nested task asks the same way — a sub-task needs my look, a piece of a bigger task
nobody checked.** Depth changes nothing about whether you are asked: the card, the roster
row and the sub-task's own room all offer the same answers from the moment it lands. What
depth changes is how LOUD it is. While the task above it is still running, the sub-task is
**not in the needs-you band**: it sits under `Waiting`, because that task's own agent is the
one being asked and has the diff to read; when the head settles, one line says the question changed hands
(`task 4 has finished, and the piece of work it handed out that nobody could check — task
6, Port the parser — is now waiting on you rather than on it.`). It is never filed under
`done`.

**It also stands on home**, in the `needs you` strip, named after the task and saying
`landed` and how long it has been waiting — from any project, in any conversation, whether
or not that conversation is open. Pressing that row opens the conversation that ran the
work **with the task's own record card in front of it**. It stays on the strip for as long
as it takes: nothing ages it out, and only your decision moves it.

## What your call can be asking — the six questions, and what a and n mean on each

There are exactly six things a `your call` row can be asking, and each closes with its own
two answers — a yes and a no, in the words that question deserves. Two of the six have more
than one sentence: a check that could not answer says whether the clock is why, and there
are three ways to end up with two versions of the same file:

| The reason on the row | its yes | its no |
| --- | --- | --- |
| `nobody could check it` | `accept` | `not right` |
| `the check ran out of time` | `accept` | `not right` |
| `the check did not pass it: <gaps>` | `accept anyway` | `not right` |
| `conflicts with your branch: <files>` | `resolve it` | `drop it` |
| `your branch changed the same files while it worked: <files>` | `resolve it` | `drop it` |
| `your folder already has files the task wrote: <files>` | `resolve it` | `drop it` |
| `starts on your word` — a proposal with no clock on it | `start` | `don't` |
| `design ready to approve` — a subharness wrote its design | `approve` | `decline` |
| `paused at the $5.00 cap` | `raise the cap` | `stop it` |

The third, fourth and fifth are **one question with three true sentences**: the branch would
not merge, or it would have merged and your own branch changed those files while the task
worked, or your folder already holds your own uncommitted copies of the very files the task
wrote. All three hand you the same two answers, and the sentence says which happened.

The first two rows are **one question with two true sentences** as well. `nobody could check
it` means the checker would not start, the provider failed it, or it answered neither way;
`the check ran out of time` means its call was cut by its window before it said the word.
Both ask whether the work is right, with the same two answers, and nothing merges on either.
The second is **a fact about the checker and not about your work** — it says nothing about
whether the work is right — so read the work and the checker's account under the row before
answering (how-tasks-run, *The check was asked twice*).

**The first four are the ones a landing card asks**, and there the two answers are chips,
always the same three columns in the same order with the same keys — only the words on them
change:

```
a <yes> · n <no> · s tell it
```

So `[a]` is always **yes to what the row is asking** and `[n]` is always **no to it**, and
you can read either off the chip rather than remembering a rule.

The last three are asked **before or during** the work rather than at its landing, and each
has its own card with its own answers: the proposal card (`yes · redirect · no`, above), a
subharness design's own page, and an adaptive run's spend gate. The row and the note read
the same six sentences whichever card is drawing them.

Every chip is a key **and** a click, and they are drawn on the **question block** above the
message box — the same block every other decision in codeaf is put to you on — so they are
in the same place whichever page you are standing on: the conversation, the task's own room,
the `/tasks` page. The landing card in the transcript keeps the head and the sentence saying
what is being asked; the answers are on the block. The letters work only over an **empty**
message box, exactly like `x`: a letter typed into a sentence stays a letter.

**The three answers sit on one row.** The one exception is the landing whose `[a]` moves
files of your own — *Your folder already has files the task wrote*, below — where the block
gives each answer a line so it can say what pressing it will do.

**No sentence on a card ends in `…` hiding the thing you need.** Where a reason is too long
for the width, the list of files is what gets cut — never the verb, and a chip that will not
fit is dropped off the end with a dim count of what went (`· +1`) rather than squeezed.

**A chip codeaf cannot spend is absent, not broken.** Where there is no door behind an
answer — no working copy left to run a merge round in, for instance — that column is simply
not drawn, and its letter does nothing rather than failing when you press it.

## How do I accept a task — what accept and not right actually do

- **`a accept`** — you looked and you are taking the work. Its branch follows the same
  landing as checked work: it merges into an ordinary checked-out branch, or is kept off a
  protected, moved or detached checkout. Everything queued behind it unblocks. The report
  leads `you took this as done`. If that merge conflicts nothing
  is forced: your checkout is left exactly as it was, the branch is kept, and the task comes
  back as `your call · conflicts with your branch` with the clashing files named.
- **`n not right`** — you looked and it is not finished. The task becomes `incomplete`,
  its branch is kept, and its previous report is kept under the refusal. Its dependents do
  not advance and land `incomplete · was blocked by another task`. The report leads
  `incomplete — you said it is not finished`.
- **`s tell it`** — you have something to say rather than an answer to give; the next
  section but one is about that.

**A card with no answer left to give draws no chips at all.** `s tell it` and `[d]` both
MOVE the question rather than answering it, so a row that offered only those would have
stopped being a question — the card draws them beside an answer or not at all.

**Answered means the chips are gone, not greyed.** They are replaced by the one receipt
line every question leaves, `✓ <the card's head> → accept · you · 14:02 · c change`:
the pick in the card's own words (`accept`, `not right`, `resolve it`, `drop it`), who
decided — `you`, `codeaf, on your settings` when the model settled it, `another window`
when somebody else got there first — and when. The report under the second card then
leads `you took this as done` or `incomplete — you said it is not finished`. After `[d]`
nothing is decided yet: the answers stay drawn and the reason row reads `codeaf is
deciding`.

The card's own head is **not** rewritten — it is the record of how the work came home, kept
branch and all. What follows is a **second** card, when the task re-settles into `done` or
`incomplete`, saying what became of the work. The transcript then reads as what happened:
this landed as your call → you took it as done → `task 7 done · merged`.

**You can also just say so.** "accept task 7", "that one isn't finished" both work: codeaf
holds the same door through its `tasks` tool, and whichever of the two is used first wins.
The other finds the question already gone and says `already answered` rather than raising
an error.

## The task has a conflict — conflicts with your branch, resolve it or drop it

When the same file changed on both sides, nothing is forced onto your branch: the merge is
abandoned, your checkout is left exactly as it was — no `<<<<<<<` markers in your files —
and the task comes back to you reading

```
? ◆ Port the parser · your call · 6m40s · 2 files · branch kept · task/parser
  conflicts with your branch: parser.go, parser_test.go
  a resolve it · n drop it · s tell it
```

- **`a resolve it`** tries to bring the two versions together and land the work.
- **`n drop it`** says the work is not to be taken. The task settles as not finished and
  **its branch is kept**, so nothing is thrown away and you can still read what it wrote.

Where git would not say which files it was about, the sentence simply stops after
`conflicts with your branch` rather than trailing off after a bare colon.

**A conflict is never handed to the model, whatever `task.settle` says.** It cannot merge
by decree: which of two versions of your own file survives is yours to say, and no verb the
model has merges anything. A landing that conflicts tells the model as much in as many
words — `its branch conflicts with the person's and that is not yours to accept` — so what
it does with one is describe the clash and leave the choice with you.

## Someone else changed the same file while the task was running — the ground moved

The other way to end up with two versions of one file is that nothing conflicted at all: the
task's work passed its check and its branch **would** have merged, and while it worked you —
or another window, or another task — changed the same files on your own branch. Merging it
quietly would put its version over yours without anybody looking, so it stops and asks:

```
? ◆ Port the parser · your call · 6m40s · 2 files · branch kept · task/parser
  your branch changed the same files while it worked: parser.go, lex.go
  a resolve it · n drop it · s tell it
```

It is the same question a conflict asks and it takes the same two answers. **`a resolve
it`** brings your branch into the task's branch — often with nothing for anyone to resolve,
since the two would have merged — checks the two changes together and lands the work.
**`n drop it`** keeps the branch and takes nothing, so both versions survive and merging
is yours to do when you want it.

This row used to read `nobody could check it`, which was untrue twice over: it **was**
checked, and it **held**. Nothing is ever merged behind this card, and your checkout is
untouched — no markers, no half-merge.

**It is not handed to the model either**, whatever `task.settle` says. The note it reads
says `their own branch changed the same files while this worked, and that is not yours to
accept`, so what it does is tell you what moved and leave the choice with you.

## Your folder already has files the task wrote — your own uncommitted copies

The third way to end up with two versions of one file is the one you are likeliest to have
caused yourself: the files the task wrote are **already sitting in your folder**, written by
hand or by an earlier turn, and git is not watching them at all. A merge would have to write
over work nothing else has a copy of, so the landing refuses, leaves your tree exactly as it
was, and asks:

```
? ◆ Emails for the leads · your call · 25m · 15 files · branch kept · task/emails
  your folder already has files the task wrote: leads-contact-sheet.md, research/method.md

? Emails for the leads
  a  resolve it  lands the branch, and your own copies are carried aside and put back —
                 kept beside the task's as .yours where both wrote the same file
  n  drop it
  s  tell it
```

**`a resolve it`** lands the branch and carries your own copies aside and back: where the
task wrote the same file, your copy is kept beside it as `<name>.yours`, and where it did
not, your copy goes straight back where it was. **Nothing of yours is ever deleted.** The
answer says so before you press it. **`n drop it`** keeps the branch and takes nothing.

**This is the one landing that asks on a card rather than on one row.** Every other `your
call` puts its three answers on a single row above the message box — `a <yes> · n <no>
· s tell it` — because the reason is already on the landing card in the conversation and
one row is enough. Here `[a]` **moves files of yours**, and a sentence saying so has
nowhere to go on a row, so the block gives each answer a line of its own and writes the
consequence beside the one it belongs to. You never press this key blind.

This road used to read `conflicts with your branch`, which was untrue — there was no branch
of yours in it — and `[a]` spent a merge round, which merges *branches* and cannot see an
untracked file at all, so it refused a second time in exactly the same words.

## Tell it something instead of answering — s tell it, and why saying looks good does not accept

`s tell it` is the third chip on **every** `your call` card, and it is not a third answer.
It opens the task's own page with the message box pointed at it, so what you type there is
sent to the task as a correction.

**A steer never resolves a task by itself.** The model or a worker reads what you said and
spends a verb, or does not — so "looks good" typed on a card is words the work receives,
never a silent accept. If you mean accept, press `[a]` or say "accept task 7".

The card keeps its chips while a steer is in flight, because nothing about the question has
changed yet.

## Why does it say codeaf is deciding — and how do I take a task back

When `task.settle` is `auto`, or after you press `d you decide` on one card, codeaf is
reading that work and will answer it. **The row says so rather than going quiet**, on the
reason line:

```
? Port the parser
  nobody could check it · codeaf is deciding
  a accept · n not right · s tell it
```

**The answers stay drawn, and answering is how you take it back.** Pressing `[a]` or `[n]`
yourself settles it and ends codeaf's turn at it; a card with a sentence on it and no handle
is the one shape this surface must never draw. Anything codeaf was about to say it may still
say; what it may no longer do is have the last word.

**A task never stays unowned past the end of a turn.** If codeaf's turn ends with a task it
was handed still unanswered, the question comes back to you by itself and the card draws
its chips — you do not have to notice it. And where the model's last message asked you
about that task, the chips are the answer surface for that question: its words above, the
chips under them, one ask rather than two.

**And closing codeaf ends the turn too.** If you quit, crash or come back to the
conversation later, a task codeaf was deciding is yours again the moment the conversation
opens — the card draws its chips rather than the `codeaf is deciding` row, because the turn
it was going to be decided in is gone and nothing is going to finish that thought.

## Can codeaf decide on its own — can the chat decide on its own, stop asking me about tasks that need a look

Yes. The setting is **`task.settle`**, in `/settings` under Session as
`who settles work that needs a look`, and it takes two words:

| Value | What happens when a task lands as `your call` |
| --- | --- |
| `ask` | **the default** — you decide. The card offers the chips above, and codeaf says what it thinks and leaves the choice with you |
| `auto` | codeaf decides. It is told to read the report and the work itself — the transcript, the diff on the branch — and settle the task, and to come back to you only when it genuinely cannot tell |

Which of the two a card follows is fixed when it lands, so changing the row does not reach
back and take the chips off a card that was already asking.

**`d let codeaf decide this one` is on the card and changes no setting.** It is drawn
dimmer than the three columns, because it is not one of the answers: it hands **this one
card** to codeaf and leaves `task.settle` exactly where it was. It used to be spelled
`decide these for me` and it used to flip the setting for good — a persistent preference
disguised as an answer, and the reason a card could end up with no choices on it and no
explanation of why.

Neither value takes anything away. Under `auto` you can still take a task back with `t` or
say "actually that one isn't finished"; under `ask` you can still say "you decide this
one". The row changes **who is asked first**, and nothing else: either way the task is
neither done nor incomplete until somebody answers, its branch is kept, and anything
waiting on it waits.

To undo it, set the row back to `ask` in `/settings`, or say so — "ask me about these
again".

**A run with nobody watching reads as `auto` whatever the row says.** `codeaf --once` and
the other headless doors have no card to press, no `/settings` to open and nobody to read a
landing that says it is waiting on somebody, so a task that needed a decision there would
stop the run until the wall clock ran out — which was measured happening on a ten-hour run.
In those sessions codeaf takes the decision itself, with the same escape to say it cannot
tell. This never applies to a session you are sitting in front of: there your row stands,
and a blank row still means codeaf asks you.

## Why is there no check again — what the engine tries before anything is your call

You are never offered `check again` on a card, and that is deliberate: **the engine has
already done it.** Three things are tried automatically, each exactly once, each written as
one plain line in the task's journal, before anything is put to you:

1. **The check is asked again, on another model.** A checker that never answers, or answers
   with neither word, is asked a second time with the same evidence and not a word about
   what the first one said — on the next model in this install's fallback chain where there
   is one (`--one-model`, and an install with no chain, ask the second on the model it
   already had). Only when that says nothing either does the work land as
   `your call · nobody could check it`.
2. **One round is spent on a conflict.** Your branch is merged into the task's branch,
   inside the task's own working copy, a worker brings the two versions together with the
   brief and both sides in front of it, the check runs again, and the landing is retried.
   Only a round that fails reaches you.
3. **The task is run once more from its branch** for a dropped connection, a provider that
   would not serve the request, and a brief whose world had moved — before the row is
   written as `incomplete`.

So by the time a card is in front of you, the thing a `check again` chip would have done
has already been spent. The verb still exists for the model — it can have a landing checked
again through its `tasks` tool — and you can ask for it in words: "have another look at
task 7". What is gone is the chip, because on a card it read as one of the two answers when
it was neither.

`a resolve it` on a conflict spends **one more** of round 2 on demand. Where there is no
working copy left, or a round is already running, it says so in one line rather than
looking as though it did something.

## Your call on a run I left going with --yolo — a check that could not run, and what taken as it stands means

On a headless run with a budget — `--once --yolo` **and** `--max-hours` or `--max-cost` —
there is nobody to put a card in front of, so a task nobody could check is not put to
anybody. The check is asked twice first: one checker, then a **fresh** one with the same
evidence and not a word about what the last one said — **on another model** where this
install has one to move to. Only when the second try says nothing
either does the work land as it stands, and the landing says so, under the task's own
account of what it did:

```
taken as it stands: one call ran 2m30s without answering and was abandoned · the window closed before a second, and the run is unattended
```

The first half of that line is whatever became of the check — a call that hung and was cut,
a checker that would not start, a reply that said neither way — so it names what happened
rather than claiming nobody could check the work.

The task then reads `finished`, its branch merges like any other, and that sentence is the
whole of what was different about it. You can still say "actually that one isn't finished"
when you come back.

**`--yolo` on its own is not this.** Without a budget nothing carries on by itself and
nothing is decided for you: a task that needs a look waits, exactly as in any other session.
Nor is it a task inside another task in a session you are watching — that one still goes to
the worker that commissioned it, which reads the diff and settles it.

**And a check that hung is not a check that ran.** No single call may spend the whole
checking window: a stream that answers nothing is abandoned about half way and the check is
asked again, on another model where there is one, inside what is left. When the second call answers, the landing carries
`checked on the second try`. Before that, one hung stream could eat all five minutes and the
check was never asked twice at all.

## A task whose work could not be brought home — it stayed where it is, and why I am not asked twice

When a finished task's work cannot be put back where you can see it, nothing merges and
nothing is thrown away: the report names the folder that then holds the only copy, and the
task waits for you. Which failure it was decides what happens when you answer.

- **The tree would not take the work at all** — the folder it ran in is not a repository,
  the disk is full, a file of your own is where a directory has to go. Accepting settles it
  where it stands, once. The report leads
  `taken as it stands, and it could not be brought home, so the work stays where it is — `
  over the sentence naming the folder, and a second answer on the same task is told
  `task 7 is done, and only a task that needs a look is waiting on somebody to decide`.
  Asking again cannot change a disk, and a measured run accepted the same task three times
  and got the same refusal three times.
- **The same file changed on both sides** — a real merge conflict. One round is spent
  bringing the two versions together first, inside the task's own working copy and never in
  your checkout; only when that round cannot do it does the task go back to
  `your call · conflicts with your branch` with the clashing files named, and its chips read
  `a resolve it · n drop it`.

The how-tasks-run page has the sentences each of those lands with, under *My task could not
save what it wrote*.

## How do I approve a task — accept a finished task from the room, the card, or by saying so

A task that landed as `your call` is approved by **accepting** it, and there are three doors
onto the same decision. Use whichever is in front of you:

| Where you are | What to do |
| --- | --- |
| **inside the task's room** (enter on the roster row, or click its landing card) | press `a` over an empty message box — no selection needed, the room is the task. `n` says no to what the row is asking, `s` steers it, `d` hands this one card to codeaf. The same chips are at the foot of the page |
| **at the landing card** in the conversation | walk to the card with `↑`/`↓` so it is selected, then the same keys — or click a chip on its answers row |
| **anywhere**, typing | say it: "accept task 7", "that one isn't finished", "have another look at task 7" |

Accepting lands the task by the same merge-or-keep rule as checked work and unblocks
everything queued behind it.
Whichever door is used first wins; the other two find the question already gone and show
`already answered` rather than raising an error.

## Task says your call but there is no button — where the answers are

If a landed task is asking and you cannot see anything to press, you are on a row that only
reports the tier: a roster row, home's `needs you` strip, or the card's own head. The
answers are in **one** place on screen, and it is the same place wherever you are standing:
the question block above the message box, reading

```
? <the task's name>
  <the reason it is asking>
  a <yes> · n <no> · s tell it
```

It is there in the conversation, in the task's own room, and on the `/tasks` page — the
landing card in the transcript keeps the head and the sentence saying what is being asked,
and the answers are on the block. The letters need an **empty** message box: they are held
to the same rule `x` is, so a letter typed into a sentence stays a letter. If the reason row
also reads `codeaf is deciding`, that is `task.settle = auto` — the answers are still yours
to press, and "ask me about these again" changes the setting for good.

## Stopping an adaptive run

An adaptive run is a run that keeps spawning tasks of its own for as long as its planner
has something left to want, and it has a page rather than a room: the nodes drawn as chips
in layers, with one fuel gauge pinned at the top — `planner: <model> · $0.87 / $100.00`, the
model doing the planning beside what it has spent of what you approved.

**`x` on that page stops the whole run**, over an empty message box, and it asks the same
one card the pointer's `Stop` asks:

```
? Stop this run? In-flight nodes halt; partial results stay.
  [stop it]   [keep going]
```

The cursor opens on `keep going`. There is no bypass.

**What stopping a run does.** It means *stop spending now*:

- **nodes in flight are cut** where they stand, and **their partial output is discarded** —
  a half-answer handed on to the next node as though it were a finding is worse than no
  answer at all. Each of them ends drawn grey with `■` and the word `stopped`.
- **queued nodes are dropped instantly**, and stay on the page rather than vanishing: the
  shape you are looking at is the shape the run crystallized into.
- **nodes that already finished keep everything** — their digests, the planner's notes, the
  whole trace.
- **no write-up is produced.** The closing synthesis is one more model call, and stopping
  is you declining to pay for it.

The header's state word becomes `stopped` and the conversation gets one line saying where
it got to: `stopped — $0.42 spent, 5 of 9 nodes done`.

**The fuel gate's own `stop` is the same stop.** When a run spends its tank it parks and
offers three answers — `add $1`, `finish with what we have`, `stop` — and choosing `stop`
there does exactly what `x` does, leaves the same trace, and says the same sentence. One
stop, one word, wherever you reach it from.

Pressing `x` on a run that has already finished does nothing but say so.

## The sessions place — tree lines, project column, folds, and the time-window keys

The **sessions** place lists conversations and their nested work across projects, under
**running** or **completed**. The whole conversation moves together. Both sections and
nested sibling lists default to newest activity first, and every level starts expanded.

**The connecting lines show ancestry.** A continuing vertical line links siblings;
branches and elbows connect each task to its parent. The collapse/expand arrow sits at
the end of the title in the left/name column, separate from the conversation's Home-style activity bullet.
Click that arrow, or use `←` and `→`, to fold and expand without changing the task.

The columns are **name**, **project**, **state**, and **age**; fold arrows stay beside their titles.
Project has its own column; it is not appended to the conversation title. The project
column appears from 60 list cells, including the current project and `~` where recorded.
Under 90 list cells, state moves to the detail view; age and project remain. Under 60,
tasks use compact cards. Long names and projects truncate to fit.

**The `state` column is never blank on a row of work.** It is the task-states word —
`your call`, `running`, `waiting`, `done`, `incomplete`, `stopped` — and nothing else, except
for three additions that belong in that cell:

- work **running right now** adds where it has got to: `working · 18 of 40`. That figure is
  the one thing on the row that changes while you watch it, and it is the first thing given
  up when the cell runs out of room.
- a **shut fold** adds what it is holding: `done · holds 3 more`.
- a **main chat's** row says its **count** instead: `5 your call`, `9 done`, `2 running` —
  how much work is under it altogether and how urgent the most urgent of it is, which is the
  question a shut fold raises.
- work **another window is running** says `another window` here instead.

**A run's row carries its live step while its worker is on one.** A run's rows are rows of
the store rather than this session's own tree, and while a step is in flight the row spends
one more line under the title — the running glyph `◐`, the shell lead `$` and the command the
step is running — with the task's own figures beneath it: how many steps its worker has taken
and what it has cost, joined ` · ` and each half left out when it is nothing:

```
 ◐ Add rate limiter to /api/upload
   $ git grep -n RateLimit internal/api
   12 steps · $0.11
```

The line is there only while a step is in flight, so a row between steps, one held behind
named work and one that has landed draw their ordinary row and no live line. A run row the
store holds admitted-and-not-started wears `queued` rather than `running`, and one held behind
named work names what holds it after the word: `queued · waits: <the work>`.

**The reason is not on the row.** Why work ended as it did is on the task's record, one
keypress away through `enter`, and in the pane beside the list where the frame is wide enough
for one. On a frame under 110 cells there is no pane, so the row **under the cursor** grows
one dim line of its own: `your call · nobody could check it`, and the first sentence of a
landed task's report after it. One row said whole, rather than twenty rows each missing the
same word. The row's kind is not drawn, and its cost is available in the detail view.

**Everything starts expanded.** Every main chat and every nested family shows its work.
`←` folds the branch under the cursor and `→` opens it again. When you fold one, the
section heading says how many rows are hidden, such as `completed · 4 folded away`.
The list scrolls through every conversation tree selected by the time window.
This includes tasks from the run store. The compact rail inside a conversation instead keeps running work first and groups finished children into a count; that compact view does not change the Sessions page’s folds or chronological order.

Type to filter; every section narrows at once, and a section the query empties is not drawn.
The one printable keys that are not the filter are `1` and `2` over a row the pane is offering
those two answers for, which answer it. `↑` and `↓` move among conversation and task rows and skip the head sentence, the blank lines
and the section words. `enter` on a main chat opens that conversation. On a task it opens its **room** when this conversation
is holding, and otherwise goes **inside** it — the record card. Rows another window is running
take the cursor too, and what `enter` does with one is *Opening a task another window is
running*, below.

**The cursor stays on the row it is on while the list re-files itself.** The page re-groups
every few seconds, and the last active task finishing moves its conversation tree from `running` into `completed` —
which shifts every row below it. The cursor is remembered by the conversation-and-number pair
that identifies the work, not by which line it was on, so a task landing while you are
reading cannot move what `enter` is about to open. If the row you were on leaves the page
entirely, the cursor parks on the nearest row that is still there.

**On a frame 110 columns or wider the body splits** and the right of it is the cursor row's
record — what it did, what it cost, where it left the work and what can be done about it. It is
described in *Preview a task without opening it*, below. The page's own head sentence, with the
time-window control on it, keeps the whole frame above the rule.

`shift+←` and `shift+→` move the time window backward and forward. `shift+↑` zooms from
days to weeks and then months; `shift+↓` zooms back in. The window is re-grouped from the
reading already in hand — nothing goes back to disk for it. On a phone the bottom line
remains the pressable `‹ back` bar. An empty place teaches what tasks are instead of drawing
empty headings, and says no count beside that prose.

## what are the dots next to a task?

The dots show how far the whole task family has moved. `●` is done, `◐` is running
or a share that is partly done, `○` is not started, and `✘` is a share holding a
failure. The running cell is the first cell that is not full, so the moving edge is
always the first unfinished part.

A family with one task has no dots and says its state word instead. From two through
ten tasks, each cell is one task. With eleven or more, ten cells divide the family
into tenths. The widest row shows all ten cells and words such as `6 of 10 · 2
running · $0.41`; the next tier keeps ten cells and shortens the count to `6/10`;
a widened rail uses five cells and `6/10` at the end of the run's row. On the rail at
its ordinary narrow width the dots take **the line under the run's title**, all ten
cells and `6/10`, so the title keeps its own line. A family that is entirely
finished says `done`, never `10 of 10`. A failure is drawn in its cell as `✘` and
said in words too: `●●●●●●●●✘✘  8 of 10 · 2 failed`.

## Why is this task indented under that one? — the plan drawn as a tree

The plan is a **graph, and the list draws it as one**. A task sits under the task
that requested it — the parent the worker wrote to the store — and a deeper task
sits under that, each joined to the row above it by the list's own connector
(`├ `, `└ `). You read the shape of the run down the indentation, not a flat list
of peers.

A task **held behind named work stays under its parent**. Waiting is a second
kind of connection; it never changes the family or indentation. The pending row
still wears `queued · waits: <that task>`, so the words say what holds it while
the tree continues to say who asked for it.

A task's **page** shows everything below it under its steps the same way, each
with its live step while its worker is on one. Opening a row (`enter`) and
leaving a note are unchanged by the tree.

## Why is this group one line? — finished families fold on the rail

The rail shows the run's tree. It puts families with running work first, newest
activity on top, then queued families, then done families folded with their age.
Inside a family it keeps store order, except that running rows float to the top and
its done rows fold into one `✔ N done` line at the bottom.

A family becomes one rail line when every task in it is done or failed. The line
keeps the family title and says how many settled below it — `· 3 done`, or
`· 3 failed` when the family failed. This is a fold, not missing work: select the
line and press `enter` to open the family's page. A family with anything running
or queued stays open on the rail.

## What does queued behind it mean? — open tasks are waiting on this one

`· 2 queued behind it` on a row means two open tasks directly wait for that task
to finish. It counts direct dependants, not every later task in the subtree, and
nothing is printed when the count is zero. Their own rows remain under their
parents and say `queued · waits: <title>`.
## can I ask what a task did without opening its conversation?

Yes: ask the conversation. It reads the task with its `tasks` tool and answers from the
run's own record, never by doing the work again. A task you handed off is read by the
number its card and the rail show, `#2`; a part the run made for itself is read by its
place under that task, `#2.1`, `#2.2`, in an order that does not move. A listing shows each
one's name, title, state and the first line of what came back; reading one task shows what
it was asked, what came back in full, what the run's checks found, and its last steps. A
store's own id is never shown. A finished task is asked about this way and is never redone
or rechecked by hand.

Tasks from earlier sittings and from other windows are still listed after the run's, and a
number the run does not hold is answered the way it always was.

There is no ask box on the run's pane yet. The turn behind it exists in the engine (three
read-only rounds over the run's rows, notes and one named task's last twelve steps, naming
the task and the steps each answer came from) and no screen opens it, so asking the
conversation is the way today.

## Preview a task without opening it — the record beside the list, seeing what a task did, and answering a task from the list with 1 and 2

On a terminal **110 columns or wider** the sessions place splits: the list keeps the left, a dim
rule divides it, and the right is the record of whatever row the cursor is on. Nothing is
opened and nothing is lost — walking down with `↑` and `↓` changes what the pane shows, and the
list stays exactly where it was.

```
 ▾ Clever Bet Prediction Model using Stochastic Processes          9h   │ Upgraded model v2: vector skills, BOCPD
   ? Upgraded model v2: vector skills, BOCPD change-point detec…    1d   │ ? your call · nobody could check it
   ■ Fit and backtest OU skill model on Liverpool's season          1d   │
                                                                        │ 5 files · $0.47 · deepseek-v4-flash · 1d ago
                                                                        │ branch task/upgraded-model-v2 · in a worktree
                                                                        │
                                                                        │ Added vector skill ratings, BOCPD change-point
                                                                        │ detection and a tail-noise model.
                                                                        │
                                                                        │ model/skills.py
                                                                        │ model/bocpd.py
                                                                        │ 4 more
                                                                        │
                                                                        │ 1 accept   2 not right   enter open
```

What the pane says, in this order, with every line that has nothing behind it left out
entirely rather than drawn as a zero or a blank:

- the task's **title**;
- its **state and the reason**, with the same mark the row wears — `your call · nobody could
  check it`, `stopped`, `done`;
- one line of **figures**: how many files it wrote, what it cost, which model ran it, and how
  long ago it landed;
- the **branch** it was left on, and the word for the copy of your repository the work
  happened in — `branch task/pages-one-two · in a worktree`;
- the **first paragraph** of what the task said at the end;
- up to **five file paths**, then `N more` counted against the honest total;
- the **verb line**.

**The report is read when the cursor stops, not while it moves.** Holding `↓` through twenty
rows reads nothing at all; the file is opened once the cursor has been still for a moment, and
what has been read is kept, so walking back up costs nothing. Until it arrives that part of the
pane is simply empty — there is no spinner and no `loading`, and nothing on the page ever waits
for a disk.

**The pane is the record card at a narrower width**, so the two never disagree. `enter` still
opens the whole card, where the report is complete and scrolls; `esc` comes back to the list
with the pane showing the same row.

**On a conversation's own row the pane is the conversation**: its title, `3 pieces of work ·
$9.30`, the first few rows under it in the order the page files them, and `enter open the chat`.

**The list's own `state` column goes while the pane is up**, and that is the table's rule
rather than a special case: the list is drawn in 72 of the frame's 122 cells, which is under
the 90 cells the state column needs, so the name keeps what it can while project and age stay. What the state column was saying is in the pane, said whole and with its
reason.

**Under 110 columns there is no pane and no rule.** The place is the list alone, and the row
under the cursor grows one dim line of its own with `state · reason` on it. Under 60 columns
the rows are two-line cards, as they have always been.

## Answer a task from the list — accept or reject a finished task with 1 and 2 without opening it

Where the row under the cursor is a task of **this conversation's** that is waiting on you, the
pane's last line leads with the two answers:

```
 1 accept   2 not right   enter open
```

The words are the task's own — a landing whose merge was refused says `1 resolve it   2 drop
it` — and they are the same words the card and the block above your message box offer, because
all three read one question. Press `1` or `2` and the answer goes to the same place it would
from the card; the letters `a` and `n` still answer it there, and the digit is simply the name
that fits on one line. Clicking the word does the same thing.

**Where there is no answer to give, no answers are drawn.** A task another conversation ran is
asking its question in *that* window, and this one cannot answer for it — so the verb line is
`enter open` alone, and `1` and `2` are typed into the filter like any other character. That
is also what happens on a frame too narrow for the pane: nothing on screen names the digits, so
nothing takes them.
## sort the tasks list — chronological order, most recent activity first

The Sessions tab defaults to sorting **running** and **completed** by the newest recorded activity
in each conversation, including task starts and completions. A conversation with no tasks
uses its own activity time. Unknown times sort after known times; refreshing the screen
does not replace an unknown timestamp with the current time.

Sorting preserves the tree. Each conversation keeps every task, and sibling tasks also
follow the selected age direction. Click **age ↓** to show oldest first (**age ↑**),
and click again for newest first. The selection stays on the same item. Filtering and
refreshing preserve this choice. Other column labels do not sort; `alt+s` and
`alt+shift+s` remain unbound on Tasks.

## filter the tasks list — type to filter, what it matches, and esc to clear it

**Type, and the list narrows as you type.** The first line of the list is the control row: a
`⌕` mark, then what you have typed, and before you type anything the dim words `type to
filter`. That is where your letters land — there is no message to send from this place, so
every printable key goes to the filter. `backspace` takes one back, `ctrl+u` clears the box,
`ctrl+w` takes a word.

The query is matched against the task's name, the main chat's title, the state word and the
file paths the work touched. Every section narrows at once, and a section the query empties
is not drawn at all. A query that matches nothing keeps the page's own heading and count and
says `nothing matches` under the list — there **is** work here, and your words are hiding it.

**A filter opens every main chat with a match in it**, so a row that matched is never sitting
behind a fold looking as though the query missed it. When you clear the filter your own folds
come back exactly as you left them.

**`esc` clears the filter first and closes the place second**, which is why the foot says
`esc clear the filter` while one is on.

## The foot of the sessions place, and the one verb on its row strip

The last line of the sessions place is assembled from the clauses that are **true of the row
under the cursor**, and never from a fixed sentence. Over a task this window is running it
reads

```
enter open its room · → verbs: stop it · type to filter · alt+. map · tab next place
```

The last two keys are on every place and the router adds them. What comes before them
changes with the cursor:

- `enter open its room` over a task **this conversation is holding** — it has a room.
- `enter go inside it` over work **another conversation ran** — no room exists, so `enter`
  opens the record card instead.
- `enter go to that conversation` over work running in a conversation **this terminal is
  already holding**: the row switches to it and stands in that task's room.
- `enter read it as it runs` over work running in a conversation **the local engine holds**
  but this window is not in — the read-only page described in *Opening a task another window
  is running*.
- `enter about that window` over work this machine cannot reach at all, which opens the card
  naming where it is.
- `→ verbs: stop it` **only while the row has that verb** — see below.
- A row whose work has raised something for you answers on the same line, behind the door:
  the foot reads `enter open its room · hello.txt · waiting in this conversation · alt+y`,
  with the page's own clauses — the fold, the verbs, the filter, the way out — giving way
  first when the width runs short. The row keeps its door, and the question keeps its way
  in, on the one line the foot draws them on.
- `→ what ran under it` or `← fold it back up` over a fold, whichever the fold is not.

The final clauses describe the **page** rather than the row:

- `esc home` returns to Home when the filter is clear.
- `type to filter`, because nothing else on the frame says that a letter goes into the box on
  the control row rather than to the page's own keys. While a filter **is** on, that slot
  says `esc clear the filter` instead — the one fact the box itself cannot show is that esc
  now means the filter and not the page.

**`→` opens the row's verbs, and the sessions place has exactly one: `s stop it`.** It is
offered over a task **this conversation is holding** that is still `queued` or `running` —
the same work the roster's own `x` can end, through the same door in the engine, and it
answers with the engine's own sentence (`stopping task 7 — its branch is kept`). A settled
task has nothing left to stop, work another conversation ran has no live worker here, and a
session whose engine has no cancel door is offered nothing — in every one of those cases the
verb is **absent**, and the foot does not name it.

**It asks no confirmation, and that is deliberate.** The confirmation card guards `x`, which
is one bare keystroke over a list; on the strip the word `stop it` is drawn on screen and
only then does `s` mean anything, which is two deliberate presses with the verb in front of
you. The card is also not available here: it is drawn in the conversation's chrome, and a
question raised over a full-screen place would be one nobody could see.

## Retry an incomplete or errored task — enter retry on its task screen

**`enter retry` re-arms incomplete or errored work on its task card.** It is the first
footer action, before `m puts it in your message`. The task must belong to the current
conversation. Stopped, interrupted, errored and incomplete ordinary tasks, quick tasks,
designs and saved-workflow runs can retry. Quick tasks keep their checklist and receive
the previous result; designs and saved workflows restart their original inputs.
The key also works over an empty message box in that task's conversation page. A pending
retry cannot be submitted twice. Failure leaves the task unchanged and says why on the
page. A read-only view or an older engine without retry support offers no retry key.

**Some rows cannot be restarted from their record.** Older designs and saved-workflow
runs may lack the original inputs; they report `no saved restart instructions` rather
than running a different kind of work. New records retain these inputs across restarts.
Background-job and adaptive-run rows are summaries owned by separate runners, not
restartable task records. Open the owning conversation to retry its tasks; read-only
views cannot restart another owner's work. Successful or still-running tasks do not
offer Retry.

The existing task updates in place in the sidebar, task list, card and conversation
receipt. It keeps its ID, original assignment, branch and journal. Earlier attempts stay
in the journal; the current status and outcome follow the new attempt.

**`continue` also re-arms the same task.** A failed or finished task can be
continued by saying `continue task 7`, or by the `tasks` tool with `id` and `continue`.
That is the same node: same id, same brief, same working copy and journal, the last
report as this round's finding. Starting the same brief again with `/task` or
`propose_task` is new work with a new id, and it is the wrong door when the person
means keep going.

## Correcting a running task from the main conversation — forwarding your words

Say which running task you mean and what should change. The main conversation can use
`tasks` with its `id` and `forward: true` to send the message it is answering verbatim,
as your own direction. The model chooses the recipient; it cannot supply replacement
words. Its reply should say what it forwarded and to which task.

`tasks` with `say` remains a message from the model. It cannot authorize an assignment
revision. Task workers cannot use `forward`, and it cannot address another session.
It cannot be combined with `say`, `continue`, `resolve`, or `stop` — and neither can
`stop` be combined with any of them, since it ends the task the others act on.
A result arriving by itself does not authorize forwarding an old message of yours.

Repeating the same forward to the same task is acknowledged without sending twice.
Typing the same sentence again is a new message. A receipt means the direction was
kept, not that it was read or applied. A correction after publication has started is
kept for a later round; it cannot recall work already coming home.

A delayed forward cannot revise over a correction recorded with a later speaking time.
For main-chat messages this is when the session reads them at a turn boundary, not
the keypress time. This is local ordering, not a guarantee across machines or clock
changes. Corrections do not automatically broadcast to a task's children.

## Does a child treat its parent's brief as something I said?

A worker opening and its finishing instructions are runtime notes, not new messages
from you. When the worker hands a piece onward, these composed instructions are not
quoted as your words, including after reopening the journal. The original person
request remains available separately, together with the child's assigned scope.

## Drafts while reading another conversation’s task

A task page opened from another conversation keeps a separate draft, identified by that
conversation and task. Its words do not replace your conversation draft or a local task
with the same number. The page is a reading view: sending, steering, and stopping belong
to the conversation that owns the work. If its status connection closes, the footer says
`reading` and the page keeps the last known state with an explanation.

A reading view continues checking its owner after the task finishes. If that
conversation opens something else, the page explains that it is showing its last
reading and stops asking. Finished local task pages still stop polling normally.

## Double-clicking a task in the chat sidebar

A sidebar row opens its task. Clicking the selected row again keeps that page, its
draft and reading position open. Press `esc` or click the page's back control to
return to the main conversation. A foreign task does not select a local sidebar
row merely because both tasks have the same number.

## Why another conversation could not open

If the owner refuses the connection or answers for a different conversation, codeaf
opens the task's read-only card with the reason and the owning window's details.
Return to the list to retry, or go to that window. Local design progress never
updates a foreign task page with the same number. The next-running-task arrow
opens a local task even when its number matches the foreign page you were reading.

## Returning to a task — keep my scroll position and expanded details

Leaving a task and reopening it in this window restores the place you were reading,
expanded work, tool details, and your choice to follow new output. Each task keeps its
own reading state, scoped to its owning conversation and host. New output while you
were away does not pull a scrolled-back reader to the bottom. If you deliberately
scroll to the live edge, reopening follows the newest output.

This keeps the 64 most recently visited task transcripts and up to 240 block display
choices per task in memory. It does not persist after quitting and does not preserve
an adaptive run's graph view. Reopening still reads the retained journal tail; if the
old anchor has fallen outside that tail, the view starts at its oldest retained row.
A loading or failed hosted read does not overwrite the saved position.

## Breadcrumbs — which chat am I in, parent tasks, and switching chats by clicking

The main chat's top bar names the conversation. `alt+k` opens the chats card from
there without changing chats until you choose — the top bar has no `▾` control of its
own, and the tab row's `Chats ▾` is deleted. Inside a task it becomes a trail:
`Shipping the parser ▸ Fix validation ▸ Add boundary checks`. Click the conversation
name to return to the main chat, or an ancestor to open that task. The current task is
inert. Narrow frames fold ancestors into `…`; clicking it opens the nearest ancestor
it hides. A guest task names its owning conversation and ancestry, but those crumbs
are inert because this window does not own that graph.

Model and conversation statistics remain in the status row. Deep trails fit at most
eight explicit ancestor levels; earlier ancestors are represented by a fold. The bar
stays pinned while you scroll. Task drafts, scroll anchors and expanded sections stay
with their task when navigating within this process, subject to the reading cache limits.

## Switching chats through the engine — duplicate names, conversations that keep running, and saved history

The ordinary engine-backed chat, `--host` and `--at` all hold **one connection per
conversation**. Opening another conversation dials another connection and closes nothing:
the chat you came from keeps writing its reply, keeps running its tasks, and its output is
all there when you go back to it. The same is true of `codeaf chat --no-host`, which runs
its conversations inside this process instead. The switcher lists them with `working`,
`needs you` or nothing against each, from the same reading that draws the mark on that
conversation's tab, and must not show a new chat under a previous chat's name.

The entry line `closed · <previous chat> — a connection holds one conversation at a time`
belongs to a door that has no way to dial a second connection, and is not something any
shipped door says today. Merely browsing the chats card (`alt+k`) does not select or
close anything.

**What ends a conversation is explicit.** `stop work` on the card that appears when you
close a working tab ends the turn and the running nodes in that one conversation and
nothing else. Over an engine-backed door, quitting codeaf detaches instead: the work
outlives the window and is still going when you come back to it. An in-process
(`--no-host`) conversation does not keep working after its terminal exits.

**Stop work ends this conversation’s work.** The engine first blocks new work,
then cancels the reply, pending questions, queued/running task nodes, adaptive
runs and background jobs, including a chat held in the background. Other
conversations are untouched. The card says `the reply, tasks and jobs stop;
nothing is deleted` when it knows about tasks or jobs. Cancellation news cannot
wake another reply. Only a fresh user message resumes work once cancellation
finishes; reopening the tab does not. Stopping does not delete standing orders
or change what quitting the whole app means.

## Accepting a saved task after its Git registration was released

If codeaf released the task's Git registration while retaining its files, a later accept
restores that registration before bringing the work home. This is not a folder that was
never a repository. The actual branch name is retained even if the task renamed it.

If the registration cannot be restored, the task stays `your call` and names the
saved folder and the cause. Its files remain in place, and acceptance can be retried
after repair. It does not claim a missing branch holds the work. Protected branches such
as main remain untouched; their task branch is kept instead.


## Task header model and price readability

Task header pricing uses regular body ink so spending stays readable. The model
uses a quieter secondary text color; separators and ancillary details remain dim.
The same hierarchy applies when the header fits its compact layout.

## The task finished, but its branch was kept — is my work done?

A completed task can still have changes only on its retained branch. When an
unattended request needs those changes in your workspace, the conversation keeps
that delivery unfinished; a worker saying done cannot replace the missing
integration. A child merged into its parent task is not yet a change in your
checkout. Protected branches still do not receive automatic task merges.

The original request's done-condition also records whether you asked for changes
in the workspace, a retained branch, or a report. An explicitly requested branch
can be the finished result without a merge. A report must actually contain the
answer. This destination is recorded before work starts, and worker messages
cannot change it. Missing or invalid destination information does not excuse
changed files left on another branch. If the branch's changes later reach the
workspace explicitly, the next completion check reads that content again.
That check has to find at least one listed changed path on the retained branch
or among the workspace's tracked files. When none of those paths exists on
either side, nothing was compared; silence is not proof of delivery, so the task
stays on its retained branch and the conversation continues to say its changes
have not reached the requested workspace.

## Back to main from a nested task — return to the conversation

While a task page is open and the task column is visible, **Back to main** is
pinned above the task list. Click anywhere on that row after the column's resize
handle to return directly to the main conversation, from any task depth. The row
highlights under the pointer and stays above the list when you scroll it.
Returning does not stop the work. Your conversation draft and reading position
are restored. The row disappears when you are already in the main conversation.
Breadcrumbs still let you choose a particular ancestor; Escape can step back
through nested task views. If the column is hidden, the header's return control
and breadcrumbs remain available.

## will the chat do it itself or start a task?

One read, one edit or one command the chat does itself. Anything with parts goes
out as tasks. With `CODEAF_TASK_BELT=bash` set there is **one way** the chat puts
work out, a task:

- **hand off:** the chat proposes a task; approving the card, or letting its
  countdown run out, starts it as a run in the conversation's plan.
- **in parallel:** the chat proposes several tasks in one message. The first opens
  the run and every one after it joins the same run as another task of it, so
  parts that do not need each other run side by side. A part that must follow
  another names it in `depends_on` and waits for it.
- **add to:** a `/task` you type while a run is live joins that run too.
- **ask about:** the chat reads the run's rows and the task's own steps and
  answers from them. It never redoes the work.

## is there a quick task with the bash belt on? can the chat still start quick tasks?

**No. With `CODEAF_TASK_BELT=bash` set the conversation has no quick task.** The
chat's only verb for putting work out is a task, and every task it starts is part
of the conversation's plan, where the tree shows it and a check reads it. A quick
task ran outside the plan, in the folder you stand in, with no check, so it was
left off rather than kept as a second road. Asked to parallelize, the chat proposes
several tasks at once. Small work it simply does itself.

With the variable unset nothing changes: quick tasks work as *What a quick task is*
describes.

## what are the what, since, now and next lines?

These four lines are a short account of a run. **what** is your goal in your
own words. **since** says what changed since you last looked, and is left out
when nothing changed. **now** says what is happening now, or how the run ended.
**next** says what comes after and whether anything needs you.

The worker model writes these lines. It refreshes them only when the shape of
the run has moved and somebody is looking, rather than every time you look, and
once when the run lands. The **now** sentence also appears under the run's dot
row in the rail, dim and two lines at most. Without a model key the lines are
absent; the task facts remain available on their own.
