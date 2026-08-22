# Standing orders — rules that stay true after this conversation

A standing order is something you said once that aforge keeps holding: "always run the
tests before you say you are done", "never touch the public API in this repo", "every
Monday draft the weekly update". It stays in force until you stop it.

It is the same one object as a reminder or a watch — see the keeping-an-eye page for
cadences, how it shares the daily allowance, and where the news lands. This page is
about the part that is a **rule**: where it applies, how to see what is standing, and
how to stop one.

## Rules, always do this, automations — all the same one thing

There is no separate "rules engine", no "automations" list and no macro language. Say
the sentence in your own words and aforge offers to make it a standing order:

- "always run the tests before you tell me it works"
- "never commit straight to main here"
- "every Monday at 9, draft the weekly update"
- "tell me when CI on main goes red"

They differ only in what wakes them — a clock, a look at the world, or nothing at all
for a rule that simply stays true and **holds**. Your own sentence is kept word for
word and is never rewritten; a short title may be drawn beside it on narrow rows.

## How do I set one up — you just say it

There is no command for this, no form, no `rules:` block in a config file and no macro
language. **You say the sentence**: "always run the tests before you tell me it works".
aforge recognises it and puts a card in the conversation; you answer the card, and it
stands.

- **In a conversation** — say it. The card appears in the transcript with your own
  sentence on it. `1` sets it up, `2` changes when it wakes, `3` does the thing once
  and leaves nothing behind, and `0` or `esc` says no.
- **From the home screen** — the `ask here` box takes the same sentence, and the card
  is drawn beside it with the same answers.

You can also ask for one in as many words — "make that a standing order", "remember
that for this project", "set that as a rule" — and the card still comes. Nothing is
ever created without one.

`/standing` (or `/orders`) shows what already stands over the conversation you are in;
`/home` shows everything standing on the machine, grouped by project.

## How does it know I mean always — instruction, or standing order

"Make sure" turns up in as many one-off instructions as it does in rules, so aforge
does not decide by the words. It asks one question about your sentence: **can it be
satisfied once and then forgotten?**

- **Yes — then it is part of the work you just asked for**, not a standing order.
  "Make sure this website you're building is 3 pages" is finished the moment the site
  has three pages. It becomes an acceptance criterion for the work in hand, and no card
  is drawn.
- **No — then it stands.** "Make sure the tests never break" can never be finished:
  work nobody has done yet could break them tomorrow. That is a rule, and it gets a
  card.

A sentence about the thing being built right now — "this website you're building",
"this PR", "what you're doing" — is about that thing, whatever words are in it. An
"always" in there is emphasis, not a rule.

**When it genuinely cannot tell, it does the instruction and offers the rule in one
line** instead of guessing. It applies your sentence to the work in front of it and
says something like *if you want this to hold for future work here too, say so and I'll
set it standing*. A card you did not ask for is worse than a question.

## Always do it this way — coding style rules, conventions and preferences

*Always do it this way. A coding style rule. Our conventions. My preferences.*

"Always run the tests before you tell me it works." "Never touch the public API in this
repo." "We use tabs here." "Prefer small commits." None of these has a time in it, and
none of them is waiting for anything to happen. They are simply true, and they stay
true.

aforge keeps that kind as a standing order that **holds**. It never fires, it is never
checked, and it costs nothing. What it does instead is ride into the world of the work
it reaches: a new conversation in this project opens already knowing it, and a task
starts with it in its brief under the heading `Standing orders`, told these are your
conditions and not suggestions.

Because nothing about one ever wakes, its card is short — your sentence, and the
`where ·` band saying how far it reaches. **No cadence line and no cost line**: there
is no moment to quote, and there is nothing it can spend. On every screen that lists
it, where a watch says what it last found, a rule says one word:

```
◦ never touch the public API                                          holds
```

On the home screen's `keeping an eye on` list, the ones that hold sit **after**
everything with an appointment, newest first — a rule has no "next", so it does not
belong in a queue of what happens next.

`p` pauses one, which stops it reaching new work; `s` stops it for good.

## Nothing stands until you say yes

Every one of them arrives as a card in the conversation, and **nothing is created until
you answer it**. The card carries your sentence, when it wakes, where it reaches, and
that it shares the day's allowance. `esc` or `0` declines, and a card left unanswered
when the turn ends sets nothing up: `the card was left unanswered — nothing was set up`.

Nothing is ever armed because a phrase looked like a rule. There is no matcher, no
inference from your files, and no order aforge made up on your behalf.

## Where an order reaches — this conversation, this project, everywhere

Three reaches, and the card always names the one it is asking for, on its own band:

```
where · just this conversation
where · for this project
where · everywhere
```

- **just this conversation** — it governs this chat and dies with it.
- **for this project** — it governs every conversation and every task in this project.
  This is what an order said in a chat gets by default.
- **everywhere** — every project on this computer. This is what an order said at the
  home screen's own box gets by default.

The reach is decided **on the card, never guessed silently**, and it does not drift
afterwards. Narrowing one later is free; widening one is a fresh card you answer again.

## Scope — the words that widen or narrow an order

Where you said it sets the default; the words in your sentence can move it:

- "just this conversation", "only here, in this chat" → **just this conversation**
- "in this project", "in this repo" → **for this project**
- "everywhere", "in all my projects", "on this machine" → **everywhere**

So "always run the tests before you say you are done, everywhere" said in a chat stands
over every project, and "keep this branch green, just in this conversation" dies when
the chat does. If you say nothing about it, the card still tells you which one it picked
before anything stands — read the `where ·` band before you press `1 yes, set it up`.

## /standing and /orders — what stands over this conversation

`/standing` (or `/orders`) opens a short page under the message box: the heading
`standing orders`, and up to three shelves in the order you read outward from where you
are sitting.

```
  standing orders
  in this conversation
› ▲ keep the tests green         needs your look · the fix touches migrations
  for this project
  ◦ draft the weekly update                                  Mondays at 9am
  everywhere
  ◦ never touch the public API                                        holds
  ─ not here: post the standup
```

Each row leads with the mark every aforge screen uses — `▲` needs you, `●` being
checked or fired right now, `◦` waiting for its time, `∙` paused or stopped — then what
the order is called, then where it stands. A rule that never wakes says `holds` there,
because it has no cadence and nothing it last found.

**A shelf with nothing on it is not drawn at all**, heading included. With nothing
standing here the page does not open: aforge says one line instead,
`nothing stands here yet — say what should always be true, and I'll hold it.`

The page is about **this conversation**. For everything standing on the whole machine,
grouped by project, open `/home`.

## The keys on the standing orders page

The line under the box says them while the page is up:
`enter open where it was asked · p pause · s stop · n not here · esc`

| Key | What it does |
| --- | --- |
| ↑ ↓ | move between orders; headings and `not here` lines are skipped |
| enter | opens the conversation that asked for this order |
| `p` | pauses it, or starts a paused one again — the receipt says `paused · …` or `going again · …` |
| `s` | stops it for good — `stopped · …` |
| `n` | not here: this place is excepted from it — `not here · …` |
| esc | closes the page and changes nothing |

`enter` on an order that was set up from the home screen and never became a conversation
says `made from home — no conversation to open`; on an order this very conversation
asked for it says `you are already in it`. Clicking a row moves the cursor and never
acts — every verb here is a key.

## Not in this project — the "not here" exception

*Not in this project.* An order that reaches wide is usually right and occasionally wrong
in one project. `n` on the standing orders page writes that down: the order stays exactly
as it is, and this project is excepted from it — it does not run here, and it does not
run in this conversation either where that is the exception you made. It then draws as
one dim line under its own shelf rather than as a row:

```
─ not here: post the standup
```

An exception names exactly one place — one project, or one conversation — and it is
always made **by you**. Two gestures write the same fact: `n` here, or saying so at the
moment an order does the wrong thing in a place. Nothing else ever writes one, and
nothing decides on its own that an order does not apply somewhere.

## The line when a conversation opens — N standing orders here

When you open a conversation that has orders over it, one dim line says so before
anything else:

```
· 3 standing orders here — /standing
```

Nothing stands, no line. It is a count and a door, not a list — the orders themselves
are one keystroke away on the page it names.

## Stop reminding me — stopping, pausing and narrowing one

Three different things, and they are not the same:

- **Stop it.** `s` on the standing orders page, or just say it: "stop the CI one", "stop
  reminding me about the plants". Stopping is permanent — setting it up afresh is a new
  card. If your words match more than one, aforge will not guess; it lists them and asks
  which.
- **Pause it.** `p`, or "pause the weekly update for now". A paused order is not checked
  and not fired, and it keeps everything it knows — what it has cost, when it last ran,
  what it last saw. `p` again starts it.
- **Not here.** `n`, when the order itself is right and this one place is the exception.

Anything you say in words needs no page at all; the page is there for when you want to
see what is true before you decide.

## What standing orders cannot do yet

Honest limits, so you do not rely on something that is not built:

- **An order shapes new work; nothing acts on a landing yet.** Every conversation and
  every task that starts in a place your orders reach now opens knowing them. They ride
  under the heading `Standing orders`, they say they are your conditions and not
  suggestions, and a task that cannot honour one is told to say so in its report. So
  "never touch the public API" is in front of a task before it writes a line. What is
  **not** built is the other direction: nothing re-reads your orders *after* a change
  lands and starts work to put it right, so a change that slipped past one is still
  yours to catch. That half is a later wave.
- **At most eight orders ride along.** When more than eight stand over one place, the
  ones that have stood the longest go, and the rest are counted — `…3 more`.
- **Money is not per order.** The card says it shares the day's allowance — the same
  machine-wide `daily_budget_usd` setting everything standing uses. If you named a
  per-run or per-day limit yourself, the card says your limit back instead.
- **There is no outward lane.** No phone, no email, no desktop notification. News lands
  in a chat you have open, or waits — the keeping-an-eye page has the order it is
  delivered in.
- **A firing cannot set up another order.** Nothing that runs on its own may arm
  something else that runs on its own.
- **Nothing is armed silently.** Every order on the page is one you answered a card for.
