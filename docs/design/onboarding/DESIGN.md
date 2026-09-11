# Onboarding — connection, controls, first conversation

*DRAFT — under review, not landed. Opened 2026-09-09 as a draft pull request for the
owner's visual review; nothing here has merged and the wording may still move.
Issue [#208](https://github.com/Agent-Field/aforge-v2/issues/208).
The code is `internal/tui3/onboarding.go` (the controls screen), `internal/tui3/firstrun.go`
(the flow and the connection step) and `internal/tui3/welcome.go` (the first conversation).*

The first run is three moments: **connect a provider → agree to models and spending →
say the first thing**. This is the rule that decided what is in each of them, and the
ledger of what was deliberately left out.

## The selection rule

A setting belongs on the first screen when all three are true:

1. it is **necessary to work**, or
2. it **materially changes the first experience**, and
3. it **can be understood before the person has used aforge**.

A setting can be important and still fail (3). Tool permissions are the clearest case:
nobody can usefully write exception rules before they have seen a tool ask for something,
so the right place to explain permissions is the first permission question, not a form.

## What the controls screen asks

| Control | Why it is here |
| --- | --- |
| **Daily limit** | Money is consequential, and an amount can be chosen with no knowledge of the engine. The shipped `$500` is a runaway backstop, not a considered new-user budget, and a person should see it before it is theirs. |
| **Chat model** | It decides the whole first experience, and somebody arriving with a preference can act on it in three keystrokes. Everybody else keeps the default. |
| **Work crew** | It explains why task work runs on models other than the one you are talking to, and offers the one adjustment worth having without exposing five role rows. |

Each carries **one line** about what it does. Everything else — the spending caveats, the
exact catalog id, the seats a crew is made of — is behind `?` on the field with the
focus, and goes when the focus does. A first screen that must be read before it can be
answered is a first screen most people leave.

`$500` **is not a new default and this wave did not move it.** The design study drew `$10`
to keep its illustration short; choosing a smaller backstop for new installs is a separate
product decision nobody has made.

## What it deliberately does not ask

| Left out | Where it lives instead |
| --- | --- |
| Memory | On by default. Shown, read-only, under `Other settings`. `/settings` changes it. |
| Tool permissions | Keeps prompting. Explained at the first real permission question, about the actual tool. |
| Task auto-start | Keeps its 15-second countdown. Taught on the task card, beside the countdown itself. |
| Per-plan approval, per-conversation ceiling | `/budget`. Different scope, different consequence, and both need a situated explanation. The previous flow asked all three money rows and that was the change with the largest single reduction in first-run length. |
| Individual crew seats, reasoning, routing, fallback, speed guard | `/settings` → Models, or the crew chooser's own detail. |
| Search, document, image, voice, video keys | Connected at the first use that needs them. |
| Concurrency, repair, background behaviour, context limits, practice | Defaults. Implementation tuning belongs after real use gives a reason. |
| Font, colours, timestamps, mouse, history, draft retention | Defaults. |
| Working folder | Shown on the first conversation, beside the box, where it is the context for the work. |

The row that names this is **`Other settings use defaults · review`** on a fresh profile
and **`Review other settings`** on one that has written any of them down. It never claims
somebody's configured settings are defaults, and what it shows is read straight off the
settings registry.

## The four rules that keep it honest

- **Every value is the resolved one.** The limit, the model and the crew are read back
  through `config.Settings` and `internal/config`; an environment variable that owns a row
  is named (`set by AFORGE_DAILY_BUDGET`) rather than quietly overwritten, and one that
  merely seeds a value says `from …` because a choice made here outranks it.
- **It writes only what it was given.** Leaving writes the day's limit. It writes a crew
  only where the person chose one here, or where the profile had none at all —
  `config.CrewConfigured` is true when *any* of the five class rows is written, "because a
  person who pinned one tier by hand has an opinion the setup must not paper over with a
  preset". A crew that is nobody's preset reads `Custom` and stays that way.
- **Cancelling chooses nothing.** A cursor inside either list is provisional until enter.
  `esc` closes the list and puts back the value that was on the row.
- **It spends nothing.** No prompt is sent and no model is called during setup. The model
  list is the catalog this process already has.

## The right-hand example panel

At 112 columns and wider the composition is a 54-cell form, an 8-cell gap and a 36-cell
panel, centred as one 98-cell object. Below 112 the panel is not drawn at all — stacking
promotional content under a form puts it between a person and the thing they came to do.
The border and its padding are inside the panel's 36 cells, so the form's width and the
gap are unchanged by the frame.

**It is bordered, and it is the only bordered thing on this surface.** v3 draws no boxes;
restraint and dim telemetry are the north star, and nothing about the form on the left has
an edge. This is the deliberate exception, for the one thing a border is actually good at.
Unboxed, the column read as a *second column of the form* — more instructions, in the same
voice, about the fields on the left. A frame with a label on its top edge cannot be read
that way. The exception is the panel and nothing else: no control is boxed, and the rest
of the surface is untouched.

Its top edge is labelled **`◌ Example · what you can do`** and its foot carries
`An illustration. Nothing here has run.` Both are load-bearing. "Example" alone invites
"an example of what?"; "what you can do" alone is a claim about this machine; and a person
who has just watched three lines appear in order has watched something that *looks* like a
run. The mark is `◌` — this surface's own glyph for "nothing is turning", borrowed from
`styles.go` rather than invented, with `o` in the ascii tier.

### The one-shot demonstration

The request **types itself out** behind the `›` marker the transcript opens a person's own
line with, and the three lines under it arrive in order. Six beats of typing at 150ms and
one beat per line: about a second and a third, then it is still.

- It is armed by exactly **two deliberate acts** — arriving on the screen, and moving the
  focus or browsing with `←`/`→` to an example that is not the one already showing.
- **Any other key settles it at once**, jumping to the finished state rather than freezing
  half-drawn. Typing an amount, narrowing the model list, opening a chooser: all settle it.
- There is **no loop and no timed switching**. Nothing on this screen changes what it shows
  without a person asking.
- The **screen-reader tier never animates**: under `Options.Linear` the panel is built
  finished on the first frame and no beat is ever armed, so one static illustration is read
  aloud rather than three lines announced again as each arrives.
- The panel is a **fixed height** for a given example — the rows the lines will land in are
  drawn empty before they arrive — so the frame at the first beat is the size of the frame
  at the last.
- A beat advances **one integer on the flow** and repaints. It moves no focus, writes
  nothing, clears no pending amount, and never takes the caret, which stays in the field on
  the left the whole time. Beats are stamped with a generation, so one left over from an
  example somebody has browsed away from is dropped rather than driving the current one.

The crew's example is the one that spells a command, and it spells a real one:
`/task Fix the failing tests and explain the changes.` — `commands.go`'s own row, *start
work you can walk away from*. The three lines under it say a brief, work, and a page to
read, and they deliberately do **not** say "a plan you approve": `/task` asks nothing of
you (`internal/manual/chat/commands.md`), and a first screen promising a gate this road
does not have would be selling a capability the product lacks.

Nothing in the panel is a measurement, a price, a discovered file, a live status or a model
call. Each line is a *shape* of a result — "work you can watch or walk away from" is true
of every run of that request, where "41 files read" would be a claim about a run that has
not happened.

The mapping is the design's: the money row is accompanied by following the work and its
cost, the crew by handing something off, the model row by understanding a project. A
fourth — comparing two options — belongs to no field and is reachable only by browsing,
which is the point: the panel is an invitation, not a caption.

This adapts [W3C carousel guidance](https://www.w3.org/WAI/tutorials/carousels/), which
asks for user control over moving content, and the motion is one-shot and person-triggered
rather than auto-advancing. It is a design hypothesis, not measured conversion evidence.

## Geometry

| Width | What is drawn |
| --- | --- |
| 120×24 | header row, blank, 54-cell form, 8-cell gap, 36-cell bordered panel; ≥3-cell outer margins |
| 80×24 | one centred 64-cell form, no panel |
| 60×20 | the same form at 54 cells, explanations wrapped |
| 40×16 | the form at 34 cells; blank rows and the explanations of unfocused fields are given up |

Rows are surrendered **whole block at a time**, ranked: blank rows first, then the
explanations of fields that are not focused, then the review's closing line, then the
lead, and last the explanation of the field you are on. The three values,
`Start a conversation` and the keyboard legend are never given up. A wrapped sentence goes
whole or stays whole — an earlier build cut one after its first line and left a fragment
hanging under a field.

The legend is measured against the whole composition rather than the form, because it is
the only row the panel never stands beside, and it is cut by **whole clauses**: a legend
that ends `enter…` has taught nobody anything. The two keys that *drive* the form come
first — what enter does, then `tab moves` — because a person told only about enter and esc
has been told everything except how to reach the other four rows. The way out is third and
appears from 60 columns up.

One monospace size throughout. Hierarchy is bold values and heading, ink on the focused
label, narration weight on the focused explanation, and dim reserved for **metadata** —
where a value came from, the keyboard legend, the panel. An earlier build painted the
labels and every explanation dim, and the result was a form whose only legible thing was
the illustration beside it. Exactly one accent: the row the person is standing on. No
nested cards and no bright panel; the one border on the screen is the example panel's, and
it is there to say *not yours*.

## The first conversation

The greeting that follows the setup **leads with the question**: the heading
(**What would you like to work on?**), one instruction, the real working folder, and three
starting points in place of the usual dim try line. Only the selected starting point gets a
helper line.

The three-row wordmark and the `~deepseek/… · max crew` line under it are the *returning*
greeting and are not drawn here; a one-word signature stands in their place. The first
build of this screen put four things above a heading nobody had seen before — a logo three
rows tall, a raw model id chosen ninety seconds earlier on the screen behind it, and a
blank — and then said the same model and crew again in the status row at the foot. The
returning greeting is unchanged.

Three rules:

- **The composer does not move when typing begins.** Every later greeting is dismissed by
  the first keystroke and the box drops to the foot of the frame; on a first conversation
  that moves the thing the person aimed at, mid-word. This one stands until a message is
  actually sent.
- **A starting point fills the box and sends nothing.** Two of the three are deliberately
  unfinished (`Fix this for me: `) because a complete request about somebody else's
  project would be guessing at their work.
- **A draft is never destroyed.** The selection can only be moved over an empty box, and
  the fill refuses a box that is not empty.

Nothing is scanned on arrival and nothing pretends work has already happened.

## Deferred, and why

**The tab bar and the place switcher are not touched by this wave.** #208 as originally
groomed reached into how the seven places are discovered; that is a navigation redesign
with its own surface area — the chords, the map, the phone-width deck — and folding it
into a first-run change would have made a visual review of the first run impossible to
separate from a review of the whole surface. The discoverability that landed in #167
stands unchanged.

**A mandatory command tutorial is not here.** `/` on an empty box still lists every
command, and the manual answers "what can you do?" — teaching a command list before
somebody has a reason to want one is the failure mode this whole design is built against.

**#83 is narrowed, not closed.** Only the daily limit is part of this implementation, with
no-limit supported as a first-class answer. Per-plan approval, the per-conversation
ceiling, and the rest of that issue's scope remain open as settings and contextual
decisions.

## Discoverability ledger — where each control is reachable afterwards

Every control on this screen has a door that does not involve the setup:

| Control | Doors |
| --- | --- |
| Daily limit | `/budget`, `/limits`, the money segment on the status line, the spend place (`alt+5`), `/settings` → Spending, a refused turn's own message |
| Chat model | `/model`, the model word on the status line, `/settings` → Models |
| Work crew | `/crew` (bare, or `/crew max`), `/settings` → Models |
| Memory | `/memories`, `/settings` → memory & practice |
| Permissions | the first permission question, `/settings` → Safety |
| Task countdown | the task card's own countdown, `/settings` → Safety |
| Working folder | `/workspace`, and the folder line on the first conversation |
| Everything the setup skipped | `/settings`, and the manual's own pages |

## Where the record is

- The compiled chat manual: `internal/manual/chat/getting-started.md` (the whole flow) and
  `internal/manual/chat/empty-screen.md` (the first conversation).
- Behaviour tests: `internal/tui3/onboarding_test.go`, `internal/tui3/firstrun_test.go`,
  `internal/tui3/setupskip_test.go`.
- The recording recipe and the frames it produced: `docs/design/onboarding/RECORDING.md`.
