# The model picker's lanes — walk in, always open, say where you are

The owner opened `/model` on 2026-09-10, pressed ← and →, and asked: *is left/right
supposed to select the provider or not? how do people change provider in a model?*

The machinery was complete. `→` on a row unfolded the machines behind a model, enter on
one pinned it (`lane.talk`), the settings panel said `lane • pinned: <name>`, the status
rider said `via <lane>` for ten minutes after an answer. What was wrong was four ways of
not saying so.

## What was true

1. **`→` opened the lanes off-screen.** The picker opens with the cursor on the model in
   use, which `listTop` leaves on the last row of the twelve-row window. The fold was
   appended below it and the cursor stayed put, so the first `→` anybody pressed changed
   nothing on the screen.
2. **`→` on an unmeasured model did nothing, silently** — while the row said
   `via together`. Only 52 of ~600 models in the owner's ledger had lane beliefs.
   `unfoldAt` refused an empty fold as "a gesture that punishes the person for trying it";
   the gesture that punished was the dead key. The settings panel's `lane` row offered
   `auto ↔ openrouter` for the same model, so the two doors disagreed.
3. **A pin was invisible outside the settings panel.** After pinning `inception` the frame
   head read `mercury-2.5 · ⠿ auto` — the thinking rung — and the owner concluded the pin
   had failed.
4. **The hint slot never mentioned the fold.** It read `enter switch · esc` wherever the
   cursor stood. The only `→ lanes` was the filter's placeholder, gone on the first
   typed character.

## The ruling: the picker is a tree, and the lane is written on the model

No new overlay, command or key.

- **A. `→` walks in; `←` walks out.** `→`/`tab` on a model opens the fold and moves the
  cursor into it — onto the pinned lane, else `auto` (`cursorToPin`) — and scrolls until
  the model and every row under it are in view (`revealFold`). `←`/`tab` inside closes it
  and returns to the model. Enter inside commits as before. Both doors read `foldKey`.
- **B. A fold always has its two answers.** A model with no measured lanes opens onto
  `auto` and `openrouter`, with one dim line where the machines would be
  (`laneUnmeasured`), no number, and `lane.WantSheet` queues its sheet with the beat.
  The settings `lane` row opens the same fold; `cycleLane` is left for a model the catalog
  does not carry.
- **C. The hint slot says what `→` does right now** (`picker.keysHint`):
  `→ lanes · enter switch · esc` on a model, `enter choose · ← back · esc` inside, and
  `tab` in place of the arrow where the caret has typed text to step over.
- **D. The pin is written on the model.** `app.modelWord` is the one place the chrome's
  model word is composed: basename, and `@lane` while a pin is in force — the `@` of
  `parseLaneTerm` and `/model @cloudflare`. The seam, the status row and the phone deck's
  chip draw it. It reads `provider.PinnedFor`, which is memory and already knows a retired
  pin and a base that refuses the choice. `/status` gains a `lane` line (so `--json`
  keeps `model` and gains one key). A press on the name opens the picker on the pin.

Under routing `off` the session sends no lane and measures nothing, so there is no fold
and no `@` — the capability is absent, not broken.

## Two defects on the same screens

- **`tail 9223372036854775807s`.** Ageing doubles a belief's spread per half-life, so a
  day-old belief's p99 is +Inf and `int(math.Round(+Inf))` is `MaxInt64`. `laneTail` draws
  no tail — and no `no tail` — for a p99 that is not finite or exceeds
  `provider.WallCeiling`.
- **ctrl+u left the cursor on row 0.** An empty box is the list the picker opened on, so
  `rank` puts the cursor back on the model in use (`cursorToCurrent`).

## Owed

- A lane pinned by name (`/model @x`) that the ledger has no belief about has no row in
  the fold, so the walk lands on `auto` and enter there un-pins.
