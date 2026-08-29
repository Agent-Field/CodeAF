# Money, rails, and why it sometimes slows down

## The daily rail

Aforge spends real money, so it works against a ceiling: **$20.00 a day** by
default, resetting at your local midnight. The ceiling counts everything —
your jobs, charter firings, media, document OCR, its own practice.

## Pause and ask, never spend on

When the day's spend reaches the ceiling, aforge does not stop mid-thought and
it does not quietly keep going. It stops *claiming new work* and posts one
question:

> Daily budget reached -- $20.00 spent of $20.00. Say the word and I'll continue
> (raises today's rail by $20.00).

Say "yes" — or "go ahead", "continue", "do it" — and the rail is raised by one
full budget unit plus anything already overshot, and work resumes where it
stopped. Nothing was cancelled while it waited. You are asked once per raise,
not once per attempt.

## `/budget`

| you type | what happens |
| --- | --- |
| `/budget` | `spent $3.40 of $20 · resets midnight` |
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
**$0.15**) and a max-per-day (default **10**). A firing that would cross the
daily rail is deferred and you are asked first.

## When work stops growing itself — it gave up early, why did it stop trying

Work that runs out mid-way is re-planned rather than abandoned: what is left is
worked out and queued as fresh pieces, and you see one line saying so. Four
things stop that from becoming a habit, and the first two read what actually
happened rather than counting:

- **Nothing is changing.** A round that left nothing at all on disk, after a
  round before it that also left nothing, ends the lineage:
  `carrying on has stopped changing anything — twice over now, nothing was written or altered — so this is handed over as it stands`.
  One fruitless round is never refused — work can run out before it writes its
  first file, and that is exactly the round this exists to buy.
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

Aforge's own practice has its own pocket: **$2.00 a day**, spent as at most two
firings of $1.00. When that is gone, practice stops for the day. It never asks
you to raise a rail on its own behalf — it just defers. The daily rail still
applies on top.

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
