# Work that runs on its own — tasks, and finding out what one actually did

## What a task is

A task is one self-contained piece of work handed off to run on its own while the
conversation carries on. It works in a copy of its own — a working copy of your repository, or
a copy of the folder when the work is about a folder — and reports back when it lands. It
never inherits the conversation: what it reads is one written brief — your own message, word
for word, then the work, what to produce and what done means, and a pointer at the
journal path and line of your original turn so it can read those words in full when the
restatement was cut. How that is assembled is on the *how tasks run* page, under *What the
task actually reads* and *Can the task see the original request*.

You can ask for the work in words, and the model grooms it and calls its `propose_task`
tool. You then get a card asking whether the work should go. The
model's window onto work that is running or already landed is its `tasks` tool; that is
also the door it uses to steer a task or to settle one, when you say so in conversation.

You can also start one directly with `/task <brief>`. **You are never asked a question by
that form.** It shows `sizing it up…` while a small judge reads your words for width, and
then one worker starts either way. If the judge found more than one job in there, one dim
line goes into the transcript —

```
the work looks wide · one worker starts, and it can split as it goes
```

— and that worker is allowed to hand the parts out later, once it has opened the material
and seen how many there really are. That is *When a task turns out to be too wide for one
worker*, below. A no, a timeout or an unreadable answer starts the same one worker in
silence, with no line about width, after `sizing it up…` disappears.

`/task solo <brief>` skips the judge and starts one worker.

**There is no `/task adaptive` any more.** `/task` cannot open an adaptive run, and the
word picks nothing. Type it and your brief is kept exactly as you typed it — the word is
left where you put it rather than cut out of your sentence — the work starts as one
ordinary worker, and one dim line says so:

```
/task adaptive retired · the word stays in your brief, and the work starts as one worker that can split as it goes
```

**Every `/task` has its brief shaped before the work starts.** Your words are kept word for
word and a fuller brief is written around them — the constraints this kind of work needs,
what was decided on your behalf, and what done means. It is the next section.

Every task carries a title, a short summary, the brief, and a done-condition — the command
that must pass, the behaviour that must hold, the output that must appear. The original brief and
done-condition stay on the record. A correction from you can change the effective
assignment through `revise_assignment`; the task keeps both your words and its revision.

You can keep working while a task runs. aforge tells you not to wait for it: its report
arrives in the conversation when it lands.

**One other thing on the roster is a task, and it is not work in a copy of its own.** A sub-harness
being designed runs as a task too — same row, same room, same `x` — with its own phases
(`designing`, then `awaiting your look`) in place of the states below, and no branch, no
changed files and no merge, because it writes none. It is admitted without a countdown,
because the question about a design is the card at the end of it. The page on saved shapes
of work has it in full.

A task can also break its own brief into smaller tasks when it finds independent parts in
it, and those are drawn as a family under it — see *When a task splits its own work*.

## Why my task's brief is longer than what I typed — the brief is shaped

A task you start with `/task` does not go out as the sentence you typed. Between the
command and the work, one model call reads your words and writes the brief the worker is
actually given: your request quoted word for word, then the things a worker alone with the
job needs settled — what kind of work this is, who the output is for and what makes it good
to them, the ways this particular kind of work goes wrong and the conditions that forbid
them, anything ambiguous decided one way with the assumption stated. It also writes a
separate done-condition that somebody other than the worker could check.

So the brief in the task's room really is longer than what you typed, and **the room is
showing you the truth** — that is the brief the worker read. Nothing shorter was sent and
nothing was kept back.

`shaping the brief…` is the line on screen while that call runs, and it is **alive**: it
carries the same spinning braille mark and the same climbing clock a running tool call and
a running compaction carry, so it reads as `⠙ shaping the brief… · 6s`. The clock is
dropped under a second. The line disappears the moment the task starts. It waits up to 25
seconds.

A still line here would mean something is wrong. If the mark is not turning, aforge is not
waiting on the shaper — look for the task's own row on the roster instead.

**You can watch the brief being written.** One dim row under the phase row carries the
newest words as they arrive — the model's own reasoning in italics while it is still
thinking, then your brief upright once it starts writing one — and `→` with an empty box
(or a click on that row) opens it into the last six lines. It is a preview of the wait and nothing is kept from it — the
whole block disappears when the task starts. The commands page has it in full, under *Can I
see the brief while it is being written*.

**If shaping cannot run, your words go as-is.** No model resolved for it, a timeout, an
answer that was not readable — the task starts with exactly your sentence and the plain
done-condition `Complete the brief and report the result and checks run.`, which is what
`/task` did before shaping existed. It is never a reason for your task to be refused, held
up, or lost.

The call is billed the way aforge's other calls-you-did-not-type are: to the session, not to
a turn. It runs on the `shaper` role, which follows the careful-work model.

## Why my task says brief kept as you wrote it — the line under a started task, my brief was not shaped

`brief kept as you wrote it` is one dim line aforge prints under `single task 12 started ·
…` when the shaping call was made and did not come back. It says exactly what it says: the
worker was handed the sentence you typed, word for word, with the plain done-condition
`Complete the brief and report the result and checks run.` and no shaped document around it.

It is not an error, and no part of your request was dropped. The task is running, it has
its name, and it sits on the roster like every other one. What it does not have is the
longer document described under *Why my task's brief is longer than what I typed* — so the
room shows your own sentence, and a worker alone with a short brief settles fewer of the
things a shaped brief would have settled in advance. If you were relying on that pass to
spell out the format or the done-condition, say it yourself and start the work again.

You see the line only when a shaper genuinely ran and was cut — the 25-second wait ran out,
or the call failed. The other ways shaping does not happen stay silent, because in those
no call was made that could be cut: a `shaper` role with no model resolved for it, and an
answer that came back whole but could not be parsed, both admit the task on your words and
say no such line. **An ordinary start is silent here**, which is how you can trust the line
when it does appear.

## Does aforge change my task, or rewrite what I asked for?

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
column of them would be unreadable. So the same call that shapes the brief also names the
work: it has just read the job closely enough to brief a worker about it, and it answers
with a short lowercase name for the thing that will exist when the job is done —
`frieren pdf summary`, `nil-map crash fix`. That part costs nothing extra: it is one more
field in an answer aforge was already paying for and already waiting on.

**Where nothing named it, a small call does.** Some work reaches the roster with no name at
all — only the sentence it was started from: a `/task` whose shaping could not run, work
that started on its own after a words-only turn, an adaptive run's own row. That title is
handed to the cheap `taskname` role, which reads the work and answers with two or three lowercase words.

**Work a model already named is left alone.** A task the conversation proposed with
`propose_task` carries the name the model wrote as an argument to that tool; a `/task` whose
shaping ran carries the shaper's. Neither is renamed — a second call to disagree with a name
aforge itself just wrote would be a bill for nothing. A title that is already two or three
words with no file path in it is left alone for the same reason.

**The work does not wait for the name.** The task is admitted, checkpointed and started
before that call is made. Until the name lands — usually a few seconds — the roster, the
home card, the tasks list and the task's card all show the fallback they showed before: your
own words, cut to the first eight of the first line. When the name arrives the row simply
changes to it. If it never arrives — no model on the class, a timeout, an answer that was
itself a path — the fallback stays, nothing is reported, and nothing about the work is
affected.

**Work aforge starts on its own is usually named before you hear about it.** When a turn is
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

- **sized** — the default. The sizing call reads your brief, one worker starts either way,
  and a brief with independent parts in it starts a worker that is allowed to split itself
  once it has opened the material.
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

## The card that asks whether to run the work

While the model is still writing the proposal, a grey block opens in the transcript and
grows: a still `◌`, the title (or just the word `task` until the title arrives), and one
row such as `⠙ forming… · 6s`. The mark spins and the clock climbs on the same grid as
the `/task` block and a running tool row. Under a second the clock is not shown. In the
plain-text tier the mark is a still `*` and only the clock climbs. It is not a question
yet — there are no options. If the turn ends before the proposal finishes arriving, the
block settles as `cancelled · the proposal never arrived`. If the call is refused before
there is a proposal to ask about, it settles as `not started · the call was refused`.

When the proposal is complete, that same block turns into the question. The card shows:

- a head with the task's own identity mark and a two-or-three-word name;
- one dim sentence under it — the first sentence of the summary, capped at 90 cells, and
  left out entirely when it would only repeat the name;
- a row of model chips, but only when more than one model matched what was asked for;
- the three options `yes`, `redirect`, `no`;
- the countdown meter;
- a dim meta line reading `model <full id> · ctrl+e for the brief`. The model id leads
  because it is the one fact nothing else on screen will say again; on a narrow frame the
  hint is dropped and the model kept.

The card is **not modal**. Unlike the permission question, it leaves the input box live —
the box becomes the redirect lane, with the placeholder
`redirect this task… (enter sends it, esc declines)`.

Only one proposal is a live question at a time. If a second one arrives while the first is
unanswered, the older card settles as `expired · the turn ended`, because a question that
can no longer be answered must stop looking like one.

## The forming card is not moving — proposal card frozen

While a proposal is arriving, the card's middle row reads like
`⠙ forming… · 6s`: the braille mark turns and the elapsed clock climbs. The `◌` in the
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

## Why a task started on its own — aforge started work I did not ask for, this looked like work, so task N started

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

**Stopping one you did not want** is the ordinary stop: `x` on its row over an empty message
box, or `Stop` on the pointer. Nothing about it is special from the moment it exists.

**There is no setting that turns this off.** No `/settings` row switches it, and nothing you
type disarms it for the session. What bounds it is the list above, and the stop.

Nodes cut by an adaptive run also appear as rows under the run's own row — see *Adaptive
runs*, which explains what those rows can and cannot do.

## A message that reads like work makes aforge look sooner — nothing else happens

**The judge that starts work on its own looks at two moments.** One is after a turn that
answered in words alone, above. The other starts **the moment you press enter**: your
message itself is read, with no reply to read instead, because there is not one yet.

**That read happens beside your answer, not in front of it.** Nothing waits for it. The
reply starts arriving exactly as fast as it always did, and the question is being answered
somewhere else while you watch it. So a no costs you nothing at all — which is most
messages, and it is why the read can afford to take its time.

**If both models say yes, you see nothing.** No line, no task, no interruption. The one
thing that changes is *when aforge looks at the work*: instead of waiting until the answer
has run ten rounds of tool calls, it looks at the very next break between rounds — and then
at the ordinary points after that. What that look is, and what it can do, is *An answer that
runs long is read and can be handed over* below.

**It used to hand the reply over on the spot, and it does not any more.** That was measured
against real transcripts and it took work out of the conversation that the conversation
would have finished faster: a message that *sounds* like four jobs is not the same fact as
four jobs, and reading the request cannot tell them apart. Reading the work can. So this
moment now only decides **how soon to look**, and looking at the work decides everything
else.

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

## Handing work over in the middle of an answer — this one wants more hands

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

**The card still appears, and the countdown still runs.** This is not the same road as a
task that started on its own (above): this work was groomed by the model, so it is offered
the way every other proposal is offered — `yes`, `redirect`, `no`, and a countdown whose
silence starts it. You are told, and it opens; what the card gives you on top of that is
the window to redirect it before it spends anything.

## An answer that runs long is read and moved — a reply that stops halfway to become a task, my answer was moved, this has parts, this is running long, carrying the ask only, no second model is set

**When one answer keeps going, aforge prices it.** A long answer costs a little more with
every round of tool calls and never stops costing; handing the same work to a task costs one
fixed price — a clean copy of your folder opened and closed, a brief written, somebody
independent reading the result — and after that the work runs watched. So the count of
finished tool rounds is compared against that fixed price, and at three points along the way
aforge stops and **looks at what is left**. **Nothing about what you asked for is read to
decide when to look.** It is the cost and only the cost. (If the message you typed already
read like work, the first of those points comes at the very next break instead — see the
section above.)

**At each point, a second model reads the answer so far.** Not the model writing your reply
— a different one, asked for one line: a sketch of what is left, as parts and arrows.
`A | B | C` means three pieces that do not wait on each other. `A > B > C` means one job in
three steps. Then one sentence saying what the letters are. If nothing is left, that line is
`(done)`, and if the only thing left is waiting on work already handed out that line is
`(waiting)` — *Watching the pieces you handed out does not move your answer*, below.
**The model answering you is never asked and never sees the question**, which is
the whole point: a model in the middle of tool calls answers a question like that with
another tool call about half the time, so it is asked of somebody who is not busy.

**That second reader is shown a short account of the work, not the whole conversation** —
your message word for word, one line per tool call naming the tool and what it touched, **the
end of what came back from the most recent of those calls**, what has been written or
changed, and the last thing the answer said. Reading the whole conversation instead was
measured costing more than the work it was judging, so what is carried is bounded: each
result is cut to its last 400 bytes, newest first, and the account says how many older ones
it left out. The tail rather than the top, because what a command concluded — `Passed: 0`,
`97 errors`, `no such file` — is in its last lines. Results used to be left out entirely, and
that made the one reader deciding whether to hand your work over the only participant who
could not see the evidence.

**If the sketch has independent parts in it, the reply is handed over.** It stops halfway,
your answer is moved to one task, and two dim lines go into the transcript:

```
this has parts · handing it to a task that can take them side by side
this looked like work, so task 4 started: finish the four pieces
```

The first line is the reason; the second is the ordinary line every task started this way
carries, with its own number and name. Both are in the transcript, so the next thing you say
is not answered on top of a message nobody replied to.

**If the sketch says one job, nothing happens at all.** No line, no note, nothing added to
your reply, and the model writing it is not told it was looked at. The answer carries on and
the next point is twice as far along.

**And if nobody can be reached, nothing happens either.** No second model configured, a
reader that faults, a reader that takes too long: each of those is a look that produced
nothing, and a look that produced nothing is the answer carrying on. The third point below
is what makes that safe.

**The third point is not a question.** Past it aforge stops looking. The answer ends where it
is, what is left of the work moves onto one task whatever the last sketch said — unless the
answer itself says nothing is left, which is the one thing that stops it (below) — and two
dim lines go into the transcript:

```
this is running long · moving it to a task that is watched and can split
this looked like work, so task 4 started: finish the four pieces
```

## It made a task out of work that was already done · why did it hand over when everything was written · the task redid what the answer had already written · it started again from my first message

**When aforge stops to look at a long answer, it asks the model writing that answer whether
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
rounds of real tool work, aforge looks again, does not believe it a second time, and the work
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

**What this does not cover: a sketch naming work you have already done.** Where the second
reader draws independent parts still to do, the work moves — even if those parts landed in
the seconds after that reader was shown its account of them. The reading is one line drawn
from a snapshot. What was closed is the opposite mistake: silence, a fault, or a shape with
no parts in it are no longer read as a reader saying work remains.

## Can I give a task a short name?

**What the task is given.** Your own message rides it **word for word** — that is true of
every task on this page and it is never rewritten. On top of it comes the brief: an
instruction for whoever picks the work up, saying what is left, what was already found out
that they would otherwise have to find again, what was ruled out, and how anybody could tell
when it is done. Where the work was handed over because it had parts, the sketch and its
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
answer that is grinding, and one that is about to stop is not. This holds at all three
points.

**One reader saying so is not enough**, and that is deliberate: a model in the middle of a
long answer saying "everything is done" is that model marking its own work at the moment it
has a reason to. So if the second reader still sees work left, the move happens anyway — on
your own message, since a reply that answered "nothing left" wrote no brief to hand anybody.

**When the second reader names parts, the task starts already divided.** The sketch is not
only a paragraph at the top of the brief — it is put to the task's own splitting road before
the worker is asked anything, so each part named in the sketch becomes a worker of its own
with its own copy of your folder, and the task they came out of stays open to gather their
reports into one answer. Nothing about that road is skipped: the parts are read together by a
second model that can sharpen their instructions, merge two that overlap, or say this is one
job after all — and if it says that, or if there is no free lane to run them in, the task
simply runs as **one worker**, which is what it would have done anyway. Where the sketch drew
a final step behind the parts — `(A | B | C) > D` — the parts are handed out and `D` stays
with the task itself, to do once their reports are in.

**Whether the sketch is read as parts.** What counts is what could be started **now**.
`A | B | C` is three. `A > B > C` is one job in three steps. `A > (B | C)` is one job too —
the fork is behind a step nobody has taken yet. `(A | B | C) > D` is three, with a fourth
step waiting on all of them. `A > B | C > D` is two chains that wait on nothing but
themselves.

**At the third point the task is allowed to split but nothing is handed out.** There the
answer outran one pair of hands by measurement and no parts were drawn, so the worker is
merely allowed to hand parts out once it has opened the material. Whether it does is its own
decision, and it still has to justify them inside the task; the roster says so if it happens —
see *When a task turns out to be too wide for one worker*.

**What it costs.** At most three calls to that second model, and only on an answer that has
already spent ten rounds of tool calls, which most answers never do. Handing over adds two
more: the draft, and the model that writes the brief out of it. One further call goes at the
**end** of any answer that touched a tool at all, asking whether your question is finished —
see the section below. An answer that called no tools costs none of this. The read at the
front of your turn is one cheap call and now starts nothing by itself.

**This applies to replies aforge started by itself, too.** When a task lands, the chat
answers it without you typing anything (see *Why did the chat reply on its own* in *how tasks
run*). That reply is priced exactly like one you asked for: same three points, same ceiling,
same handover. It used to be exempt, on the grounds that a reply about a task already had a
budget somewhere — it does not, and a measured run had one such reply make 127 tool calls
over 46 minutes with nobody watching, and then the session sat idle for seven and a half
hours. A line aforge writes to itself that nobody owes an answer for is still left alone.

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

**And when only half of what is left is a wait, the other half still moves.** If a reply
crosses one of the lines above while pieces you handed out are still running, what is left is
divided first. The self-contained work becomes the task; everything about the pieces already
out — waiting for them, integrating their branches, reviewing them, opening the pull request
over them — stays with this conversation, and the line announcing the move ends
` · the rest stays here for when the pieces already out land`.

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

## A reply that starts changing files becomes a task — why did my edit become a task, it started a task instead of just editing, how many files can a reply change, small edits inline

**Reading is free. Writing is not.** The three points above count tool ROUNDS, which is the
right unit for a reply that is looking things up and the wrong one for a reply that is
changing your files: forty rounds of reading cost you a wait, and forty rounds of editing are
unreviewed changes in the folder you are sitting in. So there is a second, much shorter
count, and it counts only the calls that CHANGE something under the folder this conversation
is open on.

**The allowance is two files, or five write calls, whichever comes first.** A reply may make
a small, obvious edit inline — fix the typo, change the one line, write the note beside it —
and that is the whole point of leaving one at all. The write that would cross the allowance
is not made in the reply. The answer ends where it is, what is left moves onto one task, and
the same two dim lines go into the transcript as at the third point above:

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
outside this folder — a scratch file in `/tmp` is not your work. A hand does not bypass this
count; see *Do hands get around the file limit* below.

**It can still decide not to move.** The move goes through the same road as the third point,
which means it can be dropped when the model writing your answer says nothing is left AND the
second reader agrees — which is exactly the reply that made its one edit and was finishing.
And it happens **once** in a reply: past it, the three points above are the governor again.

**Why it is there.** A message reading "implement this issue" was answered as an ordinary
reply for seven minutes and forty-six seconds — forty-eight tool calls, `sed -i` edits in
somebody's live checkout — before the round ceiling finally moved it. Nothing in between was
watching what those rounds did to the disk.

**A commit, an undo, a one-line edit or a single read is never moved**, whatever the write
count. Those stay in the conversation — see *A commit or an undo is never a task*. Neither is
a reply that is delivering a task's own finished result — see *Finishing a task's work stays
in this conversation*.

## Finishing a task's work stays in this conversation — it started a second task while integrating, my cherry-pick became a task, the commit after the task finished never happened, why did merging the branch start more work

**When a task lands, its report starts a reply here, and that reply is usually where the rest
of what you asked for happens**: cherry-picking the branch across, staging the files, the
commit, the pull request. That is many files changed under this folder, so the write count
above would move it. **It does not.** A reply delivering the result of a task this
conversation started finishes here, however many files it touches.

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

## An answer that stops before your question is finished is carried on — my reply stopped halfway, it said it would do the rest and then stopped, aforge kept going without me

**A reply ends when the model stops calling tools, and that happens for two different
reasons.** One is that the work is done. The other is that it reached a comfortable place to
stop — "I've finished the parser, next I'll wire the handlers" ends a reply exactly as firmly
as a finished job does. Until this, nothing checked which of the two it was, and a measured
ten-hour request ended with hours of it never touched.

**So at the end of a reply that has already run long enough to be looked at once — the first
of the three points above — the same second reader is asked one question**: is what you asked
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
finishes with a question mark, it is waiting on you, and carrying it on would be aforge
answering a question that was addressed to you. That is the whole of the test — the mark
itself, so it works whatever language you are talking in.

**A short reply that only LOOKED at things is not read at all.** If the reply ended before it
reached the first of those three points — a couple of reads and an answer, or no tools at all
— it is never read for what remains. There was not enough work in it to leave half done, and
reading every small reply cost a thinking-tier call on every message you sent: measured, that
was a third to a half of a small question's whole bill, and it almost never found anything
left to do.

**That short-reply exemption is only for a reply YOU typed.** A reply that started on its own —
the turn a finished task's landing begins, the one *Why did the chat reply on its own* in *how
tasks run* describes — is read for what remains **however short it was**. You are sitting in
front of a reply you typed and can carry a small one on yourself; nobody is sitting in front of
a woken one, so the cheapness that leaves your reply alone is exactly the reason not to leave
that one alone. Measured, the best clean run of a ten-hour request had a task come home
unfinished, the landing woke a reply, it read for six steps — under that first point — and
stopped on "let me diagnose the failures systematically", and the run then sat idle for seven
and a half of its ten hours. A woken reply that ends by **asking you** something is still left
alone, and one that **broke** is still not read — those two endings hold for every reply.

**And a woken reply is read against the request its result belongs to, not against the last
thing you typed.** A reply that a landing started owes THAT work's request: the words you used
when you asked for it, or — if you redirected the task in its room while it ran — the words you
redirected it with, together with what the deliverable and the done-condition became. What you
happened to ask about in between is not part of it, and neither is a question that was already
answered. Measured: a build was delegated, a checksum question was asked and answered while it
ran, and when the build landed the reader was handed the checksum question, said it had not
been answered, and the reply repeated an answer already on the screen.

**Several things landing at once keep several requests, in the order they arrived, and each one
says where it came from.** A batch of reports is read against each of the targets behind it,
oldest first, and a reply you typed into while work was out owes both: your words and whatever
landed in it. Which is which matters — **your latest words are the authority**, and a task's
line is only what THAT work was for, so a slow task landing after you have said something else
does not overrule what you said.

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
itself, and it is then priced like any other short reply. aforge does not try to tell a build
from a test from a read; it only asks whether the reply stopped on the change or looked at it.

**What bounds it is the same meter as everything else on this page, and one count of its own.**
Carrying on counts as a round, so it climbs the same three points, and a carried-on reply that
reaches the third one is handed to a task in the ordinary way. On top of that, one question is
carried on at most three times — see *Waiting on something, and the limit on carrying on* below.

**A reply that BROKE is never carried on.** Carrying on is for a reply that stopped early,
and a reply that ended on a **failed request** did not stop early — it broke. A provider
that says it stopped on an error, and a reply that comes back completely empty, both count:
neither is read for what remains, because the failure is what remains and the retry ladder
already owns it. A measured run read a broken reply three times in fifteen seconds, paid a
thinking-tier call each time, and re-opened a reply that could not move. See *Models,
crews and what things cost* for what the error line says now.

**With no second model set, nobody is asked at all.** The reader is a crew job, so an install
with no thinking-tier model configured has none — and rather than call, fail in two
milliseconds and write a failed reading into the session file on every round, aforge does not
ask, and notes the absence once. A reader that faults or takes too long is different: the call
was made and it came back with nothing, and the reply then ends as it would have ended before
any of this existed. In an unattended run with a budget the decision carries on without a
reader either way, on what came home and what the checks said — see *Leaving it running on its
own* in *starting aforge*.

## Waiting on something, and the limit on carrying on — it kept polling while it waited, why does it say carry on, why does it say "carried on 3 times", it turned my wait into a task

**A reply that ends while something IT started is still running is never carried on.** A
background command, a watch, a video or music render, a forked hand — while any of those is
still going, the reply is waiting on it exactly the way a reply that ends on a question is
waiting on you, and pushing it on would only make it poll.

**The ending comes back and starts a new reply by itself.** A background command exiting, a
render landing, a hand coming home, **a watch firing**: each of those wakes aforge and you get
the sentence about it without typing anything. So you can start something, close the laptop
lid on the conversation, and come back to the answer rather than to a card and silence.
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
stops there and is not read: the outcome is the task's to deliver, its landing wakes a reply
here on its own, and *that* reply is read for what remains with the report in front of it. It
is narrow on purpose — a task started for an **earlier** message excuses nothing, so a reply
that hands nothing over is read exactly as it was before; **anything you say after the
handoff**, including a correction typed into the running reply, puts the reading back; and a
task that has already **failed or finished** is news to answer rather than work to wait for.

**What that says is who owes the outcome, not that it is finished.** Nothing is marked done and
no done-condition is answered. A reply that hands one part of your message over and quietly
drops another ends here too, and what catches that is the reading the landing brings — deferred,
not skipped. The measured failure it fixes: "hand this work to a task, run the build and tell me
the marker, keep the conversation free while it runs" was done exactly as asked, read as
unfinished because the marker was not known yet, and carried on into polling the task it had
just started and a watch over its own work.

**And one question is carried on at most three times.** A reader that answers "still not
finished" about the same stopped reply three times running has stopped telling aforge anything
new. The fourth time it says so, the reply ends instead, and one dim line goes on the screen —
with what was actually read in the middle of it:

```
carried on 3 times · the last reading showed: the checks have not landed · stopping here rather than carrying on again
```

**The line quotes a reading and never asserts a conclusion.** What sits after `the last
reading showed:` is what the reader said, or — in an unattended run — the list of things that
came home unfinished and the checks that did not pass. The line used to say "and it is still
not finished", which was a claim about your work that nothing had taken a reading of. There is
no number to raise and no setting that turns it off.

**Three carry-ons can never reach the running-long point by themselves.** That point stands at
forty rounds and carrying on can add three, so a reply that gets handed to a task got there on
rounds of its own work, which is exactly the reply that point was written for.

## Every key the proposal card takes

| key | when | what it does |
| --- | --- | --- |
| `enter` | always | submits a typed answer, or answers the focused option when the box is empty |
| `esc` | always | outright **no** — declines |
| `ctrl+e` | box empty | opens or closes the brief |
| `←` `→` | box empty, picker closed | move the focus between the three options |
| `1`–`4` | same | pick that model from the models row |

The card opens with `yes` focused, because that is what the block is proposing and what the
clock will do. `←`/`→` clamp at the ends and never wrap. You can also click any chip.

The digits are given straight back the moment there is a sentence in the box, or the moment
the redirect lane has been asked for. Every bare letter is ordinary answer text: type the
whole answer, then press `enter`. This is why `no`, `run tests first`, `yes`, and "yes, but
keep the tests" can all begin in an empty proposal box without losing or acting on their
first letter. `←`/`→` still work in the redirect lane, because there is no caret to move in
an empty box.

While the card is up, the legend hint reads `enter answer · esc no`. A question the
session is blocked on outranks the roster, any open room, every overlay and the draft.

Expanding the brief: `ctrl+e` with an empty box, or `ctrl+o` on a card you selected with
`↑`/`↓`. It shows the whole summary, then the whole brief, then `done when: <acceptance>`
on its own labelled line. Clicking the card body does not open the brief — it opens the
task's room.

## The countdown on the proposal card

The meter is a draining bar and a number, recomputed every frame:
`████████░░░░  auto-starts in 3.2s`. The bar is at most 20 cells. Under ten seconds the
number is spelled in tenths (`3.2s`); above it, `47s` or `2m 13s`, always rounded up, so
the last second you have is drawn as a second.

**The clock runs toward yes.** Silence approves the work as briefed, with no redirect
appended, and the card settles as `approved · the clock`. This is the opposite of the
permission card's countdown, which runs toward denying. A task proposal is not a permission
gate — it is your window to redirect the work or wave it off before it starts.

While that countdown runs, the main footer says `starting task`. You do not have to
answer. Holding the proposal removes the countdown; the footer then says
`waiting · your call`, and other windows report `waiting on you` too. An automatic
proposal does not hide a separate question that really needs an answer.

The default window is 15 seconds. **Where is the setting for how long a proposal waits?** It
is `task.autoapprove_seconds`, and it lives on the **`Safety`** tab of the settings panel —
open that with `ctrl+,` or `/settings` — where it is the row labelled `task countdown`. It
is not on the `Spending` tab; it sits with the consent rows because it answers their
question in the other currency.

**This is one of the rows aforge will not change for you.** It decides how long you get
before work starts on its own, so `change_setting` refuses it and points you back at
`/settings`. Same for `task.parallel` below, and for the whole approval and spending
family — the permissions page lists them.

Set that window to 0 and there is no clock at all: no bar is drawn and the row reads
`waiting on you`. The card then waits until you answer it, however long that takes.

Typing the first character in the message box also stops a running proposal clock. The
bar changes to `waiting on you` immediately and the task cannot start while you finish
your answer. Deleting everything you typed does not restart the clock: `esc` still says
no, empty `enter` says yes, and `enter` with words answers from those words.

## What yes, redirect and no each do

**yes** admits the work exactly as briefed.

**redirect** with an empty box does not answer — it takes the focus and waits for your
words. The `enter` after it carries the sentence. Your words travel verbatim and are
appended to the brief; this is the last moment the brief may change. Only `enter` reads a
typed answer. Clicking `yes` approves as briefed, clicking `no` declines, and clicking
`redirect` focuses the lane regardless of what the box already holds. The box is cleared
on an answer, so your next `enter` does not send the correction to the model as a message.

**no** (or `esc`) declines. Nothing is spawned, no row appears on the roster, and no room
exists. This is a normal answer, not an error.

A complete answer typed in the box is also understood. `no`, `nope`, `n`, `stop`,
`cancel`, `don't` and `dont` decline. `yes`, `y`, `ok`, `okay`, `go` and `sure` approve
without adding a redirect. Case does not matter, and a final `.` or `!` is ignored. Only
the whole answer counts: `no, use the flag` is a redirect and approves the corrected
brief rather than declining it.

Once answered, the card collapses to its head and one foot line that keeps both halves —
what you reached for and what it came to, joined by ` · `:

| what you did | the foot line |
| --- | --- |
| approved | `yes · approved` |
| approved with words in the box | `redirect · approved · you redirected it` |
| declined | `no · declined` |
| let the clock run out | `approved · the clock` |
| the turn ended under the question | `expired · the turn ended` |

When the card offered a choice of model, the model you picked is written on the end of that
line — it is the only place your own pick is recorded.

**Honest limit:** once a card has been answered, its brief is no longer reachable from the
card. `ctrl+e` and `ctrl+o` on a settled card do nothing you can see. The whole of a task's
life is in its room instead.

## The task started before I could say no

A proposal starts on silence only while its countdown is still moving. The default window
is 15 seconds. Typing the first character in the message box stops that clock immediately;
the meter changes to `waiting on you`, and deleting the character does not restart it.
Press `esc` to decline, or type a complete no answer and press `enter`.

If nothing was typed before the meter reached zero, the work was already admitted and a
later answer cannot pull it back. Use `task.autoapprove_seconds` in the Safety settings to
give yourself a longer window, or set it to 0 so every watched proposal waits for you.

## I typed no and it started anyway

Type a complete no answer and press `enter`: `no`, `nope`, `n`, `stop`, `cancel`, `don't`
and `dont` all decline a proposed task. Case does not matter, and a final `.` or `!` is
ignored. The first character also stops the countdown, so the task waits while you finish.

Only a bare answer declines. A longer sentence such as `no, use the flag` is treated as a
correction, so the task is approved with those words appended to its brief. Press `esc` for
an unconditional no from any proposal.

## How do I stop a proposed task from starting?

Press `esc`, choose `no`, or type one of the complete no answers — `no`, `nope`, `n`,
`stop`, `cancel`, `don't`, `dont` — and press `enter`. Typing the first character stops a
running countdown and changes the meter to `waiting on you`; erasing your draft does not
restart it. Set `task.autoapprove_seconds` to 0 in Safety settings if every proposal on a
watched session should wait until you answer.

## Why it warned me another window is already in these files — two windows working on the same files

Before a brief becomes a paid run, aforge compares the paths that brief **spells out**
against what every other aforge window open on this directory has already written. Where
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

Either hold ends when the task lands, fails or is stopped. *How work runs* has the whole of
it, under *A task that has written a file holds that file* and *A task working in place
holds the directory*.

## What the other-window warning can and cannot see — and why it stayed quiet

**aforge never tells you that a file is yours alone.** No line means nothing was found, not
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

## The states a task passes through

These are the exact words on screen.

| what is happening | the word you see |
| --- | --- |
| the proposal is still arriving | `⠙ forming… · 6s` after six seconds; under one second there is no clock |
| the proposal is waiting, with no clock | `waiting on you` |
| the proposal is waiting, with a clock | `auto-starts in <time>` |
| queued behind something | `waiting · <reason>` |
| queued behind named work | `waits: <title of the work it needs>` |
| running | a turning spinner, and what it is doing this second |
| running and closing a gap | `finishing · <what it is closing>` |
| stopped by you | `stopped`, with `⊘` on the roster in place of the failure cross |
| stopped before it ever ran | `stopped before it started` |
| stopped | `stopped` |
| stopped, with work on a branch | `stopped — branch kept` |
| landed clean | `done` |
| checked and not accepted | `incomplete`, with what is still missing |
| broke while running | `failed`, with what went wrong |
| landed, but nobody could judge it | `needs your look` |
| cut off while it was being checked | `needs your look`, with `incomplete — it was stopped while its work was being checked` |
| …the same thing on the roster | `finished — look it over` |
| …the same thing when there was no report | `finished, but needs your look` |
| …its branch, on the card | `branch kept` |

`finishing` is not a separate state — the work is still running, and the sentence after the
word names the gap it is tying off.

The three reasons a queued task gives for waiting are `slot`, `machine busy` and
`rate limited`. A named prerequisite outranks any of them, because a name is something you
can act on and a queue clears itself.

How a branch came home is spelled `merged`, `branch kept · <branch>`, `in your own folder`
(there was no branch to bring home — the work edited your own files), or
`conflicted · <branch>`.

## A task that was cut off while its work was being checked — interrupted work, killed mid-check, why did my task fail when nothing was wrong with it

A worker's deadline is watched even while a command, backgrounded check or model
request produces no new events. At the deadline, the existing progress check
decides whether to renew the allowance or stop and preserve partial work. Waiting
on the worker's own command counts toward that allowance; waiting for delegated
parts keeps the existing pause rule. An unfinished check is never a passing check.

**A cancel is an interruption, never a finding about the work.** When aforge quits, a
deadline on the whole session fires, or something outside the task ends it while the check
is running, nobody has looked at the deliverable and nobody has said anything about it. So
the task does **not** land as `failed`. It lands as **`needs your look`**, with the plain
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
gaps in front of it. The difference is whether anybody actually looked: `needs your look`
means nobody could judge it; `incomplete` means the check did judge it and named the next
work.

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
first sentence, quoted because they are its words and not aforge's.

**Every fact on the head is joined by ` · `, the state word included.** It used to read
`done 4m12s`, with the state and the clock fused into one phrase while `3 files` beside
them was properly separated — so on a card asking for a hand, `needs your look 12m00s`
made the reason it was asking read as part of a duration.

**`started 14:02` is when the work began**, and the word is `started` — it said `spawned`
until 2026-09-03, which is the machinery's own verb for launching a process and not a word
anybody reads on a screen here. A task replayed out of a checkpoint carries the instant it
began, so it says the same `started 14:02` after a restart that it said before one. Where
nothing knows when the work started — a checkpoint written before the record carried the
instant — the stamp is **absent** rather than invented.

- **`done`** — a tick, muted. It is settled work on the roster.
- **`incomplete`** — a `!` in the warn hue. A check did not accept the claim, and the
  report says exactly what is still missing. Its branch is kept.
- **`failed`** — a cross, in the bad hue, drawn with the word `failed`. It is settled too,
  and means the run actually broke rather than that a check found unfinished work.
- **`needs your look`** — a `?` in the warn hue. Its family rises above running work on
  the roster. The `?` is deliberately neither a tick nor a cross: it claims neither a
  finding nor a judgement nobody made. This card carries two more rows —
  `finished, but nobody has checked it — your call` and the choices under it — unless you
  have set `task.settle` to `auto`, in which case aforge is deciding and the card is quiet.

After the name the card carries the span, the file count, and how the branch came home:
`merged`, `in your own folder`, `conflicted · <branch>`, `stopped — branch kept · <branch>`,
or `branch kept · <branch>`.

`branch kept · <branch>` on a **done** task means the work finished but your checkout was
on a protected branch, was on a different branch than when the work was cut, moved
to a different commit by your own work after the cut, or was detached. The branch
named there holds the finished work; the how-tasks-run page explains the exact reason.
Inspect that branch and keep the delivery workflow you requested. A task finishing
does not by itself request a merge or a checkout change.

Click anywhere on the card, or press `ctrl+o` with it selected, to expand it. `enter` on the
selected card opens the task's room instead. What the expansion holds, and in what order, is
under *What an expanded landing card shows* below. Each long field caps at 20 rows.

The branch row is labelled with **where the work was done**, in plain words rather than in
git's: `a branch of your repository`, `its own copy of the folder`, or `your own folder` —
the same labels the settled card uses, listed under *Does a task touch my working copy?* in
*how tasks run*. A landing whose copy aforge has no record of falls back to `branch`.

More than two landings in a row become one rollup — `✓ 3 tasks done · 9m14s` with a compact
row per task under it. Any failure in the batch swaps the header to `✗ N tasks landed`; any
`needs your look` swaps it to `? N tasks landed`. A failed delivery also keeps a warning
on the batch and its individual row. The header's span is wall-clock, first
spawn to last landing, not the sum of the parts, because tasks run at the same time.

## What an expanded landing card shows — why is the task answer in asterisks, Markdown, the delivery warning, the facts

Click a landing card, or press `ctrl+o` with it selected, to expand it. The order is
**delivery status**, **the answer**, then **supporting details**.

A failed save or integration leads with a warning behind `!`. Its headline says
`delivery needs attention` even if the work itself was accepted. Diagnostics follow
in quieter text, once. A branch deliberately kept separate is described without a
failure warning; keeping a branch can be the requested outcome.

The answer uses the normal reply renderer: headings, lists, bold, and fenced code
appear as formatted Markdown in body ink. A shortened result names where the rest
can be read: `… the whole of it is at <path>`, or `… the rest of it was not kept`.
If a check turned the result back, the card says `what it produced was not taken
as done` and points to the retained record instead of presenting an accepted answer.

Supporting details follow: `changed · <files>`, the branch, then
`model · <model> · $<cost> · ran · 14:02 → 14:14`. Whole facts wrap onto another
row when needed. An unknown price is omitted. With no end time, the card says
`started 14:02`. The acceptance criteria and original brief follow when available.

An expanded card omits the quoted preview and repeated branch from its heading.
Closing it restores the compact summary. Each long field is capped at 20 rows;
open the task's conversation for the full record.

## Watching work: the strip along the top

The strip is one row under the pinned header — a tab bar of doors into live work:

```
⠙ Fix nil-map · ◆ Auth tests · +2
```

It appears only while something is running, and goes away the moment nothing is. It needs a
frame at least 24 columns wide and 6 rows tall. It is the narrow-frame door: wherever the
roster stands — as the right column or open over the whole frame — the strip stands down.
A column you closed with `ctrl+g` is a roster standing down, so the strip comes back and
running work stays reachable. The one exception is a running sub-harness: its chip raises
the strip even beside a standing roster, because the roster's rows are tasks and a harness
run is not one — the chip is the only place on the screen that run exists.

A blank line sits under the chips, separating them from the first line of conversation.
It is part of the strip and leaves with it.

Order: running first, then work that needs you, then idle. Parked and finished work never
appear on it — the strip is the live set, the roster is this session's whole record, and
`/history` is the project's, across every session.

A chip carries one glyph and the name cut to 18 cells, and nothing else: no clock, no
spend, no tool name, no tree connector, no cursor mark, and no stop button. The room you
are standing in takes a colour band. The strip is one flat row even when a task has
children; the roster is where the family tree is drawn.

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

The roster is the **top section of the column on the right**, under a dim lowercase label
reading `tasks`. Under it the same column carries a second section labelled `standing` —
the orders standing over this conversation — and, when this conversation has started any,
a third labelled `jobs`. The standing orders page has that half; *Background jobs on the
column* below has the jobs section.

The roster holds every task **this conversation** has admitted, not just the live ones.
Background jobs are **not** rows among the families: they have their own section under
`standing`. Tasks
*other* sessions ran are not on it; the
page `/history` opens is the one that has them, and one dim line at the foot of the column,
`ctrl+. earlier`, is the door onto it. Work finishing never puts the column away, and
neither does `/new` — that takes this session's tasks with it and leaves the column
standing, with the door onto the project's record still at its foot. With no foreground
command to keep, `ctrl+g` closes it and leaves the work exactly where it was. While a
command can be kept, that command takes the key instead; the column's `❯` pointer door
still closes it. The bottom line names whichever keyboard action is available.

The column is permanent: it stands from the session's first keystroke, before any task
exists, at a frame width of 100 columns or more — 30 columns wide from 120 up, a slim 24
columns from 100 to 119. Work fills it rather than raising it. The one frame without it is
the untouched empty conversation, which opens on a centred greeting and no column at all
until you type, a task lands, or a standing order reaches it (*The empty screen* page). A conversation that has run nothing
draws no empty label or absence sentence, **whatever the project has behind it** — the
typeable `+ /task` door remains, and in a directory whose earlier sessions ran tasks the door
`ctrl+. earlier` sits at the foot of the column. Under 100 columns there is no
column, and `ctrl+t` opens the same roster over the body instead once this session has
tasks.

Closed with `ctrl+g` when no foreground command owns the key, the column leaves a
two-column edge at the right of the frame that opens it again on a click — see *The task
bar disappeared* below.

The roster is a forest. Each root task is followed by its whole family, with children
joined by three-cell connectors (`├─ `, `└─ `, `│  `). Families are ordered by their most
urgent member: needs you, running, queued, waiting, then done. `queued` is admitted work
with nothing in its way but a slot; `waiting` is admitted work held behind other work, and
the row says what it is held behind. (They were `idle` and `parked`, which were two
unrelated words for one shape of fact and disagreed with the tasks page, where the same
node read `parked`.) There are no state-group
headings. The footer keeps those totals as counts, such as
`2 need you · 3 running · 12 done`.

**The pieces INSIDE a family are ranked the same way.** They used to be drawn in the order
the session admitted them, so a run that hands four errands out and finishes them one at a
time read `done, done, running, running` — with the only rows anybody was watching at the
bottom of the block. The same ladder now applies all the way down: needs you, running,
queued, waiting, done, with admission order deciding between two pieces in the same state.
Two settled siblings therefore never trade places while you are looking at them. The kin
line under a task's own page shows the same order.

Folding belongs to each node. Families with a running, needs-you, or idle member start
open. Settled families and families containing only parked work start folded to their
root; the root then carries the family's aggregate state glyph and a `▸ +N` badge for the
hidden descendants.

**Workers under a task are the family's own rows and nothing else** — there is no second,
smaller list of hands drawn beneath a row. A task that split itself into parts, and an
adaptive run and its workers, each announce themselves as tasks with a parent, so every one
of them is an ordinary row of the forest above: its own state glyph, its own name, its own
`#id`, reachable with `↑`/`↓` and openable with `→`. A worker you can see is a row you can
walk to.

When two or more workers are running anywhere in the live work, the section label carries
the count as a quiet tail, for example `tasks · 4 working`. The number is the payload. At
zero or one running worker there is no tail at all, so the label remains `tasks`.

## What is the diamond symbol next to each task? — why the sidebar has no diamond, the mark on the cards

**On the column at the right there is no diamond.** A task's row there opens with one cell,
the **state** glyph, which changes as the work does. Then the name, then its id as `#7`,
dim, at the far end — and the id stands down when the name would be left under 12 cells.
Every row of that column leads the same way, family rows included, so it reads downward as
one column of states.

The `◆` is still on the surfaces that hold more than tasks: the proposal card, the card
that lands, the notes in the conversation, and a queued task's chip on the strip above the
conversation. It is one marker, the same on every task, saying only that this row is a
piece of work — which is worth a cell where tasks sit among other things, and worth nothing
in a column that is only tasks. It used to be on the column too, where it cost two of the
twenty-two cells the name has and pushed each row's detail line two cells out of line under
its own title. On a terminal with no colours or no unicode the marker is `#`.

The marker used to be one of eight shapes in one of six colours, picked from the task's id,
so that a given task wore `◆` teal everywhere. That is gone. It had to be learned, it was
relearned every session because ids start again in each conversation, and it told you
nothing you could act on — the `#7` already says which task, and the state glyph and its
word already say what it is doing.

**The name is the task's own title, cut to its first three words** — `Fix the nil-map`,
`Collect the sources` — and that is the name it wears everywhere: the column, the strip
above the conversation, its room's header, the card that lands, the home card and the task
page. Three words is also what the `taskname` call is asked for, so a named task fits the
column whole rather than being cut to fit it. A row reading **`task 19`** means one thing
only: nothing has told aforge what that task is called yet. It is a name you can still say
out loud, and the row takes the real one the moment the title arrives — including a room you
already have open on it. Under the row, at
most two more: what it is doing, what is holding it, what it waits on, or how its branch
came home. `conflicted · task/fix-nil` in the bad hue is the one loud row on the column.

**The row of the room you are standing in is picked out.** Walk into a task — from the
roster, a strip chip, a spawn card or a `task 7` link — and that task's row in the column
takes a colour band across its whole width, every line of it, with its title in the accent
and bold. It is the same mark the strip puts on the chip of the room you are in, so the two
lists of the work never disagree about which door you went through. It follows you: opening
another task's room moves it, and `esc` back to the conversation clears it. With no room
open no row is marked at all. On a terminal with no background colours the accent title is
what is left of it.

There is no subtitle here. The column is a presence list; the proposal card and the landing
card both carry the sentence.

Under the task rows, one dim `+ /task` row closes the section — press it and `/task ` is
typed into your message box. Then a blank line, then the column's `standing` section.

At the bottom, under both sections, up to three dim lines: `Σ $1.42 · 312k tok`,
`1 need you · 3 running`,
`148 parked · 12 done`. The `Σ` is the whole session's spend — it already contains every
task in the column plus the conversation, so there is deliberately no per-task share. Zero
figures are left out entirely, because zero means "nobody published a price", never "free".

Under those, always, one more door line. It reads `❯ ctrl+g hide` when no foreground
command can be kept and only `❯ hide` while a command owns that key. Click either form
and the column goes away. The `❯` is in ink and the words are dim, because the chevron
is what the pointer presses and the words name only the keyboard action available now.

**Work that is running never scrolls off it.** Families are already ordered so that
anything running or waiting on you leads the column, and those rows are also *pinned*: when
you walk the cursor down into a long list of finished work, everything under the running
head scrolls and the running head stays where it is. The pin gives way only on a column with more running
work than it has rows, where it keeps one row back for everything else — a session that big
is read on the task page instead.

**Non-running rows are drawn quieter.** A running task's name is in the ordinary text
colour; idle, parked and finished names are muted, the tree connectors and every detail
line are dim, and the room you are standing in is the one row in the accent. Nothing is
hidden by this — the column is a record and keeps everything — but a glance at it lands on
what is moving.

## Background jobs on the column — the jobs list on the right, what is running in the background, why a long command shows on the right, the row for a server, build or watch

**A background job is not a row among the tasks.** It used to sit in the roster with the
families. It is now a **third section** on the same column, under `tasks` and `standing`,
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
still going when aforge closed comes back `stopped`. Nothing is restarted. The log stays
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

- a watch, a render or a hand is named the moment it starts (`watch app`, the render's
  title, the hand's part) — a second call would disagree with `jobs list`
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

**A running job still counts as working** in the live-work tree, so when more than one
worker is running the `tasks` label can read `tasks · N working` with jobs included. The
jobs section's own label is the count of *jobs*: `jobs · 2 running`.

**What a job's row does not have**, because a job has none of them:

- no branch, no changed-files list and no price — a job runs in the workspace itself, and
  nothing measures what it costs, because it costs nothing but time
- no agent inside it and no transcript — enter opens a **page**, not a chat
- no `✕` on the row itself — stop it from the **page** (`x`), or ask aforge to run
  `jobs kill`

**No card is written into the conversation** when a job ends, because a job has no report
anybody wrote — what it left is its log. Aforge itself is still told, on its own side, at
the next step boundary (`job 3 exited 1: make: *** [build] Error 1`).

## How do I stop a background job — stop a job from its page, can I stop a background job from the sidebar

**Yes. Open the job's page and press `x`.** Kill a server you started the same
way. A job used to have no stop on the surface, and the only way was to ask
aforge to run `jobs kill`. The row itself still has no `✕` — walk into the jobs
section, enter on the job, then `x`. That is how you stop it from the sidebar:
the section is the door onto the page, and the page is the door onto the stop.

`x` raises the same confirmation every other stop on this surface raises, with the cursor
on the safe answer:

```
? Stop this job? The process is ended; its log is kept.
  [stop it]   [keep going]
```

`enter` on `stop it` ends the process. The engine's door is `job:3` — the same number the
handle shows. The line it answers with is `stopped job 3 (the name) — its log is kept`.
The model is told `job 3 was stopped` so it does not keep reasoning about work that is no
longer running. Pressing `x` on a job that has already ended does nothing: the settled
page does not offer the key.

**You can still ask aforge to run `jobs kill N`.** That is the model's tool and it still
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

**The default is $100.00**, which is the tank aforge applies to work nobody put a figure on.
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
proposal card and the model's own hands run under the conversation's own limit — the
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
- It is drawn whether or not this conversation has run anything, under the column's own
  `tasks` label, and it is the **pointer's** row: the roster's keyboard cursor (`ctrl+t`)
  walks task rows and skips it. From the keyboard you type the command, which is what the
  row is teaching.

Finish the sentence and send it and it is `/task <brief>` like any other: aforge sizes the
work, shapes the brief and starts it. The column's other section, `standing`, ends in a
`+ /standing` row that works the same way.

## What a bare /task does — /task with nothing after it opens the task page

**`/task` typed on its own opens the full-screen task page** — the same page `/history` and
`ctrl+.` open, holding every task this project has ever run. It used to print a one-line
usage instead. It does not any more.

The reason is the `+ /task` row at the foot of the task column: that row puts `/task ` in
your box before you have said what the work is, so a `/task` sent as it stands is asking
the only question the word can answer with no brief behind it — *what work is there.*

**On a project that has never run a task it opens the page anyway**, and the page says what
tasks are and ends `no tasks yet — /task <brief> starts one` — which is exactly what
`/history` and `ctrl+.` do there too.

**The forms that start work are unchanged.** `/task <brief>` and `/task solo <brief>`
still size, shape and start the work directly, with no proposal card in between and no
extra question. There is no third form: `/task adaptive` is retired.

**There is still no `/tasks` command**, though `tasks` is the name of the PLACE `/history`
opens — `alt+2` and `tab` get there without typing anything. As a slash word the plural is not one this surface answers to;
the two things a bare `/task` and a `/task <brief>` do are the pair of errands a person has
about tasks — go and look at the work, or give aforge some.

## Old tasks from previous sessions are not on the column — the `ctrl+. earlier` door

**The column is this conversation's work and nothing else.** No rows of earlier sessions'
tasks are drawn under it. What sits at the foot of the column instead, whenever the project
has a record this conversation never ran, is one dim line:

```
ctrl+. earlier
```

- **It is a door and not a note.** Press `ctrl+.`, or click that line, and the full-screen
  tasks place opens with every task this machine has run on it, grouped by what you do next
  — `needs your look`, `running`, `waiting`, `finished today`, `earlier`. `/history` is the same page.
- **It says what is behind it.** With a record behind it the line reads `ctrl+. earlier`;
  with no record, on a column that has merely folded a family away, the same line reads
  `ctrl+. view more`. There is only ever one such line.
- **It is drawn only when there is something behind it**, and never as `0 earlier` or any
  other count of nothing.
- **A task of your own is not behind it twice.** A task this conversation ran, running or
  landed, is on the column above in its own family; the door is offered for work this
  session never ran, and for a family the column has folded.
- It is dim, like the totals above it, and it is a button as well as a key.

**The column used to footnote the record** — up to six dulled `earlier` rows under this
session's work, walkable with `↓`, each opening a card. They are gone. Six rows out of a
record that runs to two thousand is a sample; they stood where the column's own
`no tasks yet` label goes; and the cursor walked out of this conversation's work into
another one's without the column ever saying it had. Everything they offered is on the
other side of the door, whole: every row, the filter, the cards, and `m` for the mention.

**Where old work is listed now:** the task page (`ctrl+.`, `/history`, or that line), and
home (`/home`, or space twice on an empty box). The chat can also read the whole project
record for you with its `tasks` tool — just ask.

**Running work in another aforge window** is on no surface but the task page. An ordinary
task writes nothing into the project's file until it lands, so the window next door is the
only place that work can be read from, and `/history` is the page that reads it.

## The column comes back after a restart — tasks reappear on resume or a switch back

**Reopening a conversation re-draws its own tasks, and the jobs it ran.** `/resume`,
`aforge resume`, and switching back behind home all rebuild the column from the record:
every task admitted here comes back as a row in its family — finished work included, and
work that was interrupted comes back saying so on its card — and the `jobs` section
redraws the jobs this conversation started, settled. Tasks and jobs are not lost when the
terminal closes; the column is rebuilt, not carried.

The rebuilt row reads its start and landing times from that same record. Work that landed
in a previous session therefore keeps the time it actually landed instead of taking the
time you reopened the conversation. A record made before those times were kept still
reopens; its row leaves the age absent and its completion card omits the entire
`started 14:02` segment.

**Only this window's own work comes back.** Everything else stays behind the
`ctrl+. earlier` door, exactly as the section above says, and a task is never drawn
twice — a row on the column is not also an `earlier` row.

## Hiding the task column: closing the right sidebar, panel or task bar

With no foreground command that can be kept, `ctrl+g` closes the column of tasks on the
right and gives its columns back to the conversation. Press it again and the column
comes back with the current state of the work in it, including anything that started or
finished while it was gone — nothing here is a snapshot; the column is redrawn from the
tasks every frame. While a foreground command can be kept, that command takes the key
instead and the column stays exactly where it was.

**The pointer can do the whole cycle on its own.** The last line of the column reads
`❯ ctrl+g hide` with no foreground command to keep and `❯ hide` while one owns the
key. The chevron is in ink: click either form and the column closes. What is left
behind is a thin edge carrying `❮`: click that and the column comes back. One control, two
states — `❯` to close, `❮` to open — so a closed column is never a thing you need to know a
chord to recover. See *The task bar disappeared* below.

The choice is remembered. It is written to your profile the moment the column moves, as
the `ui.task_column` setting, which also appears in the settings panel (`ctrl+,`) on the
Display tab as **task column**. A change made in the panel lands the next time aforge
starts; `ctrl+g` acts immediately and wins for this session.

With the column closed, work is still visible:

- Anything **running** draws the task strip along the top — `⠙ Fix nil-map · ◆ Auth tests
  · +2` — because the strip stands up wherever the roster stands down. Click a chip for
  that task's room, or the `+N` for the whole roster.
- The legend above the message box carries `ctrl+g tasks` in its hint slot for as long as
  this session has any tasks at all, running or not. A session that has run nothing says
  nothing there — the column you closed was empty, and `ctrl+g` still brings it back.
- `ctrl+t` still works: asking for the roster brings the column back and gives it the
  keyboard in one press.

When no command can be kept, `ctrl+g` works whether or not the session has tasks — the
column stands empty, so an empty column is still a column to close. It does nothing, and
is not swallowed, only when there is no roster on the frame at all: a frame under 100
columns where nothing has raised the roster over the body. On
the untouched empty screen, where no column has stood yet, the key is a first keystroke
first — the greeting goes — and then closes the column as usual.

## The task bar disappeared — how do I get the task column back

A closed column does not vanish without a trace. It leaves a **thin edge two columns wide
down the right of the frame**, with a `❮` handle at the middle of it, drawn in ordinary ink
rather than dim so the eye can find it:

```
 …and the parser suite passes now.                                       ❮
```

**Click anywhere on that edge and the column comes back** — the whole strip is the door,
not just the handle, so there is nothing to aim at. It is the column act `ctrl+g` performs
whenever no foreground command can be kept.

- Under the pointer the handle brightens further and the whole two-cell strip takes a
  background, which is how everything pressable on this screen says so.
- **The chevron points the way the column goes**, and it is the same control in its other
  state: `❮` while the column is away, `❯` on the final door line while it stands.
  That line says `ctrl+g hide` only when the key is available, and says `hide` otherwise.
  Clicking one gives you the other, so the pointer goes round the full cycle. On a terminal
  that cannot draw them they are `<` and `>`.
- **One cell above the handle says what the work is doing**, while there is anything worth
  saying: `?` in the question colour when a task is waiting on you, `◐` in the accent when
  something is running. Nothing at all otherwise — a session with nothing running and
  nothing waiting leaves the edge silent, and so does one that has run no work.
- The edge costs the conversation two columns, exactly as the column it stands for costs it
  its own width. The text re-wraps; nothing is ever drawn underneath it.
- **On a frame narrower than 100 columns there is no edge**, because there is no column at
  that width to bring back. The roster still opens over the whole frame with `ctrl+t`.
- The edge is for the hand that does not type chords. With no foreground command to keep,
  `ctrl+g` is the keyboard's way around the same cycle; with one, it backgrounds the command.

The legend above the message box also carries `ctrl+g tasks` while the column is away and
this session has run something.

## Using the roster from the keyboard

`ctrl+t` hands the keyboard to the roster. It is asked for, never taken: the draft is the
rest state, so a person who starts typing is typing, not navigating.

| key | what it does |
| --- | --- |
| `↑` `↓` | walk the visible tree |
| `→` | open a folded family, or step to the first child |
| `←` | fold an open family, or jump to the parent row |
| `enter` | open the task's room, or a job's page; on the `jobs` label, toggle the section |
| `alt+w` | toggle the wider 46-column tree (bare `w` only on the full-frame roster) |
| `esc` or `ctrl+t` | give the keyboard back |
| `ctrl+g` | keep a foreground command when one can be kept; otherwise close the column altogether, or bring it back — this one works whether or not the roster holds the keyboard |

The legend hint while it holds the keyboard is `↑↓ move · →← fold · enter open · esc`.
When depth has forced a title to be cut, the footer adds `alt+w widen · click seam` (or
`alt+w narrow · click seam` once it is wide); that hint is clickable as well as available
from the keyboard, and so is the `❯` door line under it. The latter includes `ctrl+g`
only when no foreground command owns the chord. **Both the hint and the seam appear only
from 120 columns up**, which is the only width that lends a wider tier — on a 100-to-119
column frame the footer says nothing about widening and the column's left edge is part of
the row, not a handle.

Every other key is given back. The roster cannot take the keyboard while the exit
confirmation, a permission question, a task proposal, or any overlay is up, and with no
tasks **and no jobs** of this conversation's on the column, `ctrl+t` falls through rather
than being swallowed — including in a directory whose earlier sessions ran plenty. There
are no rows down there to put a cursor on; the door `ctrl+. earlier` at the foot of the
column is how that work is reached. A session that has only started a server still has
the jobs section to put a cursor on, so `ctrl+t` takes it.

**The walk stops at this conversation's last job, after its last task.** `↓` walks the
families and then the jobs section under them, and clamps there rather than carrying on
into the project's record. `enter` on a task row opens that task's **room**; `enter` on a
job row opens that job's **page**; `enter` on the `jobs` label toggles the section. It
used to walk into six dulled `earlier` rows below, which meant holding `↓` took you out of
this conversation's work and into another one's; old work is walked on the task page now
(`ctrl+.`), where `enter` goes inside its card.

The cursor follows the task, not the row, when families reorder or fold around it.

With the pointer, a row takes the hover background step. The next section has the whole of
what a click on the column does.

## Clicking the tasks on the right — I cannot open a task from the sidebar, clicking a row does nothing, the list jumps instead of opening

**Anywhere on a task's row opens that task's room.** The state glyph, the name, the `#7`,
the blank cells after it — the whole visible row is that one door, at every width the
column stands at. A click moves the column's cursor to what was clicked but does not hand
the roster the keyboard; the draft is still where you type.

Exactly two things on a row mean something else, and **both of them are drawn on the screen
at the moment you press them**:

- **The `▸ +N` badge** at the right of a folded family's root. It is what says work is
  hidden under that row, so pressing it opens the family. It is on screen all the time.
- **The disclosure triangle**, which appears in the leading cell of a family root — and of
  a finished row holding a detail block back — **only while the pointer is on that row**
  (`▾` open, `▸` folded). While it is drawn, pressing it folds; the rest of the row still
  opens the task.

With the pointer anywhere else, that leading cell is the task's **state**, and pressing a
state opens the work it is the state of. It used to fold the list instead, whether or not
a triangle was drawn there — so a press aimed at a family root or a finished task made the
list jump and opened nothing. It was easiest to hit straight after walking into or out of
another task, because that is when the surface forgets where the pointer is and the
triangle is not drawn.

**Two other cells used to swallow presses and no longer do.** The column's two leftmost
cells are the resize handle only from 120 columns up; on a narrower frame there is no
wider tier to pull to, so those cells are part of the row like any other. And the column
answers only for the rows it actually draws — a press below its last row belongs to the
message box, the legend or the status line under it, not to the column.

A click inside the column that lands on no task at all still belongs to the column and
does nothing, rather than closing the task page you are reading.

**A second click on the selected row keeps its task open**, with the same draft and
reading position. Press `esc` or the back control to return to the conversation.

## What did we do last week — why is my old task not on the tasks page, an old task says now, and a task from a previous session is missing from the tasks page

This is where **task history** lives — every **old and past task**, and **work from other
sessions**, on one page.

`/history`, or `ctrl+.`, opens the machine-wide **tasks** place holding work from every project
run** — this conversation's and every conversation's before it. It is the answer the roster
cannot give: the column beside the conversation is built from *this session's* work and
nothing else, so a task you ran last week, in a session you have closed, is nowhere on the
screen until you open this.

**There is no `/tasks` command** — though `tasks` is what the PLACE this opens is called on
the tab bar, reached with `alt+2` or `tab`. `/task <brief>` starts work; `/history` opens the same tasks place
started — and so does a **bare `/task`**, which opens this very page rather than printing a
usage line. The page is also reached from the one dim door line at the bottom of the task
column — `ctrl+. earlier`, or `ctrl+. view more` where the column has merely folded a
family away.

The page takes the whole frame, the way the settings panel does. `esc` closes it. Three
pages here take the frame — the settings panel, this one, and `/home` — and **only one of
them is ever up**: opening any one closes the other two.

It opens on one heading saying what it is holding — for example
`tasks · 148 pieces of work since aug 11 · $34.10`. The count is every row the time window
holds, the date is the far edge of that window, and the money is what those rows are known
to have cost. A window with no known start drops the `since`, and rows nobody priced drop
the money: zero means "nobody published a price", never "free". **A frame too narrow for
the whole line drops the money clause** rather than cutting it, because half a figure is a
wrong number.

It used to be a paragraph — `work aforge ran on its own. 148 pieces of work since aug 11,
$34.10 between them.` — which was the widest thing on the page, taught the machinery's own
idea of itself before naming anything, and pushed the rows a person came for further down.
The place's name, its count and its window are what the line is for.

**The count is a claim about the PLACE and never about what you have typed.** With a filter
on, the sentence goes on counting every row the window holds — a word that matches nothing
does not make the machine's history empty. What matched is said on the note line at the
bottom instead: `filter · zzz · nothing matches`.

**When the time window holds none of it, that line says `tasks · nothing since jul 29`** —
in words, because a `0` there is the figure the emptiness law forbids, and with the date
still on it because the date is what says the window is the reason. The sentence stays on the frame in that state: it is the only thing naming the
window the four shift-arrows move, so a page that replaced it with the teaching prose
would have swallowed the way back. The teaching prose is for a machine that has run
nothing IN ANY WINDOW, which is a different screen.

Under it, **five sections, in the order you act on them**: `needs your look`, `running`,
`waiting`, `finished today`, then `earlier`. `running` is work a worker is actually inside;
`waiting` is work that has been admitted and that nothing is doing — behind the piece that
needs a person, or behind a slot. A waiting row **carries no age at all**: it has not
started, so there is nothing to count from. Nothing is grouped by whose work it is — a task
this conversation started sits beside one another window is running and one a session you
closed last week finished, filed by what you would do about it next.

`finished today` is **everything that ended today, however it ended** — work that came off,
work that failed, work somebody stopped. It was called `done today` and held all three,
which made `done` untrue of some of its own rows; each row still says which it was.

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
says the same word its own page says, with the reason after it: `failed · the package
manager refused the archive` when the run actually broke, `incomplete · …` when a check
named what is missing or the wire, a limit or a stale brief ended it, and `stopped · …`
when you ended it yourself. The cross is kept for the fault; the rest wear `!` and `⊘`,
because nothing was found wrong with them.

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
`finished today · 2 of 5 shown` — and `→` opens it. Everything else the window holds has a
line, and the page scrolls —
`↑`/`↓`, `pgup`/`pgdown`, `home`/`end` and the wheel all walk it. **The last rows fade** when
the list runs on below the bottom of the window: three rows, each a step fainter, saying
there is more under them. The row the cursor is on never fades wherever it sits, and a list
short enough to fit fades nothing at all — see *Why the bottom rows of a long list look
dimmer* on the screen page.

At the bottom: one dim line counting THE WORK THE WINDOW HOLDS, section by section —
not the rows drawn, which is why it can read a larger number than you can count on the
screen when a family is folded. The section heading is where that difference is said. It
reads such as
`2 needs your look · 3 running · 12 finished today · 148 earlier` — a section with nothing in it
is not counted at all — and under it the keys.

**Every door onto this place opens it, on a machine that has run nothing too.** `/history`,
a bare `/task`, `ctrl+.`, `alt+2` and `tab` all reach the same page, and with nothing on it
the page explains itself instead of drawing counts:

```
tasks is the history of work this machine has run.
it lists work aforge ran on its own, across every project.
enter opens a task's room when there is one here.
no tasks yet — /task <brief> starts one
```

That last line used to be what `/history` said **instead** of opening, with `ctrl+.` doing
nothing at all rather than raising an empty page. On a machine aforge was installed on an
hour ago that was every door onto the page, so the first thing anybody tried appeared not to
work. The sentence stayed and moved onto the page it is about. A session that has run
nothing itself still shows work another window on the same directory is running — that is
the one fact it was opened to report.

The list is as long as the record is — internal to aforge each project's record keeps the
most recent 2000 tasks — and the page scrolls rather than cutting it.

## Work running in another aforge window — a task started in my other terminal, I cannot click into a running task, open a task another window is running, why is that task page read only, the task page says it cannot ask what the work is doing, can I read another conversation's task over ssh

Two aforge windows open on one directory can see each other's running work, and `/history`
is where they see it.

**A conversation *this* terminal is holding is never one of them.** One terminal can have
several conversations open at once with `aforge chat --no-host`, in this project or others,
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
- The row's glyph is the task's own state, so `▸` is running and `◌` is queued behind
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

Yes, and it is told rather than having to go and ask. When another aforge window on this
same directory lands a task, or has one running, aforge puts a small block into the chat's
own context — never on your screen — that reads:

```
<elsewhere>
Work on this project from outside this conversation. Facts, not requests.
recently landed in other windows:
- Fix the nil-map crash · done · internal/reconciler/state.go, internal/reconciler/state_test.go
  Added the guard and the regression test; the parser suite passes.
running in another window now:
- Sweep the call sites · window "docs pass" · internal/session/agent.go
</elsewhere>
```

- **`recently landed in other windows:`** is tasks that finished **in another window**. Your
  own conversation's tasks are never repeated there — their reports already arrived here in
  full. Each row names the task, how it ended, and the files it wrote.
- **`running in another window now:`** is what those windows have out at this moment, with
  the files each run has already written. Written, not planned: nothing is reserved and
  nothing is locked by it.
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
running in other aforge windows on this project:
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

## Searching the task page: type to filter, find an old task by name, why does the tasks box say type to filter this list, my cursor jumped to another task while I was reading

**Just type.** On the task page every printable key — letters, digits and the space —
builds a filter, and both sections narrow against it as you go. The message box at the foot
of this place says so itself: it rests on `type to filter this list` rather than the
`say what you want done` every other place shows, because there is nothing to send from here
and a box inviting an instruction over a slot that only filters was the one thing on the
screen telling you the wrong story.

```
filter · parser
```

is the dim line above the keys at the foot, so a list that has lost rows never loses them
for a reason you cannot see.

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
- With nothing matching, the foot reads `filter · zzz · nothing matches`.
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
| any printable key | type into the filter |
| `backspace` `ctrl+w` `ctrl+u` | edit the filter |
| `esc` | clear the filter, or close the page when there is none |
| `ctrl+.` | close the page |

`ctrl+c` still works and still means what it always means: mid-turn it interrupts,
and at rest it takes two presses within 1.5 seconds to quit — the page stays up while
the door is armed, and the armed line names any task that would stop.

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

Clicking a row's title does what `enter` on it does, on the **first** press — the page opens things,
it does not change them. The row under the pointer takes the hover step. The wheel walks the
cursor.

## Main chats and their subtasks — the conversation tree, folds, holds 3 more

The **main chat is the parent** of the work it requested. Tasks hang beneath their
conversation; a task's children hang beneath that task, including deeper levels.
Chats in the selected time window appear even before they delegate any work.

```
needs your look
  ▾ Repair the parser                  aforge
      ▾ Update the parser
        ├ Port the lexer
        └ Port the tests
      Update the documentation
```

Conversations stay together and move to their most urgent work's section. Each child
keeps its own state: a finished sibling does not become a decision because another
child needs you. The footer counts those actual task states.

**Click a conversation title or press `enter` to open its main chat.** A task title
opens that task's room or record through the existing owner-aware navigation.
Conversation rows offer no worker stop or mention action.

Conversations start expanded. Families beneath a task start folded. `→` opens a fold;
`←` closes it. Clicking the visible disclosure arrow also toggles it; clicking the
name opens the chat or task. A shut row says `holds N more`. Open folds and the cursor
stay attached to their conversation and task identities when the page refreshes.

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

## Going inside an old task — see what a past task did, read a finished task's report

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

 a branch of your repository · ~/.aforge/v3/projects/-tmp-alpha/trees/fix-the-nil-map-crash
 transcript · ~/.aforge/v3/projects/-tmp-alpha/aaaa…/tasks/20260819-120133_7.jsonl

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

- **Anything aforge does not know is not drawn at all.** A task that spent nothing has no
  money line, one that wrote nothing has no file count, one still claiming to be running
  has no clock. That includes a run that plans itself: while it is running its card carries
  its state word and no `landed` clause at all; the clock appears when the run ends. Nothing
  here appears as a zero.
- **The first address is labelled with what that directory was**, in plain words and never
  in git's: `a branch of your repository`, `its own copy of the folder`, `your own folder`,
  or `where` when aforge's record does not say. *Does a task touch my working copy?* in
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

## The one door line at the bottom of the task column: `ctrl+. earlier`, `view more`

When there is more work than the column is showing, the roster's footer grows one more dim
line above the column's final `❯` hide door:

```
ctrl+. earlier
```

Click it, or press `ctrl+.`, and the full-screen task page opens. The column is left exactly
as it was — the page is somewhere you go and come back from, not a state the column enters.

**There is exactly one such line, never two**, and the words on it say what is behind it:

- `ctrl+. earlier` when the project's record holds tasks **this session never ran** —
  work from an earlier conversation, or from a window still open beside this one. This is
  the common case: any directory you have worked in before has one.
- `ctrl+. view more` when the only thing held back is a **folded** family, so the column
  is standing one row for work it is not drawing, and there is no earlier work to promise.

A landed task of this session's, already drawn on the column, does not earn the line: it
would be offering to show you what you are looking at. So a first-ever session in a fresh
directory, with nothing folded and no record behind it, has no door line at all, and that
is not a bug. It is never drawn as a count of nothing.

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
- a row another aforge **window** is running opens the card that says which window has it
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
from the far machine's record. `ctrl+g` closes or restores it exactly as it does for
a local conversation; it never falls back to tasks on the machine holding the screen.

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
if you get that instead, run `aforge engine --stop` on the far machine so the older
process holding your session retires, and connect again.

Opening a queued, running, or landed row is asynchronous. The room opens at once with
`bringing this task's transcript from the other machine…`, then replaces that line with
the bounded end of the task's transcript when it arrives. While work runs, the room reads
that bounded tail on its own beat and `nothing on this page yet — it fills in as the task
works` lasts only until the first block arrives. The calls, results, reasoning, and
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
| what `esc` does | interrupts the running turn | leaves the room. It never interrupts and never stops work |
| how you stop the work | `esc` | `x` over an empty box, which raises the confirmation card |
| the box's own line | the bare `› ` | a tinted segment naming the task, in its state's hue, then `› ` |
| box placeholder | the draft prompt | `Steer this task… (esc: main)`, or `Steer <title>… (esc: main)` where the frame is too narrow for the segment |
| pinned top rows | the tab strip and one thin rule under it; `Chats ▾` at its right end opens the chat picker | the tab strip, then a breadcrumb row (conversation → ancestor tasks → current task) and a quiet facts row under it |
| legend word | the branch, or remote machine | `room · esc/←← main`, and `room · esc your line back` while a history walk is on |
| legend hint | `esc interrupt` while a turn runs | `x stop` while there is work to stop, `↑↓ history` mid-walk, nothing otherwise |
| the model on the status row | the conversation's model | `task <the task's model>` |
| clicking that model | opens the picker and switches the conversation | opens the picker and switches **that task**, from its next turn — and does nothing at all once the task has landed |
| `ctrl+b` | freezes the transcript | freezes the room's own rows |
| scroll position | the conversation's | the room's own, kept separately |
| attachments | the tray sends pictures | a room's box sends words only |
| proposals | drawn as cards | never — a task's own pieces start without asking you |

The focus header is **two pinned rows**, under the tab strip:

```
  │ main ×│ the tree walk    │                                  Chats ▾
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

**And the hint slot under a room asking for your look shortens rather than vanishing.**
At sixty columns `a accept · l look again · n not right` is a few cells too long for what
the foot has left beside `room · esc/←← main`, and the slot used to go empty — so the
narrowest terminal was the one that named none of the keys answering the question it was
standing on. It now reads `a accept · l look again · +1`, and `a accept · +2` narrower
still: the same answers in the same order, with a count of the ones that did not fit. All
three letters keep working whether or not they are printed.

**`ctrl+v` inside a room moves that task's thinking rung**, one step up each press and back
round to `low` from `max`. It is the same chord home uses on the machine's own default and
on a standing item, bound here to the task whose page you are standing in; the keys page
has the whole of it. A worker already running keeps the rung it started with, so the line
aforge writes says `task 7 · thinking · high · its next call takes it`.

**The header has separate click targets.** Ancestor crumbs open their exact task;
the root and the `esc/← main` end return to the conversation. The current crumb does
nothing when pressed — so clicking its name cannot accidentally leave it — though it
still lights under the pointer, so the row never looks broken where you are standing.
`Stop` is on the row underneath and retains its confirmation. Empty header space retains
the shortcut back to main. Below 12 columns,
or on very short terminals, the bar gives its row back to the transcript; `esc` still
leaves.

Actionable waiting work retains its answer row. The parent sentence no longer repeats
the breadcrumb ancestry; a compact `handed out:` row still names children.

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

**A room holds everything the task said and did, and the settled work's outline is one
gesture away.** Out in the main thread a finished turn's machinery collapses into one chip —
`▸ worked 47s · thought 6s · 6 tool calls · ctrl+e` — so the page reads back as the question
you asked and the answer you got. A room does the same thing cut differently: a task's
whole life is one long stretch of work, so folding it by turn would put the entire page
behind one chip. It folds **by phase** instead — the work before each paragraph the task
wrote goes behind its own `▸ worked` chip, and every paragraph stays standing.

`ctrl+e` over an empty box opens the newest chip onto its caption outline, a click opens
any of them, scrolling up at the top of the page opens the one nearest the top, and
`ui.work = open` opens them all. A caption is a short status line per step (about
5 to 10 words; wraps on a narrow window); click or select one to
open its tool rows one level further. **What the task is doing right now keeps its caption
standing**, and neither your instruction, your corrections, any call that failed, any
question it asked you, nor the report at the end folds away.

So a room you walk into — a running task, a task that has landed, a piece of a recursive
task, a harness being designed — reads top to bottom as the story of the work: the
instruction it was given, its prose between calls, a chip over the caption outline behind
each paragraph, anything you steered into it, and the report at the end. Everything the
page folded is still there: one keypress opens the outline and one more opens a caption's
machinery. A caption can be wrong; its tool rows are the truth.

Three bounded things do still hold something back, and every one of them names itself and
opens:

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

## The long brief at the top of a task's page — `▸ …N more lines`, view more, expanding and collapsing the instruction, a task description that fills the whole screen

**A long task description no longer takes over the page. It shows its first three lines,
then a line you can click or press to see the rest.**

The first block on a task's page is the instruction the task was given — the words you
typed after `/task`, or the brief aforge shaped from them, or a spec you pasted in. On a
long one that used to be the whole screen: you walked into a task to watch it work and
were shown the assignment, with the first tool call somewhere below the fold.

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

**Nothing is remembered.** Opening the instruction is a thing you did to the page you are
looking at, not a setting: leave the task and come back and it is folded again. There is
no preference for it and nothing is written to disk.

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

The roster shows children with their parent and state. That tree and the breadcrumbs
use the same parent links; the separate `part of:` sentence no longer repeats
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

Type `@` in the draft and a list drops up with task rows above the file rows. The sections
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
that decision?" — aforge answers all of these by going and reading, not by remembering. It
was never in the room while the task worked, and neither were you.

What happens is two steps and you do not have to ask for either.

1. **It finds the task.** aforge searches the project's whole record — every task this
   project has ever run, this conversation's and every closed conversation's — against the
   words you used, matching titles, ids and outcomes. You need no id and no `@`.
2. **It reads that task's own transcript** — what the task actually did, in its own words.
   Every task writes a journal as it works: a real file on this disk, one line per thing it
   said, called and got back, at a path like
   `~/.aforge/v3/projects/<workspace>/<session id>/tasks/20260819-120133_7.jsonl`. The
   answer comes out of that — the decision the task made and its reasoning for it, the
   commands it ran, the files it touched by full path — and not out of the one-line outcome
   the record keeps.

Pointing at the task with `@its-name` is faster and never required: the pointer block
already carries the transcript address, so aforge follows it instead of searching.

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

- **the strip along the top** keeps one flat row of live chips on narrow frames; it does
  not draw the family tree.
- **the roster** draws the whole family together, with each piece joined to its parent by
  tree connectors and carrying its own id and state.
- **the parent's room** shows the `propose_task` calls as they are made, and the parent's
  own words when the reports come back; its children remain grouped on the roster.
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
question to answer. Neither moves the done-condition, and nothing in aforge guesses: the
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

## What the parts start with — do the parts see the parent's unfinished work, when is the parent's work frozen for its parts, the wip commit before a split

**They start with the parent's files already on disk.** A task that has opened the material
has usually *written* something by the time it decides the work is too wide — a repro, a
failing test, scraped material, a half-drafted section. All of it is there for every part,
without the parent having to describe it in a brief.

That is not automatic; it is a commit. Before the first part is handed out, aforge stages
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
- **It is aforge committing, never the worker.** Tasks are told they never run `git add`,
  and that is still true.
- **Nothing is committed in your own folder.** A task working *in place* — in the directory
  you are sitting in — has no branch of its own, so there is nothing to commit to and
  aforge does not make one. Its parts share the directory, which is what "here" means.

If the family's branch is there and **cannot take that commit** — a disk gone read-only, a
repository somebody broke — **the split is refused** rather than taken on a world the parts
do not have. The worker is told in one line and carries on with the work in its own hands.
A family whose private copy could not be made **at all** is the other case and is not
refused: the split runs, the parts share your folder instead of each getting a copy, and the
task says so in one line.

**The worker is not the only one who can ask.** When a long answer of mine was handed over
because a second model read it and drew its parts, that drawing is put to this same road
before the new task's worker is asked anything — so the task starts already divided rather
than being asked to find parts somebody has already named. Everything below applies to it
without exception: the same tests, the same reading by the mastermind, the same refusals.
The receipt reads the same too, and the worker is told the parts are already running so it
does not do them again. *An answer that runs long is read and moved* is where that happens.

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

## Can I make it always split the work — turning the width floor back on, why did it split my task into parts, AFORGE_SPLITGATE

**Why did it split my task into parts?** Because the work was read as wide, a worker asked
to hand its parts out, a lane was free, and — by default — nothing else stood in the way.
The parts are listed in the task column under their parent and each one says what it owns.

**The width floor is off by default. `AFORGE_SPLITGATE` in the environment turns it back
on**, and it is the only way to; there is no setting for it, because it picks how the
machine decides rather than anything you have a preference about.

- **unset** — off. Every division that is asked for is kept. This is what you have.
- **`AFORGE_SPLITGATE=1`** — the floor as it worked before September 2026: the work has to
  name at least six separate things or the split is refused, free, on the spot.
- **`AFORGE_SPLITGATE=judgment`** — asks the plan instead of your words. If every part is
  already the size of one sitting and none of them waits on another, the parts stand
  whatever your brief counted; where the plan has no opinion, the six-item count decides.
- **`AFORGE_SPLITGATE=0`** — off, spelled out. The same as leaving it alone.
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
the name the work was given: `aforge models` is where those rows show up. When a part is
about to be handed out as ordinary work, aforge looks that record up first. If work
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

**You may have been warned it could happen.** A `/task <brief>` whose sizing call found more
than one job in your words writes one dim line before the work starts —
`the work looks wide · one worker starts, and it can split as it goes` — and that line is
what this section is about. It promises nothing: the tests below still have to pass.

**And this is what I do with wide work too.** When I hand work off myself rather than you
typing `/task`, `propose_task` carries a `wide` flag, and I set it whenever I judged the
work broad — a sweep across many files, research across many sources, the same change over
many separate items. It still starts **one** task, armed to split itself; it is not a
planner and not three tasks. **There is no planner on my belt at all any more**, and there
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

**Where you see it:** the parts appear in the task column under their parent, joined by tree
connectors and carrying their own id and state, exactly as pieces handed out from the brief
do. Walk into the parent's room and its header lists each part by name with the state it is
in; walk into a part and its breadcrumb trail names the parent.

**Stopping.** Stop the parent and its unfinished parts stop with it, their branches kept.

It is on by default and there is no setting for it. `AFORGE_SWARM=0` in the environment
turns the whole road off — no task splits at all — and `AFORGE_SPLITGATE` decides whether a
width floor stands under the splits that do happen, which is off unless you set it
(*Can I make it always split the work*). Both are environment pins rather than preferences,
which is why neither is in the settings panel.

## Hands — several parts of one answer worked at the same time, inside the reply you are waiting on

Sometimes the work is not big enough to hand away and still has separate parts in it. Three
files to change that do not touch each other. A page to write and a table to fill in beside
it. For that, aforge can **copy itself, right there in the middle of your answer**, into two,
three or four **hands** that work side by side. Its tool for it is `fork`.

A hand is not a task and it is not a part of one. Nobody writes a brief for it: it starts
with **everything the answer has already read and said**, the whole conversation up to that
moment, and is told exactly one line — what its part is, which files it may write, and what
each of the others is doing so it does not redo their work. That is the whole trick, and it
is why hands are cheap: the expensive thing about handing work over is explaining it, and a
hand needs no explaining.

**What you see** is one dim line at the moment they go out:

```
three hands on it · each one folds in as it lands
```

The answer then carries on. It does **not** go quiet and wait for all of them — see the next
section. What they cost is folded into that turn's own cost, which is where it belongs: it is
your answer being worked on, not work that left.

Do not confuse it with `this one wants more hands · handing it over with everything found so
far`, which is the opposite move — that one is your answer **leaving** to become a task.

## Do hands get around the file limit — my reply changed six files through hands and never became a task, does forking count against the allowance

**No. What a hand changes counts against the same allowance as an edit the reply makes
itself.** The same calls count in both places: an `edit`, `write` or saved `edit_video` cut
under this folder, and a shell command that names what it changes. Reads do not count. A
refused write changed nothing and counts nothing, and neither does anything outside the
folder this conversation is open on.

A hand is a stream, so this count can arrive after the reply that called `fork` has already
ended. It arrives when the hand reports back. The reply at the next step boundary reads it:
that may be the reply already running when the report lands, or the reply the report wakes.
If the allowance has been spent, that reply says
`this is changing more than a quick edit · moving it to a task that is watched and can split`
and moves what remains onto the same one-task road as an inline edit. Each hand's landed call
is counted once.

## A hand is a stream, not a wait — the answer keeps working while its hands are out

`fork` **comes straight back**, naming the hands. Each hand's report then arrives on its own,
in the conversation, the moment that hand finishes — in the order they **come home**, not the
order they were asked for. Nothing polls and nothing waits.

That matters because the alternative was measured and it was expensive. On one benchmark task
a worker forked three hands at 00:53. The first was finished ninety seconds later. The tool
call did not return until 01:33, when the slowest one hit its budget — so for thirty-nine
minutes the worker sat inside a tool call doing nothing at all, while the first hand's
finished work sat in the working copy unbuilt and unmeasured. When it finally returned, the
worker built once, ran the check, and gained 29 passing tests. Forty minutes for work that had
been ready after two.

So now the answer builds and tests **each slice as its report lands**, while the other hands
are still writing elsewhere in the tree.

Every hand still out also rides at the foot of every result the answer reads, the way a
background job does:

```
[job 4] running 12m03s · hand 2 — the docs · last: edit docs/api.md
```

Which hand, how old, what it last did. So a hand can never be forgotten and never has to be
asked about.

**Hands get job ids now.** They are in `jobs list` beside background commands and watches,
each with a log on disk, and `jobs kill 4` ends one — its writes stay in your working copy and
may be half made, and no report comes.

**Every hand owns a slice of the files and can write nowhere else.** They share one working
copy — no branches, no copies of the repository — so what keeps them out of each other's way
is that the slices are declared before any of them starts, and two hands claiming the same
path is refused outright. A hand reaching outside its slice is refused too, by aforge and not
by good manners, and it carries on inside its own.

**A hand that declares an empty slice only reads.** `"scope": []` asks for a hand with no
`edit` and no `write` tool. Its shell keeps the existing restricted orientation policy;
this is a tool policy, not an operating-system sandbox. It can inspect sources, datasets
or files without declaring a file to change. Readers claim nothing, so **two of them may look at the same
file** and neither collides with a writing hand beside it. A fork can mix them freely.

**The `scope` key is always required.** An empty list is a request; a missing key is a slip,
and it is refused rather than read as one — the reply is told to name the paths, or to send
`[]` if the hand only reads.

## How a hand's slice of files is spelled, and when a fork is refused over it

The paths a hand may write are **relative to your working copy** — `src/parser.rs`,
`internal/session`, `docs`. A directory claims everything under it.

A path written out **in full** is accepted and means the same thing: if your working copy is
`/work/repo`, then `/work/repo/src/parser.rs` and `src/parser.rs` are one slice, and the
overlap check reads them as one. This is worth knowing because it used to be the opposite. A
task worker once declared its hands' slices in full, the door accepted them, the refusal was
made against the short form, and **every single write in every hand was refused** — the model
kept being told that files plainly inside its slice were outside it, and it never split its
work again. Both ends now read a path the same way, once.

Three spellings are turned away at the call, before any hand starts, with a line naming the
offending path and the form that would have worked:

- a path **outside your working copy**, like `/etc` or `../secrets` — a slice is a slice of
  this directory, and one that is not could never match anything a hand writes;
- **`.`**, the whole working copy — that is not a slice of it, and it collides with every
  sibling;
- **two hands claiming one path**, in any spelling — the one shape a shared working copy
  cannot survive.

Nothing is spawned in any of those cases: the answer reads the refusal, redraws the slices
and calls again.

## What hands cannot do, and how they differ from a task

**They cannot build and they cannot run tests.** All of them are writing the same working
copy at once, so a build in the middle of that reads a half-written repository: a pass would
prove nothing and a failure would be a neighbour's unfinished work. Their `bash` runs
`git diff`, `git log`, `git status`, `git show`, `pwd`, `wc`, `head` and `cat` and refuses
everything else. The build, the tests and the review happen **in the answer itself**, hand by
hand as each report lands.

**They cannot write outside their part.** An `edit` or `write` aimed anywhere but that hand's
declared files comes back refused, naming the files it does own. So a fork cannot leave your
repository in a state two of them fought over. A hand that declared `[]` does not carry those
two tools in the first place. They also cannot get around the reply's write
allowance: what they change spends the same allowance when their reports come home.

**They cannot fork again.** One level, and it is not a rule they are asked to keep — a hand
simply does not have the tool.

**A hand has nine tools, and it is told which nine.** It opens on the whole conversation —
the same transcript and the same instructions the answer that forked it was reading, word
for word, because that shared page is what makes a copy of a mind cheap to make. Those
instructions were written for the belt the **caller** carries, which is a much longer list,
so a short note is added under them naming what is actually this reader's: `read`, `grep`,
`find`, `ls`, `read_document`, `manual`, `edit`, `write` and `bash`. A hand that only reads
gets seven of those — `edit` and `write` are absent — and its note says so. Nothing else is on
a hand's belt however the page above the note reads, and a call for anything else is answered
`Unknown tool` rather than run.

**They outlive the turn, and your interrupt ends them.** A hand keeps working after the reply
that started it has finished, and its report wakes the session when it lands — the same thing
a background command's exit does. What stops a hand where it stands is **your interrupt**
(that is the difference from a background job: a job is a command you asked to be left
running, a hand is the answer itself) and closing the window. A task that forked hands does
**not** land while a hand is still out: it waits, reads the reports, and lands after.

**Each has a budget of 15 rounds of tool calls.** A hand that runs out reports it **leading**
with `OUT OF ROUNDS`, names the files it wrote, and **quotes its last sentence back verbatim**
— because that sentence is the only description in existence of the change it was halfway
through. Those files are called unverified: nothing built or ran them and the change may be
half made. The answer is told plainly that this part is not done — it is never quietly treated
as finished.

**How it differs from the other two roads.** A **task** is work that leaves: its own copy of
the repository, its own room, a check, a landing, and it survives you closing the window. A
**divided task** is that again, several workers under one, for material too wide for one
worker. **Hands** are neither — they are one answer being worked on in parallel and finished
in the same breath. If the work should still exist after this reply, it wants a task; if it
is this reply, it wants hands.

## How deep tasks nest, and how many pieces one task may hand out

Two hard bounds, and they behave differently on purpose.

**Depth: two levels.** The conversation proposes a task; that task may propose pieces; a
piece may not. The tool is simply not on a second-level task's belt — it does not have the
verb, so it cannot try and be told no.

**Fan-out: five pieces per task**, counting both ways a task hands work out — parts it saw in
its brief and parts it found once it opened the material. A task that asks for a sixth gets
its call answered with:

> no: you have already handed out 5 pieces of this work, which is as many as one task may.
> Do the rest in your own hands, or finish these and report what is left undone.

It reads that as an instruction and does the rest itself.

Neither bound is a setting. They are there because the third level and the sixth piece cost
more than they save: every piece pays for its own working copy, its own check and
its own wait, so past a few of them fanning out is slower than working. A task is told the
same thing in its own words — split only what is genuinely independent, and never shard
work that fits in its own hands.

`task.parallel` still applies to the whole session: pieces queue behind it exactly as
top-level tasks do.

## How many tasks run at once — can I have it do two things at the same time

**There is no limit by default.** aforge does not cap the number of tasks running at the
same time.

The setting `task.parallel` exists for anyone who wants a number anyway — settings panel
(`ctrl+,` or `/settings`), category "spending". Blank means no limit. A cap is a queue and
never a refusal: work past the cap waits and starts when a slot frees, and while it waits
its roster row reads `waiting · slot`; a parked-only family starts folded.

What actually runs out is the machine, not a count of tasks. Two real ceilings hold new
starts instead:

- `task.max_load` — the one-minute load average divided by core count, default **1.5** per
  core. At or above it, nothing new starts and a held task's row reads
  `waiting · machine busy`.
- `task.min_free_mb` — a floor under available memory, default **1536** MiB. Below it,
  nothing new starts.

Both gate starts only. Nothing already running is ever touched; the pressure drains as
running work finishes, and the check is re-asked every 5 seconds.

**The honest caveat:** these two governors read `/proc/loadavg` and `/proc/meminfo`, so
they only apply on a machine that has them. Where there is no `/proc` — macOS, Windows —
the governor cannot say anything and therefore never holds. On those machines
`task.max_load` and `task.min_free_mb` do nothing at all.

Separately, a task that is already running can be held by the provider's own pacing. Its
row reads `waiting · rate limited` until the calls get through — or until the task's
patience runs out, which is 60 attempts or 10 minutes of waiting, whichever comes first;
your own turn gives up sooner, at 6 attempts or 2 minutes. See how-tasks-run.

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

**A few matches — a shortlist on the card.** Two to four candidates become the models row.
It is a correction, not a gate: the countdown is already running on the closest match,
which is chip 1, and that is what silence takes. Click a chip or press its digit `1`–`4`.
Picking a model answers nothing — the question is still whether the work goes at all. Only
a chip on the row can win. Chips are spelled with the part after the vendor unless two
vendors share a tail, in which case all of them keep their full id; a chip that does not fit
is dropped rather than cut, and a row that would show one chip is not drawn at all.

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

- **It takes effect on the task's next turn.** The call the worker is in the middle of
  finishes on the model it started on — killing a request in flight would throw away work
  you have already paid and waited for — and everything after it is on the new model.
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
  and the change survives a restart.

The picker offers the same rows `/model` offers, and it opens with the cursor on the model
the task is already running — so `enter` confirms rather than changes. `esc` leaves
everything as it was.

There is still no command, key or setting for this: the model's name in the room is the
only door. `/model` always means the conversation.

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

**`x` stops it, and it asks first.** Press `x` with the roster's cursor on the task, or
inside the task's room, over an empty message box. One card comes up:

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

**There is one other way to stop a task, and it asks no card.** On the **tasks** place
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
  files, exactly as it does for every other early ending.
- **what it spent is what it spent.** The figure freezes where it was.
- **it is not a failure.** The roster draws `⊘` rather than the failure cross, the room's
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

**You can still ask in words instead** — "stop task 7" — and the model has the door
through its `tasks` tool. The key is faster and does not spend a turn.

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
main · [esc] cancel`: no revive, because the work is not over and starting it again would
make a duplicate. Wait for the check to land, or send the thought to main.

Whenever a task stops for any reason it wears `stopped — branch kept` and its branch name.
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

## Why is the task waiting for me — finished but needs your look, a sub-task needs my look, a nested task waiting on me

Some work lands with `needs your look`: it finished, but nobody could say whether it holds —
or it finished and held, and one of the files it wrote was changed by other work while it
was running — or it finished and held and its branch would not merge cleanly, because the
same file changed on both sides. The how-tasks-run page covers the last two on their own.
It is neither done nor failed. Nothing has merged, the branch is kept, and anything waiting
on it stays waiting until somebody decides. Its family rises to the top of the roster, and
its row reads `finished — look it over`.

Read it first. Its room holds the whole of it, and its landing card expands to the changed
files, the branch, the model, the cost, the done-condition and the report.

**A nested task asks the same way — a sub-task needs my look, a piece of a bigger task
nobody checked.** Depth changes nothing about whether you are asked: the card, the roster
row and the sub-task's own room all offer the four answers from the moment it lands. What
depth changes is how LOUD it is. While the task above it is still running, the sub-task
**folds** under its family head, because that task's own agent is the one being asked and
has the diff to read; when the head settles, one line says the question changed hands
(`task 4 has finished, and the piece of work it handed out that nobody could check — task
6, Port the parser — is now waiting on you rather than on it.`). It is never filed under
`done`. A sub-task like that used to draw no answers row anywhere at all, so a nested
question could sit through a whole run with nobody able to see it.

**It also stands on home**, in the `needs you` strip, named after the task and saying
`landed` and how long it has been waiting — from any project, in any conversation, whether
or not that conversation is open. Pressing that row opens the conversation that ran the
work **with the task's own record card in front of it**, so you land on the thing you
pressed rather than at the live edge of the transcript. It stays on the strip for as long
as it takes: nothing ages it out, and only your decision moves it.

The landing card then asks, in as many words, and offers the answers under it:

```
finished, but nobody has checked it — your call
[a] accept · [l] look again · [n] not right · [d] decide these for me
```

Every one of the four is a key **and** a click. The keys work on the **selected** card — walk
to it with `↑`/`↓` — and only over an **empty** message box, exactly like `x`: a letter typed
into a sentence stays a letter. Clicking a choice presses it; clicking anywhere else on that
row does nothing rather than expanding the card under your hand.

**On a narrow terminal the row drops `[d]` and says so.** The three answers are the
question and the fourth is a preference, so the preference is what goes first — and the
row then ends in a dim `· +1`, the same count every other fold on this surface draws
(`▸ +1`, `holds 3 more`). `[d]` still works unprinted. It reads:

```
[a] accept · [l] look again · [n] not right · +1
```

**The task's room asks the same question** at the foot of its page, and in there the four
keys need no selection — see *How do I approve a task* below. Room and card are one
question: answer in either and both show the receipt.

## What accept, look again and not right each do

- **`[a] accept`** — you looked and you are taking the work. Its branch follows the same
  landing as checked work: it merges into an ordinary checked-out branch, or is kept off a
  protected, moved or detached checkout. Everything queued behind it unblocks. The report leads
  `you looked at this yourself and took it as done`. If that merge conflicts nothing is
  forced: your checkout is left exactly as it was, the branch is kept, and the task stays
  waiting on you with the clashing files named.
- **`[l] look again`** — a fresh check runs against the same working copy. It answers on its
  own, minutes later, and until it does the task **still** needs a look: what goes away is
  the choices, not the state. If `check task work` is off there is no checker to ask, so
  this answer cannot be taken; the card keeps its choices and says
  `that one could not be taken — try another`. The same line appears for any answer that
  could not be spent — a working copy that has gone, for instance.
- **`[n] not right`** — you looked and it is not finished. The task becomes `incomplete`,
  its branch is kept, and its previous report is kept under the refusal. Its dependents do
  not advance and still land `failed`, as before, saying the work they waited on did not
  finish. The report leads `incomplete — you looked at this yourself and said so`.
- **`[d] decide these for me`** — the escape hatch, described in the next section.

**Answered means the choices are gone, not greyed.** The two rows are replaced by one dim
line saying what you did: `you took this as done`, `sent back to be checked again`,
`you said it is not finished`, or `already answered` when somebody got there first — the
model's own settling, a re-check that finally answered, another window.

The card's own head is **not** rewritten — it is the record of how the work came home, kept
branch and all. What follows is: the task re-settles into `done` or `incomplete`, a state it
has not been in, so a **second** landing card is drawn saying what became of the work. The
transcript then reads as what happened: this landed needing a look → you took it as done →
`task 7 done · merged`.

**You can also just say so.** "accept task 7", "that one isn't finished", "have another look
at task 7" all work: aforge holds the same door through its `tasks` tool, and whichever of
the two is used first wins. The other finds the question already gone and says `already
answered` rather than raising an error.

The `need you` footer count covers only work that will not move without you: a landing
nobody could judge, and finished work still sitting on a branch that never came home.
Work that ran in your own tree, or that ended before there was a branch, is not
undelivered — it is over, and its settled family starts folded.

## Can aforge decide on its own — stop asking me about tasks that need a look

Yes. The setting is **`task.settle`**, in `/settings` under Session as
`who settles work that needs a look`, and it takes two words:

| Value | What happens when a task lands needing a look |
| --- | --- |
| `ask` | **the default** — you decide. The card offers the four choices above, and aforge says what it thinks and leaves the choice with you |
| `auto` | aforge decides. It is told to read the report and the work itself — the transcript, the diff on the branch — and settle the task, and to come back to you only when it genuinely cannot tell |

Under `auto` the card draws **no** choices while aforge is deciding; it shows the outcome
once the task re-settles, like any other landing. Which of the two a card follows is fixed
when it lands, so changing the row does not reach back and take the choices off a card that
was already asking.

**`[d] decide these for me` is the same switch, pressed where the annoyance is.** It flips
`task.settle` to `auto` for good **and** hands the card you are looking at to aforge on the
way past — it does not settle it for you, it asks aforge to. The card then reads
`handed to the chat — it decides these from now on · saved`, and the ` · saved` is only there
when the preference actually reached your profile.

Neither value takes anything away. Under `auto` you can still say "actually that one isn't
finished"; under `ask` you can still say "you decide this one". The row changes **who is
asked first**, and nothing else about the landing: either way the task is neither done nor
failed until somebody answers, its branch is kept, and anything waiting on it waits.

To undo it, set the row back to `ask` in `/settings`, or say so — "ask me about these again".

**A run with nobody watching reads as `auto` whatever the row says.** `aforge --once` and
the other headless doors have no card to press, no `/settings` to open and nobody to read a
landing that says it is waiting on somebody, so a task that needs a look there would stop
the run until the wall clock ran out — which was measured happening on a ten-hour run. In
those sessions aforge takes the decision itself, by the same road `[d]` takes, with the same
escape to say it cannot tell. This never applies to a session you are sitting in front of:
there your row stands, and a blank row still means aforge asks you.

## Needs your look on a run I left going with --yolo — a check that could not run, and what taken as it stands means

On a headless run with a budget — `--once --yolo` **and** `--max-hours` or `--max-cost` —
there is nobody to put a card in front of, so a task nobody could check is not put to
anybody. The check is asked twice first: one checker, then a **fresh** one with the same
evidence and not a word about what the last one said. Only when the second try says nothing
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
asked again inside what is left. When the second call answers, the landing carries
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
- **The same file changed on both sides** — a real merge conflict — is yours to decide, so
  the task goes back to `needs your look` with the clashing files named. Sort the file out
  and accept it again.

The how-tasks-run page has the sentences each of those lands with, under *My task could not
save what it wrote*.

## How do I approve a task — accept a finished task from the room, the card, or by saying so

A task that landed `needs your look` (`finished — look it over` on the roster) is approved
by **accepting** it, and there are three doors onto the same decision. Use whichever is in
front of you:

| Where you are | What to do |
| --- | --- |
| **inside the task's room** (enter on the roster row, or click its landing card) | press `a` over an empty message box — no selection needed, the room is the task. `l` looks again, `n` says it is not right, `d` hands these to aforge from now on. The same four are chips at the foot of the page |
| **at the landing card** in the conversation | walk to the card with `↑`/`↓` so it is selected, then the same four keys — or click a chip on its answers row |
| **anywhere**, typing | say it: "accept task 7", "that one isn't finished", "have another look at task 7" |

Accepting lands the task by the same merge-or-keep rule as checked work and unblocks
everything queued behind it.
Whichever door is used first wins; the other two find the question already gone and show
`already answered` rather than raising an error.

## Task needs my look but there is no button — where the answers are

If a landed task is asking and you cannot see anything to press, you are on a row that
only reports the state: the roster's `finished — look it over`, home's `needs you` strip,
or the card's own head. The answers are in exactly two places on screen:

- **the foot of the task's room** — `finished, but nobody has checked it — your call` and
  `[a] accept · [l] look again · [n] not right · [d] decide these for me`. Enter on the
  roster row opens the room; the answers are at the bottom of the page and the hint slot
  under the message box names the three keys;
- **the landing card in the conversation**, under the outcome line, once the card is
  selected.

Both need an **empty** message box: the letters are held to the same rule `x` is, so a
letter typed into a sentence stays a letter. If neither place shows the rows, the task is
under `task.settle = auto` and aforge is deciding it — say "you decide" or "ask me about
these again" to change who is asked.

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
  answer at all. Each of them ends drawn grey with `⊘` and the word `stopped`.
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

## The tasks place — grouped work, folds, filtering and time-window keys, my cursor jumped to another task while I was reading

The **tasks** place lists main chats and their nested work across projects, grouped by what you do
next: `needs your look`, `running`, `waiting`, `finished today`, then `earlier` — where
`waiting` is admitted work nothing is doing, drawn with no age on it, and `finished today`
is everything that ended today however it ended. The main chat names its project once. Child rows can include
activity, age from the landing time their own records carry, and last their
measured cost. The row's kind is not drawn. Zero or unknown cost is left
blank, and so is an age whose older record never carried that landing time. A section with
nothing in it is absent.

**Related work stays in a tree.** Folds keep a long run readable, and the list scrolls
through everything the time window holds. The window's edge is named once in the page
header or its arrow control.

Type to filter; every section narrows at once, and a section the query empties is not drawn.
**The message box at the foot of this place says `type to filter this list`**, not `say what
you want done` — on the tasks place there is no message to send and every printable key goes
to the filter, so the box says what typing into it actually does. Every other place keeps the
shared prompt. `↑` and `↓` move among conversation and task rows and skip the head sentence, the blank lines
and the section words. `enter` on a main chat opens that conversation. On a task it opens its **room** when this conversation
is holding, and otherwise goes **inside** it — the record card. Rows another window is running
take the cursor too, and what `enter` does with one is *Opening a task another window is
running*, below.

**The cursor stays on the row it is on while the list re-files itself.** The page re-groups
every few seconds, and a task finishing moves out of `running` and into `finished today` —
which shifts every row below it. The cursor is remembered by the conversation-and-number pair
that identifies the work, not by which line it was on, so a task landing while you are
reading cannot move what `enter` is about to open. If the row you were on leaves the page
entirely, the cursor parks on the nearest row that is still there.

`shift+←` and `shift+→` move the time window backward and forward. `shift+↑` zooms from
days to weeks and then months; `shift+↓` zooms back in. The window is re-grouped from the
reading already in hand — nothing goes back to disk for it. On a phone the bottom line
remains the pressable `‹ back` bar. An empty place teaches what tasks are instead of drawing
empty headings, and says no count beside that prose.

## The foot of the tasks place, and the one verb on its row strip

The last line of the tasks place is assembled from the clauses that are **true of the row
under the cursor**, and never from a fixed sentence. Over a task this window is running it
reads

```
enter open its room · → verbs: stop it · alt+. map · tab next place
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
- `esc clear the filter`, **only while a filter is on**, because that is the key whose
  meaning just moved.

**The filter is not named on this line.** It used to be — `type to filter` sat here to
correct the message box two rows below, which was saying `say what you want done` over a slot
that could only ever narrow the list. The box says the true sentence itself now, so repeating
it on the foot would be one screen naming one thing twice.

**`→` opens the row's verbs, and the tasks place has exactly one: `s stop it`.** It is
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

**`continue` re-arms the same task.** There is still no `run it again` key on this place —
a row here is an account of work that happened — but a failed or finished task is
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
It cannot be combined with `say`, `continue`, or `resolve`. A result arriving by itself
does not authorize forwarding an old message of yours.

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

If the owner refuses the connection or answers for a different conversation, aforge
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

The main chat's top bar names the conversation. Its `▾` opens the same picker as
Ctrl+k without changing chats until you choose. Inside a task it becomes a trail:
`Shipping the parser ▸ Fix validation ▸ Add boundary checks`. Click the conversation
name to return to the main chat, or an ancestor to open that task. The current task is
inert. Narrow frames fold ancestors into `…`; clicking it opens the nearest ancestor
it hides. A guest task names its owning conversation and ancestry, but those crumbs
are inert because this window does not own that graph.

Model and conversation statistics remain in the status row. Deep trails fit at most
eight explicit ancestor levels; earlier ancestors are represented by a fold. The bar
stays pinned while you scroll. Task drafts, scroll anchors and expanded sections stay
with their task when navigating within this process, subject to the reading cache limits.

## Switching chats through the engine — duplicate names, one open conversation, and saved history

The ordinary engine-backed chat, `--host`, and `--at` currently select one conversation
per connection. Opening another closes the previous session on that connection; its
saved history remains available. The switcher lists saved chats directly rather than
pretending the shared connection represents several independently running chats. It
must not show the new chat under the previous chat's name or stop the newly selected
chat a second time.

The entry line says `closed · <previous chat> — a connection holds one conversation at a time`.
Use `aforge chat --no-host` for independent local conversations that remain open while
you switch between them. That in-process mode does not keep working after its terminal
exits. Persistent multiple-conversation switching on one engine connection is not yet
supported. Merely browsing Ctrl+k does not select or close anything.


## Accepting a saved task after its Git registration was released

If aforge released the task's Git registration while retaining its files, a later accept
restores that registration before bringing the work home. This is not a folder that was
never a repository. The actual branch name is retained even if the task renamed it.

If the registration cannot be restored, the task still needs your look and names the
saved folder and the cause. Its files remain in place, and acceptance can be retried
after repair. It does not claim a missing branch holds the work. Protected branches such
as main remain untouched; their task branch is kept instead.
