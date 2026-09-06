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

There is no form to fill in, no `rules:` block in a config file and no macro language.
**You say the sentence**: "always run the tests before you tell me it works".
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

Two gestures say the same thing outright, for when you want to be sure it is read as a
rule rather than as work: **`ctrl+enter`** instead of `enter`, and **`/standing <words>`**
typed as a command. Both go through the same door, and both still end in a card.

Bare `/standing` (or `/orders`) shows what already stands over the conversation you are in,
and under that, everything else standing on this computer under the heading `in other
projects`. `/home` shows the same orders filed under the project each one belongs to.

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
conditions and not suggestions, and to say so in its report if it cannot honour one.
That is the whole of how a rule is kept, and it is the only kind that arrives in those
words — see *What rides into the work* below.

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

On home, `ctrl+e` pauses one, which stops it reaching new work; `ctrl+x` stops it for
good. (On the standing orders page, press `→` on the row and the same two verbs appear as
`p pause` and `s stop`.)

## Nothing stands until you say yes

Every one of them arrives as a card in the conversation, and **nothing is created until
you answer it**. The card carries your sentence, when it wakes, where it reaches, and
that it shares the day's allowance. `esc` or `0` declines, and a card left unanswered
when the turn ends sets nothing up: `the card was left unanswered — nothing was set up`.

Nothing is ever armed because a phrase looked like a rule. There is no matcher, no
inference from your files, and no order aforge made up on your behalf.

## Make it standing on purpose — force it to be standing, make this permanent

Type the sentence and press **`ctrl+enter`** instead of `enter`. That send means "keep
this true", and aforge is *told* rather than left to work it out: it shapes your sentence
into a standing order's card — when it wakes, what it does, how far it reaches — and it
does **not** carry the sentence out as one-off work as well.

The card that comes back is the ordinary ratification card, so nothing stands until you
answer it. `ctrl+enter` decides how the sentence is *read*, never whether something is
created.

If the sentence cannot stand at all — "what time is it?", a one-off command with no
condition in it — aforge says so in one short line, tells you what would make it stand,
and does nothing else. It never quietly does it instead.

Two things it refuses rather than sending:

- A box holding pictures or a picked shape of work:
  `ctrl+enter keeps a sentence true — take the pictures or the shape of work off first`.
  Your draft stays in the box.
- A build with no ambient side at all: `nothing here can hold a standing order`.

A slash command is unaffected — `ctrl+enter` on `/standing` is just `/standing`.

There is a typed form of the same door: `/standing <words>` — see *Make a rule in one
line* below.

**One limit, and it is the terminal's.** `ctrl+enter` reaches aforge only on a terminal
that can tell it apart from a plain `enter` (the kitty protocol, win32-input). `alt+enter`
cannot be borrowed for it here — in a conversation that chord opens a new line in the
message. If `ctrl+enter` does nothing on your terminal, say it in words instead: "always
run the tests before you say you are done" is recognised on its own.

## It didn't notice — it did the rule once instead of keeping it

This is the failure the chord above exists for. Say "run the tests whenever I push" and
aforge may read it as work to do now: the tests run, you see something happen, and
**nothing was set up**. There is no error, because nothing failed — it answered a
different request.

Two things help:

- **Say it again with `ctrl+enter`.** That send cannot be read as work.
- **Watch the hint under the box.** When your draft looks like a condition, the line at
  the right end of the rule under the message box reads `ctrl+enter keeps this true`. It
  appears on drafts beginning with or containing `always`, `never`, `every`, `whenever`,
  `each time`, `from now on`, `remind me`, `keep an eye`, and on `make sure` beside a
  `never` or an `always`. It vanishes the moment the draft stops matching or is emptied,
  and it never moves the box — it rides a line that is on the screen either way.

That word list is a courtesy for teaching you the chord, **not** the rule for what can
stand. Plenty of standing sentences never trip it, and they are still recognised when you
just say them. The hint is also absent wherever the chord would refuse — a tray with
pictures on it, a picked shape of work, a build with no ambient side.

To check afterwards whether anything actually stands, open `/standing`, or read the
`◦ keeping an eye on N` count at the foot of the screen.

## keeping an eye on 2 — the count at the foot of the screen opens the page

The status line's `◦ keeping an eye on N` segment is a **door**. Click it and the standing
orders page opens — the same page `/standing` and `/orders` open, showing which N those
are. It brightens under the pointer, the way the model's name beside it does.

It counts the active orders reaching **this** window's project, so it is the fastest
answer to "did that actually stand?". Nothing standing here means no segment at all and
nothing to press.

Its mark moves while an order of this place is being acted on right now, and is still
otherwise — so movement there means one of your orders is working, out of the corner of
your eye. The keeping-an-eye page has the marks in full.

With the mouse turned off, `/standing` is the keyboard door and always has been.

## The list on the right — the standing section in the column beside the conversation

The column down the right of the screen has **two sections**, each under its own dim
lowercase label. `tasks` is at the top — the task roster, one line per task, exactly as it
always was. `standing` is under it, and it is the orders standing over this conversation.

```
tasks
⠙ Fix the nil-map       #7
+ /task

standing
◦ keep the tests green
◦ never touch the API  everywhere
+ /standing
```

- **One line per order**: its mark, what the order is called, and a dim tail on the right
  naming how far it reaches — but only when the reach is not the usual one.
- **The tail says `everywhere`** for an order governing every project on this computer, and
  **`just here`** for one that stands in this conversation only. **An order that governs
  this project — the ordinary case — has no tail at all.** No tail means the project.
  Tasks never carry one: a task is always this conversation's, and reach is a standing
  order's idea. **The name outranks the tail**: on a narrow terminal, where the column is
  twenty-four cells rather than thirty, the tail gives way so the order's own name keeps
  its room. `/standing` says the reach in full whatever the width.
- **A row breathes while its order is being checked or fired right now** — its mark becomes
  the spinner, exactly as the `keeping an eye on 2` chip on the status line does. It is
  still every other moment.
- **Orders with a next occasion coming are listed first**, and the rules that merely
  `holds` sit under them: a rule has no "next", so it does not belong in a queue of what
  happens next.
- **Clicking a row opens `/standing` on that order**, with the cursor already standing on
  it, so `p`, `s`, `n` and `enter` act on the one you pressed rather than on a list you
  have to find it in again.

**A label is drawn only when its section has rows** — the typeable door exists before
anything is in it. The rows under a label are only the real ones: with nothing standing,
the `standing` section is its label and its `+ /standing` row and nothing between them.

These rows answer to the **pointer**. The roster's keyboard cursor (`ctrl+t`) walks the
task rows only. On a build with no ambient side there is no `standing` section at all —
the column is the roster it always was.

## What is that + at the bottom — + /task and + /standing, adding one from the side

Each section of the column on the right ends in one dim `+` row: `+ /task` under the tasks,
`+ /standing` under the standing orders. It is how you make a rule without knowing a
command first.

**Pressing one types that command into your message box** — the word and a trailing space —
and hands the keyboard straight back to the box. That is the whole of it. What lands is
ordinary text you can edit or delete; there is no mode, no form, and nothing is created.

- **The word goes at the head of the line and keeps what was already typed there.** A box
  holding `fix the flaky test` with `+ /standing` pressed becomes
  `/standing fix the flaky test`, and with `+ /task` pressed, `/task fix the flaky test` —
  which is the line you were about to type anyway.
- **Pressing it twice does nothing the second time.** The word is already at the front, and
  a draft reading `/standing /standing ` is the gesture arguing with itself.
- **The `+ /standing` row is there when nothing stands**, directly under the `standing`
  label with no rows between them.

Then you finish the sentence and send it. `/standing <words>` is the form it becomes —
the words are shaped into a standing order's card, and nothing stands until you answer it.

Both rows are the **pointer's**: the roster's keyboard cursor walks task rows and skips
these. From the keyboard, type the command — that is what the row was teaching.

## /standing <words> — make a rule in one line, without the chord

`/standing` with words after it is the command's second form: **the words are a new
standing order.**

```
/standing always run the tests before you say you are done
```

They go through the same deliberate door `ctrl+enter` opens, with the same guarantee:

- **It is never read as work to do once.** aforge is *told* to shape your sentence into a
  standing order's card — when it wakes, what it does, how far it reaches — and it does not
  carry the sentence out as one-off work as well.
- **Nothing stands until you answer the card.** What comes back is the ordinary
  ratification card: `1` sets it up, `2` changes when it wakes, `0` or `esc` says no.
- **A sentence that cannot stand at all** — "what time is it?", a one-off command with no
  condition in it — gets one short line saying so, and nothing else happens.
- **Typed while an answer is still arriving**, it waits above the box like any other
  message and goes through the marked door when its turn comes.
- **On a build with no ambient side** it says `nothing here can hold a standing order` and
  sends nothing.

**A bare `/standing` (or `/orders`) is unchanged** — it opens the page of what already
stands over this conversation. The command list carries both: `/standing` on its own, and a
second row spelled `/standing <words>` with the tail
`…or keep this true · a card, never work done once`.

The `+ /standing` row at the foot of the column on the right types this command into your
box for you.

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

On the `standing` section of the column on the right there is no room for a band, so the
same three reaches are a dim tail at the end of the row — `everywhere`, `just here`, and
**nothing at all for the project**, which is the one nearly every order has.

## Scope — the words that widen or narrow an order

Where you said it sets the default; the words in your sentence can move it:

- "just this conversation", "only here, in this chat" → **just this conversation**
- "in this project", "in this repo" → **for this project**
- "everywhere", "in all my projects", "on this machine" → **everywhere**

So "always run the tests before you say you are done, everywhere" said in a chat stands
over every project, and "keep this branch green, just in this conversation" dies when
the chat does. If you say nothing about it, the card still tells you which one it picked
before anything stands — read the `where ·` band before you press `1 yes, set it up`.

**And if the reach is wrong, change it on the card.** `2 change when or where` turns the box
below into a place to say either one: "only in this project", "everywhere", "just this
chat" — the same door that changes the time. `enter` sends your words back, nothing is set
up yet, and a new card comes with the reach you asked for on its `where ·` band.

## /standing and /orders — what stands over this conversation, and everywhere else

`/standing` (or `/orders`) opens the standing place, which takes the whole terminal: the
heading `standing orders`, and up to **four** shelves in the order you read outward from
where you are sitting.

```
  standing orders
  in this conversation
› ? keep the tests green         needs your look · the fix touches migrations
  for this project
  ◦ draft the weekly update                                  Mondays at 9am
  everywhere
  ◦ never touch the public API                                        holds
  ─ not here: post the standup
  in other projects
  ◦ watch the release feed                                   Mondays at 9am
```

The first three are **where an order reaches**: this chat, this project, every project.
The fourth, `in other projects`, is everything else this computer is holding that does
not reach the conversation you are in — including orders in a folder you have never held
a conversation in. An order is on exactly one shelf: what already stands over this
conversation is never repeated down there.

Each row leads with the mark every aforge screen uses — `?` needs you, `◐` being
checked or fired right now, `◦` waiting for its time, `∙` paused or stopped — then what
the order is called, then where it stands. A rule that never wakes says `holds` there,
because it has no cadence and nothing it last found. Rows are drawn the same way on all
four shelves.

**Under the row your cursor is on**, an order that has been looked at adds a short
paragraph — its name, how long ago, and what the look found:

```
  the 6am watch, last look · 3h
  looked 3h ago · nothing had changed since yesterday, so nothing was done
```

An order that has never been looked at — a rule, or one whose moment has not come —
draws no paragraph at all.

**A shelf with nothing on it is not drawn at all**, heading included. When nothing at all
stands on this computer the page still opens and teaches — *Nothing stands here yet — the
empty standing page* below.

**Nothing is hidden behind a fold.** However many orders there are, `↑ ↓` walks them all
and the list scrolls to keep the one you are on in view; `pgup` and `pgdown` move by a
screenful of whatever your terminal is tall. `/home` shows the same orders filed under
the project each one belongs to.

**With words after it the command means something else entirely**: `/standing <words>`
makes a new order out of those words, through the same door `ctrl+enter` opens — see *Make
a rule in one line*. Nothing on this page is ever named at the command line; the way to act
on one of these is the keys below.

The same page opens with the cursor already on one order when you click its row in the
`standing` section of the column on the right.

## The keys on the standing orders page

The line under the box names the verbs of **the row you are on**, and it never names one
that is not bound. On an order that stands over this conversation it reads:
`enter open where it was asked · → verbs: pause, stop, not here · esc`

The verbs are on the row's own strip: press **`→`** and they appear as
`p pause   s stop   n not here` under the list, and only while that strip is drawn are those
letters verbs. `esc` or `←` closes it. They were bare letters while this was a small list
drawn over the conversation; standing is a place now, with a composer at the foot, so every
printable key belongs to the composer.

| Key | What it does |
| --- | --- |
| ↑ ↓ | move between orders; headings, `not here` lines and the `last look` paragraph are skipped |
| pgup pgdown | move by a screenful |
| enter | opens the conversation that asked for this order |
| `p` | pauses it, or starts a paused one again — the receipt says `paused · …` or `going again · …` |
| `s` | stops it for good — `stopped · …` |
| `n` | not here: this place is excepted from it — `not here · …` |
| esc | closes the page and changes nothing |

**On the `in other projects` shelf the strip is shorter**, because those orders do not
stand over this conversation: `→` offers `p pause` (or `p start again` on a paused one)
and `s stop`, and the hint line names only those two. **`n` is absent there** — an
exception names a place an order actually reaches, and that one does not reach here. Those
two writes go straight to the stored order and the page is redrawn from what was saved; if
this window has no way to write, no verbs are offered at all rather than keys that would
fail.

`enter` on an order that was set up from the home screen and never became a conversation
says `made from home — no conversation to open`; on an order this very conversation
asked for it says `you are already in it`. Clicking a row moves the cursor and never
acts — every verb here is a key. Clicking a heading, a `not here` line or the `last look`
paragraph does nothing at all.

**This page does not change how hard an order thinks.** `ctrl+v` does nothing here. The
rung lives on the item's own card on **home** — put the cursor on its `◦` row and press
`ctrl+v` there — because that card is where the rung is drawn and a key belongs beside the
fact it moves. The home page has the whole of it.

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

## What rides into the work — how an order reaches a task or a new conversation

Every conversation that opens, and every task that starts, in a place your orders reach
gets them in front of it, under one heading:

```
Standing orders:
```

**They do not all arrive in the same words, because they are not all the same kind of
thing.** A rule that holds — "always use tabs here" — is a condition on the work, and it
is handed over as one: *these are the person's own conditions over this place … they are
not suggestions. Work within them.* A reminder, a rhythm, a file being watched, a check
of the world — those are appointments on your clock, answered by aforge itself when
their moment comes. They ride along too, so the work knows what else is standing here,
but under a plainer sentence: *these are the person's own standing orders over this
place, each waiting on a moment, a rhythm or a change of its own … none of them is a
condition over this work and none asks anything of you now.*

When both kinds reach one place there is still **one heading and one section**, with the
rules first and the appointments under them, because the rules are the half that can
change what the work does. A task also gets one closing line the conversation does not:
if it cannot honour one of these, it says so in its report.

Why it matters: told that "remind me at 6 to check the deploy" was a condition to work
within, a worker will hedge everything it does for a sentence that was never about it.

## Stop reminding me — how do I delete, remove or get rid of a standing order, and stopping, pausing or narrowing one

**Delete, remove and get rid of are all the same thing here, and the word for it is
stop.** There is no separate delete: `s` on the standing orders page stops the order for
good, and the receipt reads `stopped · ` and then the order's own words back at you. The
row then leaves the page and home, because neither is keeping an eye on it any more; the
conversation that set it up still holds the whole record of what it did.

Three different things, and they are not the same:

- **Stop it.** `s` on the standing orders page, or just say it: "stop the CI one", "delete
  the CI one", "stop reminding me about the plants". Stopping is permanent — setting it up
  afresh is a new card. If your words match more than one, aforge will not guess; it lists
  them and asks which.
- **Pause it.** `p`, or "pause the weekly update for now". A paused order is not checked
  and not fired, and it keeps everything it knows — what it has cost, when it last ran,
  what it last saw. `p` again starts it.
- **Not here.** `n`, when the order itself is right and this one place is the exception.

Anything you say in words needs no page at all; the page is there for when you want to
see what is true before you decide.

## The standing place — the whole screen, four shelves, no fold

The **standing** place is a full place on the tab bar, not the old short overlay. It takes
the whole terminal, which is what lets it show every order you have rather than the first
four.

- **The rows are filed on shelves, not sorted into one flat list.** The first three are
  how far an order reaches — `in this conversation`, `for this project`, `everywhere` —
  and the fourth, `in other projects`, is what this computer holds that does not reach the
  conversation you are in. Inside a shelf the order is the same triage home uses: what
  needs you, then what is moving, then everything else.
- **A row says how much rope the order has, then what home says about it** — its mark, its
  name, the rope (*How much rope one has* below), and one clause: `needs your look · …`,
  `checking now`, `paused`, `holds`, `checked 4m ago · …`, or its cadence. There is one
  derivation of that clause for the whole program, so this page and home can never disagree
  about an order in front of you. On a narrow terminal the clause gives way first and the
  rope stays: it is the fact that decides whether you have to watch the thing.
- **Nothing is folded away.** `↑ ↓` walks every row and the window scrolls with the cursor;
  `pgup` and `pgdown` move by a screenful of your terminal, not by a fixed twelve.
- **The `last look` paragraph belongs to the row your cursor is on**, and is drawn only
  when that order has actually been looked at.
- **`enter`** opens the conversation where the order was asked for. **`→`** draws the row's
  verbs — `p pause` / `p start again` and `s stop` everywhere, plus `n not here` on the
  three shelves that reach this conversation. Writes on the fourth shelf go to the stored
  order and the page is redrawn from what was saved.

- **`shift+←` and `shift+→`** move the time window in the header — *When it fired* below.

Typing remains composer text and `tab` moves to the next place. When nothing at all stands
on this computer the place opens on its own three sentences — *Nothing stands here yet — the
empty standing page* below has them.

## Nothing stands here yet — the empty standing page

A machine nothing stands on **still opens the page**. `alt+3`, `tab`, `/standing` and
`/orders` all reach it, and what they reach is three dim lines saying what a standing order
is:

```
nothing stands here yet — say what should always be true, and I'll hold it.
an order stands until you stop it, and it can reach just this conversation, this project, or everywhere.
enter opens the conversation that made one, when there is one here.
```

No shelf heading is drawn over it, and neither is the time window in the header: a control
naming a span of days on a machine that has never held an order is a control about nothing.
The tab bar, the composer and the top line are all there as usual, so `tab` walks on and
anything you type is still the first sentence of something new.

The first of those lines used to be what `/standing` said **instead** of opening, with no
page behind it. On a machine aforge was installed on an hour ago that is every door onto the
page, so the sentence stayed and the refusal went.

A list emptied by the **time window** rather than by the machine is a different screen: it
keeps its header, because the header is the control that pages the window back.

## How much rope one has — asks first, earning trust, trusted alone

Every row on the standing orders page says how much rope that order has, in three words at
most. It is the one column that decides whether you have to watch the thing:

| What the row says | What it means |
| --- | --- |
| `asks first` | You have never told it what it may do unattended. It can tell you things and nothing else |
| `earning trust 3/5` | You gave it a licence, and it has fired 3 times in a row without needing you. Five in a row earns the next rung |
| `trusted alone` | It has a licence and 5 clean firings in a row behind it |

**The licence is the thing you said on the card**, in your own words — "open a pull request
but never merge it". With none, an order is on `asks first` however long it stands and
however often it fires; firing is not how rope is earned, it is how a licence you already
gave is confirmed.

**A clean firing is one that came back with nothing waiting for you and no failure.** One
firing that stops on a question, or that could not finish, puts the count back to `0/5` — so
`earning trust 0/5` after a run of good ones means the last one asked you something.
Trust is a run, not a tally: five good mornings do not buy an order past the one that asked.

**A rule that only `holds` says nothing about rope at all.** Nothing examines it and nothing
fires it, so it can never act unattended and the question does not arise.

Two honest limits. The count starts at zero for every order that existed before aforge began
keeping it — nothing on disk says those old firings were clean, so they are not counted.
And **the rung is a label, never a permission**: what a firing is allowed to do is your
banked approval rules and only those. Nothing widens because a count went up.

## When it fired — the time window on the standing orders page

The standing orders page has a time window, drawn in its header as the control and the
reading at once:

```
  standing orders                                       shift+← aug 12 – aug 25 →
```

- **`shift+←` and `shift+→`** move the window **by its own length** — one press is the
  previous or next span, not the previous day.
- **`shift+↑`** makes it coarser (days → weeks → months) and **`shift+↓`** finer, keeping
  the same number of buckets.

**It opens holding everything.** The span it starts on reaches back to the oldest firing this
computer has, so the first frame hides nothing — narrowing is something you do on purpose.
Once narrowed, orders whose last firing falls outside the window are not listed.

**An order that has never fired is never hidden by it.** A rule that only holds, and a watch
whose first moment has not come, have no firing to be outside a window — so they stay on the
page at every span. Moving the window never reads the disk: the orders are already in hand,
so you can hold the arrow down.

**On a narrow terminal there is no window at all.** Below about 60 columns the header has no
room for the control, so it is not drawn — and the four keys do nothing there rather than
moving something you cannot see.

## What standing orders cannot do yet

Honest limits, so you do not rely on something that is not built:

- **An order shapes new work; nothing acts on a landing yet.** Every conversation and
  every task that starts in a place your orders reach now opens knowing them. They ride
  under the heading `Standing orders` — the rules as your conditions and not suggestions,
  the ones waiting on a moment as what else is standing here (*What rides into the work*
  has both sentences) — and a task that cannot honour one is told to say so in its
  report. So "never touch the public API" is in front of a task before it writes a line.
  What is **not** built is the other direction: nothing re-reads your orders *after* a
  change lands and starts work to put it right, so a change that slipped past one is
  still yours to catch. That half is a later wave.
- **At most eight orders ride along.** When more than eight stand over one place, the
  rules that hold go first — whatever their age — and the longest-standing of the rest
  fill what room is left. What was cut is counted rather than dropped quietly: `…3 more`.
- **Money is not per order.** The card says it shares the day's allowance — the same
  machine-wide `daily_budget_usd` setting everything standing uses. If you named a
  per-run or per-day limit yourself, the card says your limit back instead.
- **There is no outward lane.** No phone, no email, no desktop notification. News lands
  in a chat you have open, or waits — the keeping-an-eye page has the order it is
  delivered in.
- **A firing cannot set up another order.** Nothing that runs on its own may arm
  something else that runs on its own.
- **A firing thinks at `low` unless you raised that one thing.** Standing work is held to
  the cheapest rung however deep this machine is dialled, because it is unattended and it
  repeats. `ctrl+v` on the item's row on home raises the one that needs it, and nothing
  else does — there is no way to raise them all at once, on purpose.
- **A firing gets no sizing call.** Work you type is read once for width before it starts;
  an order that fires is not, and is armed to split itself only off the items its own
  brief already names. Asking a model every night whether a sentence that has not changed
  is wide would be a bill you never agreed to. (What splitting is, and everything that
  decides it, are on the tasks page.)
- **Nothing is armed silently.** Every order on the page is one you answered a card for.
