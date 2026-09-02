---
kind: fixed
title: a crew older than a seat still fills it, and the run says which row it took
pr: 311
surface: [build, engine, docs]
invalidates:
  - "A crew profile pinned every seat. One written before #278 has no `models.tiers.worker` and never did; the work seat of every headless run came from it by inheritance, not from the key."
  - "An unset tier key meant one thing: this build's default. It now means two — a key nobody ever held asks the row it was split out of first, and only a profile with no tier keys at all reaches `DefaultModel`."
  - "`SeatSource` had four rungs and a seat's rung read `crew <preset>` or `default`. There are five: `inherited` is the crew answering through an older row, and it prints as `crew custom, inherited` in the models line and in `--json`'s `model_source`."
  - "A door printed `seats.Line()`. It prints `seats.Report()` — the models line plus the one line saying a seat was inherited — and a door that prints only the line swallows the reason."
---

The worker row landed in #278 and the profiles people already had did not gain a
key for it, so the ladder fell past four pinned rows to the shipped default and
said nothing but the word `default` in a line nobody reads twice (#302). The rule
now lives in one table, `tierLineage`: a tier with no key of its own inherits
from the row it was split out of and reports that it did, and every tier names an
ancestor or names none explicitly, so the next seat added cannot regress this
silently. The one line a person reads is said where the seats are reported and
nowhere else — a warning on every call of a run is noise somebody learns to read
past, which leaves them exactly where the silence did.
