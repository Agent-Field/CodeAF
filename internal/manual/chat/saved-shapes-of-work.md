# Saved shapes of work

## What a harness is

A harness is a shape of work you have done before, saved so it can be recognised and
offered again. It is a named, versioned procedure: steps, a list of the tools those steps
may use, and its own bounds.

You build one by asking for it in a sentence. You keep it by approving a card. Afterwards
it lives on disk and shows up in a list.

**Sub-harness and harness are the same word.** `/harness`, `/harnesses`, `/subharness` and
`/sub` all reach the same list.

**There are two ways to run one.** aforge offers one by itself when what you typed matches
a saved harness closely enough — that is the road for somebody who does not know the
registry has the thing they are describing. And you can pick one yourself: type `/harness `
with a space, choose it from the list that opens, and then type the request. See *Picking a
harness yourself* below. `/harness` with nothing after it lists what you have.

Three rules govern that offer:

- **It is a question, never a routing.** Nothing runs because a matcher said so. Saying no
  costs nothing and leaves the ordinary turn intact.
- **It is asked once per turn**, before the first request.
- **It is silent when nobody is watching.** With no registry, no runner, or no interactive
  screen, the whole feature does nothing at all.

## Why aforge offered me a harness

The detection that raises the offer is a table lookup. No model is called. It is
deterministic — the same words give the same answer on every machine.

Signals combine as independent evidence. Naming the harness fires on its own (weight
**0.9**). One whole multi-word cue fires on its own (**0.9**). Most of a phrase almost
fires (**0.6**). A single-word cue never fires alone (**0.45**; two of them together reach
0.70). The description counts at half weight (**0.5**) and can never carry a match by
itself.

The card is raised when the best entry scores at least **0.55**, or when it leads the
runner-up by **0.25**. Nothing scoring below **0.40** may reach it by either road. Ties go
to the earlier entry, and a tie is by construction not a clear lead.

**Only what a person typed is matched.** A woken turn, a note the session wrote to itself,
and an empty message are never matched. Offering a harness against a task's own completion
report would be the harness talking itself into work nobody asked for.

If you want to know why a harness you saved yesterday is no longer being offered, read
*Why it stopped offering after a restart* on this page — that is a real limit of this
build, not a matching accident.

## The offer card and the keys it takes

One row appears under the connect offer and above the message box:

```
? run harness "research"? · finds an answer across sources · [enter] run · [esc] no
```

| Key | What it does |
| --- | --- |
| `enter` or `y` | run the harness |
| `esc` or `n` | no — the ordinary turn goes ahead |
| `ctrl+c` | not swallowed; mid-turn it is still the interrupt, which releases the held turn |

Every other key does nothing while the row is up. It is modal, because the session is
holding a turn on your answer.

A click works too. Each key chip **and the word beside it** are one target. A press
anywhere else on the row is swallowed rather than falling through to what is underneath.

The line is assembled longest-first and shortened a piece at a time on a narrow frame: the
description goes first, the model survives one rung longer, and **the answers are never
dropped**. If a second question is queued behind this one, a dim row under it reads
`  N more`.

**Nothing is written to the transcript either way.** A yes is followed by the run, which
the surface notes as `harness · <name>`. A no changed nothing.

An offer whose turn has already ended is dropped unanswered — the answer would be late. A
harness with no name raises no card at all.

## Naming a model for the run

End the turn with `with <model>`, `using <model>` or `via <model>`:

```
research the pricing tiers with opus
```

The clause is read **at the end and only there**. The same words in the middle of a
sentence are ordinary English and choose nothing. It is bounded at **3 words**, must not
contain a function word, must be spelled with id characters (letters, digits, `-` `.` `_`
`/` `:` `~`), and its first word must contain a letter.

When the word resolves, it is stripped from your text before scoring **and** before the
run, so naming a model never makes the offer less likely to appear. The card then shows
` · model: claude-opus-5` in dim text, with the vendor prefix dropped, and the model the
row showed is what travels back with your answer.

**Naming a model is never a refusal.** A word this install cannot place leaves the clause
where it was, and the card says so instead:

```
model "opus" not found, running default
model "opus" matches several here, running default
```

A session with no model catalog cannot check an id, so the word travels as written.

## What a yes actually runs

The harness's head version is loaded from the store and walked step by step over a model
bridge. Each `agent.loop` step is one completion. Each `tool.call` step is one tool. Each
free-text condition is one small judgement. A single step's completion gives up after
**10 minutes**.

The trace is saved beside the harness. The turn's answer is the run card, then what the
run produced, then a line reading `trace · <path>`.

## Watching a harness run — seeing the steps while it works

A run takes minutes, and you can watch it. Under the `harness · <name>` line, **one row
shows the step that just finished** — the same row the card read back afterwards prints:

```
harness · triage-flake
  ✓ 2   gather       agent.loop     read the changelog · 4.1s
```

The mark, the step number, the id, the kind, and then what the step left behind — or, when
it went wrong, what went wrong with it, marked `✗`. A failing step is the last one you see,
because the run ends on it.

**The row is replaced, never stacked.** Only the step happening now is on it. The report
that lands when the run is over carries the whole trail, so keeping every step in the
conversation would be that trail written out twice — and it would ride in every later
request forever. Nothing about a live step is written down: it is on the screen and
nowhere else.

While that row is up, the ellipsis that means "still working" is not — a step landing every
few seconds already says the run is alive, and two answers to one question is one too many.
The row goes away with the turn, by which time the report is on screen saying what every
step did.

**A failed run still reports.** The trail is the one thing worth having when a harness went
wrong: the card's head reads `name · v1 · failed` and the failing step is marked `✗`.

The tools a harness may reach are a fixed bare set over your workspace, not the belt the
conversation itself carries. Notes, jobs, connected accounts and image generation are
things a conversation reaches for, not things a saved procedure should inherit. `bash`,
`read`, `ls`, `grep` and `find` accept a bare string. `edit` and `write` take JSON and say
so: `edit takes its arguments as a JSON object, and "..." is not one`. An unknown tool
gets `there is no tool named "x" on this surface`.

Two limits worth knowing:

- **A `human.gate` step auto-approves.** There is no lane on this screen that stops and
  asks you. The trail records that it auto-approved.
- **The run's model does not follow `/model`.** The client is built once at launch, so the
  conversation can move to another model and the run's client does not. A step that names
  its own model overrides it, and so does a turn that named one.

## Asking for a new harness — how do I make one

Ask in your own words. "Build me something that does this every sprint", "we should have a
saved procedure for release notes", "make a harness for triaging flaky tests" — all of them
work, because **deciding that you asked for one is the model's judgement**, not a phrase
this program matches. There is no grammar to learn and no keyword to remember. It reaches
the designer through a tool called `build_harness`, and the goal it passes is a brief it
wrote for you: when your words pointed at something in the conversation ("a harness for what
we just did"), that context is written into the goal, because the designer cannot see this
conversation.

It used to be a fixed cue — a message that began `make a harness for …` and nothing else —
which is why phrasing it any other way used to get you a paragraph about harnesses instead
of a harness. That cue is gone.

Two consequences worth knowing:

- **Saying it may first offer to RUN a harness you already have.** Your words are matched
  against the registry before the model sees them, so asking to build a flake-triage harness
  when one exists raises the run card first. Say no with `esc`, and the build goes ahead.
- The model may look before it builds: it can list what is saved and tell you a harness that
  already does this exists, which is usually the better answer.

**The turn does not wait, and the design becomes a task.** Its live design block appears
in the ordinary chat feed. Reasoning rolls through its last three lines; partial JSON is
translated into small facts such as `naming it: research-helper` and `4 steps so far`.
After ten seconds without a delta it keeps ticking with `thinking · 52s` rather than
leaving a blank screen. Attempts, draft checking, and truncated-versus-malformed retries
are named there too.

```
⠿ harness · designing · attempt 1/3
```

That number is the whole of what a design gained: it is a real task, with a row on the
roster, a room you can walk into, a journal, and a stop. See *The design's own task and
room* below. **Writing** the page runs inside a **30-minute** window — the design turn, its
retries, and the review pass. The card that follows has **no clock at all**: it waits on you
for as long as you take.

When the page lands, that same live block collapses in place into a fully visible
architecture card in the feed. It shows the name and purpose, a deterministic ASCII
diagram of the steps and edges, one plain-language structure point per step, the
verification law, and allowed tools. Wide layouts draw a linear chain horizontally;
phone layouts stack it vertically. The card scrolls with the conversation and is never a
popup or sheet.

The focused card takes `enter` to save, `e` to put an improvement request in the message
box, and `esc` to drop it. The same three actions are clickable. The card remains in the
feed after the choice as `saved as <name> v1`, `improvement requested`, or `dropped`.

Under the hood: one design pass, then up to **2 retries** in which a refused design is
shown the exact sentence it failed on and asked to fix it, then **one** review pass that
writes findings and a patch rather than a whole new page. A failed review loses only the
improvement, not the draft.

The whole path is off unless there is a runner, a store, **and** an interactive screen to
answer the card on.

## The design's own task and room — where do I watch a harness being designed

**A harness being designed is a task.** Asking for one admits a node to the same work graph
`propose_task` uses, so everything the roster already does works on it:

```
◆ 4 harness · triage flaky tests
    designing
```

| What | Where |
| --- | --- |
| the row | the activity strip, and the roster on `ctrl+t` |
| the room | press the row, or open it from the roster; `esc` comes back out |
| the thread | the room's journal — the brief, every reply the designer wrote, the page, and what became of it — kept on disk with the rest of the session's tasks |
| stopping it | `x` on its row, or the `✕` — the same card everything else is stopped by |
| the number | `task 4`, which is what you and aforge both call it afterwards |

**It is admitted without a countdown**, unlike an ordinary task. There is no "redirect or
wave it off" window in front of it, because the question about a design is at the *end*: the
page is shown to you as a card and nothing is written to the registry unless you approve it.
One decision, asked once.

**It spends no concurrency slot.** `task.parallel` and the machine-load governor are about
workers with a checkout and a build; a design is two model calls and a card sitting on your
screen. A design is never queued behind a busy machine.

### The three phases

The row's state word is replaced by what the design is actually doing, and it moves twice:

| Phase | What is happening |
| --- | --- |
| `designing` | the page is being written — two model calls against a long guide, inside a 30-minute window |
| `awaiting your look` | the page is written and the save-or-discard card is up — no clock runs here |
| — | it lands, and the settle card says what became of it |

**There is no progress bar and no percentage.** The live block reports only observed
reasoning, received bytes, recovered names and step counts, elapsed stall time, and the
real retry phase; it never invents completion.

The settle card is the ordinary one a task lands with, and its outcome line is one of:

```
harness "triage-flake" v1 saved
harness "triage-flake" was designed and not saved
harness "triage-flake" was designed; the card went unanswered, so nothing was saved
harness "triage-flake" could not be saved: <err>
the design failed: <err>
the design ran out of time before it finished; nothing was saved
harness design stopped; nothing was saved
```

A saved design's card reads `v1` even for a first version, because "v1" is the news that it
is the first of them. A design that failed settles as a failed task; one you declined settles
as **done**, because you were asked and you answered — nothing went wrong. So does one whose
card you never got to: the page was written, and not keeping it is not a fault.

**"Ran out of time" is only ever about the writing.** It is the 30-minute window on the two
model calls, and a design that reached a page can never land on that line — see *Why a design
timed out even though the page was there* below.

### Talking to a design — improving a harness later

The room's message box talks to the design, not to the conversation. What is in that thread
is the brief it was given and the page it wrote, so it can answer questions about the harness
with the harness in front of it: why it chose two steps, what a step does, whether it would
fit some other work.

The drafts it wrote on the way there are in the room's history to read, but they are **not**
in front of the thread. It answers from the page that was actually written; a draft that was
thrown away sitting beside it would be a wrong answer waiting to be given.

**It cannot save a revision from in there.** Writing a page is the designer's job, and a
revision is a new design — ask for one the same way you asked for the first, and it gets a
task and a thread of its own, landing as the next version of the same name. The thread says
so rather than implying otherwise.

Steering only reaches a design **while it is running** — which is both phases above,
including the long one where the card is waiting on you. After it lands, the room is the
history: the whole design thread, readable, with the outcome at the bottom of it. `list_harnesses`
names the task each harness this session designed was designed in, so "the flake-triage
thread" is a number you can go to.

**Over `--host` none of this exists**, because building a harness is switched off there
entirely.

## Watching a harness being designed — what the design room shows while it writes

**The room streams the designer's own work.** Walk into a design's room while it is
`designing` and you see it thinking and you see the page being typed — the same two things a
worker's room shows, drawn the same way: the reasoning in its own block, the reply growing
under it. The page arrives as the JSON envelope the designer is asked for, so what scrolls
past is the harness being written — its cues, its steps, its justification — and not a
summary of it.

**The conversation gets one line instead, and that is deliberate.** In the chat feed the
same stream is a single live block that replaces itself: `⠿ harness · designing · attempt
1/3`, then `naming it: research-helper`, `4 steps so far`, `thinking · 52s`. A feed you are
holding a conversation in cannot have a page of JSON typed into it. A room you walked into
in order to watch can, and that is the whole difference between the two.

**The review pass is in there too.** A design is one writing pass and then one review pass,
and both go through the same room: you see the draft written, then the critique and the
patch that answers it.

**A retry is not announced in the room.** When a draft is refused the room simply shows the
next attempt being written; the reason — `retrying · malformed draft`, `retrying · truncated
draft` — and the attempt counter are on the live block in the chat. A call that failed
outright does say so in the room, as `error: <reason>`.

**It is kept.** Every reply the designer finishes is written to the node's journal, so
opening the design tomorrow shows the writing of the page and not only the page. Nothing is
re-narrated when you walk in: you are handed the reply being typed right now, and the rest
is read off the file, exactly as it works for a worker.

## What a design can be refused for

A design must pass validation and a lint before it can be offered to you. The lint refuses:

- a tool list naming a tool that does not exist here,
- a `subharness.call` to a harness that is not registered,
- a checking step with no condition to check,
- an empty description,
- **fewer than two cues** — with one cue "the entry would be unreachable by anything but
  its own name".

A refused design is shown the sentence it failed on and gets up to two attempts to fix it —
its own page and the exact refusal, so it repairs what it wrote rather than guessing again.

Malformed JSON gets a free salvage attempt (a code fence, prose around the object,
typographic quotes, a trailing comma) and then one repair turn. **A reply that was cut off
buys no repair turn at all.** Half an object cannot be repaired into a whole one without
inventing the rest, so a design that ran out of its completion budget mid-page is told
`your reply stopped in the middle: it ran out of completion budget before the JSON object
was closed` and asked for a SMALLER harness — fewer steps, shorter briefs — on the next
attempt.

## Why a harness design keeps failing

Almost always it is one of two laws the page broke, and both are refusals with a sentence
you can read on the failure note:

- **A rung above `accept` with nothing that could do the checking.** A design that claims
  its output was checked needs a checking step in the program to have done it.
- **Two steps called independent that the program runs one into the other.** A step at the
  end of a line runs after everything earlier in that line, including the parts whose
  output it never reads.

Both are recoverable: the second attempt is usually the one that lands. A design that fails
all three attempts says so — `harness design failed: no valid design in 3 attempts` — and
the goal is worth trying again, or worth saying in fewer parts.

## Why a design timed out even though the page was there — how long a design card waits

**A design card waits as long as you do.** There is no timeout on it, no expiry, and nothing
sweeps it. Go to lunch, come back an hour later, press `enter`: the harness is saved as `v1`
and the design's task settles `harness "triage-flake" v1 saved`, exactly as it would have a
second after the page landed. The only clock in a design is the 30-minute one on **writing**
the page, and it stops the moment the page exists.

It did not always. A design used to be given one 30-minute window for the whole job — the
writing *and* your answer — so a design that wrote its page in ten minutes and then waited
for you was killed at thirty and reported as `the design ran out of time before it finished;
nothing was saved`. Both halves of that sentence were false: it had finished, and the page
was sitting in its room. Worse, the card stayed drawn in the feed with nothing behind it —
pressing `enter` on it did nothing at all, and the page was gone. If you have an old session
whose harness task says it ran out of time, that is what you are looking at; the page is not
recoverable from it, and asking for the harness again is the way back.

**What can end a waiting card**, then, is only: your answer; `x` on its row or the `✕`,
which settles it `harness design stopped; nothing was saved`; or aforge closing, which
settles it `harness "triage-flake" was designed; the card went unanswered, so nothing was
saved`. That last one is a **done** task, not a failed one — the design did its work, and
you simply never got to it. Nothing reaches the registry in either case.

## The save-or-discard card

The page the designer wrote is shown to you before anything is kept. It arrives by itself,
minutes after your build request, on a standing lane rather than on any turn.

The card is drawn above the question in dim text, capped at **14 lines**. A longer page is
cut with a line reading `… N more lines` rather than quietly stopping.

The question row changes only its verb from the run offer:

```
? save harness "triage-flake"? · [enter] save · [esc] discard
```

`enter` or `y` saves. `esc` or `n` discards. A design's row never repeats its description,
because the card's own head line already carries it.

A landing design closes the settings sheet and the expand view first — a question drawn
under a fullscreen panel is an answer nobody can reach.

**Nothing is saved without an answer.** A design nobody answered changed nothing. Unlike a
run offer, a design card is **not** swept away when the turn settles: it outlives its turn
on purpose.

Every ending says something. It is the design task's own settle card in the transcript —
the outcome lines are listed under *The design's own task and room* above — and the same
sentence reaches the model as an ambient note, so the next thing said in the conversation
happens after aforge knows what became of it.

**It is ambient and does not start a turn.** You are at the keyboard, you just answered the
card, and the card already says what happened; a model turn reading your own answer back to
you would be the same news a third time. An ordinary task's landing *does* wake a turn,
because nobody is standing there for it.

## Where harnesses are stored

Under the state root, one directory per harness, with immutable version pages and the runs
beside them:

```
~/.aforge/harnesses/<name>/v1.json
~/.aforge/harnesses/<name>/v2.json
~/.aforge/harnesses/<name>/run/20260816T101112Z.json
```

Setting `AFORGE_HOME` moves the whole tree.

**There is no head file.** The head is simply the highest version page present, so a
pointer can never disagree with the pages it points at. The version is minted by the store
and never chosen: an approved page has its version zeroed before saving, so a new name
lands as v1 and an existing name lands as the next version.

Writes are exclusive-create, so a second save racing the first fails loudly instead of
quietly replacing a version somebody has already read. Run traces are timestamped in UTC,
sortable, to the second, with a `_NN` suffix when two runs land in the same second.

**Every window on this machine reads and writes the same place.** The store holds no cache
— a stale read would be the more expensive mistake — and the registry is read on the
keystroke rather than held from boot. A harness saved in another window ten minutes ago is
in your list now.

A harness you just saved is reachable **from the very next sentence**: the session merges
what this conversation has built into the registry it matches against, and the saved entry
wins over the in-memory one of the same name.

Refusals from the store name themselves: a name that is not a short slug, a save that asks
for a version other than the next one, a version past the cap, and `subharness: not found`.

## Picking a harness yourself — running a subharness on purpose

Type `/harness` **with a space after it** and a filtering list opens under the box, latest
first — the harness that ran most recently at the top, and one that has never run sorted by
when its page was written. Keep typing and it narrows. `/subharness ` and `/sub ` open
exactly the same list.

```
◆ triage-flake     chase a flaky test · 2h ago
◆ review-diff      read a diff · never run
  Browse the registry →
```

| Key | What it does |
| --- | --- |
| `↑` / `↓`, also `ctrl+p` / `ctrl+n` | move |
| `pgup` / `pgdown` | move by a page |
| `enter`, or a click on a row | pick that harness |
| `esc` | close the list and leave what you typed alone |

The last row is always `Browse the registry →`, which opens the card panel below.

**What you picked becomes a chip above the message box**, with the name cut to 24 cells if
it is long, and a dim hint beside it:

```
⚙ triage-flake ✕  — type the request · enter runs it
```

Then type the request and press `enter`. **That harness runs on exactly those words**, with
no matching and no offer card — you already said which one you meant, so nothing asks you
again.

Limits worth knowing:

- **One chip at a time.** Picking a second harness replaces the first.
- **Take it off** by clicking the `✕`, or with `backspace` on an empty box.
- **The chip clears when the message is sent**, so the next thing you type is an ordinary
  turn again. It is also cleared by `/new`.
- `enter` with a chip and an empty box does nothing — the hint is telling you what is
  missing.
- The filtering is over the name and the description. Cues are not on the page (see *Why it
  stopped offering after a restart*), so they cannot be searched here either.
- No registry wired here: `harnesses are unavailable here`.

## /harness — the list of what you have

Type `/harness` or `/harnesses` with nothing after them. Both open the same panel, and so
do `/subharness` and `/sub`. (A space after the word opens the picker above instead.)

A short list opens under the message box, at most **10** lines:

```
◆ triage-flake       v3 · 6 runs · last ok, 2h ago
◆ review-diff        v1 · never run
```

(`#` instead of `◆` on the linear palette.)

| Key | What it does |
| --- | --- |
| `↑` / `↓`, also `ctrl+p` / `ctrl+n` | move |
| `pgup` / `pgdown` | move by a page |
| `enter` | open the highlighted harness |
| `esc` | close the panel |

The name and version sit together on the left, because "triage-flake v3" is the sentence
people say. The description and the history are the dim tail. Ages are coarse: `just now`,
`12m ago`, `3h ago`, `5d ago`. `1 run` is spelled singular. A harness that has never run
says so. A row whose trace could not be read shows the count alone rather than a shrug.

The registry is read on the keystroke, not held from boot, so a harness registered in
another window shows up. Each row reads the **head** version.

`enter`, or a click on a row, closes the panel and prints the harness's card **into the
conversation** — its steps and its bounds, with the last run's card under it when there is
one. The transcript is where prose lives and can be scrolled and copied.

Limits:

- **The panel takes no argument.** A name typed after `/harness` is a filter for the picker
  above, not an instruction to this panel.
- No registry wired here: `harnesses are unavailable here`.
- An empty registry: `no harnesses are registered yet — build one in the conversation`.
- A registry that cannot be read at all draws the empty list rather than an error.

## Why it stopped offering after a restart

This is a real limit of this build, and it is worth stating plainly.

**Cues are not saved to disk.** A harness page has nowhere to put its cue list. The
registry aforge builds at launch therefore carries no cues at all — entries are built from
name, description and version only.

What follows from that:

- A harness designed **in this session** keeps its cues in memory and is matched by them
  for as long as the program keeps running.
- After a restart, that same harness is reachable by **naming it** (the 0.9 naming signal)
  or by description overlap. Description overlap counts at half weight, and half weight
  alone never reaches the threshold.

So if a harness used to be offered when you described the work, and now it is not, nothing
is broken and nothing was lost. Name it, and it will be offered. Or type `/harness ` with a
space, pick it out of the list, and run it on purpose.

## Things that are not available in this build

Two pieces of harness machinery exist in the code and cannot be reached in this build.
They are listed here so you do not go looking for them:

- **The running-harness chip.** The activity strip can draw a chip for a harness that is
  currently running, and clicking it would open the panel. No real session provides the
  chip with anything to draw, so it never appears. `/harness` is the way in.

Two behaviours are absences rather than bugs, and are stated outright in the code:

- **A `human.gate` step inside a harness auto-approves.** A harness step that would pause
  and ask a person does not pause. There is no lane on this screen for it to ask on, and
  inventing one was refused rather than faked. The trail records the auto-approval.
- **The run's model does not follow `/model`.** A run rides the client built at launch
  unless a step or your turn names a model.

## Harnesses over --host

Two different things, two different answers.

**Running an existing harness works.** The offer card rides the turn's own event stream and
the answer travels on the wire like any other, so a remote session can offer a harness and
run it.

**Building a new one is switched off.** Over a `--host` connection aforge does not have
the designer at all: the `build_harness` and `list_harnesses` tools are left off entirely,
so the model does not have the verb and tells you it cannot do it from here rather than
starting something. The reason is that the card asking whether to keep the finished page
has no way to reach you: the design arrives on a standing lane that a remote connection
does not carry, so a design left switched on would have run, written a page, and asked a
question in an empty room. Build the harness while working on that machine directly, and it
is available to run from anywhere afterwards.

**`/harness` is unavailable over a connection.** The registry belongs to the far machine
and this build has no door onto it from here, so the command says exactly:

```
harnesses are unavailable here
```

rather than listing this machine's harnesses and offering to run them over there.

For the rest of what does and does not travel over a connection, see the page on running
the session on another machine with `--host`.
