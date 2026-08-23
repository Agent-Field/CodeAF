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
- **Some ship in this binary and some are written as bundles on disk**, and nothing on
  screen says which is which. The mark on a row reads `built-in`, `yours` or
  `from this project`, and that is where it was found, not what language it is in.
- **A run is a task.** It gets a roster row, a room, a journal and a `✕`, like every other
  piece of work you can walk away from.

**A subharness is not a harness.** They used to be one word here and they are not any more.
A harness is a saved shape of work aforge learned from watching you (`/harness`,
`/harnesses`, and the *Saved shapes of work* page). A subharness is a typed program with a
schema and a card. `/subharness` and `/sub` reach this one.

## /subharness — the list of programs you can run

Type `/subharness` (or `/sub`) with nothing after it. A filtering list opens under the
message box, at most **12** lines, with the filter box in the box's own place:

```
flake-triage      chase a flaky test · up to 15m · built-in
weekly-update     the Monday note for the team · yours · ran 2d ago
```

The left is the name. The dim tail is what it is for, then how long that kind of work is
allowed to take, then where it came from, then when it last ran.

**Anything nobody said is simply not drawn.** A subharness that has never run carries no
last-run note at all — not `never run`, not `0 runs`. One whose manifest declares no
budget shape draws no time. A gap in a row is readable; a zero that has to be explained is
not.

**Typing narrows it** over three things: the name, the one line, and the **cues** — the
words somebody wrote down at design time meaning "this is that kind of work". That is what
makes it findable when you remember what a program is for and not what it is called: typing
`flaky` reaches `flake-triage`.

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
- **Nothing fills the card in for you yet.** Every field opens blank and every required
  field opens marked, which is the honest card for `/subharness <name>` typed cold.
  Reading the conversation and filling the schema from it is chat's job and lands with the
  session side; when it does, the answered fields simply arrive already stated.
- **No last-run notes yet.** They come from the run journals the store keeps beside each
  program, so until that lands every row draws nothing there — which is exactly what a
  subharness nobody has run should draw.
- **aforge does not offer one by itself yet.** When it does, it will raise this same card
  with a line saying why it matched. Nothing ever runs without the card.
- **No subharnesses over `--host`.** The registry lives on the far machine, so the command
  answers `no subharnesses here yet — a subharness is a saved program for work that comes
  round again.` and opens nothing.
- **You cannot write one from the chat yet.** Building the program is not a command.

## Why /subharness says there are none

One sentence covers every way of having none, because they are one fact from where you are
sitting — there is nothing to pick:

```
no subharnesses here yet — a subharness is a saved program for work that comes round again.
```

You get it when no registry is wired, when the registry is empty, and on every `--host`
session. **No list opens behind it.** An overlay with no rows would be a thing you had to
dismiss before it could tell you it was useless.

The general-purpose worker is never on this list. It is what you get when you pick nothing,
not something you pick, so offering it would be offering the absence of a choice as a
choice.
