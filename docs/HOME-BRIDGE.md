# The bridge — home as a calm dashboard

*Design doc for the home redesign on the `standing/v0` line. Companion to
`docs/STANDING-ORDERS.md` and successor to `docs/home-design.md`'s layout (its
three jobs — triage, recall, the door — are unchanged and this doc reuses its
machinery). Where this doc and landed code disagree, the code wins.*

## The shape

One line on top, three columns, one strip at the bottom. No other rows, no
boxes, no borders: zones are made of typography, alignment and two hairlines.

```
aforge                                        on watch · 4 orders · $1.10 today · fri 9:41
──────────────────────────────────────────────────────────────────────────────────────────
 needs you                    aforge                         keeping an eye on
 ▲ approve schema change       ● odysseys wave 4      ⠹ 8m    ◦ tests sweep       in 2h
   hax-sdk · waiting 2h        › rename plan     yesterday    ◦ weekly review    mon 8am
 moving                       hax-sdk                        since you left
 ● port sweep     wisp · 8m    ▲ schema migration      2h     ◆ 2 tasks landed   aforge
                              ─ elsewhere ─                  today
                               af-docs · 4 more               3 chats · 5 tasks · $1.10
──────────────────────────────────────────────────────────────────────────────────────────
 › approve schema change — alter users table, add sso columns       1 yes · 2 no · 3 open
 type to start here · enter open · @ find anything · ctrl+enter ask · tab zone · m more
```

- **Left — act.** `needs you` (every blocked thing on the machine, any project,
  any kind: consent cards, task landings, firings, exchanges) and `moving`
  (every live thing: running tasks AND chats mid-turn).
- **Center — go.** Today's project-grouped conversation list, unchanged:
  homeShown rows, item rows, the elsewhere fold, `m`.
- **Right — know. THE RIGHT COLUMN IS ALWAYS A CARD:** the cursor row's card
  (today's band stack, unchanged) when the person is looking at something; the
  MACHINE'S OWN CARD when they are looking at nothing. The machine card's bands
  are `keeping an eye on` (orders and next occasions), `since you left`
  (news aggregated across projects; EVERY NEWS ROW IS A DOOR — enter jumps to
  the thing), and `today` (derived counts and spend; the pool's ceiling lives
  here and nowhere else).
- **Top — the pulse.** One line: the watch made visible. Spend today, never a
  quota fraction (the ceiling is the machine card's business).
- **Bottom — the answer strip.** The cursor's answerable card as one `›` line
  with its 1/2/3, riding the existing cross-window answers road. Cursor wins;
  the top of `needs you` speaks only when the cursor is at rest.

## The laws

- **STABLE GEOGRAPHY BEATS EMPTINESS, ON HOME ONLY.** The one-word dim zone
  labels always draw at the wide tier; only content obeys the emptiness law. A
  map that redraws itself is not a map — and an empty `needs you` is the good
  news, said with space.
- **A ROW MAY LIVE IN TWO ZONES.** A busy chat is a `moving` row and a center
  row at once, because the zones answer different questions; both rows are
  doors to the same place, and neither is a copy the other must sync with —
  both render from the one live object.
- **ONE SPINNER.** However many things move, exactly one row animates — the
  most recently active — and the rest hold a still ●. Calm, and the SSH wire
  cost of animation stays flat however busy the machine gets.
- **NEEDS-YOU ORDER IS WAITED-LONGEST FIRST**, except a consent card for
  something destructive, which pins to the top for as long as it waits.
- **HOME OPENS AT REST.** The cursor starts on no row, so the first thing a
  person sees is the machine's card — the morning glance is the default view,
  not a state you navigate to. The first key lands in `needs you`.
- **THE DOOR LAW HOLDS.** Typing anywhere is still a new conversation. Steering
  a row means entering it; the strip answers cards, it does not compose.

## Color

The palette is the closed pastel set in `internal/tui3/styles.go`, and the
bridge adds no hue to it. Differentiation comes from MAPPING MEANING, not from
new color: the ▲ needs-you glyph wears the question violet (`hueAsk`) — a
needs-you row IS the person-being-waited-on state D11 reserves violet for, and
nothing else on home may wear it; ● moving glyphs wear the spinner's own
`hueMuted` so still and animated rows agree; landed/kept clauses wear `hueAdd`;
a bound nearing its ceiling (the pool at "$4.80 of $5") wears `hueWarn` — a
bound about to matter is not a failure; `hueBad` only for a genuinely failed
thing; headings that lead and the `aforge` name wear `hueMuted`, and `hueAccent`
is spent on the one live or chosen thing on the screen and nothing else
(amended 2026-08-23: this line read "headings that lead and the `aforge` name
wear `hueAccent`" — THE ACCENT BUDGET IS ONE ELEMENT PER SCREEN and a heading is
structure, so the machine card's band headings, home's top-line name and the
welcome wordmark all stepped down to `hueMuted`); zone labels,
ages and clauses stay `hueDim` — the surface talking about itself.

THE GLYPH CARRIES THE HUE; THE TEXT STAYS CALM. A whole row in a status color
is jarring; one tinted glyph and at most one tinted leading clause beside ink
and dim text is the grammar every zone follows, and it is how a busy home
stays pastel rather than carnival.

## The width ladder

Columns → strips → stack, one system: ≥110 cols draws three columns
(attention ~28 · places widest · card ~36 — places never starves); 80–109
collapses the left column into two strips above the center list and keeps the
card pane — which is today's home plus two strips; <80 is today's narrow home;
phone stacks the strata and the machine card is the landing screen.

## What is new vs. today (the whole delta — nothing is removed)

New: the pulse line; the `needs you`/`moving` flatten; the machine card
(bandKindMachine — the band registry was built for this); the answer strip;
one-spinner; standing-orders visibility. Unchanged: center list, per-row cards
and their bands and keys, `@`, type-to-start, ctrl+enter ask-here, folds,
hover preview.

## Build contract (names the lanes code to)

- `bandKindMachine` — a fourth `bandSubject` kind; machine bands register from
  their own files like every band (`homeband_watchlist.go` = keeping an eye on,
  `homeband_sinceleft.go`, `homeband_today.go`), ordered in the existing keyed
  space. The idle right pane renders the machine subject.
- `internal/tui3/pulse.go` — the top line, derived from the same aggregates the
  machine bands read; one reader, two surfaces.
- `internal/tui3/homeattention.go` — builds `needs you` and `moving` rows from
  what home already reads (world/presence/keeper agents, item stores); no new
  data sources, a new arrangement of them.
- `internal/tui3/answerstrip.go` — the bottom `›` strip; generalizes the
  existing previewLine and answers roads, adds nothing to the engine.
- The three-column arrangement itself lands LAST, in its own lane, assembling
  the pieces above; until it lands the pieces are inert or additive (the
  machine card can ship as the idle state of today's right pane).

## Lanes and order

Wave A, parallel: `home/machine` (machine bands + pulse), `home/attention`
(zone data + rows, rendered as strips above today's list — useful standalone),
`home/strip` (answer strip). Wave B, after A merges: `home/bridge` (the
three-column layout + ladder + home-opens-at-rest + one-spinner). Manual pages
ride every lane (the manual law).
