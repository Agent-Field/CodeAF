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

A question drawn as a row or a card **opens out into a page of its own** when
there is more to read than a card holds — see below.

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
- one section per answer, folded shut except the one it would take
- a foot pinned above the box saying what `enter` would send

**A key pressed in the first quarter second is dropped.** A page that appears
under a hand already moving would otherwise turn your next keystroke into an
answer you did not give.

**A letter is a letter the moment there is a sentence in the box.** Every key
below works only while the box is empty; type anything and they all go back to
being text.

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

## Fill in the blanks

Some questions are a sentence with holes in it rather than a list of answers:

    land them in [ ~/notes ] as [ a new file ] and keep the old copy: [ no ▾ ]

`tab` moves to the next hole and `shift+tab` back. Both stop at the ends rather
than wrapping. Typing fills a hole; `←→` walks the choices where a hole has a
short list of them.

Each hole opens on whatever the asker already knew, so you are not retyping it.
Under the sentence, one dim line says what the hole you are in takes — a file or
folder, a number, a time — or why what is in it will not do. Press enter when it
reads right; the holes come back as fields, keyed by their own names.

## Pick several

A question that wants several answers at once draws a tick beside each one.

`space` ticks the one you are on, `a` takes what the asker would tick, and
`enter` sends them. Where the order matters, `shift+↑` and `shift+↓` move a row
past its neighbour and the answer carries the order you put them in.

The foot lists what is ticked, in that order, so what you can see is what would
be sent.

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
    it will tell me, then act

`←` and `→` move it, and the line underneath says what the setting you are on
actually does. On a screen reader it is drawn as `2 of 3 · tell me, then act`
instead of as a picture, and the same two keys move it.

## What an answer carries

Whatever you send goes with everything you did on the way:

- the answer or answers you picked
- anything you typed beside them
- every note you wrote on a part
- anything you asked back and what came of it
- how long the answer lasts, where the question offered a choice of that

## What is not built yet

There is no count of open questions in the status line. That is designed and not
built; if the chat offers you one, it is improvising.
