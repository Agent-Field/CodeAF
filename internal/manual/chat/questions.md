# Questions aforge asks you

## What a question is here

A question is a choice handed to you with what it is about attached. Every one
of them carries the same things: one sentence saying what is being asked, one
dim line saying why it is being asked now, the answers it will take, what is
waiting on it, and what an answer costs — whether it can be taken back, whether
it costs money or time, or whether it cannot be undone at all.

Questions that your autonomy setting leaves on `ask` wait until they are
answered. A reversible question with `recommend-then-auto` shows the model's
pick, waits for its stated clock, then takes that pick; `decide` takes it at
once. Irreversible questions and clarifications always wait for you.

The model can raise one through its `ask` tool. Questions reach home, another window on this machine, and a window attached over
`--host` as the same question object. Every surface draws the answers that object
offered; it never substitutes positional yes/no keys.

Every question is drawn by one thing now — the **question block**, described
under "How a question looks" below. It never takes the keyboard, the answers are
numbered, and `esc` on it means *later*. The approval question before a command
runs, a task proposal, a standing card, a connect offer, an offer to run a saved
program and a finished harness design are all on it, and so are the two cards you
raise yourself.

A question with more behind it than the block draws **opens out into a page of
its own** — `o` opens it, `esc` closes it, and the sections below say what you
can do in there.

## The squiggle, the tick and the amber mark at the start of a card

Every question opens with **one cell**, and it says whether the card in front of
you is waiting for you at all:

| mark | what it means |
| --- | --- |
| amber `?` | it is waiting on you and nothing moves until you answer |
| dim `≈` | it took something for granted and went on — strike a line to change it |
| dim `✓` | it did the reversible thing already and is telling you |
| dim `⊘` | it stopped needing an answer and was taken back |

**Only the first is amber.** Amber on this screen means waiting on you and
nothing else, so the three cards that are not waiting on anybody do not wear it —
a squiggle or a tick in the attention colour would be the screen asking you for
something it has just said it does not need.

On a terminal with a patched font these are drawn as icons instead of as shapes;
`/settings` → **step icons** → `plain` puts the characters above back.

## The kinds of question

Eight shapes. The shape is what says whether anything but you may answer it and
what a safe answer would be, and it also decides which of the marks above the
card opens with.

- **permission** — may this happen. The approval gate before a command runs, an
  offer to connect one of your accounts, an offer to run a saved program. The
  safe answer is to skip it.
- **choice** — which of these. Several answers that have already been thought
  through, usually with one marked as the pick.
- **judgement** — is this good enough. A written page waiting to be approved.
  Only you ever answer one.
- **clarification** — I could not tell what you meant. Words are the first
  answer to this one, not the last.
- **confirmation** — this is about to happen and it cannot be taken back. Never
  on a clock, and the answer that changes nothing is the one under the cursor.
- **landing** — work that finished and that nobody could check. It is the `your
  call` row on a task.
- **assumption** — what is being taken for granted. Everything on it stands
  unless you strike one.
- **ratify** — something reversible was already done, and this is your chance to
  unwind it. Nothing is waiting on the answer.

## Which questions wait, and which one carries a clock

Most of them wait. A wait that ended is not a no: an approval question, a
standing card and a page waiting to be approved carry no clock at all, and they
stay up until somebody answers them.

**A task proposal and a reversible recommendation may carry a clock.** The answers
row says which answer is about to be taken and when — `start it in 9s` — and when
the time runs out the work STARTS. It is your chance to correct it, not a gate the
work waits on. Any key you press stops that clock, and a proposal you hold loses
its deadline and then waits like everything else, with `waiting` on the end of the
row instead of a countdown.

**A clock says what it is going to do, in that shape's own words.** Where there
is a recommended answer the tail is that answer — `start it in 9s`. Where there
is not, the words depend on the shape: a proposal reads `starts on its own in 9s`
because something begins when it runs out, and an assumptions card reads
`goes on in 9s`, because nothing begins — the asker simply stops waiting for you
to strike a line and carries on with what it said it was assuming.

**Nothing that cannot be taken back ever runs on a clock**, and only you ever answer
one. aforge refuses to raise a question that says otherwise.

## The record — where your answers are saved

Every answer you give is written down, in the conversation's own folder, in a
file called `decisions.jsonl`. One line each: what was asked, what you
picked, anything you said beside your pick, who decided, when, and whether it can
still be changed.

The record is read BEFORE anything is asked. A question you have already answered
about the same thing is refused with `already decided:` and what you decided, so
you are not asked the same thing twice.

Every line says WHO decided, and where that was not you it says so rather than
reading as something you said.

When the autonomy dial takes a recommendation, the record identifies the answer
as `aforge, on your settings`; it never makes that answer look like yours.

**The whole record is on disk; the model is handed the newest few.** What rides at
the top of every request is the newest eight decisions plus one line saying how
many older ones `decisions.jsonl` still holds — a record that grew all day would
be re-sent, and paid for, on every message you send. Nothing is lost: the file
keeps every line, and the `already decided:` refusal above is checked against all
of them however old.

## Why did it not ask me, or why did it go ahead by itself?

The model is instructed to ask last. It first reads `the record`, then states a
safe assumption, acts and offers to unwind reversible work, shows outcomes, and
offers structured choices. A repeated decision may therefore be answered by the
record, and a reversible choice may be taken by your autonomy setting.

The settings are per project and per kind: `ask`,
`recommend-then-auto` with a wait, or `decide`. Destructive work never answers
itself, and clarification never runs on a clock.

## Make it ask me every time

Set that question kind to `ask` in the autonomy controls. This is per project;
other projects keep their own setting. An irreversible question already behaves
this way and cannot be changed to automatic.

## It assumed something wrong

Answer against the model's pick and add why. That explanation becomes a durable
preference when memory is on, is read back with relevant remembered context, and
can be removed through the existing forget control. The decision itself remains
in `decisions.jsonl` as the record of what happened.

## A finished task waiting on your word counts as needing you

Work that finished and that nobody could check sits on `your call`. That is a
question like any other: home says the conversation is waiting on you, the tab
signal lights, and the switcher marks the row — the same as it does for an
approval or a standing card.

It did not always. A task on `your call` used to leave every one of those saying
the conversation was idle, and the only way to find it was to open the
conversation and look.

## The task page shows no accept or not right keys — where the landing question is when I press enter on home

`enter` on a task that is waiting on you (home's needs-you row, or the tasks
page) opens that task's record: what it was called, what it said at the end,
its files and its transcript. **The question is drawn on that page too**, above
the foot, in the block's own shape — the task's head, why it is asking (`nobody
could check it`), and the answers row `[a] accept · [n] not right · [s] tell
it`. `←` and `→` move the pointer between the answers, `enter` takes the one it
is on, and `a`, `n` and `s` answer at once; `s tell it` puts the page down and
opens the task's room. The foot says `←→ choose · enter take it` while a
question is on the page. It is the same question the conversation's block above
your box is holding, so answering it in either place answers it in both.

The page draws only questions belonging to the conversation you are in — a task
from another conversation wearing the same number shows its record and no
question; open that conversation to answer.

## Answer from home or another window

Home lists what a conversation is waiting on and lets you answer it there, and so
does another window on the same machine. **The keys are the ones that question
wrote down and nothing else** — no window ever offers `y`/`n` on top of somebody
else's answers, and a key a question did not offer does nothing when you press it
over its row.

Every lane can be answered this way, not a chosen few: the session leaves the
whole question in the file another window reads, and the answer goes back through
the one door that knows which lane it belongs to.

A window attached over `--host` draws a question the far machine raises, but
cannot yet answer one — answering across the link is not built (see *What is not
built yet*). Answer it in a window on the machine holding the conversation.

## The row on home shows the question but my key does nothing over it

A question the model raised with `ask` is drawn on the row like any other — the
mark, the conversation's name, what was asked, and the count in the band at the
top — and no key pressed over that row takes it. The lanes that wrote their own
card before questions became one object are the ones a row still takes a key for:
a permission, a task proposal, a standing offer.

Press `enter` on the row instead. It brings that conversation here, and the
question is above the box where it was raised, with its own keys on it.

## The other window answered it, or two windows answered at the same time

The first answer is the decision. A window that finds out somebody else answered
writes the receipt with **`another window`** on it rather than `you`, so the line
never claims a key you did not press.

If two windows answer within a second of each other and choose **differently**,
both lines stay on screen and one sentence says which counted:

```
  two windows answered that · the first one is the decision
```

Nothing is merged. A later answer than that is simply late; it is ignored, and
nothing is said about it.

## It asked while I was away

**Ten minutes with nobody touching this keyboard** makes the window away. It is
measured from the last key and not from the window being in front, because a
window can be focused with nobody reading it.

While you are away:

- a question your project's rule may take is taken, and its receipt says
  `aforge, on your settings` decided it
- everything else stays open, and the desktop notification says the conversation
  is `waiting on you`
- the terminal bell rings **once**, and only for a question something is blocked
  on. A question nothing is waiting on never rings, and no question rings twice

A rule never takes a question that cannot be taken back, and never takes a
clarification — the answer to that one is something only you have.

## Stop it deciding things while I am away — /autonomy

`/autonomy` shows what this project does with each kind of question while you are
away:

```
questions while you are away
permission       ask me · change
choice           recommend, auto in 30s · change
clarification    ask me · clarification never runs on a clock
confirmation     ask me · destructive always asks
```

Change one with `/autonomy <kind> ask`, `/autonomy <kind> recommend 30s` or
`/autonomy <kind> decide`. `D` on a question does the same thing for that
question's kind and tells you it did.

Two rows can never be changed: **confirmation always asks**, because it is what
is asked before something destructive, and **clarification never runs on a
clock**. Trying to change either says so rather than failing quietly.

The rules live in the project, not in your profile — the same kind of question can
deserve a different answer in two pieces of work.

## Why is there a countdown on this question, and what is `your rule`?

A question that is going to be taken by a rule says so on its own row, with the
answer that is about to be taken and how long is left:

```
  [1] sqlite · [2] memory · [D] decide these from now on · sqlite in 28s · your rule
```

`your rule` means the clock is running because of something **this project was
told to do**, not because the question came with one. `D` is the door that
changes it, and `/autonomy` shows every row at once. **There are no hidden
rules**: a clock you did not ask for never runs without that word beside it.

## Several questions at once — the sheet, and the same answer for all of them

Quiet questions raised while one step is running do not land on top of each
other. They wait for that step to end — the moment the model speaks again, or the
turn finishes — and arrive together, grouped by the kind of decision each one is:

```
? 4 questions raised together
  asking permission
  ▸ ✓  1  read vendor/modernc.org?          allow once
      ?  2  read vendor/golang.org/x?
      ?  3  write to .github/workflows?
  choosing
      ?  4  which index should this use?
  [enter] open it · [s] send what is answered · [g] same answer for all like this · [esc] later
```

`▸` is where you are, `✓` is a row you have answered with the answer you gave
beside it, `?` is one still waiting.

- `1`–`9` move to that row
- `enter` opens that one on its own, with everything a question normally draws
- `g` gives the row you are on the same answer as every other row of that kind
  **that offers that same answer** — and says how many it reached. Two questions
  whose second answer is `deny` on one and `always` on the other are not the same
  answer, and `g` will not treat them as one
- `s` sends everything you answered and lets each remaining question take its
  own recommended answer. Anything nobody recommended an answer for stays open,
  and one line says `still needs you`. Every answer it sends leaves the same dim
  `decided …` line one answered on its own would
- `esc` puts the whole sheet off; nothing is answered, the count does not drop,
  and the chip's key brings it back

A question something is blocked on never waits for a boundary. It arrives at
once, on its own.

## The question disappeared — it vanished without me answering, withdrawal

A question can stop needing an answer: what it was about went away, the plan
changed, or another answer settled it. It is taken back by whoever asked, and one
dim line stays where it was:

```
  ⊘ allow this? — no longer needed · the turn moved on without it
```

The count in the chip drops, and an open sheet loses that row and re-flows —
**without moving anybody else's answer**, because an answer belongs to its
question and not to the position it was drawn in. A question still gathering
inside a step is dropped from that batch too, so a boundary never delivers a
decision that stopped needing to be made.

It is never called cancelled: nothing failed, and nobody decided anything. The
decision simply stopped needing to be made.

## Questions aforge refuses to put to you

Before a question reaches you it has to be a real one, and aforge refuses it
otherwise:

- a question with no sentence saying what is being asked
- a question with no reason for asking now
- a question that does not say what an answer costs
- fewer than two answers, on a kind that needs a list
- more than four answers — eight where you are ticking several — because a fifth
  is almost always two questions that have not been separated
- an answer with no label on it, named by its number (`every answer needs a
  label a person can read, and answer 2 has none`)
- a pick that names an answer not on the list
- a clock, or any setting that answers in your place, on something that cannot
  be taken back
- more than three questions standing open about ONE piece of work; past that they
  have to be put together into one

The refusal always says what to do instead: choose, or say what is being assumed.

## How a question looks

The block sits directly above the box you type in, and it draws **one question
at a time** with a count of anything behind it (`2 more`). It never covers the
screen and never takes the keyboard.

It has three shapes, and the amount of evidence decides which:

**A line** — one row with the question and its answers on it, the reason dim
underneath. If the question is about something already in the conversation — a
command about to run — that row is drawn above it, the same row you already
read, not a second description of it.

```
    ╰─▶ bash rm -rf build
  ? allow? [1] allow once · [2] always, this command · [3] deny · [esc] later · 7s
    bash pattern "rm -rf *"
```

**A card** — for a decision with more behind each answer. The question, then why
it is being asked and who is asking, then one row per answer with what taking it
produces, then the keys. **The `▸` is your pointer**: `↑` and `↓` walk it from
answer to answer (`←→`, `tab` and `shift+tab` do the same), `enter` takes the
answer it is on, and a digit still takes that answer at once. It starts on the
answer the asker recommended, when there is one, and that answer says
`suggested` in its consequence column wherever the pointer is. On a checklist
the pointer is the row `space` ticks. **Every answer gets a row of its own,
however many there are** (the model may raise up to four, eight on a
checklist), a note the asker wrote under an answer is drawn beneath its label
in the ordinary text ink — the consequence beside the label is the dim aside,
the note is the sentence you weigh — and a long answer wraps onto as many rows
as it needs —
nothing on a card ends in `…` except a note the screen has no room for, which
is held to a row or two so the question itself stays on the screen, with `o`
opening the page that has all of it. The keys
are always digits, `1` upward in the order the answers came, whatever the asker
called them. The model may ask for a line; it gets one only when every answer is
plain and fits — a checklist, blanks, pairs, a dial, or an answer with a
consequence beside it, is a card whatever was asked for. A click on any row of
an answer presses it, and the row under the pointer lights up.

```
  ? wants to start a task: rewrite the packer
    it will run on its own branch · aforge
    ▸ 1  start it   on a branch of its own · suggested
      2  not now    nothing runs
    [enter] take it · [d] you decide · [esc] later · [↑↓] choose
```

**On a line** the pointer is the highlighted answer: `←` and `→` move it along
the row, `enter` takes it, and the key row says `[enter] take it` and
`[←→] choose` while there is room for them (the arrows work either way).

**A ratify line** — one row, with a `✓`, about something already done. Nothing
is waiting on it. Reading past it is accepting it.

```
  ✓ renamed 12 files under src/ · [u] undo · [c] change
```

**An assumptions card** wears `≈` instead of `?` for the same reason: it is not
asking, it is telling you what it took for granted, and everything on it stands
until you strike one.

```
  ≈ going ahead on these unless you strike one
    nobody said which store to use · aforge
      1  the sqlite file is the source of truth
      2  the old rows can be dropped
    [esc] later · [c] change · goes on in 9m 57s
```

## Every key on a question

The keys are one set, and a question only ever draws the ones it will actually
take — nothing on the row does nothing, and nothing that does something is off
the row.

| key | what it does |
| --- | --- |
| `1`–`9` | take that answer |
| `enter` | take the answer the pointer is on — it starts on the recommended one |
| `↑` `↓` `←` `→` | move the pointer (`↑↓` on a card, `←→` on a line; both pairs work on both). On the **page** a question opens into, `↑↓` walk the answers and `←→` fold and open the one you are on |
| `esc` | later. Nothing is cancelled |
| `o` | open it out into its own page, where there is more to see. Inside the page it opens and folds the answer you are on |
| `c` | change — take an answer, but say what you want different. The answers row becomes `change: say what you want different, then enter · it goes with [2] …`; type in the box below, `enter` sends the words with the pointed answer, `esc` gives the box back |
| `?` | ask back before answering. The row becomes `ask back: type your question, then enter · the question stays open`; the reply lands in the conversation and the question is still there to answer |
| `d` | you decide |
| `D` | decide questions like this from now on |
| `r` | make it a rule |
| `u` | undo, while what was done is still real |
| `←` `→` | walk the two answers of a confirmation, or change what is in the hole where the question has one |
| `s` | on a sheet: send what you answered, and let the rest take their own picks |
| `g` | on a sheet: same answer for all like this |

On a **sheet** — several questions that arrived together — `1`–`9` move to a row
instead of answering, because the numbers on screen are the rows; `enter` opens
the row you are on, and `s` and `g` are the two keys about the whole batch.

**Typing is answering.** The box under the block stays live. While there are
words in it every ordinary key belongs to the box, and pressing `enter` sends
what you typed as your answer rather than as a message — on a question the
conversation is waiting on. Only `esc` stays the question's.

**A letter is the question's only over an empty box.** `d`, `r`, `u` and the
rest do nothing while you are mid-sentence, because a letter that acted on a
question while somebody was typing would be unforgivable.

## Pressing esc — later, and nothing is cancelled

`esc` on a question puts it away. It does **not** answer it, refuse it, or end
anything: the question is still open, whatever was waiting on it is still
waiting, and the count in the status line does not drop. What goes away is the
rows, so the box underneath is yours again.

This is what changed about the approval question, where `esc` used to deny, and
about the standing card, where `esc` used to be the outright no. Nothing is ever
decided by making something go away — which is why both of those grew a visible
answer for the refusal (`[3] deny`, `0 no`) on the way here.

To bring it back, press `alt+a`.

## The chip — what is waiting, from anywhere

The status line carries `? 3 questions · alt+a` whenever anything is open,
however you got away from it — put away with `esc`, or raised while you were on
home or in a room.

**`alt+a` brings the newest one back** and takes you to the conversation it
belongs to. The newest rather than the oldest, because if you are pressing it
you are usually asking about the thing that just changed the count.

With nothing open the chip is not drawn at all — never `0 questions`.

## A key pressed too early is dropped

A question that arrives will not take a key that was pressed before it had been
on screen for a quarter of a second. If your hand was already moving, that
keystroke was aimed at whatever was there before — so it is dropped rather than
applied late to the wrong thing.

**And a question never moves the box under your hand.** If one arrives while you
have words half typed, it waits: it is open, it is counted in the status line,
and it takes its rows only once the box is empty or you have stopped typing for
three seconds.

## Why it decided by itself, without asking me

Almost nothing does. A wait that ended is not a no, and nothing on the block
answers in your place if you say nothing.

There is exactly one thing that decides by itself: **a task proposal**, whose
card says which answer it is going to take and when — `start it in 9s`. That
card is your chance to redirect the work, not a gate the work waits on, and it
starts on its own if nobody says otherwise. Nothing else on this surface acts
without you, and nothing that cannot be taken back ever will.

If an answer was not yours, the line left behind names whoever gave it, so you
can always tell by reading it — see "What stays behind after you answer" below.

## Nobody to ask — a question in `--once`, an errand or a task lane

With nobody at a keyboard there is no block to draw, so what the settings decide
is printed rather than shown:

```
asked: which store should the ledger sit on? → 1 (default · nobody to ask)
```

It goes to the error stream beside the `tool:` lines and never into the reply, so
a run whose output you are piping somewhere still says what was taken in your
absence. Where the asker recommended nothing there is nothing to take: the line
reads `your call: <what was asked> (nobody to ask)` and the work stops there
rather than guessing.

## What stays behind after you answer

Answering leaves one dim line where the question was:

```
  decided allow this? → allow once · you · 14:02 · c change
```

What was asked, what you picked, anything you said beside it, who decided, and
when. Where a decision can still be taken back it says `c change`; where it
cannot it says `cannot change` instead.

The name in the middle is who gave the answer: `you`, or `an earlier decision`
where something you already settled covered it, or `done and not objected to`
where you handed it back with `d`. It is the same line that goes into
`decisions.jsonl`, so what you read and what the model reads are one sentence.

The line stays for half a minute and then goes — it is news, and after that it
is history, which lives in the transcript and in the record.

**There is one line however you answered.** Answering from the page a question
opens into folds that page away and leaves the same receipt above the box —
not a second account of the same decision in different words, and never one
saying `another window` about a key you pressed yourself.

## Part of the receipt is missing — a narrow window, and what it gives up first

A receipt too long for the window **gives up a whole clause** rather than running
off the right-hand edge, and the order is fixed:

1. `with: …` — what you typed beside your pick. It goes first, and it is the one
   clause the transcript and `decisions.jsonl` both still carry in full, so
   nothing is lost by dropping it from a line that is news.
2. the time.

**What is never given up**: the question and what you picked, **who decided**,
`cannot change`, and `c change`. Who decided is the one thing on that line you
cannot work out for yourself — `another window` and `aforge, on your settings`
are the whole reason it is written — and the last two are not details about the
decision, they are what is still possible about it. A line that dropped
`cannot change` would read as something you could walk back. If a window is
narrow enough that even the question will not fit beside all of that, the
question is what is cut and they stay.

Widen the window, or open the conversation: the transcript and the record always
have the whole line.

## A question that stops needing you

Sometimes the thing a question was about goes away — the turn moved on, the plan
changed, another answer settled it. When that happens the question is taken back
by whoever asked it and one dim line says so, once:

```
  ⊘ allow this? — no longer needed · the turn moved on without it
```

Then the count in the status line drops. It is never called cancelled: nothing
failed, the decision simply stopped needing to be made.

## Stop asking me about this — rules

Say yes to the same shape of question three times and the row grows one more
key: `[r] make it a rule for this project`, or `for everywhere` where the
question said an answer could reach that far. If you keep being asked about the
same thing — `rm` on a build folder, the same script, the same file — this is
how you stop asking and stop being asked.

Pressing it takes the answer and makes it stand, so the same question is not put
to you again. The scope is written on the key before you press it — **there are
no hidden rules here.** A row that a rule answers says so, so you can always see
which of your decisions is still deciding things.

Nothing is ever made into a rule on your behalf, and a `no` never becomes one: a
standing refusal is something you write in your settings on purpose.

## Undoing what you just approved

Two keys, and which one you get depends on whether the thing has happened yet.

`u` is on a ratify line — something reversible was already done, and pressing it
puts it back. It is only offered while there is really something to undo, and
that is two things at once: the work has to be **reversible**, and whatever did
it has to have written down the answer that puts it back. A ratify line about
something costly or irreversible does not draw the key, and neither does one that
named no way back — pressing `u` there would send an answer nobody described.

`c` is on the receipt after you answer, spelled `c change` on the line. It shows
what unwinding would cost before it reopens the question.

Today `u` is never drawn: nothing yet tells a ratify row that the work behind it
can still be put back, so the row reads `✓ <what was done> · [esc] later ·
[c] change` and `c` is the way back.

A decision that cannot be taken back says `cannot change` on its receipt and
offers neither. Those are the ones that were never on a clock and that nobody
but you was ever allowed to answer.

## Open a question up and read it properly — the page a question opens into

A question that carries more than a card can hold — a body under each answer, a
diagram, a diff, a table of what each one costs — opens into a page of its own,
over the conversation. `o` or `enter` opens it and `esc` closes it again.

Nothing stops while it is open. The turn under it keeps going, the box keeps
taking words, and closing the page puts the conversation back exactly where it
was, scrolled where you left it. **Closing it is not answering it**: `esc` means
later, the question folds away, and whatever was waiting on it is still waiting.

The page has, from the top:

- the question in one sentence, with the amber `?`
- who is asking and why now, dim
- what is waiting on it and what carries on without it
- anything it drew for the whole decision, under one dim heading `what it showed
  you`
- one section per answer, folded shut except the one it would take
- a foot pinned above the box saying what `enter` would send

**A key pressed in the first quarter second is dropped.** A page that appears
under a hand already moving would otherwise turn your next keystroke into an
answer you did not give.

**A letter is a letter the moment there is a sentence in the box.** Every key
below works only while the box is empty; type anything and they all go back to
being text.

## Moving around the page — the arrows, and clicking an answer

`↑` and `↓` walk down the answers. The one you are on wears a band, and the foot
says `[↑↓] choose`.

`→` opens the answer you are on and `←` folds it again. They say what they want
rather than toggling, so holding one down is safe.

`enter` **takes** the answer you are on — that is the answer, sent. The page
opens standing on the one it would take, so `enter` straight away still takes
its recommendation. A digit `1`–`9` answers at once from anywhere, whichever
answer you are standing on.

**Clicking works too, and it takes two clicks to answer.** The first click on an
answer's row moves onto it and opens it; a second click on that same row is
`enter`. One press to read, one to decide — so a click on a page you have not
finished reading cannot decide anything. Clicking a body, a diagram or a note of
your own does nothing: only an answer's own row answers to the mouse, and it is
the only row that lights up under the pointer.

## What each line under an answer means

An open answer reads in three tiers, and every line says which it is:

- the **label** — the digit and the word, bold and amber, the heading of that
  section
- what it **means** — the asker's own paragraph, in ordinary ink
- the asides, dim, each with its own word in front:
  - `then ·` what taking it would leave true
  - `why this one ·` why the asker would take it, on the one it recommends
  - `would switch if` what would change the asker's mind, which is usually
    exactly what you disagree with if you disagree
- anything it drew for that answer — a diagram, a diff, a table — under a dim
  title of its own, a blank row above it

A blank row closes each open answer, so the next answer's label is not just a
different indent.

## Compare the options

`x` lays the answers out against each other, one row per thing they differ on.

**Only the differences are there.** A row every answer reads the same on is not
a comparison, and dropping it is what leaves room for the rows that decide the
question — the page says `only what differs is here` under the table so you know
rows were left out.

Where the asker did not say what to compare on, the rows are worked out from the
`+` and `−` lines under each answer instead: what it gains and what it costs.
Where there is neither, `x` is not offered at all.

Under eighty columns the table stacks: each answer gets its own heading with its
readings underneath, because a column cut to nine characters is a column that
lies. `x` again goes back to the answers.

## Where do I type my answer — the box under the question, change and ask back, why typing did nothing

There is no separate typing place: **the message box under the question is where
the words go**, and the block says what they will mean. With nothing pressed,
what you type is a message to the conversation and the question waits. Press
`c` first and the answers row turns into `change: say what you want different,
then enter · it goes with [2] Adaptive · esc back` — now the box is the
question's: every letter types (even `d`), `enter` sends the sentence together
with the answer the pointer is on, and `esc` turns the row back into the keys
without answering. Press `?` and the row says `ask back: type your question,
then enter · the question stays open`: `enter` sends the question to the asker,
its reply lands in the conversation, and the block is still above the box to
answer afterwards.

If the row still shows the keys — `[enter] take it · [esc] later · …` — the box
is the conversation's, and what you type there goes to the model as a message.

## Comment on one option

`c` writes a note under whichever answer, blank or pair you are on. Type it and
press enter; it appears under that answer, in your own ink, led by `›`.

**A note is not the answer.** It is filed against the part you wrote it on and
goes into the record that way, so "only if the reporting job keeps its own copy"
stays attached to the answer it is about instead of becoming a sentence beside
the whole question. What you type with nothing pointed at a part is the words
beside your pick — the foot shows the difference before you press enter.

`esc` puts the note away without writing it, and the page stays open.

## Ask it something before I decide

`?` puts one question back to whoever asked, with the question still open.

Type what you want to know and press enter. The sentence goes to the model as an
ordinary message, the question stays exactly where it was, and the reply is drawn
in place under the answer you asked about, led by `↳`. Answer the question when
you have read it; the whole exchange is saved with your answer.

**One exchange per answer.** A second `?` on the same answer says `already asked
about this one` rather than doing nothing. A question that turned into a
conversation is a question that should have been a conversation, and the way to
have one is to close this and talk.

## Can I just let it decide — stop asking me this kind of thing

`d` hands one decision back.

**The first press shows you what it would take and why** — `it would take 1
postgres — because it is the only store the reporting job already reads` — and
waits. A second `d` lets it; any other key takes the offer back. Nothing is
handed over sight unseen.

What is written down afterwards says IT decided, not you. That is the whole
point of the field: a record claiming you chose something you handed over is the
one thing a record must never do.

`D` hands over the whole SHAPE of question — every may-this-happen question,
every which-of-these question — from now on, in this project. It takes two
presses too, and for a sharper reason: the first says which shape it would take
over, and only the second writes it down.

**Setting it answers nothing.** The question in front of you stays open and still
wants an answer; what you have said is about the future. The setting lives in
`.aforge/autonomy.json` beside the project, and a conversation with no project to
keep it in says so rather than pretending.

**A what-did-you-mean question can never be handed over.** aforge refuses it:
there is nothing for it to decide, because the whole question is what you meant.

## None of the answers it offered are right — say what the real question is

`n` is the answer that is not on the list. Type what you think should have been
asked and press enter; it goes back to the asker as a reframe rather than as a
pick, and nothing is chosen.

## Fill in the blanks — the holes in a sentence, and what the arrows do on the question above your box

Some questions are a sentence with holes in it rather than a list of answers:

    land them in [ ~/notes ] as [ a new file ] and keep the old copy: [ no ▾ ]

`tab` moves to the next hole and `shift+tab` back. Both stop at the ends rather
than wrapping. Typing fills a hole; `←→` walks the choices where a hole has a
short list of them.

Each hole opens on whatever the asker already knew, so you are not retyping it.
Under the sentence, one dim line says what the hole you are in takes — a file or
folder, a number, a time — or why what is in it will not do. Press enter when it
reads right; the holes come back as fields, keyed by their own names.

**A card can carry a hole too, not only a page.** The one you will meet is a task
proposal whose model shortlist the harness could not settle: it draws

```
     run it on [ anthropic/claude-opus-5 ▾ ]
```

above the proposal's own answers, and `←→` walks the shortlist. **Moving what is
in a hole answers nothing** — the countdown on the proposal keeps running, and
what is in the hole when you answer travels with the answer. On a card the holes
are `←→` only: `tab` is the page's key, and the sentence a card carries is one
line with one hole in it.

## Pick several — a checklist, tick more than one, what space and enter do on the card

A question that wants several answers at once draws a tick beside each one,
**on the card above your box as well as on the page** it opens into.

On the card, a **digit ticks its row** (press it again to untick), `space` ticks
the row the `▸` is on, `tab` moves the `▸` (`[tab] next row`; `shift+tab` back),
and `enter` sends what is ticked — the key row says `[enter] send what is ticked`
once something is, and `enter` over nothing ticked sends nothing. There is no
`take it` on a checklist: the answer the asker would tick says `· suggested`
on its row, and ticking it is yours to do. On the page, `space` ticks the one you
are on, `a` takes what the asker would tick, and `enter` sends them. Where the order matters, `shift+↑` and `shift+↓` move a row
past its neighbour and the answer carries the order you put them in.

The foot lists what is ticked, in that order, so what you can see is what would
be sent.

The asker writes a checklist as an `ask` with `input.kind` set to `checklist`
and the answers themselves in `options` — up to eight of them, each with a key
and a label. Items written under `input.blanks` with nothing in `options` are
refused with `a checklist ticks its answers, so they go in options`, and more
than eight are refused with `a question offers at most 4 answers, or 8 when
several may be ticked at once`.

## This or that, asked over and over — a run of two-way questions

A run of two-way questions is drawn one at a time:

    which matters more here?
    a  finishing tonight        b  keeping every old row
    2 of 4

`a` takes the first, `b` the second, and `=` says it does not matter either way —
which is a real answer and usually the true one. Answering moves to the next by
itself. `enter` sends what has been answered even if some are still blank.

Each pair is recorded in the words of the side that won, never as `a` or `b`.

## A dial

Some settings are a dial rather than a choice:

    how much may it decide on its own here?
    ask me everything · [ tell me, then act ] · just do it
    tell me, then act

`←` and `→` move it. The row of words is the SCALE — every notch this dial has —
and the line under it is the READING, which is where it is standing right now.

**The reading is whatever wrote the dial called that notch, word for word.**
Nothing builds a sentence around it, because not every label is a verb phrase: a
how-many dial reading `once · three times · five times` came out as
`it will five times` when it did.

On a screen reader the scale is drawn as `2 of 3 · tell me, then act` instead of
as a picture, and the same two keys move it. A dial with no words on it at all is
a bar with its number beside it, and has no reading underneath — the number is
already there.

## What an answer carries

Whatever you send goes with everything you did on the way:

- the answer or answers you picked
- anything you typed beside them
- every note you wrote on a part
- anything you asked back and what came of it
- how long the answer lasts, where the question offered a choice of that

## It asked me in plain text instead of a question block — numbered options in the reply, no keys

The model is told that every question it puts to you goes through its `ask` tool and never
as prose: a question typed out as a numbered list has no keys under it, leaves no record in
`decisions.jsonl`, and cannot be answered from home or another window. A model can still
disobey that, and when it does the reply is only words — type your answer as you would any
message. If it loaded the tool and then stopped without calling it, the turn is sent back
once (`it loaded a tool and stopped before using it · asking it to go on`); see *Why did it
say "loaded" before making a picture* on **what I can do**.

## Which questions draw this way

**The approval question does** — the one aforge asks before it runs a tool. It
has all of the above: the digits, `esc` for later, the chip, the receipt, the
settle guard, the narrow card and the phone sheet. `permissions` is its own page
and states what each answer banks.

**So does the standing card** — `wants to keep an eye on:` with `1 yes, set it up`,
`3 just once` where the item can be done once at all, and `0 no`. What is left in
the conversation is the card itself: your own words, the `when ·`, `where ·` and
`costs ·` bands, and the meter where the engine put a deadline on it. The answers
are up above the box with everything else you are being waited on for, and each
one says what it costs beside it. `c` is how you change when or where — it turns
the box into the correction lane, and `enter` sends your words back to be
re-proposed.

**`esc` on a standing card means *later* now, and it used to mean no.** It is
the one key whose meaning this move changed. Nothing is set up either way, so
nothing is lost: the question folds to the chip, the card stays open, and the
count goes on counting it. The outright no is `0 no`, which is drawn on the card
as an answer you can see and click — where `esc` never was.

**So does the connect offer** — `connect your <Name> account?` with `1 connect`
and `2 not now`. A service connected by a KEY has no `1`: a bare yes to one of
those connects nothing, so the question asks for the key in the message box under
it, masked to a bullet a character with the count beside it, and `enter` sends it.
The one answer it keeps is `2 not now`, because a question the turn is waiting on
with no visible no is a question nobody can end.

**So does the harness lane's pair.** An offer to run a saved program is one line —
`run harness "research"?` with `1 run it` and `2 not now`. A finished harness
design is a card — `wrote a program: <name>` with `1 save it`, `2 change it` and
`3 drop it`, each saying what it costs beside it. `2` resolves nothing: it walks
into the design's own room, where a change is typed, and the page stays waiting
until the rewrite lands. The page itself stays down in the conversation, where it
can be scrolled and read; only the asking is above the box. A design waiting on
you is answered with the same three digits from inside its own room, which used
to have a second row and two chords of its own.

**So does the task proposal** — `wants to start a task:` with `[1] start it` and
`[2] no`, and its clock on the end of the row. **So do the two cards you raise
yourself**: `x` on running work (`Stop this task?`) and `ctrl+w` on a busy tab
(`Close this tab?`). Those two are **confirmations**, and a confirmation differs
from every other question here in three ways worth knowing:

- **`esc` does not mean later.** There is nothing to come back to — you raised it
  with your own hand a second ago — so `esc` gives the answer that loses nothing:
  `keep going` on the stop card, `cancel` on the close card.
- **A digit moves the cursor rather than answering.** `1` and `2` walk the cursor
  onto the answer they name and light its row; `enter` is what decides. Nothing is
  decided by one keystroke.
- **They are answerable at once, and they take the whole keyboard.** The settle
  guard does not apply — your hand is already on the key that raised it — and
  while one is up your half-typed sentence stays in the box, unsent, because
  `enter` belongs to the card.

A question that is not simply a line of answers still fits here. The approval
question's widening yes, `[2] always`, has a **second beat** on a shell command:
it replaces the answers row with the shapes the rule could be written as, and
picks one before anything is written.

```
  always? [1] git status*  ·  [2] git *  ·  [3] just this line  ·  [esc] never mind
```

While a beat is up, the digits belong to it — `3` is the third shape and not the
third answer — and `esc` backs out of the beat rather than putting the question
off.
## Questions on another machine, and on the engine behind an ordinary aforge

**A question reaches you wherever the conversation is, and you answer it where
you are standing.** That is true on all three roads and there is nothing to turn
on:

- **an ordinary `aforge` or `aforge chat` in a project.** The conversation is not
  kept in your terminal — it lives in this machine's engine, so the work goes on
  when you close the window — and questions travel that link in both directions.
- **`--host`**, a terminal here attached to a conversation on another machine. The
  question crosses, and so does your answer, whole: the pick, the words beside
  it, your notes on the parts, anything you asked back, the blanks, the dial and
  how long the answer lasts.
- **in this terminal**, with `--no-host`, `--once` or `--debug`.

**A question raised while nothing was attached is waiting when you attach**,
however long that took, and so is one you were looking at when you walked away.
Nothing expires and nothing is lost: the engine says what it is still waiting on
the moment a terminal attaches, so a question asked an hour ago draws the same
block now that it would have drawn then. The count in the status line is the same
count. See *It asked me something while I was away* in **staying on that
machine** for what it says about how long it sat there.

**An engine of a different build is refused at the door, and says so.** Two
programs that might disagree about what a frame means never guess at each other,
so `aforge` tells you the engine is an older or newer aforge rather than starting
a session in which questions would silently never appear.

## Why can I not answer the question on this task page

Pressing a row of work on the **tasks** place opens that task's own page even
when the work belongs to a chat you are not sitting in — it reads that chat over
the same link, and it says so at the top.

If that conversation has stopped and is waiting on somebody, the page says so
under what it has read:

    ? which storage shape should this use? · answer it in that conversation

It is dim and it takes no key. **A page you are only reading cannot answer** —
amber and a key would be this page promising something it does not have. Go to
the chat itself (`esc`, then the row on home) and the question is there with its
answers on it.

Without that line, a page like this drew a running clock over work that had not
moved since somebody was asked something an hour ago.

## What is not built yet

`o` opens a question out into a page of its own — see the sections above, from
"Open a question up and read it properly" down. `D` writes the setting into
`.aforge/autonomy.json` beside the project AND answers the question in front of
you; a conversation with no project to keep it in says so.

**A row on home shows a question the model raised and does not take a key for
it** — see *The row on home shows the question but my key does nothing over it*.

The count of open questions in the status line IS built: that is the chip
described above. So is the sheet, and so is the per-project setting that answers
a whole kind of question while you are away (`/autonomy`, and `D` on a row).

**Every question the block draws is drawn only by the block now.** The last of the
older blocks — the connect offer, with its own answers row and its own keys — is
gone. The one card in this program that still answers to keys of its own is the
intake form `/subharness` opens for a saved program, which is a fullscreen page
with fields to fill in rather than a question above the box.
