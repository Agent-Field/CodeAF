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

A reminder fires once, says one line into this conversation, and retires. Its
cap is **1 firing a day**, because a moment cannot happen twice.

The line arrives in the conversation that asked for it, looking like this:

```
◦ remind me at 6 to leave: time to leave
```

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
moment when it answers on somebody's behalf. It waits until you press `1` yes,
`2` change it, or `3` do it once.

It does not have to be answered in that window either. A window sitting on this
card says so on **home**, and the row there carries its `1 yes` and `3 once`, so
the card can be answered from the dashboard without opening the conversation
(home's own page has the whole rule). `2 change it` stays here, where there is a
box to say the new when into.

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

The result is waiting in the conversation that asked for it. Close the lid.

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

## Why did I get this?

Every standing item remembers the conversation that made it, so the answer to
"why did I get this?" is always a conversation you can open.

If the conversation was open when it fired, the line arrived in it as it
happened. If it was not, the news waited, and the next time you open that
conversation it is folded into **one** note that begins `while you were away` —
one line per thing, with when, your own words, what happened, and the run folder
to open for the whole story. Two hours away with nothing to report is nothing at
all: silence is the design.

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
  and aforge does the thing in front of you instead.
- **It will not fire while the machine is asleep.** A late check says it was
  late; it does not pretend it happened on time.
- **It will not reach your phone.** There is no notification, no email, no
  outward lane at all. News lands in the conversation that asked for it.
- **It will not spend past the rails without asking.**
- **It cannot do anything unattended that you have not already allowed.** Nobody
  is there to answer a permission card, so the run stops and says so.
- **A firing cannot set up another standing item.** The tool is absent inside
  one: nothing that runs on its own may arm something else that runs on its own.
- **Nothing is armed by a matcher.** Nothing runs because a phrase looked like a
  rule; every single one of these was a card you said yes to.
