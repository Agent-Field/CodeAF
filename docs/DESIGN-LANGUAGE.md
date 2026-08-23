# The design language — what this surface is allowed to look like

*The written contract for aforge's visual surface. `internal/tui3/styles.go` is
the code that holds it and `internal/tui3/designlanguage_test.go` is the part a
build can fail on; where this doc and the code disagree, the code wins. Written
after an audit of [basecamp/omarchy](https://github.com/basecamp/omarchy) — the
credit is stated properly at the end, along with what we took, what we measured
ourselves, and the one thing we refused.*

## What this is for

A surface with no stated language accumulates one. Somebody needs a row to stand
out, so they bold it. Somebody else needs a different row to stand out, so they
give it a border. A third person, finding both already spent, reaches for a new
colour — and now the surface has three ways of saying one thing and a person has
to learn all three. Nobody made that decision; it happened.

So the decisions are made here, once, in advance, and they are made SMALL. There
are four ways this surface can raise something, and this document is all of
them.

## THE GROUND LADDER

A row can wear a ground — a run of cells lifted off the terminal's own
background. There are four steps and there will never be a fifth.

| step | what it means |
| --- | --- |
| **rest** | nothing at all. The row is the terminal's own background |
| **cursor** | the row the pointer is over, or the row the keyboard cursor is on |
| **selected** | the chosen thing: the current row, the current chip |
| **mark** | a marked span: copy mode's selection, the text a yank would take |

**REST IS NOT A COLOUR.** It has no value in the table because there is nothing
to author: an unremarkable row is painted by not painting it. This is the
emptiness law wearing its background clothes, and it is what makes the other
three legible — a surface that tinted every row would be a surface where a tint
said nothing.

**Cursor and hover are ONE step, not two.** Whether a person arrived at a row
with the mouse or with `↓`, the row they are on is the row they are on, and it
does not change appearance depending on which hand they used.

### THE EMPHASIS LAW

> **A row is emphasized by raising its ground and turning its leading text
> accent. Nothing else ever changes.**

No new colour arrives for the emphasized state. No run of bolding spreads across
the row. And above all **no outline is added** — emphasis is a step UP this
ladder, never a ring drawn around a thing.

Two moves, both already in the palette. The ground says which band of pixels is
being spoken about; the accent on the leading glyph or word says what it is. A
lane that finds itself reaching for a third move has found a state the ladder
does not have, rather than a colour the palette is missing.

### The values, and why they are authored rather than derived

The honest way to build this ladder is the compositor's way: take the foreground
colour, composite it over the background at a stated alpha, and every step
inherits the theme's own hue for free and self-inverts on a light terminal with
no light-mode branch at all. **We cannot.** The terminal's own background is
unknown to us — there is no variable that states it and no query a constructor
may block on — so there is nothing to composite over.

The steps are therefore **authored** per ladder, dark and light. What is
authored is aimed rather than guessed: the assumed dark ground is the range real
terminals sit in, `#101014` through `#1e1e2e`, and the light ground is near
white.

| step | dark | vs `#101014` | vs `#1a1b26` | vs `#1e1e2e` | xterm-256 |
| --- | --- | --- | --- | --- | --- |
| rest | *none* | — | — | — | — |
| cursor | `#242932` | 1.30:1 | **1.17:1** | 1.09:1 | 235 |
| selected | `#2E3440` | 1.52:1 | **1.37:1** | 1.31:1 | 237 |
| mark | `#434C5E` | 2.20:1 | **1.98:1** | 1.90:1 | 239 |

| step | light | vs `#FFFFFF` | vs `#ECEFF4` | xterm-256 |
| --- | --- | --- | --- | --- |
| rest | *none* | — | — | — |
| cursor | `#E5E9F0` | **1.22:1** | 1.06:1 | 255 |
| selected | `#D8DEE9` | **1.35:1** | 1.17:1 | 254 |
| mark | `#B7C0D1` | **1.83:1** | 1.59:1 | 251 |

The aim was cursor at 1.1–1.2:1 and selected at 1.35–1.5:1 — far below the 3:1
any accessibility guideline demands of a UI boundary, and just above the ~1.05:1
where a large flat area stops being perceptible at all. That is deliberate. A
selection tint is not a boundary; it is an anchor for something the row already
says in text.

The cost of authoring rather than deriving is stated rather than hidden: a fixed
ground reads one notch louder on a blacker terminal and one notch quieter on a
lighter one, and on a tinted page like `#ECEFF4` the whole light ladder drops
close to invisible. That is the price of not knowing, and it is cheaper than a
query that hangs.

Two things survived the retune unchanged, both deliberately. `#2E3440` is what
this surface has drawn under the pointer since the day it first drew a
background — it moves **down** one rung to become the selected step, so the row
a person has been looking at for four waves keeps its exact weight while the
pointer's own step gets quieter. And the light ladder's first two steps are
byte-identical to what they were, because they were already inside the band; a
value in band is not touched.

One more line holds the ladder honest below truecolor: **all six steps resolve
into the xterm-256 grey ramp** (235–239, 251–255) rather than its colour cube.
A ground that rounded into a hue would be a tint that looked like it meant
something, and no step on this ladder means anything by itself.

## THE PALETTE

The palette divides in two, and the division is the reason a surface can carry
eight colours and still read calm.

**The signal hues** answer *what kind of thing is this*. None of them outranks
the others, so none of them may be lighter than the others — the eye reads
lightness as figure and ground and hue as identity, so a set of signals at one
lightness reads as a single field at a glance and only resolves into colours
when somebody looks.

> **THE SIGNAL HUES SIT INSIDE A FIFTEEN-POINT HSL LIGHTNESS BAND**, on both
> ladders. `TestTheSignalHuesAreIsoluminant` holds them to it.

**The reading tiers** — ink, muted, dim — answer *how loudly is this being
said*. They are a ladder by construction, lightness is the whole of their
meaning, and the band deliberately does not govern them. The test excludes them
by name rather than by silence.

### Dark ladder, before and after this wave

| role | before | after | H | S | L | in band? |
| --- | --- | --- | --- | --- | --- | --- |
| ink | `#D8DEE9` | *unchanged* | 219° | 28% | 88.0 | reading tier |
| accent | `#9DC3E6` | *unchanged* | 209° | 59% | **75.9** | signal — band top |
| muted | `#7FA6C9` | *unchanged* | 208° | 41% | 64.3 | reading tier |
| dim | `#6B7280` | *unchanged* | 220° | 9% | 46.1 | reading tier |
| add | `#A3BE8C` | *unchanged* | 92° | 28% | **64.7** | signal |
| **del** | `#BF616A` | **`#C67173`** | 354→359° | 42→43% | **56.5 → 61.0** | signal — was the outlier |
| bad | `#D08770` | *unchanged* | 14° | 51% | **62.7** | signal |
| ask | `#C08FE8` | *unchanged* | 273° | 66% | **73.5** | signal |
| warn | `#EBCB8B` | *unchanged* | 40° | 71% | **73.3** | signal |
| **data** | — | **`#88C0D0`** *(new)* | 193° | 43% | **67.5** | signal — the payload rule's datum hue |
| violet | `#8F6FA8` | *unchanged* | 274° | 25% | 54.7 | the shell operator's, shared by both ladders |

Signal spread: **19.4 points before, 14.9 after** (unchanged by `data`, which sits mid-band).

### Light ladder

Byte-identical throughout — it was measured and it was already in band.

| role | value | H | S | L | in band? |
| --- | --- | --- | --- | --- | --- |
| ink | `#3B4252` | 222° | 16% | 27.6 | reading tier |
| accent | `#5E81AC` | 213° | 32% | **52.2** | signal |
| muted | `#8098B8` | 214° | 28% | 61.2 | reading tier |
| dim | `#9AA3B2` | 218° | 14% | 65.1 | reading tier |
| add | `#7BA23F` | 84° | 44% | **44.1** | signal |
| del | `#B55B64` | 354° | 38% | **53.3** | signal — band top |
| bad | `#C57A3C` | 27° | 54% | **50.4** | signal |
| ask | `#6F3FA8` | 267° | 46% | **45.3** | signal |
| warn | `#A6791F` | 40° | 69% | **38.6** | signal — band floor |
| **data** | **`#2C8A9E`** *(new)* | 191° | 56% | **39.6** | signal — the datum hue, deepened for the page |

Signal spread: **14.7 points** (unchanged by `data`).

### The one hue that moved, and why

`#BF616A` is nord's own red and it carried a diff's minus lines for four waves.
At L 56.5 it sat a clear five points under everything else in the signal set and
read as a *dimmer class of fact* than the plus lines beside it — which is not
what a diff means. Neither half of a diff outranks the other.

The move is as small as a move can be: **hue and saturation held, lightness
alone raised**, from L 56.5 to L 61.0. It is recognizably the same red one shade
up.

The 256-colour neighbour was re-checked, because that is where an unchecked
change silently becomes a different colour. `#C67173` resolves to **167** — a
brick red, one clear step from `bad`'s 173. The obvious alternative, L 62, lands
on **168**, which is a pink; a diff whose minus lines went pink on every
256-colour terminal would have been the fallback nobody looked at, again.

**`ask` is untouched and untouchable.** `#C08FE8` is spent on the moment the
agent is waiting for a person and on nothing else — its whole value is that
seeing it anywhere means exactly one thing. It sits at L 73.5, comfortably in
band, and nothing in this wave had a reason to go near it.

## THE ACCENT BUDGET

> **One lit element per screen.**

The accent is the loudest thing the palette can say, and its whole worth is that
a person's eye goes to it without being asked. That is a budget, not a colour.
Spend it twice and it buys nothing.

So the accent marks **the one live or chosen thing** and nothing else. Headings
are not that: a heading is furniture, it sits in the same place every time, and a
column of lit headings is a screen with no answer to "where am I". Headings, band
labels and wordmarks wear `muted` — the same hue one step back, which reads as
structure rather than as a summons. The person's own `›` glyph and the rail keep
the accent, because that is where the eye starts and where the work is.

No test can hold this. A screen is composed at fifty call sites and "how many lit
things does this frame have" is not a question a unit test can ask. It is held by
review, and by being written down in two places on purpose — here and at the head
of `styles.go`.

## THE PAYLOAD RULE

> **A line may be quiet; the fact it carries may not be.**

The accent budget above says what a screen may LIGHT. This says what a quiet
line owes the person reading it, and the two are the same argument from
opposite ends.

This surface says a great deal on its own account — a note, a hint, a legend, an
announcement — and every word of it was written in the reading tiers, because
none of it is the conversation. That is right about the LINE and it was wrong
about what the line is for. `crew → balanced · brain kimi-k3:low · hands
deepseek-v4-flash · checks qwen3.8-27b` was one flat dim run from end to end:
the words a person already knew, and the four model ids they typed the command
to learn, at exactly the same weight. The sentence was legible and the ANSWER
inside it was not.

So the prose of an informational line stays where it is, and **each load-bearing
datum inside it steps up one role.**

| what | role |
| --- | --- |
| the prose | `dim`, or whatever quiet tier the surface already used |
| a datum | `data` (`#88C0D0` dark / `#2C8A9E` light) — a model id, a figure, a count, a name, a key chord |
| something typeable | the chip a slash command already wears |
| — | **never `accent`** |

**Why a hue and not a rung.** The rule's first draft lifted a datum to `ink`,
and it failed in the field the day it shipped: `ink` is the body's colour, so a
"lifted" model id two rows under a paragraph of body text read as ordinary
prose — the exact defect the rule was written against, arriving one rung later.
The eye reads lightness as *loudness* and hue as *identity*, and a datum is a
different **kind** of thing, not a louder one. So data wear a hue of their own —
the syntax-highlighting contract every calm terminal theme keeps (greyscale for
prose, colour for identifiers) — sitting inside the fifteen-point signal band so
a line full of data still reads as one quiet field until somebody looks.
`#88C0D0` resolves to xterm-256 **110**, one clear step from the accent's 146;
`#2C8A9E` to **31**, colliding with nothing on the light ladder. The key–value
legends (a card's `enter open · ctrl+t new chat here`, the foot hints, the task
card's keys) are written in the hint grammar and painted by the same mechanism,
so every key/verb pair splits the same way everywhere.

**The chip is the slash command's mark and is not lent out.** A key chord is
typeable too, and the obvious move was to give the chords in the legend and the
key sheet the same lifted run of cells. They do not get it. A chip has meant
exactly one thing on this surface since it existed — *this word is a command
this surface runs* — and a second kind of thing wearing it is a mark that has to
be read twice to learn which one it is. A chord steps to `data` instead, which
is the same one-step move every other datum makes and costs the budget nothing.

**Never the accent, and the budget is why.** A note appears, is read, and
scrolls away. The accent marks the one live or chosen thing on a screen; a
passing line is not it, and a surface that spent the accent on every fact it
mentioned would have spent the budget forty times a minute.

**Strategy over decoration.** ONE LINE CARRIES ONE OR TWO DATA. If everything in
a line is bright then nothing in it is, and a rule that lifted every noun would
have bought back the flat line it started from. So the hint slot lifts the key
and never the verb beside it; `/status` lifts the figure and never its label;
the crew line lifts the three ids and leaves `crew →`, the preset word the
person has just typed, and the three role words where they were — those are the
question, and the ids are the answer.

**A datum is named, never guessed.** `internal/tui3/payload.go` holds the whole
mechanism: a note's builder names the words that are the answer, in the order
they appear, and the painter finds them on word boundaries. Nothing recognizes a
shape, so a line whose builder says nothing is drawn exactly as it was before
the rule existed. The one exception is the legend's hint slot, which is written
in a grammar tight enough to read — `chord verb · chord verb`, spelled the same
way at twenty-five call sites — and is therefore read rather than made to carry
a list of its own keys at each of them.

## DEPTH FADE OVER ZEBRA

Alternating row backgrounds are **banned**. A striped list is a list that has to
be read to learn nothing: the stripes carry no information, they are simply
noise arranged regularly enough that the eye stops noticing it, and they fight
every ground the ladder above draws.

Where a long list needs help being scanned, the answer is a **fade with depth** —
rows nearest the top at full ink, rows further down receding toward the
background. That is the same mechanism the thinking window already uses for its
three lines of live reasoning, generalised: a gradient tells you which end is
which, where a stripe tells you only that rows exist.

## COLOUR IS STROKE, NEVER FILL

Restated from the existing law, because it is the one most easily lost.

Saturated colour on this surface touches **glyph strokes and single characters**.
The ground is enormous and quiet; the colour is hairline. A task's identity hue
paints exactly one cell — the glyph at the head of its row — and the title beside
it keeps the ordinary ink. A needs-you row's violet is on the `▲` and not on the
sentence.

**The glyph carries the hue; the text stays calm.** A whole row in a status
colour is a row that shouts, and a screen of shouting rows is a screen with no
priority at all. The grounds in THE GROUND LADDER are the sole exception, and
they are exactly as quiet as the table above says.

## PRESENCE OVER LABELS

Also restated, because it is the emptiness law seen from the design side.

A thing with nothing to say occupies **zero pixels** — not a greyed-out
placeholder, not `0 tok`, not `$0.00`, not an empty heading. Unknown renders as
nothing. A status element appears because it has something to report, and its
appearing *is* the report.

And when it does appear, it appears as **a shape before a number**. A spinner
says a call is running; the elapsed seconds are for when you want them. The one
deliberate exception is the live status line, which keeps `$0.00` so its segments
do not jump sideways as they update.

## WHAT WE REFUSED

An audit is only worth as much as the parts of it you decline.

**Borders.** Omarchy's surfaces are separated by hairline borders and small equal
gaps — a subdivided plane. Aforge is **one continuous surface**: zones are made
of typography, alignment and at most a hairline rule. A border is a claim that
two regions are different kinds of thing, and on a conversation surface they
mostly are not. This is also why the emphasis law forbids adding a ring: rings
are borders arriving one row at a time.

**Icon-only minimalism.** Their status bar renders four glyphs and no digits, and
the most consistent complaint about it — from people who otherwise admire the
work — is that it reads as empty, and that you cannot tell an unfamiliar glyph
from a decorative one. That is the best-substantiated criticism in the whole
audit and we take it seriously. **Every mark on this surface has a word near it**:
the mark says it fast, the word says it certainly.

**Keyboard speed at discoverability's expense.** This is the important one, and
it is a straight refusal rather than a trade we tuned differently.

Omarchy accepts a steep learning curve as the price of keyboard speed — *"It's OK
that it'll take longer to learn. It's OK to have a manual."* That works for a
system whose users chose it precisely for that. **Aforge is a new interaction
paradigm**, and nobody has intuitions about it yet. A person cannot be expected
to learn what they cannot see.

So: the keyboard stays first-class, and **every chord keeps a visible, clickable,
self-teaching door beside it**. If a key does something, a person who never
pressed it can still find that something, click it, and learn the key from what
they clicked. There is no gesture on this surface whose only documentation is
documentation.

## THE SOURCE, CREDITED HONESTLY

This language was written after a close audit of
[basecamp/omarchy](https://github.com/basecamp/omarchy) — its configuration
tree, its nineteen shipped themes, the explicit design-token file its 4.x rewrite
added, and the public reception of both. Where a number here matches a number
there, that is not coincidence and it is not hidden.

What we took is the **arithmetic and the reasoning**: that a selection tint at
1.15:1 is enough when the row already says what it is; that a wide spread of hues
at one lightness reads calm while the same hues at scattered lightnesses flicker;
that a state ladder should be four named steps rather than an ad-hoc set; that
emphasis is done by raising the ground rather than by adding an outline; that
consistency is best enforced by refusing to let anything else own a value.

What we did **not** take is any value transplanted by analogy. Their ladder is
alpha over a background they know; ours is authored, because we do not know
ours — and every number in the tables above was measured against aforge's own
assumed grounds, aforge's own ink, and aforge's own xterm-256 fallback, which is
a rung their compositor does not have. Two of our steps ended up on values this
file already held, one hue moved five points, and everything else stayed where it
was. A design language you adopt wholesale is a costume; the parts worth keeping
are the ones that survive being re-derived in your own material.
