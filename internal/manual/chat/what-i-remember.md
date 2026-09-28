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
below is made, and the background tidy never runs. With it off, `/remember`,
`/forget` and `/memories` answer:

```
memory is off for this session · turn it on under /settings
```

Skills are not memory, and turning memory off keeps them: the skills in your
Claude Code, Codex and other skill folders still reach the conversation, carried
with a message that suits them and attached by `/skill`
(`skills-from-other-tools` says how).

**The memory PLACE still opens with it off.** `alt+8` and `/memory` both reach it, and
what they reach is the heading `memory` and its one line, with that same sentence written
once into the rule above the composer — *What the memory place shows when there is nothing
in it* below. It used to refuse to open at all, which made the memory key on a fresh machine a key
that did nothing.

## It said memory is off and I never turned it off

Then it is not the setting, it is the file. Two different sentences use the same
words, and the one printed on the terminal before the screen appears always says
what the trouble was after a colon:

```
memory is off for this session: could not open ~/.codeaf/graph.db: permission denied
```

Everything remembered on this machine lives in one file, `~/.codeaf/graph.db`,
and every part of codeaf opens that same file. When it will not open, this
conversation runs without memory rather than refusing to start — you asked for a
conversation, and a sulky file is no reason not to have one. Nothing already in
the file is lost by this; nothing new is written until it opens again.

The reason after the colon is the operating system's own, and it is the thing to
act on:

- `permission denied` — something owns `~/.codeaf` that you do not. `ls -la ~/.codeaf`
  says who.
- `read-only file system` — the disk it is on will not take writes.
- `file is not a database` / `database disk image is malformed` — the file is
  damaged. Move it aside and the next launch makes a fresh one, empty.

**On a machine that has never run codeaf there is no trouble to report.** The
folder is made on the way in, and the first launch comes up with an empty memory
that teaches rather than an error. It did not always: a first run once reported
`out of memory (14)`, which was never true — that is one code SQLite uses for
every reason a file will not open, and a build old enough to print it is a build
worth replacing.

## Where is everything you remember kept

In `~/.codeaf/graph.db`, one file, made the first time codeaf runs. Memories,
every conversation this machine has held, and the work it has run are all in it,
which is why a memory kept in one project is there in the next.

`CODEAF_HOME` moves the whole folder — set it and codeaf keeps everything
somewhere else, which is how a disposable run gets a brain of its own without
touching yours.

Copy the file to another machine and your memories go with it. Delete it and
they are gone; nothing else keeps a second copy.

## How do I see what codeaf remembers about me?

Open `/memory`, or `/memories`, or `alt+8` from anywhere (memory is not on the tab bar). **Memory is a
place**, one of seven, taking the whole screen with the tab bar above it and a composer at
the foot.

The page is **shelves**, not a flat list. There are exactly three of them, because a memory's
scope is a closed three: **you** (true everywhere), **this project**, and **this machine**.
They are drawn biggest first, under a section line that says what the shelves are made of —
`fact 12 · preference 8 · correction 3`. The biggest shelf opens itself; the rest stay rolled
up with `▸` in front of them, and a fold at the bottom says exactly how many lines or shelves
are not shown.

The top line is the count: `41 held · 3 shelves · 2 let go · 1 replaced`, with
`type to filter` out at the right. A half that is zero is left out entirely, and **let go and
replaced are counted apart** — one you asked for, the other happened on its own.

Each line says what it is and **how it has done**, in plain words rather than a percentage,
and every field on the row is joined by the one separator this surface joins facts with:

```
· tests live beside the file they test · lesson · helped 19 · bore on 3      2w ago
· releases go out before lunch · quirk · new, learned 3h ago                 3h
· the rail owns the cursor · fact · let go                                   6d ago
· deploys on Fridays · fact · replaced                                       3d ago
```

The **title is whole** whatever the frame is; the facts behind it are a ranked prefix, so a
narrow terminal drops `helped 19 · bore on 3` from the end rather than cutting the line's own
words. The age stays out at the right.

`helped` is how many times a line actually changed an answer; `bore on` is how many times it
was put in front of a model and had nothing to do with the reply. A line that was **let go**
stays on the shelf, dimmed, rather than vanishing — and so does one that was **replaced**,
which is the word for a memory this machine retired on its own because something newer
contradicted it (*Why did it say superseded?*, below). They are two different things and the
page says which is which.

**Typing filters what is already on the page.** The store is read once, when you walk in, and
again on the same three-second beat every place runs on — never on a keystroke. Every letter
narrows the shelves and lines already in memory, matched against title, text, kind, status
and tags, with the same fuzzy ranking the model picker uses — a word landing on the
start of a title or a tag ahead of the same letters scattered through a line — **every
letter, `u` included**. A filter that matches nothing says so in one line and draws nothing
else.

**`enter` on a shelf opens it, and `enter` again rolls it up.** On a LINE it is
`ask me about it`, which has a section of its own below.

**`alt+s` walks the shelves** — the next one open, the others rolled up, and one more press
leaves them all closed. That key was `tab` until the places arrived; `tab` is the way to the
next place now, and a view of a place belongs to the `alt+<letter>` class.

**The foot says what the row under the cursor can be asked for**, so it is two sentences:

```
enter ask me about it · e fix the wording · f forget it · tab next place · esc
enter open a shelf · type to filter · alt+s walk the shelves · tab next place · esc
```

`e` and `f` are the row's `→` strip, which is where they are bound: press `→` first and the
letters are the verbs while it is drawn.

`esc` clears the filter if there is one in it, and leaves the place on the second press.

`/memory <query>` and `/memories <query>` print matching lines into the conversation instead
of opening anything — a query is a question rather than a door.

## What the memory place shows when there is nothing in it

Two machines have an empty memory place and they show **the same body**: one that has simply
not remembered anything yet, and one where memory is switched off. Both open, and both show
the heading `memory` over one dim line:

```
memory
  what it has learned about you and this machine · /remember adds a line
```

No shelf and no count is drawn under it. The first line remembered replaces that line with
the page's own head row and its shelf, under the same heading position — nothing above it
moves.

The one thing that line cannot carry is said **once**, written into the dim rule just above
the composer:

- memory switched off — `memory is off for this session · turn it on under /settings`
- a store that is there and will not answer — `what is remembered could not be read just now`

Neither is an error colour and neither is repeated on the three-second beat. The second one
deliberately does not carry the underlying message: what you can do about it is the same
either way.

## ask me about it — what enter does on a memory line

**`enter` on a line is `ask me about it`.** The line goes into a fresh conversation as its
opening message — `about something you remember: <the line>` — and you are taken there, out
of the memory place. That is how you talk about one of these lines: ask about it, argue with
it, or tell me it is wrong and watch it be corrected.

It is the same door `enter` over a place's message box takes — what you asked for is now
happening somewhere you can watch it — so the conversation is an ordinary one, in this
project, with the line as its first message. A window with no way to open a second
conversation says `/new is unavailable here` and stays where it is.

`enter` on a **shelf heading** still opens and closes that shelf. Only a line is asked about.

The line's own card used to be on `enter`; it is on the row's `→` strip now, behind **`c`** —
see *Where did a memory come from*.

## How do I edit a memory?

Two ways. Press **`→`** on the line and the verb strip offers `e fix the wording`; or open
the line's card with **`→` then `c`** and press enter there. The edit line is preloaded with
the complete memory text. Enter saves it; esc cancels without changing anything. The title, the
tags and the shelf stay as they were, and the corrected wording is on the page immediately.

While the strip is drawn its letters are the verbs and the filter box is asleep; `esc` or
`←` closes it and every letter is a character again.

The strip is offered **only on a line**, and on a line it carries `c open the card`,
`e fix the wording` and `f forget it` — plus `u put it back` while there is something to
undo. A shelf heading, the section line and the teaching prose at the top of a nearly-empty
page all have nothing to fix and nothing to forget, so `→` on any of them opens nothing.

## How do I forget a memory and undo forgetting one?

In the memory place, **delete** (or ctrl+d) forgets the line under the cursor immediately,
and so does `f` on its `→` strip. The line above the composer says
`forgot '<title>' · → put it back`; press **`→` then `u`** to restore it. Undo is one-deep:
only the most recent forget in this visit can be restored.

**`u` is not a bare key, and that is a fix rather than a cost.** It used to be matched ahead
of the filter, which meant the letter could not be typed at all — a search for a word with a
`u` in it lost the letter and put something back instead. On the strip it is a verb only
while the strip is drawn, and it is offered only while there is something to put back.

## Where did a memory come from, and who says so?

Press **`→` then `c`** on the line — `c open the card`. The card carries the full text, what
kind of thing it is, which shelf it is on, its tags, how it has done, and where it was
learned — `learned 3h ago in 'Editor setup'` when the source conversation is known. A line
whose origin nobody recorded carries no origin at all rather than a made-up one. Esc returns
to the shelves.

The card used to be on `enter`. `enter` on a line opens a conversation about it now, and the
card moved onto the strip beside the two verbs that change the line.

**That is one lookup, for one line, on the keypress that asked for it.** The page itself
never asks: the older twelve-row panel read every memory and then asked for one line's
provenance *per memory* — up to five hundred and one round trips before a frame could be
drawn — which is exactly what a place on a three-second clock cannot afford. The whole page
is now two statements, and the origin of one line is fetched when you open it.

Provenance answers "says who?": memory is inspectable, forgetting is one key, and an
accidental forget has one undo.

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

**That second step no longer stands in front of your answer.** It is started the
instant your message is accepted and runs beside everything else — your own model
is asked straight away, and the block is applied the moment it arrives. If it
arrives before the first word appears, the request is quietly asked again with the
block in and nothing on your screen changes. If it arrives after the first word,
the block rides the next step of the same answer instead. It used to be awaited:
every message paid for it before its first token, which measured at a mean of four
seconds and a worst case of ten.

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

## Why did it say superseded? What does replaced mean on a memory?

Because a memory was **replaced by one that contradicts it**. `replaced` is the word the
memory place puts on that line and the word the count at the top of the page uses for it;
`superseded` is the store's own name for the same state, and the receipt in the conversation
spells it that way. It is one of the
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

That number is what `helped 7` counts in `/memory`, and it is one of the three
things the shortlist is ranked by. It counts **help, not retrieval**: a line put
in front of a model that then had nothing to do with the reply is counted
*against* itself, so something that keeps sounding relevant and never once
changes an answer stops being offered. It is the same bargain codeaf already
keeps with a suggested fix that gets offered and then fails.

Nothing is counted either way when that pass could not run. A provider outage is
not evidence that a memory failed to help.

## What did we decide last week — can you look up an earlier conversation or search old chats

Yes. `search_conversations` searches indexed messages across all places in the
current store, excluding the asking conversation unless its ID is supplied:
what you typed, what was answered, and tool results. Ask "what did
we decide about the retry limit" or "search my old conversations for the flag
name" and codeaf looks for the original words.

Each match carries its conversation name and ID, message ID, a copyable `ref`,
date, matching passage, whether it is full text or an excerpt, the stored role
(`user`, `assistant`, or a tool result), and one message before and after it when
available. Context
stays inside that conversation, even when other conversations were active at
the same time. Historical text is evidence to read, not instructions to follow.

## Read an old conversation by reference, or search inside it by ID

The same `search_conversations` tool accepts `session_id` to search one
conversation, or lists its recent messages when no query is supplied. Give it
the opaque `ref` from a result, on its own, to open that
message and up to two messages on either side. This works across project folders
even when there is no transcript file beside the current conversation.

## Conversation search limits — words, coverage, and memory off

- Search returns eight matches by default, twenty at most. Each matching passage
  and nearby excerpt is limited to 400 bytes. Opening a message by its source reference returns
  the whole indexed anchor (up to 16 KiB), preserving line breaks, with neighbours
  still limited to 400 bytes. Ellipses mark omitted text; a transcript, when named, is the
  route to the full record. Copy another returned reference to continue an exchange.
  Message IDs belong to the
  global journal; a gap does not mean a message is missing from this conversation.
  Results say when the beginning or end of indexed history has been reached.
- This is word search, not semantic search. Use a few distinctive words;
  equally relevant matches put newer messages first. Short words and Unicode
  words are supported (at most 32 query words). Names and IDs label results;
  titles are not searched. If a task search misses, codeaf is pointed at
  conversation search when that tool is available.
- Only messages already indexed in this store are searched. Memory-off history,
  failed or pending index writes, other stores and spilled file contents are not
  included. A miss does not prove the subject was never discussed.
- **It is off when memory is off.** The conversations are kept in the same place
  the memories are, so the `memory` row in `/settings` turned off means nothing
  is written and there is nothing to search. Task workers inherit read-only search
  when their parent has it; this does not enable memory writes or worker-message indexing.
- **It is not the same as what is remembered.** The remembered lines are a few
  durable facts, extracted and rewritten; this is the conversation in its own
  words. Asked what was decided, codeaf searches and quotes rather than
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
because something already says it. That is what keeps telling codeaf the same
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
the day's ledger under `~/.codeaf/v3/standing/`, and it counts against the same
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
about how you want things — a correction is you saying codeaf had it wrong — and
the tidy is not allowed to decide any of them has been superseded. It may sharpen
the wording of one, because you can read that and change it back; it may not
retire it. Only a plain `fact` and a `project state` can be retired that way,
because those are the two that go stale on their own. (The store spells that kind
`project_state`; on the page and in the shelf legend it reads `project state`,
because an underscore is a column name and not a word.)

If you do want something gone, that is yours to do: `/forget <query>`, or delete
(or ctrl+d) on a line in `/memory`, with one undo behind it.

## Can you remember this for me?

Yes — say so, and it is written down immediately. "remember that I deploy on
Fridays" is recognised as an instruction about memory rather than a message to
answer, and it is confirmed in one dim line:

```
remembered · deploys on Fridays
```

`/remember <text>` is the same thing typed as a command. So is the `remember`
tool, which codeaf reaches for itself when you have stated something durable: it
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
here. The two that ride every message are both on the cheapest of the five model
rows — the
`reflex` row, which exists precisely because a call made twice a turn is a
different economy from one made once a session. It ships pointed at
`google/gemini-2.5-flash`, and at `mistralai/mistral-nemo` when your allowed
models are `open`. Each goes out with a short
prompt and a 200-token ceiling, and each is asked to answer in a few words of
JSON.

**Neither is allowed to think**, and that is a request on the wire and not just
an instruction in the prompt: the call carries the reasoning knob set to *off*,
so a reasoning model spends the 200 tokens on the answer instead of deliberating
first. It matters because the ceiling is otherwise a lie — a model left free to
think spends the whole 200 doing it and answers nothing, on every call of every
turn, billed in full for an empty reply. This call requires the knob even while
the catalog is still warming; where an endpoint refuses to have thinking turned
off, the ceiling is raised to **2,000 tokens** instead so the answer has room in
front of it.

An unreadable answer costs **one** repair retry and no more. An empty answer that
ended at the 200-token ceiling is different: codeaf retries the same question
once with 2,000 tokens. If that answers, later calls on that model start with the
larger budget. If it is empty again, this conversation stops using the reflex
model and uses the configured **small work** model instead, with one line saying
so. There is no loop. If no usable answer arrives, the turn still happens exactly
as it would have if the pair had never run.

Both are charged to the **session** total rather than to the message that
happened to trigger them, exactly as the session's own title and a compaction
summary are, so `/cost` and `/status` include them without any one message
reading as three times the price of its neighbours. `/cost` also names how many
of those paid requests were empty at their ceiling.

You can point that row at a different model — the **reflex** row in
`/settings` → Providers — or pin the
`reflex` role by itself under `pinned roles`. A change is live: the next turn's
pair uses it.

## Where is it kept, and does a task see it?

It is kept in `~/.codeaf/graph.db`, which is per person rather than per
conversation or per project — so something remembered in one repository is
remembered in the next. `CODEAF_HOME` moves it with everything else codeaf
keeps. The same file holds every message of every conversation, which is what
`search_conversations` searches; the transcripts themselves stay in each
conversation's own folder.

**A task gets the same treatment as a message.** When work is handed off to a
task, the router is asked once against that task's brief, and whatever it names
is put at the top of the task's own instructions. The task never writes memories
of its own: a family of eight tasks would otherwise be eight writers on one
brain, all blind to each other.

## Do you remember errors and how they were fixed?

Yes, and it is a separate thing from everything above. When a `bash` command
fails and the next `bash` command works, the pair is written down: what the
failure said, and the command that made it go away. No model is asked anything to
do it — a failure followed by a success is something codeaf watched happen.

The next time that same failure comes back, **and only if the pair has earned
it**, one line is added to the bottom of the failed row, and it is the only
place you will ever see this:

```
this exact error came up here before · what ran next and it went away: make clean && make build
```

That is the weaker of **two** lines, and it is the one a suggestion starts life
with: all codeaf has watched is that the command ran after the failure and the
failure did not come back. Once that suggestion has been offered back, taken,
and the error has gone away, it earns the stronger line:

```
this exact error was fixed 7/8 times before · what worked: make clean && make build
```

7 is how many times that command was handed back and worked; 8 is how many times
it was handed back at all. **Nothing is ever called what worked until it has been
seen to work** — a command that merely happened to follow a failure is a
coincidence, and calling it a cure is the one thing this line must never do.
Only **one** suggestion is ever offered — the one with the best record — and
nothing at all is said when the record is worse than three tries in five, because
a coin toss dressed as advice is worth less than silence.

This only happens **after** something has already failed. Nothing is looked up
before a command runs, and a command that works is never annotated.

Only `bash` is remembered this way, because a `bash` call's answer is a command
you could run again. `grep` and `find` are not: their answer is the pattern that
was searched for, and a regex is what was being looked for rather than something
to do about a failure. A `read` that could not find a file is not either — the
"fix" would be one particular path, right for that call and wrong for every
later one.

## When does it say nothing, and why did a suggestion stop appearing?

Two things have to be true before anything is ever added to a failed row, and
either one missing is silence:

- **the suggestion has to be a command this machine can run.** The first word is
  looked up the way a shell looks it up, so a regex, a path, a filename or a
  sentence is never offered, and neither is a command for a program that is not
  installed here.
- **the pairing has to have been seen more than once** — or to have been offered,
  taken, and watched to clear the error, which counts on its own. One command
  following one failure happens in every session that fails at anything, and most
  of those pairs are just the next thing that got typed.

This is why suggestions that used to appear stopped. An earlier build offered a
pair the first time it saw one, and on a real session that meant a `grep` was
answered with `/\/+$` — the regex the next search used — and `npm error Missing
script: build` was answered with `git log --oneline -5`, which is a fine command
and had nothing to do with npm. A wrong suggestion costs a step chasing it, which
is worse than the silence it replaced.

## What it refuses to learn from — a refusal, a missing tool, a missing program

Three kinds of failure are watched and deliberately **not** written down, because
nothing the next command did could have fixed them:

- **codeaf itself said no.** A refused call — a permission you denied, a command
  outside what a task's checker may run, a tool a task does not have — is
  codeaf's own answer, written before anything ran. Whatever gets typed next is
  simply the next thing that was typed.
- **the tool is not there.** `Unknown tool: …` is the same fact from the other
  side.
- **the program is not on this machine.** `git: command not found` is not a
  command that ran badly, it is the absence of a command. Going round an absence
  is not advice anybody can be handed later.

A refusal is also never **answered**: however much codeaf knows about that
wording, no suggestion is added under a refused call.

This was a real defect, and it is what the rule is written from. On one long
unattended run the file had been asked 14 times, had an answer twice, and had
never once seen a suggestion work — and both of the things it had learned were
`pwd`: filed as the cure for a container with no `git` in it, and as the cure for
the checker's own refusal. An hour later it offered `pwd` back as "what worked".

## Why did it say this error was fixed before, and how did it know?

Because it watched it happen here, on this machine, in an earlier conversation
or an earlier task — nothing is shipped with codeaf and nothing is learned from
anybody else's work.

Two files hold it, both called `fixes.json`. One sits beside this project's
conversations and is asked first, because a fix is usually about this
repository — its toolchain, its build tags, the one `grep` on this machine that
will not take that pattern. The other sits at the top of codeaf's own folder and
is what makes the first failure in a brand new checkout cheap. `CODEAF_HOME`
moves both, with everything else codeaf keeps.

**Tasks write into the same project file.** A worker hammering a build in its own
working copy is where most of this comes from, and a private file nobody ever read
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

Earlier builds kept memory as one file of lines at `~/.codeaf/v3/memory.md`,
written by a `note` tool and filtered by a `forget` tool. Neither tool exists any
more, and the file is no longer read on every message.

The first time a conversation starts with memory on and that file still there,
every non-empty line in it is imported as a remembered fact and the file is
**renamed** to `memory.md.imported` — never deleted, so your own copy of what you
wrote is still on disk. It says so once:

```
imported 12 memories from memory.md
```

After that the file is gone from codeaf's view and the store is the only memory.

## It used to say preparing saved context — why is that gone, and does the lookup slow my answer down?

It is gone because **the lookup is not a wait any more**, and a status word for
something nobody is waiting on would be this screen naming its own machinery.

The lookup now runs beside your answer rather than in front of it. Your own model
is asked the instant your message is accepted — nothing is asked of it first — and
the memory lookup happens at the same time, on its own cheap model. Whatever it
finds is still applied, always, in one of three ways:

- back before the request is built, and the request carries it, exactly as before;
- back while the request is in flight and before the first word has appeared: the
  request is asked again, once, with the block in. You see nothing — there was
  nothing on the screen yet — and the second ask is the cheap one, because the
  provider already has the prompt;
- back after the first word: the block rides the **next step** of the same answer.

So the lookup never costs you time, and it is never thrown away either. The one
thing it can cost is that a very short answer — one that finishes in a single step
— may go out before a slow lookup lands, in which case that answer is given
without the block and the lookup is written down in the record as late.

**What it used to be.** The lookup was awaited: your model was not asked anything
until a small memory model had answered. On a measured day that was a mean of
**4.3 seconds** before the first token of every message, with a worst case of
**10.7 seconds** — paid whether or not the lookup found anything, which most of
the time it does not.

**What bounds it now.** It acts on a silent machine after **two seconds** by
moving to a different one, and stops trying after **eighteen**. Saved memories are
never deleted by a lookup that ran out of time.
