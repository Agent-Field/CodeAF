# Saved shapes of work

## What a harness is

A harness is a shape of work you have done before, saved so it can be recognised and
offered again. It is a named, versioned procedure: steps, a list of the tools those steps
may use, and its own bounds.

You build one by asking for it in a sentence. You keep it by approving a card. Afterwards
it lives on disk and shows up in a list.

**There is no slash command that runs one.** Running is offered by the turn itself: you
type what you want, and if your words match a saved harness closely enough, aforge asks
whether to run it instead of answering the ordinary way. `/harness` lists what you have.

Three rules govern that offer:

- **It is a question, never a routing.** Nothing runs because a matcher said so. Saying no
  costs nothing and leaves the ordinary turn intact.
- **It is asked once per turn**, before the first request.
- **It is silent when nobody is watching.** With no registry, no runner, or no interactive
  screen, the whole feature does nothing at all.

## Why aforge offered me a harness

The detection that raises the offer is a table lookup. No model is called. It is
deterministic — the same words give the same answer on every machine.

Signals combine as independent evidence. Naming the harness fires on its own (weight
**0.9**). One whole multi-word cue fires on its own (**0.9**). Most of a phrase almost
fires (**0.6**). A single-word cue never fires alone (**0.45**; two of them together reach
0.70). The description counts at half weight (**0.5**) and can never carry a match by
itself.

The card is raised when the best entry scores at least **0.55**, or when it leads the
runner-up by **0.25**. Nothing scoring below **0.40** may reach it by either road. Ties go
to the earlier entry, and a tie is by construction not a clear lead.

**Only what a person typed is matched.** A woken turn, a note the session wrote to itself,
and an empty message are never matched. Offering a harness against a task's own completion
report would be the harness talking itself into work nobody asked for.

If you want to know why a harness you saved yesterday is no longer being offered, read
*Why it stopped offering after a restart* on this page — that is a real limit of this
build, not a matching accident.

## The offer card and the keys it takes

One row appears under the connect offer and above the message box:

```
? run harness "research"? · finds an answer across sources · [enter] run · [esc] no
```

| Key | What it does |
| --- | --- |
| `enter` or `y` | run the harness |
| `esc` or `n` | no — the ordinary turn goes ahead |
| `ctrl+c` | not swallowed; mid-turn it is still the interrupt, which releases the held turn |

Every other key does nothing while the row is up. It is modal, because the session is
holding a turn on your answer.

A click works too. Each key chip **and the word beside it** are one target. A press
anywhere else on the row is swallowed rather than falling through to what is underneath.

The line is assembled longest-first and shortened a piece at a time on a narrow frame: the
description goes first, the model survives one rung longer, and **the answers are never
dropped**. If a second question is queued behind this one, a dim row under it reads
`  N more`.

**Nothing is written to the transcript either way.** A yes is followed by the run, which
the surface notes as `harness · <name>`. A no changed nothing.

An offer whose turn has already ended is dropped unanswered — the answer would be late. A
harness with no name raises no card at all.

## Naming a model for the run

End the turn with `with <model>`, `using <model>` or `via <model>`:

```
research the pricing tiers with opus
```

The clause is read **at the end and only there**. The same words in the middle of a
sentence are ordinary English and choose nothing. It is bounded at **3 words**, must not
contain a function word, must be spelled with id characters (letters, digits, `-` `.` `_`
`/` `:` `~`), and its first word must contain a letter.

When the word resolves, it is stripped from your text before scoring **and** before the
run, so naming a model never makes the offer less likely to appear. The card then shows
` · model: claude-opus-5` in dim text, with the vendor prefix dropped, and the model the
row showed is what travels back with your answer.

**Naming a model is never a refusal.** A word this install cannot place leaves the clause
where it was, and the card says so instead:

```
model "opus" not found, running default
model "opus" matches several here, running default
```

A session with no model catalog cannot check an id, so the word travels as written.

## What a yes actually runs

The harness's head version is loaded from the store and walked step by step over a model
bridge. Each `agent.loop` step is one completion. Each `tool.call` step is one tool. Each
free-text condition is one small judgement. A single step's completion gives up after
**10 minutes**.

The trace is saved beside the harness. The turn's answer is the run card, then what the
run produced, then a line reading `trace · <path>`.

**A failed run still reports.** The trail is the one thing worth having when a harness went
wrong: the card's head reads `name · v1 · failed` and the failing step is marked `✗`.

The tools a harness may reach are a fixed bare set over your workspace, not the belt the
conversation itself carries. Notes, jobs, connected accounts and image generation are
things a conversation reaches for, not things a saved procedure should inherit. `bash`,
`read`, `ls`, `grep` and `find` accept a bare string. `edit` and `write` take JSON and say
so: `edit takes its arguments as a JSON object, and "..." is not one`. An unknown tool
gets `there is no tool named "x" on this surface`.

Two limits worth knowing:

- **A `human.gate` step auto-approves.** There is no lane on this screen that stops and
  asks you. The trail records that it auto-approved.
- **The run's model does not follow `/model`.** The client is built once at launch, so the
  conversation can move to another model and the run's client does not. A step that names
  its own model overrides it, and so does a turn that named one.

## Asking for a new harness in conversation

Start the message with a build request:

```
make|build|create|design [me] [a|an|the] [sub]harness for|to|that <goal>
```

Courtesy openers are stripped first: `please `, `can you `, `could you `, `would you `,
`let's `, `lets `, `i want you to `, `i'd like you to `, `i would like you to `.

The cue is **anchored to the start** of what you typed, so the same words inside a
paragraph ("the reason we make a harness for this is…") never trigger it. The joiner is
required: `make a harness` with no goal names nothing and goes to the model, which can ask
you what for.

Build detection is read **before** run detection, so "make a harness for triaging flaky
tests" is not answered with an offer to run the harness that already triages flaky tests.

**The turn does not wait.** It ends the moment the design starts, with no reply, and the
surface notes `harness · designing with <model> · <goal>` — the model named there is the
one actually writing the page: the one you typed in the sentence, or the `designer` role's
(`/settings` → Session), or the model you are talking to when neither is set. A build with
no model resolved at all notes plain `harness · designing <goal>`. The design runs on the
session's own context
rather than on the turn's, inside a **30-minute** window that covers both the model work
**and** the wait for your answer to the card.

Under the hood: one design pass, then up to **2 retries** in which a refused design is
shown the exact sentence it failed on and asked to fix it, then **one** review pass that
writes findings and a patch rather than a whole new page. A failed review loses only the
improvement, not the draft.

The whole path is off unless there is a runner, a store, **and** an interactive screen to
answer the card on.

## What a design can be refused for

A design must pass validation and a lint before it can be offered to you. The lint refuses:

- a tool list naming a tool that does not exist here,
- a `subharness.call` to a harness that is not registered,
- a checking step with no condition to check,
- an empty description,
- **fewer than two cues** — with one cue "the entry would be unreachable by anything but
  its own name".

A refused design is shown the sentence it failed on and gets up to two attempts to fix it.
Malformed JSON gets a free salvage attempt and then one repair turn.

## The save-or-discard card

The page the designer wrote is shown to you before anything is kept. It arrives by itself,
minutes after your build request, on a standing lane rather than on any turn.

The card is drawn above the question in dim text, capped at **14 lines**. A longer page is
cut with a line reading `… N more lines` rather than quietly stopping.

The question row changes only its verb from the run offer:

```
? save harness "triage-flake"? · [enter] save · [esc] discard
```

`enter` or `y` saves. `esc` or `n` discards. A design's row never repeats its description,
because the card's own head line already carries it.

A landing design closes the settings sheet and the expand view first — a question drawn
under a fullscreen panel is an answer nobody can reach.

**Nothing is saved without an answer.** A design nobody answered changed nothing. Unlike a
run offer, a design card is **not** swept away when the turn settles: it outlives its turn
on purpose.

Every ending says something, both as a dim note in the transcript and as an ambient note to
the model, so the next thing said in the conversation happens after aforge knows what
became of it:

```
harness design failed: <err>
harness "triage-flake" was designed and not saved
harness "triage-flake" could not be saved: <err>
harness "triage-flake" v2 saved
```

## Where harnesses are stored

Under the state root, one directory per harness, with immutable version pages and the runs
beside them:

```
~/.aforge/harnesses/<name>/v1.json
~/.aforge/harnesses/<name>/v2.json
~/.aforge/harnesses/<name>/run/20260816T101112Z.json
```

Setting `AFORGE_HOME` moves the whole tree.

**There is no head file.** The head is simply the highest version page present, so a
pointer can never disagree with the pages it points at. The version is minted by the store
and never chosen: an approved page has its version zeroed before saving, so a new name
lands as v1 and an existing name lands as the next version.

Writes are exclusive-create, so a second save racing the first fails loudly instead of
quietly replacing a version somebody has already read. Run traces are timestamped in UTC,
sortable, to the second, with a `_NN` suffix when two runs land in the same second.

**Every window on this machine reads and writes the same place.** The store holds no cache
— a stale read would be the more expensive mistake — and the registry is read on the
keystroke rather than held from boot. A harness saved in another window ten minutes ago is
in your list now.

A harness you just saved is reachable **from the very next sentence**: the session merges
what this conversation has built into the registry it matches against, and the saved entry
wins over the in-memory one of the same name.

Refusals from the store name themselves: a name that is not a short slug, a save that asks
for a version other than the next one, a version past the cap, and `subharness: not found`.

## /harness — the list of what you have

Type `/harness` or `/harnesses`. Both open the same panel.

A short list opens under the message box, at most **10** lines:

```
◆ triage-flake       v3 · 6 runs · last ok, 2h ago
◆ review-diff        v1 · never run
```

(`#` instead of `◆` on the linear palette.)

| Key | What it does |
| --- | --- |
| `↑` / `↓`, also `ctrl+p` / `ctrl+n` | move |
| `pgup` / `pgdown` | move by a page |
| `enter` | open the highlighted harness |
| `esc` | close the panel |

The name and version sit together on the left, because "triage-flake v3" is the sentence
people say. The description and the history are the dim tail. Ages are coarse: `just now`,
`12m ago`, `3h ago`, `5d ago`. `1 run` is spelled singular. A harness that has never run
says so. A row whose trace could not be read shows the count alone rather than a shrug.

The registry is read on the keystroke, not held from boot, so a harness registered in
another window shows up. Each row reads the **head** version.

`enter`, or a click on a row, closes the panel and prints the harness's card **into the
conversation** — its steps and its bounds, with the last run's card under it when there is
one. The transcript is where prose lives and can be scrolled and copied.

Limits:

- **No argument form.** You pick a harness from rows you recognise. Typing a name after
  `/harness` is not a thing this build does.
- No registry wired here: `harnesses are unavailable here`.
- An empty registry: `no harnesses are registered yet — build one in the conversation`.
- A registry that cannot be read at all draws the empty list rather than an error.

## Why it stopped offering after a restart

This is a real limit of this build, and it is worth stating plainly.

**Cues are not saved to disk.** A harness page has nowhere to put its cue list. The
registry aforge builds at launch therefore carries no cues at all — entries are built from
name, description and version only.

What follows from that:

- A harness designed **in this session** keeps its cues in memory and is matched by them
  for as long as the program keeps running.
- After a restart, that same harness is reachable by **naming it** (the 0.9 naming signal)
  or by description overlap. Description overlap counts at half weight, and half weight
  alone never reaches the threshold.

So if a harness used to be offered when you described the work, and now it is not, nothing
is broken and nothing was lost. Name it, and it will be offered. Or open `/harness` and run
it from the list.

## Things that are not available in this build

Two pieces of harness machinery exist in the code and cannot be reached in this build.
They are listed here so you do not go looking for them:

- **The running-harness chip.** The activity strip can draw a chip for a harness that is
  currently running, and clicking it would open the panel. No real session provides the
  chip with anything to draw, so it never appears. `/harness` is the way in.
- **A hosted `/harness <name>` trigger.** The harness machinery can offer a command-line
  trigger of the form `/harness <name>`, but nothing in this build mounts it. `/harness` is
  the panel and only the panel, with no argument form.

Two behaviours are absences rather than bugs, and are stated outright in the code:

- **A `human.gate` step inside a harness auto-approves.** A harness step that would pause
  and ask a person does not pause. There is no lane on this screen for it to ask on, and
  inventing one was refused rather than faked. The trail records the auto-approval.
- **The run's model does not follow `/model`.** A run rides the client built at launch
  unless a step or your turn names a model.

## Harnesses over --host

Two different things, two different answers.

**Running an existing harness works.** The offer card rides the turn's own event stream and
the answer travels on the wire like any other, so a remote session can offer a harness and
run it.

**Building a new one is switched off.** Over a `--host` connection aforge does not have
the designer at all, so asking for `make a harness for …` gets you an answer saying it
cannot be done from here rather than silence. The reason is that the card asking whether
to keep the finished page has no way to reach you: the design arrives on a standing lane
that a remote connection does not carry, so a design left switched on would have run,
written a page, and asked a question in an empty room. Build the harness while working on
that machine directly, and it is available to run from anywhere afterwards.

**`/harness` is unavailable over a connection.** The registry belongs to the far machine
and this build has no door onto it from here, so the command says exactly:

```
harnesses are unavailable here
```

rather than listing this machine's harnesses and offering to run them over there.

For the rest of what does and does not travel over a connection, see the page on running
the session on another machine with `--host`.
