# Automations — reminders, scheduled work and watches that run on a clock

## What is an automation — a reminder, scheduled work or a watch

An **automation** is something codeaf does on a clock **while it is open**. It is a
schedule and an action, and a watch adds a look. What it holds decides which of three
kinds it is, and `/automations` names the kind in these words:

| Kind | Said like | What happens when it runs |
| --- | --- | --- |
| `reminder` | "remind me at 6 to leave" | it says its line; no model is called |
| `scheduled work` | "every Monday at 9, draft the weekly update from the git log" | a piece of work runs on its own, unattended, and says how it went |
| `watch` | "tell me when CI on main goes red" | it looks on a rhythm, the model judges what it saw against your condition, and when that turns true it says a line or runs work |

You make one by **saying it** in a conversation, by **typing** `/automations add …`, or
from home with **`ask here`** — and every road ends on a card: **nothing is saved until
you say yes**. `/automations` lists them, with what each one last came to.

They run **only while a codeaf window is open** (*Does it run when codeaf is closed*). A
rule such as "always use tabs here" is not an automation: it has no time and nothing to
run, and it is kept as a memory marked always (*Rules are not automations*).

## How do I check on it later — have codeaf look again at a time, or keep watching and tell me

Three ways, and each is a sentence in the conversation:

- **"Remind me at 5 to check the deploy"** — a reminder: at 5, one line in this
  conversation and a desktop notification, and you do the checking.
- **"At 5, check whether the deploy finished and tell me what you find"** — scheduled work:
  at 5 the check runs on its own and its one-line result arrives here.
- **"Keep an eye on the deploy and tell me when it finishes"** — a watch: it looks on a
  rhythm (every 15 minutes, say) and speaks once the condition turns true.

Each is proposed on a card first and saved only when you say so; `/automations` lists them
afterwards. All three need a codeaf window open at the time — they run only while one is.
Work you hand off with `/task` is different: it runs now, on its own, and reports back when
it lands, whatever you are doing meanwhile.

## Remind me at 6 — set a reminder by saying it

Say it in any conversation — "remind me at 6 to leave", "remind me in 20 minutes to check
the oven", "tomorrow at 9 remind me to call the bank" — and I propose it on a card headed
`wants to remind you`. The card shows the line it will say (`says · time to leave`) and
when, in words and exactly: `today at 18:00 · first run Sat 10 Oct 18:00`. Its answers are
`1 Save` and `0 Don't save`, and typing a change under it corrects it in your own words.

At the time, one dim line appears in the conversation you made it in —
`leave · done · time to leave` — and a desktop notification is raised (*Where does the
news arrive*). **A reminder calls no model and costs nothing.**

A time that has already passed is refused rather than moved, and I propose again from the
real time. "In 20 minutes" is sent as the distance itself, so it needs no clock; a time of
day is worked out from the clock line I am given, and every answer the `automation` tool
gives ends with the time now — there is no need to run `date` first. If
codeaf is closed when the moment comes, the reminder is not lost: it is said once when a
codeaf window is next open, marked `late`. A reminder made with nobody there to
answer the card — inside a task, or a headless `codeaf chat --once` — is not set up at all.

## Do something every Monday — scheduled work at a time or on a rhythm

"Every Monday at 9, draft the weekly update from the git log", "at 7 tomorrow run the full
test suite and tell me what broke", "every night at 2 bump the dependencies" — each is
proposed on a card headed `wants to schedule work`, showing what it does (`does · …`),
where (`in your checkout` or `in a separate worktree, kept for review`) and what one run
may take and spend: `up to 30m and $5.00 a run`.

**A run is your own conversation, done later.** It has your models, keys, connected
accounts, memory, the rules you keep with `/always`, and **your own approval rules**.
Nobody is there to ask, so a call your rules would ask you about is refused, and the first
such refusal stops the run, naming the call:
`weekly update · your call · needed your ok to run bash git push`. Allow that call with
`always` in a conversation — or answer it during `2 Save and run it now` — and later runs
go through.

**The work says how it went.** It finishes by reporting `done` or `incomplete` with a short
summary, and that summary is the line you read. A run that ends without saying is
`incomplete · it ended without saying how it went`. Each run's transcript is kept, and
opens from the automation's history. A run cannot start a task, change settings, or set up
another automation.

## Tell me when CI goes red, or when a new invoice arrives in my mail — a watch

A watch looks at one thing on a rhythm: **a command** run in the project
(`gh run list -b main -L 1`), **files** matching a pattern (`docs/**/*.md`), or **a tool** —
often a connected account's. Every look is put to the model with your condition — "the
latest run on main failed" — and it answers yes or no. **It speaks on the change to yes**:
it says its line, or runs its work with what it saw, then stays quiet while the condition
stays true. A no re-arms it, so the next yes speaks again. Made with `once`, the card says
`tells you once, then stops`, and it finishes after it first speaks.

The card is headed `wants to watch for something` and shows `looks · …` and
`until · <the condition>`, the rhythm, and — because the model judges every look — an
estimate when the model's price is known: `about $0.40 a day while codeaf is open`.

A look whose condition is not met is `nothing new`, and **that is never news**: no line, no
notification. Its row says `last nothing new` and its history keeps every look. A look that
cannot be judged — no API key, a provider error, an answer the model could not give — is
`couldn't check` with the reason, and changes nothing about what the watch has seen.

A command look runs for at most a minute with the provider keys taken out of its
environment, and the model reads the last 8 KiB of what it printed and its exit status. A
files look stays inside the project, lists at most 2,000 files, and says which are new,
changed or gone since the last look. Prefer a command whose output carries the thing
itself — a status field, a file's contents — rather than a bare exit code.

## Will a watch retry if my API key is missing or the provider is disconnected — couldn't check

Every look a watch takes is judged by a model — the low-tier one codeaf uses for its own
small checks, the same for every watch — through your own keys and connected accounts.
With no key, or with the
provider that serves that model disconnected or failing, the look still runs and cannot be
judged: it ends `couldn't check` with the reason (`couldn't decide: …`), and **it changes
nothing about what the watch has seen**, so it neither speaks nor forgets.

Nothing is retried in between. The next look on its rhythm simply tries again, so setting
a key or connecting the provider again (`/connect`) is all it takes: the watch carries on
from what it last decided, and speaks if the condition has turned true since. Each
`couldn't check` is news — a line and a desktop notification — so a watch that cannot be
judged does not go quiet without telling you.

A reminder calls no model and is never affected. Scheduled work that cannot reach a model
ends `incomplete · a fault: …`, and its next run tries again.

## The card — what kind of card this is, what its options mean, and how to answer, change, cancel or decline it

Every automation reaches you as a card first, and **nothing is saved until you say so**. It
is the card the standing card became, and its heading says which kind it proposes:
`wants to remind you`, `wants to schedule work` or `wants to watch for something`. It shows
the real thing, one fact per line, each only when there is something to say:

- the title, and your own sentence in quotes when it differs from the title;
- when, in words, and its first run: `every Monday at 09:00 · first run Mon 12 Oct 09:00`;
- a rhythm exactly, with the zone it is read in: `cron 0 9 * * 1 · America/Toronto`, or
  `every 15m`;
- for a watch, `looks · …` and `until · …`;
- `says · …` for a line, or `does · …` for work with `in your checkout` or
  `in a separate worktree, kept for review`;
- for anything but a reminder, the limits: `up to 30m and $5.00 a run`.

The options: **`1 Save`**; **`2 Save and run it now`** — `2 Save and check it now` on a
watch; and **`0 Don't save`**, the no, which reads `Nothing is saved and nothing runs.`
**To change it, type the change and press enter** — the line under the card says
`say what to change — when, what it does, where it runs` — and nothing is saved: I
propose it again with your correction. **`o Change…`** takes a fresh request instead: the
box reads `other: write an updated request, then enter`, and what you write replaces the
request the card came from. **`esc` is not the no**: it puts the card off for later, and
`alt+y` brings it back. My turn waits on the card for as long as it takes; to cancel it
outright, press `0`.

Answered, the card folds to its heading and one line saying what it came to:
`weekly update · Save · saved`, or `weekly update · not saved`.

## Save and run it now — try it once while you watch; what became of just once

`2 Save and run it now` saves the automation, and then I do its work **once, right away, in
this conversation**, while you watch. Any approval it needs is asked of you there, and an
`always` you give is kept, so the unattended runs after it go through. On a watch the answer
is `2 Save and check it now`: I take one look myself and tell you whether the condition
holds now.

It is not offered on a reminder — there is nothing to try; its whole content is the moment
— nor on work kept in a separate worktree, because a turn in this conversation works in
your checkout. `r` on its row in `/automations` runs any of them once, unattended, later.

There is no "just once" answer that does the thing now without keeping it: to have it done
once and not again, ask for it as ordinary work, or answer `0` and say so.

## Schedules and cron — every 15 minutes, weekdays at 8:30, a cron line, time zones

A schedule is **one moment** or **a rhythm**. A moment is a time ("at 6", "tomorrow at 9")
or a distance from now ("in 20 minutes"). A rhythm is an interval or a cron line:

- **An interval**, a minute or more: `15m`, `2h`, `1d` — `d` is a day. It is a grid laid
  from when it was saved, so a late run never pushes the next one later.
- **A cron line**, five numeric fields — minute, hour, day of the month, month, day of the
  week — with ranges, lists and steps: `0 9 * * 1` is Mondays at 09:00, `30 8 * * 1-5`
  weekdays at 08:30, `*/15 * * * *` every 15 minutes. Sunday is `0` or `7`. Names
  (`MON`), `@daily` and seconds are not read. When both day fields are restricted, a day
  matching either one runs.

The list says a rhythm in words — `every day at 09:00`, `every weekday at 08:30`,
`every Monday and Thursday at 09:00`, `on the 1st of every month at 09:00`,
`every hour at :15`, `every 15 minutes` — and a line with no plain reading as
`on the cron line 0 9 1-7 * 1`.

**A cron line keeps the time zone it was made in**, so a Toronto rhythm stays a Toronto
rhythm on a machine set to another zone. On the night the clocks go back a 01:30 line runs
once; in spring a 02:30 line waits for the next day. A line that can never happen is
refused: `the rhythm "0 0 30 2 *" never happens`. A rhythm under a minute is refused too.

## /automations — see every automation, when it runs next, how it last went, and what its mark or dot means

`/automations` opens the list — so does `alt+7`, or the `automations` heading on home. It is
not one of the tab bar's words until you are in it. One row per automation: its mark, its
title, and one dim sentence — its kind, its schedule, where it is now, and its last run:

```
weekly update   scheduled work · every Monday at 09:00 · next Mon 12 Oct at 09:00 · last done
ci on main      watch · every 15 minutes · running now · last nothing new
leave           reminder · today at 18:00
```

Where it is now reads `next …` for a rhythm waiting for its time, `running now`,
`about to run`, `paused`, or `finished` — a one-time automation that has run, or a `once`
watch that has spoken, kept with its history. The mark says the same. In plain glyphs:
`○` waiting for its time, `◐` — the half-filled dot — a run in hand right now, `=` paused,
`✓` finished, `?` when the last run needs you (`your call`, `couldn't check`) and `✕` when
it was `incomplete`; a terminal with an icon font draws icons in the same places.

Active ones come first, soonest first, then paused, then finished. The count beside
`automations` on the tab bar and the map is how many of them have news — a last run worth
telling you about that ended since you last looked. With none saved, the place keeps its
heading and
`reminders, scheduled work and watches · "remind me at 6" or "every morning at 9"`.

## Pause, resume, run now, stop a run that is going, or delete an automation — stop the thing that runs every hour — the → strip

On a row of `/automations`, `→` draws the row's verbs, and their letters work while it is
drawn:

| Key | Verb | What it does |
| --- | --- | --- |
| `r` | `run now` · `check now` on a watch · `say it now` on a reminder | one run outside its schedule, within seconds |
| `s` | `stop the run` | only while a run is in hand; it ends as `stopped` |
| `p` | `pause` / `resume` | a paused one wakes for nothing until it is resumed |
| `e` | `edit` | puts its own `/automations edit …` line in the message box |
| `d` | `delete` | asks again, then removes it and its whole history |
| `o` | `open where it was asked` | the conversation that made it |

Each says what it did on the message line: `asked to run now · weekly update`,
`asked to stop · weekly update`, `paused · weekly update`, `going again · weekly update`,
`deleted · weekly update`. The first `d` only asks:
`d again deletes it and its history · esc keeps it`. **There is no undo for delete.**

**A pause is not a miss**: a resumed rhythm starts at its next slot after now and nothing
catches up for the paused time; a one-time reminder resumed after its moment runs once,
late. A finished one offers no pause. The foot names the row's verbs:
`enter its history · → verbs: run now, pause, edit, delete, open where it was asked · tab next place · esc`.

## Change an automation — edit its time, what it does or where it runs

Press `→` then `e` on its row in `/automations`, and the automation's own line lands in the
message box, exactly as it would have to be typed:

```
/automations edit 3f2a9c1e5b7d4a60 title "weekly update" zone America/Toronto every "0 9 * * 1" do "draft the weekly update from the git log" folder /home/you/code/app
```

Change what you want and press enter. Nothing is saved yet: a card headed
`save this change?` shows the automation as it would be, and `1 Save` saves it —
`saved weekly update · next Mon 12 Oct at 09:00`. Any clause you leave out stays as it
was. A new schedule is worked out again from now, and a finished one-time automation given
a new moment wakes again. A watch whose look or condition changed forgets what it had
seen, so its next yes speaks.

A watch that looks through a tool is written without its look — a tool's arguments are not
something anybody types on one line — and the edit leaves that look as it was.

**I have no verb that changes an existing automation in place.** In a conversation I can
pause, resume, run, delete, list it and read its history; changing one is `e`, or a new
proposal that replaces it.

## History — every run, why did I get this reminder, and open where it was asked

`enter` on a row of `/automations` opens its history. The top says what it is — its title
and schedule, the rhythm exactly, what it looks at and its condition, what it says or does —
and under it every run, newest first, up to the last hundred:

```
today at 09:00            done · drafted the update from 14 commits · $0.42
Mon 5 Oct at 11:20        done · late 2h · drafted the update from 9 commits · $0.38
Mon 28 Sep at 09:00       your call · needed your ok to run bash git push
```

Each row is when it started, what it came to, how late it was, its one line and what it
cost. `enter` on a run of work opens that run's own transcript, read like any
conversation; a run that only said a line keeps none: `this run kept no transcript`.
`esc` goes back to the list. With nothing run yet it says `it has not run yet`.

**Why did I get this?** `→` then `o` on its row — `open where it was asked` — opens the
conversation the automation was made in, where your own sentence and the card you answered
are. From inside that conversation it says `you are already in it`. You can also ask me —
"what did the weekly update do last week" — and I read the same history.

## What a run came to — what done, nothing new, your call, incomplete, stopped, couldn't check and late mean

| Reads | When |
| --- | --- |
| `done` | a reminder said its line, a watch's condition turned true, or work reported it finished |
| `nothing new` | a watch looked and the condition was not met, or still holds since it last spoke |
| `your call` | work stopped on a call only you could allow: `needed your ok to run bash git push`; or did not start because today's spending limit is reached |
| `incomplete` | work did not finish: `ran out of its 30m`, `reached its $5.00 cap`, `a fault: …`, its own summary, or `it ended without saying how it went` |
| `stopped` | `stopped by you`, `codeaf closed before it finished`, or `the automation was deleted` |
| `couldn't check` | a watch could not look or be judged: `couldn't look: …`, `couldn't decide: …`, `ran out of time`, or why the model could not tell |

A run that started more than two minutes after its time adds `late` and how late it was:
`late 2h`, `late 3d`. A run you asked for with `r` is never late.

Every one but `nothing new` is news: a line in the conversation that made it, and — except
`stopped`, which you or closing codeaf caused — a desktop notification. There is no
`failed`: a run that did not finish says why in its line.

## Where does the news arrive — the line in the conversation, and the desktop notification

**In the conversation that made it.** When a run ends, one dim line appears there:
`weekly update · done · drafted it`, `ci on main · done · CI on main is red`. It is not part
of the transcript, it does not wake me, and it costs nothing. A window draws it only while
that conversation is the one in front. One that was not in front gets its lines when it
next comes to the front — a window starting with it, or a switch to it in a window already
open, within a couple of seconds — every run worth telling you about that ended since you
last wrote there, up to the last ten of each automation. A line a window has already drawn
is not drawn again when you switch back. Its history on `/automations` has every run.

**On the desktop.** A run that is news raises one notification, titled `codeaf · <title>`
with the same line as its text: on macOS through `osascript`, on Linux through
`notify-send` when it is installed, and otherwise through the terminal's own notification
escape — the one a finished turn uses, which some terminals never show. It is raised **once
per run per machine**, by whichever open window claims it first, and not at all while the
window is known to have your keyboard: you are already looking. A `stopped` run and a
watch's `nothing new` raise none. There is no setting for it.

**Elsewhere.** Home's `since you left` panel has a line for each automation whose last run
was news and ended while you were away, opening `/automations`; `/status` has an
`automations` line; and a conversation's side column lists the automations it set up.

## Automations on home — when does my reminder go off, the automations panel, /status and since you left

**Home's `automations` panel** lists every automation that will run again, soonest first —
its title, and under the cursor the sentence `/automations` draws, which says when it goes
off next: `reminder · today at 18:00`,
`scheduled work · every Monday at 09:00 · next Mon 12 Oct at 09:00`. The heading and every
row open `/automations`; nothing is paused or changed from home itself. With nothing
on the clock the panel keeps its heading and
`reminders, scheduled work and watches · "remind me at 6" or "every morning at 9"`, and on
a short terminal it is the first panel to give way.

**`since you left`** has one line per automation whose **last** run was worth telling you
about and ended while you were away — `weekly update · done · drafted it` — and `enter` on
it opens `/automations`. It reads only that last run: a watch whose newest look was
`nothing new` has no line there, and the history (`enter` on its row in `/automations`)
has every run.

**`/status`** has one line while anything is on the clock:
`automations   3 on the clock · next weekly update Mon 12 Oct at 09:00 · 1 running`. With
nothing on the clock there is no line at all. Over `--host` or `--at`, the panel, the line
and `/automations` all read the far machine's automations.

On home, typing `aut` offers the place, with `2 automations on the clock` beside it.

## The automations in a conversation's side column — the list on the right, and + /automations

The column on the right of a conversation lists, under its tasks, the automations **this
conversation** set up that have not finished: the label `automations`, then at most three
rows — each its mark and its title — with `automations · 2 more` on the label when there
are more. A row opens `/automations` with the cursor on that automation.

Under them is the door `+ /automations`. Pressing it puts `/automations ` at the start of
the message box, keeping what you had typed after it, and hands the keyboard back: send it
bare to open the list, or finish it as `/automations add …`. Pressing it twice does nothing
the second time. The section is there whenever the window can read automations — before
there are any it is the door alone.

## Does it run in the background when codeaf is closed — keep working with the terminal or the app shut, is there a helper, will a reminder fire, and a missed run or check while the laptop or computer was off or asleep

**Automations run only while a codeaf window is open.** Nothing is installed on the
operating system — no launchd agent, no systemd timer, no helper or background service — and
nothing runs with every window or the whole app closed. One small process, `codeaf clock`, does the running: a window
starts it when none is running, every window checks every half minute that it still is, and
it leaves by itself half a minute after the last window closes.

**What fell due while codeaf was closed, or while the machine slept, runs once** when a
window is next open, marked `late`. A rhythm that missed five slots runs one run, and its
next slot is the first after now: a weekly report after a fortnight away is written once,
not twice. A one-time reminder whose moment passed is never dropped; it is said when you
come back.

**Closing the last window stops a run in progress.** After that half minute — the grace
that lets a window restart onto a new build — the run is stopped and recorded as
`stopped · codeaf closed before it finished`. It is not run again; its next slot comes as
usual.

To keep automations running, keep a codeaf window open: on this machine, or attached to
another machine with `--host` or `--at`, which keeps that machine's automations running
(*Automations on another machine*). A conversation its engine goes on holding after you
closed its window is not an open window, and does not keep them running.

## Quitting while an automation is running — 1 automation is running, quitting stops it

Leaving the last codeaf window while a run is in hand says so once instead of leaving:

```
1 automation is running — quitting stops it · ctrl+c again to quit
```

With more than one it reads `2 automations are running — quitting stops them`. The second
half names the gesture you used — `· /quit again to quit` after the last `/quit` — and the
same gesture again within ten seconds leaves. Leaving stops the run and records it as
`stopped · codeaf closed before it finished`.

It asks only when it is true. With another codeaf window open, closing this one stops
nothing, and it leaves at once; so does a window that has not read the automations yet.
Closing the terminal outright, or a signal from outside, cannot ask: the run is stopped and
recorded the same way.

## Several windows, codeaf clock, CODEAF_HOME and CODEAF_NO_AUTOMATIONS — which window runs them, and where they are kept

There is **one clock per codeaf home on a machine**, whichever window started it, and every
window reads the same database, `~/.codeaf/v3/automations/automations.db` — so the list,
the lines and the notifications agree in all of them. Two windows racing to start a clock end with one,
and a run does not belong to the window that happened to start it: it stops only when the
last window is gone.

`CODEAF_HOME` moves all of it: a home of its own has its own automations and its own clock.
Two builds of codeaf under one home share the one clock and the one store, and a slot can be
taken only once, so nothing runs twice.

**`CODEAF_NO_AUTOMATIONS`**, set to anything in a window's environment, keeps that window
from starting the clock. The window still shows the list, draws the lines and counts as
open, so a clock another window started keeps running while it is there; with no other
clock, nothing runs and saved automations wait.

The clock inherits the environment — keys, `PATH`, time zone — of the window that started
it. When its own build is replaced it leaves as soon as nothing is running, and the next
window's check starts the new one. What it could not do outside a run goes to
`~/.codeaf/v3/automations/clock.log`.

## What does an automation cost — 30 minutes and $5 a run, and where the spend shows

**A reminder costs nothing**: no model is called. **Scheduled work** costs what its run
spends on your models. **A watch** costs its judgment — a low-tier model reads every look —
plus any work it runs, and its card estimates the judging:
`about $0.40 a day while codeaf is open`, or `under a cent a day while codeaf is open`.

Every run of a watch or of work is held to two limits, and the card shows both: **30
minutes** and **$5 a run** unless you named others — "give it an hour and at most $2" — or
typed `time` and `usd`. Work that reaches its money stops as
`incomplete · reached its $5.00 cap`; a run out of time is `incomplete · ran out of its 30m`
(`couldn't check · ran out of time` for a watch).

**Your daily spending limit holds over work too.** Once today's spending has reached it, a
run of work does not start, and nothing is spent on it: it reads `your call · today's
spending limit is reached · $512 spent of $500 · /budget day changes it`. A watch's looks are
not held to it — each one costs a fraction of a cent, and a refused look would be news every
few minutes — but work a watch would run is.

On `/spend` the automations have a table of their own, `by automation`: each one with how
many calls it made, what one call cost (`$0.01 a call`) and its total. The Spending tab of
`/settings` shows the default as a reading rather than a setting: its `per automation` row
reads `$5 a run`, with `each automation may name its own` beside it.

## Where does scheduled work run — in your checkout, or in a separate worktree kept for review

An automation runs in the project of the conversation that made it — or in your home folder
when it belongs to no project. A typed one runs in its window's project unless it names a
`folder`.

**Work runs in your checkout** by default, as a turn of your own does: what it changes, it
changes in the live files. Ask for it to be kept for review — "and keep it on a branch for
me to look at" — and the card says `in a separate worktree, kept for review`: each run is
cut a git worktree of its own from your last commit — uncommitted changes in the checkout
are not in it — on a branch named `automation/automation-<title>-<id>`. The branch is kept
after the run, and nothing merges or deletes it for you. That needs a git repository with a
commit — `a separate worktree needs a git repository with a commit` — and only work can have
one: `only work can run in a separate worktree`. It is a separate copy, not a sandbox.

`2 Save and run it now` is not offered for worktree work, because a turn in this
conversation works in the checkout.

## /automations add — the exact typed form, with examples

`/automations add` makes one with **no model in between**: you type exactly what you want,
and it is read back on a card — `save this automation?` — before anything is saved. Every
clause is a word and one value; a value with a space is in double quotes, and `\"` puts a
quote inside one.

```
/automations add leave at 18:00 say "time to leave"
/automations add stretch in 2h say "stand up and stretch"
/automations add "weekly update" every "0 9 * * 1" do "draft the weekly update from the git log"
/automations add "ci on main" every 15m look "gh run list -b main -L 1" until "the latest run failed" once say "CI on main is red"
/automations add docs every 1h files "docs/**/*.md" until "a page was added" say "new docs page"
/automations add nightly every "0 2 * * *" do "bump the dependencies and run the tests" worktree time 1h usd 2
```

The title comes straight after `add`. The words, each with one value:

- **when** — `at` (`18:00` today, `"tomorrow 18:00"`, `2026-10-12T09:00`), `in` (`20m`,
  `2h`, `1d`), or `every` (an interval, or a cron line in quotes); `zone` (`America/Toronto`)
  is read first;
- **what** — `say "<line>"` or `do "<brief>"`;
- **a watch** — `look "<command>"` or `files "<pattern>"`, `until "<condition>"`, and `once`;
- **where and how much** — `worktree` or `checkout`, `time 45m`, `usd 2.50`, `folder ~/code/app`.

`1 Save` on its card — `It runs on this schedule while codeaf is open.` — saves it and says
when it first runs: `saved stretch · next today at 16:30`. `0 Don't save` saves nothing.

## /automations edit, run, pause, resume and delete — by id, and what a typed line says when it cannot be read

The other forms take the automation's id — sixteen hexadecimal characters, of which the
first few are enough when no other id starts the same way:

```
/automations edit 3f2a9c every 30m
/automations run 3f2a9c
/automations pause 3f2a9c
/automations resume 3f2a9c
/automations delete 3f2a9c
```

The list's rows do not print the id: `→` then `e` on a row puts its full line, id and all,
in the box, and I can list them with their ids. `edit` raises a `save this change?` card
like `add`; **`run`, `pause`, `resume` and `delete` do not ask** — typing the id is the
confirmation — and answer on the message line as the row's own keys do. An id nothing
matches says `no automation has the id 3f2a9c`.

A line that cannot be read says why and saves nothing:

- `add needs a title first: add "weekly update" every "0 9 * * 1" do "…"`
- `unknown word "daily" — the words are title, at, in, every, zone, say, do, look, files, until, time, usd, folder, once, worktree, checkout`
- `cannot read the moment "6pm" — say 18:00, "tomorrow 18:00" or 2026-10-12T09:00`
- `Sat 10 Oct 09:00 has already passed`
- `a watch needs a rhythm to look on` · `a watch needs the condition it is looking for`
- `an automation needs something to say or something to do`

## Ask here — set one up from home without opening a conversation

On home, type the sentence — "remind me at 6 to leave", "tell me when CI on main goes red"
— and press `↑` onto `ask here` and `enter`, or `ctrl+enter` on a terminal that can send
it. A short exchange opens instead of a new conversation, and while it has the keyboard it
is home's whole screen — `esc` puts the list back, with the exchange as a row on it. Its
card arrives there: `1`, `2` and `0` answer it as they do in a conversation, and `o` opens
the pane's own box for a correction. Once it is saved the pane says
`saved · /automations lists it`.

The exchange is kept in a folder of its own, under `~/.codeaf/v3/errands/`, and it is the
conversation `open where it was asked` opens, so "why did I get this?" still has an answer.
The automation runs in the project the exchange belongs to: the project row the cursor was
on, else the project this window is in, else your home folder. *Asking from home* has the
whole of it.

## Rules are not automations — always use tabs, never push to main

"Always use tabs in this repository", "never force-push a shared branch", "our commits are
written in the imperative" — these have no time and nothing to run, so they are not
automations. **They are rules: memories marked always**, put in front of every turn of
every conversation where they hold, and in the brief of every task started there.

`/always <text>` keeps one; bare `/always` lists the rules in force here. Said in a
conversation, I keep one with `remember` set to always, on a card that asks you first.
*What I remember* — its `Always` sections — has the rest: where a rule holds, how many ride
along, and switching one off.

## How I set one up when you say it — the automation tool, and automation_report

You never name a tool. When what you say is a reminder, scheduled work or a watch, I call
the **`automation`** tool to propose it, and its card is what you see. The same tool
**lists** the automations with their ids, **pauses**, **resumes**, **deletes** and **runs**
one now, and reads one's **history** — so "pause the CI watch", "delete the weekly update"
and "what did the nightly bump do on Tuesday" work in any conversation. A change made that
way needs no card and leaves one dim line in the conversation — `ci on main · paused`,
`weekly update · deleted` — while making a new one always goes through its card.

A run of scheduled work ends by calling **`automation_report`** with `done` or `incomplete`
and a short summary: that summary is the line you read, and a run that never calls it is
`incomplete · it ended without saying how it went`.

The `automation` tool is not there inside a task, inside an automation's own run, or where
nothing can keep automations — so an automation can never set up another one.

## Where did standing orders go — what happened to /standing, the standing orders page, keeping an eye, and turning off background checks

**Standing orders are gone, and automations replaced them.** `/standing` and `/orders` are
not commands any more — they answer `there is no command called /standing · / lists them`
— and the standing orders page, the `stand` tool, the keeping-an-eye line on the status
row and in `/status`, home's standing panel and the background checks setting went with
them. There are no background checks left to turn off, on this machine or a remote one.
What a standing order did is now one of two things:

- **something on a clock** — a reminder, a routine, a watch — is an **automation**:
  `/automations`, or say it;
- **a rule** — "always run the tests before you commit" — is a **memory marked always**:
  `/always`.

**Standing orders were not carried over.** None of them is listed or changed anywhere in
codeaf any more: make again, as automations or rules, the ones you still want. Their old
folder, `~/.codeaf/v3/standing/`, is left on disk. The standing card's
answers went too: its `3 just once` is `2 Save and run it now`, which saves it as well, its
`2 change when or where` is typing the change under the card, and every no is
`0 Don't save`.

**Nothing is installed on the machine any more.** Earlier versions installed a background
timer — `ai.agentfield.codeaf.tick` under launchd, `codeaf-tick.timer` under systemd, and
the older `ai.agentfield.codeaf.wake` — that ran `codeaf tick` with no window open. This
version removes them, whoever installed them, on every start.

## What automations cannot do

- **No — they do not run while codeaf is closed.** Keep a window open, here or attached to
  another machine; anything missed runs once, late, when one is.
- **No triggers but the clock.** A watch looks on a rhythm; nothing wakes on a push, a
  webhook or a file event.
- **No phone notifications** — the desktop notification of the machine you are sitting at,
  and the line in the conversation.
- **Nothing an automation runs can set up another automation, start a task or change your
  settings.**
- **One run at a time per automation.** A slot that comes while the last run is still going
  is passed over, not queued; at most two pieces of work and four looks run at once.
- **No rhythm under a minute**, no cron names, `@daily` or seconds.
- **No thinking rung of its own** — `alt+e` has nothing to move on an automation.
- **No exception for one conversation or one project** — an automation is one thing in one
  place, and pausing it pauses it everywhere.
- **No undo for delete**, and no moving an automation to another machine.
