# Standing goals and charters

A standing goal is something you want to remain true, checked over and over,
rather than something done once. Aforge calls the durable object behind it a
**charter**.

## Saying one

Durable language is recognized before any model gets a vote:

> "Whenever the pricing page changes, summarize what moved."
> "Every weekday at 9, check the build and tell me if it's red."
> "Remind me Friday to file the report."
> "Make sure the staging site stays up."

"Whenever", "each time", "every <thing>", "remind me when…", and "make sure X
stays Y" all mark an ask that survives its own completion. A reminder is just a
degenerate charter: one watch, one firing.

## The ratification card

Nothing stands up on its own say-so. You get one card to read and answer:

```
Whenever the pricing page changes, summarize what moved.
fires: whenever (poll)
costs: ~$0.15/firing, ≤10/day — caps the default worst day at about $1.50
expires: never
What should I do?
  1 yes, stand this up
  2 change the cadence
  3 once, not standing
```

Until you pick 1, it is a draft and it does nothing.

## The rails

Every charter carries three limits, stated on that card before you agree:

- **per-firing budget** — what one firing may spend. Default **$0.15**, or the
  measured cost of similar work when aforge has actually measured it.
- **max per day** — default **10** firings; a reminder gets **1**.
- **expiry** — default **never**; a one-off reminder expires within a day.

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
"make that hourly instead". Retiring is not deleting — the record of what it did
stays.

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
