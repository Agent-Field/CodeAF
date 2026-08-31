# Money, rails, and why it sometimes slows down

## The daily rail

Aforge spends real money, so it works against a ceiling: **$500.00 a day** by
default, resetting at your local midnight. The ceiling counts everything —
your jobs, charter firings, media, document OCR, its own practice.

That figure is deliberately large. It is a **backstop against something going
wrong**, not a budget: nobody chose it for you, so it sits where nobody would
defend the spend rather than where an ordinary day's work reaches it. If you
want a number you actually chose, `/budget default 40` is the whole of it — the
rail works exactly the same at forty dollars.

**A ceiling of `0` is no ceiling at all**, and it stays that way across
restarts.

## Pause and ask, never spend on

When the day's spend reaches the ceiling, aforge does not stop mid-thought and
it does not quietly keep going. It stops *claiming new work* and posts one
question:

> Daily budget reached -- $500.00 spent of $500.00. Say the word and I'll continue
> (raises today's rail by $500.00).

Say "yes" — or "go ahead", "continue", "do it" — and the rail is raised by one
full budget unit plus anything already overshot, and work resumes where it
stopped. Nothing was cancelled while it waited. You are asked once per raise,
not once per attempt.

## `/budget`

| you type | what happens |
| --- | --- |
| `/budget` | `spent $3.40 of $500 · resets midnight` |
| `/budget 25` | raises **today's** ceiling to $25 |
| `/budget default 25` | changes the standing default to $25, from tomorrow on |
| `/budget unlimited today` | no ceiling until local midnight; the default is untouched |

The header meter is the same day on a smaller surface: `$3.40 today`, plus what
aforge has spent on itself today beside it. It counts from your local midnight,
exactly as the rail does. In a narrow window it keeps the figure and drops the
word, so the money is still there in a docked pane.

## Asking about money in words

Ask for any stretch of time — "what has this month cost?", "how much did last
week come to?", "what's been expensive lately?" — and aforge reads its own
ledger for that window: the total, and the jobs the money went on, named the way
you named them and priced heaviest first. Those job figures do not add up to the
window's total, and it will say so: planning, answering and its own upkeep
belong to no single job.

"What did that cost?" about one particular job is answered from that job's own
record instead, which also carries what it produced.

## Per-job and per-firing limits

There is no fixed per-job dollar cap — a job's budget is written into its brief,
grounded in what similar work has actually cost.

Charters are capped explicitly: each one carries a per-firing budget (default
**$5.00**) and a max-per-day (default **10**). A firing that would cross the
daily rail is deferred and you are asked first. A per-firing budget of **0** is
no per-firing limit — such a charter is bounded by the daily rail and its
max-per-day alone.

## When a worker runs out of time — what happens to what it did

A worker is given a wall to finish in. When it reaches the end of it, it is not
killed and its work is not thrown away — it stops working and spends what is
left saying where it got to, and that account is what the next worker carries
on from.

Three things follow from that, and they are all readable on the work's record:

- **A command that will not fit is cut, not the worker.** Every command a
  worker runs is bounded by the time the worker has left, less what it takes
  that worker to finish up. A command that outlives its bound comes back as
  `cut after 9m12s; output so far:` followed by everything it had written by
  then, and the worker reads that and decides what to do — usually run a
  smaller piece of it. Nothing is left running in the background afterwards.
- **The piece goes back on the queue.** Running out of time is not a failure
  and it is not a verdict on the work. The piece is offered again, and whoever
  picks it up is handed the last worker's own turns to carry on from, so it
  never starts from nothing beside a directory full of its own files. You see
  one line saying how much it picked up. A worker that ran out of time having
  recorded nothing at all is the exception: there is nothing to carry on from,
  so that one is a failure.
- **What it spent is already counted.** Money is written down as each call is
  billed, not when the work finishes, so a worker interrupted halfway still
  shows its full spend on `/cost` and in the run's own total.

## What actually stops a worker — it stopped early, what ran out

Three things can stop a worker that is still going, and the record always says
which one it was, with its own two numbers:

- **The money.** Each piece of work is given an allowance, counted in what the
  provider actually charges — fresh input, the answer, and re-read context at
  the discount the provider gives it. This is the ordinary way a long piece of
  work ends.
- **Turns.** A backstop at 400 steps, which honest work never reaches.
- **Going in circles.** A worker that re-runs the same command, or goes several
  steps without writing anything or learning anything new, is told to wrap up
  and then stopped. This is what catches a worker that is stuck rather than
  slow.

Two other numbers are watched and **neither one stops anything**: how many
tokens have gone over the wire in total, and the same figure undiscounted. They
climb just as fast for a worker doing hard work as for one going in circles — a
long conversation re-sends everything it has said so far, every step — so they
are used to tell a worker to start wrapping up, and never to end it. They used
to end it, and the work that was thrown away was real.

## What a new piece of work is told about the pieces before it

When a job splits again — because work ran out of room, or because a review
found something missing — the new pieces are not strangers to it. Each one is
handed two things, taken from the job's own record rather than from anyone's
summary of it:

- **What the job has already done**, as an outline of the earlier pieces' turns
  and the files they left on disk, so nothing sets out to write a file that is
  already there. You see one line saying how much was picked up.
- **What the job is still short of**, at the top of its instructions and in the
  words the request itself used: behaviours nothing checks yet, checks that were
  failing when they were last run, and what the last review said was missing.

Neither is rewritten on the way. A finding that reached a worker as somebody's
paraphrase used to be a finding that mostly did not reach it at all.

## When work stops growing itself — it gave up early, why did it stop trying

Work that runs out mid-way is re-planned rather than abandoned: what is left is
worked out and queued as fresh pieces, and you see one line saying so. Five
things stop that from becoming a habit, and the first three read what actually
happened rather than counting:

- **Nothing is changing.** A round that changed nothing the job is about, after
  a round before it that also changed nothing, ends the **whole job** — not just
  the piece that asked:
  `carrying on has stopped changing anything — twice over now, nothing was written or altered — so this is handed over as it stands`.
  One fruitless round is never refused — work can run out before it writes its
  first file, and that is exactly the round this exists to buy.

  **What counts as changing something** is narrower than "a file appeared". It
  is a check file, a source file the request is about or one sitting beside it,
  or the job's own shortfall getting smaller — a failing check that passes now,
  a behaviour that nothing checked and something does, a deleted public name put
  back, a review finding answered. A worker that is stuck writes scratch files
  next to the work — `debug-grid.ts`, `debug-yoga.ts` — and those are not
  progress however many of them there are. They are written down by name, so a
  run that stopped this way can be read afterwards.

  A piece that ran out of room and was picked up again counts as a round here
  too, even though nothing new was queued for it.

- **No time left.** A run given a wall stops growing while there is still time
  to finish what is running and get a verdict on it:
  `there is not enough time left on this run to finish another round of work, so this is handed over while there is still time to check it`.
  How long a round of this job takes is measured on this job — its own rounds,
  not a fixed number.
- **The same work again.** When what is left to do comes back word for word the
  same as last time, another round would ask for exactly what the last one
  already did:
  `the work left to do came back word for word the same as last time, so another round would ask for exactly what this one already did — handing over what's done`.
- **Rounds.** A lineage may grow **three** times. Past that:
  `this work has split as many times as splitting helps — handing over what's done`.
- **Size.** A job may hold **90** pieces in all:
  `this job has grown as large as jobs are allowed to grow — handing over what's done`.

Every one of them hands over what exists rather than failing, and every one is
said on the work's own record, so a job that quietly stopped growing never looks
like a job that is still going.

## The practice carve-out

Aforge's own practice has its own pocket: **$50.00 a day**, spent as at most two
firings of half of it. When that is gone, practice stops for the day. It never
asks you to raise a rail on its own behalf — it just defers. The daily rail
still applies on top.

**This is the one pocket where `0` does not mean "no limit".** Zero turns
practice off. Practice is work aforge does while nobody is watching, so there is
no way to ask for it unbounded, on purpose.

## The provider rate limiter

Aforge shares one account across everything it does, so it shares one limiter.
When a provider throttles it, capacity is **halved**; after a run of clean
calls, capacity climbs back one slot at a time, up to 64 in flight. There is no
fixed concurrency setting to tune — under pressure the fleet converges on the
rate the provider will actually sustain instead of failing.

## The host load governor: why it slows when your machine is busy

Aforge will not fight you for your own laptop. Before starting another parallel
worker it reads the machine's one-minute load average per CPU core:

- above **1.5** it stops starting new workers,
- below **1.2** it starts again.

That is all machine work, not just aforge's — so a heavy build of yours slows it
down too. Nothing running is cancelled, nothing is delayed once claimed, and
there is at least one worker admitted no matter how loaded you are. You will see
things start more slowly; you will not see an error, because none happened.

## Background shells yield the machine

Any shell aforge backgrounds is reniced to **+10** as a whole process group, so
a long build in the background loses CPU to you rather than the reverse. The
foreground shell of a task keeps normal priority — it is on the critical path.

## Headless spending

`aforge run` accepts `--yes-spend`, and `AFORGE_PREAUTHORIZE_SPEND=1` does the
same, for runs with nobody there to answer the question. Both are journaled, so
a preauthorized raise is still visible afterwards as a decision that was made.
