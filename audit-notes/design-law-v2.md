# Design law — the 2026-08-11 grooming session (canonical for wave 2)

Decisions made live with the user, superseding earlier drafts where they
conflict. Wave-2 lanes build against THIS file plus `interaction-map` diagrams
in the session log. Vocabulary rule for the file itself: plain words — chat,
sidebar, bottom bar, record, card. No invented terms.

## 1. Product stance

A colleague, not a console. The chat carries exactly three sentence classes —
COMMITMENT, DELIVERY, QUESTION. Everything else (worker lifecycle, "task
started/changed", standards-setting, progress narration) is the work record's
business, one click away, never a chat message. "While you were away" survives
as ONE briefing card (delivered / practiced / waiting-on-you), each row
expandable — never a dump.

## 2. One mouth

The main thread is the only composer; typing is the rest state and the
composer holds the keyboard by default. Task records are readable and
steerable (verbs as clicks), never a second composer. Speaking to a task is
`@task` from the one composer. Rooms are not minted implicitly: launch resumes
the last thread; empty rooms are reused/reaped. "untitled room" as a concept
dies with the naming scribe + resume-by-default.

## 3. The job block (chat side)

One block per job, at the position of the ask, evolving IN PLACE:

- committing (user-amended 2026-08-11): the card's SKELETON, drawn the frame the
  commissioning row is journaled — the head's own reading as its provisional
  title, `creating task…` with the breathe, no receipt and no subtree because
  neither is knowable yet. It is the same block, id and dress the named card
  wears, so the graph catching up changes the title and nothing else. It fills
  the gap the reader timed: "once it decides we are creating task … there is
  like a few seconds where nothing happens".
- committed/planning: `◇ <title>` + `planning…` with a slow dim→bright breathe.
- running: v1 connector tree (`├─`, `╰─`, `│` guides), one row per node:
  state glyph · name · right-aligned `<elapsed> · $<cost>`; queued rows say
  `waits: <deps>`. Verbs `steer · cancel` on the block. NO "N workers"
  vocabulary anywhere in the block — branches show multiplicity; counts
  (`2◐ 2✓`) appear only in collapsed/sidebar/HUD forms, and NEVER fractions
  (a dynamic graph cannot promise a denominator).
- delivered: the block becomes the delivery card in place.

The whole block is a click target to the record, from the moment it appears —
a click anywhere on the card that is not a narrower door (its `view more` row,
its copy chip, an option row) opens that task's page.

Under each RUNNING part row, one dim ellipsized line: the latest human line
from that part's flight recorder, updating as the recorder grows (user-amended
2026-08-11: "no 1 line update as its running"). It is the same reading the
record page's tree draws, read once and consumed twice, and it is not a fourth
motion — what moves is the work, and the part's own spinner carries it. The
read is armed only while the thread is the visible lens and a card is live.

MOTION IS SELF-SUSTAINING OR IT IS NOTHING. The animation chain must arm the
moment something becomes live WITHOUT any input — a job card arrives on a
POLL, not on a keystroke — re-arm itself on every tick while anything moves,
and disarm when nothing does. Liveness on a card is three things, not one: a
measured clock, the phase breathe (a skeleton card has nothing else), and a
running part's spinner. Asking only about the clock is what produced "animation
seems to happen only when I hover on something".

## 3b. Transcript voices and copy

NO speaker header rows — naming both parties of a two-party conversation
every turn is ceremony (§15). The assistant is the UNMARKED voice: plain,
full-width, primary tier, no label, no glyph — most ink, least chrome. The
user's words are dim, indented, `›` in the gutter — theirs because they are
the quiet short ones. Name CHIPS exist only for guest voices: work speaking
under its job's name wears a small dim chip. Long user messages collapse to
3 lines with `▸ N lines`. Copy is a layer: hover any message → dim `copy`
chip at its right edge → OSC 52 clipboard write; the two J18 copy doors
stay; the `?` sheet teaches shift+drag native selection in one line.

## 4. The delivery card

Card padding (user-amended 2026-08-11): a card's ground carries air on all four
sides — one row as it opens and one before it closes, the accent lane and a
space on the left, and one cell of ground kept clear to the right of the
longest row, so a receipt never ends at the plane's own corner. A card's ONE
internal seam is a short dim hairline INSET from both edges on the card's own
ground, never a full-measure row in a second rung: a band the width of the card
reads as a bar of shadow across it ("not a big fan of this thick black line
separating"), and a rung shift vanishes entirely at 16 colours and NoColor
where the inset rule still reads.

The ONLY element with a distinct ground + `▎` accent left edge — that
treatment means "a finished answer" and nothing else may wear it. Anatomy:
state glyph + title + right-aligned receipt (`<elapsed> · $<cost>`); the
findings themselves (3–5 sentences the assistant absorbed — never "see the
file"); artifact paths (clickable); verb row (`open · copy · rerun · ask about
this`). Failed variant: coral edge + why + `retry · see trace`. Same template
at every scale (sub-result, main answer, briefing rows).

## 5. The record (entered job)

THE RECORD IS CHRONOLOGICAL (user-amended 2026-08-11). It reads in the order
the work happened, top to bottom: the PROMPT CARD first (the job's name, its
full receipt, and the head's reading in its zones — goal lead, verbatim
request quoted, `Assumed:` bullets — on the commitment ground), then the WORK
in the middle, then the FINAL RESULT at the bottom on its own delivery card
(§4's dress: ground + `▎` edge, the answer's lead line bright, artifacts
clickable, coral + reason for a failure). This supersedes the "delivery card
on top" order that stood here: answer-first is paid back by the SCROLL
POSITION instead, and that trade is the whole amendment — entering a SETTLED
task opens the page scrolled to its result card, entering a RUNNING one opens
it at the live tail. Manual scrolling is never fought afterwards (nothing
sticky), and the anchor is spent once per entry.

The middle has two forms and the job's own shape picks, never a label (§15).
An ATOMIC job (one worker) shows the execution trace as described below. A
GRAPH job (row-0 with parts) shows the LIVING TREE full-page — one row per
part on §20's grid with v1 connector guides (`├─ └─ │`, two cells per level,
so a state glyph lands on the ladder), each row carrying `state glyph · name ·
dim receipt` (model word · `~N tok` · `$` · elapsed, the elapsed TICKING on a
running row), plus, under each RUNNING LEAF, one dim ellipsized line: the
latest human line from that part's flight recorder, changing as the recorder
grows. That changing text is not a fourth motion — §11 still permits exactly
three, and the parent row's spinner is what carries the movement. Clicking a
BRANCH row folds its subtree (§10's whole-line door, `▸`/`▾` witness, count in
parts); clicking an ATOMIC LEAF drills into that worker's own record page —
the same page one level down — and esc walks back up one level with each
level's scroll position restored. The row-0 worker's OWN recorder still draws
under the tree, behind the seam, because it is what this record's subject did
as opposed to what its parts did.

EXACTLY THREE DRESSED ELEMENTS PER RECORD PAGE (user-amended 2026-08-11): one
prompt card, one living tree (or one trace), one result card. Nothing else on
the page wears a card. A planner's status progression — "reading the request",
"exploring approaches · 1 of 3", … — is ONE updating progress line between the
prompt card and the body, newest wins, replacing in place: those rows are
delivery-SHAPED by columns, so a page with no coalescer drew one full card per
status, each re-printing the same title, receipt and subtree ("our goal is not
to have multiple cards for the task — the tree is the main thing"). Machinery
rows never take the card dress on any surface. The reader's own steers, open
questions and learning moments are speech and keep their rows (§1).

The record's prompt card opens ~15 rows of the ask before its fold, where the
conversation's card keeps its one-line preview: "the top card can have a lot
more lines before view more as tasks are pretty large".

Then the `── execution ──` seam, immediately above the first execution row and
nowhere else, then the trace. Four voices with fixed glyph + tier: `✳` model thought (dim), `$ ⌕ ✎`
tool calls (glyph accent, command primary, result size + fold on the row),
`›` user steers (bright), output dim behind a `│` gutter when expanded.
Legend taught once inside the scroll. NO turn headers and NO token counts on
rows — a blank line IS the turn boundary (the rhythm shows what a label
would spell out; "turn" is machinery vocabulary, §14). Rows within a turn
are adjacent lines; hairlines only at the delivery/execution seam. Sidebar
re-scopes to the job's tree while inside; esc returns.

Record header law: the header SCROLLS with the document — nothing sticky;
the record is one list from its first row. The header carries the full
receipt: `elapsed · $cost · Nk tok` plus the models that did the work
(deduped, dim), each figure absent when unknowable. Work that ran on a
specialist harness wears a dim chip naming the specialist (`swe`); the
general harness is unmarked — a default never wears a badge.

Tool-row law: NO success ticks (success is the default; only `✕` + reason
marks). Consecutive same-tool calls BATCH into one row (`⌕ searched 9 ·
q1 · q2 +7 · 52KB ▸`) that expands to its members. Every tool has ONE
salient input in a standardized table (search→query, sh→command,
write→path, fetch→domain); raw JSON never renders. Expanded output is a
bounded scrollable box (~12 rows, │ gutter), never a dump. Salient input
primary, glyph accent; shell commands syntax-highlighted at the dim end of
the pastel ramp. The model's narration must read as the loudest voice in the
record; tools are the quiet machinery under it.

Row receipts (user-amended 2026-08-11): the fold hint rides INLINE,
immediately after the truncated input — `$ sed -n '80,200p'… ▸ 2 lines` —
so the door sits where the eye already is, not across a gutter of empty
cells. The right edge carries the result's size as HUMAN TOKENS, marked as
the estimate it is (`~1.5k tok`, bytes/4, the ~ is the honesty), never raw
KB; per-call dollars are unknowable (calls share a turn's bill) and are not
faked.

## 6. Sidebar

Right by default, configurable (`sidebar: right | left | hidden`); hidden
compresses to the one-line dock (`◐ 2 working · 1 question · $0.31 today`).
Sections by faint lowercase words: `working` (live, floats, full ink),
`recent` (settled, dim, ~5), `▸ history (N)` (one expandable row — the
hundreds answer). Live floats / settled sinks / settled dims; stable order
while visible (7.2). Previewing a card never moves other rows (reserve
mechanism, landed). Homes rows leave the sidebar; access moves to the bottom
bar tabs.

## 7. Bottom bar

Composer + ONE contextual line. Left: places — `chat · work · notebook` as
clickable tabs, current bright, `tab` cycles when the draft is empty. Middle:
only the keys live RIGHT NOW (`esc interrupt` while streaming, `1-3 answer`
when a question is open; empty otherwise). Right: standing facts
(`<model> · $<today> today`). Breadcrumb elision keeps the CURRENT location,
drops ancestors first.

## 8. Composer

Distinct accent prompt glyph; blinking accent block cursor (§11); ghost
text is STATE-DRIVEN — the composer's empty line always says what is true
and doable right now (idle: `ask for anything · @ jobs · / commands`;
running: `keep typing — messages queue · esc interrupts`; delivery just
landed: `ask about the results · r rerun`; question open: `answer 1–3, or
say it in words`; settled room: `this work is settled — ask about it`) —
vanishing on first keystroke; multiline drafts grow upward; the streaming
indicator lives at the prompt, not as chat rows. ALL composer-anchored
popups (`@`, `/`, completions) open UPWARD.
The `/` catalog lists each action once under one canonical verb — aliases
resolve if typed but never appear as twins.

## 9. Palette

`ctrl+space` summons (ctrl+k alias kept). Fuzzy across everything: actions,
live work, rooms, history (grouped, dim), settings. Clipped lists end with
the hidden count. Enter routes through the same open-a-room path the sidebar
uses.

## 10. Expand law (every chevron surface)

The whole line is the door. Click opens; click on the line again — or
anywhere inside the expanded region — closes. `▸`/`▾` is the state's witness,
not the only target. Applies to trace rows, tree nodes, history, briefing
rows, tables.

## 11. Motion law

Exactly three motions product-wide: the braille spinner on running rows,
the slow breathe on `planning…`, and the composer's blinking cursor (the
terminal-native "you can type here" heartbeat — request via DECSCUSR,
restore the user's cursor on exit). Nothing else animates. Numbers tick;
cards appear; nothing slides or shimmers beyond these three.

## 12. Color and affordance law

Bright = content. Dim = receipts/meta. THE accent = clickable, and nothing
inert ever wears it. Glyphs carry only state (`○ ◇ ⠸ ✓ ✗`), kind
(`✳ $ ⌕ ✎ ›`), or navigation (`▸ ▾ ‹`) — one per row, fixed column, plain-tier
twin for every nerd-font glyph. Identity pastels never colour text (gate
landed). Discoverability is ambient: hierarchy + the contextual line +
hover promotion — never tutorials.

## 13. Time and money

One relative formatter everywhere (`now · 12s · 5m · 2h · yesterday`), aging
live; absolute only inside expanded records. Every tree row carries its own
`$ · elapsed`; collapsed parents carry the rollup (store.SubtreeReceipts,
landed — absent renders `—`, never `$0.00`; wall-clock, not sum). Day total
lives in the bottom bar.

## 14. Vocabulary

User-facing words only: a job, its parts, the plan, the record. BANNED on
every surface: task/node/atomic/seq/journal/fold and worker-count phrasing.
A single-part job says nothing about its shape.

## 15. Structure is never labeled

Labels never do structure's job. Grouping, sequence and belonging are shown
by position, indentation, spacing and tier — never by a header that names a
mechanism (`turn 3`, `seq`, part numbers) and never by counting things whose
multiplicity is already visible. The test before any label ships: delete it —
if the reader still knows what they are looking at from where it sits and how
it is inked, the label was clutter; if they do not, fix the spacing before
reaching for the label. At most one faint lowercase word may announce a
section (`working`, `recent`, `history`) and only where position alone is
genuinely ambiguous. Blank lines are boundaries; adjacency is belonging;
indent is descent. This law outranks any per-surface convenience.

## 16. The minute details (the polish standard every surface meets)

- RIGHT EDGE IS A COLUMN: receipts (`elapsed · $`) right-align to one shared
  column per surface, so scanning down reads like a table without being one.
- ONE ELLIPSIS GRAMMAR: text truncates with `…` at the end; paths middle-cut
  (`8812-vec…ison.md`); the current location in a trail middle-cuts last
  (landed). Never two dots, never `...`.
- MONEY: through the tokens rungs only (sub-cent rung exists); `$0.31`, never
  `$0.310`; absent is absent, never `$0.00`.
- GLYPH DISCIPLINE: every glyph comes from the 5.17/12.7 table, means exactly
  one thing product-wide, sits in a fixed column, and has a plain-tier twin.
  Nerd-font picks must read at one cell — no wide/ambiguous codepoints; when
  in doubt the plain twin wins. New glyphs need a table entry + parity golden
  update, never an inline literal.
- BORDERS: the delivery card's ground + `▎` edge is the only "border" in the
  product; dialogs get the chrome ring (dialogchrome); everything else
  separates by whitespace and faint words. No box-drawing rectangles.
- RULED LINES: exactly two legal uses — the dialog chrome ring, and the
  word-in-line seam (`── execution ──`: the label IS the rule, one row that
  separates and names at once). Any other hairline is the school-notebook
  smell; differentiate with ground shifts, spacing and tier instead.
- SURFACE SEAMS ARE GROUNDS, NOT STROKES: the composer strip carries its own
  subtle ground rectangle so scrolling content visibly slides beneath a
  distinct surface; same for any fixed chrome a scroll passes under.
- THE VERB·KEY CHIP: one renderer, every surface. Verb first at the accent
  (the whole chip is the click target), its key after it, dim, one tier
  down (`close esc`, `open ⏎`). Never a bare key beside a verb as two
  unmarked words; never a verb without its key when one exists. Fed by the
  registry so surfaces cannot drift.
- OVERLAY DISMISSAL: every overlay closes three ways — its `close esc` chip,
  the esc key, and a click anywhere outside the panel (dialogchrome owns
  "outside").
- PADDING RHYTHM: 2-cell shared left edge; one blank line between blocks;
  cards carry one leading/trailing blank inside their ground; group words get
  a blank above, none below (the word belongs to what follows).
- DIM RAMP: exactly three text tiers in use on any one surface (bright /
  normal / dim). A fourth tier on one screen is a bug.
- HOVER: pointer rest promotes the row one tier (never a band); every click
  target hovers; nothing that hovers is inert.
- ALIGNMENT: columns within a section share x-positions across rows —
  glyph column, name column, receipt column. A row that breaks the grid
  needs a reason written down.
- EMPTINESS: an absent value renders as absence (blank cell or `—` where a
  column would collapse), never zero, never a guess (8.2.20).
- CASE: all-lowercase for chrome words (group headers, verbs, hints);
  content keeps its own case. No Title Case chrome anywhere.

## 17. Rendering stack

Markdown: goldmark AST rendered through our blocks (never glamour). Code:
chroma with OUR pastel style declared in tokens, honest degradation
(truecolor pastels → 256 approx → uncoloured at 16/none). Tables fit or
scroll; never wrap to soup. One spacing table for all block kinds: blank
line above/below blocks, 2-cell shared left edge, code on subtle ground with
dim gutter.

## 18. Motion and colour keyframes

§11 says what may move and §12 says what colour means. This section is the
numbers: every frame, every period, every hex, so a motion or a hue is a thing
that can be reviewed rather than a thing that was typed. Authority is
`internal/tui2/tokens` (motion.go, palette.go, state.go, glyph.go,
breakpoints.go); `internal/tui2/blocks/clock.go` spells the cadence twice
because the edge runs tokens → blocks, and tokens/motion_test.go pins the two
equal.

### 18.1 The one cadence

`MotionInterval = 120ms` (`blocks.DefaultInterval`, twin). Every animated cell
in the product advances on this grid; no surface picks its own.

The band is `100ms..150ms` and both ends are measured, not tasteful. Below
~100ms a ten-frame braille cycle stops reading as rotation and starts reading
as noise — the eye cannot resolve ten frames a second — and every step is a
repaint of every live row, so the cost is wakeups nobody can see. Above ~150ms
the same cycle visibly steps: each frame is long enough to be read as a
separate glyph rather than as one turning thing.

The grid is also what makes the phase-lock work. `Clock.Frame` is
`floor(now/interval) % frames`, so two rows given the same latched instant show
the same frame, and a repaint landing inside one step produces byte-identical
rows and therefore zero dirty rows (8.1.3). A second interval anywhere breaks
both at once: rows drift apart AND the shell wakes on two schedules.

### 18.2 The motions (exactly three, §11)

| motion | frames | period | ease | where it is legal |
|---|---|---|---|---|
| spinner | `⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏` (10) | 1.2s = 10 × 120ms | 0 (flat) | transient tool rows, and live JOB rows on a list (board `working` rows, live subtree twigs — user-amended 2026-08-11: "when things are working we need some kind of animation"). Never a durable OBJECT — a service, a rail room card at rest, a settled anything: a dancing glyph on a long-lived object is a lie about liveness. The distinction is work-in-flight vs thing-that-exists. |
| breathe | `· • ● •` (4 frames, 3 sizes) | 1.44s = 12 × 120ms | 0.6 | the thinking line (`planning…`) while a model is working and has produced nothing yet. One per surface. |
| caret | — (terminal's own, DECSCUSR) | terminal's | — | the composer. We request the shape and restore the user's cursor on exit; we never draw a blinking cell ourselves. |

Every period is a whole number of house steps, or the motion cannot be
phase-locked. Both driven periods sit inside the calm band `0.8s..2.0s`:
faster reads as urgency, which is amber's job and not a glyph's; slower stops
answering "is this alive?" inside the glance that asked.

WIDTH STABILITY IS PART OF THE KEYFRAME. Every frame of every motion measures
one cell under both shipping rulers AND agrees with its siblings about
East_Asian_Width, so a set that is one cell for us is one cell for all of us
and two cells for all of a CJK-locale terminal. A half-ambiguous set shifts
everything to its right mid-animation — which is exactly why 5.21's proposed
`◐◓◑◒` spinner is not shipped (`◐ ◑` are Ambiguous, `◓ ◒` are not) and why the
house spinner is braille. The breathe's three dots are Ambiguous *together*.

The breathe is a SIZE RAMP on one shape, walked up and back down, not four
different marks — four distinct glyphs would be a second spinner, and §11
permits one. Its small and large dots are bytes the vocabulary already owns
(`·` is the separator and the prose bullet, `●` is the step-done dot). The
collision is deliberate and named: a slot is a meaning, not a byte, and these
three are the honest small/medium/large dots in a repertoire every terminal
has. All three are walked by the width gate under their own names (Pulse0/1/2).

`blocks.Shimmer` (24 cells/sec, 8-cell band, fixed velocity) is NOT one of the
three and has no caller on any v2 surface. It stays because it is the corrected
form of a defect the legacy TUI still ships. Wiring it to a v2 surface is a
change to §11, not a wiring change.

### 18.3 The semantic colour table

Five words and no more. Each row states the LAW — what the colour is allowed to
mean — beside its value at each profile tier. The dimmed column is the
unfocused-pane variant (`Mix(base, ground, 0.45)`), and every value below
clears its contrast gate against every ground it is legal on (§18.5).

| token | truecolor | 256 | 16 | dimmed | THE LAW |
|---|---|---|---|---|---|
| amber | `#EECE96` | 222 | bright yellow | `#8B795E` | **A HUMAN IS NEEDED. Only ever this.** Question badges, waiting-on-you states, the answerable-option keys, the consent dialog's ask, and the ctx gauge past its warn point (§18.4, the one named exception). Not a warning, not emphasis, not "note this". A cut turn is deliberately not amber — a cut is not a question, and the head re-produces rather than standing there asking (12.5.3). |
| cyan | `#A4D7EA` | 152 | bright cyan | `#627E8C` | **ALIVE.** Working glyphs, the spinner, the stream caret, the thinking breathe, a boost in flight, the composer prompt while it holds the keyboard (ready is the resting form of alive). Never on an inert or settled record. |
| green | `#A2E2BC` | 151 | bright green | `#618473` | **MONEY AND SUCCESS.** Cost figures, spend-today, the settled `✓`, a delivered artifact. Not "selected", not "added". |
| coral | `#EFA99F` | 217 | bright red | `#8C6563` | **BROKEN.** Failures, cancels, a length-cap or stream-drop cut. NOT a user-intended interrupt — that is chrome, because painting someone's own `esc` coral is the surface scolding them for using it. |
| identity 0–7 | `#DDE6B3` `#BDE6B3` `#B3E6DD` `#BDC7E5` `#C9BDE5` `#DBB3E6` `#E6B3D6` `#E6B3BF` | 187 157 158 146 182 183 218 181 | collapses (see below) | — | **WHOSE.** A glyph, a `▎` rail, or a band tint — never running text, and never promoted. Hue angles 70/108/170/225/258/288/318/345, at least 20° off every semantic hue and 25° from each other, so no identity can be misread as a state. Without a seed there is no identity: draw the grey ramp, never wheel entry 0, because a lying identity is worse than none. |

The greys carry no meaning; they carry TIER (§16's DIM RAMP: three on a
surface, a fourth is a bug): `text.primary #E6E6F0` (255) speech and titles,
`text.secondary #A0A6BB` (145) status lines and receipts, `text.tertiary
#7C8296` (102) telemetry, separators, hints, and interactive chips at rest.

Composition is one function (`ResolveToken`) and three rules: a hue always wins
(a settled `✓` stays green); with no hue the state axis picks the tier (live →
cyan, settled → primary, chrome → tertiary); the secondary tier is never
reached from the state axis, because a status line is secondary for what it IS,
not for how live it is.

DEGRADATION IS DECIDED, NOT COMPUTED. At 256 the semantic hues resolve first
and CLAIM their entries; the identity wheel then takes the nearest UNCLAIMED
entry a visible step away from every claim — because a naive nearest-neighbour
walk puts identity.1 on green's entry and identity.2 on cyan's, which is not a
degradation but the vocabulary collapsing. At 16 colours identity collapses
outright (eight hues into six chromatic slots) and the shell stops drawing
identity accents rather than drawing two neighbours the same.

THE PROMPT STATE CHANNEL. The composer's prompt cell carries exactly one live
signal and it reads on the same three words: cyan while it holds the keyboard
or a reply streams (alive/ready), amber when the surface is waiting on an
answer from you, coral when a send failed. It never invents a fourth state and
never carries a hue on the draft text beside it.

### 18.4 The context gauge

One cell, five rungs, an even ladder — the cells are a linear height ramp, so a
non-linear threshold table would draw a bar that disagrees with its own height.

| cell | `▁` | `▂` | `▄` | `▆` | `█` |
|---|---|---|---|---|---|
| from | 0.0 | 0.2 | 0.4 | 0.6 | 0.8 |

`GaugeCells` and `GaugeThresholds` in tokens; `Gauge(fraction)` walks the table
and clamps at both ends — past the window still reads full, and a reading that
does not exist (NaN) reads empty rather than guessing (§16's EMPTINESS).

THE CELL SAYS HOW FULL; THE COLOUR SAYS WHETHER THAT IS A PROBLEM, and only the
colour is a judgement. The gauge and its percentage are `text.tertiary` until
the warn point and amber at or past it — the one place amber is spent on
something other than an open question, because a window about to compact is a
thing only a human can act on. The warn point is DUAL:
`min(75% of the window, 150k tokens)`, so a 1M-window model warns at 150k
rather than at 750k, where "three quarters gone" is still more headroom than
most models have in total (8.2.17). There is no third colour and no sixth cell.

### 18.5 The ground rungs (the elevation ladder)

Every plane is derived along one axis — ground → band — so the ladder is
monotone by construction rather than by hand-picked literals.

| rung | derivation | truecolor | 256 | over the ground |
|---|---|---|---|---|
| ground | authored | `#12121A` | 233 | 1.00 |
| hug.bar | `Mix(ground, band, 0.10)` | `#14141D` | 233 | 1.02 |
| hug.input | `Mix(ground, band, 0.40)` | `#1A1A24` | 234 | 1.08 |
| sheet | `Mix(ground, band, 0.45)` | `#1B1B25` | 234 | 1.09 |
| band | authored | `#262633` | 235 | 1.25 |
| band.identity.N | `Mix(band, identityN, 0.08)` | `#35353D`… | 237 | 1.47–1.53 |

The hug's input row sits lighter than its bar row (1.06 between them): the row
you type into is nearer, the row that names where you are is further back, and
neither needs a hairline to say so (§16's SURFACE SEAMS ARE GROUNDS). Both hug
rungs stay UNDER the sheet on purpose — a dialog arrived and will leave, so it
may announce itself; the hug has been there since the window opened.

CONTRAST COVERAGE IS AUTOMATIC AND MUST STAY THAT WAY. `tokens.Legal` states
the composition law over `IsSurface()` rather than over named tokens, so a new
rung is legal ground for every focused foreground the day it is declared, and
`Pairings()` — which contrast_test.go walks — picks it up with no list to
extend. `TestEverySurfaceIsGatedAsAGround` is what keeps that promise honest:
a surface no pairing ever puts text on, or a rung declared as a bare colour
outside the token table, fails the build. The hug rungs went through the gate
on arrival with no change to Legal; the tightest foreground on either of them
is `text.tertiary` at 4.51 against a 3.0 gate.

Three rules and each is a decision: a surface is never a foreground and a
foreground is never a ground; a focused foreground may sit on ANY plane; a
DIMMED foreground may sit only on the ground, because an unfocused pane draws
no band at all — it marks its selection with the dim identity rail — and a
dimmed pastel on a raised plane is the one combination here that cannot clear
its gate.

## 19. Padding law (user-amended 2026-08-11)

"Think about proper padding across all screens." The rhythm, stated once:

- Page content never touches column 0. Chrome (a strip, a trail) sits at the
  left edge; CONTENT starts at a left margin of 2 and keeps that shared edge
  down the whole page — title, body, receipts, verbs all hang from it.
- One blank line above every section word, none below it (unchanged from §16);
  one blank line between a title block and its body; one before a verb strip.
- Inside an input surface, text never touches the field's own edge: one cell
  of inner padding left and right, and the hint/streaming line hangs from the
  same inner edge as the typed text.
- A trail segment is a NAME, not a body: ellipsize any segment past ~40 cells.
  The detail page under it says the full text; the trail only points.
- Never say a thing twice on one screen because two elements each wanted it.
  If the receipt says `candidate`, the title line does not also say it.
- Sentence-length rows ellipsize at the row's measure — the full text lives in
  the detail page (or the expanded row), never in a wrapped list row.
- Dimming is a TIER, not a meaning. A state that matters (retired, candidate,
  provisional) is said in a word in the receipt; the dim tier may echo it but
  is never the only carrier.

## 20. The grid (user-directed 2026-08-11, reference: Claude Code's own layout)

One geometry for every surface. Numbers, not vibes. All values are character
cells.

```
col:  0 1 2 3 4 5 6 7 8 …
      [G ][content at E0............................  (dim receipt)]
      [  ][└ ][child at E1.......................................]
      [  ][  ][□ ][grandchild at E2..............................]
```

- **Gutter G = cols 0–1**: the block's marker (state glyph, voice glyph, `›`,
  `●`, or nothing) and one space. Chrome only — selection rails and accent
  edges may also live here. Content NEVER starts in the gutter.
- **Content edge E0 = col 2.** Every top-level title, sentence, and row name
  on every surface hangs from this one edge. The eye learns it once.
- **Indent step S = 2.** Depth n content edge: En = 2 + 2n. A child's elbow
  (`└`) or its own glyph occupies the two cells before its edge. Never three
  spaces, never one.
- **Blocks breathe, lines don't**: exactly one blank line between top-level
  blocks; zero blank lines inside a block (a parent and its children are one
  tight unit). §16's band-word rhythm (blank above a section word, none
  below) rides on top of this.
- **Receipts sit NEAR their subject**, dim, `·`-separated, one of exactly
  three placements — never a fourth:
  1. inline after a title, in parentheses: `Candidate summaries (4m · $0.36)`
     — for block titles and one-off notes;
  2. the line directly below, at the same edge — for two-line list rows
     (the board's form);
  3. a shared right column — ONLY inside dense same-shaped lists in a narrow
     pane (rail, palette), where every row has one and the column is close.
  A receipt separated from its subject by a gulf of empty cells is the defect
  the user has now flagged twice.
- **Hints ride with their subject**: dim, in parentheses, verb·key pairs
  separated by `·` — `(↓ manage · ctrl+o expand)` — attached to the end of
  the line they explain. Never a floating hint line, never far-edge.
- **Summary tails**: a list longer than its window ends with one dim tail row
  at the list's edge — `… +1 pending, 26 done` / `▸ history (18)` — never a
  silent cut.
- Voice dressing (§3b), tiers (§16), and grounds (§18) are unchanged — the
  grid fixes GEOMETRY only. Where an older section states a conflicting
  indent or spacing, THIS section wins.
