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

While the approval question is up, the keys belong to it: typing under it does
nothing until it is answered. That is the one place a question takes the
keyboard, and it is the older shape rather than the one being built.

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

## What is not built yet

There is no page you can open a question out into, no count of open questions in
the status line, and no setting that answers a whole kind of question for you
from now on. Those are designed and not built; if the chat offers you one, it is
improvising.
