# The design language — what this surface is allowed to look like

*The written contract for codeaf's visual surface. `internal/tui3/styles.go` is
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

### THE SECTION HOLDING THE CURSOR MARKS ITS OWN HEADING

> **The heading of the section the cursor is standing in wears a ground, and one
> heading per frame wears it.**

The ladder's steps mark a ROW, and a screen made of several regions — home's
three columns are the case that provoked this — needs the same fact one scale
up. The cursor step is deliberately faint, so a frame where every region is at
rest and one row somewhere in it is faintly lifted answers *which row* and never
*which region*. Saying it twice, at two scales, in the channel the ladder already
has, costs no new colour and no fifth step: the block and the row inside it.

**The heading takes the cursor step, not a rung of its own.** What a marked
heading says is "the cursor is in here", which is the cursor step's own sentence
read at the scale of a section. It is senior to the row it stands over by being
NEW — a heading has never worn a ground at all — rather than by being louder.
Where the selected step is unspent on a screen it is the better candidate, being
the container of the chosen thing; on home it is not, because the conversation
this terminal holds already wears it in the same column, and one step carrying
two meanings is what the refusal of a fifth step exists to prevent.

**One heading, or none.** Two marked headings would be two answers to a question
that has one. None at rest, and none while a list is being filtered — a heading
over a search result groups rows rather than naming a place a person is standing.
And it follows the KEYBOARD only: the pointer previews without selecting, so a
hover moves the card and never the mark.

**The heading's text does not move.** Headings are furniture and stay `muted` or
`dim` — THE ACCENT BUDGET below forbids lighting them, and a lit heading is the
same defect arriving from the other side. The ground alone carries the fact.

**A heading that is a door underlines under the pointer, and that is all the
pointer does to a heading.** Home's headings open the place they name, and a
person holding a mouse over one is asking whether it will; the answer is the
underline — no ink, no ground, the one attribute a terminal has always used to
say "this opens somewhere" — on the word alone, and only on a heading that
opens somewhere. It is an affordance and not a step of the ladder: it marks
nothing about where the cursor is, so the marked heading above still follows
the keyboard only (added 2026-09-17).

### THE EMPHASIS LAW

> **A row is emphasized by raising its ground and turning its leading text
> accent. Nothing else ever changes.**

No new colour arrives for the emphasized state. No run of bolding spreads across
the row. And above all **no outline is added** — emphasis is a step UP this
ladder, never a ring drawn around a thing.

A QUESTION'S FRAME IS NOT EMPHASIS (owner ruling, 2026-09-11). The dim frame a
question hangs in (WHAT WE REFUSED → Borders, amended) says "this is one object,
and it is asking you" — a claim about what the thing IS, drawn once, the same
way whatever row inside it is focused. It never lights, never thickens and never
follows the cursor. Emphasis inside it is still exactly this law: the focused
answer's ground steps up to `selected` and its leading mark turns; nothing else
changes, and no ring is ever drawn around a row.

Two moves, both already in the palette. The ground says which band of pixels is
being spoken about; the accent on the leading glyph or word says what it is. A
lane that finds itself reaching for a third move has found a state the ladder
does not have, rather than a colour the palette is missing.

### The values, and why they are authored rather than derived

The honest way to build this ladder is the compositor's way: take the foreground
colour, composite it over the background at a stated alpha, and every step
inherits the theme's own hue for free and self-inverts on a light terminal with
no light-mode branch at all. **In a constructor we cannot.** There is no variable
that states the terminal's background and no query a constructor may block on, so
at the moment the palette is built there is nothing to composite over.

**Both halves now exist, and which one you see depends on your terminal.** The
first frame asks the terminal for its background with `tea.RequestBackgroundColor`
and nothing waits for the answer; a terminal that replies gets every value on this
ladder derived against its real ground, and a terminal that stays silent keeps the
authored values below forever. The derivation is `internal/tui3/adaptive.go` and
is aimed at exactly the ratios this section already states — it changes what the
steps are measured *against*, not what they are aimed at. Where an authored value
already lands in band on the measured ground it is not touched, so most terminals
see nothing change; a pure black screen and a tinted page, the two grounds nobody
could aim at, are what move.

The steps below are therefore **authored** per ladder, dark and light, and they
are what a silent terminal paints — which is not a degraded mode, it is four
waves of aim. What is
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

The cost of authoring rather than deriving is stated rather than hidden, and it
is exactly the cost a reply pays off: a fixed ground reads one notch louder on a
blacker terminal and one notch quieter on a lighter one, and on a tinted page like
`#ECEFF4` the whole light ladder drops close to invisible. That is the price of
not knowing. It is still cheaper than a query that hangs — which is why the query
does not hang.

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

### THE GLARE LAW

> **The body may not be the brightest thing on the screen.**
> The reading tier's top rung sits between **8:1 and 11:1** against the middle
> of its assumed ground, on both ladders. `TestTheBodyInkDoesNotGlare` holds it
> there.

The signal band governs hue; this governs the one colour a person looks at for
minutes at a time. Above about 11:1 a white on a dark terminal stops being
legible and starts being a lamp — strokes halate, counters fill in, and every
quieter thing beside it reads as switched off.

The dark ink was `#D8DEE9` for five waves and measured **12.65:1** against
`#1a1b26`, half again as bright as the person's own accent at 9.26:1. So the
loudest thing on a surface whose accent budget is *one lit element per screen*
was the paragraph, and the budget bought nothing because whatever it was spent
on was outshone by the text around it.

| ink | vs `#101014` | vs `#1a1b26` | vs `#1e1e2e` | xterm-256 |
| --- | --- | --- | --- | --- |
| `#D8DEE9` | 14.05:1 | **12.65:1** | 12.14:1 | 254 |
| `#C6CDDA` | 11.88:1 | **10.70:1** | 10.27:1 | 252 |

The move is the smallest one that fixes it: **hue held at 219°**, lightness down
L 88.0 → 81.6, saturation eased 28% → 21% — a body white is the one colour here
with no identity to carry, and a tint nobody can name is a tint paid for in
contrast. The 256 neighbour was re-checked, as every change to this table owes:
`#C6CDDA` lands on **252**, claimed by nothing on either ladder. The near miss
is worth naming — the old ink's own 254 is the *light* ladder's selected ground
and 255 is its cursor step, so an ink that drifted back up a rung would share an
index with furniture.

The ladder still reads as a ladder, which is the other half of the law: ink
10.70, muted 6.67, dim 3.54 against the middle of the range. Coming down far
enough to be comfortable without landing on the second voice is the whole width
of the move, and the test asserts that gap as well as the ceiling.

**The light ladder's ink is untouched** at `#3B4252` — 10.06:1 against `#FFFFFF`
and 8.73:1 against nord's `#ECEFF4`. It was measured against the same band and
was already inside it, and a value in band is not touched. The test walks both
ladders regardless.

**And the transcript reaches the ink through a seam.** A model's markdown is
rendered by `internal/tui2/prose`, which resolves colour from
`internal/tui2/tokens`, whose body tier is brighter still — so the reply was
painted by a palette this surface does not own while every row around it wore
this one. Two whites, one screen, the louder on the thing people read most.
`tokens.Styler.WithBodyInk` is the fix: the colour authority a caller hands
prose may be asked to say the body tier in the caller's own voice. One Styler,
one answer to *which white*, and the heading ladder, the code ramp and the
raised plane under an inline span all stay prose's. The v2 surface keeps tokens'
own white — the override travels on the Styler and never touches the table.

### Dark ladder, before and after this wave

| role | before | after | H | S | L | in band? |
| --- | --- | --- | --- | --- | --- | --- |
| **ink** | `#D8DEE9` | **`#C6CDDA`** | 219° | 28→21% | **88.0 → 81.6** | reading tier — see THE GLARE LAW |
| accent | `#9DC3E6` | *unchanged* | 209° | 59% | **75.9** | signal — band top |
| muted | `#7FA6C9` | *unchanged* | 208° | 41% | 64.3 | reading tier |
| dim | `#6B7280` | *unchanged* | 220° | 9% | 46.1 | reading tier |
| add | `#A3BE8C` | *unchanged* | 92° | 28% | **64.7** | signal |
| **del** | `#BF616A` | **`#C67173`** | 354→359° | 42→43% | **56.5 → 61.0** | signal — was the outlier |
| bad | `#D08770` | *unchanged* | 14° | 51% | **62.7** | signal |
| ask | `#C08FE8` | *unchanged* | 273° | 66% | **73.5** | signal |
| warn | `#EBCB8B` | *unchanged* | 40° | 71% | **73.3** | signal |
| **data** | — | **`#91C5D4`** *(new)* | 193° | 43% | **70.0** | signal — the payload rule's datum hue |
| violet | `#8F6FA8` | *unchanged* | 274° | 25% | 54.7 | shared by both ladders; was the shell operator's until the transcript restraint greyed shell grammar — held in the table, currently unspent |

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

**`ask` was untouched and untouchable — until the owner retired it (2026-09-11).**
`#C08FE8` (light `#6F3FA8`) was spent on the moment the agent is waiting for a
person and on nothing else. What was true: it was also spent on every WORD of that
moment — the head, each answer, each key — while home, the places and the chip
said the same "waiting on you" in `warn`'s amber, so the one meaning wore two
colours. What is true now (colour pick C, COLOUR IS STROKE, NEVER FILL): the role
is gone from both ladders, `palette.ask` paints with `warn`, and a question's
amber touches its three marks (`?`, `▸`, `◆`) and nothing else. The two rows above
keep the old values as history; neither is on any ladder any more.

## THE PLACE LADDER — the same table, with three roles re-pointed

**Owner-signed, 2026-08-25, and then reversed by the owner the same day.** The first
order was "follow the exact design": home and the six places beside it — tasks,
standing, memory, spend, search, settings — took the captured design's nine hexes,
painted `#12121A` over every cell of the
frame, and carried place-scoped variants of the glare law, the isoluminant band and
the ground ladder to admit them. It was built and shipped for testing. The owner ran
it and said:

> "i want bg color and text color to be same as in inside chat please this new bg
> looks weird i think we were taking user terminal stuff or something previously"

**So a place paints from the table above and authors nothing of its own.** There is no
second palette, no second ceiling, no second band, and no page ground on any surface —
`placeSignalBand`, `placeGlareCeiling` and the place ground ladder are gone from
`internal/tui3/designlanguage_test.go`, and REST IS NOT A COLOUR is one law again.

| The design's role | What a place asks the palette for | The hue it gets |
| --- | --- | --- |
| tier 1, the subject | `p.ink`, bold where the design bolds | `hueInk` `#C6CDDA` |
| tier 2, what is true of it | `p.muted` | `hueMuted` `#7FA6C9` |
| a note about the subject | `p.narr` | `hueNarr` `#848FA6` |
| tier 3, the margin | `p.dim` | `hueDim` `#6B7280` |
| amber, **needs a human** | `p.warn` / `p.ask` | `hueWarn` `#EBCB8B` |
| cyan, **alive** | `p.accent` | `hueAccent` `#9DC3E6` |
| green, **money** | `p.money` | `hueMoney` `#90D0AA` |
| the page | — | the terminal's own background |
| the band | `p.selected` | `hueSelected`, THE GROUND LADDER's own step |

> **ONE ACCENT PER MEANING SURVIVES THE REVERSAL, BECAUSE IT IS ARITHMETIC ABOUT HOW
> MANY MEANINGS A SCREEN CARRIES rather than a claim about which hexes carry them.**
> Amber for waiting on a person, the accent for in flight, and nothing else on a place
> is in colour. Green is a unit rather than a signal. Every hierarchy step past the
> greys is made with bold, case, indent or air.

So `placeRampFrom` retires three of the conversation's roles and re-points nothing
else: the question's violet onto the amber (home said one thing in two colours — and
since 2026-09-11 the conversation says it in amber too, so that re-point is gone), the
payload cyan onto the body ink (a datum lifts by being the subject), and the finished
tick's olive onto the second voice (the `✓` already says it landed, and the one green
on a place is money). It takes the ramp the palette is ALREADY HOLDING, so adaptive.go's
measured derivation reaches the places exactly as it reaches a transcript.

What the fidelity wave landed that was never a colour all stays: the `?` and `◐` marks,
the design's feet word for word, the bold subject in the tab bar, `p.money` on every
figure in dollars, and the four laws about the plain floor, private-use glyphs, the
marks and italics.

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
| a datum | `data` (`#91C5D4` dark / `#2C8A9E` light) — a model id, a figure, a count, a name, a key chord |
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
`#91C5D4` resolves to xterm-256 **116**, one clear step from the accent's 146;
`#2C8A9E` to **31**, colliding with nothing on the light ladder. The dark value
was nord's own `#88C0D0` for four waves and it **did** collide: `#88C0D0` rounds
to **110**, which is `muted`'s index and a steel blue rather than a cyan, so on
every 256-colour terminal the datum wore the second voice's own colour. Only the
lightness moved (L 67.5 → 70.0, hue and saturation held), and
`TestNoTwoRolesShareA256Index` now walks the whole table on both ladders so the
next one fails instead of shipping.

The key–value legends (a card's `enter open · ctrl+t new chat here`, the foot
hints, the task card's keys) are written in the hint grammar and painted by the
same mechanism, so every key/verb pair splits the same way everywhere.

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
it keeps the ordinary ink. A needs-you row's amber is on the `?` and not on the
sentence.

A QUESTION WEARS AMBER ON ITS MARKS AND NOWHERE ELSE (owner ruling, 2026-09-11,
colour pick C). Until that day the conversation painted a question's head, every
answer label, every key and every separator in a violet of its own (`#C08FE8`)
while home and the places said the same "waiting on you" in amber — two hues for
one meaning, and whole rows in a status colour, which is this law broken on the
one object a person must act on. Now the question's three marks — `?` the
question, `▸` the pointer, `◆` the recommended answer — are amber, the home
page's question hue, in chat, in the question's page, on home, in the task room
and on the status line alike; every word is ordinary ink, every aside dim, the
frame's edge dim, and the focused row sits on the `selected` ground. The violet
is retired: `palette.ask` paints with the amber, and no row is ever painted in
it (a law test walks the question renderers' paints).

**The glyph carries the hue; the text stays calm.** A whole row in a status
colour is a row that shouts, and a screen of shouting rows is a screen with no
priority at all. The grounds in THE GROUND LADDER are the sole exception, and
they are exactly as quiet as the table above says.

### The current model on the message-box seam

Adopted with #1071 (merged 2026-09-21): the current model is always **bold in the data hue** on
home and conversation seams. It must be easy to find without hovering or first
changing it. The provider beside it and the surrounding telemetry keep their
existing weight; the emphasis is confined to the model name.
The effort follows a colon without a space or badge: `model (provider):effort`. Its
styling stays separate from the model. The conversation seam carries no git branch. Home starts
with those controls and puts `project: <path>` at the far right. Project paths on
home preserve their absolute root (or `~/` under the home directory) and truncate
at the right end, never at the left.


## ONE MARK PER TOKEN

> **One token wears one visible mark.**

A mark must say one thing without borrowing emphasis from another channel. An
inline code span normally wears the raised plane; when that span is a linkable
path, it drops the plane and keeps the underline that says it is a location.
The OSC 8 hyperlink stays because it occupies no cells and is not a visible
mark. Non-path inline code keeps its plane and never gains an underline, while
fenced code blocks are unchanged.

Stacking plane, underline and brighter ink made plumbing paths louder than the
facts around them. The rule keeps both meanings legible while refusing to turn
their overlap into extra emphasis.

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

**Where a label does hold ground over nothing, it teaches.** Home's panels are
the one place on this surface that keep a heading with no rows under it — a map
that redraws itself is not a map — and each carries one dim line naming what
arrives there and the one thing that puts it there (the copy of record is
docs/design/home-mission-control/DESIGN.md §4). That line says what the region
is FOR; it never says that the region is empty. `no tasks yet` and every
sentence like it are the emptiness law inverted into words and they stay
banned. The exception is the label, not the announcement.

## FOCUS WAKES AT THE CENTER OF MASS

> **The first movement key lands the cursor in the region the layout itself
> declares primary.**

A screen with several regions has already said which one is the main thing,
before a person touches a key: it said it with width, with density, and with
where on the frame it put each region. Focus is the fourth signal, and its only
job is to agree with the other three. A layout that says *this is the main
thing* with three hundred cells and *but start over here* with the one cursor on
it has spent its whole hierarchy arguing with itself, and a person cannot obey
both.

**The landing is fixed, and never a function of machine state.** Whatever the
flanks happen to hold this morning, the first key goes to the same place — the
whole return on a landing is that it becomes a habit, and a landing that moves
with what is running is one nobody can learn. This is the rule that bites: home
used to wake in `needs you` when something was waiting and in the list when
nothing was, which is two screens wearing one set of keys.

**The flanks are reached by pointing at them, or by a named key.** `←` and `→`
follow the geography, and a key with a *semantic* name — `tab next zone` — enters
the region it is named after. Neither is diminished by not being the default;
what a default buys is one less decision on the way in, and it can only be spent
once.

Reading order is not an argument. A line list built flank-first is an
implementation detail of how the screen was assembled, and letting it choose
where focus wakes is the assembly order leaking through the design.

## THE SPACING LADDER

Whitespace is this surface's border system. It is not leftover room and it is
not a kindness each block adds for itself: it says which words belong together,
where one voice ends, and where a section begins. The best existing screens
already use a short ladder; this names it so a fifty-first call site cannot
invent a fifth distance.

| step | value | what it means |
| --- | --- | --- |
| **clause** | 0 blank rows | parts of one sentence stay on one line, divided by ` · ` |
| **block** | 1 blank row | related blocks, bands in one card, or machinery within one speaker |
| **boundary** | 1 blank row, or a hairline with 1 row of clearance | a person's next turn, a section change, or the seam above a foot |
| **breath** | 2 blank rows | only the tall-window breathing room above the composer; it steps down to one and then zero as height is taken away |

**A GAP ASKED FOR TWICE IS STILL ONE GAP.** The conversation compositor owns
its block rows for exactly this reason, and every screen builder joining optional
blocks owes the same idempotence. Blank rows do not open or close a block. Two
consecutive blank rows are not emphasis; outside the named breathing rung they
are drift. On phone pages, two empty rows may belong to a three-row tap target;
those rows are control geometry rather than separation and must share the
control's hit target.

The horizontal ladder is just as small. The conversation's machinery and every
surface note have a **2-cell lead**; the closed work rail also spends **2 cells**,
one of air and one on its handle. Home's panes and its three-column bridge use a
**4-cell gutter**, padded on every row so alignment, not a border, makes the
edge. Sheet rows may use the established **4-cell hanging indent** when a second
line belongs under a labelled first line. The message box's 1-cell inset is
input geometry, not a new content margin.

**A NEW DISTANCE NEEDS A NEW RELATIONSHIP.** Reuse the nearest named step when
the relationship is the same. A new value may be added only here and as a named
constant beside the shared geometry it governs, with the screen and width where
the existing ladder fails. A lone call site does not get to mint a rung.

## WHAT WE REFUSED

An audit is only worth as much as the parts of it you decline.

**Borders.** Omarchy's surfaces are separated by hairline borders and small equal
gaps — a subdivided plane. codeaf is **one continuous surface**: zones are made
of typography, alignment and at most a hairline rule. A border is a claim that
two regions are different kinds of thing, and on a conversation surface they
mostly are not. This is also why the emphasis law forbids adding a ring: rings
are borders arriving one row at a time.

*Amended 2026-09-11, owner ruling (question views, frame pick A):* **a question
hangs above the box in a frame.** What was true: no border anywhere, with three
dim rounded boxes that shipped anyway as argued exceptions (the `/folder`
chooser, the `ctrl+k` switcher card, the onboarding panel), each with its own
copy of the corner pieces. What is true now: a question IS a different kind of
thing from the conversation around it — it is the one object on the surface
that is waiting for the person, it is answered and then gone, and borderless it
read as more transcript with the answers somewhere inside it. So a question
hangs below the latest submitted question as ONE framed object: a rounded, dim edge; its title in
the top edge and a right-aligned aside beside it; its keys in the bottom edge;
and on a terminal refused box drawing, two plain rules and no sides. There is
exactly one frame on this surface (`internal/tui3/frame.go`), its pieces are
vocabulary slots like every other mark, and the three older boxes are drawn by
it. Everything else is still one continuous surface: a frame is for an object
that asks, or a sheet raised over the page on purpose, and never for a zone.

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
system whose users chose it precisely for that. **codeaf is a new interaction
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
ours — and every number in the tables above was measured against codeaf's own
assumed grounds, codeaf's own ink, and codeaf's own xterm-256 fallback, which is
a rung their compositor does not have. Two of our steps ended up on values this
file already held, one hue moved five points, and everything else stayed where it
was. A design language you adopt wholesale is a costume; the parts worth keeping
are the ones that survive being re-derived in your own material.

## A working logo is one line

The running-logo studies occupy a reserved nine-column slot immediately below
the latest submitted question, before the answer and tool details. A two-column
gap precedes a dim two- or three-word developer caption that changes every ten seconds. Its 28-column slot
keeps any later content aligned. A slow decoding ripple changes one letter
at a time into symbols before restoring it, with readable pauses between passes. The row scrolls with the question and
disappears on completion; it adds nothing to the input chrome. Chevrons and a
gold dot use native glyphs. Accessible and small-window views retain text.
See `docs/design/work-logo.md` for the shared geometry contract.
