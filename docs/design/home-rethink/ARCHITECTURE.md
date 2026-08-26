# Home rethink — the architecture the code must have

Read after `LANES.md`. This is the shape the switcher settles into; every lane that lands after
2026-08-25 evening builds to it, and one refactor lane brings what landed before it into line.
The owner's standing order: **modular, clean, efficient — no band-aids.**

## The three layers, and what each may know

```
seams        internal/session, internal/store, internal/standing, cmd/aforge/chatv3.go
             — every fact a place reads, behind Options; cached on home's 3-second clock,
             never read on a draw or a keystroke.

readings     internal/tui3/{switcher,tasksplace,standingplace,memoryplace,spendplace,searchplace}.go
             — PURE: (data, window, width, palette) → rows / stops / verbs. No app, no clock,
             no I/O. Tested with fixtures at 60/80/120/200 columns. Shared prose in placeprose.go.

places       internal/tui3/place_*.go — one thin struct per place: its cursor, its window, its
             open folds, its cached reading; implements the `place` interface below. Nothing else.

frame        internal/tui3/pages.go (registry + placeFrame + showPage), placekeys.go (the one key
             grammar), verbstrip.go (the strip), placebodies.go (the shared foot: composer, chip,
             note, hint). These files know the INTERFACE and never a concrete place.
```

A lower layer never imports the one above it; a reading never sees `*app`.

## The `place` interface — the only contract a page has

```go
// place is what a page of the switcher must be able to do. The registry is the
// one thing that knows all of them; NOTHING SWITCHES ON A PAGE ID outside it.
type place interface {
	id() page
	open(a *app) tea.Cmd          // prime caches on entry, once — never per frame
	close(a *app)                 // write the look stamp, drop the strip, keep folds
	tick(a *app, now time.Time)   // home's 3-second beat: refresh the cached reading
	body(a *app, width, room int) []placeRow[hit]   // rows + the hit map the frame stores
	stops() []int                 // cursor-legal rows of the last body; the frame moves the cursor
	enter(a *app) tea.Cmd         // the row under the cursor, opened
	verbs(a *app) []verb          // the → strip for the row under the cursor
	alt(a *app, letter rune) bool // "show this place differently"; false = not mine
	window(a *app, key string) bool // shift+arrows; false = this place has no window
	box(a *app) *editor           // the composer this place types into (the shared one by default)
	note(a *app, width int) []string
	hint(a *app) string
	changed(a *app, since time.Time) int   // the tab's count
}
```

- `placeBase` gives defaults for everything a place does not need (no window, no alt, shared
  box, empty note), so a place file is only what is particular to it.
- The registry: `var placeRegistry = map[page]place{}` filled by each `place_*.go`'s `init`,
  exactly as `registerHomeBand` does. `pages()` reads the registry's order table.
- The app holds ONE field, `a.page` (`pageNone` = the conversation), plus `a.places map[page]place`
  state. The six `open bool`s and `standDownFullscreen`'s seven-way close are retired: `showPage`
  is `current.close(a); a.page = id; next.open(a)`. The rewind sheet stays a fullscreen page and
  is not a place; it keeps its own `open`.
- `placeFrame` is the only frame. It asks the place for `body`, `note`, `hint`, `box`; it draws the
  pulse, the tab bar (counts from `changed`), the rule, the body, the composer, the hint, and writes
  the hit maps. A place never draws chrome.
- Keys: `placeKey` is the whole six-class grammar and calls the interface (`alt`, `window`,
  `verbs`, `enter`, `box`); anything it does not take goes to the place's own reading of the
  key ONLY through `enter`/`verbs`/`alt`/`window` — a place has no private key table. Digits keep
  the drawn-answer path.

## Per-place files

| Place | file | reading layer | state it keeps |
| --- | --- | --- | --- |
| home | `place_home.go` | `switcher.go` (+ the typed drop-up in home.go) | cursor, grouped, hideQuiet, folds, card fold state |
| tasks | `place_tasks.go` | `tasksplace.go` | cursor, window, filter |
| standing | `place_standing.go` | `standingplace.go` | cursor, folds |
| memory | `place_memory.go` | `memoryplace.go` | cursor, open shelves, filter |
| spend | `place_spend.go` | `spendplace.go` | cursor, window, `session.UsageCache` |
| search | `place_search.go` | `searchplace.go` | ask generation, hits, cursor |
| settings | `place_settings.go` | (its own sections, already tabular) | as today, behind the interface |

The old `taskSheet`, `standPage`, `memoryPanel` structs are either the place's state struct
(renamed) or deleted; no page keeps two states. `home.go` shrinks to the typed surface, the doors
(`homeOpenLine` and its refusals), the clock, and the card; the tiers/strips/elsewhere machinery
that the switcher replaced is removed, not dead-coded.

## Laws the refactor lane pins with tests

1. `TestNothingSwitchesOnAPageIdOutsideTheRegistry` — greps `internal/tui3/*.go` (non-test) for
   `case page`/`switch a.page`/`== page` outside `pages.go`; zero hits.
2. `TestEveryPlaceIsRegisteredOnceAndInTabOrder`.
3. `TestEveryPlaceTakesExactlyTheWholeFrameAtEveryWidth` — one loop over the registry at
   60/80/120/160/200 columns, replacing per-page copies.
4. `TestAPlaceNeverReadsTheDiskOnADraw` — the syscall counters home already has (`homeFolderThere`,
   the stat counter) plus a fake seam that panics when called from `body`.
5. `TestTheReadingLayersImportNoApp` — a reading file must not mention `*app`.
6. `TestOnePlaceOneFile` — each registered place has its `place_<word>.go`.

## What "no band-aid" means for a lane

- A feature is a new place file, a new reading function, or a new seam — never a new `case`.
- A fact spelled twice is a bug (`placeprose.go` holds the one spelling).
- A test that pins a shape the design replaced is rewritten to pin the law it protected; it is not
  skipped, not loosened, not deleted without saying which law died.
- A refusal is a sentence the person reads, in one place (`a.say`), not a silent `return`.
- Caches live on the place and refresh on `tick`; a keystroke rebuilds rows from the cache only.
