---
kind: fixed
title: One thinking ladder, walked the same way at every door a person can turn it
pr: 745
surface: [chat, docs]
invalidates:
  - The model picker's ctrl+t walked a list of its own — off, low, medium, high — while the
    ladder has five rungs and the thinking settings row offers all five. nextReasoning
    answered "" for any word not on that list, so a level it did not recognise was CLEARED
    rather than climbed - `aforge --reasoning xhigh` lost its pin to one keypress and the next
    turn went out with no reasoning object at all, and xhigh and max could not be reached from
    the picker at any number of presses. The walk is now auto → low → medium → high → xhigh →
    max → auto, off effort.Rungs, and it is the same step a task's own thinking control takes.
  - The picker and a task's control each had their own answer for what happens off the top of
    the wheel. Both now take internal/tui3's effortNextClearing, the wheel for the two scopes
    whose absence means "hand this back to whatever stands above it" and which the surface is
    the only door onto. The conversation chip and a standing item keep effortNext, which never
    lands on absence, because those rungs are cleared where they are written down.
  - SetTaskEffort refused an unknown word with "Use one of, low, medium, high, xhigh, max, or
    off" while the settings row, the task control and the manual all call absence auto. The
    refusal ends ", or auto". off and "" are still read - a profile written by an older build
    means what it meant.
  - The thinking row in /settings offered ctrl+v "on home with the cursor on no row" as a door
    onto the install's rung. That card was retired when ↑ off the top of home's list started
    reaching the tab bar; the row now names the doors that exist, ctrl+t in /model among them.
    keys.md and models-and-cost.md said the same retired thing and no longer do.
  - commands.md and models-and-cost.md said the picker walks off → low → medium → high → off
    and "does not offer xhigh or max", while task-controls.md described the six-stop walk. All
    three now read the walk, the row's choices and the shipped default out of
    config.EffortChoices and config.EffortWord(effort.Ship) through internal/manual's truth
    table, so a page carrying an old spelling goes red with its owner named.
  - Three sentences outlived their code and are corrected. A standing item's firing and its
    sentinel have carried no low floor since the resolver's roleFloor kept only errands, so
    models-and-cost.md's precedence list, home.md's reminder page and the thinking row's own
    hint no longer promise one - nothing but the item's own rung reaches a firing, and an item
    nobody has dialled asks for nothing.
  - TestCtrlTCyclesTheLevelAndSkipsModelsThatTakeNone asserted the four-step cycle this change
    calls the defect. Its want is the ladder's six stops now; the rest of the test - a row
    carries its level, a model with no reasoning knob is not offered one - is unchanged.
---

A pin that one keypress throws away is a knob that does nothing for whoever set
it, which is the law #643 settled on the wire and this is the surface's half of
it. What made the picker able to drop a level at all was that it held a second
list of what the rungs are: a dial with two vocabularies has a word each surface
does not recognise, and the honest thing to do with a word you do not recognise
is not to clear it. The ladder is written down once, the walk off it is written
down once, and the pages are filled from both rather than repeating them.

The shipped default is untouched: it is still absence, which #653 chose
deliberately, and the pages now say so in a sentence the code fills in.
