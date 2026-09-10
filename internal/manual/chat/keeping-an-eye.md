# Keeping an eye on things — reminders, watches, rules, overnight work, and checking on it later

Some things you say are not work for right now. "Remind me at 6 to leave", "tell
me when CI on main goes red", "every Monday draft the weekly update", "keep main
green", "tonight run the full suite" — each of those asks aforge to leave
something behind that keeps working after this window is closed.

The tool behind all of them is `stand`. You never type it; aforge recognises the
words. Nothing is ever set up without a card you answer.

## It asked while I was away

**Ten minutes with nobody touching the keyboard** makes a window away — measured from the
last key, not from which window is in front.

After that, a question this project has a rule for may take its own recommended answer,
and its receipt says `aforge, on your settings` decided it. Only a **reversible** question
with a recommended answer can go that way. Everything else stays open: it is on home, the
desktop notification says the conversation is `waiting on you`, and the terminal bell rings
**once** — only for a question something is blocked on, and never twice for the same one.

Two kinds never run on a clock however you set it up: **confirmation always asks**, because
it is what is asked before something destructive, and **clarification never runs on a
clock**, because the answer is something only you have.

`/autonomy` shows and changes those rules, per project. A question that is about to be
taken by one says `your rule` on its own row while the clock runs — there are no hidden
rules.

## Remind me about something

Say it the way you would say it to a person: "remind me at 6 to leave", "remind
me on Friday to send the invoice". A card comes up with your own sentence on it,
when it will fire, and that it shares the day's allowance. Say yes and it stands.
The card has **no countdown on it** — it waits for you — and nothing is set up
until you answer.

A reminder fires once, says one line to you, and retires.

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
   errand. An errand has no conversation to come back to, so the news is filed
   under the project instead, and the next ordinary conversation you open in that
   project folds it into its own `while you were away`. At phone width home shows
   it under `since you left`; on a wider frame the conversation is where it
   surfaces.

So a reminder set from home while you are working in a chat in the same window
arrives **in the chat you are working in**. Nothing is ever delivered only into
the `ask here` pane, and nothing is ever left in a file no screen reads.

**What is drawn, and when.** In whichever chat it reaches, a firing draws one dim
row of its own:

```
◦ remind me at 6 to leave · said: time to leave
? keep main green · your call: the fix touches migrations
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

`3 just once` means **do the action now, as an ordinary turn, and leave
nothing behind**. For a watch, a rule, a routine or overnight work that is a real
answer: you wanted the tests run, not the arrangement. For "remind me at 6 to
leave" the whole content of the request is the **6** — doing it now says
`time to leave` hours early, or says nothing at all. So the card does not offer
it there:

```
?  wants to keep an eye on: remind me at 6 to leave
     1  yes, set it up   it keeps happening until you stop it
     0  no               nothing happens, now or later
```

Two answers, `1` and `0`, and `3` does nothing — on the question in the
conversation, in home's `ask here` pane, and on home's own answer row. The hint
under the box says so too: `1 yes, set it up · 0 no · esc later`. The **way out
is still drawn**: `0 no` is on every standing card there is.

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
hours behind is not. An `expires` in the past is refused the same way — and so
is an `expires` that stands before the reminder's own moment, which the next
section is about.

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

## Can I give one an end date — expires, and why an end before the first firing is refused

Yes. Anything that stands can carry an **end**, and the check that runs it
retires it on that day: "watch the build until Friday", "never touch the public
API until the release lands". Leave it out and it stands until you stop it.

**The end has to be later than the first time the thing would fire**, and aforge
refuses one that is not, before any card is drawn:

```
Invalid arguments: rails.expires 23:11:00 -04:00 is not after when.at 23:11:11
-04:00, so it would retire before it ever fired. Put it after that moment, or
leave it out — a one-off retires as it fires and needs no end at all.
```

For a routine the same sentence names `its first firing` instead of `when.at`,
because a rhythm has no single moment to edit.

This is not fussiness about a second. The check asks **"has it run out of
time?" first**, before it asks whether anything is due, so an end at or before
the moment kills the item by every road there is — the window's own pass, the
background timer, `aforge tick`, all of them. It would have run **0 times** and
been marked `expired`, and the only trace would be a line in its own log:
`its time ran out — no longer watching`.

The mistake it exists to catch is arithmetic, not carelessness: a reminder said
as "in 1 minute — 23:11" lands at 23:11:11, and an end taken from the same words
lands at 23:11:00 — eleven seconds too early. That is why the refusal spells
both stamps out to the second.

**And the end has to outlive a check, not just the moment.** Nothing watches an
item continuously: a check runs every five minutes, so an end that falls between
the moment and the next check is found expired at the same instant it would have
been found due. An end twenty-five seconds after a one-minute reminder is after
the moment and still dead, and the second refusal says so:

```
Invalid arguments: rails.expires 21:05:00 -04:00 is less than one check after
when.at 21:04:35 -04:00, so a check can find it out of time at the same moment
it would have found it due. Checks are 5m0s apart. Put the end at least that far
after the moment, or leave it out — a one-off retires as it fires and needs no
end at all.
```

**A one-off reminder never needs an end.** It retires the moment it fires, and
if nothing ever picks it up it stops being watched a day after its moment
anyway.

## Tell me when something happens — watch for it, and tell me when the build breaks

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
set it up`, `0 no`, `c` to change it, or — where the card offers it —
`3 just once`.

`esc` **puts it off and does not answer it**. The question folds away so you can
type, the work stays waiting on it, and the count beside the box goes on
counting it. Press the chip beside that count to bring it back. That is a
change: `esc` used to be the outright no on this card, and the outright no is
`0 no` now — one visible answer rather than a key you had to know.

A **one-off reminder's card has only two answers**; the next section says why. It
still takes the decline.

It does not have to be answered in that window either. A window sitting on this
card says so on **home**, and the row there carries the same answers the card is
offering — `1 yes`, `3 once` and `0 not set up`, or `1 yes` and `0 not set up` on
a one-off reminder — so the card can be **answered or declined** from the
dashboard without opening the conversation (home's own page has the whole rule).
`c change when or where` stays here, where there is a box to say the new time or
place into.

## I don't understand these options — what each answer on the card does, how to change the time on a standing card, and how to cancel

The card in the conversation shows what is being proposed — your own words, when it would
wake, where it reaches, what it costs. The **question sits above the message box**, where
every decision on this screen is put, and every answer carries what it costs beside it:

```
?  wants to keep an eye on: every Monday at 9, post the standup note from the git log
     1  yes, set it up   it keeps happening until you stop it
     3  just once        it happens now, and nothing is kept
     0  no               nothing happens, now or later
   [enter] take the pick · [c] change · [esc] later
```

- **`1 yes, set it up`** — it gets set up and starts happening, and goes on until you stop
  it. The `when ·` and `where ·` bands on the card above say when and how far.
- **`c` change when or where** — you want it, but not like that. Press `c` and the box below
  becomes a place to say the **time or the place** instead — "make it 8", "only in this
  project", "everywhere" — and `enter` sends your words back. Nothing is set up until a new
  card comes with them in it. (This was `2 change when or where` on the card's own row of
  chips; `c` is the key every question on this screen uses to say "I will take one of these,
  but not as it stands".)
- **`3 just once`** — do the thing now, this once, and keep nothing. Some cards do not
  offer it; the section above says why.
- **`0 no`** — **this is cancel**. Nothing is set up, nothing is run, and the card settles
  as `not set up`.
- **`esc`** — later. Nothing is decided, the rows come off the screen so you can type, and
  the question is still counted beside the box. It used to be the outright no here; it is
  not any more.

Each answer is a row of its own and **a click anywhere along it takes that answer**. The
digit takes it too.

Answering leaves a line where the question was — `decided wants to keep an eye on: … → yes,
set it up · you · 14:02` — and the card in the conversation settles with the answer and what
it came to on its bottom edge: `yes, set it up · set up`.

## How do I decline a standing card or say no to a reminder — 0, esc, or the no on the card

`0 no` is the way out, and it is one keystroke and **one visible answer** everywhere
a card like this is drawn: in the conversation, on home's answer row, and in
home's `ask here` pane. Nothing is created, nothing is run, and the card settles
as `not set up`. On home's answer row the chip reads `0 not set up`, which is the
same answer said as its outcome, because that row has no card under it to settle.

**`esc` is not the no.** It used to be, in the conversation, and it is *later*
now: the question folds away, nothing is decided, and the count beside the box
goes on counting it. That is the same thing `esc` does to every question this
program asks, and it is why the no had to become something you can see and click.

It is a `0` and not a `4` because the answers are numbered by where they sit —
`1 yes`, `3 just once` — so a fourth digit would move under your hand on a card
that drew one answer fewer. `0` is off the end of that numbering, on every card,
always the same answer.

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
and by a quiet per-run backstop. Crossing either cuts the turn where it stands;
whatever it already did stands with it.

**And a firing that turns out to be wide can split itself.** If the brief you
banked already names many separate items — "every night, bring the eleven
adapters up to the new interface" — the firing starts as one worker that is
allowed to hand the parts out under itself, exactly as a task you typed can (the
tasks page, *a task that splits itself*). The same tests apply and none is
skipped because nobody is watching: there has to be a lane free for the parts,
and any width floor you turned on still counts. A brief that names nothing to
count never gets the option, and a run that is refused simply carries on as one worker — unless the reading
finds that what is left is work only a person can do, which stops it and leaves the firing
needing your look in the morning (the tasks page, *a task that landed needing your look
without doing anything*). The firing
does not finish until its parts are home, their work is folded into the one
account you read in the morning, and everything they spent is on this run's cost
row.

What a firing gets is the read of its own words and nothing else. There is no
sizing call before an unattended run: asking a model every night whether a
sentence that has not changed since yesterday is wide would be a bill you never
agreed to.

## Always, never, we do it this way — a rule with nothing to wake it

The rules above all have a trigger in them: a PR opening, a push, main going red.
Plenty of rules have none. "Always run the tests before you tell me it works."
"Never touch the public API here." "We use tabs." "Prefer small commits."

A rule like that is not checked and never fires — it **holds**. It costs nothing
and it has no cadence, so its card carries no `when ·` line and no `costs ·`
line: your sentence and how far it reaches is the whole of it. Instead of waking,
it rides into the work it governs — every new conversation and every task that
starts where it reaches opens already knowing it.

On any screen that lists what is standing, one of these says `holds` where a
watch would say what it last found, and on the home screen it sits after
everything that has an appointment. The standing-orders page has the rest: how to
say one, and how aforge decides an "always" is a rule rather than an instruction
for the job in hand.

## Tonight — overnight and long-horizon work

"Tonight, run the full suite and the benchmark and have a report for me in the
morning." "Later when it's idle, look into the flaky test."

`tonight` and `later when idle` are both real shapes. An idle item waits until
nobody is working anywhere on this machine and nobody has typed for the span you
named — the machine being quiet, not a clock.

The result is waiting for you — in the chat that asked for it if it is open, and
otherwise wherever the delivery rules put it (see "Where a reminder arrives").
Close the lid.

## When will it run next, and how often does it check — every hour, every five minutes

Two different clocks, and mixing them up is why an item can look late when it is not.

**Everything standing is checked every five minutes.** That is the pass: whichever of an
open aforge window or this machine's own timer gets there first walks everything you have
standing and asks which of them the world has something to say about. The two run exactly
the same work, so it does not matter which one is awake — and with no window open at all,
the machine's timer still does it (*Does it keep working when I close the terminal or shut
the laptop?* below).

**An item fires on the cadence you gave it, not on that pass.** "Every hour", "every
Monday at 9", "at 6 tonight", "whenever these files change" — the pass is only how often
aforge looks; your own words are what decides whether looking finds anything. So an hourly
item is checked twelve times an hour and fires once, and a rule with no cadence at all is
never woken by the clock — something has to happen first.

**Which means a firing can be up to five minutes late.** A window's first check is a whole
five minutes after it opened — nothing is checked at the moment you launch — so a reminder
set for one minute from now arrives on the check after it comes due, not on the second.
Nothing is checked while the machine is **asleep**, and nothing is checked when **you are
not logged in**: the timer runs under your own login and is not a system service. The
machine's own timer is asked to catch a missed check up rather than skip it, so a laptop
that was shut picks the pass up when it comes back — once, not once for every check it
slept through. What was actually missed is on the item itself: its row says when it last
ran.

The card you said yes to states the cadence back to you in words — "Mondays at 9am" — and
an item's own row says when it last ran. Those two together are the honest answer to "when
will it run next".

## How far does it reach — just this chat, this project, or everywhere

Everything you set up has one **reach**, and the card names it before you answer:

| Reach | What it governs |
| --- | --- |
| this conversation | this chat alone, and it goes when the chat does |
| this project | every conversation and every task in this project |
| everywhere | everything you do on this machine |

**Where you said it is the default.** Said in a project, it stands for that
project — which is what everything set up before reaches were spelled already
did, so nothing you already have has changed. Said in a chat that belongs to no
project at all, it stands everywhere.

**Your own words move it.** "Just this chat" and "only here" keep it to the
conversation; "everywhere", "all my projects" and "on this machine" lift it to
the whole machine. Nothing is ever widened quietly — the card states the reach,
and "make it just this project" on the card is a correction like any other.

Two smaller things ride the same card. A short **title** — three or four words,
like `main stays green` — is what a row too narrow for your sentence falls back
to; your own sentence is still what every screen leads with, and it is never
rewritten. And if you say what acting on it may do without asking — "open a pull
request but never merge it" — that sentence is written down and shown on the
card. It is a record you can read: what actually bounds a firing today is still
the shared allowance, quiet backstops and the permissions you have already banked.

## What does it cost — one daily allowance, not money per order

Everything standing shares one machine-wide daily allowance: the same
`daily_budget_usd` row `/budget` and the settings sheet already write. When that
setting has a dollar figure, the card says `shares the day's $… allowance`; when
it is unlimited, the card says `shares the day's allowance` without inventing a
figure.

There are quiet per-run and per-day backstops inside each item so one noisy order
cannot consume the pool unchecked, but aforge does not ask the model to negotiate
them and does not quote them on an ordinary card. The real protection is the
shared allowance and the approval rules you already banked. If you said "spend at
most a dollar" or named a daily firing limit yourself, that is different: the
card says your limit back exactly, and the item keeps it.

A check that fires nothing still costs the look and the one small judgement call,
so it shares the allowance even when it has nothing to say.

## How many times did my watch run this week, and what did it spend?

Every firing and every check writes one line to a ledger — one file per local
day, under the standing folder — and that is where these counts come from.

On **home**, put the cursor on the item. Its card says both figures:

```
4 runs · spent $0.08
ran 3 times this week · $0.04
```

The first line is everything it has ever done. The second is **the last seven
days**, and a cursor on the whole project sums the same seven days over all of
its items: `4 runs this week · $0.06`, at the foot of that card's `keeping an
eye` band.

A **check that found nothing does not count as a run** — it costs the look, so it
counts in the money and never in the runs, which is the same rule the daily rail
uses. An item that has done nothing in the last seven days shows **no weekly
line at all**, and nothing anywhere reads `0 runs this week`.

In a conversation, asking "what do you have standing?" lists what stands here
with its rails; the standing place, and an item's card on a frame wide enough to
draw one, are where the counts are.

## Does it keep working when I close the terminal or shut the laptop?

**Yes — background checks are on out of the box, and nobody asks you first.**

While any aforge window is open, one of them runs the pass every **5 minutes** —
whichever window takes the lock first; the others do nothing and say nothing. One
other piece of quiet work rides that same pass: at most once every six hours, and
only while nobody has typed anywhere for a quarter of an hour, it tidies the notes
kept across sessions (`what I remember`).
For "no terminal open at all", the **first thing you ever set up** installs one
small timer under your own login that runs `aforge tick` every 5 minutes: a
launchd agent called `ai.agentfield.aforge.tick` on a Mac, a systemd user timer
called `aforge-tick.timer` on Linux. Nothing else is installed, ever — no
server, no port, no account.

You are told, once, in one dim line under the card you just said yes to:

```
checks every 5 minutes, window or not · background checks under /settings
```

If the install did not take, the line says that instead, with the reason:

```
could not install the background check · <what your machine said> · background checks under /settings
```

Either way it is said **once, ever**. The fact is written down beside your items
**before** your machine is touched, so an install that half-worked is still one
you were told about rather than one you are told about again tomorrow.

**The limits are real.** Nothing runs while the machine is **asleep**; the timer
is asked to catch one missed check up when the machine comes back rather than
skip it. Nothing runs when **you are not logged in**: it is a per-user timer,
not a system service. And
on any host that is neither macOS nor Linux there is no timer to install, so
things are checked only while a window is open and the settings row is not there
at all.

## Do the background checks run before I set an API key — the walk happens, the judgment does not

**Yes, the pass still walks.** On a machine that has never been given a key, the
5-minute pass behaves exactly as it would with one: whichever window or timer takes the
lock first goes through every reminder, watch and routine you have, and any other that
wakes at the same moment finds the lock held and leaves without doing anything. What
changed is that leaving quietly, and walking at all, no longer need a key first — a pass
that will do nothing costs nothing and needs nothing.

**A reminder at a time still fires.** "Remind me at 6 to leave" is a question about the
clock and not about the world, so nothing has to be judged: at 6 the line is delivered
to the window you are sitting in, or waits for you on home.

**A watch that has to judge something stops on its own row.** "Tell me when the build
goes red" runs its command first — that part costs nothing and needs nobody — and then
needs a model to say whether what came back means yes. With no key, that item's `last
look` reads

```
could not check: no API key: this session has not been given one yet
```

the walk carries straight on to the next item, and nothing fires, because a firing
needs a yes and nobody was able to say one. The item still records that it looked, so
the count of what was examined is honest and the record of the pass carries one error.

Set the key — `/settings` → **openrouter key**, or say "set up my api key" — and the
next pass judges normally. Nothing has to be re-made and nothing was lost while there
was no key.

## Turn background checks off

`/settings` → **Workspace** → **background checks**, or just say "turn off the
background checks" and the chat will do it. It is a two-word row — `on` or
`off` — and `on` is the default.

- **on** installs the timer named above and things are checked with no window
  open.
- **off** removes it. Nothing is lost: your reminders, watches and routines are
  all still there, still due, and still checked every 5 minutes by any window you
  have open. What stops is the checking that happens when you have none.

**The row reads your machine, not a file.** It says `on` when the timer's own
definition is actually on disk, so if something removed the agent by hand the row
says `off` — it cannot tell you the checks are running when they are not.

`/status` says the same thing in a line: `keeping watch  installed`,
`keeping watch  while a window is open`, or
`keeping watch  nothing is checking · background checks are off · /settings`.

## Does aforge keep running when my terminal is closed — if I close this window does the chat stop, closing the terminal app, is it still running in the other shell

**Yes.** Your conversation does not live inside the terminal you started it in.
`aforge chat` runs the conversation in this folder's **engine** — a process with
no terminal of its own — and the window you type into is a view onto it. Close the
window, kill the shell, `ctrl+c` out of it: the reply goes on arriving, the tasks
go on running, and the engine keeps the transcript.

**To get back to it, open aforge again and press `enter` on that conversation's
row on home.** It comes up here, mid-reply, in well under a second. The row says
`open in the engine` when the engine has it with no window anywhere, and
`another window` when a terminal is sitting in it; either way one `enter` brings
it here, and the terminal that had it steps back and says so. See
*Continue a conversation from another terminal* on the home page.

**A plain `aforge` in that folder sits straight down in it**, with nothing to press
at all. A launch that names no conversation is asking for this folder's latest,
and the engine hands back the one it is already holding — mid-reply, with its
tasks still running. It never opens a second chat on top of one it holds, and
never says `open in another window` about its own conversation.

The engine lets go on its own when there is nothing left to hold: a conversation
with no window, no turn and no waiting question is kept for half an hour and then
closed, and an engine holding nothing at all exits. `aforge engine --stop` in the
folder ends it now.

Three launches keep the old arrangement, where the conversation really does live
in the window and ends with it: `--no-host`, `--debug`, and `--once`.

The other half of running with the terminal closed is the **standing side** —
reminders, watches, rules, overnight work — and it is separate machinery. Every 5 minutes, terminal closed
or not, the timer runs `aforge tick`, which takes a few seconds, does whatever is
due, and exits. There is no daemon sitting in memory between those moments, and
closing the terminal app changes nothing about it.

When something fires with nothing open, it waits for you: it is on home the next
time you open it, and it folds into the next conversation you open in that
project under one "while you were away". Nothing reaches your phone, your email,
or a notification — there is no outward lane at all.

If the program itself moves — you rebuild it somewhere else and delete the old
one, or an upgrade leaves the old path empty — the timer would be pointing at a
program that is gone. Every launch checks for exactly that and quietly puts the
timer back on the program you are actually running. You are not asked and nothing
is said on screen; one line goes to `v3/standing.log` under your aforge home.

## I have two copies of aforge and my reminders fired twice, or stopped firing — which build runs the background checks, and does AFORGE_HOME move the timer

There is one timer per login and it runs one build, so nothing ever fires twice
from two copies — if a reminder arrived twice, it was not two timers. And if your
reminders stopped firing after you installed a second aforge and removed the
first, the timer was naming a program that is gone; the next launch of any build
under that home puts it back.

There is **one timer per login**, and it is a pair: the home it checks and the
program it runs. Its definition carries both — `AFORGE_HOME` and the path to the
program — and a launch speaks only for its own pair.

- **Two builds on one machine** (a release beside one you built, two versions side
  by side): the timer keeps running whichever build installed it, for as long as
  that program is still there. A launch of the other build leaves it alone and says
  nothing. It steps in only when the program the timer names is **gone**, or the
  definition is not one this build would have written. So the timer never flips
  between builds on every launch, and `/settings` says `on` while it is running the
  other build — something is checking, and that is what the row reads.
- **To move the timer to the aforge you are running**, turn **background checks**
  off and on again under `/settings`. That is the one deliberate hand that moves
  it; a launch never does.
- **`AFORGE_HOME`**: the timer checks the home that turned it on, and `aforge tick`
  runs with that `AFORGE_HOME` set. If you export it permanently, your background
  checks run against that home and not against `~/.aforge`. A launch under some
  other `AFORGE_HOME` — a test, a throwaway home — reads the timer as somebody
  else's: it neither claims it nor rewrites it, and its `/status` says nothing is
  checking that home.

## Do reminders work over --host — yes, on the far machine

Yes, and this is the one ambient thing a connection does not take away. Over
`aforge chat --host devbox` the conversation runs on devbox, and so does
everything you set up from it:

- The `stand` tool is on the belt, so "remind me at 6", "tell me when CI goes
  red" and "every Monday post the standup" all work exactly as they do locally.
- The card is drawn on **your** screen and answered with **your** keys — the
  proposal crosses the connection like every other event, and `1`, `2`, `3` and
  `0` cross back.
- The item is created, checked and fired **on devbox**: its store is devbox's,
  the workspace it runs in is devbox's, and the rules a firing runs under are
  devbox's own profile rules, not this laptop's.
- It keeps working after this window closes **and after the connection drops**.
  Nothing about it needs the terminal you are sitting at.
- Background checks belong to devbox: the first thing you set up over the
  connection installs the timer **on devbox**, and turning the `background
  checks` row turns devbox's. Neither ever touches this machine.

**Home and the standing place both work over a connection**, and both are about the far
machine: home lists that machine's projects with each one's `◦` band under it, and the
standing place lists both what stands on this conversation and what stands anywhere else on
that machine. `p` and `s` write to the far machine's store and the refusal, if the store
refuses, is that store's own. The status line counts too — `◦ keeping an eye on 2` is about
the workspace this window is on, which over `--host` is a path on the far machine.

One live detail is still missing over a connection, and it says nothing rather than
guessing:

- **`/status` reads devbox's background timer.** Its `keeping watch` line says
  `installed` or `nothing is checking` from the far machine's own scheduler. If
  that engine has no scheduler to ask, the whole line is absent.
- **No row ever shows the firing mark `◐`.** Nothing on disk says an item is
  firing at this instant, so nothing claims it — the same silence a local window
  keeps.

## How do I stop one? — stop the thing that runs every hour, stop a routine, turn off a recurring check

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

`s` on the `/standing` page does the same thing to the row under the cursor, and
`n` there is the third answer: keep it, but not in this place. The
standing-orders page has both.

## How do I pause one and start it again?

"Pause the weekly update for now" and "start the weekly update again". A paused
item is not checked and not fired, and keeps everything else it knows: what it
has cost, when it last fired, what it last saw.

To see what stands here, ask what you have set up, or type `/standing` for the page
of it with `p` and `s` on its rows. Each row leads with the glyph every aforge
screen uses — `?` needs you, `◐` being checked or fired right now, `◦` waiting
for its time, `∙` paused or stopped — then your own words, then the cadence in
words. A project with nothing set up answers
`Nothing stands in this project yet.`

Where an order applies — this conversation, this project, or everywhere — and how
to except one place from it are on the standing-orders page.

## Why does my watch show a filled dot right now

`◐` on an item means a pass has that item in its hands **at this moment** — not
that something is wrong, and not that it has news for you. It is one of two
things, and the item's card says which:

```
◐ checking now · since 4s
◐ firing now
```

**Checking** is the look: a probe command running, the files you are watching
being fingerprinted, the cheap yes/no judgment deciding whether there is
anything to say. **Firing** is the work that followed a yes — the line being
delivered, or the overnight job running.

The dot is not guessed from anything the item remembers. While a pass is working
on an item, whichever process is doing it leaves a small marker in that item's
own folder — `~/.aforge/v3/standing/<id>/running` — naming the process, the
moment it started and which of the two halves it is in, and removes it when that
item's pass ends. Every aforge screen reads that file. So a firing started by
the OS timer with no window open at all, or by a window in another terminal,
still shows `◐` on your home and in your status line.

A marker is always doubted before it is believed. If the process that wrote it
is gone, or the marker is older than the **2 minutes** one pass may last, it is
read as a leftover and no dot is drawn — a machine that lost power mid-firing
never leaves a watch reading "checking now" for the rest of the week.

Most passes are over in far less than a second, so the ordinary state of a
healthy watch is `◦`. Seeing `◐` means you caught one working. On the status
line the `keeping an eye on N` segment is still while nothing is running and
turns while something is.

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
- a **run that came to nothing** — a firing whose work saved no file, left
  nothing waiting for you and had nothing to say when it finished. Nothing is
  delivered for one of those: no line in your chat, no row on home, because the
  whole of the news would have been that there was no news. Each run writes down
  what it came to, and only the word `nothing` may be swept.

Nothing else is ever removed. Not your items, not the ledgers, not a run that
said something, landed something, failed, or is waiting for you; not a run whose
folder never said what it came to; and not a folder something still holds open.

**Conversations are swept in exactly one case**, and it is the same week and the
same judgement: one you started **in a temp directory**, seven days after you
last said anything to it. Everything it holds goes with it — the transcript, and
the `work/` workspace if it owned one. If a task of that conversation was still
running when you last closed it, furrow is told to forget the copy of your
folder that task was working in as well, so `furrow forks` is never left naming
a directory that has gone. Every other conversation under
`~/.aforge/v3/projects/` stays whatever its age. If you work in a temp directory
and want to keep what a conversation makes, anchor it with `/workspace <path>`
or copy the files out; the starting-aforge page has both under *I deleted my chat
and lost the files the task made*.

**Sweeping a run removes the folder and nothing else.** The money line stays: the
day's ledger row for that firing is never reaped, and the item goes on
remembering that it ran, when, and what it came to — so `4 runs · spent $0.08`
on its card is still right long after the folders behind those runs are gone.

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
- **It will not fire while the machine is asleep**, and it will not fire when you
  are not logged in. A missed check is caught up once when the machine comes
  back, and the item's own row says when it last ran.
- **It will not reach your phone.** There is no notification, no email, no
  outward lane at all. News lands in a chat you have open, or waits on home and
  in the next chat you open in that project ("Where a reminder arrives").
- **It will not spend past the shared allowance or its quiet backstops without asking.**
- **It cannot do anything unattended that you have not already allowed.** Nobody
  is there to answer a permission card, so the run stops and says so.
- **A firing cannot set up another standing item.** The tool is absent inside
  one: nothing that runs on its own may arm something else that runs on its own.
- **Nothing is armed by a matcher.** Nothing runs because a phrase looked like a
  rule; every single one of these was a card you said yes to.
