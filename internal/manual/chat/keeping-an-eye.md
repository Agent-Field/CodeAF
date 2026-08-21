# Keeping an eye on things — reminders, watches, rules and overnight work

Some things you say are not work for right now. "Remind me at 6 to leave", "tell
me when CI on main goes red", "every Monday draft the weekly update", "keep main
green", "tonight run the full suite" — each of those asks aforge to leave
something behind that keeps working after this window is closed.

The tool behind all of them is `stand`. You never type it; aforge recognises the
words. Nothing is ever set up without a card you answer.

## Remind me about something

Say it the way you would say it to a person: "remind me at 6 to leave", "remind
me on Friday to send the invoice". A card comes up with your own sentence on it,
when it will fire, and what it costs. Say yes and it stands. The card has **no
countdown on it** — it waits for you — and nothing is set up until you answer.

A reminder fires once, says one line to you, and retires. Its cap is
**1 firing a day**, because a moment cannot happen twice.

The line looks like this, wherever it reaches you:

```
◦ remind me at 6 to leave: time to leave
```

Which chat it lands in is the next section.

## Where a reminder arrives — the chat you are in, or the next one you open

A firing reaches **you**, not one particular window. aforge tries four addresses
in this order and stops at the first one that ends at a person:

1. **The conversation that asked for it, if it is open.** The line arrives in it
   as it happens, exactly as a finished task's news does.
2. **Any other conversation of the same project that is open**, the one you most
   recently opened or spoke in first. This is what happens when the chat that set
   the reminder up has been closed — and it is always what happens when you asked
   from home with `ask here`, because that exchange is a pane on the home screen
   and not a room you sit in. It is never steered into.
3. **The conversation's own inbox**, when nothing is open. The next time you open
   that conversation it is folded into one note beginning `while you were away`.
4. **The project's inbox**, when nothing is open *and* it was an `ask here`
   errand. An errand has no row on home to come back to, so the news is filed
   under the project instead: it shows on home under that project as
   `◆ N things since you left`, and the next ordinary conversation you open in
   that project folds it into its own `while you were away`.

So a reminder set from home while you are working in a chat in the same window
arrives **in the chat you are working in**. Nothing is ever delivered only into
the `ask here` pane, and nothing is ever left in a file no screen reads.

**What is drawn, and when.** In whichever chat it reaches, a firing draws one dim
row of its own:

```
◦ remind me at 6 to leave · said: time to leave
▲ keep main green · needs your look: the fix touches migrations
```

Live — roads 1 and 2, a chat that is open — the row is drawn **the moment the
firing arrives**. It does not wait for the chat to answer it, and it does not
wait for you to type: it appears while you are sitting there. The chat is woken
by the same firing, so a reply usually follows underneath a few seconds later,
and that reply is an ordinary turn you can read, scroll and interrupt.

Waiting — roads 3 and 4, nothing was open — the rows are drawn **when you open
the conversation**, one for each thing that was waiting, oldest first, before
anything you type. The chat is also handed the same news as one `while you were
away` note so it can answer questions about it.

The row leads with your own sentence cut to its first six words, so
`remind me in 1 minute to drink water` draws as
`◦ remind me in 1 minute to · said: 💧 Time to drink water!`. The whole sentence
is in the transcript, and on the item's own row on home.

There is no phone, no email and no desktop notification — see the last section of
this page.

## Why did it run date before setting the reminder — it knows the clock now

It does not any more. aforge tells the model the time in its own instructions, as
one line:

```
- Now: 2026-08-21 06:52 -04:00 (America/New_York, Friday)
```

The local time to the minute, the numeric offset, the zone by name and the day of
the week. Before that line existed, "remind me in 2 minutes" began with a
`bash date +"%Y-%m-%dT%H:%M:%S%z"` — a tool row you could see, spending a step to
read a clock the program already had. The instructions now say plainly: never run
`date` to learn the time.

For a moment you name — "at 6", "tomorrow at 9am" — aforge works the stamp out
from that line itself. For a distance from now — "remind me in 2 minutes" — it
sends the **duration** instead and aforge resolves it against the real clock at
that instant, then says back the moment it landed on. That is what the card
shows:

```
in 2 minutes — 06:54
```

so you can check the time it settled on without reading a timestamp.

A session that has been open for hours cannot drift on this. The `Now` line is
stamped when a turn opens and **re-stamped** on any turn that opens more than ten
minutes after the last stamp, anything relative is resolved against the real clock
when you ask rather than from that line, and every `stand` result ends with
`now: 07:34 -04:00` whatever else it said. A moment that has already passed is
refused outright — see "Why did it set my reminder for a time that already
passed".

## Why is there no once on my reminder card — a one-off reminder has two answers

Because "once" would not be a smaller version of what you asked for; it would be
a different thing at the wrong moment.

`3 once, not standing` means **do the action now, as an ordinary turn, and leave
nothing behind**. For a watch, a rule, a routine or overnight work that is a real
answer: you wanted the tests run, not the arrangement. For "remind me at 6 to
leave" the whole content of the request is the **6** — doing it now says
`time to leave` hours early, or says nothing at all. So the card does not offer
it there:

```
│ [ 1 yes, set it up ]  [ 2 change when ]
```

Two chips, `1` and `2`, and `3` does nothing — on the card in the conversation,
on the card in home's `ask here` pane, and on home's own answer row. The hint
under the box says so too: `1 yes · 2 change when · esc no`. `esc` still declines
the whole thing, as it does on every card.

Everything else keeps all three: a watch, a rule, a routine, overnight work.

## Why did it set my reminder for a time that already passed — it cannot any more

It cannot. A moment that has already gone is **refused before the card is ever
drawn**, and the refusal tells aforge what time it is now so it can work the
stamp out again:

```
Invalid arguments: when.at 05:42 -04:00 has already passed — it is now 07:34
-04:00 (Friday 2026-08-21). For a distance from now send when.in ("1m"); for a
clock time compute it from now.
```

A stamp up to **30 seconds** behind the clock is still taken: that is arithmetic
that was right when it was done, and firing at once is what you asked for. Two
hours behind is not. An `expires` in the past is refused the same way.

This used to be possible. A session that had been open for hours carried the time
it *opened* with in its instructions, so "remind me in 1 minute" was worked out
from a stale stamp and landed in the past. Two things changed:

- The `Now` line in aforge's instructions is **re-stamped** whenever a turn opens
  more than ten minutes after the last one, so the clock does not drift over a
  long session.
- **Every `stand` result ends with the real time**, on its own line:
  `now: 07:34 -04:00`. Whatever the model was told when the turn opened, it is
  told the true time at the moment it matters.

And "in 1 minute" no longer needs a stamp at all: aforge sends the **duration**
and the engine resolves it against the real clock. A one-minute reminder is an
ordinary standing one-off — if you are ever told aforge "cannot hold a
1-minute timer", that is wrong, and this is the page that says so.

## Tell me when something happens

"Tell me when CI on main goes red." "Tell me when the docs site stops returning
200." "Tell me when the cert on api.example.com has under 14 days."

A watch takes a look at the world on a timer and a small, cheap model reads what
it found against your own words. It speaks **only when the answer is yes** — a
check that found nothing writes nothing anywhere, which is why a watch that ran
faithfully for thirty mornings and found nothing reads differently from one that
never ran at all.

The look is either a shell command run in this project, or one of aforge's own
tools — including a tool one of your connected accounts brought (see the
accounts page). One look is bounded at **60 seconds**; what it printed is
clipped to the last **8KB**, because the news in a command's output is at the
end of it.

The judgement is one sentence in plain words: "the last run on main failed". It
carries the last few things it said, so something you have already been told
about is not raised again every five minutes. When the answer is neither a clear
yes nor a clear no, aforge treats it as a no and the log says
`there was no clear answer, so nothing was said`.

## How long do I have to answer the card — the card does not time out

As long as it takes. The card carries **no clock**: no countdown, no bar, and no
moment when it answers on somebody's behalf. It waits until you press `1 yes,
set it up`, `2 change when`, or — where the card offers it — `3 once, not
standing`.

A **one-off reminder's card has only two answers**; the next section says why.

It does not have to be answered in that window either. A window sitting on this
card says so on **home**, and the row there carries the same chips the card is
offering — `1 yes` and `3 once`, or just `1 yes` on a one-off reminder — so the
card can be answered from the dashboard without opening the conversation (home's
own page has the whole rule). `2 change when` stays here, where there is a box to
say the new when into.

That is the opposite of a task proposal's card, which does count down and starts
the work on silence. A task is bounded work somebody is watching; a standing
item spends money on its own, at times nobody chose, so silence may never arm
one.

What ends a card unanswered is the turn ending — `esc`, or the window closing.
Then **nothing is set up**, and aforge is told exactly that:
`the card was left unanswered — nothing was set up`. That is not a refusal
anybody made; it is a card nobody reached.

A session nobody is watching — a headless `--once` run, a task, a firing's own
run — cannot draw the card at all, and answers
`nobody is here to say yes — this can only be set up in a conversation`.

## Every morning, every Monday — routines

"Every Monday at 9, draft the weekly update from the git log." "Every night, one
line on what changed in this repo." "At the end of each day, write two lines on
what I committed."

A rhythm is written as a cron line (`0 9 * * 1`) or as a plain interval (`20m`,
`2h`) — never shorter than a minute. Whatever the spec says, the card shows you
the cadence **in words** ("Mondays at 9am"), because a cron line read back is
something nobody can check.

If aforge invented a cadence because you did not give one, the card **asks**
rather than states: "about every 2 minutes — you didn't say, so that's my guess.
Right?"

## Keep main green — rules you stop thinking about

"Keep main green." "Whenever a PR opens, review it and leave a summary."
"Whenever I push a branch, run the tests before I open the PR."

A rule's firing is real work: a brief, run headless in this project, in a session
folder of its own with its own transcript and its own cost row. It runs under
**the approval rules you have already banked in your profile** — not under a
repository's own rules, because a checked-in file must not be able to widen what
runs while you are asleep.

Anything that would have stopped to ask you refuses instead, the run stops, and
the item is marked as needing you. That is the whole of "unattended": what you
already allowed is what runs, and nothing else.

One firing's work is bounded at **60 tool calls** unless the card said otherwise,
and at its per-run cost cap. Crossing either cuts the turn where it stands;
whatever it already did stands with it.

## Tonight — overnight and long-horizon work

"Tonight, run the full suite and the benchmark and have a report for me in the
morning." "Later when it's idle, look into the flaky test."

`tonight` and `later when idle` are both real shapes. An idle item waits until
nobody is working anywhere on this machine and nobody has typed for the span you
named — the machine being quiet, not a clock.

The result is waiting for you — in the chat that asked for it if it is open, and
otherwise wherever the delivery rules put it (see "Where a reminder arrives").
Close the lid.

## What does it cost?

Two figures, both quoted on the card before you answer, both of them yours to
change by saying so:

| Rail | Default | What it bounds |
| --- | --- | --- |
| per run | **$0.15** | everything one firing spends — the look, the judgement, the work |
| a day | **10** firings | how many times it may fire in one local day (**1** for a one-off reminder) |

Above those sits your **daily budget** — the same `daily_budget_usd` row `/budget`
and the settings sheet already write, not a second number invented for this. The
ambient side spends inside it or asks.

A check that fires nothing still costs the look and the one small judgement call,
which is why the cap is quoted per run and not per firing.

## Does it keep working when I close the terminal or shut the laptop?

While any aforge window is open, one of them runs the pass every **5 minutes** —
whichever window takes the lock first; the others do nothing and say nothing.

For "no terminal open at all", aforge asks **once, ever**, the first time you set
anything up: keep checking when no window is open? A yes installs an OS user
timer that runs `aforge tick` every 5 minutes — a launchd agent on macOS
(`ai.agentfield.aforge.tick`), a systemd user timer on Linux
(`aforge-tick.timer`) — and the answer is written down **before** your machine
is touched, so an install that half-worked is still an install you were asked
about, and you are never asked twice. A no is remembered as a no and never
raised again.

On any other host there is no timer to install, so the offer is never made and
standing items are checked only while a window is open.

## How do I stop one?

Say so: "stop the CI one", "stop reminding me about the plants". You can name it
by its id or by any part of your own sentence — nobody remembers an id.

Stopping is permanent. aforge answers:

```
stopped: tell me when CI on main goes red
It will not fire again. Setting it up afresh is a new card.
```

If your words match more than one, it will not guess — pausing the wrong watch is
a silence you would not notice until it mattered — so it answers
`"CI" matches more than one — say which:` and lists them.

## How do I pause one and start it again?

"Pause the weekly update for now" and "start the weekly update again". A paused
item is not checked and not fired, and keeps everything else it knows: what it
has cost, when it last fired, what it last saw.

To see what stands here, ask what you have set up. Each row leads with the glyph
every aforge screen uses — `▲` needs you, `●` firing now, `◦` waiting for its
time, `∙` paused or stopped — then your own words, then the cadence in words. A
project with nothing set up answers `Nothing stands in this project yet.`

## Where is the record of a reminder I made from home

If you set it up from the home screen with `ask here`, the short exchange that
produced it is a real conversation with a real transcript — home just does not
list it, so finished errands cannot fill up the screen you typed them at.

The moment something stands, that folder is filed **under the thing it made**:

```
~/.aforge/v3/standing/<item id>/exchange/transcript.jsonl
```

and the pane says `kept · this exchange is filed under it`. The item's own
record points there, so "why did I get this?" opens the exchange that made it,
the same way an item made in a conversation opens that conversation. Runs of the
same item are numbered folders beside it, under
`~/.aforge/v3/standing/<item id>/runs/`.

An errand that came to nothing stays where it was made,
`~/.aforge/v3/standing/exchanges/<id>/`, until the sweep clears it. See the
asking-from-home page for the rest of that door.

## What gets cleaned up, and when

Once per launch aforge sweeps its own folders. On the ambient side it removes
exactly two things, and only after **7 days** with nothing touching them:

- an **errand that came to nothing** — a folder under
  `~/.aforge/v3/standing/exchanges/` that never became a standing thing and was
  never continued as a conversation;
- a **run that delivered nothing** — a firing that said nothing, landed nothing
  and left nothing waiting for you. Each run writes down what it came to, and
  only the word `nothing` may be swept.

Nothing else is ever removed. Not your items, not the ledgers, not a run that
said something, landed something, failed, or is waiting for you; not a run whose
folder never said what it came to; not a folder something still holds open; and
never a conversation under `~/.aforge/v3/projects/`, whatever its age.

## A reminder fired but nothing showed up in my chat

It should show up, and in this build it does. A firing that reaches an open chat
draws its own row there as it arrives:
`◦ remind me at 6 to leave · said: time to leave`. If you were away, the rows are
drawn when you open the conversation. "Where a reminder arrives" above has the
whole order and what each road draws.

If a firing really did leave nothing on your screen, these are the ordinary
reasons, in the order worth checking:

- **It went to a different chat.** A firing reaches *you*, not one window. If the
  chat that set it up was closed, the line went to another chat of the same
  project that was open — including one in a different pane or a different
  terminal. It is never delivered into the `ask here` pane on home.
- **Nothing was open when it fired**, so it is waiting: home shows the project
  with `◆ N things since you left`, and the rows appear the moment you open a
  conversation there.
- **It has not fired yet.** A window checks its items every five minutes, the
  first check five minutes after it opened, so a one-minute reminder can arrive
  up to five minutes late. `/status` shows `keeping an eye on N` while something
  stands.
- **The check found nothing to say.** A watch or a rule that looked and found
  nothing writes nothing at all — that is the design, not a fault. The item's own
  record on home says when it last ran.
- **Nothing was ever set up.** A card left unanswered sets nothing up:
  `the card was left unanswered — nothing was set up`.

Ask "what is standing?" in any chat to see the list, when each last ran, and what
it came to.

## Why did I get this?

Every standing item remembers the conversation that made it, so the answer to
"why did I get this?" is always a conversation you can open.

If the conversation was open when it fired, the line arrived in it as it
happened. If it was not, it went to whichever chat of that project you did have
open; and if none was, the news waited, and the next time you open a conversation
there it is folded into **one** note that begins `while you were away` — one line
per thing, with when, your own words, what happened, and the run folder to open
for the whole story. Two hours away with nothing to report is nothing at all:
silence is the design. The four addresses in order are under "Where a reminder
arrives".

A firing's run folder is a normal session folder outside your projects, so its
transcript reads with the same tools as any other conversation.

## What it will not do

- **It will not set anything up when nobody is watching.** A headless `--once`
  run, a task, and a firing's own session all answer
  `nobody is here to say yes — this can only be set up in a conversation`, and
  the tool is not even on the list there.
- **Silence arms nothing.** The card waits with no clock on it, and a turn that
  ended with it still up leaves nothing behind:
  `the card was left unanswered — nothing was set up`. This is the opposite of a
  task proposal, where silence starts the work: a task is bounded work somebody
  is watching, and a standing item spends money at times nobody chose.
- **A "do it once" answer sets nothing up.** It answers
  `do it once, now, as an ordinary turn — nothing stands. Nothing was set up.`
  and aforge does the thing in front of you instead. A **one-off reminder's card
  does not offer that answer** — see "Why is there no once on my reminder card".
- **It will not set a reminder for a moment that has already passed.** The stamp
  is refused with the current time in it, and aforge is asked to work it out
  again from that.
- **It will not fire while the machine is asleep.** A late check says it was
  late; it does not pretend it happened on time.
- **It will not reach your phone.** There is no notification, no email, no
  outward lane at all. News lands in a chat you have open, or waits on home and
  in the next chat you open in that project ("Where a reminder arrives").
- **It will not spend past the rails without asking.**
- **It cannot do anything unattended that you have not already allowed.** Nobody
  is there to answer a permission card, so the run stops and says so.
- **A firing cannot set up another standing item.** The tool is absent inside
  one: nothing that runs on its own may arm something else that runs on its own.
- **Nothing is armed by a matcher.** Nothing runs because a phrase looked like a
  rule; every single one of these was a card you said yes to.
