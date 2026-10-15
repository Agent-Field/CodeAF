---
kind: fixed
title: a finished reminder moved later with e wakes again, and a save after its new moment is refused
pr: 1836
surface: [chat, engine, docs]
invalidates:
  - "The edit line `e` puts in the box wrote a one-time moment to the minute, so a reminder made with `in 2m` came back a few seconds earlier and the next minute in the box could be seconds away. It keeps the seconds (`at 2026-10-14T20:33:13`), and the line saved unedited is the same moment."
  - "`Store.Update` saved a new one-time moment that had already passed while the card was up, so the clock said the reminder at once and the row was finished again. It refuses it with `… has already passed`, and nothing changes."
  - "A finished reminder's own edit line was refused as `… has already passed` even when only its title changed. Only a line that moves the moment into the past is refused."
  - "The typed edit's card worked its next run out from the schedule alone, so a paused one or an unchanged rhythm promised a run the save did not give. Both doors use `automation.NextAfterChange`, the rule the save keeps."
---
