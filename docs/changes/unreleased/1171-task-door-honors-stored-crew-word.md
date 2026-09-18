---
kind: fixed
title: the task door honors a stored crew word, and keeps its pins under the learned pick
pr: 1171
surface: [chat, engine]
invalidates:
  - "`models.crew` was derived from the five tier rows and no reader consulted a stored one, so a run that wrote a single crew word into config.json and none of the tier rows seated the balanced crew whatever word it named. A stored preset word now names the budget its seats run at, and a seat nobody wrote reads that word's own row."
  - "Under `models.crew.pick=learn` a class row holding the preset's own table value was recomputed by the pick, so a run that pinned the crew table's own ids had them overridden. A class row written on its own is now a pin the pick leaves alone — and a stored crew word marks every row beside it as one — while a `/crew` apply, which writes all five rows at once, keeps the old computation."
---

The v3 task door resolves its worker seat through `internal/roles`
(`session`'s `defaultTaskModel` reads `roles.TierWorker` off the key map
`cmd/codeaf`'s `v3RolesSource` builds), and that map fills the seat from
`internal/config`'s tier ladder. The crew word a run stored was read by nothing:
the preset is derived from the five tier rows (`crewPresetUnder`), and a run that
wrote one word instead of the rows fell to the default preset — balanced — for
every arm.

`config.storedCrewWord` reads a `models.crew` word that names a preset, and both
the preset reading (`crewPresetUnder`, `crewStoredAt`) and the two ladders
(`tierSeatUnder`, `resolveSeat`) honor it: a seat nobody wrote reads that word's
own table row (`unwrittenSeat`). And `pickedSeat` now treats a class row written
on its own as a pin: it leaves the row alone when the profile stores the crew
word, when the five tier rows are not all written (`allTiersWritten` — a hand pin
writes one, a `/crew` apply writes all five), or when the row's id is not the
preset's own.
