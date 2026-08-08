# Standing goals and charters

A standing goal is something you want to remain true, checked over and over,
rather than something done once. Aforge calls the durable object behind it a
**charter**.

## Saying one

Durable language is recognized before any model gets a vote:

> "Whenever the pricing page changes, summarize what moved."
> "Every weekday at 9, check the build and tell me if it's red."
> "Remind me every Sunday to water the plants."
> "Remind me at 6pm to leave for the school run."
> "Make sure the staging site stays up."

"Whenever", "each time", "every <thing>", "remind me when…", and "make sure X
stays Y" all mark an ask that survives its own completion.

Time is read the way you say it: a named day ("every Sunday", "on Tuesdays"), a
clock ("at 8pm", "at 7:45"), a part of the day ("every morning"), an interval
("every 20 minutes"), or a single moment ("tomorrow at 9", "in two hours"). Days
and clocks are in your machine's own timezone. A named day repeats on that day
forever; only a single moment happens once and then stops.

## The ratification card

Nothing stands up on its own say-so. You get one card to read and answer:

```
Remind me every Sunday to water the plants.
when: Sundays at 9am
costs: about $0.02 a run, at most 1 a day
What should I do?
  1 yes, stand this up
  2 change when it runs
  3 once, not standing
```

Until you pick 1, it is a draft and it does nothing.

When you did not say when — "watch for the Dyson dropping under 500" says
nothing about how often — the card does not invent a rhythm and state it as
fact. It says what it would do and asks:

```
when: about every 2 minutes — you didn't say, so that's my guess. Right?
```

Answer with words ("every morning", "Sundays at 8pm") and that becomes the
schedule.

## The rails

Every charter carries three limits, stated on that card before you agree:

- **per-firing budget** — what one firing may spend. Default **$0.15**, or the
  measured cost of similar work when aforge has actually measured it.
- **max per day** — default **10** firings; a reminder gets **1**.
- **expiry** — default **never**. Only a reminder for a single moment expires,
  and it expires within a day of that moment; a rule that repeats does not
  expire because it ran.

At firing time they are checked in order: expired charters retire, a charter at
its daily quota is blocked, and one that would push you past your daily rail is
deferred and you are asked before anything spends.

## Probation and tenure

A new charter is on **probation**: every firing is proposed to you first. After
**3** consecutive clean firings it becomes tenured and fires without asking. One
bad firing — including one that cost more than its own per-firing budget —
demotes it back to probation.

## Cadence, pausing, retiring

Say it plainly: "pause the pricing watch", "stop reminding me about Friday",
"make that hourly instead", "change it to Tuesday", "push the reminder to 8pm",
"change the plant reminder to say water the plants and take the bins out".
A day or a clock moves when it runs; "to say …" changes what it says and leaves
the timing alone. If more than one rule could be the one you mean, you get one
short question naming each in your own words. Retiring is not deleting — the
record of what it did stays.

If your cadence words are not recognized as a clock ("whenever the file
changes"), the charter becomes a poll rather than a schedule, and the card tells
you so before you agree.

## Where to see them

- `/standing` lists your active charters.
- The **self** place (alt+3) shows them alongside what aforge has learned.
- `aforge doctor` prints them from outside the chat, together with the standing
  watch state and today's spend.

## Keeping watch when you are not here

The first time you ratify a charter, aforge asks — once, ever — whether it
should keep checking when you are not around. Saying yes installs an operating
system timer that wakes it every few minutes with no terminal open. That is the
standing watch, and it is what makes a charter mean anything overnight.
