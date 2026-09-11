# Spend lenses, and "used lately" in `/model`

*Date: 2026-09-11. Status: design → build. Companion to `docs/design/spending/DESIGN.md`
(the rails and the one editor) and the spend place as it stands today (one reading, one
window, three blocks).*

## The one sentence

The spend place stays a **reading** — still never an editor — and gains four **lenses**
(`rhythm`, `models`, `days`, `year`) cycled with `[` `]`; the model picker, on **every
door that opens it**, gains a `used lately` section and a dim fortnight spend chip, plus
`/model used` as a typed filter.

## What was true

- The spend place answered one shape: a fortnight window, a sparkline, *what ran it*,
  *what it was for*. Moving time was `shift+←→` / `shift+↑↓`. There was no year view, no
  day calendar, and no sortable model table.
- `enter` on a loudest-day row or a *what it was for* row opened the thing the money went
  on. A day itself was never a door into that day's rhythm.
- `/spend` opened the place and took no argument. Window presets lived only on the keys.
- `/model` ranked the catalog. Recent use was invisible; money spent on a model this
  fortnight was nowhere on the row. Filtering was free text only — there was no `used`
  word that meant "only what I have actually run".
- The Spending tab was, and remains, the **only** editor for the rails.

## What is true now

### 1. Four lenses on the spend place

The place still answers *what did it cost*. It still points at the rails; it still never
edits them. The first line stays the dim pointer:

```
today $3.42 of $500 · /budget sets the limits
```

`enter` on that line still opens the Spending tab. What changes is **which reading** sits
under that pointer.

| lens | what it answers | default arrival |
|---|---|---|
| `rhythm` | the window and its shape — sparkline, *what ran it*, *what it was for* | today's place, unchanged |
| `models` | which models ran, sortable, with In/Out, role, realized `$/1M` | — |
| `days` | a calendar of days in the window | — |
| `year` | the year as a heatmap, with streak / peak / favorite | — |

`[` and `]` cycle the four, in that order, wrapping. The active lens is named once on the
head row in the asker's word — `rhythm`, `models`, `days`, `year` — never as a tab strip
and never as a second bar of chrome. The foot names the keys this place has, including
`[ ] lenses` where the row under the cursor does not need the cells for something else.

**Still a reading.** The Spending tab remains the only editor. Nothing on any lens
writes a limit, a model binding, or a profile row.

### 2. Day → Rhythm

On the `days` lens, `enter` on a day drills into that day's `rhythm` window — the same
rhythm reading, with the window clamped to that calendar day (grain: day, length: one).
`esc` (or `[` / `]` back through) returns to `days` with the cursor on the day you left.
A quiet day draws nothing for its figure (emptiness law) and still accepts `enter`, so
the drill always lands on a named window rather than a blank refuse.

### 3. Models lens — the sortable table

One table, one row per model that has a priced line in the current window:

| column | what it is |
|---|---|
| model | the spoken slug (`claude-opus-4-1`), same spelling `/model` and the status line use |
| In / Out | tokens in and out for the window |
| role | the crew binding(s) that model holds **right now** — `execution`, `conversation`, … — or nothing when unbound |
| realized `$/1M` | what this window actually paid per million tokens on that model (not the catalog list price) |
| spend | the window's money on that model |

Sort is one column at a time; the head cells are the sort doors. Default sort is spend,
dearest first. Grouping is optional and cycles with the same keys the rest of the surface
already uses for a secondary axis where one exists: by **role**, then by **subject**
(the project / conversation family the money went on), then ungrouped. An unbound model
stays in an unbound group rather than inventing a role word.

A model with no priced line in the window is absent — not a zero row. Emptiness law.

### 4. Year lens — heatmap, Stats-lite

The year is one plane of days, density from the day's spend, **no borders**, no card
chrome, no framed legend box. Ink follows the design language: restrained, dim
telemetry. Three readings sit under the plane when they exist, and are absent when they
do not:

- **streak** — consecutive days with any priced spend, ending today (or nothing if today
  is quiet and no streak is running)
- **peak** — the dearest day in the year, named and figured once
- **favorite** — the model that took the most of the year's money, spoken slug only

No share card, no "wrapped", no social sentence. A year with nothing spent is the place's
empty reading (`every chat and task is priced here as it runs`) — not a hollow grid of
zeros.

### 5. `/model` — `used lately`, the fortnight chip, `/model used`

**Every door that opens the picker** grows the same two additions: `/model`, the status
line's model word, a task's model control, the Providers tab's **your model** row, and
the media slots that reuse the component. One picker type; no door is special-cased into
having less.

1. **`used lately`** — a section above `all models`, holding the models this machine has
   actually run in the recent window (the ledger's last fortnight of priced lines),
   dearest or most-recent first. A machine that has run nothing yet draws no section —
   not an empty heading. The rest of the catalog stays under **`all models`**.
2. **The fortnight spend chip** — dim, on the right of a `used lately` row (and on a row
   under `all models` when that model has spend in the fortnight). Shape:

   ```
   this fortnight $12 · 2.1M
   ```

   Money and tokens; either half drops when absent. Unknown → nothing. Never `$0.00`,
   never `0 tok`. Narrow widths drop the words first (`$12 · 2.1M`), then the tokens,
   then the chip.
3. **`/model used`** — opens the picker with the filter already meaning *only what I
   have run*: the list is the `used lately` set, and typing still narrows inside it.
   `/model used <text>` keeps that set and applies `<text>` as the ordinary filter
   tokens. A slug that is not in the set is still accepted by `/model <slug>` the way
   today's bare switch works; `used` is a filter word, not a refuse.

The placeholder keeps naming the keys it already names; it does not grow a novel essay.
The section labels are the person-facing strings above, exact.

### 6. Slash — `/spend` takes an argument

| form | what it does |
|---|---|
| `/spend` | opens the spend place on `rhythm` (today's arrival) |
| `/spend models` | opens on the `models` lens |
| `/spend days` | opens on the `days` lens |
| `/spend year` | opens on the `year` lens |
| `/spend export` | writes the current window's ledger reading to a file (same export family the surface already uses for a conversation; path rules follow `/export`) |
| `/spend 7d` | opens `rhythm` with the window set to the last seven days |
| `/spend month` | opens `rhythm` with the window set to the current calendar month |

Unknown words after `/spend` are refused in one short line naming what is accepted —
never opened as a second editor. `/cost` is untouched: still this conversation's bill
printed into the conversation.

## Words

- Lenses are named in the person's vocabulary: **rhythm**, **models**, **days**,
  **year**. Lowercase in the head row and in `/spend` args; Title Case only if a heading
  in the manual needs it for search.
- Picker sections: **`used lately`**, **`all models`** — those spellings, nowhere else.
- Chip: **`this fortnight $12 · 2.1M`** as the shape (figures vary; the words do not).
- Keys: `[` `]` cycle lenses; the existing spend time keys stay on `rhythm` (and on any
  lens that still owns a window).
- "limit" in person-facing money lines; never rail / ceiling / budget cap. No machinery
  words (`auditor`, `verdict`, `verified`, `refuted`).

## Explicit non-goals

- **No top-bar models tab.** Models are reached through `/model` and the doors that
  already open the picker. The tab bar stays `home  tasks  spend  settings`.
- **No themes.** The year heatmap is density on the existing palette, not a skin, not a
  seasonal costume, not a toggle.
- **No social wrapped.** No share image, no year-in-review card, no "you vs last year"
  poster. Streak / peak / favorite are quiet readings under the plane, and they obey the
  emptiness law when they have nothing to say.

## Acceptance (tests the build must add)

1. `[` `]` cycle `rhythm → models → days → year → rhythm` on the spend place; the head
   names the active lens; the Spending tab is never opened by those keys.
2. `enter` on a `days` row lands on `rhythm` for that day; `esc` returns to `days` on
   that row.
3. Models lens sorts by each column; grouping by role then subject then none; unbound
   models carry no invented role word; zero-spend models are absent.
4. Year lens draws no borders; streak / peak / favorite appear only when known; an empty
   year is the place's empty reading, not a grid of zeros.
5. Every picker door shows `used lately` above `all models` when the ledger has a
   fortnight row, and omits the section when it does not; the chip spells
   `this fortnight $… · …` and drops by the emptiness law.
6. `/model used` and `/model used <text>` filter to the used set; `/spend models|days|
   year|export|7d|month` do what the table says; an unknown `/spend` arg is one refuse
   line.
7. Manual: new sections in `models-and-cost.md`, and the `/spend` / `/model` / spend
   place passages updated in the same change. Gates green.

## Not done (follow-ups, not flags)

- A year rung on the existing `shift+↑` zoom is still refused on `rhythm`; the `year`
  lens is the year question, not a fifth grain of the window control.
- Export format details for `/spend export` (columns, path default) are settled in the
  build against `/export`'s existing path rules — this note only names the door.
- Home's pulse and the status line's money segment do not grow lens chrome; they keep
  pointing at the place and the Spending tab the way spending's design already says.
