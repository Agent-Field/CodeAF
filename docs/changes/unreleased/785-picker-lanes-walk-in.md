---
kind: changed
title: → walks into a model's lanes, a fold always opens, and a pinned lane rides the model's name
pr: 785
surface: [chat, docs]
invalidates:
  - "`→` or `tab` on a model in /model unfolded its lanes below it and left the cursor on the model, so with the model in use on the last row of the window the first `→` changed nothing on screen. It now moves the cursor into the fold — onto the pinned lane, else `auto` — and scrolls until the model and every lane are in view; `←` or `tab` walks back out."
  - "A model with no measured lanes did not unfold: `→` did nothing and the manual said so. It now opens onto `auto` and `openrouter` with the line `no machine has been measured for this model yet — machines show up after its first answer`, and queues the model's sheet with the beat. The settings panel's `lane` row opens the same fold instead of walking auto ↔ openrouter; the walk is left for a model the catalog does not carry."
  - "A pinned lane was shown only in the settings panel, inside an open picker, or as `via …` for ten minutes after an answer. The chrome now writes it on the model's name as `model@lane` — the seam, the status row and the phone deck's chip — from the pin the transport will actually send (not on `auto`, `openrouter`, a retired pin, a base that refuses the choice, routing `off`, or a hosted window). /status has a `lane` line under `model`; /status --json keeps `model` unchanged and gains a `lane` key. Pressing the name opens the picker on the pin."
  - "While /model was open the hint slot read `enter switch · esc` wherever the cursor stood. It now reads `→ lanes · enter switch · esc` on a model and `enter choose · ← back · esc` inside its lanes, with `tab` in place of the arrow where typed text is in the way."
  - "A lane whose belief had aged a day could print `tail 9223372036854775807s`. A p99 that is not finite or exceeds the transport's twenty-minute wall ceiling is now drawn as nothing, and the why line no longer claims `no tail` for it."
  - "Emptying the picker's filter with ctrl+u left the cursor on row zero. It returns to the model in use."
---

The owner opened /model, pressed ← and →, and asked how anybody changes the provider of
a model. Everything needed was built; the picker never said so. The design is in
docs/design/lanes-picker/DESIGN.md.
