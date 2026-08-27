# Subharnesses

## What a subharness is — the programs you can run

A **subharness** is a named program for one narrow kind of work that comes round again:
"the weekly marketing note for company X", "chase a flaky test". It is not a conversation
and it is not a prompt. It takes a **typed input** — named fields, some of them required —
and answers a **typed output**, so a run either produces the shape it promised or it is
incomplete.

Three things follow from that, and they are the whole feature:

- **You settle the input before it runs.** That is the intake card (below): every field of
  the schema, the answered ones stated, the required blanks marked.
- **They come from four places and nothing on screen says which is which.** Some ship in
  this binary; some are bundles under `~/.aforge/subharnesses`; some are bundles committed
  into the project you are working in; and some are the pages a design wrote when you asked
  aforge to build you one, under `~/.aforge/harnesses`. The mark on a row reads `built-in`,
  `yours` or `from this project`, and that is where it was found, not what language it is
  in or which door built it.
- **A run is a task.** It gets a roster row, a room, a journal and a `✕`, like every other
  piece of work you can walk away from.

**A harness is a subharness.** One system, one name for it, several doors onto it. What you
get by asking aforge to build you one — "make me a harness for the weekly marketing images"
— is saved as a page and is on this list from the moment it is saved, marked `yours` like
anything else of yours. `/harness` and `/harnesses` are the other door onto those, with a
picker and a typed request instead of a card; the *Saved shapes of work* page describes
designing one. `/subharness` and `/sub` list everything runnable here, whichever door made
it.

## /subharness — the list of programs you can run

Type `/subharness` (or `/sub`) with nothing after it. A filtering list opens under the
message box, at most **12** lines, with the filter box in the box's own place:

```
flake-triage      chase a flaky test · up to 15m · built-in
weekly-update     the Monday note for the team · yours · yesterday · finished · $0.12
```

The left is the name. The dim tail is what it is for, then how long that kind of work is
allowed to take, then where it came from, then the last run.

**The last run reads `when · how it went`**, and a cost after it when one was reported:
`yesterday · finished · $0.12`, `1h · incomplete`, `10m · finished`. A run that did not
reach the end it promised is `incomplete` and is never called a failure. `/harness` spells
the same fact about the same program in the same words — one system, two doors, one
vocabulary.

**Anything nobody said is simply not drawn.** A subharness that has never run carries no
last-run note at all — not `never run`, not `0 runs`. One whose manifest declares no
budget shape draws no time. A cost nobody reported is not drawn either, because a provider
that published no figure left "nobody said" behind and not "free". A gap in a row is
readable; a zero that has to be explained is not.

**A run started from `/harness` shows up here too.** The two doors read one history: a
program you had designed keeps a full trace of every run beside its page, a bundle keeps
a one-line note beside itself, and the row answers with whichever of the two is newer. So
running `triage-flake` from the `/harness` picker this morning is what this row says about
it this afternoon.

**Typing narrows it** over three things: the name, the one line, and the **cues** — the
words somebody wrote down at design time meaning "this is that kind of work". That is what
makes it findable when you remember what a program is for and not what it is called: typing
`flaky` reaches `flake-triage`.

**One kind carries no cues:** a program saved as a page by a design. The words it was
designed against are not kept on the page, so it is found by its name and by the one line it
was given — which is why that line is worth writing well when you approve the card.

| Key | What it does |
| --- | --- |
| `↑` / `↓`, also `ctrl+p` / `ctrl+n` | move |
| `pgup` / `pgdown` | move by a page |
| anything printable | filter |
| `enter` | open that one's intake card |
| `esc` | close the list |

A click on a row **moves the cursor and does not act**. One of the rows on the card behind
this list starts work and spends money, and a click that did that is a gesture nobody can
aim.

A filter that matches nothing says so where the rows were: `nothing here by that name`.

## /subharness &lt;name&gt; — going straight to one

`/subharness <name>` skips the list and opens that subharness's intake card. `/sub <name>`
does the same.

A name **nothing answers to is not an error here** — it becomes the list's filter instead.
So `/subharness flake` opens the list narrowed to `flake-triage`, and a typo turns into a
search rather than a complaint.

Names are one lowercase word: letters, digits, `-` and `_`, starting with a letter, at most
64 characters. That is the same string in the binary, in the store on disk and on this
command line, which is why this command takes one where `/harness` does not.

## The harness I just had built — how an approved design reaches this list, and how to run it

You asked aforge to build you a harness, you approved the card, and the design settled with
two lines:

```
subharness "social-marketing-images" v1 saved
/subharness runs it, and it offers itself when what you say matches.
```

That is where it is. Open `/subharness` and it is a row: its name, the one line it was
designed for, and `yours` as its mark — the same row shape as everything else on the list.

**No refresh and no restart.** The list is read from the disk each time you open it, so a
page approved a minute ago is on it, and so is one another window saved while you sat here.
If it is not there, the name is worth checking first: what you type after `/subharness` is
the same string the design saved it under.

Its card asks for one thing:

```
social-marketing-images · the weekly marketing pictures for company X · yours
▲ brief             what this run is about, in your own words
  run it
```

**One free-text field, and it is required.** The request goes in in your own words — the
same sentence you would have typed after picking it with `/harness ` — and there is nothing
else to fill in, because that is all a page can be handed.

The row draws no time and the card promises no shape: a page states neither, and drawing
either would be a claim the design never made. From `run it` it is a task like every other
run — a roster row, a room where its steps land as they happen, a journal, and a `✕`.

## When aforge offers one — the card it raises by itself

You do not have to go looking. When what you are asking for is the shape of work a saved
program already does, aforge offers it — and the offer is **this same intake card**, raised
in front of you with a line saying why:

```
flake-triage · chase a flaky test · up to 15m · built-in
this looks like flake-triage: the brief and a failing test name are both here
▲ test              the failing test's name
  branch            main
  [ 1 run it ]  [ 0 no ]
  it runs as a task beside this conversation — you can watch it, answer it, stop it
```

**The reason line is aforge's own**, in your terms and about what matched — not a
description of the program, which you can read on the line above it.

**The fields arrive already filled in** from what has been said. What was read out of the
conversation is drawn brighter, the schema's defaults stay dim, and `▲` still marks
anything required that nobody has answered. You change what is wrong and fill what is
missing exactly as on any other intake card.

**The last row is the answers**, and that is the only difference from the card you open
yourself. `run it` starts it; `no` ends it and nothing happens. Under the row is one line
saying what the answer under the cursor will actually do:

- on `run it`: `it runs as a task beside this conversation — you can watch it, answer it, stop it`
- on `no`: `nothing runs, and we carry on here`

**Nothing runs because aforge suggested it.** There is no countdown that says yes for you,
no default and no "you did not object": the only thing that starts a run is you answering
`run it`.

While the card is up, the status line reads `waiting · your call`. Every other window on
this machine, and `/home`, sees this conversation as `waiting on you`, with the line
`wants to run <name>` under it.

## Keys on the card aforge raised

| Key | What it does |
| --- | --- |
| `←` / `→` | walk the answers |
| `enter` | take the answer under the cursor |
| `1` | run it, from anywhere on the card |
| `0` or `esc` | no — nothing runs |
| `↑` / `↓` | move between the fields and the answers |
| `enter` on a field | open the box and type its value |

The hint under the answers reads `←→ · enter takes it · 0 or esc, no`.

**`esc` here is a no and not a way out.** A turn is waiting on this question, so the key
that dismisses every other overlay answers this one instead — nothing runs, and the
conversation carries on immediately.

**If you never answer it, nothing runs.** The offer holds the turn for at most **15
minutes**; when that runs out the card comes down by itself, the line under the
conversation reads `<name> · the offer ended, nothing ran`, and the conversation carries
on. Interrupting the turn with `ctrl+c` ends it the same way. That window is a bound on the
turn, not a deadline on you: it can only ever end in nothing having run.

**Switching to another conversation does not answer it.** The card is put away with the
conversation and is there again when you come back to it, filled in as it was raised; a
conversation waiting on one shows `waiting on you` on `/home` and in every other window
meanwhile.

**aforge offers at most one at a time, and stays quiet when it is unsure.** An offer is
only raised when the program's own name or one of its cues actually appears in what has
been said; a weak match raises nothing at all, because a suggestion you have to swat away
costs more than one you never got.

**There are no offers where the card cannot be drawn.** Over `--host`, in a headless
`--once` run, and inside a task, aforge is not given the ability at all rather than
offering something nobody could answer.

## The intake card — filling in what a subharness needs

The card is what opens on `enter` from the list, and what `/subharness <name>` opens
directly. **It is one card either way** — the same rows, the same keys.

```
flake-triage · chase a flaky test · up to 15m · built-in
▲ test              the failing test's name
  branch            main
  run it
```

The first line names the subharness and repeats the tail from the list. Under it is **one
row per input field**, and then `run it`.

Each field's tail is one of three things, in this order:

1. **the answer**, if something has been filled in — drawn brighter than the rest of the
   row, because it is the thing the card is about;
2. **the default** the schema states, when nobody has answered;
3. **what the field is**, in the schema's own words, when there is neither.

A field with none of the three draws nothing at all.

**`▲` marks a required field nobody has answered.** It is the same mark a person being
waited on wears everywhere else in aforge. A field that is answered, or that you do not
have to answer, has no mark — so the card's marks are exactly the list of what is still
needed.

The cursor opens on the first thing you have to answer, and on `run it` when there is
nothing to answer.

## Keys on the intake card

| Key | What it does |
| --- | --- |
| `↑` / `↓`, also `ctrl+p` / `ctrl+n` | move between the fields and `run it` |
| `enter` on a field | open the box and type its value |
| `enter` on `run it` | start it |
| `esc` | back to the list, or close the card when it was opened by name |

`enter` means "act on the row under the cursor", which is what it means everywhere else in
aforge. There is no separate key for starting the run — `run it` is a row.

While a field's box is open it takes the message box's place, with the placeholder
`the value · enter keeps it · esc`. `enter` keeps what you typed; `esc` leaves the field
exactly as it was. **An empty box clears the field** rather than storing a blank, because
`""` is an answer and a blank is not.

What you type is read as the schema asked: a `string` field keeps your text as text, a
`boolean` takes `yes`/`no`/`true`/`false`, and everything else is read as JSON when it is
valid JSON and as text when it is not. A field with no type on it is read as text.

After a field is kept, the cursor moves to the next required blank, or to `run it` when
there is none left.

## Starting a run, and what stops it

`enter` on `run it` hands the subharness's name and the input the card settled to the
launching door.

**The input is the fields you actually answered**, in the card's order, and nothing else. A
field carrying only its schema's default is **not** sent: the default is written down once,
in the schema, and the runner reads it there.

**A required field still blank stops it, out loud.** Nothing is sent, the line under the
conversation reads `still blank · <field>`, and the cursor lands on that field.

When the run starts, the overlay closes and the line reads
`subharness <name> started · <title>`. **From there it is a task**: a roster row, a room,
a journal and a `✕` that stops it, like every other piece of work you can walk away from.
This list draws nothing further about it.

## What subharnesses cannot do yet

Stated plainly, because the surface is finished before everything behind it is.

- **Running one needs the launching door wired.** On a build where it is not, `run it`
  answers with the door's own sentence: `did not start · there is nothing here to run`.
  Nothing is half-done and nothing is spent.
- **A card opened cold can still be blank.** The fields are filled from what has been said
  in this conversation, so a card opened before anything relevant has been said — or one
  whose required fields nothing answers — opens with those fields marked `▲` and waiting
  for you. That is the honest card, not a fault.
- **An offer cannot reach you everywhere.** aforge offering one by itself is the card
  described above, and it is raised only where a window can draw it: not over `--host`,
  not in a headless `--once` run, not inside a task. In those places the ability is absent
  rather than present and failing.
- **No subharness list over `--host`.** The registry lives on the far machine and this
  build has no door onto it. The command answers `<machine> owns subharnesses ·
  change it on that machine` and opens nothing; it does not report that registry empty.
- **Writing a bundle is not a command.** Asking aforge to build you one is: say so in the
  conversation and a design is started, and the page it saves is on this list. What you
  cannot do from here is write the *bundle* form — the one with a schema of several fields —
  which is a file you put on disk yourself.

## Why /subharness says there are none

One sentence covers every way of having none, because they are one fact from where you are
sitting — there is nothing to pick:

```
no subharnesses here yet — a subharness is a saved program for work that comes round again.
```

You get it when no registry is wired or when the local registry is empty. Over `--host`,
the command instead names the connected machine and says to change it there, because this
surface has not asked whether that registry is empty. **No list opens behind either
answer.** An overlay with no rows would be a thing you had to dismiss before it could tell
you it was useless.

The general-purpose worker is never on this list. It is what you get when you pick nothing,
not something you pick, so offering it would be offering the absence of a choice as a
choice.
