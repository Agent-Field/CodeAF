---
kind: fixed
title: the add context sheet leaves no cell of itself behind, and always draws its way out
pr: 742
surface: [chat, docs]
invalidates:
  - "Closing the add context sheet was believed to put the conversation back whole. It did not: the sheet is the one surface that COVERS the frame rather than replacing it, the rows revealed underneath end short of where its right edge was, and the terminal kept whatever the frame-to-frame diff did not overwrite — so its right-hand slice stayed on screen, survived typing, and cleared only when something else repainted those cells full width. Every door out now asks for a whole-screen repaint on the way."
  - "`choosing-a-folder.md` said `esc · cancel` stays on the sheet's bottom edge on a terminal with too few rows. That was the intent and not the behaviour: below five rows the sheet built one row more than the terminal had and the foot rule fell off the bottom, while `contextWin` was recorded from the rows BUILT — so the hit map offered a cancel target on a row nobody could click. The page is true again."
  - "`scripts/context-modal-native-fixture.sh` was believed to exercise the half-cell picture renderer. Its `pixel.png` failed CRC32 on both IHDR and IDAT, so the row previewed `this picture could not be opened` and the renderer was never reached. The picture opens now."
  - "`docs/remote-access-testing.md` §3e listed `/attach takes a path` and `<name> is a folder` under refusals to try. #657 deleted both — a bare `/attach` opens the sheet and a directory registers the folder — so a reader following the runbook recorded a PASS for behaviour nobody checked."
---

Padding our own rows cannot repair the residue and was not attempted: the renderer
clears its cell buffer before every frame, so trailing spaces and absent cells
produce the same diff. The one lever is a full repaint, asked for at the one moment
this surface has a layer to undraw, and routed through a single `closeContextSheet`
so no way out can leave the layer standing.

The sheet's rows now yield in one order — the browser's own rows, then the thin rule
under the box, then the box — which leaves the head rule and the foot rule as the
last two to go: four rows draw `add context`, the box, the rule and the way out.
With no box on screen there is nowhere to type, so the frame hides the caret rather
than blinking it at a row that is not there.
