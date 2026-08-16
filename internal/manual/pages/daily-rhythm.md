# The daily rhythm: what happens while you are gone

You are not the only reason aforge is awake. Between the moment you close the
terminal and the moment you come back, it keeps a small, budgeted, honest
routine. Nothing here spends without a ceiling, and nothing here is hidden from
you — every piece of it leaves a line you can read.

## The standing watch: a quiet check every few minutes

If you said yes when it offered to keep watch, aforge installs a timer with your
operating system — a launchd agent on macOS, a systemd user timer on Linux —
that runs `aforge wake` **every 5 minutes**, with no terminal open.

A wake pass is a real, bounded shift: at most **120 seconds** and at most **32
passes** of the reconciler, stopping early the moment a pass produces nothing
new. If a live aforge already holds the lease, the wake refuses and exits, so
the two never run at once.

For those checks to reach a provider with no shell environment, your API key is
copied once into your profile config, written so only you can read it. Aforge
tells you when it does that.

Ask "who's keeping watch?" or "when was the last wake?" and you get the real
state: installed or not, the last wake, the next check.

## Charters firing on their watches

Every standing goal you ratified is a charter with its own watch — a clock, a
file, a place in the graph, or a poll. On each wake, the charters that are due
get **one cheap sentinel call** each: a yes/no judgment about whether the thing
you care about actually happened. A "no" costs a few tokens and ends there.

A "yes" fires the charter, and the rails decide whether it may:

1. past its expiry → the charter retires;
2. already at its **max firings per day** → blocked for today;
3. its **per-firing budget** would cross today's daily rail → deferred, and you
   are asked whether to raise the rail.

A new charter is on **probation**: each firing is proposed to you rather than
just done. After **3** consecutive clean firings it becomes tenured and fires
unattended. One bad assessment — including one that costs more than its
per-firing budget — puts it back on probation.

## The practice loop in idle time

When you have been idle — no work of yours pending or running, and at least
**20 minutes** since you last spliced anything — aforge may practice.

It picks one measured knowledge gap where it has actually been surprised
before, and only in areas a machine can check itself on: it must write or find
a real verifier (a build, a test, a runnable check) and run it. Practice never
posts into your thread and never touches your work.

It has its own money, separate from yours: **$2.00 a day**, spent as at most
**2 firings of $1.00**. When that is gone, practice simply stops for the day. It
never asks you to raise a rail on its own behalf.

## Belief aging: what it stops believing

About once a minute, aforge scores everything in its notebook by how recently
and how often that belief has actually been used. A belief that has fallen below
the retention threshold is **quarantined** — it leaves the notebook and stops
shaping answers.

Nothing is deleted. A quarantined belief stays in the record and can be restored
with `aforge notebook restore <seq>`. You see it happen as a `· let go —` line.

## The retrospective

At most every **30 minutes**, and only once at least **3** jobs have settled with
at least one new since last time, aforge reads back over its most recent **12**
settled jobs and asks what they taught it. It records at most **4** durable
facts per pass, and it says nothing at all when a pass produced no real change.

Every **5th** retrospective it audits itself instead: it checks how often its own
tuning has been reversed and eases or tightens exactly one dial — how eagerly it
promotes a skill, how long it holds a belief, how often it proposes, when it
consolidates a crowded scope. One step at a time, inside fixed floors and
ceilings.

## The arrival brief

When you come back after **4 hours or more away**, and something actually
happened in that gap, aforge posts one card. It always opens **"While you were
away"** and it folds the whole absence into one reading:

- what finished, what failed, what was cancelled
- questions waiting for you
- charters that fired
- what it learned, and any skill that became available
- what it all spent

Come back after twenty minutes and there is no card — you did not miss anything.

## What the whole rhythm costs

Every part of it is capped before it starts. Charters are capped per firing and
per day. Practice is capped at $2.00 a day out of its own pocket. Everything
together still sits under your **$20 daily rail**, and when that rail is reached
the work stops and asks you rather than spending on.
