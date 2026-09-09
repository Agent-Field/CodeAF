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

The model can raise one through its `ask` tool. Not every question can be answered from somewhere else yet. The approval
question, a task proposal and a standing card reach home, another window on this
machine, and a window attached over `--host`; the rest are answered in the
conversation that raised them. What is written down about all of them is the
same either way.

Questions come in two shapes on screen right now, and it is worth knowing which
you are looking at. The **question block** is the new one — it is described
under "How a question looks" below, it never takes the keyboard, and `esc` on it
means later. The older blocks — the approval question before a command runs, a
task proposal, a standing card, a connect offer, an offer to run a saved program
— each still draw themselves the way they always did, with their own keys, and
the approval question still takes the keyboard while it is up: typing under it
does nothing until it is answered. They are being moved onto the block one at a
time.

## The kinds of question

Eight shapes. The shape is what says whether anything but you may answer it and
what a safe answer would be. Not all eight are raised today — nothing yet asks
you to strike an assumption or to unwind something already done.

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

**A task proposal and a reversible recommendation may carry a clock.** The card says how long is left, and
when the time runs out the work STARTS — the card is your chance to redirect it,
not a gate the work waits on. A proposal you hold loses its deadline and then
waits like everything else.

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

## Answering a question from somewhere else

Home lists what a conversation is waiting on and lets you answer it there. So
does another window on the same machine, and a window attached to this machine
over `--host`.

**Three of them travel that way today**: the approval question before a command
runs, a task proposal, and a standing card. A connect offer, a page waiting to be
approved and a question a saved program asked are answered in the conversation
that raised them.

The keys are the ones the question wrote down and nothing else — a window never
offers an answer the conversation would drop. **The first answer wins.** If two
windows answer the same question, the second is simply late, and nothing is said
about it.

## Questions aforge refuses to put to you

Before a question reaches you it has to be a real one, and aforge refuses it
otherwise:

- a question with no sentence saying what is being asked
- a question with no reason for asking now
- a question that does not say what an answer costs
- fewer than two answers, on a kind that needs a list
- more than four answers — eight where you are ticking several — because a fifth
  is almost always two questions that have not been separated
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
  ? allow this? [1] allow once · [2] always · [3] deny · [esc] later
    bash pattern "rm -rf *"
```

**A card** — for a decision with more behind each answer. The question, then why
it is being asked and who is asking, then one row per answer with what taking it
produces, then the keys. A `▸` marks the answer that was recommended; it is not
where your cursor is.

```
  ? wants to start a task: rewrite the packer
    it will run on its own branch · aforge
      1  start it   on a branch of its own
    ▸ 2  not now    nothing runs
    [enter] take the pick · [d] you decide · [esc] later
```

**A ratify line** — one row, with a `✓`, about something already done. Nothing
is waiting on it. Reading past it is accepting it.

```
  ✓ renamed 12 files under src/ · [u] undo · [c] change
```

## Every key on a question

The keys are one set, and a question only ever draws the ones it will actually
take — nothing on the row does nothing, and nothing that does something is off
the row.

| key | what it does |
| --- | --- |
| `1`–`9` | take that answer |
| `enter` | take the recommended answer — only shown when there is one |
| `esc` | later. Nothing is cancelled |
| `o` | open it out into its own page, where there is more to see |
| `c` | change — take an answer, but say what you want different |
| `?` | ask back before answering |
| `d` | you decide |
| `D` | decide questions like this from now on |
| `r` | make it a rule |
| `u` | undo, while what was done is still real |
| `←` `→` | walk the two answers of a confirmation |

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

This is different from the older approval question, where `esc` denies. On the
block, nothing is ever decided by making something go away.

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
puts it back. It is only offered while there is really something to undo; a
ratify line about something that cannot be taken back does not draw the key at
all.

`c` is on the receipt after you answer, spelled `c change` on the line. It shows
what unwinding would cost before it reopens the question.

A decision that cannot be taken back says `cannot change` on its receipt and
offers neither. Those are the ones that were never on a clock and that nobody
but you was ever allowed to answer.

## What is not built yet

There is no page you can open a question out into — `o` puts the question away
for now instead of opening one — and no setting file that answers a whole kind
of question for you from now on, so `D` answers the question in front of you and
records that you asked for it rather than standing for the next one.

The count of open questions in the status line IS built: that is the chip
described above.

The older blocks — the approval question, a task proposal, a standing card, a
connect offer, an offer to run a saved program, and the cards that ask before
stopping work or closing a busy tab — have not moved onto the block yet and keep
their own keys until they do.
