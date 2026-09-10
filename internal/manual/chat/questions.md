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

**A task proposal and a reversible recommendation may carry a clock.** The answers
row says which answer is about to be taken and when — `start it in 9s` — and when
the time runs out the work STARTS. It is your chance to correct it, not a gate the
work waits on. Any key you press stops that clock, and a proposal you hold loses
its deadline and then waits like everything else, with `waiting` on the end of the
row instead of a countdown.

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
  and one line says `still needs you`
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

## What is not built yet

`o` opens a question out into a page of its own — see the sections above, from
"Open a question up and read it properly" down. `D` writes the setting into
`.aforge/autonomy.json` beside the project AND answers the question in front of
you; a conversation with no project to keep it in says so.

**Answering over `--host` is not built.** A window attached to another machine
draws a question that machine raises, but the answer does not cross the link, so
it has to be given in a window on the machine holding the conversation.

The count of open questions in the status line IS built: that is the chip
described above. So is the sheet, and so is the per-project setting that answers
a whole kind of question while you are away (`/autonomy`, and `D` on a row).

**Every question the block draws is drawn only by the block now.** The last of the
older blocks — the connect offer, with its own answers row and its own keys — is
gone. The one card in this program that still answers to keys of its own is the
intake form `/subharness` opens for a saved program, which is a fullscreen page
with fields to fill in rather than a question above the box.
