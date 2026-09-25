# Questions codeaf asks you

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
its own** — `O` opens it, `esc` closes it, and the sections below say what you
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

## Which questions wait, which one carries a clock, and when it keeps working while you decide

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
one. codeaf refuses to raise a question that says otherwise.

**A question the model raises does not always stop the work.** It says when it
asks whether its turn waits for you. Where it does, the turn stands still until
you answer, and the row says `waiting`. Where it does not — because it has work
in hand that does not depend on what you decide — it carries on, and your answer
reaches it as a message the moment you give it: at the end of whatever step it is
in the middle of, or as a new turn if it has finished and gone quiet. Either way
nothing is lost by taking your time, and the question stays on the block until
you answer it.

**A ratify never stops the work**, and that is the one shape where the model does
not get to choose: something reversible was already done, the row is your chance
to unwind it, and a turn standing still over finished work would make the
cheapest rung on the ladder the most expensive. It goes up, the work carries on,
and an answer — `put it back` — reaches the model as a message whenever you give
it. No window says this conversation is waiting on you because of one.

## The record — where your answers are saved, and why it does not ask the same thing twice

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
as `codeaf, on your settings`; it never makes that answer look like yours.

**The whole record is on disk; the model is handed the newest few.** What rides at
the top of every request is the newest eight decisions plus one line saying how
many older ones `decisions.jsonl` still holds — a record that grew all day would
be re-sent, and paid for, on every message you send. Nothing is lost: the file
keeps every line, and the `already decided:` refusal above is checked against all
of them however old. Answering does not rewrite that copy and so costs nothing
beyond the answer itself.

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
could check it`), and the answers row `a accept · n not right · s tell
it`. `←` and `→` move the pointer between the answers, `enter` takes the one it
is on, and `a`, `n` and `s` answer at once; `s tell it` puts the page down and
opens the task's room. The foot says `←→ choose · enter take it` while a
question is on the page. It is the same question the conversation's block above
your box is holding, so answering it in either place answers it in both.

The page draws only questions belonging to the conversation you are in — a task
from another conversation wearing the same number shows its record and no
question; open that conversation to answer.

## Answer from home or another window, or over --host

Home lists what a conversation is waiting on and lets you answer it there, and so
does another window on the same machine. **The keys are the ones that question
wrote down and nothing else** — no window ever offers `y`/`n` on top of somebody
else's answers, and a key a question did not offer does nothing when you press it
over its row.

Every lane can be answered this way, not a chosen few: the session leaves the
whole question in the file another window reads, and the answer goes back through
the one door that knows which lane it belongs to.

A window attached over `--host` answers the same way a local one does: the key
crosses to the engine that owns the work, and a refusal comes back in that
engine's own words. See *The answer I pressed was refused*.

## The row on home shows the question but my key does nothing over it

A question the model raised with `ask` is drawn on the row like any other — the
mark, the conversation's name, what was asked, and the count in the band at the
top — and no key pressed over that row takes it. The lanes that wrote their own
card before questions became one object are the ones a row still takes a key for:
a permission, a task proposal, a standing offer.

Press `enter` on the row instead. It brings that conversation here, and the
question is above the box where it was raised, with its own keys on it.

**And a question takes keys only where it is drawn.** If a page is over the
conversation — the start page a new chat opens on, a task's page, the rewind
timeline, a place — the block is not on screen, so none of its keys are live:
everything you type belongs to what you are looking at. `ctrl+t` then typing
`hello there` used to grant a waiting tool on the `t` and leave `here` in the
box. Close the page and the question is back with its keys.

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

## The answer I pressed was refused, or the engine did not answer in time

**Your answer lands the instant you give it.** The question closes and its
receipt appears at once, before the engine has said anything back. Waiting for
the round trip first used to cost about ten seconds a keystroke and left the
screen unchanged for all of it. If the engine then refuses, the question comes
back with the reason on it.

An answer the engine turned down is said on screen in the engine's own words —
the work finished, somebody else answered first, the conversation moved.
Nothing is invented here. The question stays open unless the news of a
decision arrives afterwards.

If the connection is still up but this window did not hear back, it says:

```
the engine did not answer in time
```

or, when the machine has a name, `devbox did not answer in time`. That is not
the connection dying. The engine may already have taken the answer; the
receipt still closes as yours when the news arrives. The connection itself
says `the connection to <machine> is gone` only when the link has actually
dropped — see *when the connection drops*.

## It asked while I was away

**Ten minutes with nobody touching this keyboard** makes the window away. It is
measured from the last key and not from the window being in front, because a
window can be focused with nobody reading it.

While you are away:

- a question your project's rule may take is taken, and its receipt says
  `codeaf, on your settings` decided it
- everything else stays open; the desktop notification says the conversation is
  `waiting on you`, and it is **pinned** rather than left to fade, so it is still
  there when you come back
- the first key, click or focus after you return re-delivers what is still
  waiting, so a notice that timed out on the desktop while you were gone is not
  a question you never hear about
- the terminal bell rings **once**, and only for a question something is blocked
  on. A question nothing is waiting on never rings, and no question rings twice

A rule never takes a question that cannot be taken back, and never takes a
clarification — the answer to that one is something only you have.

## Stop it deciding things while I am away, can it decide by itself, will it decide for me, how do I turn that off — the settings row and /autonomy

Short answers first. **Can it decide by itself?** Only where you have said it
may. **Will it decide for me?** Not unless a rule you wrote covers that kind of
question, and never for anything destructive. **Stop asking me this:** write the
rule for that kind — the settings row below, `D` on the question in front of
you, or `/autonomy <kind> decide`. **How do I turn that off:** `/autonomy <kind> ask`
puts it back to asking you every time, and `/autonomy` on its own shows every
row as it stands.

**Open settings** (`ctrl+,` or `/settings`) and look under **Safety** for
`questions while you are away`. There is a row per kind of question, and `enter`
walks the answer round three words:

```
questions while you are away
permission        ask me
choice            recommend then go · 30s
judgement         ask me
clarification     ask me · never runs on a clock
confirmation      ask me · destructive always asks
landing           decide yourself
assumptions       recommend then go · 10m
already done      ask me
```

- **`ask me`** — it waits for you, however long that takes.
- **`recommend then go`** — it shows you what it would do, waits, and then does
  it if nobody answers. The row says how long it waits.
- **`decide yourself`** — it answers questions of that shape without you.

**`/autonomy` is the same rules from the box**, and it is how you name a wait of
your own: `/autonomy <kind> ask`, `/autonomy <kind> recommend 30s`,
`/autonomy <kind> decide`. `D` on a question does the same thing for that
question's kind and tells you it did. There is one store behind all three doors,
so they can never disagree.

Two rows can never be changed: **confirmation always asks**, because it is what
is asked before something destructive, and **clarification never runs on a
clock**. Trying to change either says so rather than failing quietly.

The rules live in the project, not in your profile — the same kind of question can
deserve a different answer in two pieces of work.

## Why is there a countdown on this question, why did it go with the recommended answer, and what is `your rule`?

A question that is going to be taken by a rule says so on its own row, with the
answer that is about to be taken and how long is left:

```
  1 sqlite · 2 memory · D decide these from now on · sqlite in 28s · your rule
```

`your rule` means the clock is running because of something **this project was
told to do**, not because the question came with one. `D` is the door that
changes it, and `/autonomy` shows every row at once. **There are no hidden
rules**: a clock you did not ask for never runs without that word beside it.

**A clock is something you turn on.** No question the model raises carries one
until you set a rule for that kind — `/autonomy choice recommend`, `D` on a row,
or the settings. Where you name no length it is **ten minutes**, which is the
same stretch of silence this program reads everywhere else as nobody being at the
keyboard; `/autonomy choice recommend 30s` names a different one. The one shape
that carries a clock without being asked to is an assumptions card, and nothing
is decided at the end of that one: the asker stops waiting for you to strike a
line and goes on with what it said it was assuming.

**Any key on the question stops the clock, for good.** The question says so
itself while the clock runs, on a dim line under it: `any key stops the clock ·
you can still change the answer afterwards`. Press one and the tail changes to
`paused`, that line goes, the work goes on waiting for you, and nothing decides
it but you. There is no way back to a running clock — a countdown that started
again would fire exactly when you had looked away mid-decision.

**When a clock does run out, it goes with the recommended answer and says so.**
The decision is written down as this program's own rather than yours — the
receipt reads `codeaf, on your settings` — and what the model is told says in
so many words that the answer is provisional and you may still change it. That is
the next section.

## Several questions at once — tabs, go back to the previous question, answer them all at once

When one step of the work raises more than one question — the model asks two or
three things in the same breath, or a batch of calls each needs your ok — they
are **one panel with a tab each**, not a pile. The questions that belong
together are the ones the same step raised; a question from another step waits
behind the panel as `N more`.

```
╭─ ● storage   ○ naming   ○ tests   ✓ review ────────────── 1 of 3 ─╮
│                                                                   │
│ ? Which storage for the session index?                            │
│                                                                   │
│ ▸ 1  SQLite       one file beside the conversation  ◆ recommended │
│   2  JSONL        append-only, no new dependency                  │
│   3  something else…                                              │
│                                                                   │
╰─ ←→ question · ↑↓ choose · enter take it · esc later ─────────────╯
```

`●` is the question on screen, `○` one still waiting, `✓` one you have
answered. Everything a single question does works on its tab.

- **`←` and `→` go to the previous or next question.** They stop at the ends.
- **`enter` or a digit holds that answer** — nothing is sent yet — and moves to
  the next question still waiting. To change one, go back to its tab and pick
  again.
- **The last tab is the review**, and it sends everything at once:

```
╭─ ✓ storage   ✓ naming   ○ tests   ● review ────────────── review ─╮
│                                                                   │
│ ✓ storage       SQLite                                            │
│ ✓ naming        session-index.db                                  │
│ ○ tests         not answered — ← to go back                       │
│                                                                   │
│ ▸ send the 2 answered · 1 stays open                              │
│                                                                   │
╰─ enter send · ← back · esc later ─────────────────────────────────╯
```

`enter` on the review sends every held answer in one go, in tab order — `send
all 3` when everything is answered. **A question you did not answer stays open**;
nothing is picked for you, and with nothing answered the review offers no send.

**`esc` puts the whole set off**: it folds to one rule, `? 3 questions · storage,
naming, tests`, what you had answered stays held, and `space` or the chip brings
every tab back. **Only a key on the panel holds an answer** — answer one from
home, another window or its own page (`O`) and it goes at once, and its tab
disappears.

**What never joins a set:** anything irreversible, anything asking you to
confirm, and a question whose `←` `→` move something (a dial, a choice inside a
sentence). There is no "same answer for all of these" key on tabs; for
permissions there is `allow all`, on permissions.md's grouped frame. Under sixty
columns the questions come one at a time.

## The question disappeared — it vanished without me answering, withdrawal

A question can stop needing an answer: what it was about went away, the plan
changed, or another answer settled it. It is taken back by whoever asked, and one
dim line stays where it was:

```
  ⊘ allow this? — no longer needed · the turn moved on without it
```

The count in the chip drops, and an open set of tabs loses that tab —
**without moving anybody else's answer**, because an answer belongs to its
question and not to the position it was drawn in.

It is never called cancelled: nothing failed, and nobody decided anything. The
decision simply stopped needing to be made.

**A question goes when the turn that raised it ends.** Most of them go the moment
the work stops waiting on them, but two kinds can still be on screen after the
reply is written — a ratify, which never stopped anything, and one you asked
something about and did not come back to. Both belong to the turn that raised
them, so both are taken back when it finishes, with `the turn moved on without
it`. A question something else is still waiting on — a running task, or one whose
asker said it would read the answer whenever it came — stays.

## Questions codeaf refuses to put to you

Before a question reaches you it has to be a real one, and codeaf refuses it
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

## How a question looks — show me the options side by side

The block sits directly above the box you type in, and it draws **one question
at a time** with a count of anything behind it (`2 more`). It never covers the
screen and never takes the keyboard.

**A question with anything to weigh hangs in a frame**: a rounded panel with a
dim edge, its own sentence written into the top edge with who is asking beside
it, and the keys that answer it written into the bottom edge.

```
╭─ ? Which storage for the session index? ──────────────── model asks ─╮
│ The index needs somewhere to live between launches.                  │
│                                                                      │
│ ▸ 1  SQLite        one file beside the conversation   ◆ recommended  │
│      it survives a crash mid-write · fairly sure                     │
│   2  JSONL         append-only, no new dependency                    │
│   3  BoltDB        fastest reads · adds a dependency                 │
│   4  something else…                                                 │
│                                                                      │
╰─ esc later · o other · ? clarify ──────────────────────────────╯
```

**The `▸` is your pointer.** `↑` and `↓` walk it from answer to answer (`←→`,
`tab` and `shift+tab` do the same), `enter` takes the answer it is on, and a
digit takes that answer at once. It starts on the answer the model recommended
where there is one — and that answer carries **`◆ recommended`** at the right
edge of its row, in every view, so the recommendation and the pointer are never
the same mark saying two things. Where nobody but you may answer — every permission
that is not plainly reversible, and anything irreversible — nobody is allowed to
recommend anything, so the answer that loses nothing says **`safe answer`**
instead. That mark is on the row whenever the question has no recommendation,
whatever the pointer is doing: it tells you where your way out is, not where you
are standing.

**Where the pointer opens on a permission depends on how much the call can cost
you**, and that is the gate's judgement, not this block's: on an ordinary call it
opens on `allow once`, and on a grave one — a call codeaf always stops you for —
it opens on `deny`. permissions.md, *What happens if you just press `enter`?*,
says it in full.

**Under the pointer, and only there**, the panel draws what that answer means:
the note the asker wrote under it, and where it is the recommendation, why it
would take it, how sure it is, and what would change its mind. Move the pointer
and the lines move with it. `O` opens the page that has all of it.

**Where the answers brought things to look at** — a diagram, a diff, a table, two
layouts — the panel splits down the middle and shows them side by side: the
answers on the left, and on the right the word of the answer you are standing on,
what it means, its dim `then ·` / `why this one ·` / `would switch if` /
`confidence ·` lines, and everything it drew. Walk the pointer and the right-hand
side changes with it.

```
╭─ ? which store should the ledger sit on? ─────────────── codeaf asks · waiting ─╮
│ a schema change is next and it is cheaper before there are rows                 │
│                                           │                                     │
│  ▸ 1  postgres              ◆ recommended │ postgres                            │
│    2  sqlite beside the project           │ Rows already carry a foreign key    │
│    3  a file per day                      │ into it.                            │
│    4  something else…                     │ then · one connection for both      │
│                                           │ confidence · fairly sure            │
╰─ esc later · o other · ? clarify ─────────────────────────────────────────╯
```

**Narrower than about a hundred columns there is no room for two**, so the same
lines unfold under the answer's own row instead — and the row gives its `then`
line up to them rather than saying it twice and cutting it the first time.

Either way **the panel never takes more than half the screen**; the conversation
keeps the rest. Evidence longer than that is cut on a dim line that says what is
left and the way to it: `… 4 more lines · O open full`.

**The last answer is `something else…`.** Walk the pointer onto it and the row
becomes a box you type your own answer into: `enter` sends what you wrote as the
answer, `↑` goes back to the list, and `esc` puts the question off. The `o other`
shortcut opens a separate field for an updated request; it does not select an
option or attach words to the highlighted answer.

**It is a real box**, not a bare line: the caret keys every other box answers
work in it too — `⌘←`/`⌘→` to the ends of the line, `⌥⌫`/`ctrl+⌫` to kill a word,
`ctrl+u` to kill to the start of the line (keys.md, *Word jump and line jump work
in every box*).

**A question with nothing to weigh is one row**, because a frame around one
sentence is a box drawn around nothing:

```
  ? publish the draft?  ▸1 publish it   2 hold it   3 ask Sam
    nobody has read it
    ←→ choose · enter take it · esc later · o other · ? clarify
```

**A permission is always the panel**, because allowing a call means reading the
call: the command stands on the panel's first row with what it touches beside
it.

```
╭─ ? needs your ok to run bash ────────────────────── bash · 10s ─╮
│ rm -rf build · bash pattern "rm -rf *"                          │
│                                                                 │
│   1  allow once                                                 │
│   2  always, this tool (session)                                │
│ ▸ 3  deny                                          safe answer  │
│                                                                 │
╰─ esc later · o other · ? clarify ─────────────────────────╯
```

**Under sixty columns it is a bottom sheet** — every answer a full-width band a
thumb can land on, with the same pointer, the same `◆ recommended`, and the keys
on a row of their own.

**A ratify line** — one row, with a `✓`, about something already done. Nothing
is waiting on it: the turn carried on without stopping for it, it is never
counted on the chip, and home does not say you are needed. Reading past it is
accepting it; an answer to one reaches the model as a message. It belongs to the
turn that did the thing, so one you never looked at goes when that turn ends.

```
  ✓ renamed 12 files under src/  esc later · c change
```

**An assumptions card** wears `≈` instead of `?` for the same reason: it is not
asking, it is telling you what it took for granted, and everything on it stands
until you strike one.

A long answer **wraps onto as many rows as it needs** and every one of those rows
presses that answer; nothing is ever cut at the edge of the panel except a note
the screen has no room for. A click on any row of an answer presses it, and the
row under the mouse lights up.

## The keys on a decision box — esc, o other, ? clarify

A framed decision ends with `esc later · o other · ? clarify` on its lower
boundary. Only actions the question supports appear. There is no second hint
row, ordinary message box or footer underneath it. The seam above keeps its
model and telemetry but omits the duplicate question and `waiting · your call`
text while this box is open. Folding it restores the conversation's controls.

Arrow keys and Tab still move through the options, wrapping from either end to
the other. Scrolling over the box does the same; scrolling over the conversation
still scrolls the conversation. Enter takes the selected answer and the numbered
answer keys still work. These navigation keys need no separate hint.

`o other` and `? clarify` work without first moving through the list. The brief
settle guard still protects a newly appeared question from a keystroke already
in flight. A draft already being typed keeps its keys.

Full question pages and sets of questions keep their own navigation controls.

## Other replaces the request; clarify keeps the decision open

Press `o other`, write what you want instead, and press Enter. The pending
decision closes without granting approval. The interrupted work stays in the
transcript, followed by the updated request and its response. `Esc` while writing
returns to the decision without sending anything.

Press `? clarify`, write your question, and press Enter. The reply appears in the
conversation with the pending decision as context. You can still answer the
original decision while the clarification runs. The clarification uses the same
approvals mode. If it needs your input, its question takes priority; answer it
before returning to the original dialog. Clarification never chooses an option
or approves the original action. Its tools and reply appear above the dialog.

## Every key on a question

The keys are one set, and a question only ever draws the ones it will actually
take — every displayed shortcut works, while navigation and additional commands
remain available without being listed on a decision box.

| key | what it does |
| --- | --- |
| `1`–`9` | take that answer — these work straight away, except on the page a question opens into, where a digit walks the pointer to that answer and `enter` takes it |
| `enter` | take the answer the pointer is on — it starts on the recommended one where there is a recommendation. On a permission it follows the stakes: **`allow once`** on an ordinary call, **`deny`** on a grave one (permissions.md, *What happens if you just press `enter`?*) |
| `↑` `↓` `←` `→` | move the pointer (`↑↓` on the panel, `←→` on a one-row question; both pairs work on both). On the **page** a question opens into, `↑↓` walk the answers, `→` hands them to the evidence beside them and `←` takes them back |
| `esc` | later. Nothing is cancelled — the question folds to one titled rule where it stood |
| `space` | open a question that has been folded to its rule, while the box is empty |
| `O` | open full: the page with everything the asker attached. It is not a key on the page itself — the page IS what it opens |
| `o` | other — write an updated request, then press Enter. The old decision is withdrawn without approval, its turn stops, and the revised request starts in the same conversation |
| `?` | clarify — ask a question in context while the original decision stays open and answerable. A question needed by the clarification takes priority until answered |
| `c` | on the full question page, attach a note to the focused option; on a completed receipt, change the recorded answer |
| `d` | you decide |
| `D` | decide questions like this from now on |
| `r` | make it a rule |
| `u` | undo, while what was done is still real |
| `←` `→` | walk the two answers of a confirmation, or change what is in the hole where the question has one. On **tabs** — several questions from one step — they go to the previous or next question |
| `enter` | on the **review** tab: send every answer you gave, at once |

On **tabs**, `enter` and the digits answer the question on screen and hold the
answer until the review sends them all; `esc` puts the whole set off.

**Typing follows the field you opened.** In `other`, Enter sends an updated
request. In `clarify`, it sends a question without answering the decision. In an
ordinary answer field, Enter sends your answer. While you type, letter shortcuts
belong to the field; Esc leaves it without sending.

**Other letter shortcuts require aiming at the question.** `o other` and
`? clarify` work immediately after the initial settle guard. `d`, `O`,
`r`, `u`, `x`, `D` are each the first letter of a word people type into the
box — the `d` of "do the schema first" used to hand the call back to the asker
and leave "o the schema first" behind — so the question does not take one until
you have looked at it: press `↑`, `↓`, `tab` or `enter`, or click an answer, and
the letters are the question's from then on. Any of those is enough, and the
question gives the keyboard back when it goes, so the next one starts the same
way.

**The numbered answers are not on that road.** `1`–`9`, and the letters printed
on an answer itself (`a`, `n`, `s` on a landing), take that answer the moment the
row is on screen. A key drawn in front of you as `1 allow once` is pressable,
always — the key, one space, the word, with no brackets round it.

## Pressing esc — later, and nothing is cancelled

`esc` on a question puts it away. It does **not** answer it, refuse it, or end
anything: the question is still open, whatever was waiting on it is still
waiting, and the count in the status line does not drop. What goes away is the
rows, so the box underneath is yours again.

**It folds in place, to one titled rule** where the panel stood — it never jumps
to the status line and leaves a gap:

```
── ? Which storage for the session index? · 3 answers · ◆ SQLite ──── space open ──
```

`space` opens it again (while the box is empty), and `alt+y` opens it from
anywhere.

This is what changed about the approval question, where `esc` used to deny, and
about the standing card, where `esc` used to be the outright no. Nothing is ever
decided by making something go away — which is why both of those grew a visible
answer for the refusal (`3 deny`, `0 no`) on the way here.

To bring it back, press `alt+y`.

## The chip — what is waiting, from anywhere

The status line carries the question **in its own words** whenever anything is
open, however you got away from it — put away with `esc`, or raised while you
were on home or in a room:

```
  ? Which storage for the session index? · alt+y
```

With more than one waiting it counts them instead — `? 3 questions · alt+y`. A
ratify line is never counted: nothing is waiting on it.

The chip never takes the keys off the page you are standing on. A task that
raised a question while you were on the sessions place says so at the right of the
foot, and the row's own `enter open its room` stays where it was.

**`alt+y` brings the newest one back** and takes you to the conversation it
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
  ✓ allow this? → allow once · you · 14:02 · c change · ◐ working
```

What was asked, what you picked, anything you said beside it, who decided, and
when. A decision you changed says what it replaced — `changed from Hello` —
so the two lines about one question never read as two different questions.
Where a decision can still be taken back it says `c change`, and the key works
from there: it asks you the same question again and your new answer replaces the
old one. A permission you answered `always` says `u undo` beside it as well —
one key that hands the standing yes back without asking you anything. Where a
decision cannot be taken back it says `cannot change` instead, and nothing on it
is a key.

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
`cannot change`, `u undo` and `c change`. Who decided is the one thing on that line you
cannot work out for yourself — `another window` and `codeaf, on your settings`
are the whole reason it is written — and the last two are not details about the
decision, they are what is still possible about it. A line that dropped
`cannot change` would read as something you could walk back. If a window is
narrow enough that even the question will not fit beside all of that, the
question is what is cut and they stay.

Widen the window, or open the conversation: the transcript and the record always
have the whole line.

## You answered it, then switched tab and came back — is it asked again?

**No.** An answer is the conversation's, not the screen's. Leave a tab while a
reply is still running, come back to it, and what you get is the conversation as
it stands: the work that has happened, and only the questions still waiting on
you. A decision you already made is not put to you a second time, and neither is
one somebody answered in another window or one the clock took.

What you will see of the answered one is nothing at all, once the receipt's half
minute has passed — that line is news, and coming back later is later. The
decision itself is in the transcript and in `decisions.jsonl` for good.

**A question you have NOT answered is still there when you come back**, wherever
you went — another tab, home, the sessions place. That is the point of leaving it:
nothing about the work moves while it waits, and the chip in the status line
counts it from every page.

This was wrong once and it was worth its own line here: a task-start question
approved mid-reply came back on every return to the tab, and because a question
takes the keyboard where it is drawn, the conversation underneath it could not be
scrolled while it stood. Both were the same defect, and both are gone.

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
key: `r make it a rule for this project`, or `for everywhere` where the
question said an answer could reach that far. If you keep being asked about the
same thing — `rm` on a build folder, the same script, the same file — this is
how you stop asking and stop being asked.

Pressing it takes the answer and makes it stand, so the same question is not put
to you again. The scope is written on the key before you press it — **there are
no hidden rules here.** A row that a rule answers says so, so you can always see
which of your decisions is still deciding things.

Nothing is ever made into a rule on your behalf, and a `no` never becomes one: a
standing refusal is something you write in your settings on purpose.

## Undo what you just approved, or change an answer you already gave

**Yes — you can change an answer you already gave**, and you can undo a
permission you just approved. Two keys, and which one you get depends on whether
the thing has happened yet.

`u undo` is on the receipt of a permission you answered **always**, and it hands
that standing yes straight back: this conversation stops remembering it, the
saved line in your settings goes with it, and an account capability the yes left
switched on goes back to asking. It asks you nothing, because there is nothing
left to decide — the next time that tool runs you get the question again. The
line it leaves says `taken back · this tool asks again`. A shell `always` is the
one that cannot be finished from here: the rule it saved is about a command
shape, and the receipt does not know which line it wrote, so the row says `taken
back for this conversation · the saved command rule is in /permissions`.

`u` is also on a ratify line — something reversible was already done, and
pressing it puts it back. There it is only offered while there is really
something to undo, and that is two things at once: the work has to be
**reversible**, and whatever did it has to have written down the answer that puts
it back. A ratify line about something costly or irreversible does not draw the
key, and neither does one that named no way back.

`c` is on the receipt after you answer, spelled `c change`. It asks you the same
question again, with no clock on it; your new answer replaces the old one in the
record, and the model is told what changed — `was sqlite, now jsonl` — so that
anything it did on the old answer is looked at again. It is the receipt's key
only while the receipt is on screen and your box is empty. A permission changed
this way stops applying: the next call of that tool asks again.

On a ratify line `u` is still never drawn: nothing yet tells that row the work
behind it can be put back, so it reads `✓ <what was done> · esc later ·
c change` and `c` is the way back there.

**Nothing irreversible is changed this way.** A decision that cannot be taken
back says `cannot change` on its receipt and offers neither key; answering it
again is refused in words, because what it allowed has already happened. Those
are the ones that were never on a clock and that nobody but you was ever allowed
to answer.

## Open a question up and read it properly — open it bigger, the page a question opens into

A question that carries more than a card can hold — a body under each answer, a
diagram, a diff, a table of what each one costs — opens into a page of its own,
over the conversation. `O` or `enter` opens it and `esc` closes it again.

Nothing stops while it is open. The turn under it keeps going, the box keeps
taking words, and closing the page puts the conversation back exactly where it
was, scrolled where you left it. **Closing it is not answering it**: `esc` means
later, the question folds away, and whatever was waiting on it is still waiting.

**Wide enough, the page is two panes with a rule across the top of them.** The
answers stand on the left; the answer you are standing on has its evidence on the
right, and the right-hand side follows the pointer. Narrower than about a hundred
columns the page is one column and that answer unfolds under its own row instead
— the same lines, stacked rather than beside. (The task column counts against the
width: `ctrl+g` stows it and gives the page its cells back.)

The page has, from the top:

- the question in one sentence, with the amber `?`
- who is asking and what is waiting on it, dim — `codeaf asks · the turn waits on
  it` — with the reason under it
- the answers, one row each, `something else…` last, with `◆ recommended` on the
  one the asker would take
- anything it drew for the whole decision, under one dim heading `what it showed
  you`
- the answer you are standing on, in full — beside the list, or under its row
- a foot pinned above the box: what `enter` would send, then the keys in two
  tiers, the ones that answer on the left and the quieter ones on the right

There are no folding sections on the page and no `‹ back` row: one answer is open
at a time and it is the one the pointer is on.

**A key pressed in the first quarter second is dropped.** A page that appears
under a hand already moving would otherwise turn your next keystroke into an
answer you did not give.

**A letter is a letter the moment there is a sentence in the box.** Every key
below works only while the box is empty; type anything and they all go back to
being text.

## Moving around the page — the arrows, and clicking an answer

`↑` and `↓` walk down the answers. The one you are on wears a band, the foot says
`↑↓ choose`, and **the answer you are on stays on screen** — the list scrolls to
it rather than leaving you standing on a row that has gone past the edge.

`→ detail` hands the arrows to the evidence: `↑↓ scroll` moves it, and `← back to
the answers` gives them back to the list. The foot says which of the two the
arrows belong to.

`enter` **takes** the answer you are on — that is the answer, sent. The page opens
standing on the one it would take, so `enter` straight away still takes its
recommendation.

**A digit moves the pointer here; it does not answer.** `1`–`9` walks to that
answer and brings its evidence up, and `enter` takes it. On the block above the
box a digit still answers at once — the page is what you opened in order to read
before deciding, so nothing on it decides on one keystroke. (On a checklist a
digit ticks that answer and ticks it off again.)

**Clicking works too, and it takes two clicks to answer.** The first click on an
answer's row moves onto it and brings up its evidence; a second click on that
same row is `enter`. One press to read, one to decide — so a click on a page you
have not finished reading cannot decide anything. Clicking the evidence beside
the list, a diagram, or a note of your own does nothing: only an answer's own row
answers to the mouse, and it is the only row that lights up under the pointer.

The wheel scrolls whichever side it is over: the answers on the left, the
evidence on the right.

## What each line under an answer means — what does each option look like

The answer you are standing on is drawn in full — beside the list where there is
room for two panes, under its own row where there is not — and it reads in three
tiers, every line saying which it is:

- the **label** — the answer's own word, bold, at the head of the pane. Under its
  own row there is no second label: the row above it is the label
- what it **means** — the asker's own paragraph, in ordinary ink
- the asides, dim, each with its own word in front:
  - `then ·` what taking it would leave true
  - `why this one ·` why the asker would take it, on the one it recommends
  - `would switch if` what would change the asker's mind, which is usually
    exactly what you disagree with if you disagree
  - `confidence ·` how sure the asker is — `sure`, `fairly sure`, `a guess`
- anything it drew for that answer — a diagram, a diff, a table, two layouts side
  by side — a blank row above each

It is the same account of an answer everywhere it is drawn: beside the list, under
its row, and in the panel above your box.

The answers you are not on keep their row and nothing else. What one of them would
leave true is said once, in its own evidence, when you walk to it.

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

## Where do I type my answer — other and clarify, why typing did nothing

`o other` opens a text field inside the decision box, with `other: write an
updated request, then enter`. Enter withdraws the pending decision without
approving it, stops the old turn, and submits your updated request. It never
selects the highlighted option.

`? clarify` opens a field in the same box, with `clarify: type your question,
then enter · the question stays open`. The clarification runs in the conversation's
context while the original decision remains answerable. If the clarification
needs permission or another answer, its dialog takes priority until answered.
The original decision then returns. Neither action silently grants permission.

Every letter types while the field is active. Enter sends it; Esc returns to the
choices without sending. Both shortcuts work before you navigate the options.
Nothing is drawn below the box's lower boundary.

## I typed instead of pressing a key — answering a question in my own words, talking past it

Send a message while a question is open and **the question comes down and your
sentence goes to the model at once**. The row it leaves behind reads

```
  ⊘ Which painting genres do you like? — no longer needed · you said something else instead
```

and your own line is marked `took this instead of the question`.

The model is told, in as many words, that nobody answered it and that your
sentence is the next thing it will read — so `portrait` typed under a list of
genres is read as your answer to it, and nothing is filed in the record as a
choice you pressed. If what you said leaves the decision open, it asks again.

**This is why nothing gets stuck.** A question waits for you and the work waits
for the question, so a sentence that only queued behind the question would be
waiting for itself: you would type, and the screen would sit there. Your words
end the wait instead.

If you meant to say something *beside* the question and keep it open, `?` is the
key for that. On a question the model itself asked you get an answer back while
the question stays open; on a permission, a task proposal or a standing card the
ask-back reaches the model only once you have answered, and the section on `?`
below says why.

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

Type what you want to know and press enter. The question stays exactly where it
was; the reply is drawn
in place under the answer you asked about, led by `↳`, and the whole exchange is
saved with your answer. Nothing is decided by asking and no line is written to
the record. Your answer, when you give it, reaches the model as the result of the
call it asked from, or as a message where that turn has already moved on. While
a question is still open nothing asks it again: a second copy of it is refused
with `already asked and still open:`.

**On a question the model itself asked, the reply comes back while you are still
deciding — even when the work is parked on it.** The sentence goes back as the
result of the call the model is waiting on, carrying a note that the question is
still on your screen and not to ask it again, so the model answers you and the
answers stay exactly where they were. Nothing is decided by asking.

**A limit worth knowing, and it is now only about the OTHER questions.** A
permission, a task proposal and a standing card are not asked by a call the model
is parked on, so a sentence asked back on one of those goes in as an ordinary
message and is queued behind the question — which is waiting for you. No reply
comes until you answer. There is no error and nothing is lost: answer the
question and the ask-back reaches the model with it.

**One exchange per answer.** A second `?` on the same answer says `already asked
about this one` rather than doing nothing. A question that turned into a
conversation is a question that should have been a conversation, and the way to
have one is to close this and talk.

## Can I just let it decide — stop asking me this kind of thing

**There is a row for it where settings are browsed.** Open settings (`ctrl+,`,
or `/settings`), go to **Safety**, and the section *questions while you are
away* has one row per kind of question — permission, choice, judgement, landing,
assumptions — each saying `ask me`, `recommend then go · 30s` or `decide
yourself`, and `enter` walks it round the three. Searching settings for "away"
or "decide" finds them. It is kept per project. `/autonomy` is the same rules
from the box, and it is the way to name a wait of your own.

And on one question in front of you, `d` hands that decision back.

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
`.codeaf/autonomy.json` beside the project, and a conversation with no project to
keep it in says so rather than pretending.

**A what-did-you-mean question can never be handed over.** codeaf refuses it:
there is nothing for it to decide, because the whole question is what you meant.

## None of the answers it offered are right — say what the real question is

`n` is the answer that is not on the list. Type what you think should have been
asked and press enter; it goes back to the asker as a reframe rather than as a
pick, and nothing is chosen.

## Fill in the blanks — the holes in a sentence, and what the arrows do on the question above your box

Some questions are a sentence with holes in it rather than a list of answers:

    land them in [ ~/notes ] as [ a new file ] and keep the old copy: [ no ▾ ]

`tab` moves to the next hole (`tab next blank` on the page's foot) and
`shift+tab` back. Both stop at the ends rather than wrapping. Typing fills a
hole; `←→` walks the choices where a hole has a short list of them. The walk is
the shape's own verb — it stays on the foot at a hundred columns, the way
`space tick it` stays on a checklist.

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
the row the `▸` is on, `tab` moves the `▸` (`tab next row`; `shift+tab` back),
and `enter` sends what is ticked — the key row says `enter send what is ticked`
once something is, and `enter` over nothing ticked sends nothing. There is no
`take it` on a checklist: the answer the asker would tick says `◆ recommended`
at the right of its row, and ticking it is yours to do. The tick stands in a cell
of its own between the digit and the word, so the digit that toggles a row is
always on it. On the page, `space` ticks the one you
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

**The approval question does** — the one codeaf asks before it runs a tool. It
has all of the above: the digits, `esc` for later, the chip, the receipt, the
settle guard, the narrow card and the phone sheet. `permissions` is its own page
and states what each answer banks.

**So does the standing card** — `wants to keep an eye on:` with `1 yes, set it up`,
`3 just once` where the item can be done once at all, and `0 no`. What is left in
the conversation is the card itself: your own words, the `when ·`, `where ·` and
`costs ·` bands, and the meter where the engine put a deadline on it. The answers
are up above the box with everything else you are being waited on for, and each
one says what it costs beside it. `o` starts an updated request — it turns
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
with no visible no is a question nobody can end. The waiting mark and the
sentence arrive as one fact: the moment a sign-in needs you, the line is already
`connect your <Name> account?`, on this page, on home and on the tab.

**So does the harness lane's pair.** An offer to run a saved program is one line —
`run harness "research"?` with `1 run it` and `2 not now`. A finished harness
design is a card — `wrote a program: <name>` with `1 save it`, `2 change it` and
`3 drop it`, each saying what it costs beside it. `2` resolves nothing: it walks
into the design's own room, where a change is typed, and the page stays waiting
until the rewrite lands. The page itself stays down in the conversation, where it
can be scrolled and read; only the asking is above the box. A design waiting on
you is answered with the same three digits from inside its own room, which used
to have a second row and two chords of its own.

**So does the task proposal** — `wants to start a task:` with `1 start it` and
`2 no`, and its clock on the end of the row. **So do the two cards you raise
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
question's widening yes, `2 always`, has a **second beat** on a shell command:
it replaces the answers row with the shapes the rule could be written as, and
picks one before anything is written.

```
  always? 1 git status*  ·  2 git *  ·  3 just this line  ·  esc never mind
```

While a beat is up, the digits belong to it — `3` is the third shape and not the
third answer — and `esc` backs out of the beat rather than putting the question
off.
## Questions over the session host and on another machine — do questions work over --host, and on the engine behind an ordinary codeaf when I run it normally

**A question reaches you wherever the conversation is, and you answer it where
you are standing.** That is true on all three roads and there is nothing to turn
on:

- **an ordinary `codeaf` or `codeaf chat` in a project.** The conversation is not
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
so `codeaf` tells you the engine is an older or newer codeaf rather than starting
a session in which questions would silently never appear.

## Why can I not answer the question on this task page

Pressing a row of work on the **sessions** place opens that task's own page even
when the work belongs to a chat you are not sitting in — it reads that chat over
the same link, and it says so at the top.

If that conversation has stopped and is waiting on somebody, the page says so
under what it has read:

    ? which storage shape should this use? · answer it in that conversation

It is dim and it takes no key. **A page you are only reading cannot answer** —
amber and a key would be this page promising something it does not have. Go to
the chat itself (`space` `space` for home, then its row there) and the question is there with its
answers on it.

Without that line, a page like this drew a running clock over work that had not
moved since somebody was asked something an hour ago.

## What is not built yet

`O` opens a question out into a page of its own — see the sections above, from
"Open a question up and read it properly" down. `D` writes the setting into
`.codeaf/autonomy.json` beside the project AND answers the question in front of
you; a conversation with no project to keep it in says so.

**A row on home shows a question the model raised and does not take a key for
it** — see *The row on home shows the question but my key does nothing over it*.

The count of open questions in the status line IS built: that is the chip
described above. So are the tabs for several questions from one step, and so is
the per-project setting that answers
a whole kind of question while you are away (`/autonomy`, and `D` on a row).

**Every question the block draws is drawn only by the block now.** The last of the
older blocks — the connect offer, with its own answers row and its own keys — is
gone. The one card in this program that still answers to keys of its own is the
intake form `/subharness` opens for a saved program, which is a fullscreen page
with fields to fill in rather than a question above the box.
