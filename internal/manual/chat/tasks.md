# Work that runs on its own

## What a task is

A task is one self-contained piece of work handed off to run on its own while the
conversation carries on. It works in its own copy of the repository and reports back when
it lands. It never sees the conversation: what it reads is one written brief — your own
message, word for word, then the work, what to produce and what done means. How that is
assembled is on the *how tasks run* page, under *What the task actually reads*.

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
that must pass, the behaviour that must hold, the output that must appear. The brief and
the done-condition are frozen the moment the work is admitted. Steering can add a missing
fact or correct a step, but it cannot change what the work is for. If the objective itself
was wrong, the answer is to propose the work again.

You can keep working while a task runs. aforge tells you not to wait for it: its report
arrives in the conversation when it lands.

**One other thing on the roster is a task, and it is not work in a worktree.** A sub-harness
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

**If shaping cannot run, your words go as-is.** No model resolved for it, a timeout, an
answer that was not readable — the task starts with exactly your sentence and the plain
done-condition `Complete the brief and report the result and checks run.`, which is what
`/task` did before shaping existed. It is never a reason for your task to be refused, held
up, or lost.

The call is billed the way aforge's other calls-you-did-not-type are: to the session, not to
a turn. It runs on the `shaper` role, which follows the careful-work model.

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
grows: a pulsing `◌`, the title (or just the word `task` until the title arrives), and one
row reading `forming…`. It is not a question yet — no options and no clock. If the turn
ends before the proposal finishes arriving, the block settles as
`cancelled · the proposal never arrived`.

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

## Why a task started on its own — this looked like work, so task N started

**Sometimes work starts without you asking for it, and you are told after.** After a turn
that answered a substantial message in **words alone** — no tool call — a cheap model reads
what you asked and the first two lines of the reply, and decides one thing: should that have
been work?

**A yes is asked twice.** The cheap model only screens — it reads every wordy turn, which is
why it is cheap — and it cannot start anything on its own. Before a task exists, the same
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
  conversation you have just taken back is not interrupted again on the next line.
- **Nothing after a turn that used tools.** A turn that called tools was already work, and
  asking whether work should have been work has no useful answer.
- **Nothing on a short message.** Under six words it is not read at all — "thanks", "run
  the tests", "what does this key do" are answered in words by construction.
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
box, or the ✕ on the pointer. Nothing about it is special from the moment it exists.

**There is no setting that turns this off.** No `/settings` row switches it, and nothing you
type disarms it for the session. What bounds it is the list above, and the stop.

Nodes cut by an adaptive run also appear as rows under the run's own row — see *Adaptive
runs*, which explains what those rows can and cannot do.

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

## Every key the proposal card takes

| key | when | what it does |
| --- | --- | --- |
| `enter` | always | answers the focused option |
| `esc` | always | outright **no** — declines |
| `ctrl+e` | box empty | opens or closes the brief |
| `←` `→` | box empty, picker closed | move the focus between the three options |
| `y` | box empty, redirect not asked for | approve |
| `r` | same | ask for the redirect lane |
| `n` | same | decline |
| `1`–`4` | same | pick that model from the models row |

The card opens with `yes` focused, because that is what the block is proposing and what the
clock will do. `←`/`→` clamp at the ends and never wrap. You can also click any chip.

The letters and digits are given straight back the moment there is a sentence in the box,
or the moment the redirect lane has been asked for. "yes, but keep the tests" starts with a
`y`, and a surface that read that as approval would approve the thing you were in the
middle of correcting. `←`/`→` still work in the redirect lane, because there is no caret to
move in an empty box.

While the card is up, the legend hint reads `y yes · r redirect · n no`. A question the
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

The default window is 5 seconds. It is the setting `task.autoapprove_seconds`, under
`task.` in the "spending" category of the settings panel (`ctrl+,` or `/settings`).

**This is one of the rows aforge will not change for you.** It decides how long you get
before work starts on its own, so `change_setting` refuses it and points you back at
`/settings`. Same for `task.parallel` below, and for the whole approval and spending
family — the permissions page lists them.

Set that window to 0 and there is no clock at all: no bar is drawn and the row reads
`waiting on you`. The card then waits until you answer it, however long that takes.

## What yes, redirect and no each do

**yes** admits the work exactly as briefed.

**redirect** with an empty box does not answer — it takes the focus and waits for your
words. The `enter` after it carries the sentence. Your words travel verbatim and are
appended to the brief; this is the last moment the brief may change. With something already
typed, `yes` and `redirect` converge: a correction in the box is a correction whichever one
you reached for. The box is cleared on any answer, so your next `enter` does not send the
correction to the model as a message.

**no** (or `esc`) declines. Nothing is spawned, no row appears on the roster, and no room
exists. This is a normal answer, not an error.

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
| the proposal is still arriving | `forming…` |
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
| landed short | `failed` |
| landed, but nobody could judge it | `needs your look` |
| …the same thing on the roster | `finished — look it over` |
| …the same thing when there was no report | `finished, but needs your look` |
| …its branch, on the card | `branch kept` |

`finishing` is not a separate state — the work is still running, and the sentence after the
word names the gap it is tying off.

The three reasons a queued task gives for waiting are `slot`, `machine busy` and
`rate limited`. A named prerequisite outranks any of them, because a name is something you
can act on and a queue clears itself.

How a branch came home is spelled `merged`, `conflicted`, or `inplace` (the work ran
directly in your own tree because there was no repository to branch from).

## The three ways work lands

Every landing writes a card into the conversation, with a blank row on each side, and moves
the task's row on the roster.

```
✓ ◆ Fix nil-map crash · done 4m12s · 3 files
  "the guard is in and the regression test passes" · spawned 14:02 · ctrl+o output
```

The head is what happened. The muted line under it is what came of it, in the task's own
first sentence, quoted because they are its words and not aforge's.

- **`done`** — a tick, muted. It is settled work on the roster.
- **`failed`** — a cross, in the bad hue, drawn with the word `failed`. It is settled too,
  not work that needs you: by the time you see the word it is news, not a decision.
- **`needs your look`** — a `?` in the warn hue. Its family rises above running work on
  the roster. The `?` is deliberately neither a tick nor a cross: it claims neither a
  finding nor a judgement nobody made. This card carries two more rows —
  `finished, but nobody has checked it — your call` and the choices under it — unless you
  have set `task.settle` to `auto`, in which case aforge is deciding and the card is quiet.

After the name the card carries the span, the file count, and how the branch came home:
`merged`, `inplace`, `conflicted · <branch>`, `stopped — branch kept · <branch>`, or
`branch kept · <branch>`.

Click anywhere on the card, or press `ctrl+o` with it selected, to expand it: `changed`,
`worktree`, `model`, `cost`, `ran`, `done when`, the report, then the brief. `enter` on the
selected card opens the task's room instead. Each long field caps at 20 rows.

More than two landings in a row become one rollup — `✓ 3 tasks done · 9m14s` with a compact
row per task under it. Any failure in the batch swaps the header to `✗ N tasks landed`; any
`needs your look` swaps it to `? N tasks landed`. The header's span is wall-clock, first
spawn to last landing, not the sum of the parts, because tasks run at the same time.

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
reading `tasks`. (Under it the same column carries a second section labelled `standing` —
the orders standing over this conversation. The standing orders page has that half.)

The roster holds every task **this conversation** has admitted, not just the live ones —
and, beside them, every **background job** this conversation started, because work shows on
the right whatever door started it (see *Background jobs on the column* below). Tasks
*other* sessions ran are not on it; the
page `/history` opens is the one that has them, and one dim line at the foot of the column,
`ctrl+. earlier`, is the door onto it. Work finishing never puts the column away, and
neither does `/new` — that takes this session's tasks with it and leaves the column
standing, with the door onto the project's record still at its foot. One thing closes it:
`ctrl+g`, which takes the column off the frame and leaves the work exactly where it was.
The bottom line of the column says so.

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

Closed with `ctrl+g`, the column leaves a two-column edge at the right of the frame that
opens it again on a click — see *The task bar disappeared* below.

The roster is a forest. Each root task is followed by its whole family, with children
joined by three-cell connectors (`├─ `, `└─ `, `│  `). Families are ordered by their most
urgent member: needs you, running, idle, parked, then done. There are no state-group
headings. The footer keeps those totals as counts, such as
`2 need you · 3 running · 12 done`.

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

A task's row opens with two glyphs answering two questions: its state, which changes, and
its own identity mark, which never does. Then the name, then its id as `#7`, dim, at the far
end — and the id stands down when the name would be left under 12 cells.

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

Under those, always, one more line: `❯ ctrl+g hide`. It is the column's own door, and it
is a button as well as a key — click that line and the column goes away. The `❯` is in ink
and the words are dim, because the chevron is what the pointer presses and the words are
what the keyboard reads.

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

## Background jobs on the column — why a long command shows on the right, the row for a server, build or watch

**A background job gets a row on the roster, the same column your tasks are on.** It is
there from the moment the job starts until the moment it ends. This covers everything the
`jobs` tool can list except a task's own worker, which already has a row of its own:

- a command run with `bash background:true` — a server, a long build, a sweep
- a foreground command that ran past its bound and became a job (`still running as job 3`),
  including one you sent there yourself with `ctrl+g`
- a `watch`, whose row reads `watch <name>`
- a video render, which is a job while it renders

The row's name is the command itself — `npm run dev` — or the watch's or render's own
label where it has one, cut to three words like every other name on this column. Under it,
one dim line: `job 3 · log /path/to/.aforge-v3/jobs/3.log`, cut to the column's width. That
line is the whole handle back to the work: the number is what `jobs output 3` and `jobs kill
3` take, and the path is a file you can open yourself.

**It settles where it stands.** A job that exits cleanly reads as finished and drops into
the column's done fold; a job that exits non-zero, and a job that was killed, read as
incomplete and lead that fold. Nothing else happens: **no card is written into the
conversation** when a job ends, because a job has no report anybody wrote — what it left is
its log. Aforge itself is still told, on its own side, at the next step boundary
(`job 3 exited 1: make: *** [build] Error 1`).

**A running job counts as working.** It is in the `tasks · 4 working` tail on the section
label, and in the `3 running` line in the footer, exactly as a task worker is.

Three things a job's row deliberately does **not** have, because a job has none of them:

- **no room.** `→` on the row opens nothing — there is no agent inside a job and no
  transcript to read. The log file named under the row is where you read it.
- **no `✕` and no stop key.** `x` does not aim at a job row. Ask, and aforge kills it with
  `jobs kill`; every running job is also killed when the conversation closes.
- **no branch, no changed-files list and no price.** A job runs in the workspace itself,
  and nothing measures what it costs, because it costs nothing but time.

**Zero jobs draws nothing at all** — no label, no empty row, no "0 jobs". A conversation
that has started none looks exactly as it did before jobs had rows.

**The rows come back; the jobs do not.** Switching away to home or another conversation and
coming back redraws every job this conversation started, live ones in the state they are in
right now. Reopening the conversation tomorrow redraws them as **history**: a job that
exited comes back as it ended, and one still going when aforge closed comes back stopped,
reading

```
it ended when aforge closed; its log is kept
```

A job is a child of the aforge process and dies with it, so **nothing is restarted** — no
restored row is running, none of them is counted on the status line, and none can be
stopped, because there is nothing left to stop. The log file it was writing is still under
`<workspace>/.aforge-v3/jobs/`.

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

**On a project that has never run a task it says `no tasks yet — /task <brief> starts one`**
and opens nothing, which is the same refusal `/history` gives there.

**The forms that start work are unchanged.** `/task <brief>` and `/task solo <brief>`
still size, shape and start the work directly, with no proposal card in between and no
extra question. There is no third form: `/task adaptive` is retired.

**There is still no `/tasks` command.** The plural is not a word this surface answers to;
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
  task page opens with every task the project has ever run on it — running work at the top,
  a flat `earlier` list under it, newest first. `/history` is the same page.
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

**Reopening a conversation re-draws its own tasks.** `/resume`, `aforge resume`, and
switching back behind home all rebuild the column from the record: every task admitted
here comes back as a row in its family — finished work included, and work that was
interrupted comes back saying so on its card. Tasks are not lost when the terminal
closes; the column is rebuilt, not carried.

**Only this window's own work comes back.** Everything else stays behind the
`ctrl+. earlier` door, exactly as the section above says, and a task is never drawn
twice — a row on the column is not also an `earlier` row.

## Hiding the task column: closing the right sidebar, panel or task bar

`ctrl+g` closes the column of tasks on the right and gives its columns back to the
conversation. Press it again and the column comes back with the current state of the
work in it, including anything that started or finished while it was gone — nothing here
is a snapshot; the column is redrawn from the tasks every frame.

**The pointer can do the whole cycle on its own.** The last line of the column reads
`❯ ctrl+g hide` with the chevron in ink: click it and the column closes. What is left
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

`ctrl+g` works whether or not the session has tasks — the column stands empty, so an
empty column is still a column to close. It does nothing, and is not swallowed, only when there is no roster on the frame at
all: a frame under 100 columns where nothing has raised the roster over the body. On
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
not just the handle, so there is nothing to aim at. It is the same act as `ctrl+g`, which
still works and is still the key.

- Under the pointer the handle brightens further and the whole two-cell strip takes a
  background, which is how everything pressable on this screen says so.
- **The chevron points the way the column goes**, and it is the same control in its other
  state: `❮` while the column is away, `❯` on the `❯ ctrl+g hide` line while it stands.
  Clicking one gives you the other, so the pointer goes round the full cycle. On a terminal
  that cannot draw them they are `<` and `>`.
- **One cell above the handle says what the work is doing**, while there is anything worth
  saying: `▲` in the question colour when a task is waiting on you, `●` in the accent when
  something is running. Nothing at all otherwise — a session with nothing running and
  nothing waiting leaves the edge silent, and so does one that has run no work.
- The edge costs the conversation two columns, exactly as the column it stands for costs it
  its own width. The text re-wraps; nothing is ever drawn underneath it.
- **On a frame narrower than 100 columns there is no edge**, because there is no column at
  that width to bring back. The roster still opens over the whole frame with `ctrl+t`.
- The keyboard is unchanged. The edge is for the hand that does not type chords; `ctrl+g`
  is for the one that does.

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
| `enter` | open the task's room |
| `w` | toggle the wider 46-column tree |
| `esc` or `ctrl+t` | give the keyboard back |
| `ctrl+g` | close the column altogether, or bring it back — this one works whether or not the roster holds the keyboard |

The legend hint while it holds the keyboard is `↑↓ move · →← fold · enter open · esc`.
When depth has forced a title to be cut, the footer adds `w · click seam — widen` (or
`w · click seam — narrow` once it is wide); that hint is clickable as well as available
from the keyboard, and so is the `❯ ctrl+g hide` line under it.

Every other key is given back. The roster cannot take the keyboard while the exit
confirmation, a permission question, a task proposal, or any overlay is up, and with no
tasks of **this conversation's** on the column, `ctrl+t` falls through rather than being
swallowed — including in a directory whose earlier sessions ran plenty. There are no rows
down there to put a cursor on; the door `ctrl+. earlier` at the foot of the column is how
that work is reached.

**The walk stops at this conversation's last task.** `↓` clamps there rather than carrying
on into the project's record, and `enter` on any row of this column opens that task's
**room**. It used to walk into six dulled `earlier` rows below, which meant holding `↓` took
you out of this conversation's work and into another one's; old work is walked on the task
page now (`ctrl+.`), where `enter` goes inside its card.

The cursor follows the task, not the row, when families reorder or fold around it.

With the pointer, a row takes the hover background step. On a family root, only hovering
the glyph cell reveals its disclosure triangle (`▾` open, `▸` folded). Click that glyph
cell or the root's `▸ +N` badge to toggle the family; click its title to open the room.
A click that hits no task still belongs to the column and does nothing. A click moves the
cursor but does not hand the roster the keyboard.

## What did we do last week — seeing every task: task history, old and past tasks, work from other sessions

`/history`, or `ctrl+.`, opens a full-screen page holding **every task this project has ever
run** — this conversation's and every conversation's before it. It is the answer the roster
cannot give: the column beside the conversation is built from *this session's* work and
nothing else, so a task you ran last week, in a session you have closed, is nowhere on the
screen until you open this.

**There is no `/tasks` command.** `/task <brief>` starts work; `/history` shows what was
started — and so does a **bare `/task`**, which opens this very page rather than printing a
usage line. The page is also reached from the one dim door line at the bottom of the task
column — `ctrl+. earlier`, or `ctrl+. view more` where the column has merely folded a
family away.

The page takes the whole frame, the way the settings panel does. `esc` closes it. Three
pages here take the frame — the settings panel, this one, and `/home` — and **only one of
them is ever up**: opening any one closes the other two.

It has two sections.

- **`running`** — the tree. Every family with anything still going, drawn whole and never
  folded, with the same connectors the column uses. Each row carries more than the column
  has room for: what the task is doing right now, and under it its clock, its tokens, its
  spend and its model. A family's finished members are drawn with it, so the shape makes
  sense. **Under the tree, one flat row for each piece of work every *other* aforge window
  open on this directory has out**, each saying `another window` on the right — see
  *Work running in another aforge window* below.
- **`earlier`** — a flat list, newest first, of everything the project has finished. No
  tree, one line each: a state glyph, the mention mark `⧉`, the title, and how long ago it
  landed. These are the same rows the `@` list offers, so a task looks the same in both
  places. Work already shown under `running` — this session's, or another window's — is not
  repeated here. A row that still claims `running` with no window behind it lands here
  reading `incomplete`.

**The two sections are separated by a blank line**, never by a rule or a divider, and
`earlier` carries that blank line only when `running` is above it — a page holding nothing
but the record opens on its own word.

**The last rows of the page fade** when the record runs on below the bottom of the window:
three rows, each a step fainter, saying there is more under them. The row the cursor is on
never fades wherever it sits, and a record short enough to fit fades nothing at all — see
*Why the bottom rows of a long list look dimmer* on the screen page.

At the bottom: one dim line counting what is on the page, such as `3 running · 148 earlier`
— a section with nothing in it is not counted at all — and under it the keys.

**`/history` on a project that has never run one says `no tasks yet — /task <brief> starts
one`** and opens nothing; a bare `/task` says the same line there. `ctrl+.` there does
nothing at all rather than raising an empty page. A session that has run nothing itself **does** open the page when another window on
the same directory is running something — that is the one fact it was opened to report.

The list is as long as the project's record is — internal to aforge that record keeps the
most recent 2000 tasks — and the page scrolls rather than cutting it.

## Work running in another aforge window — a task started in my other terminal

Two aforge windows open on one directory can see each other's running work, and `/history`
is where they see it.

**A conversation *this* terminal is holding is never one of them.** One terminal can have
several conversations open at once, in this project or others, and every one of them writes
the same file the rows below are read from — so they are filtered out by name before the
page is drawn. Telling you to go to a window that is two keystrokes away in the terminal
you are already sitting in would be the same wrong refusal home used to make about another
project. Those conversations are reached with `tab` or from home; the count of them is on
the status line as `2 open`. Under this session's own `running` tree the page draws **one flat row
for every task each other window has out right now**:

```
running
▸ Sweep the call sites                              another window · Fix the nil-map crash
◌ Port the parser                                   another window
```

- The right-hand note is dim and says `another window`, followed by that window's own name
  when it has settled on one. A window nothing has named says only `another window`.
- The row's glyph is the task's own state, so `▸` is running and `◌` is queued behind
  something.
- **These rows are read, not pressed.** The cursor steps straight over them, `enter` does
  nothing, and there is no mention mark `⧉` on them: a room is a live lane onto a task in
  *this* session's work, and a `@` mention resolves against work that has **landed** — a
  task still running in another window is neither. Go to that window to act on it. On a
  page whose only rows are another window's, the foot reads `esc close · ↑↓ move` rather
  than promising a door.
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

## Searching the task page: type to filter, find an old task by name

**Just type.** On the task page every printable key — letters, digits and the space —
builds a filter, and both sections narrow against it as you go:

```
filter · parser
```

is the dim line above the keys at the foot, so a list that has lost rows never loses them
for a reason you cannot see.

- It matches a task's **title**, its **id** (typed exactly: `7` finds task 7 and nothing
  else), its **name** as the `@` list spells it, and its **outcome**. Letters in order are
  enough — `prsr` finds `Port the parser`.
- **Both sections are filtered**, another window's rows included — those match on their
  **title only**, never on an id, because ids restart with every conversation and `7` typed
  here is a number you read in *this* window. A section with no match is not drawn at all,
  heading and all, so a filter that only matches old work leaves the `earlier` list alone on
  the page.
- The `running` section goes **flat** while a filter is on: a tree with rows taken out of
  the middle is a tree whose connectors point at nothing, and someone searching is looking
  for a name.
- `backspace` deletes a character, `ctrl+w` a word, `ctrl+u` all of it.
- **`esc` clears the filter first and closes the page on the second press** — the same
  layering the settings panel's search has. `ctrl+.` closes the page from anywhere.
- With nothing matching, the foot reads `filter · zzz · nothing matches`.
- `↑`/`↓` and `enter` keep working over exactly the rows the filter left.

## Keys and clicks on the task page

| key | what it does |
| --- | --- |
| `↑` `↓` (or `ctrl+p` / `ctrl+n`) | move, stepping over the section words and over another window's rows |
| `pgup` `pgdown` | move twelve rows |
| `home` `end` | first row, last row |
| `enter` | open it — a room, or inside the record card; see below |
| any printable key | type into the filter |
| `backspace` `ctrl+w` `ctrl+u` | edit the filter |
| `esc` | clear the filter, or close the page when there is none |
| `ctrl+.` | close the page |

`ctrl+c` still works and still means what it always means: mid-turn it interrupts,
and at rest it takes two presses within 1.5 seconds to quit — the page stays up while
the door is armed, and the armed line names any task that would stop.

**What `enter` opens depends on the row**, and the last line of the page says which you are
going to get:

- A task **this session is holding** — anything under `running`, and anything under
  `earlier` that this conversation ran — opens its room, exactly as `enter` on the roster
  does. The foot reads `esc close · ↑↓ move · enter opens its room`.
- A task **another conversation ran** has no room to open: a room is a live lane onto a task
  in this session's work, and that session is closed. `enter` **goes inside it** instead —
  the card of everything the project wrote down about that piece of work, over the same
  page, with this list still underneath. The foot reads
  `esc close · ↑↓ move · enter goes inside it`. See *Going inside an old task* below.
- A task **running in another window right now** takes no cursor at all: `↑`/`↓` step over
  it and `enter` does nothing, because it has neither a room here nor a landed row for a
  mention to point at. On a page whose only rows are those, the foot reads
  `esc close · ↑↓ move`.

Clicking a row does what `enter` on it does, on the **first** press — the page opens things,
it does not change them. The row under the pointer takes the hover step. The wheel walks the
cursor.

## Tasks on a phone — the ▸ tasks door, the list, and the way back

Under 60 columns the whole task flow is a thumb's, with no keyboard anywhere in it. None of
the surfaces is new — the strip, the roster page and the record card all exist and all
answer a key — but each is reshaped so a finger can do the flow end to end:

1. **The strip is one door, and it looks like one.** Instead of a row of tiny chips a few
   cells apart, the strip at phone width is a single full-width row that says what is there
   and that it opens: `▸ 3 tasks · 1 running`. The `▸` is the same fold glyph the rest of
   the phone screen folds with, the count is how many tasks are live, and the tail is the
   most urgent of them — `running`, or `needs you`, or `idle`. One task still draws the
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

The door opens the **roster page** — the running tree and the project's record — not the
overlay column a keyboard drives. Mouse motion is ignored on the glass: a tap opens in one
gesture, and no row lights up under a finger that is only resting on it.

## Open a task by tapping — the tap targets under 60 columns

Every door onto a piece of work is a tap target at phone width, and none of them is a new
door — they are the ones above, reshaped:

- **The strip along the top** is one full-width door — `▸ 3 tasks · 1 running` — and a tap
  anywhere on it opens the roster page. It is not a row of chips at this width.
- **The roster's rows** are two-line cards, each a full-width target, and one press opens it
  — a room, or the record card — which is what a click already did at every width.
- **The roster's foot** is a `‹ back` band in place of the key legend `esc close · ↑↓
  move · enter opens its room`. Tap it to go back to the conversation.
- **The record card's foot.** `esc back · ↑↓ scroll · m puts it in your message` is a
  sentence about keys; under 60 columns the two things a thumb can do become bands instead —
  `‹ back` and `m puts it in your message`. Tap either, or press the key it names. The
  scroll is the screen itself.
- **A task row on home's sheet.** Tapping a task on the work band of a conversation's card
  opens **that task's record** — the same card `enter` opens on the task page.

## Going inside an old task — see what a past task did, read a finished task's report

`enter` on any `earlier` row of the task page (`ctrl+.`, `/history`) **goes inside that
task**. A click does the same on the first press. The task column carries no rows of old
work — its `ctrl+. earlier` line is the door onto this page — so the page is where every
old task is opened.
What opens is a full-screen card over the same page, with the list still underneath:

```
 Fix the nil-map crash                                              esc back
 ─────────────────────────────────────────────────────────────────────────────
 done · landed 3h ago · ran 4m12s

 Added the guard and the regression test; the parser suite passes.

 anthropic/claude-sonnet-4.5 · $0.42 · 12k tok
 3 files changed

 worktree · ~/.aforge/v3/projects/-tmp-alpha/trees/fix-the-nil-map-crash
 transcript · ~/.aforge/v3/projects/-tmp-alpha/aaaa…/tasks/20260819-120133_7.jsonl

 what it said at the end
 Added a nil check in parseRow before the map write, and a regression test that
 fails without it. The parser suite passes: 84 tests, 0 failures.
 ─────────────────────────────────────────────────────────────────────────────
 esc back · ↑↓ scroll · m puts it in your message
```

Top to bottom: the title; the state it came home in, when it landed and how long it ran;
the outcome sentence; what it ran on and what it spent; how many files it changed; where it
left the work and where the story is; and then **the last thing the task itself said** —
the whole report, read off that task's own journal, of which the outcome above is the first
sentence.

- **Anything aforge does not know is not drawn at all.** A task that spent nothing has no
  money line, one that wrote nothing has no file count, one still claiming to be running
  has no clock. Nothing here appears as a zero.
- **The two addresses are clickable where they still exist.** The transcript is a real file
  on this disk and opens in your editor on a click; a worktree that has since been merged
  and pruned is printed as plain text, because a link that opens nothing is worse than no
  link. A task whose worktree is gone says `branch` and the branch name instead — that is a
  name inside your repository, not a place on the disk, so it is never a link.
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
line above `❯ ctrl+g hide`:

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
With the pointer, the room's own pinned header is the way back: it reads `esc/← main` and
the whole row answers to a press. **Clicking inside the page does not leave it** — a press
on a blank row, or on prose with nothing behind it, does nothing at all, the same as it
does in the conversation.

What refuses to open: a proposal whose task has had no update yet (the id is real, but a
room on it would be an empty page with nothing coming), and a queued task by way of `→`
(it has no worker yet, so there is nothing to talk to). An agent with no rooms at all
writes one note in the conversation: `room unavailable — this session has no task rooms`.

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
| pinned top line | none | the focus header, and the family lines under it |
| legend word | the branch, or remote machine | `room · esc/←← main`, and `room · esc your line back` while a history walk is on |
| legend hint | `esc interrupt` while a turn runs | `x stop` while there is work to stop, `↑↓ history` mid-walk, nothing otherwise |
| the model on the status row | the conversation's model | `task <the task's model>` |
| clicking that model | opens the picker and switches the conversation | opens the picker and switches **that task**, from its next turn — and does nothing at all once the task has landed |
| `ctrl+b` | freezes the transcript | freezes the room's own rows |
| scroll position | the conversation's | the room's own, kept separately |
| attachments | the tray sends pictures | a room's box sends words only |
| proposals | drawn as cards | never — a task's own pieces start without asking you |

The focus header is an accent line pinned at the top:
`─ ⠙ main ▸ Fix the nil-map crash · running · 2m12s · $0.04 ──── esc/←← main ─`. It carries
the state glyph, a trail that always names `main` as the root, then the state word, the
clock, the spend and the model — each dropped when nobody published it. It is pinned
because a fact that scrolls away is only true at the top of the page.

**The header is a button as well as a line.** Press it anywhere along its width and you
are back in the conversation, which is the pointer's version of the `esc/← main` it
prints. The one exception is the `✕` at its right end, which asks to stop the work
instead. Every kind of room draws this header — a task's page, a sub-harness design, an
adaptive run's graph, a run node's transcript — so the way out is always named and always
pressable. It is dropped only on a terminal too short or narrower than 12 columns to draw
it, where `esc` still leaves.

Under it, dim and indented, come up to three more pinned lines saying where this task sits
in its family — see *Who started this task, and what it handed out*.

## Typing in a task's room — the up arrow, editing what you sent, and escape

The box in a room is the same box as the one in the main thread, and it behaves the same
way. There is no separate "steer widget" with rules of its own.

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

## Steering a task that is waiting on its pieces — I typed into a task that split its work and nothing happened

A task that handed pieces of its work out stops talking and waits. It has said everything it
had to say, and the only thing that was ever going to move it again is one of its pieces
finishing (*When a task splits its own work*). The column still draws it as working, because
it is: waiting on its own pieces is the work.

**Type into its room anyway — your line is what wakes it.** The task reads your words as your
words, answers them, and goes back to waiting for its pieces. Because the page has been
still, the room says what just happened when it takes the line:

```
· it was waiting on its pieces — your line wakes it
```

Nothing else about the task moves. What it was asked to do and what it will be checked
against were frozen when it started, and steering never touches either — if the goal itself
was wrong, the answer is a new task (*Does aforge change my task, or rewrite what I asked
for?*).

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
instead. When the task has landed, a foot line reads `task finished — esc to return`, and
a landed room with no journal to read says `this task's transcript is not here any more`
above it. A finished task's room replays its whole transcript after a restart as well —
see *A task's room after a restart*.

`pgup`/`pgdown` scroll a page, the mouse wheel scrolls, and reaching the bottom re-sticks
to the live edge. `↑`/`↓` walk your history first and only scroll a line when there is no
history to walk — see *Typing in a task's room*. `ctrl+b` freezes the room's rows for
copying — one known wrinkle: leaving copy mode rejoins the conversation's live edge, so
freezing a room while the conversation was scrolled up loses that scroll.

The task's elapsed clock freezes while you stand in its room. That number exists to ask
whether you should go and look; being there is the answer. Nothing is stopped, only
unreported, and it thaws at the value it would have had when you leave.

## Seeing the whole conversation inside a task — a room never folds its work away

**A room shows everything the task said and did, and it stays shown.** Out in the main
thread a finished turn's machinery collapses into one chip —
`▸ worked 47s · thought 6s · 6 tool calls · ctrl+e` — so the page reads back as the question
you asked and the answer you got. **That never happens inside a room.** A task's whole life
is one long stretch of work ending in a report, so a chip there would hide the entire page
and leave you the report you already had. There is no `▸ worked` line in a room, nothing to
click open, and the `ui.work` setting does not reach one.

So a room you walk into — a running task, a task that has landed, a piece of a recursive
task, a harness being designed — reads top to bottom as the discussion it was: the
instruction it was given, its prose between calls, its thinking blocks, every tool call with
its arguments and result, anything you steered into it, and the report at the end. A landed
task's room is the whole transcript, not the summary.

Two bounded things do still hold something back, and both name themselves and open:

- a **thinking block** shows three lines until you press `ctrl+e` or click it —
  `⠿ thought for 6s · 148 tok · ctrl+e`;
- a run of **more tool calls in a row than fit your window** shows a screenful of the
  newest ones above a line reading `9 earlier tool calls · scroll up or ctrl+o`; scrolling
  up at the top of the page, `ctrl+o`, or a click on that line unfolds the run. (The
  conversation keeps three and its line reads `· ctrl+o`; a task's page keeps as many as
  the window is tall — see "Reading a task's page".)

Inside a room `ctrl+e` over an empty box opens the thinking block and nothing else, because
there is no work chip for it to mean instead.

## Who started this task, and what it handed out

Standing inside a task, the pinned lines under the focus header say where it sits in the
family — who asked for the work, what the work handed out, and what it is still behind.
They are dim, indented two cells under the trail, and each one is simply absent when there
is nothing to say:

```
─ ⠙ main ▸ Write the tree · working · 2m 12s ────── esc/← main · ✕ ─
  part of: Ship the port
  spawned: Cut the goldens — queued · Wire the seam — running
```

- **`part of: <title>`** names the task that handed this work out — the parent. A task
  nobody spawned draws no such line, so its absence means *this is a top-level task*. A
  parent this session has had no update for is left unsaid rather than named as a bare id.
- **`spawned: <title> — <state>`**, one entry per piece, separated by ` · `, in the order
  the session met them. The state is the same word the roster uses: `queued`, `working`,
  `finishing`, `waiting`, `done`, `failed`, `stopped`, `needs your look`. A piece that is
  itself queued behind another piece says only `queued` here; open its own room to see what
  it is behind.
- **what this task waits on** is on the accent line itself, as its state word:
  `waits: <title>` names the prerequisites that have not finished. It is there rather than
  on a line of its own so the header never says the same thing twice.

Nothing new is being tracked for these lines — they are the roster's own tree, read from
the one node you are standing in, said in words because the tree shape is not on screen
here.

Limits, so you know when the page is not telling you everything: at most **three** lines,
wrapped on their spaces and cut there, because they are charged to the transcript
underneath them. They stand down entirely on a terminal shorter than **16 rows** or
narrower than **12 columns**, where the header itself is already fighting for room. An
adaptive run's page draws none of them: the graph with its edges is already on screen
there.

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

Nothing asks you about those. **A sub-task starts without a card:** the countdown card is
how a person redirects work, and there is nobody inside a worktree to show one to, so a
task's own proposals begin the moment they are made. What you see instead is the tree.

Where they show up:

- **the strip along the top** keeps one flat row of live chips on narrow frames; it does
  not draw the family tree.
- **the roster** draws the whole family together, with each piece joined to its parent by
  tree connectors and carrying its own id and state.
- **the parent's room** shows the `propose_task` calls as they are made, and the parent's
  own words when the reports come back — and its pinned header lists each piece by name
  with the state it is in (*Who started this task, and what it handed out*).
- **the piece's own room** says `part of: <the parent's title>` under its header, so a task
  you walked into knows it is a piece of something.

Each piece works in a copy of the repository taken from its **parent's** copy, and its
branch merges back into the parent's — so a family's work comes home as the parent's work,
in one merge, not as three branches racing for yours.

A parent never lands while a piece of it is still running. Its own turn may end long
before; the task stays open, each report is put in front of it as it arrives, and only then
is the parent's work checked and merged. While it waits it is not taking steps — so a line
you type into its room is what wakes it, and it goes back to waiting afterwards (*Steering a
task that is waiting on its pieces*). If you stop a parent, its unfinished pieces are
stopped with it and their branches are kept.

## When a task turns out to be too wide for one worker — a task that splits itself, dividing work, parts of a task

A task is usually one worker. It is not fixed to one.

The section above is about parts a task can see from its brief. This is the other moment:
the worker has **opened the material** and there is more of it than anybody knew when the
work was written. The directory holds eleven adapters. The search matched forty call sites.
The report needs a section per region and there are nine regions. Nobody could have known
that from the sentence you typed.

So the worker can say so. Its tool for it is `divide_work` — it names the parts it found and
what it actually saw that revealed them — and **the work splits**: each part becomes a worker of its own under the task, in its
own copy of the repository, with its own branch coming home into the parent's. Your
transcript says it in plain words — `split into 3 parts:` and then each part by number and
name.

**The worker does not go away.** It keeps whatever part it decided to keep, every part's
report reaches it as that part lands, and the one thing it owes you at the end is a single
deliverable made out of all of it. A task never finishes while a part of it is still running.

**Two things have to be true, and neither is the worker's confidence.**

- **There must be enough separate items.** Below six, one worker doing them in order beats
  paying for a copy of the repository, a check and a wait for each part. This was measured,
  not guessed: twelve image files won, four modules and three bugs lost.
- **There has to be a lane free for the parts.** This is your own `task.parallel` cap and
  nothing else — the worker asking does not count, because it hands its lane back the
  moment it starts waiting on its parts. With every lane busy the parts would be done one
  at a time anyway and each would still cost a copy of the repository, so the split is not
  taken and the worker is told to ask again once something finishes. With `task.parallel`
  set to exactly **1** there is no second pair of hands at all, and the worker is told
  plainly that asking again will not change it.

If either says no, **nothing happens** — nothing is cancelled, nothing extra is spent, and
the worker carries straight on as one worker. That is why this costs nothing on ordinary
work: a task that is not wide is never split, and finding that out is free.

**And then the plan itself is read once, by your `mastermind` model.** Both tests above are
about whether a split is worth it; neither of them reads the parts. But a part's
brief is everything that worker will ever know — it never sees your conversation and cannot
ask anybody anything — and the briefs were written by whatever model the task itself runs on.
So once the two tests have passed, the whole division goes to the mastermind at once: the
evidence, the work it came out of, and every part beside its siblings. It can sharpen a
brief, fix a boundary two parts share, fold two parts into one, or say the parts are really
stages of one procedure and not a division at all — in which case nothing is split and the
worker carries on, exactly as a no from either test above. So the parts you see may be fewer
than the worker asked for, and their briefs may not be word for word what it wrote.

**This reading can only ever improve a split; it cannot lose you one.** If the mastermind
cannot be reached, times out, or answers something unusable, the division goes ahead **as the
worker wrote it**. It had already passed the two tests that were measured, and a second opinion that
cannot be had is not a reason to throw work away.

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

**A busy machine is not one of the two tests.** `task.max_load` and `task.min_free_mb` never
refuse a split. If the machine is over one of them when the work divides, the split happens
and the parts simply **wait** — the same wait any queued task does, drawn as
`waiting · machine busy` — and they start themselves as soon as the machine clears. The
worker is told so in its receipt and has nothing to come back for.

**You may have been warned it could happen.** A `/task <brief>` whose sizing call found more
than one job in your words writes one dim line before the work starts —
`the work looks wide · one worker starts, and it can split as it goes` — and that line is
what this section is about. It promises nothing: the two tests below still have to pass.

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
night to re-read the same sentence is a bill nobody agreed to. The two tests still decide and
the plan is still read once before the parts exist, and the machine is still respected: a
division at 3am on a loaded box is admitted and the parts wait for it. The firing stays open until its parts are home and their spend is on its
own cost row. The standing orders page has the rest of what an unattended run is.

**Where you see it:** the parts appear in the task column under their parent, joined by tree
connectors and carrying their own id and state, exactly as pieces handed out from the brief
do. Walk into the parent's room and its header lists each part by name with the state it is
in; walk into a part and its header says `part of: <the parent's title>`.

**Stopping.** Stop the parent and its unfinished parts stop with it, their branches kept.

It is on by default and there is no setting for it. `AFORGE_SWARM=0` in the environment
turns the whole road off, and `AFORGE_SPLITGATE=0` takes the width test away and lets a
worker's request to split be taken at its word — both are for somebody rolling something
back, not preferences, which is why neither is in the settings panel.

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
more than they save: every piece pays for its own copy of the repository, its own check and
its own wait, so past a few of them fanning out is slower than working. A task is told the
same thing in its own words — split only what is genuinely independent, and never shard
work that fits in its own hands.

`task.parallel` still applies to the whole session: pieces queue behind it exactly as
top-level tasks do.

## How many tasks run at once

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

Name nothing and the task runs on `task.model` if you have set it, and otherwise on the
model the conversation was on **when the task was admitted**. The id is settled at that
moment and remembered for the task's whole life — it survives a restart, and switching the
conversation's model afterwards does not move work that was already handed over. This
holds for `/task` and for a task the model proposed alike. What *can* move it afterwards is
you, from inside that task's own room — see the next section.

## Changing the model for one task while it is running — switch, change or swap a task's model

**Walk into the task's room and press the model's name at the bottom of the screen.**

While you are in a room the status line names that node: `<mark> <task name> · task
<model>`. Press the `task <model>` part and the ordinary model picker opens, aimed at that
task. Choose a row and that task moves onto it.

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
  ladder: `task.model` from settings if you have set one, otherwise the model the
  conversation is on. A pick made inside one room is not a preference the session learns.
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

A word matching more than four models is refused the same way, because that is a list and
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
There is no bypass key: the card is always asked.

With a pointer, the `✕` at the right end of a room's pinned header raises the same card.
Strip chips do not carry a stop button.

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
| stop it | `x`, or the `✕` in its room's header — one confirmation card, always |
| change its brief or its done-condition | **cannot** — frozen; propose the work again |

Steering sends your words into the task's own loop verbatim, and they land in its room as
your own line. If nobody is listening any more — it landed, it was stopped, its worker is
gone — the room asks rather than dropping the sentence or quietly sending it to the main
model: `<title> is parked — [r] revive and send · [m] send to main · [esc] cancel`, with
the engine's own reason on a dim second row. `r` leaves the room and asks the model to start
the work again with your instruction; `m` leaves the room and sends your words to the model
unwrapped; `esc` cancels and leaves your words exactly where they are in the box.

Whenever a task stops for any reason it wears `stopped — branch kept` and its branch name.
Nothing is thrown away: on every ending except a clean merge the branch is kept and named,
and what the task made is committed onto that branch before it lands — so the files it
produced are listed under `changed:` and `git merge task/…` brings them over. The merge is
never done for you, because only work that was checked reaches your branch.

## Why is the task waiting for me — finished but needs your look

Some work lands with `needs your look`: it finished, but nobody could say whether it holds —
or it finished and held, and one of the files it wrote was changed by other work while it
was running, which the how-tasks-run page covers on its own.
It is neither done nor failed. Nothing has merged, the branch is kept, and anything waiting
on it stays waiting until somebody decides. Its family rises to the top of the roster, and
its row reads `finished — look it over`.

Read it first. Its room holds the whole of it, and its landing card expands to the changed
files, the branch, the model, the cost, the done-condition and the report.

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

**The task's room asks the same question** at the foot of its page, and in there the four
keys need no selection — see *How do I approve a task* below. Room and card are one
question: answer in either and both show the receipt.

## What accept, look again and not right each do

- **`[a] accept`** — you looked and you are taking the work. Its branch merges into yours
  exactly as checked work does, and everything queued behind it unblocks. The report leads
  `you looked at this yourself and took it as done`.
- **`[l] look again`** — a fresh check runs against the same working copy. It answers on its
  own, minutes later, and until it does the task **still** needs a look: what goes away is
  the choices, not the state. If `check task work` is off there is no checker to ask, so
  this answer cannot be taken; the card keeps its choices and says
  `that one could not be taken — try another`. The same line appears for any answer that
  could not be spent — a working copy that has gone, for instance.
- **`[n] not right`** — you looked and it is not finished. The task fails, its branch is
  kept, its previous report is kept under the refusal, and its dependents fail with it. The
  report leads `incomplete — you looked at this yourself and said so`.
- **`[d] decide these for me`** — the escape hatch, described in the next section.

**Answered means the choices are gone, not greyed.** The two rows are replaced by one dim
line saying what you did: `you took this as done`, `sent back to be checked again`,
`you said it is not finished`, or `already answered` when somebody got there first — the
model's own settling, a re-check that finally answered, another window.

The card's own head is **not** rewritten — it is the record of how the work came home, kept
branch and all. What follows is: the task re-settles into `done` or `failed`, a state it has
not been in, so a **second** landing card is drawn saying what became of the work. The
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

## How do I approve a task — accept a finished task from the room, the card, or by saying so

A task that landed `needs your look` (`finished — look it over` on the roster) is approved
by **accepting** it, and there are three doors onto the same decision. Use whichever is in
front of you:

| Where you are | What to do |
| --- | --- |
| **inside the task's room** (enter on the roster row, or click its landing card) | press `a` over an empty message box — no selection needed, the room is the task. `l` looks again, `n` says it is not right, `d` hands these to aforge from now on. The same four are chips at the foot of the page |
| **at the landing card** in the conversation | walk to the card with `↑`/`↓` so it is selected, then the same four keys — or click a chip on its answers row |
| **anywhere**, typing | say it: "accept task 7", "that one isn't finished", "have another look at task 7" |

Accepting merges the task's branch into yours and unblocks everything queued behind it.
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
in layers, with one fuel gauge pinned at the top — `planner: <model> · $0.87 / $2.00`, the
model doing the planning beside what it has spent of what you approved.

**`x` on that page stops the whole run**, over an empty message box, and it asks the same
one card the pointer's `✕` asks:

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
