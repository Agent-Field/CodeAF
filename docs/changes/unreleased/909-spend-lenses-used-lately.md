---
kind: added
title: spend lenses (rhythm/models/days/year) and a framed /model sheet with used-lately
pr: 909
surface: [chat]
invalidates:
  - "The spend place was one reading — sparkline, what ran it, what it was for. It has four lenses now (`rhythm`, `models`, `days`, `year`), cycled with `[` and `]`, and `/spend models|days|year|export|7d|month` open them by name."
  - "`/spend` took no argument and opened the place alone. Words after it pick a lens, a window, or write a JSON export of the current window's priced lines beside the usage ledger."
  - "A day on the spend place was never a door. On the `days` lens, `enter` drills into that day's `rhythm` window, and `esc` returns to `days`."
  - "The model picker ranked the catalog with no memory of what you had paid. Every door onto it (`/model`, Providers, task model, home draft, composer) now draws `used lately` above `all models` when the fortnight ledger has priced rows, and a dim chip `this fortnight $12 · 2.1M` on those rows (absent when unused)."
  - "`/model` (and the status model word) used to be bottom chrome — a filter box in the draft's place with a short list under it. It is a framed sheet over a faded conversation now: title `choose model` (or `choose task model`), filter inside the sheet, `esc · cancel` on the foot, the same covering grammar as the context chooser. Settings, home and the composer still embed the same picker type in their bodies."
  - "`/model used` was not a filter. It opens the same picker already narrowed to models this machine has run in the last fortnight; `used` is a filter word, not a refuse of bare `/model <slug>`."
  - "`session.ModelSpend` and `DaySpend` carried only a token sum. They carry `Input` and `Output` as well, so the Models and Days lenses can compare the two halves; `PeakHour` and `ActiveStreak` answer the Year lens facts."
---

The Spending tab remains the only editor for the rails. Lenses are readings of the
same machine ledger; `/cost` is still this conversation's tree bill.
