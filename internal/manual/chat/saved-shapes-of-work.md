# Saved shapes of work

## What a harness is

A harness is a shape of work you have done before, saved so it can be recognised and
offered again. It is a named, versioned procedure: steps, a list of the tools those steps
may use, and its own bounds.

You build one by asking for it in a sentence. You keep it by approving a card. Afterwards
it lives on disk and shows up in a list.

**A harness is a subharness.** One system, one name for it — *subharness* — and several
doors onto it. `/harness` and `/harnesses` open the saved shapes of work this page is
about. `/subharness` and `/sub` open the whole list: these, the ones built into aforge, and
the ones written as bundles on disk, all in one list with nothing marking which is which
except where it was found. See the *Subharnesses* page for that list and the card you
settle before one runs.

**There are three ways to run one.** aforge offers one by itself when what you typed matches
a saved harness closely enough — that is the road for somebody who does not know the
registry has the thing they are describing. You can pick one yourself: type `/harness `
with a space, choose it from the list that opens, and then type the request. Or open
`/subharness`, put the cursor on its row, say what the run is about in the one field its
card asks for, and press `enter` on `run it`. See *Picking a harness yourself* below.
`/harness` with nothing after it lists what you have.

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
| `ctrl+c` | not swallowed; mid-turn it is still the interrupt, which releases the held turn. It never quits on one press — the door takes two |

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

## Which tools can a harness use? Can a saved harness make an image or audio?

A harness reaches the seven working tools — `bash`, `read`, `write`, `edit`, `grep`,
`find`, `ls` — **plus the media verbs this machine has models for**: `generate_image`,
`speak`, `generate_music`, `generate_video` and `view_image`. Making a picture to a fixed
recipe is exactly what a saved procedure is for, so a design may whitelist those verbs and
a step may call them.

It is **not** the whole belt the conversation carries. Notes, jobs, connected accounts,
watches, settings and the task verbs are left out on purpose: those are things a
conversation reaches for, and a recipe run months later should not be able to rewrite your
settings or start a task tree.

The media verbs follow the same rule they follow everywhere — **no model for that kind of
media, no verb** — and the list the designer is shown when it writes a harness is the same
list the run resolves against, so a design can never whitelist a verb this machine cannot
run.

`bash`, `read`, `ls`, `grep`, `find`, `generate_image`, `generate_music`, `generate_video`,
`speak` and `view_image` accept a bare string as their one obvious argument — the command,
the path, the pattern, the prompt, the words to speak. `edit` and `write` take JSON and say
so: `edit takes its arguments as a JSON object, and "..." is not one`. Any other argument —
a size, a voice, a destination — is written as JSON too. An unknown tool gets
`there is no tool named "x" on this surface`.

Two limits worth knowing:

- **A `human.gate` step auto-approves.** There is no lane on this screen that stops and
  asks you. The trail records that it auto-approved.
- **The run's model does not follow `/model`.** The client is built once at launch, so the
  conversation can move to another model and the run's client does not. A step that names
  its own model overrides it, and so does a turn that named one.

## What a harness run costs — does running one show up in /cost, spend, and the rail

**It does.** A run makes its own model calls — one for every step, several for a step that
uses tools, and the calls of any harness it calls in turn — and all of them land on this
conversation's total. `/cost` and `/status` show the money and the tokens, the status line
moves, and the requests are counted in `model calls`.

They are counted the way every call aforge makes on your behalf is counted: against the
session, not against the turn you typed. The turn's own receipt stays empty, because the
run's work is not the conversation's — it thinks on its own messages, on its own model.

It counts against a spending limit too. A run that takes the session past the ceiling you
set in `/settings` leaves the next thing you type refused, exactly as any other spend does.

**A provider that publishes no accounting adds nothing.** Zero tokens and no price means
"nobody said", never "the run was free", so nothing is shown rather than a $0.00 you could
believe.

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
⠿ subharness · designing · attempt 1/3
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

The focused card takes `enter` to save, `e` to open the design's own room so you can say
what to change, and `esc` to drop it — drawn as `[enter] save   [e] change it   [esc] drop`.
The same three actions are clickable. The card remains in the feed after an answer as
`saved as <name> v1` or `dropped`.

**The three keys are read only while the message box is empty and nothing is over the
conversation** — not with home, the settings panel or the model picker up, not inside a
task room, and not in copy mode or rewind. They are letters, and a letter typed into a
sentence is a letter: a bare `e` in the middle of "even the tests pass" types an `e`. The
card's three columns stay clickable at every one of those moments, so nothing is
unreachable — click the one you want, or clear the box and press the key.

**`e` no longer throws the design away.** It used to be labelled "improve", and what it
actually did was discard the page and put `Improve harness <name>: ` in your message box —
so asking for a change destroyed the thing you were asking about and started a second
design from scratch. Nothing said so. It is a door now: the design stays exactly where it
is, still waiting, and `e` walks you into its room, which is where a change is actually
made. A design with no room to open says `this design has no room to open — answer the card
here` and leaves the card alone.

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
| the thread | the room's journal — the brief, the milestones in plain words (a draft written, an attempt refused, what the review changed), the card, and what became of it — kept on disk with the rest of the session's tasks |
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
| `awaiting your look` | the page is written and the save-or-discard card is up — no clock runs here, and the row counts as `needs you` rather than `running` |
| — | it lands, and the settle card says what became of it |

It moves twice for a design you approve on the first card, and **twice more each time you
ask for the page to be changed**: `awaiting your look` goes back to `designing` while the
rewrite is written, then back again when the new card goes up. The 30-minute window is on
the writing, and each rewrite gets it whole — a design you spend an afternoon getting right
is never cut off for taking it seriously.

**There is no progress bar and no percentage.** The live block reports only observed
reasoning, received bytes, recovered names and step counts, elapsed stall time, and the
real retry phase; it never invents completion.

The settle card is the ordinary one a task lands with, and its outcome line is one of:

```
subharness "triage-flake" v1 saved
/subharness runs it, and it offers itself when what you say matches.
harness "triage-flake" was designed and not saved
harness "triage-flake" was designed; the card went unanswered, so nothing was saved
harness "triage-flake" could not be saved: <err>
the design failed: <err>
the design ran out of time before it finished; nothing was saved
harness design stopped; nothing was saved
```

A saved design's card is **two lines** — the name and the version it landed as, then where it
is now. It reads `v1` even for a first version, because "v1" is the news that it is the first
of them. What it saved is a subharness like any other: it is on `/subharness` from that
moment, it runs from there, and it is still offered by the turn itself when what you say
matches. A design that failed settles as a failed task; one you declined settles
as **done**, because you were asked and you answered — nothing went wrong. So does one whose
card you never got to: the page was written, and not keeping it is not a fault.

**"Ran out of time" is only ever about the writing.** It is the 30-minute window on the two
model calls, and a design that reached a page can never land on that line — see *Why a design
timed out even though the page was there* below.

### Talking to a design — asking it about the page it wrote

The room's message box talks to the design, not to the conversation. What is in that thread
is the brief it was given and the page it wrote, so it can answer questions about the harness
with the harness in front of it: why it chose two steps, what a step does, whether it would
fit some other work.

What became of the drafts on the way there — a draft written, an attempt refused, what the
review changed — is in the room's history to read, but none of it is in front of the thread.
It answers from the page that was actually written; a draft that was thrown away sitting
beside it would be a wrong answer waiting to be given.

**It cannot save.** Nothing in that thread reaches the registry: the card is yours to
answer, and only your approval saves a page. What it *can* do is change the page — see
*How do I change a harness design* below.

Steering only reaches a design **while it is running** — which is both phases above,
including the long one where the card is waiting on you. After it lands, the room is the
history: the whole design thread, readable, with the outcome at the bottom of it. `list_harnesses`
names the task each harness this session designed was designed in, so "the flake-triage
thread" is a number you can go to.

## How do I change a harness design — can I iterate on a design before it is saved

**Say what you want different, in the design's room, in your own words.** The page is
rewritten with your change in it and put back in front of you as a new card. You can do
that as many times as it takes.

```
that's close, but it should run the linter before it reports
```

That is the whole gesture. There is no command and no flag: the design's thread reads what
you said, decides it is a change rather than a question, and calls `revise_design` with it.
Then, in order:

1. **The card comes down**, out in the conversation and in the room, because the page it
   was about is about to stop existing. A save key over a replaced draft would save the
   wrong page.
2. **The block in the feed goes back to being live**, with what you asked for on it, and the
   design's row goes back to `designing`.
3. **The designer writes the whole page again** — your change in it, the rest left as you
   already accepted it. It is held to exactly the same law as the first draft: the same
   validator, the same **2 retries**, the same one review pass, and its own fresh window.
4. **A new card goes up**, on the same task number. One design is one number however many
   times it is rewritten.

**Nothing is saved along the way.** The registry is untouched until you approve a card, and
a rewrite you do not like can simply be rewritten again.

**Asking a question does not rewrite anything.** "Why two steps?" and "would this fit the
nightly build?" are answered from the page; only a request for something *different* becomes
a rewrite.

**It works only while a card is up.** Ask for a change while the page is still being written
and the thread is told `this design is not waiting on an answer right now, so there is no
page in front of them to change`; ask for a second change while the first is being made and
it is told `a change to this page is already being made`.

**A rewrite that cannot be written lands the task honestly**, saying `the rewrite failed and
nothing was saved: …` rather than pretending the first design failed. The registry is
untouched either way.

## How do I approve a design from inside its room

**Two chords, pinned above the message box** whenever the design you are standing in is
waiting on you:

```
waiting on your approval — this design saves only if you say so
[ctrl+k] save it · [ctrl+x] drop it · or say below what to change
```

`ctrl+k` saves the page. `ctrl+x` drops it. Both are clickable. Both do exactly what the
card in the conversation does — it is one question with one id, so answering in the room
turns the card out there into `saved as <name> v1` or `dropped`, and answering the card
takes this row down. Whichever you answer first wins; the other finds the question gone.

**They are chords and not letters on purpose.** `esc` leaves the room and `enter` sends your
message, so neither can be taken, and a bare letter would stop being a letter you can type.
`ctrl+k` and `ctrl+x` carry no text and are bound **only** while this row is up.

**The row never swallows your typing.** Every other key falls through to the message box,
because the third answer to the question is a sentence you type there.

Once you answer, the row reads `saved as <name> v1` or `dropped` for the moment before the
task's settle card arrives. While a rewrite is being written there is nothing to approve, so
the row is gone.

## How do I know a message went into a subharness design — the dim words after my own line

**Your own line says what it was part of.** A sentence typed into a design's room is not a
remark to the conversation — it can rewrite the page — so the transcript marks it, dim, on
the line you typed:

```
› use models dynamically in it · designing subharness flake-triage
```

The words after the `·` are the design you are inside. They are quiet on purpose: the mark
is a fact about where the sentence went, not a summons, so it never takes the bright colour
this surface saves for the one live thing on a screen.

| What you see | What it means |
| --- | --- |
| nothing after your line | an ordinary message, to the conversation — this is nearly every message |
| `· designing a subharness` | it went into a design whose page has not been written yet, so it has no name to give you |
| `· designing subharness <name>` | it went into that design, and the page it has written is called `<name>` |

**The name is the real one, and it arrives when it exists.** For the first minute of a
design there is no page and no name, so the mark says `designing a subharness` and nothing
more — aforge does not guess a name for something that has not been written. The moment the
page is written the mark reads `designing subharness <name>`, on new lines and on the ones
already above them, because the room is read back from one place.

**Where it appears:** every line you type into a design's room, and the design's opening
brief at the top of that room, so the history reads back with the mark on it. Walking into
the room again later shows the same marks — they are not a live decoration.

**Where it does not appear:** an ordinary task's room. A task is work you handed over and
walked away from, not a place you are inside, so there is nothing to name and nothing is
drawn — the same rule that keeps `$0.00` and `0 tok` off this surface.

**How the flow begins and ends is already said, and is not said twice.** When a design
starts, the conversation writes one dim line naming it and the task it is running as; when
it lands, the task's settle card says what became of it. The room's own header carries the
task the whole time you are standing in it. This mark is the part that lives in the
transcript itself, so the exchange still says what it was when you scroll back to it or
export it.

## Why does a harness design say "needs you" instead of "running"

Because it is not running. A design at `awaiting your look` has finished everything a
machine can do for it: the page is written, and the only remaining step is you saying
whether to keep it. So:

- the roster's footer counts it under **`needs you`**, not `running`
- the status row's `⏺ N running` does not count it
- its row wears the waiting mark **`?`**, not a spinner — nothing is turning, because
  nothing is happening

Underneath, the task is still `running` on the wire, and that is deliberate and not a bug:
the node stays open so its room stays open, `x` still stops it, and the message box still
reaches its thread. What changed is only what the surface *says* about it, which used to
report a card that had been sitting unanswered since before lunch as work in progress.

The moment you ask for a change, the phase goes back to `designing` and all of it goes back
to running — because at that moment something is.

**Over `--host` none of this exists**, because building a harness is switched off there
entirely.

## Watching a harness being designed — what the design room shows, and why no JSON streams past

**The room shows the designer thinking, not the page being typed.** Walk into a design's
room while it is `designing` and what streams live is the designer's **reasoning**, as
prose, under the one status row that replaces itself rather than stacking:

```
naming it: research-helper
4 steps so far
receiving · 2.3 KB
```

The row carries the stall clock when nothing has arrived for ten seconds, and reads
`reviewing` once the review pass has the draft.

**Raw JSON never appears in the room, and that is deliberate.** The page is written as a
JSON envelope, and an envelope arriving a character at a time is not something a person can
read or act on. What you get instead is the reasoning while it writes, and then — **the
moment each stage finishes, while you are still standing there** — a plain account of what
it did: any attempt that was refused, in the validator's own sentence; the accepted draft
with its name, its step count, why it is shaped that way, and **the card itself** — the
numbered steps in plain words, in a fenced block, exactly as *How to read a harness card*
below describes it; and what the review pass changed or left alone. The same
accounts are what the journal keeps, so watching live and reopening the design tomorrow
read as the same story — see *What a design writes into its thread* below.

**It is kept, and it stays visible.** Every reply the designer finishes is written to the
node's journal, so opening the design tomorrow shows the writing of the page and not only
the page. Nothing is re-narrated when you walk in: you are handed the reply being typed
right now, and the rest is read off the file, exactly as it works for a worker. A room
never folds its work into a `▸ worked` chip the way the main thread does, so a design that
has already landed still reads as the whole discussion with the outcome at the bottom —
see *Seeing the whole conversation inside a task* on the tasks page.

**While a reasoning model thinks, there may genuinely be nothing to show.** Some models
answer in one burst at the end rather than streaming, and until that burst arrives the room
has the brief at the top and one dim line at the bottom saying what is happening —
`subharness · designing · attempt 1/3 · thinking · 52s`. That line replaces itself in place
and is never written to the journal. When the burst lands, the whole reply appears at once.

**A call that failed outright does say so in the room**, as `error: <reason>`. A design you
stopped, or one whose window ran out, says nothing here — the settle card is already the
account of it.

## Can I see the harness page's JSON or YAML — where is the page source

**You do not, anywhere.** Not while it is being written, not at the end, not in the room and
not in the thread; not as JSON and not as YAML. No person-facing surface prints it. **The
card is the page as a person reads it** — the name, the purpose, the numbered steps and the
bounds, drawn the way every surface draws them. The literal text is kept for the two readers
that need it: the model answering your questions in the design thread, and the registry it is
written to if you approve it. Both are handed it out of sight, as a message no surface draws.

**The conversation gets one line, which is a different question.** In the chat feed the same
design is a single live block that replaces itself: `⠿ subharness · designing · attempt 1/3`,
then `naming it: research-helper`, `4 steps so far`, `thinking · 52s`. A feed you are
holding a conversation in wants one line that stays a line; a room you walked into in order
to watch wants to be told what is happening.

## What a design writes into its thread — the draft, a refusal in plain words, the review's findings

Each of these lands in the room as it happens **and stays in the thread's journal**, so
opening the design tomorrow shows how the page came to be and not only the card at the end
of it. None of it is the page's own text — that is never printed to a person.

**When a draft is accepted**, the thread gets a human account of it. First the line:

```
The draft is written — <name> · <N> steps.
```

then the designer's own justification, as plain prose; then the cues that will reach it:

```
It answers to: <cue> · <cue> · …
```

then the harness card — the numbered step list every surface draws — in a fenced code
block, so it reads aligned.

**A refusal is one sentence, and never the reply that was refused:**

```
Attempt <N> was refused: <reason>
```

`<reason>` is the validator's own sentence; the ones it can say are under *What a design can
be refused for* below. The refused page does go back to the model, privately, so it can
repair exactly what it wrote — a person is shown the news and not the mess.

**The review pass reads as findings.** One of:

```
The review read the draft and changed <N> things:
The review read the draft and left it as written.
```

The first is followed by the findings themselves, as bullets; a review that changed
exactly one says `thing`, not `things`.

**The finished page** lands as `The page is written.`, then the card in its fenced block,
and last the line saying nothing has been kept yet:

```
Nothing is saved yet — the card is up, and it is saved only if it is approved.
```

That line is the truth about the clock as well: the 30-minute window was on the *writing*,
and it stopped when the page landed. The card below it waits with no clock at all — see
*Why a design timed out even though the page was there* below.

## How to read a harness card — what the steps, the indented lanes and the last lines mean

The same block is drawn everywhere: on the card in the conversation that asks you to keep
a design, after `The page is written.`, under `/harness` and `enter`, and in a design's own
room. It is written to be read without knowing anything about how aforge works, so no step
ever names the machinery behind it.

**There is no box around it and no diagram above it.** The card in the conversation used to
draw its own picture — `[plan]──▶[fetch]──▶[verify]`, a bullet per step, and two rows
reading `verify: report` and `tools: read · grep` — inside a bordered frame. All of that is
gone: those were names from inside aforge, shown to somebody who has never seen inside it,
and what stands there now is exactly the block below with `[enter] save   [e] change it
[esc] drop` under it.

```
triage-flake · v2 · chase a flaky test to a fix

1  start    starts when you run it  · takes since, label

2  name-it  name the test that failed and why  · reads files, searches inside files · up to 8 turns

3  pick
   if contains flaky → rerun
     ·  rerun    runs a command — go test -run TestFoo -count 20
     ·  tries    repeat until ok (up to 3 times)
     ·  check    check — go test ./...
     ·  land     asks you — land the fix?
   otherwise → explain
     ·  explain  say why it is not flaky

can use · reads files · searches inside files · runs commands
may pick its own way as it runs, up to 4 times
checks its own work and fixes what it finds
```

- **The head** is the name, the version — or `draft` while nothing has saved it — and the
  one-line description.
- **The numbers are the run's own order**, top to bottom, so reading them is reading the
  run. Each step is its name and what it does; the quiet tail after `·` is what that step
  may reach and how many turns it has.
- **`side by side, one lane each:`** heads work that happens at the same time. Each lane is
  indented under it with a `·` and appears exactly once. Where the lanes come back together
  there is no row at all — the indent simply ends.
- **`if … → name`** and **`otherwise → name`** are a choice. Steps that only one arm reaches
  are indented under that arm rather than numbered as steps of the whole run.
- **`repeat until … (up to N times)`** is a bounded loop, **`asks you — …`** is a stop that
  waits for your answer, and **`check — …`** is the harness testing its own output.
- **The last lines are the bounds**: what it can use, what it may decide for itself while it
  runs, how hard it checks itself, and what it was tried on. A bound that is not in force
  prints nothing, so a short foot means a narrow harness. `can use · nothing` means it
  reaches no tools at all.

Nothing on the card is a name from inside aforge. The tool names on a page — `bash`,
`generate_image` — are shown as what they do (`runs commands`, `makes images`); a tool this
build has never heard of is printed as its own name instead.

## What happens to a design when aforge closes or restarts — does a design survive closing the chat

Two different things, depending on how far the design had got.

**A finished page waiting on your answer survives.** If the page was written and the card
was up — the phase read `awaiting your look` — closing aforge (quit, `ctrl+c`, a crash)
does not throw it away. The page rides the task checkpoint, and the next session raises
**the same card over the same page**: approve it and it saves exactly as it would have
last night. The recovered-graph line counts it as coming back:

```
recovered task graph: 2 done · 1 design asks again · 1 waiting
```

**A design still writing does not resume.** Cut off mid-page, it comes back **failed**,
saying:

```
the design did not finish before aforge closed; nothing was saved
```

That is the literal truth rather than a soft ending. Nothing reaches the harness registry
until you approve the card, and a design cut off while still writing left nothing behind
to continue from — no page, no half-saved entry, no worktree, no branch. The design room
and its thread stay on disk and are still readable; there is just nothing in the registry.

That case shows on the recovered-graph line as:

```
recovered task graph: 2 done · 1 design did not finish (nothing saved) · 1 waiting
```

**Ask for the same harness again and it is designed from the start.** That is one more
design's worth of model calls; there is no partial page to pick up. If designs keep running
out before they finish, ask for a smaller harness — fewer steps, shorter briefs.

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
inventing the rest. What it gets instead is **one continuation turn**: the partial page
goes back to the designer as its own unfinished reply and it is asked to write the
remaining characters, so a big page that hit the completion ceiling is finished rather
than thrown away — complex harnesses are the ones that hit it, and they are usually the
ones asked for on purpose. Only when the continuation fails too is the attempt spent: the
design is told `your reply stopped in the middle: it ran out of completion budget before
the JSON object was closed` and asked for a SMALLER harness — fewer steps, shorter briefs
— on the next attempt.

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
and the design's task settles `subharness "triage-flake" v1 saved`, exactly as it would have a
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

A subharness can live in one of several places, and this is one of them: the pages a design
writes. (The others are the ones built into aforge, `~/.aforge/subharnesses`, and the
project's own `.aforge/subharnesses` — the *Subharnesses* page lists all four.) They are one
list wherever they came from; where a program lives decides only the mark on its row, and a
page written here is marked `yours`.

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
when its page was written. Keep typing and it narrows. `/harnesses ` opens exactly the same
list. `/subharness ` and `/sub ` open the other door onto the same programs: one list of
everything runnable here, and an intake card instead of a chip and a typed request.

```
◆ triage-flake     chase a flaky test · 2h · finished
◆ review-diff      read a diff
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

Type `/harness` or `/harnesses` with nothing after them. Both open the same panel. (A space
after the word opens the picker above instead.) `/subharness` and `/sub` do not open this
panel — they open the other door onto the same programs: everything this conversation can
run, in one list, with an intake card behind each row.

A short list opens under the message box, at most **10** lines:

```
◆ triage-flake       v3 · chase a flaky test · 6 runs · 2h · finished
◆ review-diff        v1 · read a diff
```

(`#` instead of `◆` on the linear palette.)

| Key | What it does |
| --- | --- |
| `↑` / `↓`, also `ctrl+p` / `ctrl+n` | move |
| `pgup` / `pgdown` | move by a page |
| `enter` | open the highlighted harness |
| `esc` | close the panel |

The name and version sit together on the left, because "triage-flake v3" is the sentence
people say. The description, the run count and the last run are the dim tail. `1 run` is
spelled singular.

**The last run is spelled the way `/subharness` spells it**, because it is the same fact
about the same program: `2h · finished`, or `2h · incomplete` when it did not reach the end
it promised. There is no third word — declined at a gate, stopped, or ended by an error are
all `incomplete`, and none of them is called a failure. Ages come off the one clock the
whole product reads with: `now`, `12s`, `5m`, `2h`, `yesterday`, `mon`, `aug 3`.

Nothing is drawn there for a harness with no history — not `never run`, not `0 runs`. An
unreadable newest trace still shows the count, because "ran, and I cannot read the trace"
is a different fact from "never ran".

The registry is read on the keystroke, not held from boot, so a harness registered in
another window shows up. Each row reads the **head** version.

`enter`, or a click on a row, closes the panel and prints the harness's card **into the
conversation** — its steps and its bounds, with the last run's card under it when there is
one. The transcript is where prose lives and can be scrolled and copied. **The card keeps
its own indentation there**: every line lands in the column the card put it in, and one too
wide for the frame is cut with a `…` rather than folded onto a second row, because the
indent under a lane is what says the step belongs to that lane.

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
space, pick it out of the list, and run it on purpose — or open `/subharness`, which lists
it beside everything else you can run here, and start it from its card.

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
and this build has no door onto it from here, so the command names that machine:

```
<machine> owns harnesses · change it on that machine
```

rather than listing this machine's harnesses and offering to run them over there.

For the rest of what does and does not travel over a connection, see the page on running
the session on another machine with `--host`.
