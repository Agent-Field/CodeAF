# What I remember

## Do you remember me between conversations?

Yes, in two different ways. A handful of durable things are carried from one
conversation to the next: something you asked to be remembered, a preference you
stated, a correction you made, a decision that still binds. Those are short lines
and a new session starts with an empty screen — none of the old conversation is
put back on it.

The words themselves are not gone, though. Everything said in every conversation
on this machine is kept and can be **searched** — see "Can you look up what we
said in an earlier conversation" below. What is carried automatically is the
handful of lines; what was actually said is looked up when it is asked for.

The memory commands have two postures:

- `/memory` — open the memory panel.
- `/memory <query>` or `/memories <query>` — print only matching memories into
  the conversation.
- `/memories` — print every memory into the conversation, preserving its older
  list posture.
- `/remember <text>` — keep one thing.
- `/forget <query>` — drop the one thing that best matches.

Memory can be turned off entirely. The `memory` row in `/settings` is on by
default; off, nothing is carried, nothing is written, neither of the two calls
below is made, and the background tidy never runs. With it off, all three
commands answer:

```
memory is off · turn it on under /settings
```

## How do I see what aforge remembers about me?

Open `/memory` — or `/memories`, or `alt+4`, or `tab` from any other place. **Memory is a
place now**, one of seven, taking the whole screen with the tab bar above it and a composer
at the foot, rather than a twelve-row list drawn over the conversation.

The list starts with the most recently updated memories. Type to filter title, text and
tags — **every letter, `u` included** — with the same prefix, substring and fuzzy
subsequence ranking the model picker uses. A `*` marks a memory that has helped at least
five times.

**`alt+s` walks the shelves**: all, user, project, env, then all again. That was `tab` until
the places arrived; `tab` is the way to the next place now, and a view of a place belongs to
the `alt+<letter>` class.

Enter expands the selected memory to show its full text, tags, how many times it has helped
(`used · 7`), age, and where it came from. Esc returns to the list; esc from the list leaves
the place. `/memory <query>` and `/memories <query>` print matching lines into the
conversation instead of opening anything — a query is a question rather than a door.

## How do I edit a memory?

Two ways. Press **`→`** on the row and the verb strip offers `e fix the wording`; or press
enter to expand the row and then enter again (or `e`) to edit. The edit line is preloaded
with the complete memory text. Enter saves it; esc cancels without changing anything. The
title and tags stay as they were.

While the strip is drawn its letters are the verbs and the filter box is asleep; `esc` or
`←` closes it and every letter is a character again.

## How do I forget a memory and undo forgetting one?

In the memory place, **delete** (or ctrl+d) forgets the selected row immediately, and so
does `f` on the row's `→` strip. The line above the composer says
`forgot '<title>' · → put it back`; press **`→` then `u`** to restore it. Undo is one-deep:
only the most recent forget in this visit can be restored.

**`u` is not a bare key, and that is a fix rather than a cost.** It used to be matched ahead
of the filter, which meant the letter could not be typed at all — a search for a word with a
`u` in it lost the letter and put something back instead. On the strip it is a verb only
while the strip is drawn, and it is offered only while there is something to put back.

## Where did a memory come from, and who says so?

Expand it with enter. The provenance line reads `learned <age> in '<session
title>'` when the source conversation is known. Older rows without provenance
say `learned <age> ago`. Provenance answers “says who?”: memory is inspectable,
forgetting is one key, and an accidental forget has one undo.

## How does it decide what to put in front of the model?

It happens in two steps, and only the second one is a model.

First, **a ranking in the database picks the eight lines most likely to matter**
to what you just typed. It fuses three orderings the store already keeps: the
words themselves (a full-text match over title, text and tags), **how often each
line has actually helped before**, and how recently it changed. They fail in
different directions, which is the point — something you have leaned on for
months reaches the shortlist even when it shares no word with your message, and
something you corrected this morning reaches it on the strength of that alone.

Then **a small model on its own cheap tier reads those eight lines** — titles
only, never the full text — and answers which two or three of them bear on this
message. Only those are put in front of the model, as a short `<memory>` block.
Most messages need none, and an empty answer is the ordinary one.

The split is deliberate. Arithmetic is good at finding candidates and bad at
telling a near-miss from a match; a model is the opposite. So the model's whole
job is to throw out the lines that merely sound related — one
plausible-but-wrong line in the prompt costs more than the right one gains.

That is why a thousand remembered things cost the same as eight, and the eight
is a shortlist rather than a cap: anything remembered can reach it.

Two messages are never routed at all, because there would be nothing to match:
an empty message, and a continuation shorter than three words — `yes`, `go on`,
`that one` — unless it says `remember` or `forget`.

**If that small model is unreachable, the message goes out unchanged.** No
error, no warning, no memory in the prompt. A memory failure is never allowed to
break the thing you actually asked for.

## Does a remembered line show its age?

Yes, and it is told. Every line in the `<memory>` block carries when it was last
written, in the same words `/memory` uses:

```
- deploys on Fridays: Deploys go out on Friday afternoons. (learned 3mo ago)
```

Something learned in the last hour reads `just now`, then hours, days, weeks and
months. A memory old enough to be worth doubting is a memory that says so, which
is the difference between a standing preference and a fact about a project that
has moved on since.

A line whose age is unknown — an old row from before this was recorded — simply
carries no age rather than a zero. Nothing here asks a model to work out a date
range for itself; it is only ever shown one.

## Why did it say superseded?

Because a memory was **replaced by one that contradicts it**. It is one of the
two things here that change what is remembered without you asking — the other is
the background tidy further down — and it is the riskier one:

```
superseded · deploys on Fridays → deploys on Tuesdays
```

It happens when the pass that reads an exchange finds something durable, and
what the store already holds nearest to it says the opposite. The old line is
retired and the new one takes its place, in one step, so there is never a moment
where nothing at all is remembered about the subject.

This used to happen in complete silence, and that was wrong. Retiring something
true is the riskiest thing this feature does — it is a small model deciding, out
of ordinary conversation and with nobody asked, that something you said has
stopped being true. So it now says one dim line, exactly as `remember` and
`forget` do.

**The old line is not destroyed.** It is retired, not deleted: it leaves every
list, every search and every message, and the record of what it said survives.
If the replacement is wrong, `/remember` the original and it is written back.

The background tidy can retire a line the same way, and says so in the same dim
register — `memory tidied · 2 merged · 1 superseded`. It is held to a narrower
rule than this pass is: it may never retire a preference, a decision or a
correction.

## Does a memory count as used when it actually helped?

It asks. When a message was answered with remembered lines in front of it, the
same cheap pass that reads the exchange afterwards is also shown those lines and
asked which of them **bore on the answer** — as in, would the reply have been
different without it. It costs no extra call and about ten words of answer.

That number is what `used · 7` counts in `/memory`, and it is one of the three
things the shortlist is ranked by. It counts **help, not retrieval**: a line put
in front of a model that then had nothing to do with the reply is counted
*against* itself, so something that keeps sounding relevant and never once
changes an answer stops being offered. It is the same bargain aforge already
keeps with a suggested fix that gets offered and then fails.

Nothing is counted either way when that pass could not run. A provider outage is
not evidence that a memory failed to help.

## Can you look up what we said in an earlier conversation — searching old chats

Yes. aforge has a tool called `search_conversations`, and it searches **every
message of every conversation on this machine, verbatim** — what you typed, what
was answered, and what the tools came back with. Ask for something that was said
somewhere else — "what did we decide about the retry limit", "what did I tell you
about the deploy last week", "search my old conversations for the flag name" —
and it goes and looks instead of answering from memory.

Each result is one line: how long ago it was said, the conversation it was said
in, who said it, and the words themselves — plus the transcript file that
conversation lives in, which aforge can then open and read around the excerpt.

The limits are worth knowing:

- **Excerpts are bounded** at 400 bytes each, and there are eight of them by
  default (twenty at most). A search result is a pointer back into a
  conversation, not a replay of it — when the excerpt is not enough, the
  transcript named under it is read for the rest.
- **A search is words, not meaning.** It matches the words that were actually
  typed, newest first among equally good matches, so the person's own phrasing
  finds more than a paraphrase of it. Nothing found is said plainly rather than
  guessed at.
- **It is off when memory is off.** The conversations are kept in the same place
  the memories are, so the `memory` row in `/settings` turned off means nothing
  is written and there is nothing to search. Work handed to a task cannot search
  them either.
- **It is not the same as what is remembered.** The remembered lines are a few
  durable facts, extracted and rewritten; this is the conversation in its own
  words. Asked what was decided, aforge searches and quotes rather than
  reciting a memory, because the words somebody actually used are the answer and
  a summary of them is not.

There is no slash command for it — you ask in the conversation, and it searches.

## How does something get remembered without me asking?

After a message has been answered — off your path entirely, with nothing waiting
on it — the same cheap model reads the exchange and answers whether it held
anything worth carrying into another session. Most exchanges hold nothing, and
that is the answer it gives most of the time.

When it does find something, it is settled against what is already remembered
near it before anything is written. Four outcomes: it is added, it refines an
existing line, it replaces a line that has stopped being true, or it is skipped
because something already says it. That is what keeps telling aforge the same
preference in three sessions from leaving three near-identical lines behind.

Nothing is announced when a memory is **added or refined** by this pass — no
card, no line in the transcript; `/memory` is how you see what it did. The one
exception is a line that **replaces** something that contradicts it, which says
`superseded · old → new`, because retiring something you said is not a thing to
do quietly.

## Why did it say memory tidied — my memories got merged while I was away

Because a third call, quite separate from the two above, went over what is
remembered while nobody was here and tidied it. When it changes something it
says so in one dim line and nothing else:

```
memory tidied · 2 merged · 1 superseded
```

A half that is zero is left out, and a pass that changed nothing says nothing at
all — which is most passes. The line arrives in whichever conversation you most
recently touched, if any is open; on a machine with no window open there is no
line, and the change is simply there the next time you look at `/memory`.

**When it runs.** It rides the same 5-minute background pass that checks
everything standing, and three things have to be true at once: memory is **on**,
**nobody has said anything anywhere for fifteen minutes**, and the last tidy was
**more than six hours ago**. On top of that at least **two** remembered lines
must have changed since the last one — one new line has already been settled
against its neighbours on the turn that wrote it, so there would be nothing to
merge it with.

**What it may do.** It reads the fifty most recently touched lines, grouped by
how far each one's truth reaches, and answers with at most **eight** changes:

- **merge** two lines that say the same thing into one clearer line, keeping
  every fact both of them carried;
- **retire** a line that another line has replaced, putting the line that is
  true now in its place.

**What it costs.** One call on the **small work** class — the `consolidate` role
in `/settings` → Providers — a few times a day at most. It spends under the same
daily budget as everything else that runs in the background, and it is the first
thing a spent day stops paying for.

**Where the record is.** Every change is an ordinary memory event, so `/memory`
is where you see what it did — and because a line it rewrote was last touched by
the tidy rather than by a conversation, that row's `learned <age>` is the age of
the rewrite and it names no session. Nothing is deleted: a retired line keeps its
row and the store keeps what it said before. What the pass spent is one line in
the day's ledger under `~/.aforge/v3/standing/`, and it counts against the same
daily budget as everything else that runs in the background.

## Does it clean up or delete old memories on its own?

**It never deletes anything, and it never drops a memory for being old.** There
is no expiry, no half-life and no floor on how often a memory has to be used.
Something you told it a year ago is still there.

The only thing the background tidy above ever removes from the active list is a
line **another line has replaced** — the api moved from v2 to v3, the deploy
window changed — and even then the old row is kept and stays readable. Age on its
own is never a reason.

**And it will never replace something you said yourself with something it
worked out.** A `preference`, a `decision` and a `correction` are your own words
about how you want things — a correction is you saying aforge had it wrong — and
the tidy is not allowed to decide any of them has been superseded. It may sharpen
the wording of one, because you can read that and change it back; it may not
retire it. Only a plain `fact` and a `project_state` can be retired that way,
because those are the two that go stale on their own.

If you do want something gone, that is yours to do: `/forget <query>`, or delete
(or ctrl+d) on a row in `/memory`, with one undo behind it.

## Can you remember this for me?

Yes — say so, and it is written down immediately. "remember that I deploy on
Fridays" is recognised as an instruction about memory rather than a message to
answer, and it is confirmed in one dim line:

```
remembered · deploys on Fridays
```

`/remember <text>` is the same thing typed as a command. So is the `remember`
tool, which aforge reaches for itself when you have stated something durable: it
takes the line and, optionally, how far the truth reaches — `user` for something
true about you everywhere (the default), `project` for something true only in
this project, `env` for something true only on this machine.

All three go through the same settling step, so saying it twice refines one
memory rather than making a second.

## Forgetting something

"forget what I said about the deploy" is recognised the same way `remember` is,
and so is `/forget <query>`. The query is matched against what is remembered and
**the single best match is dropped**, with its title said back:

```
forgot · deploys on Fridays
```

If nothing matches, that is the answer and nothing is changed:

```
nothing matched pineapples
```

One at a time is deliberate. A query that matched three memories and quietly
dropped all three would be losing two things you never named.

A dropped memory leaves a tombstone rather than a hole — the record that you
asked for it to be forgotten survives, and the memory itself is out of every
list, every search and every message from that instant.

## What does remembering cost?

Two calls per message — plus one that is not per message at all: the background
tidy, on the **small work** class, a few times a day at most while nobody is
here. The two that ride every message are both on the cheapest of the four crew
classes — the
`reflex` class, which exists precisely because a call made twice a turn is a
different economy from one made once a session. It ships pointed at
`nex-agi/nex-n2-mini`. Each goes out with a short
prompt, a 200-token ceiling and temperature 0, and each is asked to answer in a
few words of JSON.

**Neither is allowed to think**, and that is a request on the wire and not just
an instruction in the prompt: the call carries the reasoning knob set to *off*,
so a reasoning model spends the 200 tokens on the answer instead of deliberating
first. It matters because the ceiling is otherwise a lie — a model left free to
think spends the whole 200 doing it and answers nothing, on every call of every
turn, billed in full for an empty reply. The knob is only sent to a model whose
catalog row says it accepts one; where the endpoint refuses to have thinking
turned off, the ceiling is raised to **2,000 tokens** instead so the answer has
room in front of it.

An unusable answer costs **one** retry and no more, and an *empty* answer costs
none — there is nothing to ask the model to fix. Either way the turn happens
exactly as it would have if the pair had never run.

Both are charged to the **session** total rather than to the message that
happened to trigger them, exactly as the session's own title and a compaction
summary are, so `/cost` and `/status` include them without any one message
reading as three times the price of its neighbours.

You can point that class at a different model — the **reflex** row in
`/settings` → Providers, or the whole crew in one word with `/crew` — or pin the
`reflex` role by itself under `pinned roles`. A change is live: the next turn's
pair uses it.

## Where is it kept, and does a task see it?

It is kept in `~/.aforge/graph.db`, which is per person rather than per
conversation or per project — so something remembered in one repository is
remembered in the next. `AFORGE_HOME` moves it with everything else aforge
keeps. The same file holds every message of every conversation, which is what
`search_conversations` searches; the transcripts themselves stay in each
conversation's own folder.

**A task gets the same treatment as a message.** When work is handed off to a
task, the router is asked once against that task's brief, and whatever it names
is put at the top of the task's own instructions. The task never writes memories
of its own: a family of eight tasks would otherwise be eight writers on one
brain, all blind to each other.

## Do you remember errors and how they were fixed?

Yes, and it is a separate thing from everything above. When a command fails and
the next run of that same tool works, the pair is written down: what the failure
said, and the command that made it go away. No model is asked anything to do it
— a failure followed by a success is something aforge watched happen.

The next time that same failure comes back, **one line is added to the bottom of
the failed row**, and it is the only place you will ever see this:

```
this exact error was fixed 7/8 times before · what worked: make clean && make build
```

7 is how many times that command has worked; 8 is how many times it has been
tried. Only **one** suggestion is ever offered — the one with the best record —
and nothing at all is said when the record is worse than three tries in five,
because a coin toss dressed as advice is worth less than silence.

This only happens **after** something has already failed. Nothing is looked up
before a command runs, and a command that works is never annotated.

Only `bash`, `grep` and `find` are remembered this way, because their answer is
something you could run again. A `read` that could not find a file is not: the
"fix" would be one particular path, right for that call and wrong for every
later one.

## Why did it say this error was fixed before, and how did it know?

Because it watched it happen here, on this machine, in an earlier conversation
or an earlier task — nothing is shipped with aforge and nothing is learned from
anybody else's work.

Two files hold it, both called `fixes.json`. One sits beside this project's
conversations and is asked first, because a fix is usually about this
repository — its toolchain, its build tags, the one `grep` on this machine that
will not take that pattern. The other sits at the top of aforge's own folder and
is what makes the first failure in a brand new checkout cheap. `AFORGE_HOME`
moves both, with everything else aforge keeps.

**Tasks write into the same project file.** A worker hammering a build in its own
worktree is where most of this comes from, and a private file nobody ever read
would waste it.

The counts are a confidence rather than a tally: they are **halved every week**,
so something confirmed once months ago drops out entirely and something
confirmed again this morning rises. A suggestion that gets offered and then
fails on the same error is counted against itself, and stops being offered once
its record falls.

There is no command for this and no panel: it is not part of `/memory`, nothing
about it appears there, and turning memory off does not turn it off. The one
line on a failed row is the whole of what it ever says.

## I used to have a memory.md file

Earlier builds kept memory as one file of lines at `~/.aforge/v3/memory.md`,
written by a `note` tool and filtered by a `forget` tool. Neither tool exists any
more, and the file is no longer read on every message.

The first time a conversation starts with memory on and that file still there,
every non-empty line in it is imported as a remembered fact and the file is
**renamed** to `memory.md.imported` — never deleted, so your own copy of what you
wrote is still on disk. It says so once:

```
imported 12 memories from memory.md
```

After that the file is gone from aforge's view and the store is the only memory.
