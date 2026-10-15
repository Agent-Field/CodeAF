---
kind: fixed
title: a finished reminder moved later with e wakes again, and a save after its new moment is refused
pr: 1837
surface: [chat, engine, docs]
invalidates:
  - "The edit line `e` puts in the box wrote a one-time moment to the minute, so a reminder made with `in 2m` came back a few seconds earlier and the next minute in the box could be seconds away. It keeps the seconds (`at 2026-10-14T20:33:13`), a moment in the hour a fall-back repeats is written with its offset, and the line saved unedited is the same moment."
  - "`Store.Update` saved a new one-time moment that had already passed while the card was up, so the clock said the reminder at once and the row was finished again. It refuses it with `… has already passed` (to the second when it passed in the current minute), and nothing changes. `Store.Create` is unchanged: an add answered late still runs once, late."
  - "A typed edit of a paused automation said `saved … · next …` for a slot it does not run at. It says `saved … · still paused — resume starts it again`."
  - "The manual did not say a change can be refused at save time, or that a finished reminder can be renamed. `automations.md` has a section for both."
  - "A finished reminder's own edit line was refused as `… has already passed` even when only its title changed. Only a line that moves the moment into the past is refused."
  - "The typed edit's card worked its next run out from the schedule alone, so a paused one or an unchanged rhythm promised a run the save did not give. Both doors use `automation.NextAfterChange`, the rule the save keeps."
---
